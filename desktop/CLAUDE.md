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
- **service.rs** — k2 daemon lifecycle (Windows/Linux only at runtime; macOS routes to ne.rs via `#[cfg]`):
  - `daemon_exec`: macOS → `ne::ne_action()`; Windows/Linux → `core_action()` HTTP to :1777
  - `get_udid`: macOS → `ne::get_udid_native()`; others → daemon HTTP `/api/device/udid`
  - `ensure_service_running`: macOS → `ne::ensure_ne_installed()`; others → daemon lifecycle (ping → version check → install)
  - `admin_reinstall_service`: macOS → `ne::admin_reinstall_ne()`; Windows → PowerShell elevated
  - `detect_old_kaitu_service()` / `cleanup_old_kaitu_service()`: upgrade cleanup for non-macOS paths
  - `versions_match()`: strips build metadata after `+` for semver comparison
  - k2 daemon API (non-macOS): `POST http://127.0.0.1:1777/api/core` with `{"action":"...","params":{...}}`
- **ne.rs** — macOS Network Extension bridge (`#[cfg(target_os = "macos")]` only):
  - `ne_action()`: routes up/down/status/version to Swift NE helper via C FFI
  - `ensure_ne_installed()`: replaces `ensure_service_running` on macOS — installs NE VPN profile via `k2ne_install()`
  - `get_udid_native()`: hardware UUID via `sysctl -n kern.uuid` (no daemon required)
  - `admin_reinstall_ne()`: removes + reinstalls NE VPN profile
  - `register_state_callback()`: NEVPNStatusDidChange → Tauri `ne-state-changed` event
  - Linked against `libk2_ne_helper.a` (Swift static library) + NetworkExtension framework
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
| `daemon_exec` (non-macOS) | service | Proxy VPN actions (up/down/status/version) to k2 daemon HTTP |
| `daemon_exec` (macOS) | ne | VPN actions routed to NE helper via C FFI (`ne_action()`) |
| `get_platform_info` | service | Returns `{ os, version }` |
| `get_udid` (non-macOS) | service | Returns device UDID from daemon |
| `get_udid` (macOS) | ne | Hardware UUID via `sysctl -n kern.uuid` (no daemon) |
| `get_pid` | service | Returns k2 daemon PID |
| `ensure_service_running` (non-macOS) | service | Ping + version check + auto-install daemon |
| `ensure_service_running` (macOS) | ne | Install NE VPN profile via `k2ne_install()` |
| `admin_reinstall_service` (non-macOS) | service | Elevated reinstall via PowerShell |
| `admin_reinstall_service` (macOS) | ne | Remove + reinstall NE VPN profile |
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
- macOS NE mode: no k2 daemon process — VPN managed via NEVPNManager in NE App Extension; `#[cfg(target_os = "macos")]` gates in service.rs route all IPC to ne.rs
- NE helper uses DispatchSemaphore — C FFI functions in ne.rs must NOT be called from the main thread (use `tokio::task::spawn_blocking`)
- NE appex must be codesigned with the same identity as the main app, with a separate entitlements file that includes `com.apple.developer.networking.networkextension`
