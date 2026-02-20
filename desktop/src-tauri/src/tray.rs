//! System tray module

use std::sync::Mutex;
use tauri::{
    menu::{Menu, MenuItem},
    tray::{MouseButtonState, TrayIconBuilder, TrayIconEvent},
    Manager, State,
};

/// Shared locale state for tray menu i18n
pub struct TrayLocale(pub Mutex<String>);

const TRAY_ID: &str = "main-tray";

/// Get translated tray menu text: (show, quit)
fn get_translations(locale: &str) -> (&'static str, &'static str) {
    match locale {
        "zh-CN" => ("显示", "退出"),
        "zh-TW" | "zh-HK" => ("顯示", "退出"),
        "ja" => ("表示", "終了"),
        // en-US, en-GB, en-AU and any unknown locale
        _ => ("Show", "Quit"),
    }
}

fn build_tray_menu(
    app: &tauri::AppHandle,
    locale: &str,
) -> Result<Menu<tauri::Wry>, Box<dyn std::error::Error>> {
    let (show_text, quit_text) = get_translations(locale);

    let show = MenuItem::with_id(app, "show", show_text, true, None::<&str>)?;
    let separator = MenuItem::with_id(app, "sep1", "---", false, None::<&str>)?;
    let quit = MenuItem::with_id(app, "quit", quit_text, true, None::<&str>)?;

    Ok(Menu::with_items(app, &[&show, &separator, &quit])?)
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

    let _tray = TrayIconBuilder::with_id(TRAY_ID)
        .icon(app.default_window_icon().unwrap().clone())
        .menu(&menu)
        .menu_on_left_click(false)
        .on_menu_event(|app, event| match event.id.as_ref() {
            "show" => {
                if let Some(window) = app.get_webview_window("main") {
                    let _ = window.show();
                    let _ = window.set_focus();
                }
            }
            "quit" => {
                app.exit(0);
            }
            _ => {}
        })
        .on_tray_icon_event(|tray, event| {
            if let TrayIconEvent::Click {
                button_state: MouseButtonState::Up,
                ..
            } = event
            {
                let app = tray.app_handle();
                if let Some(window) = app.get_webview_window("main") {
                    let _ = window.show();
                    let _ = window.set_focus();
                }
            }
        })
        .build(app)?;

    Ok(())
}
