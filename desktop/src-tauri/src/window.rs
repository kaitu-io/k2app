//! Window management module
//!
//! Startup: Window created hidden, sized based on screen, then shown
//! Runtime: show/hide via tray, dock, or second instance
//!
//! Sizing is computed from `Monitor::work_area()` — the usable rectangle that
//! already excludes macOS Dock + menu bar, Windows taskbar, and the MacBook
//! Pro 14"/16" notch area. Using `monitor.size()` (full physical screen)
//! would push the window under the Dock on small-screen macs or with large
//! text scaling.

use std::sync::atomic::{AtomicBool, Ordering};
use tauri::{AppHandle, LogicalSize, Manager, WebviewWindow};

/// Track if app was started in minimized mode (autostart with --minimized)
/// Only affects frontend_ready() - user actions always show window
static IS_MINIMIZED_START: AtomicBool = AtomicBool::new(false);

// ─── kaitu: portrait phone-shaped window ─────────────────────────────────────
/// Aspect ratio: 9:20 (modern iPhone-like tall screen)
#[cfg(not(brand_overleap))]
const ASPECT_RATIO: f64 = 9.0 / 20.0;

/// Ideal height ratio relative to usable work area height
#[cfg(not(brand_overleap))]
const IDEAL_HEIGHT_RATIO: f64 = 0.80;

/// Maximum height ratio - window must not exceed this fraction of usable area
#[cfg(not(brand_overleap))]
const MAX_HEIGHT_RATIO: f64 = 0.85;

/// Preferred minimum window dimensions (applied when work area is large enough)
#[cfg(not(brand_overleap))]
const MIN_WIDTH: u32 = 320;
#[cfg(not(brand_overleap))]
const MIN_HEIGHT: u32 = 568;

/// Hard floor for dynamic min height — UI is not designed/tested below this,
/// must match the static `minHeight` in tauri.conf.json so runtime relaxation
/// never goes below what the config already allows.
#[cfg(not(brand_overleap))]
const MIN_HEIGHT_FLOOR: u32 = 460;

/// Maximum width to prevent overly wide windows on large screens
#[cfg(not(brand_overleap))]
const MAX_WIDTH: u32 = 480;

// ─── overleap: landscape desktop window with sidebar layout ──────────────────
// Sizes must match tauri.conf.overleap.json (width/height/minWidth/minHeight).
#[cfg(brand_overleap)]
const DEFAULT_WIDTH: u32 = 1040;
#[cfg(brand_overleap)]
const DEFAULT_HEIGHT: u32 = 700;
#[cfg(brand_overleap)]
const MIN_WIDTH: u32 = 880;
#[cfg(brand_overleap)]
const MIN_HEIGHT: u32 = 620;
/// Hard floor for dynamic min height on tiny work areas (the webapp falls back
/// to its mobile layout below a 600px short side — accepted degradation).
#[cfg(brand_overleap)]
const MIN_HEIGHT_FLOOR: u32 = 560;
/// The window never takes more than this fraction of the work area, per axis.
#[cfg(brand_overleap)]
const MAX_AREA_RATIO: f64 = 0.85;

/// Compute the runtime minimum window height for the given usable height.
///
/// - Usable height large enough for `MIN_HEIGHT` → return `MIN_HEIGHT` (default).
/// - Usable height smaller → return the usable height so the window fits.
/// - Floor at `MIN_HEIGHT_FLOOR` so the UI is never crushed below its
///   designed lower bound; on an extremely small usable area the window
///   will overflow by at most `MIN_HEIGHT_FLOOR - usable_height` px.
fn calculate_dynamic_min_height(usable_height: u32) -> u32 {
    MIN_HEIGHT.min(usable_height).max(MIN_HEIGHT_FLOOR)
}

/// Calculate window size based on screen dimensions
/// Maintains 9:20 aspect ratio while respecting screen boundaries
#[cfg(not(brand_overleap))]
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

/// Overleap: fixed default size, clamped to 85% of the work area per axis and
/// never below the minimum. No aspect lock — the sidebar layout is fluid.
#[cfg(brand_overleap)]
fn calculate_window_size_overleap(usable_width: u32, usable_height: u32) -> (u32, u32) {
    let max_w = (usable_width as f64 * MAX_AREA_RATIO) as u32;
    let max_h = (usable_height as f64 * MAX_AREA_RATIO) as u32;
    let width = DEFAULT_WIDTH.min(max_w).max(MIN_WIDTH);
    let height = DEFAULT_HEIGHT.min(max_h).max(MIN_HEIGHT);
    (width, height)
}

