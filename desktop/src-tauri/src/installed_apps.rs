// desktop/src-tauri/src/installed_apps.rs
//
// Tauri command `list_installed_apps` — enumerates ALL installed user-facing
// applications (not just running ones) for the redesigned App Bypass page.
// macOS: filesystem scan of standard .app dirs, reading each Info.plist.
// Windows: registry Uninstall hive scan (added in a later task).
// Other targets: empty list (Linux daemon serves its own path).
//
// camelCase serde so the JS bridge sees id / processNames / paths / iconUrl /
// installerPackageName (matches webapp InstalledApp).

use serde::Serialize;

#[derive(Serialize, Debug, Clone, PartialEq)]
#[serde(rename_all = "camelCase")]
pub struct InstalledApp {
    pub id: String,
    pub label: String,
    pub process_names: Vec<String>,
    /// Directories whose every executable belongs to this app (macOS: the
    /// `.app` bundle). The engine matches a process by where its executable
    /// lives (`match.app_paths`), so nothing inside has to be enumerated.
    #[serde(skip_serializing_if = "Vec::is_empty")]
    pub paths: Vec<String>,
    pub icon_url: Option<String>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub installer_package_name: Option<String>,
}

/// Pure helper: from a bundle dir name like "WeChat.app" return the default
/// label ("WeChat"). Used when Info.plist has no CFBundleName.
#[cfg_attr(not(target_os = "macos"), allow(dead_code))]
fn label_from_bundle_dir(dir_name: &str) -> String {
    dir_name.strip_suffix(".app").unwrap_or(dir_name).to_string()
}

// ---------------------------------------------------------------------------
// Windows registry-value helpers.
//
// Pure string/path logic kept OUTSIDE the #[cfg(target_os = "windows")]
// module so the tests run on every platform (cfg-tagged code never executes
// on dev machines — the same failure mode k2 solved by extracting
// provider/process_wintable.go). Only the actual winreg/syscall code stays
// behind the cfg.
//
// Windows paths are manipulated as strings here, never through std::path —
// Path::new("C:\\x\\y.exe").parent() is platform-dependent (on Unix the
// backslashes are ordinary characters), which would make these helpers pass
// on Windows and silently misbehave in cross-platform tests, or vice versa.
// ---------------------------------------------------------------------------
#[cfg_attr(not(target_os = "windows"), allow(dead_code))]
mod winreg_util {
    /// Last path separator cut: `C:\Dir\App.exe` → `C:\Dir`. Returns None for
    /// separator-less strings and bare drive roots (`C:\App.exe` → `C:` is not
    /// a directory we should ever enumerate).
    fn parent_dir_of(exe_path: &str) -> Option<String> {
        let cut = exe_path.rfind(['\\', '/'])?;
        let dir = exe_path[..cut].trim_end();
        if dir.is_empty() || (dir.len() <= 2 && dir.ends_with(':')) {
            return None;
        }
        Some(dir.to_string())
    }

    /// Install dir from a DisplayIcon value: `"C:\Dir\App.exe",0` /
    /// `C:\Dir\App.exe,0` / `C:\Dir\App.ico`. The `,N` icon-index suffix is
    /// stripped only when the tail is numeric — a comma inside a directory
    /// name must survive.
    pub fn dir_from_display_icon(raw: &str) -> Option<String> {
        let mut s = raw.trim();
        if let Some(i) = s.rfind(',') {
            let tail = s[i + 1..].trim();
            if !tail.is_empty() && tail.chars().all(|c| c.is_ascii_digit() || c == '-') {
                s = &s[..i];
            }
        }
        parent_dir_of(s.trim().trim_matches('"').trim())
    }

    /// Install dir from an UninstallString: `"C:\Dir\Uninst.exe" /S` or the
    /// unquoted-with-spaces form NSIS also writes (`C:\Dir\Uninstall App.exe`).
    /// For unquoted values everything through the first `.exe` is the path —
    /// the argument split that ignores spaces is the same heuristic
    /// uninstall-locator tools settled on. `MsiExec.exe /X{…}` has no
    /// separator and correctly yields None.
    pub fn dir_from_uninstall_string(raw: &str) -> Option<String> {
        let s = raw.trim();
        let exe: &str = if let Some(rest) = s.strip_prefix('"') {
            rest.split('"').next()?
        } else {
            let i = s.to_ascii_lowercase().find(".exe")?;
            &s[..i + 4]
        };
        parent_dir_of(exe.trim())
    }

