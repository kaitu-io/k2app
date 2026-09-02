# desktop — Tauri v2 Shell (macOS + Windows only)

Rust desktop shell using Tauri v2. UI loads via the `kaitu-ui://` custom protocol (web-ota bundle from disk, embedded assets as fallback).

**Linux is NOT supported by this shell.** Linux desktop ships a single Go
binary from `cmd/k2` with the React webapp embedded via `k2/webui`; users
open `http://127.0.0.1:1777` in their browser after running
`packaging/linux/install.sh`. Install flow: root `CLAUDE.md` "Cross-Layer Conventions".
(`k2/webui/CLAUDE.md` covers the `//go:embed` package itself — not the installer.)

## Commands

```bash
cd src-tauri && cargo check     # Rust compilation check
cd src-tauri && cargo test      # Rust tests
yarn tauri dev                  # Dev mode (expects Vite on :1420)
yarn tauri build --target universal-apple-darwin  # bare Tauri macOS build; the shipped .pkg comes from `make build-macos` (scripts/build-macos.sh)
yarn tauri build --runner cargo-xwin --target x86_64-pc-windows-msvc  # Windows cross-build from macOS (shipped path: `make build-windows`)
```

## Brand (双品牌: kaitu / overleap)

Build-time 烘焙，与 webapp 同一契约：`BRAND=overleap make build-macos`（Makefile
`BRAND ?= kaitu` → `export K2_BRAND`）。三层生效：webapp dist（`__K2_BRAND__`）、
Rust `cfg(brand_overleap)`（build.rs 发 cfg；updater 端点（channel.rs）/ 桌面日志目录（main.rs）/
updater 回退路径（updater.rs）编译期分叉——另一品牌 URL 不进二进制）、Tauri
`--config src-tauri/tauri.conf.overleap.json`（productName/identifier/icon/entitlements/
updater endpoints；**合并时数组整体替换，overlay 里数组字段必须写全量**）。

- 产物命名 `Overleap_{VERSION}_{ARCH}.{EXT}`；S3/CDN 路径段 `/overleap/desktop/`；
  latest.json 双份、独立 GitHub Release tag `overleap-v{VERSION}`
  （`scripts/publish-desktop.sh --brand=overleap`）。
- **k2 daemon 品牌中立**：1777 端口、launchd label `kaitu`（`k2/cmd/k2/service_darwin.go`）、
  NSIS `SERVICE_NAME "kaitu"` 不随品牌变。两品牌同机共存 = last-install-wins 接管
  daemon，双方 app 都能控制 VPN。
- 纯度守卫 `scripts/check-desktop-brand-purity.sh <brand> <path>`：webapp 资源 +
  二进制 strings（k2 sidecar 与裸 `kaitu` 内部 token——`kaitu-icon://` scheme、HKDF
  盐、S3 桶、上面的 service name——豁免）；`.app.tar.gz` 目标额外按**内容**（`tar tzf`
  顶层 `.app` 目录名）而非文件名校验——两品牌共享同一 Tauri bundle 目录，naive
  `find *.app.tar.gz | head -1` 式收集会按字母序（Kaitu < Overleap）拿到上一次 kaitu
  构建残留的 tar.gz，产物文件名对但内容是另一品牌（曾实际发生，见 commit
  `813bf3f5`）。`build-macos.sh` 现在按品牌精确路径收集，且收集后立即跑这道门。webapp payload
  被 brotli（macOS 二进制）/ LZMA（NSIS）压缩后对 `strings` 不可见——webapp 品牌纯度真正的门是
  打包前的 `webapp/scripts/check-brand-purity.sh` dist 检查。
- 递归 `make` 只透传 `K2_BRAND`（导出的 env），不透传 `BRAND`（make 变量本身）——脚本里
  任何裸 `make build-webapp` 调用都会被子 make 进程自己的 `BRAND ?= kaitu` 吃掉、
  静默改回 kaitu，必须写成 `make build-webapp BRAND=$BRAND`（见 commit `dd2d8608`）。
- `desktop/src-tauri/installer-hooks.overleap.nsh` 是构建期由 Makefile 对
  `installer-hooks.nsh` 做 sed 替换（`io.kaitu.desktop` → `io.overleap.desktop`）生成的
  派生副本——仅当 `BRAND=overleap` 才生成，gitignored，永不手改。
