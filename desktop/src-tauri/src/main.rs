#![cfg_attr(not(debug_assertions), windows_subsystem = "windows")]

mod service;
mod tray;
mod updater;

use tauri::Manager;

fn main() {
    tauri::Builder::default()
        .plugin(tauri_plugin_localhost::Builder::new(14580).build())
        .plugin(tauri_plugin_single_instance::init(|app, _args, _cwd| {
            if let Some(window) = app.get_webview_window("main") {
                let _ = window.show();
                let _ = window.set_focus();
            }
        }))
        .plugin(tauri_plugin_process::init())
        .invoke_handler(tauri::generate_handler![
            service::admin_reinstall_service,
            service::ensure_service_running,
        ])
        .setup(|app| {
            if let Some(window) = app.get_webview_window("main") {
                window.show().ok();
            }

            let app_version = env!("CARGO_PKG_VERSION").to_string();
            tauri::async_runtime::spawn(async move {
                if let Err(e) = service::ensure_service_running(app_version).await {
                    log::error!("[startup] Service error: {}", e);
                }
            });

            Ok(())
        })
        .run(tauri::generate_context!())
        .expect("error while running tauri application");
}
