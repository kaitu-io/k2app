# Architecture Decisions

Knowledge distilled from executed features. Links to validating tests.

---

## Split Globals Architecture — _k2 + _platform (2026-02-17, webapp-architecture-v2)

**Decision**: Replace VpnClient/PlatformApi factory abstractions with two injected globals: `window._k2: IK2Vpn` (VPN control) and `window._platform: IPlatform` (platform capabilities). Cloud API calls go through `cloudApi.request()` module.

**Supersedes**: VpnClient Abstraction Pattern, NativeVpnClient Mobile Bridge, PlatformApi Abstraction (all removed — modules deleted in webapp v2 migration).

**Why split globals over factory pattern**:
- Platform injection happens before React loads — no async factory chain needed
- Single `_k2.run(action, params)` method replaces 3 VpnClient implementations
- `_platform` exposes storage, UDID, clipboard, etc. directly — no conditional imports
- Each platform (Tauri/Capacitor/Web) injects its own implementation at startup

**Key interfaces** (defined in `webapp/src/types/kaitu-core.ts`):
- `IK2Vpn.run<T>(action, params): Promise<SResponse<T>>` — all VPN control
- `IPlatform` — os, storage, getUdid, clipboard, openExternal, updater

**Validating tests**: `webapp/src/stores/__tests__/vpn.store.test.ts`

---

## Service Version Matching with Build Metadata (2026-02-14, k2app-rewrite)

**Decision**: Compare service and app versions by stripping build metadata after `+` character. See `desktop/src-tauri/src/service.rs:88` — `versions_match()`.

**Why**: k2 binary version includes commit hash (e.g., `0.4.0+abc123`) while Tauri config has clean `0.4.0`. Per semver spec, build metadata after `+` is ignored for precedence.

**Validating tests**: `desktop/src-tauri/src/service.rs` — `test_versions_match_with_build_metadata`

---

## Antiblock Entry URL Resolution (2026-02-14→2026-02-17, k2app-rewrite + webapp-antiblock)

**Decision**: Webapp resolves Cloud API entry URL through multi-source fallback chain with AES-256-GCM decryption and localStorage cache.

**Flow**: localStorage cache → JSONP `<script>` from CDN mirrors (jsDelivr, Statically) → AES-256-GCM decrypt (hardcoded key) → parse entries JSON → pick first entry → cache to localStorage. Background refresh on cache hit.

**Why AES-256-GCM (not base64)**:
- Encrypted payload prevents CDN-level content scanning/blocking
- JSONP delivery (`window.__k2ac = {...}`) avoids CORS and fetch fingerprinting
- Key is hardcoded in client JS — not security against determined reverse engineering, but raises the bar for automated censorship tools
- No `atob()` in source — uses manual base64→Uint8Array to avoid detection patterns

**Integration with cloudApi**: `cloudApi.request()` calls `resolveEntry()` before every fetch. Returns absolute URL (e.g., `https://entry.example.com`) or empty string (fallback to relative URL). 401 refresh and retry also use `resolveEntry()`.

**Server-side CORS**: `/api/*` routes use `ApiCORSMiddleware()` — echoes origin with credentials for private origins only (localhost, 127.0.0.1, RFC 1918, `capacitor://localhost`). Public origins get no CORS headers. This allows cross-origin cloudApi calls from Tauri/Capacitor/dev-server while blocking CSRF from public sites.

**Validating tests**: `webapp/src/services/__tests__/antiblock.test.ts` (10 tests — crypto, JSONP, cache, CDN sources), `webapp/src/services/__tests__/cloud-api.test.ts` (antiblock integration — absolute URL, refresh URL), `api/middleware_cors_test.go` (10 tests — private/public origin, preflight)

---

## Private-Origin CORS for Client API Routes (2026-02-17, webapp-antiblock)

**Decision**: `/api/*` client routes use dynamic origin echo restricted to private/local origins. `/app/*` admin routes keep existing whitelist CORS.

**Why not `Access-Control-Allow-Origin: *`**: Auth endpoints (`/api/auth/web-login`, `/api/auth/refresh`, `/api/auth/logout`) set `HttpOnly` cookies. Browser spec: wildcard `*` with `credentials: include` causes browsers to silently discard `Set-Cookie` headers. Login breaks.

**Why not a static whitelist**: Client apps run from many origins — `http://localhost:1420` (Vite dev), `http://localhost:14580` (Tauri), `capacitor://localhost` (iOS), `https://localhost` (Android), `http://192.168.x.x` (OpenWrt router). A whitelist would need constant updating.

**Solution**: `isPrivateOrigin()` checks origin hostname against:
- `localhost` (any port, http/https)
- `127.0.0.1` (loopback)
- `capacitor://localhost` (iOS Capacitor)
- RFC 1918 ranges: `10.x.x.x`, `172.16-31.x.x`, `192.168.x.x`

Matching origins get `Access-Control-Allow-Origin: <origin>` + `Access-Control-Allow-Credentials: true`. Non-matching origins (public internet) get no CORS headers — browser blocks the request.

**Security model**: Attacker must be on user's LAN to exploit. Cookie `SameSite=Lax` + CSRF token provide additional protection.

**Validating tests**: `api/middleware_cors_test.go` — 10 tests covering all origin categories + preflight + boundary cases

---

## Old Service Cleanup on Upgrade (2026-02-14, k2app-rewrite)

**Decision**: Detect and remove old kaitu-service 0.3.x on first launch of k2app 0.4.0.

**Detection**: macOS checks LaunchDaemons plists; Windows checks `sc query kaitu-service`.
**Cleanup**: macOS `launchctl unload` + delete; Windows `sc stop && sc delete`.
**Why**: Old and new service both listen on :1777 — automatic cleanup prevents conflict.

**Validating tests**: `desktop/src-tauri/src/service.rs` — `test_detect_old_kaitu_service_no_crash`

---

## tauri-plugin-localhost for Mixed Content (2026-02-14, k2app-rewrite)

**Decision**: Serve webapp from `http://localhost:14580` instead of `https://tauri.localhost` to avoid WebKit mixed content blocking when calling HTTP daemon.

**Why**: WebKit (macOS, Linux) strictly blocks `https://` → `http://` requests even for loopback. Serving the webapp over HTTP eliminates mixed content. Security model unchanged — localhost already exposed via daemon :1777.