- 签名主体（Developer ID / SimplySign / notarize 账号）与 updater minisign 密钥两品牌
  共用——已拍板的品牌泄漏点，不再讨论。
- overleap 图标是占位紫（`yarn tauri icon` 重跑即换正式稿）；NSIS 语言 overleap 仅 English。
- 图像资产（logo PNG 等二进制）不在任何纯度守卫覆盖范围内——`strings`/`grep` 抓不到位图里的文字；
  靠发布前人工视觉 smoke 把关，不是自动化门。

## Rust Modules (`src-tauri/src/`)

- **main.rs** — App setup: panic hook (appends to desktop.log — see gotcha below), then plugins with single-instance **first** (plugins initialize in registration order; the duplicate-instance `exit(0)` must precede all other side effects). Wires tray + service + updater in setup closure. `RunEvent::ExitRequested` → `service::stop_vpn()` (sends `down`, once) then auto-applies pending updates; `CloseRequested` on `main` is intercepted → `hide_window` (only Quit exits); `ScaleFactorChanged` → `window::reclamp_min_size`.
- **service.rs** — k2 daemon lifecycle. Routes VPN actions to the k2 daemon HTTP API at `:1777` on all platforms.
  - `daemon_exec`: HTTP to `:1777/api/core`
  - `ensure_service_running`: phase 1 `cleanup_old_kaitu_service()` (legacy `kaitu-service` plist / SCM entry) + poll `action:version` up to 15 s; phase 2 elevated `k2 service install` (osascript on macOS, PowerShell `-Verb RunAs` on Windows) only if phase 1 fails; phase 3 re-poll 5 s — actual service state is the sole arbiter, a reported install error is diagnostics only.
  - `admin_reinstall_service`: elevated reinstall (osascript on macOS, PowerShell on Windows)
  - `set_log_level`: IPC command with beta channel check — forces debug when beta. Uses `set_log_level_internal()` (pub, blocking HTTP, reusable by updater/main).
- **channel.rs** — Update channel persistence (stable/beta). Reads/writes `update-channel` file in app data dir via `get_channel(app)`. `endpoints_for_channel()` returns stable or beta CDN URLs.
- **status_stream.rs** — SSE client for daemon's `GET /api/events`:
  - Maintains persistent SSE connection, auto-reconnects with 3s delay
  - Emits `service-state-changed { available }` on connect/disconnect
  - Emits `vpn-status-changed { ...engine.Status }` on SSE status events