    /// Refuse directories that must never be enumerated for process names:
    /// anything under the Windows directory (an MsiExec-style value that
    /// slipped through would otherwise attribute half the OS to one app),
    /// bare drive roots, BARE shared roots like `C:\Program Files` (a known
    /// installer-authoring bug writes `[ProgramFilesFolder]` without the
    /// product subfolder — scanning it would attribute every installed app's
    /// exes to one Uninstall entry), and non-drive-absolute paths
    /// (UNC/relative) where we can't reason about what we'd be scanning.
    /// Product subdirectories UNDER the shared roots are of course fine —
    /// only the exact root is refused.
    pub fn is_unsafe_install_dir(dir: &str) -> bool {
        let d = dir.replace('/', "\\").to_ascii_lowercase();
        let d = d.trim_end_matches('\\');
        let b = d.as_bytes();
        if b.len() < 2 || !b[0].is_ascii_alphabetic() || b[1] != b':' {
            return true; // UNC, relative, or empty
        }
        let rest = &d[2..];
        if rest.is_empty() || rest == "\\windows" || rest.starts_with("\\windows\\") {
            return true;
        }
        matches!(
            rest,
            "\\program files"
                | "\\program files (x86)"
                | "\\programdata"
                | "\\users"
                | "\\program files\\windowsapps"
        )
    }

    /// Known multi-process apps whose traffic-bearing executables live OUTSIDE
    /// the install tree, so directory scanning cannot discover them. WeChat 4.x
    /// unpacks the WMPF runtime (`WeChatAppEx.exe` — mini-programs, Channels,
    /// the built-in browser) into per-user AppData at run time; the engine
    /// matches by basename, so pinning the family here is sufficient. Keyed by
    /// the Uninstall registry key name (`Weixin` = 4.x, `WeChat` = 3.x).
    pub fn supplemental_process_names(key_name: &str) -> &'static [&'static str] {
        match key_name {
            "Weixin" | "WeChat" => &[
                "Weixin.exe",
                "WeChat.exe",
                "WeChatAppEx.exe",
                "WeChatOCR.exe",
                "WeChatPlayer.exe",
                "WeChatUtility.exe",
                "WeixinExt.exe",
            ],
            _ => &[],
        }
    }

    /// Recursively collect `.exe` basenames under `dir` (depth-bounded).
    /// Depth 3 covers the "launcher at root + versioned subdirectory" layout
    /// NSIS apps favour (WeChat `Weixin\4.1.12.55\…`, Douyin
    /// `douyin\8.4.0\tray\…`). Uninstaller stubs are skipped — they never
    /// carry app traffic and would only bloat the generated rule.
    pub fn collect_exes(dir: &std::path::Path, out: &mut Vec<String>, depth: usize) {
        if depth > 3 {
            return;
        }
        let Ok(entries) = std::fs::read_dir(dir) else { return };
        for e in entries.flatten() {
            let p = e.path();
            if p.is_dir() {
                collect_exes(&p, out, depth + 1);
            } else if p
                .extension()
                .and_then(|s| s.to_str())
                .map(|s| s.eq_ignore_ascii_case("exe"))
                .unwrap_or(false)
            {
                if let Some(base) = p.file_name().and_then(|s| s.to_str()) {
                    if base.to_ascii_lowercase().starts_with("unins") {
                        continue;
                    }
                    let b = base.to_string();
                    if !out.contains(&b) {
                        out.push(b);
                    }
                }
            }
        }
    }
}

#[cfg(test)]
mod tests {
    use super::winreg_util::*;
    use super::*;
    use std::fs;

    #[test]
    fn label_strips_dot_app() {
        assert_eq!(label_from_bundle_dir("WeChat.app"), "WeChat");
        assert_eq!(label_from_bundle_dir("No Suffix"), "No Suffix");
    }

