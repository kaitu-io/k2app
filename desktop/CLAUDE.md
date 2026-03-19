# desktop — Tauri v2 Shell

Rust desktop shell using Tauri v2. Serves webapp via tauri-plugin-localhost (port 14580) to avoid WebKit mixed content blocking.

## Commands

```bash
cd src-tauri && cargo check     # Rust compilation check
cd src-tauri && cargo test      # Rust tests (43 tests)
yarn tauri dev                  # Dev mode (expects Vite on :1420)
yarn tauri build --target universal-apple-darwin  # macOS build
yarn tauri build --runner cargo-xwin --target x86_64-pc-windows-msvc  # Windows cross-build from macOS
yarn tauri build --bundles appimage  # Linux AppImage (requires Linux host)
```

## Rust Modules (`src-tauri/src/`)

- **main.rs** — App setup: localhost plugin (14580), single-instance, process, updater, opener, clipboard-manager plugins. Wires tray + service + updater in setup closure. Beta channel: forces debug log level via `channel::is_beta_early()`, chains daemon debug after `ensure_service_running`, starts beta auto-upload. `RunEvent::ExitRequested` handler auto-applies pending updates.
- **service.rs** — k2 daemon lifecycle. macOS dual build: NE mode (`--features ne-mode`) routes to `ne.rs`; daemon mode (default) uses HTTP to :1777 (same as Win/Linux).
  - `daemon_exec`: NE mode → `ne::ne_action()`; daemon mode → `core_action()` HTTP to :1777
  - `get_udid`: NE mode → `ne::get_udid_native()`; daemon mode → daemon HTTP
  - `ensure_service_running`: NE mode → `ne::ensure_ne_installed()`; daemon mode → ping + version check + install
  - `admin_reinstall_service`: NE mode → `ne::admin_reinstall_ne()`; Windows → PowerShell elevated
  - `set_log_level`: IPC command with beta channel check — forces debug when beta. Uses `set_log_level_internal()` (pub, blocking HTTP, reusable by updater/main).
  - All macOS cfg gates: `#[cfg(all(target_os = "macos", feature = "ne-mode"))]`
- **channel.rs** — Update channel persistence (stable/beta). Reads/writes `update-channel` file in app data dir. Two read paths: `get_channel(app)` (runtime, needs `AppHandle`) and `get_channel_early()` (pre-setup, uses `dirs` crate directly). `endpoints_for_channel()` returns stable or beta CDN URLs.
- **ne.rs** — macOS Network Extension bridge (`#[cfg(all(target_os = "macos", feature = "ne-mode"))]`):
  - `ne_action()`: routes up/down/status/version to Swift NE helper via C FFI
  - `ensure_ne_installed()`: installs NE VPN profile via `k2ne_install()`
  - `register_state_callback()`: emits `service-state-changed` + `vpn-status-changed` Tauri events
  - Linked against `libk2_ne_helper.a` (Swift static library) + NetworkExtension framework
- **status_stream.rs** — SSE client for daemon's `GET /api/events` (daemon mode only, not in ne-mode):
  - Maintains persistent SSE connection, auto-reconnects with 3s delay
  - Emits `service-state-changed { available }` on connect/disconnect
  - Emits `vpn-status-changed { ...engine.Status }` on SSE status events