**Cross-reference**: See Framework Gotchas → "WebKit Mixed Content Blocking on macOS" for full details.

**Validating tests**: Integration test — fetch `/ping` from webview succeeds on macOS.

---

## Single Source of Truth for Versioning (2026-02-14, k2app-rewrite)

**Decision**: Root `package.json` version is the single source. All derivations:
- `tauri.conf.json` → `"version": "../../package.json"` (Tauri native reference)
- k2 binary → Makefile ldflags `-X main.version=$(VERSION)`
- webapp → `public/version.json` (generated by `make pre-build`)
- Release → git tags `v$(VERSION)`

**Why**: Prevents version drift. Single update point for releases.

**Validating tests**: `scripts/test_version_propagation.sh`

---

## iOS Two-Process vs Android Single-Process VPN (2026-02-14, mobile-rewrite)

**Decision**: iOS uses NEPacketTunnelProvider (separate NE process). Android runs everything in one process.

**iOS** (two processes): Main App → NETunnelProviderManager; NE Process → PacketTunnelProvider → gomobile Engine. Communication via `sendProviderMessage()` for status, `NEVPNStatusDidChange` for events, App Group UserDefaults for shared state.

**Android** (single process): K2Plugin → K2VpnService → gomobile Engine. Direct method calls (same process). K2Plugin binds via `bindService()`.

**Why different**: Apple requires VPN tunnels in NE extension (sandboxed process). Android VpnService runs in app process. This fundamentally changes status query pattern: iOS needs IPC, Android calls Engine directly.

**Validating tests**: Manual device testing on both platforms.

---

## iOS NE→App Error Propagation via App Group + cancelTunnelWithError (2026-02-16, ios-vpn-fixes)

**Decision**: NE process writes error text to App Group UserDefaults (`vpnError` key), then calls `cancelTunnelWithError(error)` to trigger system disconnect. Main app reads error from App Group in `NEVPNStatusDidChange` handler when status is `.disconnected`, pushes `vpnError` event to JS, then clears the key.

**Why this pattern**: iOS NE and main app are separate processes — no direct method calls or callbacks. The system provides exactly one real-time push channel from NE→App: `NEVPNStatusDidChange`. App Group provides the data channel. Together: NE writes error → triggers system notification → App reads error on notification.

**State source of truth**: `NEVPNStatusDidChange` is the ONLY VPN state source in the main app. All previous `UserDefaults("vpnState")` writes from NE were removed as orphaned (written but never read by main app). K2Plugin maps `NEVPNStatus` enum to `"connected"/"connecting"/"stopped"` strings.

**Error flow**: Go Engine error → `EventBridge.onError()` → App Group write + `cancelTunnelWithError(error)` → system sends `.disconnected` → K2Plugin reads App Group `vpnError` → JS `vpnError` event → clears key.

**State flow**: Go Engine disconnect → `EventBridge.onStateChange("disconnected")` → `cancelTunnelWithError(nil)` → system sends `.disconnected` → K2Plugin maps to `"disconnected"` → JS `vpnStateChange` event.

**Files**: `PacketTunnelProvider.swift` (NE side), `K2Plugin.swift` (App side)

**Validating tests**: Manual device testing — no test yet.

---

## Go→JS JSON Key Remapping at Native Bridge (2026-02-14, mobile-rewrite)

**Decision**: Go `json.Marshal` outputs snake_case; native bridge layers (K2Plugin.swift/kt) remap to camelCase before passing to webapp.

**Key map**: `connected_at→connectedAt`, `uptime_seconds→uptimeSeconds`, `wire_url→wireUrl`. State values pass through unchanged (Go engine `"disconnected"` arrives as `"disconnected"` in JS).

**Why remap at bridge, not Go**: Go convention is snake_case (changing requires struct tags across all code). Native bridge is the natural boundary. TypeScript expects camelCase.

**State passthrough (v2)**: The v1 bridge remapped `"disconnected"→"stopped"`, but webapp's `ServiceState` type has no `"stopped"` value, breaking all derived booleans. Fixed in mobile-webapp-bridge-v2: states pass through unchanged.

**Cross-reference**: See Framework Gotchas → "Go json.Marshal snake_case vs JavaScript camelCase" for the discovery story. See Bugfix Patterns → "Go→JS JSON Key Mismatch" for the original bug.

**Validating tests**: `webapp/src/services/__tests__/capacitor-k2.test.ts` — tests verify `"disconnected"` and `"connected"` states pass through correctly.

---

## Android AAR: Direct flatDir, No Wrapper Module (2026-02-16, android-aar-fix)

**Decision**: Remove `k2-mobile` wrapper module. App module references `k2mobile.aar` directly via Gradle `flatDir` + `implementation(name: 'k2mobile', ext: 'aar')`.

**Context**: gomobile bind produces `k2mobile.aar`. Originally a `k2-mobile` wrapper module used `api files('libs/k2mobile.aar')` — but `files()` treats AARs as JARs, losing Android resource/manifest handling.

**Why only app needs AAR**: k2-plugin module uses `VpnServiceBridge` interface (decoupled from AAR). Only `app` module's `K2VpnService` directly instantiates gomobile `Engine`. So only one consumer needs the AAR.

**Build pipeline**: `gomobile bind` → `k2/build/k2mobile.aar` → copy to `mobile/android/app/libs/` → Gradle resolves via flatDir.

**Alternatives rejected**:
- **Plan A** (keep wrapper): `api files()` doesn't work for AAR, `api` dependency exposed AAR transitively but incorrectly.
- **Plan B** (flatDir in all modules): Unnecessary — only app needs it.

**Validating tests**: `./gradlew assembleRelease` succeeds; APK contains `libgojni.so` for all 4 ABIs.

---

## Build System: Makefile Orchestration + Script Composition (2026-02-14)

**Decision**: Makefile handles version extraction and target orchestration. Platform-specific build logic lives in `scripts/build-*.sh`.

**Pattern**: `make pre-build` (version.json) → `make build-webapp` → platform-specific target (`build-macos`, `build-windows`, `build-mobile-ios`, `build-mobile-android`).

