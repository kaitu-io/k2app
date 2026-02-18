//! System tray module

use std::sync::Mutex;
use tauri::{
    menu::{Menu, MenuItem},
    tray::{MouseButton, MouseButtonState, TrayIconBuilder, TrayIconEvent},
    Manager, State,
};

/// Shared locale state for tray menu i18n
pub struct TrayLocale(pub Mutex<String>);

/// IPC command: sync locale from webapp to Rust for tray menu i18n
#[tauri::command]
pub fn sync_locale(locale: String, state: State<'_, TrayLocale>) {
    let mut current = state.0.lock().unwrap();
    *current = locale;
    log::info!("[tray] Locale synced: {}", &*current);
}

fn service_base_url() -> String {
    let port = std::env::var("K2_DAEMON_PORT")
        .ok()
        .and_then(|v| v.parse::<u16>().ok())
        .unwrap_or(1777);
    format!("http://127.0.0.1:{}", port)
}

pub fn init_tray(app: &tauri::AppHandle) -> Result<(), Box<dyn std::error::Error>> {
    let show_hide = MenuItem::with_id(app, "show_hide", "Show/Hide", true, None::<&str>)?;
    let separator1 = MenuItem::with_id(app, "sep1", "---", false, None::<&str>)?;
    let connect = MenuItem::with_id(app, "connect", "Connect", true, None::<&str>)?;
    let disconnect = MenuItem::with_id(app, "disconnect", "Disconnect", true, None::<&str>)?;
    let separator2 = MenuItem::with_id(app, "sep2", "---", false, None::<&str>)?;
    let quit = MenuItem::with_id(app, "quit", "Quit", true, None::<&str>)?;

    let menu = Menu::with_items(app, &[&show_hide, &separator1, &connect, &disconnect, &separator2, &quit])?;

    let _tray = TrayIconBuilder::new()
        .icon(app.default_window_icon().unwrap().clone())
        .menu(&menu)
        .on_menu_event(|app, event| {
            match event.id.as_ref() {
                "show_hide" => {
                    if let Some(window) = app.get_webview_window("main") {
                        if window.is_visible().unwrap_or(false) {
                            let _ = window.hide();
                        } else {
                            let _ = window.show();
                            let _ = window.set_focus();
                        }
                    }
                }
                "connect" => {
                    // Send connect command to k2 daemon (tray uses direct HTTP, not VpnClient)
                    std::thread::spawn(|| {
                        if let Ok(client) = reqwest::blocking::Client::builder()
                            .timeout(std::time::Duration::from_secs(5))
                            .no_proxy()
                            .build()
                        {
                            let _ = client
                                .post(format!("{}/api/core", service_base_url()))
                                .json(&serde_json::json!({"action": "up"}))
                                .send();
                        }
                    });
                }
                "disconnect" => {
                    std::thread::spawn(|| {
                        if let Ok(client) = reqwest::blocking::Client::builder()
                            .timeout(std::time::Duration::from_secs(5))
                            .no_proxy()
                            .build()
                        {
                            let _ = client
                                .post(format!("{}/api/core", service_base_url()))
                                .json(&serde_json::json!({"action": "down"}))
                                .send();
                        }
                    });
                }
                "quit" => {
                    app.exit(0);
                }
                _ => {}
            }
        })
        .on_tray_icon_event(|tray, event| {
            if let TrayIconEvent::Click {
                button: MouseButton::Left,
                button_state: MouseButtonState::Up,
                ..
            } = event
            {
                let app = tray.app_handle();
                if let Some(window) = app.get_webview_window("main") {
                    if window.is_visible().unwrap_or(false) {
                        let _ = window.hide();
                    } else {
                        let _ = window.show();
                        let _ = window.set_focus();
                    }
                }
            }
        })
        .build(app)?;

    Ok(())
}
