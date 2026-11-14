# desktop — Tauri v2 Shell (macOS + Windows only)

Rust desktop shell using Tauri v2. UI loads via the `kaitu-ui://` custom protocol (web-ota bundle from disk, embedded assets as fallback).

**Linux is not this shell** — see root `CLAUDE.md` "Linux desktop has no Tauri".

**Long-form notes (not auto-loaded — read the one you need before changing that code):** `desktop/docs/brand-notes.md` (release tags, purity guard incident, icons, window shape, signing description, log prefixes) · `desktop/docs/shell-notes.md` (full Rust module map, packaging / thin updater / minos, App Bypass enumeration, startup gotcha history).

## Commands

```bash
cd src-tauri && cargo check     # Rust compilation check
cd src-tauri && cargo test      # Rust tests
yarn tauri dev                  # Dev mode (expects Vite on :1420)
yarn tauri build --target universal-apple-darwin  # bare Tauri macOS build; the shipped .pkg comes from `make build-macos` (scripts/build-macos.sh)
yarn tauri build --runner cargo-xwin --target x86_64-pc-windows-msvc  # Windows cross-build from macOS (shipped path: `make build-windows`)
```

## Brand (双品牌: kaitu / overleap)

Build-time 烘焙，与 webapp 同一契约：`BRAND=overleap make build-macos`（Makefile `BRAND ?= kaitu` → `export K2_BRAND`）。三层：webapp dist（`__K2_BRAND__`）、Rust `cfg(brand_overleap)`（build.rs 发 cfg；另一品牌 URL 不进二进制）、Tauri `--config src-tauri/tauri.conf.overleap.json`（**合并时数组整体替换，overlay 里数组字段必须写全量**）。Invariants（机制与事故：`desktop/docs/brand-notes.md`）：

- **GitHub Release tag 绝不能以 `v` 开头**：`overleap-v{VERSION}` / kaitu 自 0.4.13 起 `kaitu-v{VERSION}`（`scripts/publish-desktop.sh --brand=…`）——`gh release create` 会推这个 tag，`v*` 触发两品牌全平台重建并覆盖 CDN 产物（0.4.12 踩过）。
- **k2 daemon 品牌中立**：1777 端口、launchd label `kaitu`（`k2/cmd/k2/service_darwin.go`）、
  NSIS `SERVICE_NAME "kaitu"` 不随品牌变。两品牌同机共存 = last-install-wins 接管
  daemon，双方 app 都能控制 VPN。
- 纯度守卫 `scripts/check-desktop-brand-purity.sh <brand> <path>`；`.app.tar.gz` 按**内容**（顶层 `.app` 目录名）校验——两品牌共享 bundle 目录，glob 收集会拿到另一品牌残留（`813bf3f5`）。`build-macos.sh` 按品牌精确路径收集并立即跑门。webapp 品牌纯度的真正门是打包前的 `webapp/scripts/check-brand-purity.sh`（压缩后 `strings` 看不见）。
- 脚本里调 `make` 必须显式带 `BRAND=$BRAND`（递归 make 只透传 `K2_BRAND`）——见根 CLAUDE.md Cross-Layer。
- `desktop/src-tauri/installer-hooks.overleap.nsh` 是构建期由 Makefile 对
  `installer-hooks.nsh` 做 sed 替换（`io.kaitu.desktop` → `io.overleap.desktop`）生成的
  派生副本——仅当 `BRAND=overleap` 才生成，gitignored，永不手改。
- 签名主体（Developer ID / SimplySign / notarize 账号）与 updater minisign 密钥两品牌
  共用——已拍板的品牌泄漏点，不再讨论。
- 图标由 `webapp/brand-assets/{kaitu,overleap}/generate.sh` 生成（`icons/` / `icons-overleap/`），不要用 `yarn tauri icon`；NSIS 语言 overleap 仅 English；位图里的文字纯度守卫抓不到，靠发布前人工视觉 smoke。
- **窗口形态按品牌分叉**（kaitu 竖版 9:20 锁比；overleap 横向 `tauri.conf.overleap.json` min 880×620）——overleap 的 minHeight 620 是 webapp 侧栏布局（短边 ≥ 600）的契约。**`K2_BRAND=overleap cargo test` 必须与默认 `cargo test` 各跑一次**；`ci.yml` 的 `test-linux-rust` / `test-macos` / `test-windows` 各自紧跟一步。
- **Windows Authenticode 描述随品牌**：`scripts/ci/sign-description.sh` 读 `K2_BRAND` → `Overleap Desktop` / `Kaitu Desktop`（`scripts/ci/windows/sign-binary.ps1` 同一 env 分叉）。测试：`bash scripts/test-sign-description.sh`。
- **日志上传按品牌分前缀**（`log_upload.rs`）。**两品牌的主二进制都叫 `k2app`**，所以 overleap 的 `MACOS_LOG_PREDICATE` 是 `process CONTAINS "k2app"`（`"Overleap"` 匹配不到任何进程）；kaitu 谓词按字节保留。`brand_prefix_tests` 每个 cfg 各钉一条字面量。

