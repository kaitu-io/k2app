# Desktop Window Management

> Port the production-validated window management system from kaitu/client into k2app.

## Meta

- Status: draft
- Version: 1.0
- Created: 2026-02-20
- Source: `kaitu/client/desktop-tauri/src-tauri/src/window.rs` + `desktop-tauri/src/main.tsx`

## Background

k2app currently has a static window config (430x956 in `tauri.conf.json`) with minimal runtime handling — just `window.show().ok()` in setup. The kaitu/client project has a production-validated window management system that handles:

1. Screen-adaptive window sizing (multi-monitor, DPI-aware)
2. Frontend viewport CSS scaling for narrow windows
3. Platform-specific show/hide behavior (Windows vs macOS)
4. Close-to-tray with proper event handling
5. `--minimized` autostart support

This spec ports that system to k2app verbatim ("抄作业").

## Reference Files (kaitu/client)

| File | Purpose |
|------|---------|
| `desktop-tauri/src-tauri/src/window.rs` | Complete window sizing + show/hide logic (196 lines) |
| `desktop-tauri/src-tauri/src/main.rs:335-423` | Setup flow + RunEvent handlers |
| `desktop-tauri/src-tauri/src/tray.rs:85-104` | Tray left-click → `show_window_user_action()` |
| `desktop-tauri/src/main.tsx:15-64` | `setupViewportScaling()` — CSS transform scaling |
| `desktop-tauri/src/index.html` | HTML/CSS base for scaling support |

## Changes

### 1. New file: `desktop/src-tauri/src/window.rs`

Copy `kaitu/client/desktop-tauri/src-tauri/src/window.rs` verbatim. This is a self-contained module with:

```
Constants:
  ASPECT_RATIO = 9.0 / 20.0
  IDEAL_HEIGHT_RATIO = 0.80
  MAX_HEIGHT_RATIO = 0.85
  MIN_WIDTH = 320, MIN_HEIGHT = 568
  MAX_WIDTH = 480

Static:
  IS_MINIMIZED_START: AtomicBool

Functions:
  calculate_window_size(screen_height) -> (u32, u32)
  get_optimal_window_size(app) -> (u32, u32)    // Multi-monitor + DPI
  adjust_window_size(app) -> Option<WebviewWindow>  // Set size + center
  init_startup_state(minimized: bool)
  show_window(app)        // unminimize→show→focus, Windows always-on-top trick
  show_window_user_action(app)  // Clears minimized flag + show
  hide_window(app)        // Windows: minimize, macOS: hide
```

No modifications needed — the module is generic and uses only `tauri` APIs.

### 2. Modify: `desktop/src-tauri/src/main.rs`

**Add module declaration:**
```rust
mod window;
```

**Add IPC commands:**
```rust
#[tauri::command]
fn hide_window(app: tauri::AppHandle) {
    window::hide_window(&app);
}

#[tauri::command]
fn show_window(app: tauri::AppHandle) {
    window::show_window(&app);
}
```

Register in `invoke_handler`: add `hide_window`, `show_window`.

**Modify setup closure** (replace current `window.show().ok()`):
```rust
.setup(|app| {
    // Check for --minimized argument (autostart)
    let args: Vec<String> = std::env::args().collect();
    let should_minimize = args.contains(&"--minimized".to_string());
    window::init_startup_state(should_minimize);

    // Adjust window size and show (skip if minimized)
    if !should_minimize {
        if let Some(_win) = window::adjust_window_size(app.handle()) {
            window::show_window(app.handle());
            #[cfg(debug_assertions)]
            _win.open_devtools();
        }
    }

    // ... rest of setup (tray, service, updater) unchanged
})
```

**Modify autostart plugin** — pass `--minimized` argument:
```rust
.plugin(tauri_plugin_autostart::init(
    tauri_plugin_autostart::MacosLauncher::LaunchAgent,
    Some(vec!["--minimized"]),  // was: None
))
```

**Add RunEvent handlers** (replace current minimal handler):
```rust
.run(|app, event| {
    match event {
        // Close button → hide (not quit)
        RunEvent::WindowEvent {
            label,
            event: WindowEvent::CloseRequested { api, .. },
            ..
        } => {
            if label == "main" {
                api.prevent_close();
                window::hide_window(app);
            }
        }
        // Exit → apply pending update
        RunEvent::ExitRequested { .. } => {
            updater::install_pending_update(app);
        }
        // macOS: Dock click when hidden → show
        #[cfg(target_os = "macos")]
        RunEvent::Reopen { has_visible_windows, .. } => {
            if !has_visible_windows {
                window::show_window_user_action(app);
            }
        }
        _ => {}
    }
})
```

