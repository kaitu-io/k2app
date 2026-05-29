// desktop/src-tauri/src/app_list.rs
//
// Tauri command `list_running_processes` — enumerates currently running user-facing
// applications for the App Bypass feature. On macOS it walks NSWorkspace's
// runningApplications and groups child PIDs (helper processes) under their
// owning bundle. On Windows it walks sysinfo's process list. Linux + other
// targets return an empty list (the standalone Linux daemon implements its own
// listing path — see Task 4.3).
//
// The Rust output uses `#[serde(rename_all = "camelCase")]` so the JS bridge
// observes `processNames` / `iconUrl` (spec H1).

use serde::Serialize;

#[derive(Serialize, Debug, Clone)]
#[serde(rename_all = "camelCase")]
pub struct RunningApp {
    pub id: String,
    pub label: String,
    pub process_names: Vec<String>,
    pub icon_url: Option<String>,
}

/// Pure helper: given a bundle URL and (pid, exe_path) list, return basenames
/// of paths inside the bundle, deduplicated and preserving first-seen order.
#[cfg_attr(not(target_os = "macos"), allow(dead_code))]
fn collect_helper_basenames(bundle_url: &str, pid_paths: &[(i32, &str)]) -> Vec<String> {
    let mut seen = std::collections::HashSet::new();
    let mut out = Vec::new();
    for (_, path) in pid_paths {
        if !path.starts_with(bundle_url) {
            continue;
        }
        let basename = std::path::Path::new(path)
            .file_name()
            .and_then(|s| s.to_str())
            .unwrap_or("");
        if basename.is_empty() {
            continue;
        }
        if seen.insert(basename.to_string()) {
            out.push(basename.to_string());
        }
    }
    out
}

/// True if `path` lives under a macOS OS system directory (Apple daemons/bins).
/// Trailing slashes guard against false prefixes (e.g. "/usr/bindle").
#[cfg_attr(not(target_os = "macos"), allow(dead_code))]
fn is_macos_system_path(path: &str) -> bool {
    const SYS_DIRS: &[&str] = &[
        "/System/", "/usr/libexec/", "/usr/sbin/", "/sbin/", "/usr/bin/", "/bin/",
    ];
    SYS_DIRS.iter().any(|p| path.starts_with(p))
}

/// True if `path` is inside a `.app` bundle (some ancestor path segment ends in
/// ".app"). Such processes are GUI apps already covered by the NSWorkspace pass.
#[cfg_attr(not(target_os = "macos"), allow(dead_code))]
fn is_inside_app_bundle(path: &str) -> bool {
    path.split('/').any(|seg| seg.ends_with(".app"))
}

/// True if `path` is under `C:\Windows` (case-insensitive, slash-normalized).
#[cfg_attr(not(target_os = "windows"), allow(dead_code))]
fn is_windows_system_path(path: &str) -> bool {
    let p = path.to_ascii_lowercase().replace('/', "\\");
    p.starts_with("c:\\windows\\")
}

#[cfg(target_os = "macos")]
mod macos {
    use super::*;
    use libproc::libproc::proc_pid;
    use libproc::processes::{pids_by_type, ProcFilter};
    use objc2_app_kit::NSWorkspace;

    /// Snapshot all currently-running PIDs together with their executable paths.
    fn snapshot_pid_paths() -> Vec<(i32, String)> {
        let pids = match pids_by_type(ProcFilter::All) {
            Ok(p) => p,
            Err(_) => return Vec::new(),
        };
        pids.into_iter()
            .filter_map(|pid| {
                let pid_i32 = pid as i32;
                if pid_i32 <= 0 {
                    return None;
                }
                proc_pid::pidpath(pid_i32).ok().map(|p| (pid_i32, p))
            })
            .collect()
    }

