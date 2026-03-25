#![cfg_attr(not(debug_assertions), windows_subsystem = "windows")]

mod channel;
#[cfg(target_os = "linux")]
mod linux_updater;
mod log_upload;
mod ne;
mod service;
mod storage;
mod storage_crypto;
mod status_stream;
mod tray;
mod updater;
mod window;

use std::path::PathBuf;
use tauri::{Manager, RunEvent, WindowEvent};

/// Desktop log directory — shared between log plugin and log_upload.
pub(crate) fn get_desktop_log_dir() -> PathBuf {
    #[cfg(target_os = "macos")]
    {
        dirs::home_dir()
            .map(|h| h.join("Library/Logs/kaitu"))
            .unwrap_or_else(|| PathBuf::from("/tmp/kaitu"))
    }

    #[cfg(target_os = "windows")]
    {
        dirs::data_local_dir()
            .map(|d| d.join("kaitu").join("logs"))
            .unwrap_or_else(|| PathBuf::from(r"C:\temp\kaitu"))
    }

    #[cfg(target_os = "linux")]
    {
        dirs::home_dir()
            .map(|h| h.join(".local/share/kaitu/logs"))
            .unwrap_or_else(|| PathBuf::from("/tmp/kaitu"))
    }

    #[cfg(not(any(target_os = "macos", target_os = "windows", target_os = "linux")))]
    {
        PathBuf::from("/tmp/kaitu")
    }
}

fn resolve_log_dir() -> PathBuf {
    let log_dir = get_desktop_log_dir();
    if let Err(e) = std::fs::create_dir_all(&log_dir) {
        eprintln!("Warning: Failed to create log directory {}: {}", log_dir.display(), e);
    }
    log_dir
}

/// Hide window (Tauri command for TypeScript)
#[tauri::command]
fn hide_window(app: tauri::AppHandle) {
    window::hide_window(&app);
}

/// Show window (Tauri command for TypeScript)
#[tauri::command]
fn show_window(app: tauri::AppHandle) {
    window::show_window(&app);
}

fn main() {
    #[allow(unused_mut)]
    let mut builder = tauri::Builder::default()
        .plugin(tauri_plugin_localhost::Builder::new(14580).build())
        .plugin({
            let log_dir = resolve_log_dir();
            // K2_BUILD_LOG_LEVEL env var at compile time (default: debug)
            tauri_plugin_log::Builder::new()
                .level(match option_env!("K2_BUILD_LOG_LEVEL") {
                    Some("info") => log::LevelFilter::Info,
                    Some("warn") => log::LevelFilter::Warn,
                    Some("error") => log::LevelFilter::Error,
                    _ => log::LevelFilter::Debug,
                })
                .timezone_strategy(tauri_plugin_log::TimezoneStrategy::UseLocal)
                .filter(|metadata| !metadata.target().starts_with("reqwest"))
                .target(tauri_plugin_log::Target::new(
                    tauri_plugin_log::TargetKind::Folder {
                        path: log_dir,
                        file_name: Some("desktop".into()),
                    },
                ))
                .max_file_size(20_000_000) // 20 MB
                .rotation_strategy(tauri_plugin_log::RotationStrategy::KeepOne)
                .build()
        })
        .plugin(tauri_plugin_single_instance::init(|app, _args, _cwd| {
            window::show_window_user_action(app);
        }))
        .plugin(tauri_plugin_process::init())
        .plugin(tauri_plugin_updater::Builder::new().build())
        .plugin(tauri_plugin_opener::init())
        .plugin(tauri_plugin_clipboard_manager::init())
        .plugin(tauri_plugin_autostart::init(
            tauri_plugin_autostart::MacosLauncher::LaunchAgent,
            Some(vec!["--minimized"]),
        ))
        .invoke_handler(tauri::generate_handler![
            show_window,
            hide_window,
            service::admin_reinstall_service,
            service::ensure_service_running,
            service::daemon_exec,
            service::daemon_helper_exec,
            service::set_log_level,
            service::get_platform_info,
            updater::check_update_now,
            updater::apply_update_now,
            updater::get_update_status,
            updater::get_update_channel,
            updater::set_update_channel,
            tray::sync_locale,
            service::get_pid,
            service::set_dev_enabled,
            log_upload::upload_service_log_command,
            storage::storage_get,
            storage::storage_set,
            storage::storage_remove,
        ]);

    #[cfg(feature = "mcp-bridge")]
    {
        builder = builder.plugin(tauri_plugin_mcp_bridge::init());
    }

    let storage_state = storage::StorageState::new();

    builder
        .manage(tray::TrayLocale(std::sync::Mutex::new("en-US".to_string())))
        .manage(storage_state)
        .setup(|app| {
            // Initialize native storage (load from disk)
            let state = app.handle().state::<storage::StorageState>();
            storage::init(app.handle(), &state);
            // Check for --minimized argument (autostart)
            let args: Vec<String> = std::env::args().collect();
            let should_minimize = args.contains(&"--minimized".to_string());
            window::init_startup_state(should_minimize);

            // Adjust window size based on screen and show (skip if minimized)
            if !should_minimize {
                if let Some(_win) = window::adjust_window_size(app.handle()) {
                    window::show_window(app.handle());
                    #[cfg(debug_assertions)]
                    _win.open_devtools();
                }
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

            // Installing a beta build activates the beta channel unconditionally.
            // User can switch back to stable in-app, which triggers a stable version download.
            // Next launch (stable binary) won't contain "-beta" so channel stays as-is.
            if app_version.contains("-beta") {
                log::info!("[startup] Beta build detected ({}), activating beta channel", app_version);
                if let Err(e) = channel::save_channel(app.handle(), "beta") {
                    log::error!("[startup] Failed to save beta channel: {}", e);
                }
            }
            let startup_handle = app.handle().clone();
            tauri::async_runtime::spawn(async move {
                match service::ensure_service_running(app_version).await {
                    Ok(()) => {
                        log::info!("[startup] Service ready");
                        // Force daemon to debug level if beta channel
                        if channel::get_channel(&startup_handle) == "beta" {
                            log::info!("[startup] Beta channel: forcing daemon debug log level");
                            let _ = tokio::task::spawn_blocking(||
                                service::set_log_level_internal("debug")
                            ).await;
                        }
                    }
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
            match event {
                RunEvent::WindowEvent {
                    label,
                    event: WindowEvent::CloseRequested { api, .. },
                    ..
                } => {
                    if label == "main" {
                        api.prevent_close();
                        window::hide_window(app);
                        log::debug!("Window close requested, hiding instead");
                    }
                }
                RunEvent::ExitRequested { .. } => {
                    log::info!("Exit requested, stopping VPN before exit");
                    service::stop_vpn();

                    // On Windows, NSIS installer already launched — skip restart
                    #[cfg(not(target_os = "windows"))]
                    updater::install_pending_update(app);
                }
                #[cfg(target_os = "macos")]
                RunEvent::Reopen {
                    has_visible_windows,
                    ..
                } => {
                    if !has_visible_windows {
                        log::debug!("Dock icon clicked with no visible windows, showing window");
                        window::show_window_user_action(app);
                    }
                }
                _ => {}
            }
        });
}
