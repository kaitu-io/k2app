#![cfg_attr(not(debug_assertions), windows_subsystem = "windows")]

mod log_upload;
mod ne;
mod service;
mod status_stream;
mod tray;
mod updater;

use tauri::{Manager, RunEvent};

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
        .plugin(tauri_plugin_updater::Builder::new().build())
        .plugin(tauri_plugin_opener::init())
        .plugin(tauri_plugin_clipboard_manager::init())
        .plugin(tauri_plugin_autostart::init(
            tauri_plugin_autostart::MacosLauncher::LaunchAgent,
            None,
        ))
        .invoke_handler(tauri::generate_handler![
            service::admin_reinstall_service,
            service::ensure_service_running,
            service::daemon_exec,
            service::get_udid,
            service::get_platform_info,
            updater::check_update_now,
            updater::apply_update_now,
            updater::get_update_status,
            tray::sync_locale,
            service::get_pid,
            log_upload::upload_service_log_command,
        ]);

    #[cfg(feature = "mcp-bridge")]
    {
        builder = builder.plugin(tauri_plugin_mcp_bridge::init());
    }

    builder
        .manage(tray::TrayLocale(std::sync::Mutex::new("en-US".to_string())))
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

            // In NE mode: register NE state callback before ensuring NE is installed.
            // This ensures state change events are emitted to the webapp from startup.
            #[cfg(all(target_os = "macos", feature = "ne-mode"))]
            ne::register_state_callback(app.handle().clone());

            // Ensure k2 service / NE configuration is running with correct version
            let app_version = env!("CARGO_PKG_VERSION").to_string();
            log::info!("[startup] App version: {}, os: {}", app_version, std::env::consts::OS);
            tauri::async_runtime::spawn(async move {
                match service::ensure_service_running(app_version).await {
                    Ok(()) => log::info!("[startup] Service ready"),
                    Err(e) => log::error!("[startup] Service error: {}", e),
                }
            });

            // Start SSE status stream (daemon mode only — not NE mode)
            #[cfg(not(all(target_os = "macos", feature = "ne-mode")))]
            {
                let sse_handle = app.handle().clone();
                status_stream::start(sse_handle);
            }

            // Start auto-updater
            updater::start_auto_updater(app.handle().clone());

            Ok(())
        })
        .build(tauri::generate_context!())
        .expect("error while building tauri application")
        .run(|app, event| {
            if let RunEvent::ExitRequested { .. } = &event {
                updater::install_pending_update(app);
            }
        });
}