- **tray.rs** — System tray: menu is Show / Hide / Quit only (`build_tray_menu`, re-localised via `sync_locale`); Quit calls `service::stop_vpn()` then `app.exit(0)`; left click shows the window. There are no connect/disconnect items.
- **updater.rs** — Auto-updater: 5s delay → 30min periodic check loop. `UpdateInfo` struct (currentVersion, newVersion, releaseNotes). Emits `update-ready` Tauri event. Windows: NSIS install + `app.exit(0)`. macOS: store update, apply on exit via `install_pending_update()` (= `app.restart()`). Beta channel: `set_update_channel` saves/restores pre-beta log level, returns `{channel, logLevel}` JSON so JS can update localStorage directly. Downgrade detection: stable channel + beta build → `version_comparator(!=)`. A channel switch runs a forced check; on macOS a successful channel-switch install restarts through a `sh -c` helper that waits for the old PID and `open`s the bundle (`restart_after_channel_switch`). Installing a `-beta` build activates the beta channel unconditionally at startup (`main.rs`).
- **window.rs** — Window management: calculates optimal size from screen dimensions using 9:20 aspect ratio with min/max constraints. Startup creates the window hidden (`visible: false`); `main.rs` setup calls `adjust_window_size()` then `show_window()` immediately — **before** `web_ota::prepare_boot` navigates; there is no `frontend_ready` handshake (comments naming it are stale). Supports `--minimized` autostart (tray-only). `show_window()` uses always-on-top trick on Windows to bring window to front. `hide_window()` minimizes on Windows (keeps taskbar icon) vs hides on macOS/Linux.
- **storage.rs** — App-private key-value storage. Persists `storage.json` in Tauri app data dir. In-memory `HashMap` mirror with atomic write (write `.tmp` then `fs::rename`). Single-instance plugin guarantees no concurrent writers. Used by webapp for secure storage on desktop (IPlatform.storage). Values encrypted with AES-256-GCM via `storage_crypto.rs`; reads auto-detect `ENC1:` prefix for backward compat with plaintext.
- **storage_crypto.rs** — AES-256-GCM for storage values; key = HKDF-SHA256 of the `machine-uid` crate's hardware ID; `ENC1:` prefix, plaintext read transparently. Sources, history and threat model: "Storage Encryption" below.
- **log_upload.rs** — Log upload (runs in Tauri, not the daemon, so it works when the daemon is dead): stages `k2*.log` / `.log.gz` / `panic-*.log` from both the root and user daemon log dirs, `desktop*.log`, macOS `log show` output, and the Windows NSIS installer diagnostics (`install-diag.log`, `kaitu-preinstall.log`); sanitizes, tar.gz, uploads to S3 `desktop/{version}/{udid}/{date}/logs-{ts}-{id}.tar.gz`. Reason `beta-auto-upload` switches to per-file `auto/{udid}/{name}` PUTs. Uses `spawn_blocking`. **Read-only**: it never deletes or truncates a source log — only its own staging/tmp files.
- **app_list.rs** — `list_running_processes` command: running user-facing apps (macOS: NSWorkspace `runningApplications` with child PIDs grouped under the owning `.app` via libproc; Windows: sysinfo process list, `id` = exe path). App Bypass **supplement** — see "App Bypass app lists".
- **installed_apps.rs** — `list_installed_apps` command: installed apps `{id, label, processNames, iconUrl, installerPackageName?}` (camelCase serde). macOS: Info.plist scan of `/Applications`, `/System/Applications`, `~/Applications` incl. nested helper bundles; Windows: registry Uninstall scan. App Bypass **primary** list — see "App Bypass app lists".
- **icon_protocol.rs** — Registers the `kaitu-icon://` URI scheme (`handle_kaitu_icon`) serving per-app icons to the App Bypass UI. macOS renders via NSWorkspace + NSBitmapImageRep → PNG; Windows is a v1 stub (404).
- **web_ota.rs** — Web OTA: polls `{CDN}/{brand}/web[/beta]/latest.json` (5s + 30min, channel-aware), gates on `sig`(mandatory minisign, updater pubkey)/`min_desktop`/`min_bridge`/`isNewerVersion`, downloads+verifies web.zip into `$APPDATA/web-ota/pending/`, atomically swaps pending→current (current→previous). `.boot-pending` marker + `startup_rollback()` quarantine failed bundles. `DESKTOP_BRIDGE_VERSION` compile-time constant. Debug builds no-op (dev keeps Vite devUrl). `K2_WEB_OTA_BASE` env overrides endpoints for UAT (signature gates still enforced).
- **ui_protocol.rs** — Registers `kaitu-ui://` (page origin `kaitu-ui://localhost` on macOS, `http://kaitu-ui.localhost` on Windows): serves the web-ota `current/` bundle from disk, falls back to embedded assets via `asset_resolver`, MIME by extension, SPA fallback for extensionless routes. Scheme name is brand-neutral (kaitu-icon precedent).
- **storage_migration.rs** — One-time origin migration: unmigrated startup boots the embedded UI at the legacy tauri:// origin with `?migrate=export`; webapp dumps localStorage via `storage_migration_put/done`; `storage-migrated` marker (app data root) + 20s watchdog fallback. Desktop auth lives in storage.json (origin-independent) — migration carries preferences/caches, not login state.

## Tauri Config (`src-tauri/tauri.conf.json`)

- Window: 430×956 (mobile-like), non-maximizable, hidden title bar
- Bundle: `targets: ["app", "nsis"]` — no DMG. The macOS installer is a `.pkg` built by `scripts/build-macos.sh` (`pkgbuild` + `productsign` + notarize) from the `.app`, with `scripts/pkg-scripts/{preinstall,postinstall}`; `createUpdaterArtifacts` yields the `.app.tar.gz` + `.sig`. NSIS is `perMachine` with `installerHooks: installer-hooks.nsh`. `csp: null`, `withGlobalTauri: true`.
- Updater: CloudFront endpoints with minisign public key
- Version: `"../../package.json"` (references root, single source of truth)
- Identifier: `io.kaitu.desktop`

## Plugins