**Why Makefile + scripts**:
- Makefile provides declarative dependency graph and variable extraction
- Scripts handle platform conditionals (codesign, xcodebuild, gradlew) that are awkward in Make
- CI workflows call `make` targets — same commands locally and in CI

**Key gotcha**: `k2/build/` directory must exist before `gomobile bind` — Makefile creates it.

**Validating tests**: `scripts/test_build.sh` — 14 build verification checks

---

## Decoupled Code Signing for Windows Releases (2026-02-14)

**Decision**: Windows code signing happens outside GitHub Actions via an external `kaitu-signer` service orchestrated through S3 + SQS.

**Flow**: CI uploads unsigned .exe to S3 (scoped by run ID) → SQS message triggers external signer → signer signs with SimpliSign + TOTP → uploads signed artifact to S3 → CI polls with `scripts/ci/wait-for-signing.sh` (10s intervals, 600s timeout).

**Why decoupled**: Windows code signing requires hardware token access or cloud HSM. Neither is available inside GitHub Actions runners. S3-based artifact exchange is simple, auditable, and supports concurrent releases via run ID scoping.

**Validating tests**: Release workflow dry-run; `wait-for-signing.sh` timeout handling

---

## Vite Multi-Page Entry for Debug/Diagnostic Pages (2026-02-16, mobile-debug)

**Decision**: Use Vite `rollupOptions.input` to add standalone HTML pages alongside the main React app. `debug.html` is a second entry point — pure HTML+JS, no React/Store/Auth, shares the same `dist/` output.

**Why multi-page, not separate project**: Same build pipeline, zero Capacitor config changes. `cap sync` copies entire `dist/` including `debug.html`. Dev server serves both entries automatically.

**Key insight**: Capacitor bridge is **WebView-level**, not page-level. Any HTML loaded within the same Capacitor WebView has access to `window.Capacitor.Plugins.K2Plugin`. This means standalone debug pages can call native plugins directly without going through the React app's bootstrap chain.

**Navigation**: `window.location.href = '/debug.html'` causes full page navigation — React unmounts, debug page loads fresh. Return via back navigation re-initializes React (acceptable for debug tool).

**Pattern applicability**: Any Capacitor/Tauri app needing isolated diagnostic pages without framework dependencies. Useful for:
- Native bridge debugging (this use case)
- Network diagnostics
- Performance profiling pages

**Validating tests**: `yarn build` produces both `dist/index.html` and `dist/debug.html`; `webapp/src/pages/__tests__/Settings.test.tsx` validates hidden entry point.

---

## Dark-Only Theme with MUI + Tailwind v4 Design Tokens (2026-02-16→2026-02-17, kaitu-feature-migration + webapp-v2)

**Decision**: Ship exclusively dark mode. Design tokens defined in `webapp/src/theme/colors.ts` (MUI palette) and `webapp/src/app.css` (`@theme` block for Tailwind v4).

**Why dark-only**: Brand decision. Reduces code complexity — no `dark:` prefix, no theme toggle state, design once.

**Token system (v2)**: MUI `ThemeProvider` with custom dark palette (`webapp/src/contexts/ThemeContext.tsx`). Tailwind v4 `@theme` in `app.css` defines utility classes (`bg-primary`, `bg-success`, etc.) directly from design token names. Use token utilities, not Tailwind palette colors (`bg-primary` not `bg-blue-600`).

**Validating tests**: `npx tsc --noEmit` clean; visual verification.

---

## Unified Engine Package for Desktop + Mobile (2026-02-16, unified-engine)

**Decision**: Extract shared tunnel lifecycle logic from desktop `daemon/tunnel.go` and mobile `mobile/mobile.go` into a single `k2/engine/` package. Desktop daemon becomes a thin HTTP shell over `engine.Engine`. Mobile wrapper becomes a gomobile type adapter.

**Engine package structure**:
- `engine/engine.go` — Engine struct, Start(), Stop(), StatusJSON(), Status()
- `engine/config.go` — Config struct with platform-specific optional fields
- `engine/event.go` — EventHandler interface + state constants
- `engine/dns_handler.go` — DNS middleware for mobile TUN (moved from mobile/)
- `engine/engine_test.go` — 14 unit tests covering all config combinations

**Key design: Config.FileDescriptor discriminates platform behavior**:
- `fd >= 0` → Mobile (platform provides TUN fd, use DNS middleware)
- `fd == -1` → Desktop (self-create TUN with route exclusion)

**Optional Config fields for desktop-only features**:
- `DirectDialer` — Custom outbound interface binding
- `PreferIPv6` — Wire server IPv6 preference
- `Mode == "proxy"` → SOCKS5 proxy instead of TUN
- `DNSExclude` — Route exclusion for DNS servers
- `RuleConfig` — Complete k2rule config (overrides RuleMode)

**Mobile simplification**: `mobile/mobile.go` reduced from 251 lines to 57 lines by delegating everything to `engine.Engine`.

**Desktop simplification**: `daemon/tunnel.go` deleted — its `BuildTunnel()` logic is now `engine.Start()`.

**Why unified**: 80% code duplication eliminated. Single tunnel assembly implementation ensures consistent behavior across platforms. Desktop-specific features isolated to optional Config fields.

**Validating tests**: `k2/engine/engine_test.go` — TestEngineStart_MobileConfig, TestEngineStart_DesktopConfig, TestEngineStart_ProxyMode

---

## Config-Driven Connect — ClientConfig as Universal Currency (2026-02-16, config-driven-connect)

**Decision**: Replace opaque `connect(wireUrl)` passthrough with structured `connect(config: ClientConfig)` across all layers. `*config.ClientConfig` is the universal currency — webapp assembles it, daemon accepts it, state persists it, mobile receives it. Matches WireGuard/V2Ray/Clash pattern: GUI = config editor.

**Three representations, one struct**:
- **Go**: `config.ClientConfig` (YAML tags for CLI, JSON tags for API)
- **TypeScript**: `ClientConfig` interface (webapp assembles, passes to connect)
- **YAML**: `config.yml` (CLI users edit directly)

**Desktop daemon**: `doUp(cfg *config.ClientConfig, pid int)`. API accepts `{ "config": {...} }` JSON. `persistedState` saves `*config.ClientConfig` for auto-reconnect. `buildEngineConfig()` deleted — `engineConfigFromClientConfig()` called directly.