    #[test]
    fn display_icon_variants() {
        assert_eq!(
            dir_from_display_icon(r#""C:\Program Files\Tencent\Weixin\Weixin.exe",0"#).as_deref(),
            Some(r"C:\Program Files\Tencent\Weixin")
        );
        assert_eq!(
            dir_from_display_icon(r"C:\Apps\Foo\foo.exe,-1").as_deref(),
            Some(r"C:\Apps\Foo")
        );
        // Plain icon path, no index.
        assert_eq!(
            dir_from_display_icon(r"C:\Apps\Foo\app.ico").as_deref(),
            Some(r"C:\Apps\Foo")
        );
        // Comma inside the path must not be treated as an icon index.
        assert_eq!(
            dir_from_display_icon(r"C:\Apps\a,b\x.exe").as_deref(),
            Some(r"C:\Apps\a,b")
        );
        assert_eq!(dir_from_display_icon(""), None);
        assert_eq!(dir_from_display_icon("no-separator.exe"), None);
        // Bare drive root is not a scannable directory.
        assert_eq!(dir_from_display_icon(r"C:\app.exe"), None);
    }

    #[test]
    fn uninstall_string_variants() {
        assert_eq!(
            dir_from_uninstall_string(r#""C:\Program Files\Tencent\Weixin\Uninstall.exe" /S"#)
                .as_deref(),
            Some(r"C:\Program Files\Tencent\Weixin")
        );
        // Unquoted with spaces — the Douyin/NSIS form.
        assert_eq!(
            dir_from_uninstall_string(
                r"C:\Program Files (x86)\ByteDance\douyin\Uninstall douyin.exe"
            )
            .as_deref(),
            Some(r"C:\Program Files (x86)\ByteDance\douyin")
        );
        // MsiExec has no path separator → no directory to derive.
        assert_eq!(
            dir_from_uninstall_string(r"MsiExec.exe /X{9A25302D-30C0-39D9-BD6F-21E6EC160475}"),
            None
        );
        assert_eq!(dir_from_uninstall_string(""), None);
    }

    #[test]
    fn unsafe_install_dirs() {
        assert!(is_unsafe_install_dir(r"C:\Windows"));
        assert!(is_unsafe_install_dir(r"C:\Windows\System32"));
        assert!(is_unsafe_install_dir(r"c:\windows\syswow64\"));
        assert!(is_unsafe_install_dir(r"C:\"));
        assert!(is_unsafe_install_dir(r"\\server\share\app"));
        assert!(is_unsafe_install_dir(r"relative\dir"));
        // BARE shared roots: a buggy InstallLocation like `C:\Program Files`
        // would otherwise fold every installed app's exes into one entry.
        assert!(is_unsafe_install_dir(r"C:\Program Files"));
        assert!(is_unsafe_install_dir(r"C:\Program Files (x86)"));
        assert!(is_unsafe_install_dir(r"c:\program files\"));
        assert!(is_unsafe_install_dir(r"C:\ProgramData"));
        assert!(is_unsafe_install_dir(r"C:\Users"));
        assert!(is_unsafe_install_dir(r"C:\Program Files\WindowsApps"));
        // …but product subdirectories under them are the normal, safe case.
        assert!(!is_unsafe_install_dir(r"C:\Program Files\Tencent\Weixin"));
        assert!(!is_unsafe_install_dir(r"C:\Program Files (x86)\ByteDance\douyin"));
        assert!(!is_unsafe_install_dir(r"C:\ProgramData\SomeApp"));
        assert!(!is_unsafe_install_dir(r"C:\WindowsApps-like\dir")); // prefix ≠ path component
        assert!(!is_unsafe_install_dir(r"D:\Games\Steam"));
    }

    #[test]
    fn wechat_family_supplement() {
        for key in ["Weixin", "WeChat"] {
            let names = supplemental_process_names(key);
            assert!(names.contains(&"WeChatAppEx.exe"), "{key}: {names:?}");
            assert!(names.contains(&"Weixin.exe"));
        }
        assert!(supplemental_process_names("douyin").is_empty());
        assert!(supplemental_process_names("").is_empty());
    }

    // Versioned-subdirectory layout (WeChat/Douyin): launcher at the root,
    // real executables in `<version>\` and `<version>\tray\`. Uninstaller
    // stubs at any depth are excluded; duplicates collapse.
    #[test]
    fn collect_exes_versioned_layout() {
        let root = std::env::temp_dir().join("k2_installed_apps_win_collect_test");
        let _ = fs::remove_dir_all(&root);
        let touch = |dir: std::path::PathBuf, name: &str| {
            fs::create_dir_all(&dir).unwrap();
            fs::write(dir.join(name), b"MZ").unwrap();
        };
        touch(root.clone(), "douyin.exe");
        touch(root.clone(), "Uninstall douyin.exe"); // filtered
        touch(root.join("8.4.0"), "douyin.exe"); // dup of root basename
        touch(root.join("8.4.0"), "app_shell_updater.exe");
        touch(root.join("8.4.0").join("tray"), "douyin_tray.exe");
        touch(root.join("8.4.0").join("tray"), "push_detect.exe");
        touch(root.join("8.4.0").join("tray"), "unins000.exe"); // filtered
        touch(root.join("8.4.0").join("tray").join("deep"), "too_deep_ok.exe"); // depth 3 → still collected
        touch(
            root.join("8.4.0").join("tray").join("deep").join("deeper"),
            "beyond_depth.exe",
        ); // depth 4 → cut off

        let mut out = Vec::new();
        collect_exes(&root, &mut out, 0);

        assert!(out.contains(&"douyin.exe".to_string()));
        assert_eq!(out.iter().filter(|n| *n == "douyin.exe").count(), 1);
        assert!(out.contains(&"app_shell_updater.exe".to_string()));
        assert!(out.contains(&"douyin_tray.exe".to_string()));
        assert!(out.contains(&"push_detect.exe".to_string()));
        assert!(out.contains(&"too_deep_ok.exe".to_string()));
        assert!(!out.contains(&"beyond_depth.exe".to_string()), "{out:?}");
        assert!(!out.iter().any(|n| n.to_ascii_lowercase().starts_with("unins")), "{out:?}");

        let _ = fs::remove_dir_all(&root);
    }
}

#[cfg(target_os = "macos")]
mod macos {
    use super::*;
    use std::path::{Path, PathBuf};

    const SCAN_DIRS: &[&str] = &["/Applications", "/System/Applications"];

    fn home_apps_dir() -> Option<PathBuf> {
        std::env::var_os("HOME").map(|h| Path::new(&h).join("Applications"))
    }

    /// Read CFBundleName / CFBundleIdentifier / CFBundleExecutable from a
    /// bundle's Info.plist. Returns None if not a usable app bundle.
    fn read_bundle(app_path: &Path) -> Option<InstalledApp> {
        let plist_path = app_path.join("Contents/Info.plist");
        let value = plist::Value::from_file(&plist_path).ok()?;
        let dict = value.as_dictionary()?;

        let bundle_id = dict
            .get("CFBundleIdentifier")
            .and_then(|v| v.as_string())
            .map(|s| s.to_string());
        // Hide Apple first-party apps — bypass use cases target 3rd-party apps.
        if let Some(ref id) = bundle_id {
            if id.starts_with("com.apple.") {
                return None;
            }
        }

        let dir_name = app_path.file_name().and_then(|s| s.to_str()).unwrap_or("");
        let label = dict
            .get("CFBundleName")
            .and_then(|v| v.as_string())
            .map(|s| s.to_string())
            .unwrap_or_else(|| label_from_bundle_dir(dir_name));

        // The app is identified by its bundle directory: the engine matches
        // any process whose executable lives under it (match.app_paths), so
        // helpers, sub-apps, XPC services and crash handlers are covered
        // wherever they sit in the bundle — without opening a single file in
        // it. (The previous approach walked the tree for executable basenames:
        // 387k files across /Applications on a dev Mac, and basenames collide
        // across apps — every Chromium app ships `chrome_crashpad_handler`.)
        //
        // process_names keeps the main executable only: it feeds the "common
        // domestic app" classifier, and it is the fallback identity when the
        // app runs from somewhere else (App Translocation gives a quarantined
        // app a randomized path). Case is PRESERVED — the engine's Darwin
        // process matcher is case-sensitive.
        let process_names: Vec<String> = dict
            .get("CFBundleExecutable")
            .and_then(|v| v.as_string())
            .filter(|exe| !exe.is_empty())
            .map(|exe| vec![exe.to_string()])
            .unwrap_or_default();

        // id = bundle path (stable, also the icon key); icon via kaitu-icon.
        let id = app_path.to_string_lossy().to_string();
        let icon_url = Some(format!(
            "kaitu-icon://bundle/{}",
            urlencoding::encode(&id)
        ));

        Some(InstalledApp {
            paths: vec![id.clone()],
            id,
            label,
            process_names,
            icon_url,
            installer_package_name: None,
        })
    }

    pub fn enumerate() -> Result<Vec<InstalledApp>, String> {
        let mut dirs: Vec<PathBuf> = SCAN_DIRS.iter().map(PathBuf::from).collect();
        if let Some(h) = home_apps_dir() {
            dirs.push(h);
        }
        let mut seen: std::collections::HashSet<String> = std::collections::HashSet::new();
        let mut out: Vec<InstalledApp> = Vec::new();
        for dir in dirs {
            let Ok(entries) = std::fs::read_dir(&dir) else {
                continue;
            };
            for e in entries.flatten() {
                let p = e.path();
                if p.extension().and_then(|s| s.to_str()) != Some("app") {
                    continue;
                }
                if let Some(app) = read_bundle(&p) {
                    if seen.insert(app.id.clone()) {
                        out.push(app);
                    }
                }
            }
        }
        out.sort_by(|a, b| a.label.to_lowercase().cmp(&b.label.to_lowercase()));
        Ok(out)
    }

    #[cfg(test)]
    mod tests {
        use super::*;
        use std::fs;

        fn scratch(name: &str) -> std::path::PathBuf {
            let root = std::env::temp_dir().join(format!("k2_installed_apps_{name}_{}", std::process::id()));
            let _ = fs::remove_dir_all(&root);
            root
        }

        fn write_plist(app: &Path, body: &str) {
            fs::create_dir_all(app.join("Contents")).unwrap();
            fs::write(
                app.join("Contents/Info.plist"),
                format!(
                    r#"<?xml version="1.0" encoding="UTF-8"?>
<plist version="1.0"><dict>{body}</dict></plist>"#
                ),
            )
            .unwrap();
        }

        // The bundle directory IS the app's identity: one path covers every
        // executable inside, so read_bundle must not need (or look at) any of
        // them — only Info.plist.
        #[test]
        fn bundle_is_identified_by_its_path_and_main_executable() {
            let root = scratch("ident");
            let app = root.join("QQ.app");
            write_plist(
                &app,
                "<key>CFBundleIdentifier</key><string>com.tencent.qq</string>\
                 <key>CFBundleName</key><string>QQ</string>\
                 <key>CFBundleExecutable</key><string>QQ</string>",
            );
            // Helpers exist on disk but must NOT be enumerated into names.
            let nested = app.join("Contents/MacOS/QQEXDOC.app/Contents/MacOS");
            fs::create_dir_all(&nested).unwrap();
            fs::write(nested.join("QQEXDOC"), b"x").unwrap();

            let got = read_bundle(&app).expect("bundle readable");
            let path = app.to_string_lossy().to_string();
            assert_eq!(got.id, path);
            assert_eq!(got.paths, vec![path], "the bundle path is what the engine matches");
            assert_eq!(got.process_names, vec!["QQ".to_string()], "main executable only");
            assert_eq!(got.label, "QQ");

            let _ = fs::remove_dir_all(&root);
        }

        // An app without CFBundleExecutable used to be dropped (no names to
        // match). Its path still identifies it.
        #[test]
        fn bundle_without_declared_executable_is_still_listed() {
            let root = scratch("noexe");
            let app = root.join("Odd.app");
            write_plist(&app, "<key>CFBundleIdentifier</key><string>io.example.odd</string>");

            let got = read_bundle(&app).expect("bundle readable");
            assert!(got.process_names.is_empty());
            assert_eq!(got.paths, vec![app.to_string_lossy().to_string()]);
            assert_eq!(got.label, "Odd", "label falls back to the bundle dir name");

            let _ = fs::remove_dir_all(&root);
        }

        #[test]
        fn apple_first_party_and_plistless_dirs_are_hidden() {
            let root = scratch("hidden");
            let apple = root.join("Notes.app");
            write_plist(&apple, "<key>CFBundleIdentifier</key><string>com.apple.Notes</string>");
            assert!(read_bundle(&apple).is_none());
            let bare = root.join("Bare.app");
            fs::create_dir_all(&bare).unwrap();
            assert!(read_bundle(&bare).is_none());
            let _ = fs::remove_dir_all(&root);
        }

        // Serialized shape the webapp reads: `paths` present on macOS.
        #[test]
        fn paths_serialize_as_camel_case_array() {
            let app = InstalledApp {
                id: "/Applications/A.app".into(),
                label: "A".into(),
                process_names: vec!["A".into()],
                paths: vec!["/Applications/A.app".into()],
                icon_url: None,
                installer_package_name: None,
            };
            let v = serde_json::to_value(&app).unwrap();
            assert_eq!(v["paths"], serde_json::json!(["/Applications/A.app"]));
            assert_eq!(v["processNames"], serde_json::json!(["A"]));
        }

        // The whole point of dropping the tree walk: listing every installed
        // app must cost one Info.plist read each, not a pass over every file.
        #[test]
        fn real_enumerate_is_fast_and_every_app_has_its_path() {
            let started = std::time::Instant::now();
            let apps = enumerate().expect("enumerate failed");
            let elapsed = started.elapsed();
            eprintln!("enumerate(): {} apps in {elapsed:?}", apps.len());
            for a in &apps {
                assert_eq!(a.paths, vec![a.id.clone()], "{}", a.label);
                assert!(a.id.ends_with(".app"), "{}", a.id);
            }
            assert!(
                elapsed < std::time::Duration::from_secs(5),
                "enumerate() took {elapsed:?} — it must not walk bundle contents"
            );
        }
    }
}

#[cfg(target_os = "windows")]
mod windows {
    use super::winreg_util::*;
    use super::*;
    use std::path::Path;
    use winreg::enums::*;
    use winreg::RegKey;

    const UNINSTALL: &str = r"Software\Microsoft\Windows\CurrentVersion\Uninstall";

    /// One Uninstall view. `view_flag` is 0 (hive default) or KEY_WOW64_64KEY /
    /// KEY_WOW64_32KEY. Scanning ONLY the process-default view was the original
    /// sin: 32-bit NSIS installers (WeChat 4.x — yes, the 64-bit app; Douyin,
    /// Edge, Steam…) get WOW64-redirected into `WOW6432Node\…\Uninstall`, which
    /// a 64-bit process never sees without KEY_WOW64_32KEY.
    fn scan_hive(
        root: RegKey,
        view_flag: u32,
        out: &mut Vec<InstalledApp>,
        seen: &mut std::collections::HashSet<String>,
    ) {
        let Ok(uninstall) = root.open_subkey_with_flags(UNINSTALL, KEY_READ | view_flag) else {
            return;
        };
        for sub in uninstall.enum_keys().flatten() {
            let Ok(k) = uninstall.open_subkey_with_flags(&sub, KEY_READ | view_flag) else {
                continue;
            };
            let name: String = match k.get_value("DisplayName") {
                Ok(n) => n,
                Err(_) => continue, // entries without a display name are components/patches
            };
            // Skip system components + updates.
            if let Ok(sys) = k.get_value::<u32, _>("SystemComponent") {
                if sys == 1 { continue; }
            }
            if k.get_value::<String, _>("ParentKeyName").is_ok() { continue; }

            // Install dir: InstallLocation is OPTIONAL (NSIS default omits it —
            // WeChat 4.x has none), so fall back to the DisplayIcon exe's
            // directory, then the UninstallString exe's directory. Reject
            // system directories (an MsiExec-style value would otherwise make
            // us enumerate C:\Windows).
            let install_dir: Option<String> = [
                k.get_value::<String, _>("InstallLocation").ok(),
                k.get_value::<String, _>("DisplayIcon").ok().and_then(|v| dir_from_display_icon(&v)),
                k.get_value::<String, _>("UninstallString").ok().and_then(|v| dir_from_uninstall_string(&v)),
            ]
            .into_iter()
            .flatten()
            .map(|d| d.trim().trim_end_matches(['\\', '/']).to_string())
            .find(|d| !d.is_empty() && !is_unsafe_install_dir(d));

            let mut process_names: Vec<String> = Vec::new();
            if let Some(ref dir) = install_dir {
                collect_exes(Path::new(dir), &mut process_names, 0);
            }
            // Known families whose helpers live outside the install tree
            // (WeChat's WMPF runtime in AppData). Works even when no install
            // dir could be derived at all.
            for extra in supplemental_process_names(&sub) {
                if !process_names.iter().any(|n| n == extra) {
                    process_names.push((*extra).to_string());
                }
            }
            if process_names.is_empty() {
                continue; // nothing to match a process against
            }

            let id = install_dir.clone().unwrap_or_else(|| sub.clone());
            // Windows paths are case-insensitive, and the same entry can
            // legitimately show up in more than one view — dedup on the
            // normalized id.
            if !seen.insert(id.to_ascii_lowercase()) { continue; }
            // Icon: exe-path scheme keyed by install dir (v1 handler is a 404
            // stub on Windows; keep the URL shape for when it lands).
            let icon_url = install_dir
                .as_ref()
                .map(|d| format!("kaitu-icon://exe/{}", urlencoding::encode(d)));
            out.push(InstalledApp {
                id,
                label: name,
                process_names,
                paths: Vec::new(),
                icon_url,
                installer_package_name: None,
            });
        }
    }

    pub fn enumerate() -> Result<Vec<InstalledApp>, String> {
        let mut out: Vec<InstalledApp> = Vec::new();
        let mut seen: std::collections::HashSet<String> = std::collections::HashSet::new();
        // HKLM needs BOTH WOW64 views; HKCU has a single view (flag 0) —
        // per-user installs (admin-less Chrome et al.) live there.
        scan_hive(RegKey::predef(HKEY_LOCAL_MACHINE), KEY_WOW64_64KEY, &mut out, &mut seen);
        scan_hive(RegKey::predef(HKEY_LOCAL_MACHINE), KEY_WOW64_32KEY, &mut out, &mut seen);
        scan_hive(RegKey::predef(HKEY_CURRENT_USER), 0, &mut out, &mut seen);
        out.sort_by(|a, b| a.label.to_lowercase().cmp(&b.label.to_lowercase()));
        Ok(out)
    }

    // Registry-backed tests for the F2/F3 fixes. They build synthetic
    // Uninstall entries in a REAL registry and read them back through the
    // production code, because that is the only place those bugs are visible:
    // winreg_util's string helpers are covered portably elsewhere, and no
    // amount of string testing can show that a 64-bit process opened the
    // WOW6432Node view. Delete `KEY_WOW64_32KEY` from enumerate() and every
    // other test in this crate still passes — wow64_32bit_view_is_scanned is
    // the only thing that goes red.
    #[cfg(test)]
    mod tests {
        use super::*;
        use std::fs;
        use std::io;

        /// An Uninstall entry in one registry view, plus a temp install dir.
        /// Removed on drop so a failing assertion cannot leave the machine or
        /// a developer's registry dirty.
        struct Fixture {
            hive: winreg::HKEY,
            view: u32,
            key: String,
            dir: std::path::PathBuf,
        }

        impl Fixture {
            /// Creates the install dir (one real exe + one uninstaller that
            /// collect_exes must filter out) and an empty Uninstall key.
            /// Err means the view is not writable — HKLM without elevation.
            fn new(hive: winreg::HKEY, view: u32, key: &str) -> io::Result<Self> {
                let dir = std::env::temp_dir().join(key);
                let _ = fs::remove_dir_all(&dir);
                fs::create_dir_all(&dir)?;
                fs::write(dir.join("k2gateapp.exe"), b"MZ")?;
                fs::write(dir.join("unins000.exe"), b"MZ")?;

                let root = RegKey::predef(hive);
                let (uninstall, _) =
                    root.create_subkey_with_flags(UNINSTALL, KEY_READ | KEY_WRITE | view)?;
                let (k, _) = uninstall.create_subkey_with_flags(key, KEY_READ | KEY_WRITE | view)?;
                k.set_value("DisplayName", &key.to_string())?;

                Ok(Self { hive, view, key: key.to_string(), dir })
            }

            fn key(&self) -> RegKey {
                RegKey::predef(self.hive)
                    .open_subkey_with_flags(UNINSTALL, KEY_READ | KEY_WRITE | self.view)
                    .and_then(|u| u.open_subkey_with_flags(&self.key, KEY_READ | KEY_WRITE | self.view))
                    .expect("fixture key vanished")
            }

            fn set_str(&self, name: &str, value: &str) {
                self.key().set_value(name, &value.to_string()).expect("set_value");
            }

            fn set_u32(&self, name: &str, value: u32) {
                self.key().set_value(name, &value).expect("set_value u32");
            }

            fn dir_str(&self) -> String {
                self.dir.to_string_lossy().to_string()
            }
        }

        impl Drop for Fixture {
            fn drop(&mut self) {
                if let Ok(u) = RegKey::predef(self.hive)
                    .open_subkey_with_flags(UNINSTALL, KEY_READ | KEY_WRITE | self.view)
                {
                    let _ = u.delete_subkey_all(&self.key);
                }
                let _ = fs::remove_dir_all(&self.dir);
            }
        }

        /// Scan one view only — the whole-machine enumerate() walks every
        /// installed program's tree, which is far too slow to run per-test.
        fn scan_one(hive: winreg::HKEY, view: u32) -> Vec<InstalledApp> {
            let mut out = Vec::new();
            let mut seen = std::collections::HashSet::new();
            scan_hive(RegKey::predef(hive), view, &mut out, &mut seen);
            out
        }

        fn find<'a>(apps: &'a [InstalledApp], label: &str) -> Option<&'a InstalledApp> {
            apps.iter().find(|a| a.label == label)
        }

        #[test]
        fn hkcu_entry_is_enumerated_with_its_exes() {
            let f = Fixture::new(HKEY_CURRENT_USER, 0, "K2GateTest_HkcuBasic").unwrap();
            f.set_str("InstallLocation", &f.dir_str());

            let apps = scan_one(HKEY_CURRENT_USER, 0);
            let app = find(&apps, "K2GateTest_HkcuBasic")
                .expect("synthetic HKCU Uninstall entry was not enumerated");
            assert!(
                app.process_names.iter().any(|n| n == "k2gateapp.exe"),
                "install dir exe not collected: {:?}",
                app.process_names
            );
            assert!(
                !app.process_names.iter().any(|n| n.starts_with("unins")),
                "uninstaller leaked into process_names: {:?}",
                app.process_names
            );
        }

        // F3: InstallLocation is optional (NSIS omits it). Both fallbacks have
        // to produce a usable directory or the entry is dropped entirely,
        // which is what made the list "近乎为空" on real machines.
        #[test]
        fn display_icon_supplies_the_missing_install_dir() {
            let f = Fixture::new(HKEY_CURRENT_USER, 0, "K2GateTest_IconFallback").unwrap();
            f.set_str("DisplayIcon", &format!("{}\\k2gateapp.exe,0", f.dir_str()));

            let apps = scan_one(HKEY_CURRENT_USER, 0);
            let app = find(&apps, "K2GateTest_IconFallback")
                .expect("entry with only DisplayIcon was dropped (F3 regression)");
            assert!(app.process_names.iter().any(|n| n == "k2gateapp.exe"));
        }

        #[test]
        fn uninstall_string_supplies_the_missing_install_dir() {
            let f = Fixture::new(HKEY_CURRENT_USER, 0, "K2GateTest_UninstFallback").unwrap();
            f.set_str("UninstallString", &format!("\"{}\\unins000.exe\" /S", f.dir_str()));

            let apps = scan_one(HKEY_CURRENT_USER, 0);
            let app = find(&apps, "K2GateTest_UninstFallback")
                .expect("entry with only UninstallString was dropped (F3 regression)");
            assert!(app.process_names.iter().any(|n| n == "k2gateapp.exe"));
        }

        #[test]
        fn system_components_stay_out_of_the_user_facing_list() {
            let f = Fixture::new(HKEY_CURRENT_USER, 0, "K2GateTest_SysComponent").unwrap();
            f.set_str("InstallLocation", &f.dir_str());
            f.set_u32("SystemComponent", 1);

            let apps = scan_one(HKEY_CURRENT_USER, 0);
            assert!(
                find(&apps, "K2GateTest_SysComponent").is_none(),
                "SystemComponent=1 entry surfaced in the app list"
            );
        }

        // F2, the root cause: 32-bit NSIS installers (WeChat 4.x, Douyin,
        // Edge, Steam) land in WOW6432Node, invisible to a 64-bit process
        // that does not ask for KEY_WOW64_32KEY. The fixture is written
        // through that same view, so it exists ONLY there — the 64-bit and
        // HKCU scans cannot see it. This goes through the real enumerate()
        // rather than scan_one, because the bug was in which views
        // enumerate() asks for, not in scan_hive itself.
        #[test]
        fn wow64_32bit_view_is_scanned() {
            let f = match Fixture::new(HKEY_LOCAL_MACHINE, KEY_WOW64_32KEY, "K2GateTest_Wow6432") {
                Ok(f) => f,
                Err(e) => {
                    assert!(
                        std::env::var_os("CI").is_none(),
                        "cannot write HKLM ({e}) — the CI runner is no longer elevated. \
                         That also invalidates the Windows service install gate; fix the \
                         runner rather than relaxing this test."
                    );
                    eprintln!(
                        "SKIP wow64_32bit_view_is_scanned: HKLM is not writable ({e}). \
                         Run an elevated shell to exercise the F2 regression guard."
                    );
                    return;
                }
            };
            f.set_str("InstallLocation", &f.dir_str());

            // Sanity: the fixture must be invisible to the other two scans,
            // otherwise passing this test would prove nothing about the view.
            // Both are asserted — enumerate() reads three views, and a pass
            // that came from either of the other two would be a false green.
            assert!(
                find(&scan_one(HKEY_LOCAL_MACHINE, KEY_WOW64_64KEY), "K2GateTest_Wow6432").is_none(),
                "fixture leaked into the 64-bit view — it is not exercising WOW6432Node"
            );
            assert!(
                find(&scan_one(HKEY_CURRENT_USER, 0), "K2GateTest_Wow6432").is_none(),
                "fixture leaked into HKCU — it is not exercising WOW6432Node"
            );

            let started = std::time::Instant::now();
            let apps = enumerate().expect("enumerate failed");
            let elapsed = started.elapsed();
            eprintln!("enumerate() over the real registry took {elapsed:?} ({} apps)", apps.len());

            assert!(
                find(&apps, "K2GateTest_Wow6432").is_some(),
                "a 32-bit-view Uninstall entry was not enumerated — enumerate() is not \
                 scanning HKLM with KEY_WOW64_32KEY, which is exactly the F2 bug that made \
                 WeChat/Douyin/Edge/Steam invisible on real machines"
            );
            // Loose ceiling: this walks every installed program's tree and
            // blocks the app-bypass page. Only catches pathological growth.
            assert!(
                elapsed < std::time::Duration::from_secs(120),
                "enumerate() took {elapsed:?} — the app list would hang the UI"
            );
        }
    }
}

#[tauri::command]
pub async fn list_installed_apps() -> Result<Vec<InstalledApp>, String> {
    #[cfg(target_os = "macos")]
    {
        return tokio::task::spawn_blocking(macos::enumerate)
            .await
            .map_err(|e| format!("list_installed_apps join error: {e}"))?;
    }
    #[cfg(target_os = "windows")]
    {
        return tokio::task::spawn_blocking(windows::enumerate)
            .await
            .map_err(|e| format!("list_installed_apps join error: {e}"))?;
    }
    #[cfg(not(any(target_os = "macos", target_os = "windows")))]
    {
        Ok(Vec::new())
    }
}