- `tauri-plugin-single-instance` — Show + focus existing window (**must be registered first**, see Gotchas)
- `tauri-plugin-log` — `desktop.log` in the per-brand log dir (`~/Library/Logs/{kaitu|overleap}` / `%LOCALAPPDATA%\{brand}\logs`), 20 MB `KeepOne`, level fixed at compile time by `K2_BUILD_LOG_LEVEL`, `reqwest` targets filtered out
- `tauri-plugin-updater` — Auto-update with CDN endpoints
- `tauri-plugin-process` — Process management
- `tauri-plugin-autostart` — Launch on system boot
- `tauri-plugin-opener` — Open external URLs in system browser
- `tauri-plugin-clipboard-manager` — Read/write system clipboard
- `tauri-plugin-mcp-bridge` — cargo feature `mcp-bridge` only (tauri-mcp dev bridge); `build.rs` writes/removes the gitignored `capabilities/mcp-bridge.json`; `make build-macos-test` builds with `--features=mcp-bridge`

## IPC Commands (JS → Rust)

| Command | Module | Purpose |
|---------|--------|---------|
| `show_window` / `hide_window` | main | Show (unminimize + focus) / hide the main window — same fns the tray and single-instance callback use |
| `daemon_exec` | service | Proxy VPN actions (up/down/status/version) to k2 daemon HTTP |
| `daemon_helper_exec` | service | Proxy `adb-*` actions to k2 daemon `/api/helper` (not `/api/core`) |
| `get_platform_info` | service | Returns `{ os, version }` |
| `get_pid` | service | Returns k2 daemon PID |
| `ensure_service_running` | service | Legacy-service cleanup + version poll + elevated install + post-install verify (see service.rs) |
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
| `list_running_processes` | app_list | Running apps — App Bypass **supplement** (see "App Bypass app lists") |
| `list_installed_apps` | installed_apps | Installed apps `{id,label,processNames,iconUrl,installerPackageName?}` — App Bypass **primary** list |
| `router_http_request` | router_bridge | SSRF-gated HTTP to a LAN k2r router (`http://` + private IPv4 only, no redirects) |
| `get_default_gateway` | router_bridge | Physical-interface default gateway, excluding TUN (unconsumed — see "Router LAN Bridge") |
| `ui_boot_ok` | web_ota | Webapp boot confirmation — clears `.boot-pending` rollback marker |
| `storage_migration_put` | storage_migration | Store localStorage snapshot (legacy-origin export page) |
| `storage_migration_get` | storage_migration | Read stored snapshot (new-origin import) |
| `storage_migration_clear` | storage_migration | Delete stored snapshot after import |
| `storage_migration_done` | storage_migration | Mark migrated + navigate window to `kaitu-ui://` |

## App Bypass app lists (`installed_apps.rs` + `app_list.rs`)

`list_installed_apps` is the **primary** list; `list_running_processes` is a supplement the webapp folds in (`AppBypass.tsx` `foldRunningIntoInstalled`; the `_platform.appList` contract is in `webapp/CLAUDE.md`). Running rows are only fetched when both enumerators exist.

- **Windows fold**: `installed.id` is the install *directory*, `running.id` is the exe *path*; a running exe under an installed dir extends that app's `processNames` and drops out of the "more — running" section. **macOS** ids differ in kind (bundle path vs bundle id), so the supplement is deduped by process name instead.
- **Windows scan** (`windows::scan_hive`): HKLM in both WOW64 views (`KEY_WOW64_64KEY` + `KEY_WOW64_32KEY` — 32-bit NSIS installers such as WeChat 4.x live under `WOW6432Node`) plus HKCU; skips `SystemComponent=1` / `ParentKeyName` entries. Install dir = first of `InstallLocation` → `DisplayIcon` dir → `UninstallString` dir that passes `is_unsafe_install_dir` (refuses `C:\Windows*`, drive roots, bare `Program Files` / `(x86)` / `ProgramData` / `Users` / `WindowsApps`, UNC/relative). `collect_exes` walks depth ≤ 3 skipping `unins*`; `supplemental_process_names` pins WeChat's out-of-tree `WeChatAppEx.exe` family by Uninstall key name (`Weixin` / `WeChat`); entries with no exe are dropped.
- **Consequence**: Windows grouping is keyed only on Uninstall registry entries — PATH tools, Go/Node binaries and portable exes are never grouped, they only ever appear as running rows. macOS groups by `.app` bundle path + child PIDs (`collect_helper_basenames`) and scans `/Applications`, `/System/Applications`, `~/Applications` including nested sub-app helpers.
- Icons: `kaitu-icon://bundle/<id>` (macOS) / `kaitu-icon://exe/<dir>` (Windows, 404 stub).

