#![cfg_attr(not(debug_assertions), windows_subsystem = "windows")]

use tauri::Manager;

mod service;
mod tray;
mod updater;

fn main() {
    tauri::Builder::default()
        .plugin(tauri_plugin_localhost::Builder::new(14580).build())
        .plugin(tauri_plugin_single_instance::init(|_app, _args, _cwd| {}))
        .plugin(tauri_plugin_process::init())
        .setup(|app| {
            // Show window after ready
            if let Some(window) = app.get_webview_window("main") {
                window.show().ok();
            }

            // Initialize system tray
            let app_handle = app.handle().clone();
            tauri::async_runtime::spawn(async move {
                if let Err(e) = tray::init_tray(&app_handle) {
                    log::error!("Failed to init tray: {}", e);
                }
            });

            Ok(())
        })
        .run(tauri::generate_context!())
        .expect("error while running tauri application");
}
