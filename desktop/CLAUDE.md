# desktop — Tauri v2 Shell (macOS + Windows only)

Rust desktop shell using Tauri v2. Serves webapp via tauri-plugin-localhost (port 14580) to avoid WebKit mixed content blocking.

**Linux is NOT supported by this shell.** Linux desktop ships a single Go
binary from `cmd/k2` with the React webapp embedded via `k2/webui`; users
open `http://127.0.0.1:1777` in their browser after running
`packaging/linux/install.sh`. See root `CLAUDE.md` and `k2/webui/CLAUDE.md`.

## Commands

```bash
cd src-tauri && cargo check     # Rust compilation check
cd src-tauri && cargo test      # Rust tests
yarn tauri dev                  # Dev mode (expects Vite on :1420)
yarn tauri build --target universal-apple-darwin  # macOS build
yarn tauri build --runner cargo-xwin --target x86_64-pc-windows-msvc  # Windows cross-build from macOS
```

## Rust Modules (`src-tauri/src/`)

- **main.rs** — App setup: localhost plugin (14580), single-instance, process, updater, opener, clipboard-manager plugins. Wires tray + service + updater in setup closure. `RunEvent::ExitRequested` handler auto-applies pending updates.
- **service.rs** — k2 daemon lifecycle. Routes VPN actions to the k2 daemon HTTP API at `:1777` on all platforms.
  - `daemon_exec`: HTTP to `:1777/api/core`
  - `ensure_service_running`: ping + version check + auto-install daemon (osascript on macOS, PowerShell elevated on Windows)
  - `admin_reinstall_service`: elevated reinstall (osascript on macOS, PowerShell on Windows)
  - `set_log_level`: IPC command with beta channel check — forces debug when beta. Uses `set_log_level_internal()` (pub, blocking HTTP, reusable by updater/main).
- **channel.rs** — Update channel persistence (stable/beta). Reads/writes `update-channel` file in app data dir via `get_channel(app)`. `endpoints_for_channel()` returns stable or beta CDN URLs.
- **status_stream.rs** — SSE client for daemon's `GET /api/events`:
  - Maintains persistent SSE connection, auto-reconnects with 3s delay
  - Emits `service-state-changed { available }` on connect/disconnect
  - Emits `vpn-status-changed { ...engine.Status }` on SSE status events
- **tray.rs** — System tray: Show/Hide window, Connect (`action:up`), Disconnect (`action:down`), Quit
- **updater.rs** — Auto-updater: 5s delay → 30min periodic check loop. `UpdateInfo` struct (currentVersion, newVersion, releaseNotes). Emits `update-ready` Tauri event. Windows: NSIS install + `app.exit(0)`. macOS/Linux: store update, apply on exit via `install_pending_update()`. Beta channel: `set_update_channel` saves/restores pre-beta log level, returns `{channel, logLevel}` JSON so JS can update localStorage directly. Downgrade detection: stable channel + beta build → `version_comparator(!=)`.
- **window.rs** — Window management: calculates optimal size from screen dimensions using 9:20 aspect ratio with min/max constraints. Startup creates window hidden, `adjust_window_size()` resizes based on monitor, then `frontend_ready()` shows. Supports `--minimized` autostart (tray-only). `show_window()` uses always-on-top trick on Windows to bring window to front. `hide_window()` minimizes on Windows (keeps taskbar icon) vs hides on macOS/Linux.
- **storage.rs** — App-private key-value storage. Persists `storage.json` in Tauri app data dir. In-memory `HashMap` mirror with atomic write (write `.tmp` then `fs::rename`). Single-instance plugin guarantees no concurrent writers. Used by webapp for secure storage on desktop (IPlatform.storage). Values encrypted with AES-256-GCM via `storage_crypto.rs`; reads auto-detect `ENC1:` prefix for backward compat with plaintext.
- **storage_crypto.rs** — AES-256-GCM encryption for storage values. Key derived via HKDF-SHA256 from platform hardware ID (macOS: IOPlatformUUID via `ioreg`, Windows: Registry `HKLM\SOFTWARE\Microsoft\Cryptography\MachineGuid`, Linux: `/etc/machine-id`). Encrypted values prefixed with `ENC1:`. Plaintext values (pre-encryption) read transparently for backward compatibility. Platform-specific hardware ID tests gated with `#[cfg(target_os)]`.
- **log_upload.rs** — Service log upload: reads 4 log sources (service, crash, desktop, system), sanitizes sensitive data, gzip compresses, uploads to S3 with `desktop/{version}/{udid}/{date}/logs-{ts}-{id}.tar.gz` key format. Uses `spawn_blocking` for blocking HTTP. Auto-cleans up log files after successful `beta-auto-upload` (delete on macOS/Linux, truncate on Windows due to file locks).
- **app_list.rs** — `list_running_processes` command: running GUI app process names for the App Bypass page (macOS/Windows).
- **installed_apps.rs** — `list_installed_apps` command: installed apps (`id`, `label`, `processNames`, `icon_url`) for the App Bypass page.
- **icon_protocol.rs** — Registers the `kaitu-icon://` URI scheme (`handle_kaitu_icon`) serving per-app icons to the App Bypass UI. macOS renders via NSWorkspace + NSBitmapImageRep → PNG; Windows is a v1 stub (404).

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
| `daemon_exec` | service | Proxy VPN actions (up/down/status/version) to k2 daemon HTTP |
| `daemon_helper_exec` | service | Proxy `adb-*` actions to k2 daemon `/api/helper` (not `/api/core`) |
| `get_platform_info` | service | Returns `{ os, version }` |
| `get_pid` | service | Returns k2 daemon PID |
| `ensure_service_running` | service | Ping + version check + auto-install daemon |
| `admin_reinstall_service` | service | Elevated reinstall (osascript on macOS, PowerShell on Windows) |
| `check_update_now` | updater | Manual update check |
| `apply_update_now` | updater | Apply downloaded update |
| `get_update_status` | updater | Returns `UpdateInfo \| null` |
| `set_log_level` | service | Set daemon log level (beta forces debug) |
| `get_update_channel` | updater | Returns current channel ("stable"/"beta") |
| `set_update_channel` | updater | Set channel, accepts `currentLogLevel` for pre-beta save |
| `storage_get` | storage | Get value by key from app storage |
| `storage_set` | storage | Set key-value pair in app storage |
| `storage_remove` | storage | Remove key from app storage |
| `sync_locale` | tray | Sync locale to system tray |
| `upload_service_log_command` | log_upload | Collect + upload logs to S3 |
| `set_dev_enabled` | service | Toggle WebView devtools inspection |
| `list_running_processes` | app_list | Running GUI app process names (App Bypass) |
| `list_installed_apps` | installed_apps | Installed apps for App Bypass (id/label/processNames/icon_url) |

