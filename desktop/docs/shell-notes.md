# Desktop shell notes — Rust module map, packaging, App Bypass enumeration, startup gotchas

Moved verbatim from `desktop/CLAUDE.md` (2026-11-14) so the per-session doc stays small. **Read this before changing `main.rs` setup / plugin order, `service.rs` install phases, `updater.rs`, `web_ota.rs` / `ui_protocol.rs` boot flow, `tauri.conf.json` bundle settings, the macOS thin-updater / minos build steps, or `installed_apps.rs` / `app_list.rs`** — the CLAUDE.md keeps invariants; the full wording and incident history are here.

## Rust Modules (`src-tauri/src/`)

- **main.rs** — `install_panic_hook()` first, single-instance plugin first (Gotchas). `RunEvent::ExitRequested` → `service::stop_vpn()` (sends `down`, once) then applies pending updates; `CloseRequested` on `main` → `hide_window` (only Quit exits); `ScaleFactorChanged` → `window::reclamp_min_size`. Installing a `-beta` build activates the beta channel at startup.
- **service.rs** — k2 daemon at `:1777` (`daemon_exec` → `/api/core`, `daemon_helper_exec` → `/api/helper` for `adb-*`). `ensure_service_running`: phase 1 `cleanup_old_kaitu_service()` + poll `action:version` ≤15 s; phase 2 elevated `k2 service install` (osascript / PowerShell `-Verb RunAs`) only if phase 1 fails; phase 3 re-poll 5 s — **actual service state is the sole arbiter**, a reported install error is diagnostics only. `set_log_level` forces debug on beta (`set_log_level_internal()` is the blocking reusable form).
- **service_diag.rs** — macOS daemon diagnostics readable without root (#3911: launchd refusing the job leaves k2.log silent): `launchd_summary()`, `background_blocker()` ("Allow in the Background" off → `get_service_blocker`), durable `service-history.log` next to desktop.log (`record()`, trimmed to its newest half past 256 KB, written by service.rs / updater.rs), and the pkg trail from `/var/log/install.log`; `log_upload.rs` bundles both.
- **dock_icon.rs** — macOS only, `refresh()` runs in setup before any window work: sets the Dock tile from the bundled icon every launch, and once per icon change (fingerprint file `dock-icon-fingerprint`, skipped on a fresh install) deletes the Dock icon cache and restarts the Dock — the only thing that cleared the stale logo after 0.4.12.
- **channel.rs** — stable/beta persisted in `update-channel` (app data dir); `endpoints_for_channel()`.
- **status_stream.rs** — SSE client for daemon `GET /api/events` (3 s reconnect) → `service-state-changed { available }` / `vpn-status-changed`.
- **tray.rs** — Show / Hide / Quit only (re-localised via `sync_locale`); Quit = `stop_vpn()` + `app.exit(0)`. No connect items.
- **updater.rs** — 5 s delay → 30 min loop; emits `update-ready`. Windows: NSIS install + `app.exit(0)`; macOS: apply on exit (`install_pending_update()` = `app.restart()`). `set_update_channel` saves/restores pre-beta log level and returns `{channel, logLevel}`. Downgrade detection: stable + beta build → `version_comparator(!=)`. macOS channel-switch install restarts via a `sh -c` helper that waits for the old PID (`restart_after_channel_switch`).
- **window.rs** — size from screen; created hidden, `adjust_window_size()` + `show_window()` run in setup **before** `web_ota::prepare_boot` navigates — there is no `frontend_ready` handshake (comments in window.rs / web_ota.rs naming it are stale). `--minimized` autostart; Windows always-on-top trick to focus; `hide_window()` minimizes on Windows, hides on macOS.
- **storage.rs / storage_crypto.rs** — `storage.json` in app data dir, in-memory mirror, atomic `.tmp` + rename (single-instance = single writer); values AES-256-GCM (see "Storage Encryption").
- **log_upload.rs** — runs in Tauri (works with the daemon dead); see "S3 Log Upload".
- **app_list.rs / installed_apps.rs / icon_protocol.rs** — App Bypass `list_running_processes` / `list_installed_apps` (`{id, label, processNames, paths?, iconUrl, installerPackageName?}`, camelCase serde) and `kaitu-icon://` (macOS PNG; Windows 404 stub); see "App Bypass app lists".
- **web_ota.rs** — polls `{CDN}/{brand}/web[/beta]/latest.json` (5 s + 30 min), gates on `sig` (mandatory minisign, updater pubkey) / `min_desktop` / `min_bridge` / `isNewerVersion`, pending → current (→ previous) atomic swap, `.boot-pending` + `startup_rollback()` quarantine. Owns `DESKTOP_BRIDGE_VERSION`. Debug builds no-op. `K2_WEB_OTA_BASE` overrides endpoints for UAT (signature gates still enforced).
- **ui_protocol.rs** — `kaitu-ui://` (origin `kaitu-ui://localhost` macOS, `http://kaitu-ui.localhost` Windows): disk `current/` bundle, embedded fallback, SPA fallback.
- **storage_migration.rs** — one-time origin migration: legacy `tauri://` origin boots with `?migrate=export`, webapp dumps localStorage via `storage_migration_put/done`, `storage-migrated` marker + 20 s watchdog. Login lives in storage.json, so it carries preferences/caches, not auth.
- **router_bridge.rs** — see "Router LAN Bridge".

## Tauri Config (`src-tauri/tauri.conf.json`)

- Bundle: `targets: ["app", "nsis"]` — no DMG. The macOS installer is a `.pkg` built by `scripts/build-macos.sh` (`pkgbuild` + `productsign` + notarize) from the `.app`, with `scripts/pkg-scripts/{preinstall,postinstall}`; `createUpdaterArtifacts` yields the `.app.tar.gz` + `.sig`. **First download ≠ update**: the `.pkg` (and `_universal.app.tar.gz`, kept as `darwin-universal` + fallback) stay universal because a browser can't tell Apple Silicon from Intel; `build_thin_update` additionally ships `_aarch64.app.tar.gz` / `_x64.app.tar.gz` (`lipo -thin` → same `sign_app` → own notarize + staple, ~half size) and `publish-desktop.sh` points `darwin-aarch64` / `darwin-x86_64` at them only when **both** sigs exist on S3, otherwise at the universal one. The desktop k2 sidecar is built with `-trimpath` (`K2_DESKTOP_GOFLAGS` in the root Makefile) — no CI paths in shipped binaries. Its darwin build also gets `-mmacosx-version-min` = `tauri.conf.json` `minimumSystemVersion` via `CGO_CFLAGS`/`CGO_LDFLAGS` (`K2_DARWIN_CGOFLAGS`) — without it clang stamps the build host's macOS as minos (0.4.7–0.4.13 shipped k2 at 26.0 in a 12.0 app); `MACOSX_DEPLOYMENT_TARGET` alone is not enough (not in Go's build-cache key, stale cgo objects get relinked). `build-macos.sh` runs `scripts/check-macos-minos.sh` (every Mach-O slice ≤ the bundle's `LSMinimumSystemVersion`). NSIS is `perMachine` with `installerHooks: installer-hooks.nsh`. `csp: null`, `withGlobalTauri: true`.

## App Bypass app lists (`installed_apps.rs` + `app_list.rs`)

`list_installed_apps` is the **primary** list; `list_running_processes` is a supplement the webapp folds in (`AppBypass.tsx` `foldRunningIntoInstalled`; the `_platform.appList` contract is in `webapp/CLAUDE.md`). Running rows are only fetched when both enumerators exist.

- **Windows fold**: `installed.id` is the install *directory*, `running.id` is the exe *path*; a running exe under an installed dir extends that app's `processNames` and drops out of the "more — running" section. **macOS** ids differ in kind (bundle path vs bundle id), so the supplement is deduped by process name instead.
- **Windows scan** (`windows::scan_hive`): HKLM in both WOW64 views (`KEY_WOW64_64KEY` + `KEY_WOW64_32KEY` — 32-bit NSIS installers such as WeChat 4.x live under `WOW6432Node`) plus HKCU; skips `SystemComponent=1` / `ParentKeyName` entries. Install dir = first of `InstallLocation` → `DisplayIcon` dir → `UninstallString` dir that passes `is_unsafe_install_dir` (refuses `C:\Windows*`, drive roots, bare `Program Files` / `(x86)` / `ProgramData` / `Users` / `WindowsApps`, UNC/relative). `collect_exes` walks depth ≤ 3 skipping `unins*`; `supplemental_process_names` pins WeChat's out-of-tree `WeChatAppEx.exe` family by Uninstall key name (`Weixin` / `WeChat`); entries with no exe are dropped.
- **Consequence**: Windows grouping is keyed only on Uninstall registry entries — PATH tools, Go/Node binaries and portable exes are never grouped, they only ever appear as running rows. macOS groups by `.app` bundle path + child PIDs (`collect_helper_basenames`) and scans `/Applications`, `/System/Applications`, `~/Applications`.
- **macOS identifies an app by its bundle directory, not by executable names**: `paths = [bundle path]` feeds the engine's `match.app_paths` (a process matches when its executable lives under the bundle — helpers, sub-apps, XPC services, crash handlers, wherever they sit), and `processNames` is just `[CFBundleExecutable]` (classifier input + fallback identity when the app runs from an App Translocation path). Do not bring back a bundle walk for names: it had to open every file in every app (387k on a dev Mac) and basenames collide across apps (`chrome_crashpad_handler`). Running GUI apps carry `paths` too. **Windows** emits `paths = [install dir]` as well (only a dir that passed `is_unsafe_install_dir`) and keeps the exe-name walk: names carry the out-of-tree supplements (`WeChatAppEx.exe`) and match on engines that predate `app_paths`.
- Icons: `kaitu-icon://bundle/<id>` (macOS) / `kaitu-icon://exe/<dir>` (Windows, 404 stub).

## Gotchas

- Beta channel forces the **daemon** log level to debug (`set_log_level` IPC coerces to debug while on beta; `set_log_level_internal("debug")` at startup and on channel switch; `tauri-k2.ts` mirrors it). The desktop `tauri-plugin-log` level and the engine `log.level` that `buildConnectConfig()` emits are compile-time (`K2_BUILD_LOG_LEVEL` / `__K2_BUILD_LOG_LEVEL__`) — the channel does not change them.
- Pre-beta log level stored in `{app_data_dir}/pre-beta-log-level` file. Frontend passes `currentLogLevel` (from localStorage) when calling `set_update_channel` IPC since Rust cannot read browser localStorage.
- Windows Authenticode signing requires intermediate CA chain: `osslsigncode` must use `-ac scripts/ci/macos/certum-chain.pem` (Certum Code Signing 2021 CA). Without it, Windows UAC shows "Publisher: Unknown" because it can't trace Wordgate LLC cert to a trusted root. SimplySign PKCS#11 token must be logged in first (`make simplisign-login`).
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

## IPC Commands (JS → Rust)

- `get_service_blocker` / `open_service_settings` (bridge v4) — macOS: report a user-flippable reason the daemon cannot run (Login Items "Allow in the Background" off, via `service_diag::background_blocker()`; `None` = none known) and open System Settings → Login Items & Extensions; `open_service_settings` errors on non-macOS. Webapp `ServiceAlert`.

## Router LAN Bridge (`router_bridge.rs`)

- **`router_http_request`** — the only channel for `_platform.routerRequest`. Gated by `is_private_host()`: scheme `http` **and** host parses as a private/loopback `Ipv4Addr` (hostnames, IPv6, public IPv4 rejected). `reqwest::blocking` inside `spawn_blocking`. Built with `redirect(Policy::none())` on purpose: the gate validates only the requested URL, so following a `Location:` from a compromised router would bounce to an arbitrary public URL post-validation; the 3xx is returned instead.
- **`get_default_gateway`** — macOS `route -n get default` (skips `utun*`), Windows `Get-NetRoute` filtered against `wintun`/`tap`/`kaitu`/`tunnel` adapters. **Unconsumed** (discovery is anchor-only).
