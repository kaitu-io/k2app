#![cfg_attr(not(debug_assertions), windows_subsystem = "windows")]

mod service;
mod tray;
mod updater;

use tauri::Manager;

fn main() {
    let mut builder = tauri::Builder::default()
        .plugin(tauri_plugin_localhost::Builder::new(14580).build())
        .plugin(tauri_plugin_single_instance::init(|app, _args, _cwd| {
            if let Some(window) = app.get_webview_window("main") {
                let _ = window.show();
                let _ = window.set_focus();
            }
        }))
        .plugin(tauri_plugin_process::init())
        .plugin(tauri_plugin_http::init())
        .plugin(tauri_plugin_shell::init())
        .plugin(tauri_plugin_updater::Builder::new().build())
        .invoke_handler(tauri::generate_handler![
            service::admin_reinstall_service,
            service::ensure_service_running,
            service::daemon_exec,
            service::get_udid,
            service::get_platform_info,
            updater::check_update_now,
            updater::apply_update_now,
            updater::get_update_status,
        ]);

    #[cfg(all(feature = "mcp-bridge", debug_assertions))]
    {
        builder = builder.plugin(tauri_plugin_mcp_bridge::init());
    }

    builder
        .setup(|app| {
            if let Some(window) = app.get_webview_window("main") {
                window.show().ok();
            }

            // Initialize system tray
            let tray_handle = app.handle().clone();
            tauri::async_runtime::spawn(async move {
                if let Err(e) = tray::init_tray(&tray_handle) {
                    log::error!("Failed to init tray: {}", e);
                }
            });

            // Ensure k2 service is running with correct version
            let app_version = env!("CARGO_PKG_VERSION").to_string();
            tauri::async_runtime::spawn(async move {
                if let Err(e) = service::ensure_service_running(app_version).await {
                    log::error!("[startup] Service error: {}", e);
                }
            });

            // Start auto-updater
            updater::start_auto_updater(app.handle().clone());

            Ok(())
        })
        .run(tauri::generate_context!())
        .expect("error while running tauri application");
}