## Gotchas

- WebKit blocks HTTPS→HTTP mixed content even for loopback — that's why we use localhost plugin
- `main.rs` is the merge conflict hotspot — every module registers plugins/commands/setup there
- k2 binary must be at `binaries/k2-{arch}-{os}` for Tauri sidecar resolution
- Event permissions require `core:event:default` in capabilities (NOT `event:default`)
- `reqwest::blocking::Client` panics in async context — always wrap in `tokio::task::spawn_blocking()`
- SSE status stream (`status_stream.rs`) emits Tauri events: `service-state-changed` (SSE connection state) + `vpn-status-changed` (VPN status from SSE). Used by webapp VPN store for event-driven updates instead of 2s polling.
- Missing `#[tauri::command]` registration in `tauri::generate_handler![]` causes white screen — first `invoke()` fails silently, React never renders.
- Windows updater: `update.install()` launches NSIS as child process, must call `app.exit(0)` immediately — NSIS needs old process to exit to overwrite binaries.
- Beta channel forces ALL 3 log layers to debug: desktop log (tauri-plugin-log), daemon log (HTTP via `set_log_level_internal`), engine log (`buildConnectConfig()` in webapp). User cannot override while on beta.
- Pre-beta log level stored in `{app_data_dir}/pre-beta-log-level` file. Frontend passes `currentLogLevel` (from localStorage) when calling `set_update_channel` IPC since Rust cannot read browser localStorage.
- Windows Authenticode signing requires intermediate CA chain: `osslsigncode` must use `-ac scripts/ci/macos/certum-chain.pem` (Certum Code Signing 2021 CA). Without it, Windows UAC shows "Publisher: Unknown" because it can't trace Wordgate LLC cert to a trusted root. SimplySign PKCS#11 token must be logged in first (`make simplisign-login`).
- Windows cross-build from macOS: requires `cargo-xwin`, `makensis`, `osslsigncode`, `libp11`. See `docs/plans/2026-03-11-windows-build-on-macos.md` for full setup.

## Storage Encryption (`storage_crypto.rs`)

Desktop `storage.json` values encrypted with AES-256-GCM. Key derived via HKDF-SHA256 from the `machine-uid` crate's firmware-level ID:

- **macOS**: `ioreg IOPlatformUUID`
- **Windows**: registry `HKLM\SOFTWARE\Microsoft\Cryptography\MachineGuid`
- **Linux**: `/var/lib/dbus/machine-id` → `/etc/machine-id`

**Not** `sysctl kern.uuid` — that is a UUIDv3 derived from hostname and collided in v0.4.0 (see commit `d4ebdd6`).

`ENC1:` prefix marks encrypted values; plaintext read transparently for backward compat. MCP Go (`mcp/storage_crypto.go`) reimplements the same crypto with shared test vectors for read-only session sharing.

**Threat model**: scope is 落盘混淆 + 硬件绑定, **not** anti-local-attacker. See the `storage_crypto.rs` module doc for details.

## macOS PKG Install Order

Preinstall runs the OLD binary, postinstall runs the NEW. Always `launchctl unload` before overwriting plist; otherwise the old process keeps the binary locked and the install silently leaves the old one running.

## Artifact Naming

`Kaitu_{VERSION}_{ARCH}.{EXT}` — underscore-separated.

- macOS: `_universal.pkg` / `_universal.app.tar.gz` / `.sig`
- Windows: `_x64.exe` / `.sig`
- S3 path: `kaitu/desktop/{VERSION}/`

Never use hyphen separator (`Kaitu-`) or `-setup` suffix.

## Root Daemon adb Discovery

Daemon runs as root on macOS → different `$PATH` and `$HOME` from the user. `findAdbCandidates()` scans all `/Users/*/Library/Android/sdk/` and Homebrew paths before falling back to CDN download. Uses `gadb` (pure Go ADB TCP client) for device ops — no external `adb` dependency at runtime.

## S3 Log Upload (Desktop)

- **Feedback upload**: bundle tar.gz with unique feedbackId key: `desktop/{version}/{udid}/{date}/logs-{ts}-{id}.tar.gz`
- **Beta auto-upload** (desktop only): per-file PUT to `auto/{udid}/{filename}`. Active `.log` files overwrite (latest snapshot). Rotated `.log.gz` files use HEAD check to skip if already uploaded.
- Legacy `service-logs/` / `feedback-logs/` prefixes still supported by Lambda.
- Upload modules are read-only — never truncate source log files.
