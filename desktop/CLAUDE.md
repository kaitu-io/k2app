# desktop — Tauri v2 Shell (macOS + Windows only)

Rust desktop shell using Tauri v2. UI loads via the `kaitu-ui://` custom protocol (web-ota bundle from disk, embedded assets as fallback).

**Linux is not this shell** — see root `CLAUDE.md` "Linux desktop has no Tauri".

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
  （`scripts/publish-desktop.sh --brand=overleap`）。kaitu 自 0.4.13 起同理用
  `kaitu-v{VERSION}`：Release tag **绝不能以 `v` 开头**——`gh release create` 会推这个
  tag，`v*` 触发两品牌全平台重建并覆盖刚签进 latest.json 的 CDN 产物（0.4.12 踩过，及时取消）。
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
- 脚本里调 `make` 必须显式带 `BRAND=$BRAND`（递归 make 只透传 `K2_BRAND`）——见根 CLAUDE.md Cross-Layer。
- `desktop/src-tauri/installer-hooks.overleap.nsh` 是构建期由 Makefile 对
  `installer-hooks.nsh` 做 sed 替换（`io.kaitu.desktop` → `io.overleap.desktop`）生成的
  派生副本——仅当 `BRAND=overleap` 才生成，gitignored，永不手改。
- 签名主体（Developer ID / SimplySign / notarize 账号）与 updater minisign 密钥两品牌
  共用——已拍板的品牌泄漏点，不再讨论。
- kaitu 图标由 `webapp/brand-assets/kaitu/generate.sh` 从 `logo.svg`（≤32px 用 `logo-small.svg`）生成进 `icons/`；`.icns` 用带透明边距的 824/1024 版，其余为满幅圆角块。
- overleap 图标由 `webapp/brand-assets/overleap/generate.sh` 从 `logo.svg` 生成进 `icons-overleap/`
  （不要用 `yarn tauri icon`，它只认 PNG 源且会覆盖 `.icns/.ico` 的生成方式）；NSIS 语言 overleap 仅 English。
- 图像资产（logo PNG 等二进制）不在桌面纯度守卫覆盖范围内——`strings`/`grep` 抓不到位图里的文字；
  桌面侧靠发布前人工视觉 smoke 把关（原 `web/tests/brand-assets.test.ts` 哈希守卫随 overleap 站迁出 `web/` 已删除）。
- **窗口形态按品牌分叉**：kaitu 是竖版手机比例（`tauri.conf.json` 430×956、max 480，`window.rs` 9:20 锁比）；
  overleap 是横向桌面窗口（`tauri.conf.overleap.json` 1040×700、min 880×620、可最大化，`window.rs`
  `cfg(brand_overleap)` 分支按工作区 85% 夹紧、不锁比）。webapp 侧栏布局靠短边 ≥ 600 触发，所以
  overleap 的 minHeight 620 是布局契约，不是随手值。两套常量各有测试模块，`K2_BRAND=overleap cargo test`
  必须与默认 `cargo test` 各跑一次——`ci.yml` 的三个 Rust 测试 job（`test-linux-rust` / `test-macos` /
  `test-windows`）现在各自紧跟默认跑一步 `K2_BRAND=overleap cargo test`。
- **Windows Authenticode 描述随品牌**：`scripts/ci/sign-description.sh` 读 `K2_BRAND`（Makefile `export`
  → Tauri `bundle.windows.signCommand` 进程环境）→ `Overleap Desktop` / `Kaitu Desktop`，供
  `scripts/ci/macos/windows-sign.sh` 的 osslsigncode `-n`；`scripts/ci/windows/sign-binary.ps1` 用同一 env
  分叉 signtool `/d`（`Overleap` / `Kaitu`）。`src-tauri/windows-sign.sh` 的 journal 多记一行 `brand=`，
  dry-run（`K2_SIGN_LOG=… SKIP_WINDOWS_SIGNING=true`）即可证明 env 到达了签名器。测试：
  `bash scripts/test-sign-description.sh`。证书主体（Wordgate LLC）两品牌共用，见上一条。
- **日志上传按品牌分前缀**（`log_upload.rs`，前缀值见 "S3 Log Upload"）：`MACOS_LOG_PREDICATE` = `process CONTAINS "kaitu"` /
  `process CONTAINS "k2app"`。**两品牌的主二进制都叫 `k2app`**（crate 名；两份 tauri.conf 都没设
  `mainBinaryName`，Tauri v2 不会改名，已建 `Kaitu.app` 的 `CFBundleExecutable = k2app`）——所以 overleap
  谓词写进程实名，`"Overleap"` 匹配不到任何进程；kaitu 谓词按字节保留。`brand_prefix_tests` 每个 cfg
  各钉一条字面量。同一桶（`kaitu-service-logs`）两品牌共用，靠前缀区分。

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
- Updater: CloudFront endpoints with minisign public key
- Version: `"../../package.json"`; window size / identifier differ per brand (see Brand)