    pub fn enumerate() -> Result<Vec<RunningApp>, String> {
        let pid_paths_owned = snapshot_pid_paths();
        let pid_paths_refs: Vec<(i32, &str)> = pid_paths_owned
            .iter()
            .map(|(pid, p)| (*pid, p.as_str()))
            .collect();

        // SAFETY: NSWorkspace::sharedWorkspace() and the AppKit APIs we touch
        // here are documented as main-thread safe for read-only metadata access
        // (bundleIdentifier / localizedName / bundleURL / icon path). We do not
        // mutate any AppKit state. Tauri commands run on the async runtime, but
        // reading these properties from a worker thread is well-supported in
        // practice (NSWorkspace's running-applications list is a snapshot).
        let workspace = unsafe { NSWorkspace::sharedWorkspace() };
        let running = unsafe { workspace.runningApplications() };
        let count = running.count();

        // Collect raw candidates first so we can post-filter sub-bundles (e.g.
        // /Applications/Slack.app/.../Slack Helper (Plugin).app whose parent
        // /Applications/Slack.app is also in the list).
        let mut raw: Vec<(String /*bundle_path*/, RunningApp)> = Vec::new();
        let mut seen_ids: std::collections::HashSet<String> = std::collections::HashSet::new();

        for i in 0..count {
            let app = unsafe { running.objectAtIndex(i) };

            // bundleURL → Option<Retained<NSURL>>
            let bundle_url_obj = unsafe { app.bundleURL() };
            let Some(bundle_url_ns) = bundle_url_obj else {
                continue;
            };
            // NSURL.path() → Option<Retained<NSString>>
            let path_obj = unsafe { bundle_url_ns.path() };
            let Some(path_ns) = path_obj else {
                continue;
            };
            let bundle_path = path_ns.to_string();

            // Only show real .app bundles (skip daemons / Dock helpers without bundle)
            if !bundle_path.ends_with(".app") {
                continue;
            }

            // bundleIdentifier (Option)
            let bundle_id = unsafe { app.bundleIdentifier() }
                .map(|s| s.to_string())
                .unwrap_or_else(|| bundle_path.clone());

            // Hide all Apple-owned bundles (system daemons + first-party apps like
            // Safari/Mail/Messages). Rationale: 99% of bypass use cases target third-
            // party apps (WeChat, banking, 12306). The rare case of bypassing a built-in
            // Apple app stays reachable via the "+ 手动添加" button.
            if bundle_id.starts_with("com.apple") {
                continue;
            }

            // NSWorkspace returns one NSRunningApplication per process; helper bundles
            // like com.google.Chrome.helper appear once per renderer PID. Dedup by
            // bundleId — all instances share the same bundle_url, so process_names
            // collected below are identical anyway.
            if !seen_ids.insert(bundle_id.clone()) {
                continue;
            }

            // localizedName (Option)
            let label = unsafe { app.localizedName() }
                .map(|s| s.to_string())
                .unwrap_or_else(|| {
                    // Fallback: derive from bundle path ".app" basename
                    std::path::Path::new(&bundle_path)
                        .file_stem()
                        .and_then(|s| s.to_str())
                        .unwrap_or("Unknown")
                        .to_string()
                });

            let helpers = collect_helper_basenames(&bundle_path, &pid_paths_refs);
            if helpers.is_empty() {
                // App is in NSWorkspace's list but no child PID matched its
                // bundle path (rare — sandbox or path-mismatched). Skip; the
                // bypass route needs at least one process name to match.
                continue;
            }

            let icon_url = format!(
                "kaitu-icon://bundle/{}",
                urlencoding::encode(&bundle_id)
            );

            raw.push((
                bundle_path,
                RunningApp {
                    id: bundle_id,
                    label,
                    process_names: helpers,
                    icon_url: Some(icon_url),
                },
            ));
        }

        // Drop nested sub-bundles: any candidate whose bundle_path lives strictly
        // inside another candidate's bundle_path is redundant — the parent
        // already covers all child PIDs via collect_helper_basenames.
        let all_paths: Vec<String> = raw.iter().map(|(p, _)| p.clone()).collect();
        let out: Vec<RunningApp> = raw
            .into_iter()
            .filter(|(this_path, _)| {
                !all_paths.iter().any(|other| {
                    other != this_path && this_path.starts_with(&format!("{other}/"))
                })
            })
            .map(|(_, app)| app)
            .collect();

        Ok(out)
    }
}

#[cfg(target_os = "windows")]
mod windows {
    use super::*;
    use sysinfo::System;

