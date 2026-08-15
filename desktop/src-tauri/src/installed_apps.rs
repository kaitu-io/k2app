// desktop/src-tauri/src/installed_apps.rs
//
// Tauri command `list_installed_apps` — enumerates ALL installed user-facing
// applications (not just running ones) for the redesigned App Bypass page.
// macOS: filesystem scan of standard .app dirs, reading each Info.plist.
// Windows: registry Uninstall hive scan (added in a later task).
// Other targets: empty list (Linux daemon serves its own path).
//
// camelCase serde so the JS bridge sees id / processNames / iconUrl /
// installerPackageName (matches webapp InstalledApp).

use serde::Serialize;

#[derive(Serialize, Debug, Clone, PartialEq)]
#[serde(rename_all = "camelCase")]
pub struct InstalledApp {
    pub id: String,
    pub label: String,
    pub process_names: Vec<String>,
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

        // process_names: the main executable basename + any helper .app
        // executables one level down. Case is PRESERVED (the engine's Darwin
        // process matcher is case-sensitive) — never lowercase these.
        let mut process_names: Vec<String> = Vec::new();
        if let Some(exe) = dict.get("CFBundleExecutable").and_then(|v| v.as_string()) {
            process_names.push(exe.to_string());
        }
        collect_helper_executables(app_path, &mut process_names);
        if process_names.is_empty() {
            return None;
        }

        // id = bundle path (stable, also the icon key); icon via kaitu-icon.
        let id = app_path.to_string_lossy().to_string();
        let icon_url = Some(format!(
            "kaitu-icon://bundle/{}",
            urlencoding::encode(&id)
        ));

        Some(InstalledApp {
            id,
            label,
            process_names,
            icon_url,
            installer_package_name: None,
        })
    }

    /// Collect the basename of every executable the bundle can spawn — i.e.
    /// every regular file that sits directly inside any `Contents/MacOS/`
    /// directory anywhere in the bundle tree. macOS attributes a connection to
    /// the basename of the running process's executable (`proc_pidpath`
    /// basename == lsof COMMAND), and every Mach-O an app launches lives in
    /// some `Contents/MacOS/`, so this is exactly the set of names attribution
    /// can report for this bundle.
    ///
    /// The bundle tree is deeply nested: large apps embed whole sub-apps under
    /// `Contents/MacOS/` (e.g. QQ ships `QQEXDOC.app` / `QQEXMiniProgram.app`
    /// there, each with its own `Contents/Frameworks/<Helper>.app/Contents/
    /// MacOS/…`). The earlier version only scanned `Contents/{Frameworks,
    /// Helpers,Library}` for nested `*.app` CFBundleExecutable values and
    /// silently missed everything under `Contents/MacOS/` sub-apps — their live
    /// connections (verified: QQ's `QQEXDOC`) then bypassed the user's per-app
    /// rule. Walking for `MacOS`-dir files catches them all, and matches the
    /// on-disk basename lsof actually reports (more correct than the declared
    /// CFBundleExecutable, which can differ from the file name).
    fn collect_helper_executables(app_path: &Path, out: &mut Vec<String>) {
        collect_macos_execs(app_path, false, out, 0);
    }

    /// Recurse `dir`, emitting each regular file's basename when `in_macos`
    /// (the immediate parent directory is named `MacOS`). Depth-bounded so a
    /// pathological framework/symlink tree can't run away.
    fn collect_macos_execs(dir: &Path, in_macos: bool, out: &mut Vec<String>, depth: usize) {
        if depth > 10 {
            return;
        }
        let Ok(entries) = std::fs::read_dir(dir) else {
            return;
        };
        for e in entries.flatten() {
            let p = e.path();
            if p.is_dir() {
                let is_macos = p.file_name().and_then(|s| s.to_str()) == Some("MacOS");
                collect_macos_execs(&p, is_macos, out, depth + 1);
            } else if in_macos {
                if let Some(base) = p.file_name().and_then(|s| s.to_str()) {
                    let b = base.to_string();
                    if !out.contains(&b) {
                        out.push(b);
                    }
                }
            }
        }
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

        // Regression: QQ embeds whole sub-apps under Contents/MacOS/ (QQEXDOC.app,
        // QQEXMiniProgram.app), each with its own Frameworks/<Helper>.app. The old
        // enumerator only scanned Contents/{Frameworks,Helpers,Library} and missed
        // every executable under Contents/MacOS/ sub-apps, so those processes'
        // connections (verified: QQEXDOC held a live socket) silently bypassed the
        // user's per-app rule. Walking for MacOS-dir files must catch them all.
        #[test]
        fn collects_execs_from_nested_macos_subapps() {
            let root = std::env::temp_dir().join("k2_installed_apps_macos_test");
            let _ = fs::remove_dir_all(&root);
            let app = root.join("QQ.app");

            let touch = |dir: std::path::PathBuf, name: &str| {
                fs::create_dir_all(&dir).unwrap();
                fs::write(dir.join(name), b"\x7fELF").unwrap();
            };
            // Main executable.
            touch(app.join("Contents/MacOS"), "QQ");
            // Framework helper (already handled by the old code).
            touch(app.join("Contents/Frameworks/QQ Helper.app/Contents/MacOS"), "QQ Helper");
            // Whole sub-app nested under Contents/MacOS (the missed case).
            touch(app.join("Contents/MacOS/QQEXDOC.app/Contents/MacOS"), "QQEXDOC");
            // ...and that sub-app's own helper.
            touch(
                app.join("Contents/MacOS/QQEXDOC.app/Contents/Frameworks/QQEXDOC Helper.app/Contents/MacOS"),
                "QQEXDOC Helper",
            );

            let mut out = Vec::new();
            collect_helper_executables(&app, &mut out);

            assert!(out.contains(&"QQ".to_string()), "main exe missing: {out:?}");
            assert!(out.contains(&"QQ Helper".to_string()), "framework helper missing: {out:?}");
            assert!(out.contains(&"QQEXDOC".to_string()), "MacOS-nested sub-app missing (regression): {out:?}");
            assert!(out.contains(&"QQEXDOC Helper".to_string()), "sub-app helper missing: {out:?}");

            let _ = fs::remove_dir_all(&root);
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