## Plugins

- `tauri-plugin-single-instance` — **must be registered first** (Gotchas). (`tauri-plugin-log` rotation: root CLAUDE.md "Log rotation".)
- `tauri-plugin-mcp-bridge` — cargo feature `mcp-bridge` only (tauri-mcp dev bridge); `build.rs` writes/removes the gitignored `capabilities/mcp-bridge.json`; `make build-macos-test` builds with `--features=mcp-bridge`

## IPC Commands (JS → Rust)

The command list is `main.rs` `generate_handler![]` (the bridge contract test parses it into `contracts/bridge-api.json`). Non-obvious ones:

- `daemon_helper_exec` — `adb-*` actions go to daemon `/api/helper`, not `/api/core`.
- `get_service_blocker` / `open_service_settings` (bridge v4) — macOS: report a user-flippable reason the daemon cannot run (Login Items "Allow in the Background" off, via `service_diag::background_blocker()`; `None` = none known) and open System Settings → Login Items & Extensions; `open_service_settings` errors on non-macOS. Webapp `ServiceAlert`.
- `ui_boot_ok` clears `.boot-pending`; `storage_migration_*` serve the origin migration.

## App Bypass app lists (`installed_apps.rs` + `app_list.rs`)

`list_installed_apps` is the **primary** list; `list_running_processes` is a supplement the webapp folds in (`AppBypass.tsx` `foldRunningIntoInstalled`; the `_platform.appList` contract is in `webapp/CLAUDE.md`). Running rows are only fetched when both enumerators exist.

- **Windows fold**: `installed.id` is the install *directory*, `running.id` is the exe *path*; a running exe under an installed dir extends that app's `processNames` and drops out of the "more — running" section. **macOS** ids differ in kind (bundle path vs bundle id), so the supplement is deduped by process name instead.
- **Windows scan** (`windows::scan_hive`): HKLM in both WOW64 views (`KEY_WOW64_64KEY` + `KEY_WOW64_32KEY` — 32-bit NSIS installers such as WeChat 4.x live under `WOW6432Node`) plus HKCU; skips `SystemComponent=1` / `ParentKeyName` entries. Install dir = first of `InstallLocation` → `DisplayIcon` dir → `UninstallString` dir that passes `is_unsafe_install_dir` (refuses `C:\Windows*`, drive roots, bare `Program Files` / `(x86)` / `ProgramData` / `Users` / `WindowsApps`, UNC/relative). `collect_exes` walks depth ≤ 3 skipping `unins*`; `supplemental_process_names` pins WeChat's out-of-tree `WeChatAppEx.exe` family by Uninstall key name (`Weixin` / `WeChat`); entries with no exe are dropped.
- **Consequence**: Windows grouping is keyed only on Uninstall registry entries — PATH tools, Go/Node binaries and portable exes are never grouped, they only ever appear as running rows. macOS groups by `.app` bundle path + child PIDs (`collect_helper_basenames`) and scans `/Applications`, `/System/Applications`, `~/Applications`.
- **macOS identifies an app by its bundle directory, not by executable names**: `paths = [bundle path]` feeds the engine's `match.app_paths` (a process matches when its executable lives under the bundle — helpers, sub-apps, XPC services, crash handlers, wherever they sit), and `processNames` is just `[CFBundleExecutable]` (classifier input + fallback identity when the app runs from an App Translocation path). Do not bring back a bundle walk for names: it had to open every file in every app (387k on a dev Mac) and basenames collide across apps (`chrome_crashpad_handler`). Running GUI apps carry `paths` too. **Windows** emits `paths = [install dir]` as well (only a dir that passed `is_unsafe_install_dir`) and keeps the exe-name walk: names carry the out-of-tree supplements (`WeChatAppEx.exe`) and match on engines that predate `app_paths`.
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

Flow: `webapp/CLAUDE.md` "Router Tab".

- **`router_http_request`** — the only channel for `_platform.routerRequest`. Gated by `is_private_host()`: scheme `http` **and** host parses as a private/loopback `Ipv4Addr` (hostnames, IPv6, public IPv4 rejected). `reqwest::blocking` inside `spawn_blocking`. Built with `redirect(Policy::none())` on purpose: the gate validates only the requested URL, so following a `Location:` from a compromised router would bounce to an arbitrary public URL post-validation; the 3xx is returned instead.
- **`get_default_gateway`** — macOS `route -n get default` (skips `utun*`), Windows `Get-NetRoute` filtered against `wintun`/`tap`/`kaitu`/`tunnel` adapters. **Unconsumed** (discovery is anchor-only).

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