/// Returns the primary monitor, falling back to the first available monitor
/// if the OS doesn't designate a primary (happens on some multi-monitor
/// Windows setups).
fn get_primary_or_first_monitor(app: &AppHandle) -> tauri::Monitor {
    match app.primary_monitor() {
        Ok(Some(m)) => {
            log::debug!("Using primary monitor");
            m
        }
        Ok(None) | Err(_) => {
            log::info!("primary_monitor() unavailable, using available_monitors()");
            let monitors = app
                .available_monitors()
                .expect("Failed to enumerate monitors - should never happen on desktop");
            monitors
                .into_iter()
                .next()
                .expect("No monitors found - desktop app requires at least one display")
        }
    }
}

/// Returns the monitor's usable-area logical height (work_area excludes
/// Dock + menu bar on macOS, taskbar on Windows, notch on MacBook Pro).
///
/// Debug builds honor `K2_FAKE_USABLE_HEIGHT=<px>` to simulate small-screen
/// scenarios that are otherwise impossible to reproduce without specific
/// hardware (e.g. old MacBook Air with macOS "Larger Text" scaling). This
/// hook is stripped from release builds.
fn get_usable_logical_height(monitor: &tauri::Monitor) -> u32 {
    #[cfg(debug_assertions)]
    {
        if let Ok(raw) = std::env::var("K2_FAKE_USABLE_HEIGHT") {
            if let Ok(v) = raw.parse::<u32>() {
                log::warn!(
                    "[test] K2_FAKE_USABLE_HEIGHT override active: returning {} (real work_area ignored)",
                    v
                );
                return v;
            }
            log::warn!(
                "[test] K2_FAKE_USABLE_HEIGHT set to invalid value {:?}, ignoring",
                raw
            );
        }
    }
    let scale_factor = monitor.scale_factor();
    let work_area = monitor.work_area();
    (work_area.size.height as f64 / scale_factor) as u32
}

/// Work-area logical width (same source as `get_usable_logical_height`).
#[cfg(brand_overleap)]
fn get_usable_logical_width(monitor: &tauri::Monitor) -> u32 {
    let scale_factor = monitor.scale_factor();
    (monitor.work_area().size.width as f64 / scale_factor) as u32
}

/// Get optimal window size based on the monitor's usable work area.
/// Returns (width, height) in logical pixels.
/// This function MUST succeed - desktop apps always have a display.
fn get_optimal_window_size(app: &AppHandle) -> (u32, u32, u32) {
    let monitor = get_primary_or_first_monitor(app);
    let scale_factor = monitor.scale_factor();
    let physical_size = monitor.size();
    let work_area = monitor.work_area();

    let usable_height = get_usable_logical_height(&monitor);
    #[cfg(not(brand_overleap))]
    let (width, height) = calculate_window_size(usable_height);
    #[cfg(brand_overleap)]
    let (width, height) =
        calculate_window_size_overleap(get_usable_logical_width(&monitor), usable_height);

    log::info!(
        "Window size: {}x{} logical (screen: {}x{} physical, work_area: {}x{} physical, scale: {:.0}%, usable_logical_h: {})",
        width,
        height,
        physical_size.width,
        physical_size.height,
        work_area.size.width,
        work_area.size.height,
        scale_factor * 100.0,
        usable_height,
    );

    (width, height, usable_height)
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

    // Compute optimal size + usable area based on current monitor
    let (width, height, usable_height) = get_optimal_window_size(app);
    apply_dynamic_min_size(&window, usable_height);

    log::info!(
        "Adjusting window size to {}x{}",
        width,
        height
    );

    if let Err(e) = window.set_size(LogicalSize::new(width, height)) {
        log::error!("Failed to set window size: {}", e);
    }

    if let Err(e) = window.center() {
        log::error!("Failed to center window: {}", e);
    }

    log::info!("Window size adjusted, waiting for frontend_ready()");

    Some(window)
}

/// Re-read the current monitor's work area and update only min_size.
/// Call this on scale factor change ("Larger Text" toggled, monitor
/// switched with different DPI) so the min constraint stays consistent
/// with what can actually fit — without resizing the user's window.
pub fn reclamp_min_size(app: &AppHandle) {
    let Some(window) = app.get_webview_window("main") else {
        return;
    };
    let monitor = get_primary_or_first_monitor(app);
    let usable_height = get_usable_logical_height(&monitor);
    apply_dynamic_min_size(&window, usable_height);
}