## Rust Modules (`src-tauri/src/`) — full map in `desktop/docs/shell-notes.md`

`main.rs` (setup; panic hook + single-instance first — Gotchas), `service.rs` (k2 daemon at `:1777`: `daemon_exec` → `/api/core`, `daemon_helper_exec` → `/api/helper`), `service_diag.rs` (macOS daemon diagnostics without root, #3911), `dock_icon.rs`, `channel.rs`, `status_stream.rs` (SSE `GET /api/events`), `tray.rs` (Show / Hide / Quit only), `updater.rs`, `window.rs`, `storage.rs` / `storage_crypto.rs`, `web_ota.rs`, `ui_protocol.rs`, `storage_migration.rs`, `log_upload.rs`, `app_list.rs` / `installed_apps.rs` / `icon_protocol.rs`, `router_bridge.rs`. Invariants:

- `ensure_service_running`: elevated `k2 service install` only if the cleanup + poll phase fails — **actual service state is the sole arbiter**, a reported install error is diagnostics only.
- `CloseRequested` on `main` → `hide_window` (only Quit exits); `RunEvent::ExitRequested` → `service::stop_vpn()` then pending updates. Installing a `-beta` build activates the beta channel.
- `window.rs`: `show_window()` runs in setup **before** `web_ota::prepare_boot` navigates — there is no `frontend_ready` handshake (comments in window.rs / web_ota.rs naming it are stale).
- `storage.json`: atomic `.tmp` + rename; single-instance = single writer.
- `web_ota.rs` gates on `sig` (mandatory minisign, updater pubkey) / `min_desktop` / `min_bridge` / `isNewerVersion`, owns `DESKTOP_BRIDGE_VERSION`, no-ops in debug builds; `K2_WEB_OTA_BASE` overrides endpoints for UAT (signature gates still enforced).
- `ui_protocol.rs` origin: `kaitu-ui://localhost` macOS, `http://kaitu-ui.localhost` Windows. `storage_migration.rs` carries preferences/caches, not auth (login lives in storage.json).

## Tauri Config (`src-tauri/tauri.conf.json`) — packaging detail in `desktop/docs/shell-notes.md`

- Bundle `targets: ["app", "nsis"]` — **no DMG**; the macOS installer is a `.pkg` from `scripts/build-macos.sh` (`pkgbuild` + `productsign` + notarize, `scripts/pkg-scripts/{preinstall,postinstall}`). NSIS is `perMachine` with `installerHooks: installer-hooks.nsh`. `csp: null`, `withGlobalTauri: true`.
- **First download ≠ update**: `.pkg` and `_universal.app.tar.gz` stay universal; `build_thin_update` also ships `_aarch64.app.tar.gz` / `_x64.app.tar.gz`, and `publish-desktop.sh` points `darwin-aarch64` / `darwin-x86_64` at them only when **both** sigs exist on S3.
- **k2 sidecar minos**: darwin build gets `-mmacosx-version-min` = `minimumSystemVersion` via `CGO_CFLAGS`/`CGO_LDFLAGS` (`K2_DARWIN_CGOFLAGS`) — without it 0.4.7–0.4.13 shipped k2 at 26.0 in a 12.0 app; `MACOSX_DEPLOYMENT_TARGET` alone is not enough (stale cgo cache). `build-macos.sh` runs `scripts/check-macos-minos.sh`. Sidecar also built `-trimpath` (`K2_DESKTOP_GOFLAGS`).
- Updater: CloudFront endpoints with minisign public key. Version: `"../../package.json"`; window size / identifier differ per brand (see Brand).

## Plugins

- `tauri-plugin-single-instance` — **must be registered first** (Gotchas). (`tauri-plugin-log` rotation: root CLAUDE.md "Log rotation".)
- `tauri-plugin-mcp-bridge` — cargo feature `mcp-bridge` only (tauri-mcp dev bridge); `build.rs` writes/removes the gitignored `capabilities/mcp-bridge.json`; `make build-macos-test` builds with `--features=mcp-bridge`

## IPC Commands (JS → Rust)

The command list is `main.rs` `generate_handler![]` (the bridge contract test parses it into `contracts/bridge-api.json`). Non-obvious ones:

- `daemon_helper_exec` — `adb-*` actions go to daemon `/api/helper`, not `/api/core`.
- `get_service_blocker` / `open_service_settings` (bridge v4, macOS) — Login Items "Allow in the Background" off → webapp `ServiceAlert`; `open_service_settings` errors on non-macOS.
- `ui_boot_ok` clears `.boot-pending`; `storage_migration_*` serve the origin migration.

## App Bypass app lists (`installed_apps.rs` + `app_list.rs`)

`list_installed_apps` is the **primary** list; `list_running_processes` is a supplement the webapp folds in (`AppBypass.tsx` `foldRunningIntoInstalled`; the `_platform.appList` contract is in `webapp/CLAUDE.md`). Running rows are only fetched when both enumerators exist. Enumeration detail: `desktop/docs/shell-notes.md`.

- **Windows fold**: `installed.id` is the install *directory*, `running.id` is the exe *path*; a running exe under an installed dir extends that app's `processNames` and drops out of the "more — running" section. **macOS** ids differ in kind (bundle path vs bundle id), so the supplement is deduped by process name instead.
- **Windows grouping is keyed only on Uninstall registry entries** (HKLM in **both** WOW64 views + HKCU — 32-bit installers such as WeChat 4.x live under `WOW6432Node`); install dirs must pass `is_unsafe_install_dir`. PATH tools / portable exes only ever appear as running rows.
- **macOS identifies an app by its bundle directory**: `paths = [bundle path]` feeds the engine's `match.app_paths`; `processNames` is just `[CFBundleExecutable]`. Do not bring back a bundle walk for names (387k file opens, basename collisions). Windows also emits `paths = [install dir]` and keeps the exe-name walk.
- Icons: `kaitu-icon://bundle/<id>` (macOS) / `kaitu-icon://exe/<dir>` (Windows, 404 stub).

## Gotchas

- `main.rs` is the merge conflict hotspot — every module registers plugins/commands/setup there
- k2 sidecar must be at `binaries/k2-<rust target triple>` (`k2-x86_64-pc-windows-msvc.exe`; `k2-universal-apple-darwin` plus the per-arch copies `k2-aarch64-apple-darwin` / `k2-x86_64-apple-darwin` that `build-macos.sh` makes for the universal build)
- Event permissions require `core:event:default` in capabilities (NOT `event:default`)
- `reqwest::blocking::Client` panics in async context — always wrap in `tokio::task::spawn_blocking()`
- Missing `#[tauri::command]` registration in `tauri::generate_handler![]` causes white screen — first `invoke()` fails silently, React never renders.
- Windows updater: `update.install()` launches NSIS as child process, must call `app.exit(0)` immediately — NSIS needs old process to exit to overwrite binaries.
- Beta channel forces only the **daemon** log level to debug; the `tauri-plugin-log` level and the engine `log.level` are compile-time (`K2_BUILD_LOG_LEVEL`). Rust cannot read localStorage, so the frontend passes `currentLogLevel` to `set_update_channel` (pre-beta level saved in `{app_data_dir}/pre-beta-log-level`).
- Windows Authenticode signing requires intermediate CA chain: `osslsigncode` must use `-ac scripts/ci/macos/certum-chain.pem` (Certum Code Signing 2021 CA). Without it, Windows UAC shows "Publisher: Unknown" because it can't trace Wordgate LLC cert to a trusted root. SimplySign PKCS#11 token must be logged in first (`make simplisign-login`).
- Windows cross-build from macOS: requires `cargo-xwin`, `makensis`, `osslsigncode`, `libp11`. See `docs/plans/2026-03-11-windows-build-on-macos.md` for full setup.
- Quit (tray / `app.exit`) sends `down` to the daemon via `service::stop_vpn()`; the window close button only hides — the VPN keeps running until Quit.
- `build.rs` `cargo:rerun-if-env-changed=K2_BRAND` is load-bearing: without it, switching `BRAND` between builds reuses the other brand's object files (串包二进制).
- `src-tauri/keys/private.key` (rsign-encrypted updater minisign secret; its pubkey is `tauri.conf.json` `plugins.updater.pubkey`) is tracked in the repo, and the same pubkey gates web-OTA bundles (`web_ota::updater_pubkey`). `keys/apple_certificate_base64.txt` and `profiles/*.provisionprofile` are tracked too.
- `service::versions_match` ignores `+build` metadata, and a `"dev"` version on either side always matches — dev builds never trigger a daemon reinstall.
- Web-OTA poller skips a tick while `.boot-pending` exists (else `startup_rollback` quarantines the wrong bundle); `MAX_BUNDLE_BYTES` = 64 MB; `discard_stale_disk_ui` wipes `current/` + `previous/` when the shell is newer than the disk UI.
- **Do not hardcode a url in tauri.conf.json** to fix the boot URL — `web_ota::prepare_boot` navigates to `kaitu-ui://` in setup (Windows needs the http://kaitu-ui.localhost form; a config url breaks dev mode).
- **导航目标必须是 origin 根，永远不是 `index.html`**（`ui_protocol::ui_boot_url()`）——`…/index.html` 匹配不到 `BrowserRouter` 路由 → 白屏而一切日志"健康"（0.4.8 事故）。防线：`boot_url_is_the_origin_root_not_a_file`、webapp catch-all 路由、`smoke-dist.mjs` 双路径冒烟、`ci.yml` `smoke-windows` 的 `scripts/ci/windows/assert-ui-rendered.ps1`。
- **dev 模式测不出启动 URL / 协议处理器 / origin 迁移**（`cfg!(debug_assertions)` 关掉 web OTA）——必须用 release 构建验证（`make build-macos` 或 `make build-macos-test`）。
- **`tauri-plugin-localhost` 已移除（`c8263b42`），不要再加回来**：端口被占即 panic，`panic = "abort"` 放大成启动即崩、无日志（0.4.7）。由此：`main()` 第一行 `install_panic_hook()`（任何线程 panic 直接写 `desktop.log`）；`single-instance` 必须是**第一个**注册的插件。"启动即崩、无日志"先怀疑启动期 panic。

## Router LAN Bridge (`router_bridge.rs`)

Flow: `webapp/CLAUDE.md` "Router Tab".

- **`router_http_request`** — the only channel for `_platform.routerRequest`. Gated by `is_private_host()`: scheme `http` **and** a private/loopback `Ipv4Addr` literal. **Keep `redirect(Policy::none())`** — the gate validates only the requested URL; following a router's `Location:` would escape it.
- **`get_default_gateway`** — **unconsumed** (discovery is anchor-only).

## Storage Encryption (`storage_crypto.rs`)

- AES-256-GCM, key = HKDF-SHA256 of the `machine-uid` crate's ID (macOS `IOPlatformUUID`, Windows `MachineGuid`) — **not** `sysctl kern.uuid` (hostname-derived UUIDv3, collided in v0.4.0, `d4ebdd6`).
- `ENC1:` prefix marks encrypted values; plaintext is read transparently. `mcp/storage_crypto.go` reimplements it with shared test vectors (read-only session sharing) — change both together.
- Threat model: 落盘混淆 + 硬件绑定, **not** anti-local-attacker (module doc).

## macOS PKG Install Order

Preinstall runs the OLD binary, postinstall runs the NEW. Always `launchctl unload` before overwriting plist; otherwise the old process keeps the binary locked and the install silently leaves the old one running.

## Artifact Naming

This section is the source of truth (root CLAUDE.md points here). `{Product}_{VERSION}_{ARCH}.{EXT}` — underscore-separated, `Product` = `Kaitu` / `Overleap`.

- macOS: `_universal.pkg` / `_universal.app.tar.gz` + `.sig`, plus thin updater bundles `_aarch64.app.tar.gz` / `_x64.app.tar.gz` + `.sig` (see Tauri Config)
- Windows: `_x64.exe` + `.sig`
- S3 path: `{brand}/desktop/{VERSION}/` (`kaitu/` or `overleap/`; `scripts/publish-desktop.sh`)

Never use hyphen separator (`Kaitu-`) or `-setup` suffix.

## S3 Log Upload (`log_upload.rs`)

Root CLAUDE.md "Log rotation" covers caps/retention and the read-only rule. Desktop specifics:

- Stages `k2*.log` / `.log.gz` / `panic-*.log` from both root and user daemon log dirs, `desktop*.log`, `service-history.log` + pkg installer trail (macOS), macOS `log show` output, Windows NSIS diagnostics (`install-diag.log`, `kaitu-preinstall.log`); sanitizes, tar.gz, `spawn_blocking`.
- **Feedback**: `{S3_PREFIX_MANUAL}/{version}/{udid}/{date}/logs-{ts}-{id}.tar.gz` (`desktop/` kaitu, `desktop-overleap/` overleap). **Beta auto-upload** (desktop only): per-file PUT `{S3_PREFIX_AUTO}/{udid}/{filename}` (`auto/` / `auto-overleap/`); active `.log` overwrites, rotated `.log.gz` skipped via HEAD.
- **The notify Lambda (`scripts/lambda/s3-log-notify/index.mjs`) only recognises `PLATFORM_PREFIXES = {desktop, mobile}`** — `desktop-overleap/` uploads land but trigger no notification until that set gains it and the Lambda is redeployed (`auto/` never notifies, by design).

The daemon's adb helper (root daemon → its own adb, CDN `tools/tools.json`) is daemon-layer: `k2/daemon/helper_adb.go`.