**Mobile**: `Engine.Start(configJSON string, fd int, dataDir string)` parses JSON → `config.SetDefaults()` → `engine.Config`. K2Plugin passes config JSON through `providerConfiguration` (iOS) / Intent extra (Android).

**CLI**: Resolves URL → `ClientFromURL()` or YAML → `LoadClient()` before sending to daemon. Daemon doesn't know how input was specified.

**Eliminated**: `setRuleMode()` hack (Swift, Kotlin, TS), `&rule=` URL append, native-side ruleMode storage (UserDefaults, SharedPreferences), `wire_url`/`config_path` daemon API params, `lastWireURL`/`lastConfigPath` daemon fields.

**Why this matters**: Rule mode, DNS, proxy settings all flow through config — no side channels. Adding new user preferences (DNS, log level) is just adding fields to ClientConfig, not wiring new native storage.

**Validating tests**: `k2/config/config_test.go` — JSON round-trip, YAML equivalence. `k2/daemon/daemon_test.go` — config-driven API.

---

## Global LoginDialog Modal Replaces Login Route (2026-02-16, kaitu-feature-migration)

**Decision**: Remove dedicated `/login` route. All authentication flows use a global `LoginDialog` modal component triggered on demand.

**Trigger sources**:
- App startup (AuthGuard): if no valid session, open dialog automatically
- Protected routes (LoginRequiredGuard): redirect → open dialog with context
- Feature pages (Purchase, Invite): click "login to continue" → open dialog with custom message

**State management**: `login-dialog.store.ts` Zustand store with `{ isOpen, trigger, message, open(), close() }`. On success, dialog closes and triggering page refreshes.

**Why modal over route**:
- No route navigation disruption — user stays on intended page
- Context-aware messaging ("Login to purchase" vs "Login to manage invites")
- Purchase page can show inline login form for unauthenticated users (same EmailLoginForm component)
- Mobile UX: modal feels more native than full-page route change

**Validating tests**: `webapp/src/stores/login-dialog.store.ts` — store logic; component tests pending v2 migration.

---

## Keep-Alive Tab Pattern for Tab Pages (2026-02-16, kaitu-feature-migration)

**Decision**: Tab pages (Dashboard, Purchase, Invite, Account) are mounted on first visit, then hidden (not unmounted) when switching tabs.

**Implementation**: `Layout.tsx` tracks mounted tabs in `useState`. Active tab rendered normally; inactive tabs use `visibility: hidden` + `position: absolute` CSS.

**Why keep-alive**:
- Preserves scroll position when switching tabs
- Maintains component state (form inputs, expanded sections)
- Reduces re-fetch on tab return (data stays loaded)
- Better perceived performance (instant tab switch, no loading spinner)

**Trade-off**: More memory usage (4 tab DOMs in memory). Acceptable for 4-tab app.

**Non-tab pages**: Sub-pages (`/devices`, `/issues`, etc.) use normal React Router `<Outlet />` — mount/unmount on navigate.

**Validating tests**: `webapp/src/components/Layout.tsx` — implementation; keep-alive tests pending v2 migration.

---

## Tauri Desktop Bridge: IPC for Daemon + Native Fetch for Cloud API (2026-02-17→2026-02-18, tauri-desktop-bridge)

**Decision**: Tauri desktop injects `window._k2` and `window._platform` from the webapp side (not Rust initialization scripts). Daemon communication uses Tauri IPC. Cloud API uses native `window.fetch` directly (no Rust proxy needed).

**Two-layer approach**:
1. **Daemon calls** (`_k2.run()`) -> `invoke('daemon_exec')` -> Rust `spawn_blocking` -> `reqwest` to `127.0.0.1:1777`
2. **Cloud API** (`cloudApi.request()`) -> native `window.fetch` -> HTTPS directly (server has CORS for localhost)
3. **Platform info** (`_platform.os`, `.version`) -> `invoke('get_platform_info')` -> Rust `std::env::consts::OS`

**Why IPC for daemon (not direct fetch)**: WebView on `localhost:14580` cannot fetch `http://127.0.0.1:1777` -- different port = cross-origin, daemon has no CORS headers. IPC bypasses WebView entirely.

**Why native fetch for cloud API (not Rust proxy)**: Cloud API has `ApiCORSMiddleware` that allows localhost origins with credentials. HTTP→HTTPS is an upgrade (not mixed content), so WebKit allows it. No fetch patching needed. Previous `@tauri-apps/plugin-http` approach was removed because the plugin's static import caused WebKit JS engine to freeze (see Framework Gotchas).

**Why webapp-side injection (not Rust init script)**: Rust `webview.eval_script()` cannot use ES module imports. Webapp dynamic import + `window.__TAURI__` detection provides full TypeScript type safety and access to npm packages.

**Detection order in `main.tsx`**: `window.__TAURI__` -> Tauri bridge; `Capacitor.isNativePlatform()` -> Capacitor bridge; `!_k2 || !_platform` -> standalone fallback.

**Validating tests**: `webapp/src/services/__tests__/tauri-k2.test.ts` -- 8 tests (IPC delegation, platform detection, transformStatus, standalone regression)

---

## Capacitor Mobile Bridge: K2Plugin → Split Globals Adapter (2026-02-17, mobile-webapp-bridge-v2)

**Decision**: `capacitor-k2.ts` wraps K2Plugin's separate methods (connect/disconnect/getStatus/getVersion) into the `IK2Vpn.run(action, params)` interface, and injects `window._platform` with mobile capabilities.

**Detection chain in `main.tsx`**: `window.__TAURI__` → Tauri bridge; `Capacitor.isNativePlatform()` → Capacitor bridge; `!_k2 || !_platform` → standalone fallback.