fn apply_dynamic_min_size(window: &WebviewWindow, usable_height: u32) {
    let dynamic_min_h = calculate_dynamic_min_height(usable_height);
    log::info!(
        "Applying dynamic min_size {}x{} (usable height: {})",
        MIN_WIDTH,
        dynamic_min_h,
        usable_height
    );
    if let Err(e) = window.set_min_size(Some(LogicalSize::<f64>::new(
        MIN_WIDTH as f64,
        dynamic_min_h as f64,
    ))) {
        log::warn!("Failed to set window min_size: {}", e);
    }
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

#[cfg(all(test, brand_overleap))]
mod overleap_tests {
    use super::*;

    #[test]
    fn overleap_constants_match_tauri_conf_overleap() {
        // Keep in sync with tauri.conf.overleap.json minWidth/minHeight.
        assert_eq!(MIN_WIDTH, 880);
        assert_eq!(MIN_HEIGHT, 620);
        assert_eq!(MIN_HEIGHT_FLOOR, 560);
    }

    #[test]
    fn overleap_1080p_gets_default_size() {
        // 1080p work area ≈ 1920x1040 logical; 85% of both exceeds the defaults.
        let (w, h) = calculate_window_size_overleap(1920, 1040);
        assert_eq!((w, h), (DEFAULT_WIDTH, DEFAULT_HEIGHT));
    }

    #[test]
    fn overleap_small_screen_shrinks_but_keeps_min() {
        // MacBook Air 13" with Larger Text: ~1147x700 usable.
        let (w, h) = calculate_window_size_overleap(1147, 700);
        assert!(w >= MIN_WIDTH && w <= DEFAULT_WIDTH, "w={w}");
        assert_eq!(h, MIN_HEIGHT, "85% of 700 = 595 < MIN_HEIGHT → clamp up to MIN_HEIGHT");
    }

    #[test]
    fn overleap_never_exceeds_85_percent_of_a_large_area() {
        let (w, h) = calculate_window_size_overleap(1200, 800);
        assert!(w <= (1200.0 * MAX_AREA_RATIO) as u32);
        assert!(h <= (800.0 * MAX_AREA_RATIO) as u32);
    }

    #[test]
    fn overleap_dynamic_min_height_floors_at_560() {
        assert_eq!(calculate_dynamic_min_height(1000), MIN_HEIGHT);
        assert_eq!(calculate_dynamic_min_height(600), 600);
        assert_eq!(calculate_dynamic_min_height(500), MIN_HEIGHT_FLOOR);
    }
}

// kaitu portrait-window assertions — unchanged; not applicable to the overleap build.
#[cfg(all(test, not(brand_overleap)))]
mod tests {
    use super::*;

    #[test]
    fn test_dynamic_min_height_normal_screen() {
        // Usable height comfortably above MIN_HEIGHT → use MIN_HEIGHT
        assert_eq!(calculate_dynamic_min_height(1000), MIN_HEIGHT);
        assert_eq!(calculate_dynamic_min_height(800), MIN_HEIGHT);
        assert_eq!(calculate_dynamic_min_height(568), MIN_HEIGHT);
    }

    #[test]
    fn test_dynamic_min_height_small_screen_above_floor() {
        // Usable height between FLOOR and MIN_HEIGHT → use usable
        // (window fits snugly against work area)
        assert_eq!(calculate_dynamic_min_height(540), 540);
        assert_eq!(calculate_dynamic_min_height(500), 500);
        assert_eq!(calculate_dynamic_min_height(460), MIN_HEIGHT_FLOOR);
    }

    #[test]
    fn test_dynamic_min_height_below_floor_clamps_to_floor() {
        // Extremely small usable area (pathological) — UI would be unusable,
        // stay at the FLOOR and accept mild overflow.
        assert_eq!(calculate_dynamic_min_height(400), MIN_HEIGHT_FLOOR);
        assert_eq!(calculate_dynamic_min_height(300), MIN_HEIGHT_FLOOR);
        assert_eq!(calculate_dynamic_min_height(0), MIN_HEIGHT_FLOOR);
    }

    #[test]
    fn test_dynamic_min_height_never_exceeds_min_height() {
        // Must never return > MIN_HEIGHT regardless of input
        for usable in [0u32, 100, 460, 500, 568, 600, 900, 1500, 5000] {
            let got = calculate_dynamic_min_height(usable);
            assert!(
                got <= MIN_HEIGHT,
                "usable={}: got {} > MIN_HEIGHT {}",
                usable,
                got,
                MIN_HEIGHT
            );
        }
    }

    #[test]
    fn test_dynamic_min_height_matches_config_floor() {
        // MIN_HEIGHT_FLOOR must stay in sync with tauri.conf.json minHeight.
        // If this assertion fires, update both together.
        assert_eq!(MIN_HEIGHT_FLOOR, 460);
    }

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
