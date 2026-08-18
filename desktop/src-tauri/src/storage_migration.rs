// One-time desktop origin migration (spec §5.2). The kaitu-ui:// protocol is a
// new browsing origin, so localStorage written under the legacy tauri://
// origin (http://tauri.localhost on Windows) does not carry over. Flow:
//   1. unmigrated startup → shell boots the *embedded* UI at the legacy origin
//      with ?migrate=export
//   2. webapp dumps localStorage → storage_migration_put → storage_migration_done
//   3. done writes the storage-migrated marker and navigates to kaitu-ui://
//   4. at the new origin the webapp imports via storage_migration_get + clear
// Failure fallback: the watchdog forces the marker after 20s so a broken
// export can never trap the user at the legacy origin (fresh start instead).
// NOTE: desktop auth tokens live in Rust storage.json (storage.rs), which is
// origin-independent — login state survives even a failed migration. What this
// carries over: kaitu-language, k2_log_level, k2_developer_mode, kaitu_cache:*,
// announcement dismissals, drag positions.

use std::path::{Path, PathBuf};
use tauri::{AppHandle, Manager};

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn migrated_marker_roundtrip() {
        let tmp = tempfile::tempdir().unwrap();
        assert!(!is_migrated(tmp.path()));
        write_migrated(tmp.path());
        assert!(is_migrated(tmp.path()));
    }

    #[test]
    fn data_put_get_clear() {
        let tmp = tempfile::tempdir().unwrap();
        assert_eq!(get_data(tmp.path()), None);
        put_data(tmp.path(), r#"{"kaitu-language":"zh-CN"}"#).unwrap();
        assert_eq!(
            get_data(tmp.path()).as_deref(),
            Some(r#"{"kaitu-language":"zh-CN"}"#)
        );
        clear_data(tmp.path());
        assert_eq!(get_data(tmp.path()), None);
    }

    #[test]
    fn put_data_atomic_no_tmp_leftover() {
        let tmp = tempfile::tempdir().unwrap();
        put_data(tmp.path(), "{}").unwrap();
        let leftovers: Vec<_> = std::fs::read_dir(tmp.path())
            .unwrap()
            .map(|e| e.unwrap().file_name().to_string_lossy().into_owned())
            .filter(|n| n.ends_with(".tmp"))
            .collect();
        assert!(leftovers.is_empty(), "tmp leftovers: {leftovers:?}");
    }

    #[test]
    fn legacy_export_url_shape() {
        let url = legacy_export_url();
        #[cfg(target_os = "windows")]
        assert_eq!(url, "http://tauri.localhost/index.html?migrate=export");
        #[cfg(not(target_os = "windows"))]
        assert_eq!(url, "tauri://localhost/index.html?migrate=export");
    }
}

const MIGRATED_MARKER: &str = "storage-migrated";
const DATA_FILE: &str = "storage-migration.json";
const WATCHDOG_SECS: u64 = 20;

fn app_data_dir(app: &AppHandle) -> Option<PathBuf> {
    app.path().app_data_dir().ok()
}

pub fn is_migrated(dir: &Path) -> bool {
    dir.join(MIGRATED_MARKER).is_file()
}

pub fn write_migrated(dir: &Path) {
    let _ = std::fs::create_dir_all(dir);
    if let Err(e) = std::fs::write(dir.join(MIGRATED_MARKER), "1") {
        log::error!("[migration] failed to write migrated marker: {e}");
    }
}

pub fn put_data(dir: &Path, json: &str) -> Result<(), String> {
    std::fs::create_dir_all(dir).map_err(|e| e.to_string())?;
    let path = dir.join(DATA_FILE);
    let tmp = dir.join(format!("{DATA_FILE}.write.tmp"));
    std::fs::write(&tmp, json).map_err(|e| e.to_string())?;
    std::fs::rename(&tmp, &path).map_err(|e| e.to_string())
}

pub fn get_data(dir: &Path) -> Option<String> {
    std::fs::read_to_string(dir.join(DATA_FILE)).ok()
}

pub fn clear_data(dir: &Path) {
    let _ = std::fs::remove_file(dir.join(DATA_FILE));
}

/// Legacy-origin export URL — the default Tauri protocol this app shipped with.
pub fn legacy_export_url() -> &'static str {
    #[cfg(target_os = "windows")]
    {
        "http://tauri.localhost/index.html?migrate=export"
    }
    #[cfg(not(target_os = "windows"))]
    {
        "tauri://localhost/index.html?migrate=export"
    }
}

pub fn navigate_to_new_origin(app: &AppHandle) {
    let Some(win) = app.get_webview_window("main") else {
        return;
    };
    // F8: if an OTA'd disk UI is already sitting in current/, mark it
    // boot-pending before navigating to it — otherwise a poller apply during
    // the export window (or the watchdog's forced-fresh-start path) could
    // swap current/ out from under a first disk-UI boot that has no rollback
    // protection, since prepare_boot (the normal setter) never runs for this
    // navigation path.
    if let Some(dirs) = crate::web_ota::ota_dirs(app) {
        if crate::web_ota::serve_root(&dirs).is_some() {
            crate::web_ota::mark_boot_pending(&dirs);
        }
    }
    let url = crate::ui_protocol::ui_boot_url();
    match url.parse::<tauri::Url>() {
        Ok(u) => {
            if let Err(e) = win.navigate(u) {
                log::error!("[migration] navigate to {url} failed: {e}");
            }
        }
        Err(e) => log::error!("[migration] invalid url {url}: {e}"),
    }
}

/// Fallback: if the export page never signals done (JS error, hung bridge),
/// force-complete after WATCHDOG_SECS — user starts fresh at the new origin
/// (spec §5.2 step 4: functionality over data).
pub fn spawn_migration_watchdog(app: AppHandle) {
    tauri::async_runtime::spawn(async move {
        tokio::time::sleep(std::time::Duration::from_secs(WATCHDOG_SECS)).await;
        let Some(dir) = app_data_dir(&app) else {
            return;
        };
        if is_migrated(&dir) {
            return;
        }
        log::warn!(
            "[migration] export did not complete within {WATCHDOG_SECS}s — forcing fresh start at new origin"
        );
        write_migrated(&dir);
        navigate_to_new_origin(&app);
    });
}

#[tauri::command]
pub fn storage_migration_put(json: String, app: AppHandle) -> Result<(), String> {
    let dir = app_data_dir(&app).ok_or("cannot resolve app data dir")?;
    log::info!("[migration] received localStorage snapshot ({} bytes)", json.len());
    put_data(&dir, &json)
}

#[tauri::command]
pub fn storage_migration_get(app: AppHandle) -> Option<String> {
    app_data_dir(&app).and_then(|d| get_data(&d))
}

#[tauri::command]
pub fn storage_migration_clear(app: AppHandle) {
    if let Some(dir) = app_data_dir(&app) {
        clear_data(&dir);
        log::info!("[migration] snapshot cleared");
    }
}

#[tauri::command]
pub fn storage_migration_done(app: AppHandle) {
    if let Some(dir) = app_data_dir(&app) {
        write_migrated(&dir);
    }
    log::info!("[migration] export complete — navigating to kaitu-ui origin");
    navigate_to_new_origin(&app);
}
