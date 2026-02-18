# desktop — Tauri v2 Shell

Rust desktop shell using Tauri v2. Serves webapp via tauri-plugin-localhost (port 14580) to avoid WebKit mixed content blocking.

## Commands

```bash
cd src-tauri && cargo check     # Rust compilation check
cd src-tauri && cargo test      # Rust tests (4 tests)
yarn tauri dev                  # Dev mode (expects Vite on :1420)
yarn tauri build --target universal-apple-darwin  # macOS build
```

## Rust Modules (`src-tauri/src/`)

- **main.rs** — App setup: localhost plugin (14580), single-instance, process, updater plugins. Wires tray + service + updater in setup closure.
- **service.rs** — k2 daemon lifecycle:
  - `ensure_service_running(app_version)`: ping → version check → install if needed
  - `admin_reinstall_service()`: elevated install via osascript (macOS) / PowerShell (Windows)
  - `detect_old_kaitu_service()` / `remove_old_kaitu_service()`: upgrade cleanup
  - `versions_match()`: strips build metadata after `+` for semver comparison
  - k2 daemon API: `POST http://127.0.0.1:1777/api/core` with `{"action":"...","params":{...}}`
- **tray.rs** — System tray: Show/Hide window, Connect (`action:up`), Disconnect (`action:down`), Quit
- **updater.rs** — Auto-updater: 5s startup check, `check_update_now()`, `apply_update_now()`, `get_update_status()` IPC commands

## Tauri Config (`src-tauri/tauri.conf.json`)

- Window: 430×956 (mobile-like), non-maximizable, hidden title bar
- Bundle: DMG (macOS) + NSIS (Windows), code signing configured
- Updater: CloudFront endpoints with minisign public key
- Version: `"../../package.json"` (references root, single source of truth)
- Identifier: `io.kaitu.desktop`

## Plugins

- `tauri-plugin-localhost` — HTTP serving to avoid mixed content
- `tauri-plugin-single-instance` — Show + focus existing window
- `tauri-plugin-updater` — Auto-update with CDN endpoints
- `tauri-plugin-process` — Process management
- `tauri-plugin-autostart` — Launch on system boot
- `tauri-plugin-opener` — Open external URLs in system browser
- `tauri-plugin-clipboard-manager` — Read/write system clipboard

## Gotchas

- WebKit blocks HTTPS→HTTP mixed content even for loopback — that's why we use localhost plugin
- `main.rs` is the merge conflict hotspot — every module registers plugins/commands/setup there
- k2 binary must be at `binaries/k2-{arch}-{os}` for Tauri sidecar resolution
- 4 `dead_code` warnings expected (exit/tray functions not yet wired to all callers)
