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

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn label_strips_dot_app() {
        assert_eq!(label_from_bundle_dir("WeChat.app"), "WeChat");
        assert_eq!(label_from_bundle_dir("No Suffix"), "No Suffix");
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

    /// Walk Contents/Frameworks + Contents/Helpers + Contents/Library for
    /// nested *.app executables, pushing unique basenames.
    fn collect_helper_executables(app_path: &Path, out: &mut Vec<String>) {
        let subdirs = ["Contents/Frameworks", "Contents/Helpers", "Contents/Library"];
        for sub in subdirs {
            let dir = app_path.join(sub);
            collect_app_execs(&dir, out, 0);
        }
    }

    fn collect_app_execs(dir: &Path, out: &mut Vec<String>, depth: usize) {
        if depth > 3 {
            return;
        }
        let Ok(entries) = std::fs::read_dir(dir) else {
            return;
        };
        for e in entries.flatten() {
            let p = e.path();
            if p.is_dir() {
                if p.extension().and_then(|s| s.to_str()) == Some("app") {
                    if let Ok(v) = plist::Value::from_file(p.join("Contents/Info.plist")) {
                        if let Some(exe) = v
                            .as_dictionary()
                            .and_then(|d| d.get("CFBundleExecutable"))
                            .and_then(|v| v.as_string())
                        {
                            let name = exe.to_string();
                            if !out.contains(&name) {
                                out.push(name);
                            }
                        }
                    }
                } else {
                    collect_app_execs(&p, out, depth + 1);
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
}

#[cfg(target_os = "windows")]
mod windows {
    use super::*;
    use std::path::Path;
    use winreg::enums::*;
    use winreg::RegKey;

    const UNINSTALL: &str = r"Software\Microsoft\Windows\CurrentVersion\Uninstall";

    fn scan_hive(root: RegKey, out: &mut Vec<InstalledApp>, seen: &mut std::collections::HashSet<String>) {
        let Ok(uninstall) = root.open_subkey(UNINSTALL) else {
            return;
        };
        for sub in uninstall.enum_keys().flatten() {
            let Ok(k) = uninstall.open_subkey(&sub) else { continue };
            let name: String = match k.get_value("DisplayName") {
                Ok(n) => n,
                Err(_) => continue, // entries without a display name are components/patches
            };
            // Skip system components + updates.
            if let Ok(sys) = k.get_value::<u32, _>("SystemComponent") {
                if sys == 1 { continue; }
            }
            if k.get_value::<String, _>("ParentKeyName").is_ok() { continue; }
            let install_location: String = k.get_value("InstallLocation").unwrap_or_default();
            let mut process_names: Vec<String> = Vec::new();
            if !install_location.is_empty() {
                collect_exes(Path::new(&install_location), &mut process_names, 0);
            }
            if process_names.is_empty() {
                continue; // nothing to match a process against
            }
            let id = if !install_location.is_empty() { install_location.clone() } else { sub.clone() };
            if !seen.insert(id.clone()) { continue; }
            // Icon: reuse the exe path scheme; first exe under install dir.
            let icon_url = process_names.first().map(|_| {
                format!("kaitu-icon://exe/{}", urlencoding::encode(&id))
            });
            out.push(InstalledApp {
                id,
                label: name,
                process_names,
                icon_url,
                installer_package_name: None,
            });
        }
    }

    fn collect_exes(dir: &Path, out: &mut Vec<String>, depth: usize) {
        if depth > 2 { return; }
        let Ok(entries) = std::fs::read_dir(dir) else { return };
        for e in entries.flatten() {
            let p = e.path();
            if p.is_dir() {
                collect_exes(&p, out, depth + 1);
            } else if p.extension().and_then(|s| s.to_str()).map(|s| s.eq_ignore_ascii_case("exe")).unwrap_or(false) {
                if let Some(base) = p.file_name().and_then(|s| s.to_str()) {
                    let b = base.to_string();
                    if !out.contains(&b) { out.push(b); }
                }
            }
        }
    }

    pub fn enumerate() -> Result<Vec<InstalledApp>, String> {
        let mut out: Vec<InstalledApp> = Vec::new();
        let mut seen: std::collections::HashSet<String> = std::collections::HashSet::new();
        scan_hive(RegKey::predef(HKEY_LOCAL_MACHINE), &mut out, &mut seen);
        scan_hive(RegKey::predef(HKEY_CURRENT_USER), &mut out, &mut seen);
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
