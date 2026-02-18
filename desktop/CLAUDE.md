# desktop — Tauri v2 Shell

Rust desktop shell using Tauri v2. Serves webapp via tauri-plugin-localhost (port 14580) to avoid WebKit mixed content blocking.

## Commands

```bash
cd src-tauri && cargo check     # Rust compilation check
cd src-tauri && cargo test      # Rust tests (14 tests)
yarn tauri dev                  # Dev mode (expects Vite on :1420)
yarn tauri build --target universal-apple-darwin  # macOS build
```

## Rust Modules (`src-tauri/src/`)

- **main.rs** — App setup: localhost plugin (14580), single-instance, process, updater, opener, clipboard-manager plugins. Wires tray + service + updater in setup closure. `RunEvent::ExitRequested` handler auto-applies pending updates.
- **service.rs** — k2 daemon lifecycle:
  - `ensure_service_running(app_version)`: ping → version check → install if needed
  - `admin_reinstall_service()`: elevated install via osascript (macOS) / PowerShell (Windows)
  - `detect_old_kaitu_service()` / `remove_old_kaitu_service()`: upgrade cleanup
  - `versions_match()`: strips build metadata after `+` for semver comparison
  - k2 daemon API: `POST http://127.0.0.1:1777/api/core` with `{"action":"...","params":{...}}`
- **tray.rs** — System tray: Show/Hide window, Connect (`action:up`), Disconnect (`action:down`), Quit
- **updater.rs** — Auto-updater: 5s delay → 30min periodic check loop. `UpdateInfo` struct (currentVersion, newVersion, releaseNotes). Emits `update-ready` Tauri event. Windows: NSIS install + `app.exit(0)`. macOS/Linux: store update, apply on exit via `install_pending_update()`.
- **log_upload.rs** — Service log upload: reads 4 log sources (service, crash, desktop, system), sanitizes sensitive data, gzip compresses, uploads to S3, notifies Slack. Uses `spawn_blocking` for blocking HTTP.

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

## IPC Commands (JS → Rust)

| Command | Module | Purpose |
|---------|--------|---------|
| `daemon_exec` | service | Proxy VPN actions (up/down/status/version) to k2 daemon |
| `get_platform_info` | service | Returns `{ os, version }` |
| `get_udid` | service | Returns device UDID from daemon |
| `get_pid` | service | Returns k2 daemon PID |
| `ensure_service_running` | service | Ping + version check + auto-install |
| `admin_reinstall_service` | service | Elevated reinstall (osascript/PowerShell) |
| `check_update_now` | updater | Manual update check |
| `apply_update_now` | updater | Apply downloaded update |
| `get_update_status` | updater | Returns `UpdateInfo \| null` |
| `sync_locale` | tray | Sync locale to system tray |
| `upload_service_log_command` | log_upload | Collect + upload logs to S3 |

## Gotchas

- WebKit blocks HTTPS→HTTP mixed content even for loopback — that's why we use localhost plugin
- `main.rs` is the merge conflict hotspot — every module registers plugins/commands/setup there
- k2 binary must be at `binaries/k2-{arch}-{os}` for Tauri sidecar resolution
- Event permissions require `core:event:default` in capabilities (NOT `event:default`)
- `reqwest::blocking::Client` panics in async context — always wrap in `tokio::task::spawn_blocking()`
- Windows updater: `update.install()` launches NSIS as child process, must call `app.exit(0)` immediately