**Status format adaptation**: K2Plugin returns minimal status (`{ state, connectedAt?, uptimeSeconds?, error? }`). Bridge transforms to full `StatusResponseData`:
- `running` = derived from state (connecting/connected = true)
- `networkAvailable` = always true (mobile relies on system network detection)
- `startAt` = connectedAt ISO string → Unix seconds
- `error` = plain string → `ControlError { code: 570, message }`
- `retrying` = always false (gomobile engine doesn't auto-retry)

**Config-driven connect**: `run('up', config)` serializes config to JSON and passes to `K2Plugin.connect({ config: JSON.stringify(config) })`. Config is assembled by Dashboard from UI state (selectedCloudTunnel.url + activeRuleType). Go's `config.SetDefaults()` fills the rest.

**Event strategy**: Bridge registers `vpnStateChange` + `vpnError` listeners that log to console. State updates come through existing 2s polling in vpn.store via `run('status')`. Events supplement polling for faster detection (future optimization: trigger immediate re-poll on event).

**State passthrough fix**: Removed invalid `"disconnected"→"stopped"` mapping from both K2Plugin.swift and K2Plugin.kt. Go engine states now pass through unchanged to match webapp's `ServiceState` type.

**Validating tests**: `webapp/src/services/__tests__/capacitor-k2.test.ts` — 15 tests (injection, status mapping, action dispatch, platform capabilities, getK2Source, standalone regression)

---

## iOS TUN fd via KVC: Industry Standard for Go-Based VPN Engines (2026-02-17, ios-vpn-audit)

**Decision**: `packetFlow.value(forKey: "socket")` KVC 获取 TUN fd 是行业标准做法，不是 bug。所有基于 Go engine 的 iOS VPN app（WireGuard、sing-box、Clash）都用相同模式。

**Why KVC fd is necessary**: Apple 官方推荐 `readPacketObjects/writePacketObjects`，但这是 Swift async 回调模型。Go engine 使用阻塞 I/O（goroutine 里 `read(fd)` 循环），无法适配 Swift 的 async completion handler。将 Swift callbacks 桥接到 Go channel 会导致：
- 每个 packet 两次跨语言调用（Swift→Go read, Go→Swift write）
- `NEPacket` 对象分配开销（~50% 吞吐量损失）
- 异步→同步桥接复杂度极高

**k2 的完整 fd 链路**:
```
PacketTunnelProvider.swift
    → packetFlow.value(forKey: "socket") as? Int32  (KVC)
    → engine.start(configJSON, fd: Int(fd), dataDir: dataDir)  (gomobile)
    → mobile.Engine.Start(configJSON, fd, dataDir)  (Go)
    → engine.Config{FileDescriptor: fd}
    → provider.NewTUNProvider → tun.New(Options{FileDescriptor: fd})  (sing-tun)
    → tun.NewSystem(Stack{Handler: handlerAdapter})
    → sing-tun 拥有 fd，运行 async packet loop
```

**k2 已使用 sing-tun v0.7.11**: Go 侧不做任何 raw packet 处理。sing-tun 管理 fd 的读写循环、IP/TCP/UDP 解析、L4 stream 分发。k2 engine 只接收 `ConnectionHandler.HandleTCP/HandleUDP` 回调。

**同行验证**:
- WireGuard-apple: `WireGuardAdapter` 内部获取 fd，传给 wireguard-go
- sing-box-for-apple: `ExtensionPlatformInterface` 提供 fd 给 Go libbox
- Clash for iOS: 同样的 KVC fd 模式

**Apple 的态度**: Apple Developer Technical Support 表态 "the file-descriptor method works till now, but this is not a supported technique." 但没有在任何 iOS 版本中破坏过它，因为太多 VPN app 依赖此模式。

**风险评估**: 极低。Apple 破坏此行为 = 所有第三方 VPN app 失效，包括 WireGuard 官方 iOS 客户端。

**Cross-reference**: See Framework Gotchas → "iOS TUN fd Must Be Acquired After setTunnelNetworkSettings"

**Tests**: No test — platform-level constraint.
**Source**: ios-vpn-audit (2026-02-17)
**Status**: verified (cross-validated against WireGuard, sing-box, sing-tun source)

---

## ~~nativeExec: Generic IPC Passthrough~~ → SUPERSEDED by Specific Methods (2026-02-18)

**Superseded by**: platform-interface-cleanup (2026-02-18). `nativeExec` deleted from `IPlatform`. Replaced by `reinstallService?(): Promise<void>` — the only action that ever used `nativeExec`.

**Why reversed**: "Generic IPC passthrough" sounds extensible but was YAGNI — only one action (`admin_reinstall_service`) ever used it across 4 months. Meanwhile, it created a type-unsafe `string` action name with `any` params/return, making it impossible for TypeScript to catch misuse. Specific optional methods (`reinstallService?`) provide type safety, discoverability, and platform-appropriate fallbacks.

**Consumer migration**: `ServiceAlert.tsx` changed from `platform?.nativeExec('admin_reinstall_service')` to `platform?.reinstallService()`.

---

## Tauri Auto-Updater: Platform-Specific Install Paths (2026-02-18, tauri-updater-and-logs)

**Decision**: Auto-updater uses `tauri-plugin-updater` with platform-specific post-install behavior. macOS/Linux: store update info in Rust static state, notify frontend via Tauri event, apply on app exit or user action. Windows: `update.install()` launches NSIS installer as child process, then `app.exit(0)` immediately (NSIS takes over).

**Why platform divergence**: macOS/Linux updater replaces the app binary in-place — the app must restart to load the new binary. Windows NSIS installer is a separate process that handles extraction, service stop/start, and app relaunch itself. Keeping the app running during NSIS install causes file lock conflicts.

**Rust static state pattern**:
```rust
static UPDATE_READY: AtomicBool = AtomicBool::new(false);
static UPDATE_INFO: Mutex<Option<UpdateInfo>> = Mutex::new(None);
```
Cross-async coordination: the background check loop (`tokio::spawn`) sets these statics; IPC commands (`get_update_status`, `apply_update_now`) and `RunEvent::ExitRequested` handler read them. `AtomicBool` for the fast-path check, `Mutex<Option<T>>` for the payload.

**Background check loop**: 5s initial delay (let app settle), then 30min `tokio::time::interval`. Skips check if `UPDATE_READY` is already true. Download progress logged at 10% intervals.

**Frontend bridge**: `IUpdater` interface on `_platform.updater` with mutable properties (`isUpdateReady`, `updateInfo`, `isChecking`, `error`). `listen('update-ready')` for Tauri event. `invoke('get_update_status')` at initialization to restore pending update state from Rust (covers case where update was downloaded before frontend loaded).

**ExitRequested hook**: `main.rs` `.run()` callback checks `RunEvent::ExitRequested` → calls `install_pending_update()` → `app.restart()`. This ensures updates apply even if user closes the app without clicking "Update Now".

**Validating tests**: `desktop/src-tauri/src/updater.rs` — 3 unit tests (serialization, null notes, ready default); `webapp/src/services/__tests__/tauri-k2.test.ts` — 5 updater tests (initial state, existing update restore, apply, manual check, event listener)

---

## Log Upload in Tauri Shell, Not Daemon (2026-02-18, tauri-updater-and-logs)

**Decision**: Service log upload runs in the Tauri Rust process (`log_upload.rs`), not in the k2 daemon. Uses `tokio::task::spawn_blocking` to run `reqwest::blocking::Client` HTTP calls from a `#[tauri::command]` async handler.

**Why Tauri, not daemon**: The primary use case for log upload is when the daemon is crashed or unresponsive. If log upload lived in the daemon, it would be unavailable exactly when it's needed most. The Tauri shell process is always alive while the app window is open.

**4 log sources**: service (Go daemon `service.log`), crash (Go panic `panic-*.log`), desktop (Tauri `desktop.log`), system (macOS Console via `log show` / Windows Event Log stub). Each uploaded separately with its own S3 key.

**Sanitization**: `sanitize_logs()` strips sensitive patterns (`"token":"`, `"password":"`, `"secret":"`, `Authorization: Bearer`, `X-K2-Token:`) by prefix-matching and replacing with `***`. Simple string replacement — not regex — because log sanitization must never fail.

**Upload pipeline**: Read log → sanitize → gzip compress (flate2) → S3 PUT (public bucket, no auth) → Slack webhook notification with S3 links. Partial success allowed — some logs may fail while others succeed.

**spawn_blocking pattern** (reusable for any heavy I/O in Tauri):
```rust
#[tauri::command]
pub async fn upload_service_log_command(params: UploadLogParams) -> Result<UploadLogResult, String> {
    tokio::task::spawn_blocking(move || upload_service_log(params))
        .await
        .map_err(|e| format!("Task failed: {}", e))
}
```
The inner function uses `reqwest::blocking::Client` freely. `spawn_blocking` moves it to a dedicated thread pool, avoiding Tokio runtime panic from blocking in async context.

**Validating tests**: `desktop/src-tauri/src/log_upload.rs` — 7 unit tests (sanitize, compress roundtrip, S3 key format, Slack message, param deserialization); `webapp/src/services/__tests__/tauri-k2.test.ts` — 1 uploadLogs test

---

## IPlatform Cleanup: 19→12 Members, Native Plugins Replace WebView APIs (2026-02-18, platform-interface-cleanup)

**Decision**: Slim `IPlatform` from 19 to 12 members. Delete 7 unused/redundant methods, make 4 methods required (previously optional), replace WebView API stubs with native platform plugins.

**Deleted members** (zero production consumers): `isDesktop`, `isMobile`, `showToast`, `getLocale`, `exit`, `debug`, `warn`, `nativeExec`.

**Renamed**: `uploadServiceLogs` → `uploadLogs` (clarity).

**Made required** (previously optional): `openExternal`, `writeClipboard`, `readClipboard`, `syncLocale`.

**Kept optional** (desktop-only): `reinstallService?`, `getPid?`, `updater?`, `uploadLogs?`.

**Why delete `isDesktop`/`isMobile`**: Derivable from `os` field. Layout store has its own responsive `isDesktop`/`isMobile` (screen-size based) — name collision caused confusion. Consumers now use `['ios', 'android'].includes(os)`.

**Native plugin replacements**:

| Capability | Before (WebView API) | After (Native Plugin) |
|-----------|---------------------|----------------------|
| Tauri clipboard | `navigator.clipboard` | `@tauri-apps/plugin-clipboard-manager` |
| Tauri open URL | `window.open()` | `@tauri-apps/plugin-opener` |
| Capacitor clipboard | `navigator.clipboard` | `@capacitor/clipboard` |
| Capacitor open URL | `window.open()` | `@capacitor/browser` |

**Why native plugins over WebView APIs**: WebView clipboard is unreliable (Android WebView clipboard completely broken, Windows WebView2 focus bug). `window.open()` may be blocked by popup blockers. Native plugins use OS-level APIs — always work.

**Tauri Rust additions**: `sync_locale` IPC command (tray menu i18n), `get_pid` IPC command (process monitoring). Both registered in `main.rs` `invoke_handler`.

**Validating tests**: `webapp/src/services/__tests__/tauri-k2.test.ts` (13 tests — 5 new plugin tests), `webapp/src/services/__tests__/capacitor-k2.test.ts` (20 tests — 5 new plugin tests)

---

## Bridge as State Contract Translation Layer — transformStatus() Mandatory (2026-02-17, vpn-error-reconnect)

**Decision**: Every bridge layer (`tauri-k2.ts`, `capacitor-k2.ts`) MUST implement `transformStatus()` to normalize backend state before exposing it to the webapp. Backends must never pass raw state strings directly to the webapp. The bridge is the contract translation layer.

**Root cause discovered**: Daemon outputs `state: "stopped"` but webapp's `ServiceState` type has no `"stopped"` value — only `"disconnected"`. Tauri bridge was a pass-through, so all downstream booleans computed from state were wrong on desktop:
- `isDisconnected` → always false (because `"stopped" !== "disconnected"`)
- `isError` → never triggered
- `handleToggleConnection` button disabled logic → broken

**State contract table (after fix)**:

| ServiceState | Source | Produced by |
|-------------|--------|-------------|
| `disconnected` | Backend | Daemon `"stopped"` → bridge normalizes; Engine `"disconnected"` → direct |
| `connecting` | Backend | Both backends |
| `connected` | Backend | Both backends |
| `error` | Bridge synthesis | `disconnected + lastError` → bridge synthesizes `"error"` |
| `reconnecting` | Engine EventHandler | Transient signal (engine state stays `"connected"`) |
| `disconnecting` | UI optimistic | `setOptimisticState('disconnecting')` — never from backend |

**transformStatus() responsibilities**:
1. State normalization: `"stopped"` → `"disconnected"` (daemon-specific rename)
2. Error synthesis: `state === "disconnected" && raw.error` → `state = "error"`
3. Field mapping: `connected_at` → `startAt` (Unix seconds); `error` string → `ControlError { code: 570, message }`
4. Defaults: `running`, `networkAvailable`, `retrying` — synthesized from state

**Validating tests**: `webapp/src/services/__tests__/tauri-k2.test.ts` (6 tests — stopped normalization, error synthesis, connected_at mapping), `webapp/src/services/__tests__/capacitor-k2.test.ts` (3 tests — error synthesis fix)

---

## OnNetworkChanged: Engine Wire Reset Pattern (2026-02-17, vpn-error-reconnect)

**Decision**: `engine.OnNetworkChanged()` resets cached wire connections without changing engine state. It emits a transient `"reconnecting"` signal via `EventHandler.OnStateChange`, calls `wire.ResetConnections()`, then emits `"connected"`. Engine state remains `StateConnected` throughout — only the event handler sees the transient state.

**Why transient signal, not state change**: Engine's 3-state model (`disconnected`, `connecting`, `connected`) is correct and simple. `"reconnecting"` is a UI concern — a momentary signal that wire is self-healing. Making it a persistent engine state would require extra transitions and teardown logic.

**Wire Resettable interface** (`k2/wire/transport.go`):
```go
type Resettable interface {
    ResetConnections()
}
```
Type-assertion pattern: `if r, ok := e.wire.(Resettable); ok { r.ResetConnections() }`. Optional capability — not all wire implementations need it.

**QUICClient reset**: Closes `c.conn` + `c.transport` + `c.udpMux`, sets them to nil, keeps `closed=false`. Next `connect()` call lazy-rebuilds from nil. TUN fd not affected (kernel interface, independent of physical network).

**TCPWSClient reset**: Closes `c.sess` + `c.udpMux`, sets to nil. Same lazy-rebuild pattern.

**gomobile export** (`k2/mobile/mobile.go`):
```go
func (e *Engine) OnNetworkChanged() {
    e.inner.OnNetworkChanged()
}
```

**Validating tests**: `k2/engine/engine_test.go` (4 tests — connected state triggers reset, non-connected state no-ops, reconnecting signal emitted), `k2/wire/transport_test.go` (5 subtests — reset clears conn, keeps closed=false, lazy reconnect on next dial)

---

## Platform Network Change Detection: Bridge Layer Owns Mobile Reconnect (2026-02-17, vpn-error-reconnect)

**Decision**: Mobile network change detection (WiFi→4G) must be implemented in the native bridge layer (Android Kotlin, iOS Swift), not the Go engine. Platform provides the TUN fd — any reconnect requires tearing down and rebuilding the TUN fd from the platform.

**Why bridge layer, not engine**: `engine.Start()` requires a platform-provided TUN fd (`VpnService.Builder.establish()` on Android, `packetFlow` on iOS NE). The engine cannot independently reconnect on mobile — it has no way to get a new TUN fd. This constraint was documented in `k2/mobile-sdk/spec.md`: "Auto-reconnect on network change — native shell responsibility".

**Android implementation** (`K2VpnService.kt`):
```kotlin
// ConnectivityManager.NetworkCallback with 500ms debounce
override fun onAvailable(network: Network) {
    handler.removeCallbacks(reconnectRunnable)
    handler.postDelayed(reconnectRunnable, 500)
}
```
Wire reset via `engine?.onNetworkChanged()`. Network callback registered after engine start, unregistered on stop.

**iOS implementation** (`PacketTunnelProvider.swift`, NE process):
```swift
// NWPathMonitor with 500ms DispatchWorkItem debounce
pathMonitor.pathUpdateHandler = { [weak self] path in
    if path.status == .satisfied {
        self?.debounceReconnect()
    }
}
```
NE process is separate from app process — NWPathMonitor runs in the VPN extension sandbox.

**500ms debounce rationale**: Network change events fire multiple times during transition (interface down, interface up, route updates). 500ms absorbs the storm and triggers only once after stabilization.

**Desktop**: Not implemented. Daemon already has `tryAutoReconnect` mechanism based on persisted state. sing-tun `DefaultInterfaceMonitor` is viable for future desktop reconnection but not in scope.

**Validating tests**: Manual device testing (Android WiFi→4G, iOS WiFi→4G). No unit tests for native platform callbacks — they require Android SDK / iOS SDK mocks.

---

## sing-tun Network Monitoring: Available But Unused by k2 Engine (2026-02-17, android-vpn-audit)

**Context**: Investigating why k2 engine has no network change detection or auto-reconnect.

**Discovery**: sing-tun v0.7.11 (already a k2 dependency) provides a complete cross-platform network monitoring system that k2 does NOT use.

**Two monitor interfaces** (`github.com/sagernet/sing-tun/monitor.go`):
- `NetworkUpdateMonitor` — detects any route/link changes, fires `NetworkUpdateCallback`
- `DefaultInterfaceMonitor` — tracks default network interface, fires `DefaultInterfaceUpdateCallback` with interface info + flags. 1-second debounce built in.

**Platform implementations**:

| Platform | NetworkUpdateMonitor | DefaultInterfaceMonitor |
|----------|---------------------|------------------------|
| macOS | `AF_ROUTE` socket → route messages | `route.FetchRIB` or socket connect to `10.255.255.255:80` |
| Linux | `netlink.RouteSubscribe` + `LinkSubscribe` | Main routing table query |
| Windows | `winipcfg.RegisterRouteChangeCallback` + `InterfaceChangeCallback` | `GetIPForwardTable2` lowest metric |
| Android | **BANNED** — returns `ErrNetlinkBanned` | Inspects `netlink.RuleList` for VPN routing rules |
| iOS (NE) | N/A | `UnderNetworkExtension` mode: socket connect method |

**k2 current usage**: ZERO. `tun.Options.InterfaceMonitor` never set. No `NetworkUpdateMonitor` created. No callbacks registered.

**What happens on network change today** (silent tunnel death):
1. User switches WiFi→4G → underlying socket loses route
2. QUIC keepalive pings fail → 30s `MaxIdleTimeout` expires
3. `QUICClient.connect()` caches dead `c.conn` (never cleared on error)
4. All new streams fail through dead connection
5. Engine still reports `"connected"` — no error, no state change

**k2/ own documentation confirms scope exclusion**:
- `mobile-sdk/spec.md`: "Auto-reconnect on network change — native shell responsibility"
- `p1-platform-readiness/spec.md`: "Wire session health detection + auto-rebuild is P2 (not in scope)"

**Architecture decision points for adding reconnection**:
1. **Engine layer** (Go, cross-platform): Use sing-tun `DefaultInterfaceMonitor` → detect interface change → tear down + rebuild wire. Works on desktop. DOES NOT work on Android (netlink banned).
2. **Wire layer** (Go): Clear cached `c.conn` in `QUICClient` on error → force lazy reconnect on next dial. Simplest change, self-healing after 30s timeout. Does not detect network change — just recovers from dead connections.
3. **Bridge layer** (platform-native): Android `ConnectivityManager.NetworkCallback`, iOS `NWPathMonitor` → stop engine + restart with saved config + new TUN fd.
4. **Daemon layer** (desktop only): Register sing-tun `DefaultInterfaceMonitor` → trigger `doDown()` + `doUp(savedConfig)`.

**Key constraint**: Mobile reconnect MUST involve bridge layer — `engine.Start()` requires platform-provided TUN fd (`VpnService.Builder.establish()` on Android, `NEPacketTunnelProvider` on iOS). Engine cannot independently reconnect on mobile.

**sing-tun `Options.InterfaceMonitor` vs application reconnect**: `InterfaceMonitor` in `tun.Options` is for auto-route management (telling sing-tun which interface to exclude). For application-level reconnection, use `NewNetworkUpdateMonitor()` + `NewDefaultInterfaceMonitor()` directly and register own callbacks.

**Scrum verdict (2026-02-17)**: Short-term — bridge-layer error synthesis + Android NetworkCallback. Long-term — engine adds `StateError` + wire clears dead connections. See TODO: `docs/todos/android-mobile-error-state-ux-gap.md`.

**Tests**: No test yet — discovery from code audit.
**Source**: android-vpn-audit (scrum session 2026-02-17)
**Status**: verified (code-level confirmation)

---

## Structured Error Codes: Engine-Layer Classification with HTTP-Aligned Codes (2026-02-18, structured-error-codes)

**Decision**: Error classification happens in the k2 engine layer (`k2/engine/error.go`), not in wire layer, bridge layer, or webapp. `ClassifyError(err error) *EngineError` takes any Go error and maps it to one of 7 HTTP-aligned codes. Priority chain: `net.Error.Timeout()` check first (408), then string pattern matching, then fallback 570.

**Why engine layer, not wire or bridge**:
- Wire has 80+ error sites — adding classification at each would be high risk / high churn
- Bridge regex (JS-side) is fragile — string formats can change between k2 versions
- Engine is the single convergence point for all wire errors, downstream of all wire implementations

**Seven codes (HTTP-aligned)**:

| Code | Name | Trigger |
|------|------|---------|
| 400 | BadConfig | "parse URL", "missing auth", "unsupported scheme", "missing port" |
| 401 | AuthRejected | "stream rejected by server" |
| 403 | Forbidden | "pin mismatch", "blocked CA" |
| 408 | Timeout | `net.Error.Timeout() == true` (checked FIRST) |
| 502 | ProtocolError | "uTLS handshake", "certificate", "QUIC dial" |
| 503 | ServerUnreachable | "TCP dial", "connection refused", "network unreachable", "listen UDP" |
| 570 | ConnectionFatal | default / unclassified |

**Daemon alignment**: `k2/daemon/daemon.go` `lastError` changed from `string` to `*engine.EngineError`. `doUp()` calls `engine.ClassifyError(err)` instead of `fmt.Sprintf`. `statusInfo()` serializes structured error. `ClassifyError` is exported (uppercase) for cross-package access.

**API format change** (breaking): `"error"` field in daemon status changed from string to object:
```
Before: {"state": "stopped", "error": "wire: TCP dial: connection refused"}
After:  {"state": "stopped", "error": {"code": 503, "message": "wire: TCP dial: ..."}}
```

**Backward compat in bridges**: Both bridges check `typeof raw.error === 'object'` first; fall back to string → code 570 for old daemons. This allows webapp to deploy before daemon binary is updated.

**OnError() interface unchanged**: `EventHandler.OnError(message string)` stays as string. Status polling path (`StatusJSON()`) is the primary error delivery channel. gomobile compatibility preserved.

**Webapp alignment**: `control-types.ts` renamed to `vpn-types.ts`. Dead codes removed (100–119 range, 510–519 range). Kept 400/401/402/403/408/502/503/570. `getErrorI18nKey()` maps each to an i18n key. All 7 locales updated.

**Validating tests**: `k2/engine/error_test.go` (22 tests — 19 classification subtests + StatusJSON + OnError preservation), `k2/daemon/daemon_test.go` (3 daemon tests — structured error, no error, clear on doUp), `webapp/src/services/__tests__/tauri-k2.test.ts` (backward compat + structured error path)

---

## Unified Debug Page at Abstraction Layer (2026-02-18, unified-debug-page)

**Decision**: Debug page tests at `window._k2.run()` / `window._platform` abstraction layer instead of raw native APIs (K2Plugin or Tauri IPC). One `debug.html` works on all 3 platforms.

**Supersedes**: mobile-debug v1 which called `window.Capacitor.Plugins.K2Plugin.method()` directly.

**Why abstraction layer over raw native**:
- Single page works everywhere — Tauri, Capacitor, Standalone
- Catches bridge-layer bugs (transformStatus, key remapping) that raw native testing misses
- Config-driven connect elevated ClientConfig to a first-class concept on all platforms; both Tauri and Capacitor use `_k2.run('up', config)` identically
- Raw native debugging (when needed) is better served by Xcode/Android Studio/DevTools

**Platform-conditional UI**: Desktop-only features (updater, reinstallService, getPid, uploadLogs) shown/hidden via `window.__TAURI__` check. Capacitor-only features (none currently) would use `Capacitor.isNativePlatform()`.

**Tests**: No unit test — page is a diagnostic tool; build verification only (`vite build` → `dist/debug.html` exists).
**Source**: unified-debug-page (2026-02-18)
**Status**: verified (build passes, manual test)

---