- **tray.rs** — System tray: Show/Hide window, Connect (`action:up`), Disconnect (`action:down`), Quit
- **updater.rs** — Auto-updater: 5s delay → 30min periodic check loop. `UpdateInfo` struct (currentVersion, newVersion, releaseNotes). Emits `update-ready` Tauri event. Windows: NSIS install + `app.exit(0)`. macOS/Linux: store update, apply on exit via `install_pending_update()`. Beta channel: `set_update_channel` saves/restores pre-beta log level, returns `{channel, logLevel}` JSON so JS can update localStorage directly. Downgrade detection: stable channel + beta build → `version_comparator(!=)`.
- **window.rs** — Window management: calculates optimal size from screen dimensions using 9:20 aspect ratio with min/max constraints. Startup creates window hidden, `adjust_window_size()` resizes based on monitor, then `frontend_ready()` shows. Supports `--minimized` autostart (tray-only). `show_window()` uses always-on-top trick on Windows to bring window to front. `hide_window()` minimizes on Windows (keeps taskbar icon) vs hides on macOS/Linux.
- **linux_updater.rs** — Linux-specific tar.gz updater (replaces tauri-plugin-updater on Linux). Fetches `latest.json` from CDN, downloads ~6-8MB tar.gz (vs 85MB AppImage), verifies minisign signatures, extracts to `/opt/kaitu/` via `pkexec` two-phase staging. Relaunches via helper process that polls PID exit then `nohup`. Shares `UpdateInfo`/`UPDATE_READY` state with `updater.rs`. Channel switch triggers auto-apply without user prompt.
- **log_upload.rs** — Service log upload: reads 4 log sources (service, crash, desktop, system), sanitizes sensitive data, gzip compresses, uploads to S3 with `desktop/{version}/{udid}/{date}/logs-{ts}-{id}.tar.gz` key format. Uses `spawn_blocking` for blocking HTTP. Auto-cleans up log files after successful `beta-auto-upload` (delete on macOS/Linux, truncate on Windows due to file locks).

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
| `daemon_exec` (daemon mode) | service | Proxy VPN actions (up/down/status/version) to k2 daemon HTTP |
| `daemon_exec` (NE mode) | ne | VPN actions routed to NE helper via C FFI (`ne_action()`) |
| `daemon_helper_exec` | service | Proxy `adb-*` actions to k2 daemon `/api/helper` (not `/api/core`) |
| `get_platform_info` | service | Returns `{ os, version }` |
| `get_udid` (daemon mode) | service | Returns device UDID from daemon |
| `get_udid` (NE mode) | ne | Hardware UUID via `sysctl -n kern.uuid` (no daemon) |
| `get_pid` | service | Returns k2 daemon PID |
| `ensure_service_running` (daemon mode) | service | Ping + version check + auto-install daemon |
| `ensure_service_running` (NE mode) | ne | Install NE VPN profile via `k2ne_install()` |
| `admin_reinstall_service` (daemon mode) | service | Elevated reinstall via PowerShell |
| `admin_reinstall_service` (NE mode) | ne | Remove + reinstall NE VPN profile |
| `check_update_now` | updater | Manual update check |
| `apply_update_now` | updater | Apply downloaded update |
| `get_update_status` | updater | Returns `UpdateInfo \| null` |
| `set_log_level` | service | Set daemon log level (beta forces debug) |
| `get_update_channel` | updater | Returns current channel ("stable"/"beta") |
| `set_update_channel` | updater | Set channel, accepts `currentLogLevel` for pre-beta save |
| `sync_locale` | tray | Sync locale to system tray |
| `upload_service_log_command` | log_upload | Collect + upload logs to S3 |

## Gotchas

- WebKit blocks HTTPS→HTTP mixed content even for loopback — that's why we use localhost plugin
- `main.rs` is the merge conflict hotspot — every module registers plugins/commands/setup there
- k2 binary must be at `binaries/k2-{arch}-{os}` for Tauri sidecar resolution
- Event permissions require `core:event:default` in capabilities (NOT `event:default`)
- `reqwest::blocking::Client` panics in async context — always wrap in `tokio::task::spawn_blocking()`
- macOS dual build: default = daemon mode (no NE, same as Win/Linux); `--features ne-mode` = NE mode (gomobile + sysext + NE helper). `cfg(all(target_os = "macos", feature = "ne-mode"))` gates all NE code.
- NE helper uses DispatchSemaphore — C FFI functions in ne.rs must NOT be called from the main thread (use `tokio::task::spawn_blocking`)
- NE appex must be codesigned with the same identity as the main app, with a separate entitlements file that includes `com.apple.developer.networking.networkextension`
- SSE status stream (`status_stream.rs`) emits Tauri events: `service-state-changed` (SSE connection state) + `vpn-status-changed` (VPN status from SSE). Used by webapp VPN store for event-driven updates instead of 2s polling.
- `get_channel_early()` uses `dirs::data_dir().join("io.kaitu.desktop")` to read channel BEFORE `AppHandle` exists (log plugin is configured in builder chain before `.setup()`). Path must match Tauri's `app_data_dir()` on all platforms.
- Missing `#[tauri::command]` registration in `tauri::generate_handler![]` causes white screen — first `invoke()` fails silently, React never renders.
- Windows updater: `update.install()` launches NSIS as child process, must call `app.exit(0)` immediately — NSIS needs old process to exit to overwrite binaries.
- Beta channel forces ALL 3 log layers to debug: desktop log (tauri-plugin-log via `is_beta_early()`), daemon log (HTTP via `set_log_level_internal`), engine log (`buildConnectConfig()` in webapp). User cannot override while on beta.
- Pre-beta log level stored in `{app_data_dir}/pre-beta-log-level` file. Frontend passes `currentLogLevel` (from localStorage) when calling `set_update_channel` IPC since Rust cannot read browser localStorage.
- Windows Authenticode signing requires intermediate CA chain: `osslsigncode` must use `-ac scripts/ci/macos/certum-chain.pem` (Certum Code Signing 2021 CA). Without it, Windows UAC shows "Publisher: Unknown" because it can't trace Wordgate LLC cert to a trusted root. SimplySign PKCS#11 token must be logged in first (`make simplisign-login`).
- Windows cross-build from macOS: requires `cargo-xwin`, `makensis`, `osslsigncode`, `libp11`. See `docs/plans/2026-03-11-windows-build-on-macos.md` for full setup.