## Gotchas

- `main.rs` is the merge conflict hotspot — every module registers plugins/commands/setup there
- k2 sidecar must be at `binaries/k2-<rust target triple>` (`k2-x86_64-pc-windows-msvc.exe`; `k2-universal-apple-darwin` plus the per-arch copies `k2-aarch64-apple-darwin` / `k2-x86_64-apple-darwin` that `build-macos.sh` makes for the universal build)
- Event permissions require `core:event:default` in capabilities (NOT `event:default`)
- `reqwest::blocking::Client` panics in async context — always wrap in `tokio::task::spawn_blocking()`
- Missing `#[tauri::command]` registration in `tauri::generate_handler![]` causes white screen — first `invoke()` fails silently, React never renders.
- Windows updater: `update.install()` launches NSIS as child process, must call `app.exit(0)` immediately — NSIS needs old process to exit to overwrite binaries.
- Beta channel forces the **daemon** log level to debug (`set_log_level` IPC coerces to debug while on beta; `set_log_level_internal("debug")` at startup and on channel switch; `tauri-k2.ts` mirrors it). The desktop `tauri-plugin-log` level and the engine `log.level` that `buildConnectConfig()` emits are compile-time (`K2_BUILD_LOG_LEVEL` / `__K2_BUILD_LOG_LEVEL__`) — the channel does not change them.
- Pre-beta log level stored in `{app_data_dir}/pre-beta-log-level` file. Frontend passes `currentLogLevel` (from localStorage) when calling `set_update_channel` IPC since Rust cannot read browser localStorage.
- Windows Authenticode signing requires intermediate CA chain: `osslsigncode` must use `-ac scripts/ci/macos/certum-chain.pem` (Certum Code Signing 2021 CA). Without it, Windows UAC shows "Publisher: Unknown" because it can't trace Wordgate LLC cert to a trusted root. SimplySign PKCS#11 token must be logged in first (`make simplisign-login`).
- Windows cross-build from macOS: requires `cargo-xwin`, `makensis`, `osslsigncode`, `libp11`. See `docs/plans/2026-03-11-windows-build-on-macos.md` for full setup.
- Quit (tray / `app.exit`) sends `down` to the daemon via `service::stop_vpn()`; the window close button only hides — the VPN keeps running until Quit.
- `build.rs` `cargo:rerun-if-env-changed=K2_BRAND` is load-bearing: without it, switching `BRAND` between builds reuses the other brand's object files (串包二进制).
- `src-tauri/keys/private.key` (rsign-encrypted updater minisign secret; its pubkey is `tauri.conf.json` `plugins.updater.pubkey`) is tracked in the repo, and the same pubkey gates web-OTA bundles (`web_ota::updater_pubkey`). `keys/apple_certificate_base64.txt` and `profiles/*.provisionprofile` are tracked too.
- `service::versions_match` ignores `+build` metadata, and a `"dev"` version on either side always matches — dev builds never trigger a daemon reinstall.
- Web-OTA poller: skips a tick while `.boot-pending` exists (rotating `current/` before `ui_boot_ok` would make the next `startup_rollback` quarantine the wrong bundle); `MAX_BUNDLE_BYTES` = 64 MB hard cap on web.zip; `discard_stale_disk_ui` wipes `current/` + `previous/` when the shell version is newer than the disk UI, falling back to embedded assets until the next poll.
- The window's initial URL is still the config default (legacy tauri:// origin); `web_ota::prepare_boot` navigates to `kaitu-ui://` in setup. `show_window()` runs earlier in the same setup closure (nothing keeps the window hidden until the webapp renders) — do not "fix" it by hardcoding a url in tauri.conf.json (Windows needs the http://kaitu-ui.localhost form, and a config url would break dev mode).
- **导航目标必须是 origin 根，永远不是 `index.html`**（`ui_protocol::ui_boot_url()`，三条
  启动路径共用）：webapp 的 `BrowserRouter` 挂在 `/` 下，`…/index.html` 匹配不到路由 → 空树，
  而 Rust 日志、`ui_boot_ok`（只证明 JS 跑过）、bridge 轮询全部"健康"——0.4.8 就这样给每个用户
  发了白屏。四道防线：`ui_protocol.rs` 测试 `boot_url_is_the_origin_root_not_a_file`、webapp
  catch-all 路由、`smoke-dist.mjs` 对 `/` 与 `/index.html` 双路径冒烟、`ci.yml` `smoke-windows`
  装完 NSIS 后跑 `scripts/ci/windows/assert-ui-rendered.ps1` 读 WebView2 无障碍树断言真的渲染了。
