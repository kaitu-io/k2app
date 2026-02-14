//! Auto-updater module
//!
//! Checks for updates on startup, notifies via Tauri event system.

use std::sync::atomic::{AtomicBool, Ordering};
use tauri::Emitter;
use tauri_plugin_updater::UpdaterExt;

static HAS_PENDING_UPDATE: AtomicBool = AtomicBool::new(false);

/// Check for updates using Tauri updater plugin
#[tauri::command]
pub async fn check_update_now(app: tauri::AppHandle) -> Result<Option<String>, String> {
    log::info!("[updater] Manual update check");

    let updater = app.updater_builder().build()
        .map_err(|e| format!("Failed to build updater: {}", e))?;

    match updater.check().await {
        Ok(Some(update)) => {
            let version = update.version.clone();
            log::info!("[updater] Update available: {}", version);
            HAS_PENDING_UPDATE.store(true, Ordering::SeqCst);
            Ok(Some(version))
        }
        Ok(None) => {
            log::info!("[updater] No update available");
            Ok(None)
        }
        Err(e) => {
            log::warn!("[updater] Check failed: {}", e);
            Err(format!("Update check failed: {}", e))
        }
    }
}

/// Apply pending update
#[tauri::command]
pub async fn apply_update_now(app: tauri::AppHandle) -> Result<(), String> {
    log::info!("[updater] Applying update");

    let updater = app.updater_builder().build()
        .map_err(|e| format!("Failed to build updater: {}", e))?;

    match updater.check().await {
        Ok(Some(update)) => {
            log::info!("[updater] Downloading and installing update v{}", update.version);
            update.download_and_install(|_, _| {}, || {}).await
                .map_err(|e| format!("Failed to install update: {}", e))?;
            log::info!("[updater] Update installed, restarting");
            app.restart();
        }
        Ok(None) => {
            Err("No update available".to_string())
        }
        Err(e) => {
            Err(format!("Update check failed: {}", e))
        }
    }
}

/// Get update status
#[tauri::command]
pub fn get_update_status() -> bool {
    HAS_PENDING_UPDATE.load(Ordering::SeqCst)
}

/// Check if there's a pending update
pub fn has_pending_update() -> bool {
    HAS_PENDING_UPDATE.load(Ordering::SeqCst)
}

/// Start auto-update checker on app startup
pub fn start_auto_updater(app: tauri::AppHandle) {
    tauri::async_runtime::spawn(async move {
        // Wait a bit before checking (let app finish startup)
        tokio::time::sleep(std::time::Duration::from_secs(5)).await;

        let updater = match app.updater_builder().build() {
            Ok(u) => u,
            Err(e) => {
                log::warn!("[updater] Failed to build updater: {}", e);
                return;
            }
        };

        match updater.check().await {
            Ok(Some(update)) => {
                log::info!("[updater] Update available: v{}", update.version);
                HAS_PENDING_UPDATE.store(true, Ordering::SeqCst);
                // Emit event to frontend
                let _ = app.emit("update-available", &update.version);
            }
            Ok(None) => {
                log::info!("[updater] App is up to date");
            }
            Err(e) => {
                log::warn!("[updater] Auto-check failed: {}", e);
            }
        }
    });
}

/// Install pending update (called on app exit)
pub fn install_pending_update(app: &tauri::AppHandle) {
    if !has_pending_update() { return; }
    log::info!("[updater] Installing pending update on exit");
    // The actual update will be applied on next launch via Tauri updater
    let _ = app.emit("installing-update", ());
}

/// Get pending update info (from, to versions)
pub fn get_pending_update_info() -> Option<(String, String)> {
    if has_pending_update() {
        Some((env!("CARGO_PKG_VERSION").to_string(), "pending".to_string()))
    } else {
        None
    }
}