Add `WindowEvent` to the `use tauri::` import.

**Modify single-instance plugin** — use proper show sequence:
```rust
.plugin(tauri_plugin_single_instance::init(|app, _args, _cwd| {
    if let Some(window) = app.get_webview_window("main") {
        let _ = window.unminimize();
        let _ = window.show();
        let _ = window.set_focus();
    }
}))
```

### 3. Modify: `desktop/src-tauri/src/tray.rs`

Replace inline window show/hide with calls to `window::` module:

```rust
use crate::window;

// Menu "show" handler:
window::show_window_user_action(app);

// Menu "quit" handler: unchanged (app.exit(0))

// Tray icon left-click handler:
window::show_window_user_action(tray.app_handle());
```

Also add a "Hide" menu item (matching kaitu/client pattern):
- Show Window
- Hide Window
- ---
- Quit

### 4. Modify: `webapp/src/main.tsx`

Add viewport scaling for Tauri desktop (before rendering):

```typescript
const DESIGN_WIDTH = 430;

function setupViewportScaling() {
  function applyScale() {
    const windowWidth = window.innerWidth;
    const windowHeight = window.innerHeight;
    const scaleX = windowWidth / DESIGN_WIDTH;
    const scale = Math.min(scaleX, 1);

    if (scale < 1) {
      document.body.style.width = `${DESIGN_WIDTH}px`;
      document.body.style.height = `${windowHeight / scale}px`;
      document.body.style.transform = `scale(${scale})`;
      document.body.style.transformOrigin = "top left";
    } else {
      document.body.style.width = "";
      document.body.style.height = "";
      document.body.style.transform = "";
      document.body.style.transformOrigin = "";
    }
  }

  applyScale();
  window.addEventListener("resize", applyScale);
}
```

Call `setupViewportScaling()` ONLY when `window.__TAURI__` is detected, BEFORE rendering. Mobile/web do not need this (they have their own viewport handling).

### 5. Modify: `webapp/index.html`

Adjust base CSS to support viewport scaling (align with kaitu/client):
- Add `width: 100%` to html/body
- Add `display: flex; flex-direction: column` to `#root`
- Keep existing `user-select: none` and `100dvh`
- Add `@media (prefers-color-scheme: dark)` background color (#0f0f13)

### 6. Add `show_window` call in Tauri bridge

In `webapp/src/services/tauri-k2.ts`, at the end of `injectTauriGlobals()`, add:

```typescript
// Show window after frontend is fully initialized
// Prevents size flashing on Windows
try {
  await invoke('show_window');
  console.info('[TauriK2] Window shown after initialization');
} catch (error) {
  console.warn('[TauriK2] Failed to show window:', error);
}
```

This is belt-and-suspenders with the Rust-side show — ensures window is visible and focused after frontend is ready.

## Acceptance Criteria

1. **Dynamic sizing**: Window size adapts to screen resolution. On 1080p screen, window is ~80% of screen height with 9:20 aspect ratio. On 4K screen with HiDPI, uses logical pixels correctly.
2. **Multi-monitor**: Falls back to first available monitor if primary_monitor() returns None (common on Windows multi-monitor).
3. **Viewport scaling**: When window is resized below 430px width, UI scales down proportionally via CSS transform. MUI Dialogs and Popovers also scale (body-level transform).
4. **Close-to-tray**: Clicking window close button hides to tray (macOS: `hide()`, Windows: `minimize()`). App does NOT quit.
5. **macOS dock reopen**: Clicking dock icon when window is hidden shows the window.
6. **`--minimized` autostart**: App starts hidden when launched via autostart (`--minimized` flag). User can still show via tray click.
7. **Windows always-on-top trick**: On Windows, `show_window()` uses `set_always_on_top(true/false)` toggle to bring window to front.
8. **Tray has Hide option**: Tray menu includes both "Show" and "Hide" items.
9. **Frontend show_window IPC**: Bridge calls `invoke('show_window')` after initialization to prevent Windows size flashing.

## Non-Goals

- Window position persistence across sessions (neither project does this)
- Window state memory (size is always recalculated from screen)
- Multi-window support

## Version History

| Version | Date | Changes |
|---------|------|---------|
| 1.0 | 2026-02-20 | Initial spec — port from kaitu/client (validated production code) |