    pub fn enumerate() -> Result<Vec<RunningApp>, String> {
        let mut sys = System::new_all();
        sys.refresh_processes();
        let mut seen_exe: std::collections::HashMap<String, RunningApp> = Default::default();
        for proc in sys.processes().values() {
            let Some(exe_path) = proc.exe().and_then(|p| p.to_str()) else {
                continue;
            };
            if exe_path.is_empty() {
                continue;
            }
            if seen_exe.contains_key(exe_path) {
                continue;
            }
            let basename = std::path::Path::new(exe_path)
                .file_name()
                .and_then(|s| s.to_str())
                .unwrap_or("")
                .to_string();
            if basename.is_empty() {
                continue;
            }
            // label: use stem for cleaner UI; process_name still uses full basename
            let label = std::path::Path::new(exe_path)
                .file_stem()
                .and_then(|s| s.to_str())
                .unwrap_or(&basename)
                .to_string();
            let icon_url = format!("kaitu-icon://exe/{}", urlencoding::encode(exe_path));
            seen_exe.insert(
                exe_path.to_string(),
                RunningApp {
                    id: exe_path.to_string(),
                    label,
                    process_names: vec![basename],
                    icon_url: Some(icon_url),
                },
            );
        }
        Ok(seen_exe.into_values().collect())
    }
}

#[tauri::command]
pub async fn list_running_processes() -> Result<Vec<RunningApp>, String> {
    #[cfg(target_os = "macos")]
    {
        return tokio::task::spawn_blocking(macos::enumerate)
            .await
            .map_err(|e| format!("list_running_processes join error: {e}"))?;
    }
    #[cfg(target_os = "windows")]
    {
        return tokio::task::spawn_blocking(windows::enumerate)
            .await
            .map_err(|e| format!("list_running_processes join error: {e}"))?;
    }
    #[cfg(not(any(target_os = "macos", target_os = "windows")))]
    {
        Ok(Vec::new())
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn helpers_under_bundle_collected() {
        let bundle_url = "/Applications/TestApp.app";
        let pid_paths = vec![
            (100, "/Applications/TestApp.app/Contents/MacOS/TestApp"),
            (
                101,
                "/Applications/TestApp.app/Contents/Frameworks/TestApp Helper.app/Contents/MacOS/TestApp Helper",
            ),
            (102, "/Applications/Other.app/Contents/MacOS/Other"),
        ];
        let helpers = collect_helper_basenames(bundle_url, &pid_paths);
        assert_eq!(
            helpers,
            vec!["TestApp".to_string(), "TestApp Helper".to_string()]
        );
    }

    #[test]
    fn dedupes_helper_names() {
        let bundle_url = "/Applications/Foo.app";
        let pid_paths = vec![
            (100, "/Applications/Foo.app/Contents/MacOS/Foo"),
            (101, "/Applications/Foo.app/Contents/MacOS/Foo"),
        ];
        let helpers = collect_helper_basenames(bundle_url, &pid_paths);
        assert_eq!(helpers, vec!["Foo".to_string()]);
    }

    #[test]
    fn macos_system_paths_excluded() {
        assert!(is_macos_system_path("/System/Library/CoreServices/foo"));
        assert!(is_macos_system_path("/usr/libexec/configd"));
        assert!(is_macos_system_path("/usr/sbin/systemstats"));
        assert!(is_macos_system_path("/sbin/launchd"));
        assert!(is_macos_system_path("/usr/bin/ssh"));
        assert!(is_macos_system_path("/bin/zsh"));
        // User-relevant locations are NOT system paths:
        assert!(!is_macos_system_path("/opt/homebrew/bin/node"));
        assert!(!is_macos_system_path("/usr/local/bin/yt-dlp"));
        assert!(!is_macos_system_path("/Users/david/go/bin/mytool"));
        assert!(!is_macos_system_path("/Applications/QQ.app/Contents/MacOS/QQ"));
    }

    #[test]
    fn detects_inside_app_bundle() {
        assert!(is_inside_app_bundle("/Applications/QQ.app/Contents/MacOS/QQ"));
        assert!(is_inside_app_bundle(
            "/Applications/Slack.app/Contents/Frameworks/Slack Helper.app/Contents/MacOS/Slack Helper"
        ));
        // Standalone binaries are NOT inside a bundle:
        assert!(!is_inside_app_bundle("/opt/homebrew/bin/node"));
        assert!(!is_inside_app_bundle("/usr/local/bin/python3"));
    }

    #[test]
    fn windows_system_paths_excluded() {
        assert!(is_windows_system_path("C:\\Windows\\System32\\svchost.exe"));
        assert!(is_windows_system_path("c:\\windows\\explorer.exe")); // case-insensitive
        assert!(is_windows_system_path("C:/Windows/System32/services.exe")); // forward slashes
        // User-installed / portable exes are NOT system paths:
        assert!(!is_windows_system_path("C:\\Program Files\\nodejs\\node.exe"));
        assert!(!is_windows_system_path("C:\\Users\\david\\tools\\portable.exe"));
    }
}
