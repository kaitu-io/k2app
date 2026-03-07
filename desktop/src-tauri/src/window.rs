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

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_window_size_1080p() {
        let (width, height) = calculate_window_size(1080);

        assert!(width >= MIN_WIDTH, "width {} below MIN_WIDTH {}", width, MIN_WIDTH);
        assert!(width <= MAX_WIDTH, "width {} above MAX_WIDTH {}", width, MAX_WIDTH);
        assert!(height >= MIN_HEIGHT, "height {} below MIN_HEIGHT {}", height, MIN_HEIGHT);

        let max_allowed = (1080_f64 * MAX_HEIGHT_RATIO) as u32;
        assert!(height <= max_allowed, "height {} exceeds max allowed {}", height, max_allowed);
    }

    #[test]
    fn test_window_size_768p() {
        let (width, height) = calculate_window_size(768);

        let max_allowed = (768_f64 * MAX_HEIGHT_RATIO) as u32;
        assert!(
            height <= max_allowed,
            "height {} exceeds MAX_HEIGHT_RATIO limit {} for 768p screen",
            height,
            max_allowed
        );
        assert!(width <= MAX_WIDTH, "width {} above MAX_WIDTH {}", width, MAX_WIDTH);

        // On 768p, MAX_HEIGHT_RATIO constraint dominates: capping height to 652
        // recalculates width to 293, which falls below MIN_WIDTH (320).
        // This is expected — the function prioritizes not exceeding screen height
        // over maintaining minimum width on low-res screens.
        assert_eq!(height, max_allowed, "768p should be height-capped by MAX_HEIGHT_RATIO");
        assert!(
            width < MIN_WIDTH,
            "768p is expected to produce width {} below MIN_WIDTH {} due to height cap priority",
            width,
            MIN_WIDTH
        );
    }

    #[test]
    fn test_window_size_4k() {
        let (width, height) = calculate_window_size(2160);

        assert!(
            width <= MAX_WIDTH,
            "4K screen produced width {} exceeding MAX_WIDTH {}",
            width,
            MAX_WIDTH
        );
        assert!(height >= MIN_HEIGHT, "height {} below MIN_HEIGHT {}", height, MIN_HEIGHT);

        let max_allowed = (2160_f64 * MAX_HEIGHT_RATIO) as u32;
        assert!(height <= max_allowed, "height {} exceeds max allowed {}", height, max_allowed);
    }

    #[test]
    fn test_window_size_small_screen() {
        let (width, height) = calculate_window_size(600);

        let max_allowed = (600_f64 * MAX_HEIGHT_RATIO) as u32;
        assert!(
            height <= max_allowed,
            "height {} exceeds max allowed {} for 600p screen",
            height,
            max_allowed
        );

        // On a 600p screen, the MAX_HEIGHT_RATIO constraint caps height to 510,
        // which gives width = 510 * 0.45 = 229. This is below MIN_WIDTH (320)
        // but the function correctly prioritizes staying within screen bounds.
        assert_eq!(height, max_allowed, "600p should be height-capped by MAX_HEIGHT_RATIO");
        assert!(
            width < MIN_WIDTH,
            "600p is expected to produce width {} below MIN_WIDTH {} due to height cap priority",
            width,
            MIN_WIDTH
        );
    }

    #[test]
    fn test_window_size_aspect_ratio_maintained() {
        let screen_heights = [600, 768, 900, 1080, 1440, 2160];
        let tolerance = 0.02; // Allow 2% deviation due to integer truncation

        for &screen_h in &screen_heights {
            let (width, height) = calculate_window_size(screen_h);
            let actual_ratio = width as f64 / height as f64;
            let deviation = (actual_ratio - ASPECT_RATIO).abs();

            assert!(
                deviation < tolerance,
                "screen_height={}: ratio {:.4} deviates from ASPECT_RATIO {:.4} by {:.4} (tolerance {:.4})",
                screen_h,
                actual_ratio,
                ASPECT_RATIO,
                deviation,
                tolerance
            );
        }
    }
}