- **dev 模式测不出启动 URL / 协议处理器 / origin 迁移这类问题**：web OTA 启动流与 poller 都被
  `cfg!(debug_assertions)` 关掉，`yarn tauri dev` 始终停在 Vite devUrl——必须用 release 构建验证
  （`make build-macos` 或 `make build-macos-test`）。
- **`tauri-plugin-localhost` 已移除（2026-08-18，`c8263b42`），不要再加回来**：它在匿名线程里
  `Server::http("localhost:14580").expect(...)`，端口被另一实例/另一品牌占住即 panic，
  `panic = "abort"` 放大成整进程 SIGABRT（0.4.7 macOS 启动 221ms 即崩、无日志）。
- 由此落地的两道防线：`main()` 第一行 `install_panic_hook()`（任何线程 panic 直接 append 进
  `desktop.log`，不经 log 门面）；`single-instance` 必须是**第一个**注册的插件（按注册顺序
  初始化，重复实例要先 `exit(0)`）。改插件顺序前先想清这两条；"启动即崩、无日志"先怀疑启动期 panic。

## Router LAN Bridge (`router_bridge.rs`)

Two Tauri commands support app-direct control of a headless k2r router (companion to the same feature's mobile bridge, see `mobile/CLAUDE.md`). Full design: `docs/superpowers/specs/2026-07-17-k2r-headless-app-control-design.md`.

- **`router_http_request(opts: RouterRequestOptions) -> RouterResponse`** — the only channel webapp's `_platform.routerRequest` uses to reach a LAN router (`http://10.17.79.1:1779`, k2r's DNAT-intercepted anchor address — see `webapp/CLAUDE.md` "Router Tab"). Gated by `is_private_host()`: scheme must be `http` **and** host must parse as an `Ipv4Addr` that is private or loopback (hostnames, IPv6, and public IPv4 are all rejected). Built on `reqwest::blocking` inside `spawn_blocking` (blocking client panics in Tauri's async runtime — same rule as elsewhere in this file). The client is built with `redirect(Policy::none())` — deliberate: `is_private_host` only validates the *requested* URL, not a `Location:` header from a possibly-compromised router, so following redirects would let a validated private-IP target bounce the client to an arbitrary public URL post-validation. The 3xx is surfaced to the caller instead of chased.
- **`get_default_gateway() -> Option<String>`** — physical-interface default gateway, macOS via `route -n get default` (skips `utun*` interfaces), Windows via `Get-NetRoute -DestinationPrefix '0.0.0.0/0'` filtered against adapter descriptions containing `wintun`/`tap`/`kaitu`/`tunnel`. **Currently unconsumed**: router discovery is anchor-only (constant address, DNAT-intercepted by k2r), so `router-service.ts` never calls this — kept as an `IPlatform`-optional capability for possible future diagnostics.
- Mirrored on the mobile side by `capacitor-k2.ts`'s `isPrivateIPv4Literal`/`assertRouterUrlAllowed` TS gate (`CapacitorHttp` has no native URL allowlist, so the equivalent check has to live in TS) plus `disableRedirects: true`.

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

Daemon runs as root on macOS → different `$PATH` and `$HOME` from the user, so it never relies on the user's `adb` install. `k2/daemon/helper_adb.go` `prepareAdb()`: reuse an existing adb server on `:5037` (avoids USB conflicts with Android Studio), else download platform-tools per the CDN `tools/tools.json` manifest and start its own server. Device ops go through `gadb` (pure Go ADB TCP client) — no external `adb` binary at runtime.

## S3 Log Upload (Desktop)

- **Feedback upload**: bundle tar.gz with unique feedbackId key: `desktop/{version}/{udid}/{date}/logs-{ts}-{id}.tar.gz`
- **Beta auto-upload** (desktop only): per-file PUT to `auto/{udid}/{filename}`. Active `.log` files overwrite (latest snapshot). Rotated `.log.gz` files use HEAD check to skip if already uploaded.
- Legacy `service-logs/` / `feedback-logs/` prefixes still supported by Lambda.
- Upload modules are read-only — never truncate source log files.
