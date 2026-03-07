//! System tray module
//!
//! - All platforms: left click shows window, right click shows context menu
//! - Menu is rebuilt when locale changes

use std::sync::Mutex;
use tauri::{
    menu::{Menu, MenuItem},
    tray::TrayIconBuilder,
    State,
};

use tauri::tray::{MouseButtonState, TrayIconEvent};

use crate::service;
use crate::window;

/// Shared locale state for tray menu i18n
pub struct TrayLocale(pub Mutex<String>);

const TRAY_ID: &str = "main-tray";

/// Get translated tray menu text: (show, hide, quit)
fn get_translations(locale: &str) -> (&'static str, &'static str, &'static str) {
    match locale {
        "zh-CN" => ("显示窗口", "隐藏窗口", "退出"),
        "zh-TW" | "zh-HK" => ("顯示窗口", "隱藏窗口", "退出"),
        "ja" => ("ウィンドウを表示", "ウィンドウを非表示", "終了"),
        // en-US, en-GB, en-AU and any unknown locale
        _ => ("Show Window", "Hide Window", "Quit"),
    }
}

fn build_tray_menu(
    app: &tauri::AppHandle,
    locale: &str,
) -> Result<Menu<tauri::Wry>, Box<dyn std::error::Error>> {
    let (show_text, hide_text, quit_text) = get_translations(locale);

    let show = MenuItem::with_id(app, "show", show_text, true, None::<&str>)?;
    let hide = MenuItem::with_id(app, "hide", hide_text, true, None::<&str>)?;
    let separator = MenuItem::with_id(app, "sep1", "---", false, None::<&str>)?;
    let quit = MenuItem::with_id(app, "quit", quit_text, true, None::<&str>)?;

    Ok(Menu::with_items(app, &[&show, &hide, &separator, &quit])?)
}

/// IPC command: sync locale from webapp to Rust for tray menu i18n
#[tauri::command]
pub fn sync_locale(locale: String, state: State<'_, TrayLocale>, app: tauri::AppHandle) {
    let mut current = state.0.lock().unwrap();
    *current = locale.clone();
    log::info!("[tray] Locale synced: {}", &*current);
    drop(current);

    match build_tray_menu(&app, &locale) {
        Ok(menu) => {
            if let Some(tray) = app.tray_by_id(TRAY_ID) {
                if let Err(e) = tray.set_menu(Some(menu)) {
                    log::error!("[tray] Failed to set menu: {}", e);
                }
            }
        }
        Err(e) => log::error!("[tray] Failed to build menu: {}", e),
    }
}

pub fn init_tray(app: &tauri::AppHandle) -> Result<(), Box<dyn std::error::Error>> {
    let menu = build_tray_menu(app, "en-US")?;

    let mut builder = TrayIconBuilder::with_id(TRAY_ID)
        .icon(app.default_window_icon().unwrap().clone())
        .menu(&menu)
        .on_menu_event(|app, event| match event.id.as_ref() {
            "show" => {
                window::show_window_user_action(app);
            }
            "hide" => {
                window::hide_window(app);
            }
            "quit" => {
                log::info!("Quit menu item clicked, stopping VPN and exiting app");
                service::stop_vpn();
                app.exit(0);
            }
            _ => {}
        });

    // All platforms: left click shows window, right click shows menu
    builder = builder
        .show_menu_on_left_click(false)
        .on_tray_icon_event(|tray, event| {
            if let TrayIconEvent::Click {
                button_state: MouseButtonState::Up,
                ..
            } = event
            {
                window::show_window_user_action(tray.app_handle());
            }
        });

    let _tray = builder.build(app)?;

    Ok(())
}
