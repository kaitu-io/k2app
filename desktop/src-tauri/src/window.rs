//! Window management module
//!
//! Startup: Window created hidden, sized based on screen, then shown
//! Runtime: show/hide via tray, dock, or second instance

use std::sync::atomic::{AtomicBool, Ordering};
use tauri::{AppHandle, LogicalSize, Manager, WebviewWindow};

/// Track if app was started in minimized mode (autostart with --minimized)
/// Only affects frontend_ready() - user actions always show window
static IS_MINIMIZED_START: AtomicBool = AtomicBool::new(false);

/// Aspect ratio: 9:20 (modern iPhone-like tall screen)
const ASPECT_RATIO: f64 = 9.0 / 20.0;

/// Ideal height ratio relative to screen height
const IDEAL_HEIGHT_RATIO: f64 = 0.80;

/// Maximum height ratio - window must not exceed this (for Windows low-res screens)
const MAX_HEIGHT_RATIO: f64 = 0.85;

/// Minimum dimensions to ensure usability
const MIN_WIDTH: u32 = 320;
const MIN_HEIGHT: u32 = 568;

/// Maximum width to prevent overly wide windows on large screens
const MAX_WIDTH: u32 = 480;

/// Calculate window size based on screen dimensions
/// Maintains 9:20 aspect ratio while respecting screen boundaries
fn calculate_window_size(screen_height: u32) -> (u32, u32) {
    let max_allowed_height = (screen_height as f64 * MAX_HEIGHT_RATIO) as u32;

    let mut height = (screen_height as f64 * IDEAL_HEIGHT_RATIO) as u32;
    let mut width = (height as f64 * ASPECT_RATIO) as u32;

    // Constraint 1: Width must not exceed maximum
    if width > MAX_WIDTH {
        width = MAX_WIDTH;
        height = (width as f64 / ASPECT_RATIO) as u32;
    }

    // Constraint 2: Width must not be below minimum
    if width < MIN_WIDTH {
        width = MIN_WIDTH;
        height = (width as f64 / ASPECT_RATIO) as u32;
    }

    // Constraint 3 (Critical): Height must not exceed screen limit
    if height > max_allowed_height {
        height = max_allowed_height;
        width = (height as f64 * ASPECT_RATIO) as u32;
    }

    // Constraint 4: Height should meet minimum if screen allows
    if height < MIN_HEIGHT && max_allowed_height >= MIN_HEIGHT {
        height = MIN_HEIGHT;
        width = (height as f64 * ASPECT_RATIO) as u32;
    }

    (width, height)
}

/// Get optimal window size based on monitor
/// Returns (width, height) in logical pixels
/// This function MUST succeed - desktop apps always have a display
fn get_optimal_window_size(app: &AppHandle) -> (u32, u32) {
    // Try primary_monitor first
    let monitor = match app.primary_monitor() {
        Ok(Some(m)) => {
            log::debug!("Using primary monitor for window sizing");
            m
        }
        Ok(None) | Err(_) => {
            // primary_monitor can return None on some Windows configs (multi-monitor, etc.)
            // Fall back to available_monitors which MUST have at least one entry
            log::info!("primary_monitor() unavailable, using available_monitors()");

            let monitors = app
                .available_monitors()
                .expect("Failed to enumerate monitors - this should never happen on desktop");

            monitors
                .into_iter()
                .next()
                .expect("No monitors found - desktop app requires at least one display")
        }
    };

    let physical_size = monitor.size();
    let scale_factor = monitor.scale_factor();

    // Convert physical pixels to logical pixels
    let logical_screen_height = (physical_size.height as f64 / scale_factor) as u32;

    let (width, height) = calculate_window_size(logical_screen_height);

    log::info!(
        "Window size: {}x{} logical (screen: {}x{} physical, scale: {:.0}%)",
        width,
        height,
        physical_size.width,
        physical_size.height,
        scale_factor * 100.0
    );

    (width, height)
}

/// Adjust window size based on screen dimensions
///
/// Window properties (title, resizable, etc.) are defined in tauri.conf.json.
/// This function only adjusts the size dynamically based on screen resolution.
/// Window remains hidden (visible: false) until frontend_ready().
///
/// Returns the window reference if found, None otherwise.
pub fn adjust_window_size(app: &AppHandle) -> Option<WebviewWindow> {
    let window = app.get_webview_window("main")?;

    // Calculate optimal size based on screen
    let (width, height) = get_optimal_window_size(app);

    log::info!(
        "Adjusting window size to {}x{} (portrait orientation)",
        width,
        height
    );

    // Set the window size
    if let Err(e) = window.set_size(LogicalSize::new(width, height)) {
        log::error!("Failed to set window size: {}", e);
    }

    // Re-center after resize
    if let Err(e) = window.center() {
        log::error!("Failed to center window: {}", e);
    }

    log::info!("Window size adjusted, waiting for frontend_ready()");

    Some(window)
}

/// Initialize minimized startup state
pub fn init_startup_state(minimized: bool) {
    IS_MINIMIZED_START.store(minimized, Ordering::Relaxed);
    log::info!(
        "Startup mode: {}",
        if minimized { "minimized" } else { "normal" }
    );
}

/// Show the main window with focus (always shows, no conditions)
pub fn show_window(app: &AppHandle) {
    if let Some(window) = app.get_webview_window("main") {
        // Order matters: unminimize first (especially on Windows where hide = minimize)
        let _ = window.unminimize();
        let _ = window.show();
        let _ = window.set_focus();

        // Windows: Use always-on-top trick to bring window to front
        #[cfg(target_os = "windows")]
        {
            let _ = window.set_always_on_top(true);
            let _ = window.set_always_on_top(false);
        }

        log::debug!("Window shown and focused");
    }
}

/// Show window from user action (tray click, dock click, second instance)
/// Clears minimized flag so future auto-shows work
pub fn show_window_user_action(app: &AppHandle) {
    IS_MINIMIZED_START.store(false, Ordering::Relaxed);
    show_window(app);
}

/// Hide the main window
/// On Windows: minimize instead of hide to keep taskbar icon
pub fn hide_window(app: &AppHandle) {
    if let Some(window) = app.get_webview_window("main") {
        #[cfg(target_os = "windows")]
        {
            let _ = window.minimize();
            log::debug!("Window minimized (Windows)");
        }

        #[cfg(not(target_os = "windows"))]
        {
            let _ = window.hide();
            log::debug!("Window hidden");
        }
    }
}
