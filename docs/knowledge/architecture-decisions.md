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

## NetworkChangeNotifier: Engine Interface, sing-tun Adapter in Daemon (2026-02-18, network-change-reconnect)

**Decision**: `NetworkChangeNotifier` interface is defined in the `engine` package (not daemon). The daemon package provides `singTunMonitor` adapter that implements it by wrapping sing-tun's `NetworkUpdateMonitor` + `DefaultInterfaceMonitor`. Mobile platforms (iOS/Android) call `OnNetworkChanged()` directly from native bridge code — they don't need this interface.

**Why interface in engine, not daemon**:
- Engine must not import third-party tun libraries directly — clean dependency boundary
- Interface allows testability with mock monitors (used in 5 engine tests)
- Mobile calls `OnNetworkChanged()` without any monitor (platform handles detection natively)

**Instance sharing: same DefaultInterfaceMonitor for engine callback AND tun.Options**:
```go
// k2/daemon/network_monitor.go
func NewNetworkMonitor() (engine.NetworkChangeNotifier, tun.DefaultInterfaceMonitor, error) {
    // Returns SAME ifaceMon for both uses
    return &singTunMonitor{...}, ifaceMon, nil
}
// k2/daemon/daemon.go — both receive same instance
ecfg.NetworkMonitor = monitor      // engine callback path
ecfg.InterfaceMonitor = ifaceMon   // tun.Options self-exclusion path
```
`tun.Options.InterfaceMonitor` calls `RegisterMyInterface(tunName)` internally, excluding TUN self-routing from triggering change events. If you create separate instances, this self-exclusion is lost — the engine would constantly reconnect when the TUN interface is set up.

**MonitorFactory for testability** (same pattern as `EngineStarter`):
```go
type Daemon struct {
    EngineStarter  func(engine.Config) (*engine.Engine, error)
    MonitorFactory func() (engine.NetworkChangeNotifier, any, error)
}
```
Tests inject mock factories. Production uses `defaultMonitorFactory()` which calls `NewNetworkMonitor()`.

**Non-fatal on failure**: Certain environments (containers, minimal Linux, edge cases) may return `ErrInvalid` from `NewNetworkUpdateMonitor`. Daemon logs a warning and continues — engine degrades to passive 30s idle timeout recovery.

**Validating tests**: `k2/engine/engine_test.go` — `TestEngine_NetworkMonitor_NilMonitor_NoPanic`, `TestEngine_NetworkMonitor_StartedOnEngineStart`, `TestEngine_NetworkMonitor_ClosedOnEngineStop`, `TestEngine_NetworkMonitor_ClosedOnFail`, `TestEngine_NetworkMonitor_CallbackTriggersOnNetworkChanged`; `k2/daemon/network_monitor_test.go` — `TestNewNetworkMonitor_ReturnsAdapter`, `TestDaemon_EngineConfig_IncludesMonitor`, `TestDaemon_MonitorFactory_Failure_NonFatal`

---

## Polling-Only UI State: Events Are Debug-Only (2026-02-18, network-change-reconnect)

**Decision**: 2s polling via `_k2.run('status')` remains the sole source of UI state updates. Network change events (Android `vpnStateChange`, iOS `EventBridge` transient states) are logged for debug observability only. No event-driven store updates.

**Context**: The `reconnecting` transient state (emitted by `engine.OnNetworkChanged()`) is microsecond-duration — engine stays `StateConnected` throughout. The UI would never actually see it via any polling interval.

**Why not event-push hybrid**:
- Dual-channel (event + poll) creates timing consistency problems — debounce logic was designed for single source
- Polling's self-healing property (every 2s gets ground truth from backend) is more reliable than event + missed-event recovery
- Fast network transitions (<5s) are user-invisible — no `reconnecting` UI flash needed
- Long disconnections (>5s) produce `error` state that existing polling catches via `error synthesis` in bridge

**Implementation**:
- iOS `EventBridge.onStateChange`: added `else { NSLog("[K2:NE] transient state: \(state)") }` — `reconnecting` + `connected` logged, not propagated to App process
- Android `capacitor-k2.ts`: `console.debug('[K2:Capacitor] vpnStateChange:', event.state, ...)` — structured log, VPN store not touched

**Future consideration**: If real-time bandwidth stats or sub-second latency display are needed, design a complete event-push architecture from scratch. Do NOT incrementally extend the debug log listeners into a hybrid.

**Validating tests**: `k2/engine/engine_test.go` — `TestEngine_NetworkMonitor_CallbackTriggersOnNetworkChanged` (verifies `OnNetworkChanged` called); AC9 verified manually (VPN store not updated by events).

---

## Android onLost: Immediate Call, No Debounce (2026-02-18, network-change-reconnect)

**Decision**: `K2VpnService.registerNetworkCallback()` `onLost` override calls `engine.onNetworkChanged()` immediately (no debounce). `onAvailable` retains 500ms debounce.

**Rationale**:
- `onLost`: Network is already gone. There is no benefit to waiting — the connection is dead. Clearing QUIC/TCP-WS cached connections immediately allows faster lazy-reconnect when the next network arrives.
- `onAvailable`: New network may trigger multiple callbacks during interface stabilization (routing table updates, DHCP). 500ms debounce absorbs the storm and reconnects once.

**Implementation**:
```kotlin
override fun onAvailable(network: Network) {
    pendingNetworkChange?.let { mainHandler.removeCallbacks(it) }
    val runnable = Runnable { engine?.onNetworkChanged() }
    pendingNetworkChange = runnable
    mainHandler.postDelayed(runnable, 500)
}
override fun onLost(network: Network) {
    Log.d(TAG, "Network lost, clearing cached connections")
    pendingNetworkChange?.let { mainHandler.removeCallbacks(it) }
    engine?.onNetworkChanged()  // Immediate, cancels pending debounce too
}
```

**Key invariant**: `onLost` cancels any pending `onAvailable` debounce before calling immediately. This prevents double-reconnect if onLost fires during the onAvailable debounce window.

**Validating tests**: Manual device testing — airplane mode on/off cycle; WiFi drop with 4G available.

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

## Dual-CDN Manifest with Relative URLs — Single Source of Truth (2026-02-18, updater-android-router)

**Decision**: K2Plugin (iOS/Android) uses an ordered endpoint array for manifest fetching. Manifest `url` fields use relative paths. Client resolves full download URL by prepending the base URL of whichever CDN endpoint returned the manifest.

**Endpoint arrays** (CloudFront first, S3 fallback):
```swift
// iOS
private let webManifestEndpoints = [
    "https://d13jc1jqzlg4yt.cloudfront.net/kaitu/web/latest.json",
    "https://d0.all7.cc/kaitu/web/latest.json"
]
```
```kotlin
// Android
private val ANDROID_MANIFEST_ENDPOINTS = listOf(
    "https://d13jc1jqzlg4yt.cloudfront.net/kaitu/android/latest.json",
    "https://d0.all7.cc/kaitu/android/latest.json"
)
```

**fetchManifest(endpoints) returns (data, baseURL)**: Try each endpoint in order (10s timeout). First success returns both the manifest JSON and the base URL of that CDN endpoint. All subsequent download URLs are resolved relative to that base URL.

**resolveDownloadURL(url, baseURL)**: If `url` starts with `http://` or `https://`, use as-is (backward compat). Otherwise: `baseURL + url`. Example: manifest from CloudFront with `url: "0.5.0/webapp.zip"` → `https://d13jc1jqzlg4yt.cloudfront.net/kaitu/web/0.5.0/webapp.zip`.

**Why relative URLs over duplicate manifests**: Desktop uses two absolute-URL manifests (`cloudfront.latest.json`, `d0.latest.json`) — they can drift. One relative manifest = one source of truth per channel. CloudFront is a read-through cache of the same S3 bucket, so one upload propagates everywhere automatically.

**S3 layout**: `{channel}/latest.json` (single file) + `{channel}/{version}/{artifact}` (versioned artifact).

**Contrast with desktop**: Desktop Tauri updater uses Tauri's built-in `endpoints` array with absolute URLs in each manifest — a different approach for a different system. Mobile rolls its own because K2Plugin is custom code.

**Validating tests**: `test_resolveDownloadURL_relative`, `test_resolveDownloadURL_absolute_passthrough` (manual iOS/Android); `scripts/test-publish-mobile.sh` tests 4 and 6 verify relative URL format in generated manifests.

---

## Mobile Auto-Update Cold Start Check Pattern (2026-02-18, updater-android-router)

**Decision**: K2Plugin fires auto-update check in `load()` with a 3-second delay. Checks native update first (Android: download APK silently; iOS: emit event), then web OTA (silent download+apply). From background resume, no check.

**iOS implementation**:
```swift
override func load() {
    DispatchQueue.main.asyncAfter(deadline: .now() + 3.0) { [weak self] in
        self?.performAutoUpdateCheck()
    }
}
```

**Android implementation**:
```kotlin
override fun load() {
    android.os.Handler(android.os.Looper.getMainLooper()).postDelayed({
        Thread { performAutoUpdateCheck() }.start()
    }, 3000)
}
```

**Sequencing — native before web OTA**: New native version may contain incompatible web changes. Installing native update is a complete app replace. If native update found on Android, web OTA is skipped (it would be overwritten anyway).

**Why 3 seconds**: Startup UI must be interactive before any background work begins. 3s is enough for VPN state poll, auth check, and initial render to complete.

**Why cold start only**: Background resume frequency is high (every app foreground). Manifest fetches consume data. Web OTA cannot take effect until next cold start anyway. Native update modals during resume are disruptive.

**Why no periodic polling**: Mobile app lifecycle is short — killed by system frequently. Cold start check is equivalent to periodic polling at the user's natural usage cadence.

**Event names**:
- `nativeUpdateReady` — Android APK fully downloaded, ready to install. Payload: `{version, size, path}`
- `nativeUpdateAvailable` — iOS new version found. Payload: `{version, appStoreUrl}`

Web OTA is silent (no event emitted). Download, verify sha256, extract — next cold start uses new webapp.

**Validating tests**: `test_autoCheck_triggered_on_load`, `test_autoCheck_native_before_web` (manual device); `webapp/src/services/__tests__/capacitor-k2.test.ts` — `test_updater_handles_nativeUpdateReady_event`, `test_updater_handles_nativeUpdateAvailable_event`.

---

## Two-Phase Mobile Release: CI Uploads Artifacts, Human Publishes latest.json (2026-02-18, updater-android-router)

**Decision**: CI (`build-mobile.yml`) uploads artifacts to versioned S3 paths but never updates `latest.json`. Operators run `make publish-mobile VERSION=x.y.z` after verifying artifacts to publish the version pointer.

**Phase 1 (CI, automatic on v* tag)**:
```
s3://kaitu-releases/android/{version}/Kaitu-{version}.apk
s3://kaitu-releases/web/{version}/webapp.zip
```
`latest.json` unchanged — users see no update.

**Phase 2 (manual, after verification)**:
```bash
make publish-mobile VERSION=0.5.0
```
Script validates artifacts exist on S3, downloads to compute sha256+size, generates `latest.json` with relative URL, uploads.

**Why two-phase**: Same principle as desktop `publish-release.sh`. Artifacts may have last-minute issues found during QA. The gate between "built" and "deployed" is human intent, not CI automation.

**scripts/publish-mobile.sh key behaviors**:
- `--s3-base=PATH` flag: use local filesystem instead of real S3 (enables `scripts/test-publish-mobile.sh` to run without AWS)
- `--dry-run` flag: print what would be uploaded without uploading
- `set -euo pipefail`: exit immediately on any failure
- `trap 'rm -rf "$WORK_TMPDIR"' EXIT`: always clean up temp files

**iOS skipped**: iOS publish is App Store Connect review — no APK to hash. iOS `latest.json` only contains `{version, appstore_url, released_at}` — no hash/size. This is generated separately (or by a future iOS-specific publish step).

**Validating tests**: `scripts/test-publish-mobile.sh` — 10 tests covering script existence, missing-artifact exit code, JSON field presence, relative URL format, sha256 prefix, version consistency, CI workflow S3 references.

---

## Next.js Website: Terminal Dark Forced Theme (2026-02-21, website-k2-redesign)

**Decision**: The `web/` Next.js website uses a forced dark-only "Terminal Dark" color scheme. `EmbedThemeProvider` sets `defaultTheme="dark"` with no `enableSystem` prop. The `ThemeToggle` component is deleted. CSS variables in `:root` are set to dark values directly — no `.dark {}` conditional block needed.

**Terminal Dark color palette** (`web/src/app/globals.css`):
```css
:root {
  --background: #0a0a0f;      /* Deep black */
  --foreground: #e0e0e0;      /* Light grey text */
  --card: #111118;             /* Card background */
  --primary: #00ff88;          /* Terminal green — primary accent */
  --secondary: #00d4ff;        /* Cyan — secondary accent */
  --muted: #1a1a22;
  --muted-foreground: #666;
  --border: rgba(0, 255, 136, 0.15);  /* Green glow border */
}
```

**Scope**: Affects `[locale]` public pages only. `(manager)` admin dashboard has its own independent theme and is unaffected.

**Why forced dark**: Brand decision for the k2 protocol — "terminal hack aesthetic" consistent with the security/stealth positioning. Reduces code complexity (no `dark:` variants, no theme toggle state).

**Cross-reference**: See Architecture Decisions → "Dark-Only Theme with MUI + Tailwind v4 Design Tokens" for the webapp (VPN client) equivalent decision.

**Validating tests**: `web/src/lib/__tests__/theme.test.ts` — `test_theme_provider_forces_dark`, `test_header_no_theme_toggle`, `test_css_variables_terminal_dark`
**Source**: website-k2-redesign (2026-02-21)

---

## Velite Content: order + section Frontmatter for Sidebar Navigation (2026-02-21, website-k2-redesign)

**Decision**: Extend Velite's post schema with two optional frontmatter fields: `order: number` (sidebar sort weight) and `section: string` (sidebar grouping key). Content files at `web/content/{locale}/k2/*.md` use these fields to drive the `/k2/` sidebar navigation without any hardcoded sidebar configuration.

**Schema extension** (`web/velite.config.ts`):
```typescript
order: s.number().optional(),
section: s.string().optional(),
```

**Content file path convention**: `web/content/{locale}/k2/{name}.md` → Velite slug `k2/{name}` → URL `/{locale}/k2/{name}`.

**getK2Posts() helper** (`web/src/lib/k2-posts.ts`): Single shared function that filters posts by locale + `k2/` slug prefix, sorts by `order` ascending (undefined → Infinity, sorts last), and groups by `section`. Used by `K2Sidebar`, `K2Page`, and `sitemap.ts` — no repeated filter logic.

**Section values used**:
- `"getting-started"` — index, quickstart, server, client
- `"technical"` — protocol, stealth
- `"comparison"` — vs-hysteria2

**Sidebar i18n**: Section labels are translated via `messages/{locale}/k2.json` namespace and passed as props to `K2Sidebar` (Client Component) from the Server Component layout.

**Why content-driven over hardcoded config**: Adding a new doc page requires only a new markdown file — no code changes to sidebar config, no route changes. Velite rebuilds at next `yarn build`.

**Validating tests**: `web/tests/k2-route.test.ts` — `test_velite_schema_accepts_order_section`, `test_k2_sidebar_groups_by_section`; `web/tests/k2-content.test.ts` — `test_k2_content_has_required_frontmatter`
**Source**: website-k2-redesign (2026-02-21)

---

## Next.js SSR/SSG: Homepage CSR→Server Component Conversion (2026-02-21, website-k2-redesign)

**Decision**: The homepage (`web/src/app/[locale]/page.tsx`) is converted from CSR (`"use client"` + `useTranslations()`) to a Server Component (`async function` + `getTranslations()` from `next-intl/server`) with `export const dynamic = 'force-static'`. This ensures build-time static HTML generation, CDN distribution, and full crawlability by search engines and AI agents that don't execute JavaScript.

**Pattern** (reference: `[...slug]/page.tsx` was already a Server Component in production):
```tsx
// Before (CSR — SEO harmful)
"use client";
import { useTranslations } from 'next-intl';
export default function Home() {
  const t = useTranslations();
}

// After (SSG — SEO friendly)
import { getTranslations, setRequestLocale } from 'next-intl/server';
export const dynamic = 'force-static';
export default async function Home({ params }: { params: Promise<{ locale: string }> }) {
  const { locale } = await params;
  setRequestLocale(locale as (typeof routing.locales)[number]);
  const t = await getTranslations();
}
```

**Client components extracted**: `HomeClient.tsx` wraps any interactive DOM work needed at the homepage level. Header stays a Client Component (auth state, language toggle) but is composed inside the Server Component page via React composition pattern.

**Why `force-static`**: Prevents Amplify (`WEB_COMPUTE` / Next.js SSR mode) from routing the homepage through Lambda. Static HTML is served directly from CloudFront CDN at zero Lambda cost.

**AC1 verification criteria** (must pass before other ACs proceed):
1. `yarn build` → homepage is `.html` in build output (not Lambda route)
2. `curl http://localhost:3000/zh-CN/ | grep -c 'k2\|隐身\|隧道'` → returns > 0

**Validating tests**: `web/tests/homepage-ssr.test.ts` — `test_homepage_ssr_renders_content`, `test_homepage_generates_metadata`
**Source**: website-k2-redesign (2026-02-21)

---

## Store Pre-Built URLs, Not Decomposed Components (2026-02-20, k2v5-tunnel-expression)

**Decision**: Sidecar constructs the complete `k2v5://` connection URL from `connect-url.txt` + config (domain, port, hop range), then stores the full URL as `SlaveTunnel.ServerURL`. API returns it directly. Replaced the prior v1 approach of decomposing into `CertPin` + `ECHConfigList` fields and reassembling on API response.

**Why store full URL over decomposed fields**:
- Decompose-store-reassemble adds complexity with zero benefit — the URL is never queried by its parts
- Single `ServerURL` field vs two fields (`CertPin`, `ECHConfigList`) — simpler model
- `buildK2V5ServerURL()` server-side function deleted — no runtime computation on API read
- Sidecar already has all the context to build the URL (connect-url.txt + configured domain/port/hop)

**URL construction** (`sidecar.BuildServerURL()`):
1. Parse connect-url.txt → extract `ech` and `pin` query params
2. Strip auth credentials (`udid:token@`) and dev flags (`insecure=1`)
3. Use sidecar-configured domain/port (not source URL's host:port)
4. Append hop range if configured
5. Result: `k2v5://domain:port?ech=xxx&pin=sha256:xxx[&hop=start-end]`

**Pattern applicability**: When a downstream consumer needs a formatted value, build it at the source (where all context is available) rather than storing raw components and formatting on read. Especially true when:
- The formatted value is never queried by its parts
- The source has all necessary context
- Multiple fields would be needed to store components

**Validating tests**: `docker/sidecar/sidecar/connect_url_test.go` — 8 tests (`TestBuildServerURL_Full`, `_NoHop`, `_NoECH`, `_OverridesDomainPort`, `_InvalidURL`, `_EmptyString`, `_NoParams`, `TestTunnelConfig_MarshalWithServerURL`); `api/api_tunnel_test.go` — `TestUpsertTunnel_K2V5WithServerURL`, `TestApiK2Tunnels_K2V5HasServerUrl`

---

## Three-Layer Config Chain: ClientConfig → engine.Config → ProviderConfig (2026-02-20, k2-service-call-requirements)

**Decision**: TUN device configuration (IPv4/IPv6 addresses) flows through the three-layer config chain without defaults injection at intermediate layers. Only the final consumer (provider's `tun_desktop.go`) applies defaults.

**Chain**:
1. `config.ClientConfig.Tun` (`TunConfig{IPv4, IPv6}`) — user-facing, YAML/JSON
2. `engine.Config.TunIPv4` / `TunIPv6` — internal, flat strings
3. `provider.ProviderConfig.IPv4Address` / `IPv6Address` — platform-specific

**Mapping points**:
- `daemon.engineConfigFromClientConfig()` maps `cfg.Tun.IPv4` → `ecfg.TunIPv4`
- `engine.Start()` maps `cfg.TunIPv4` → `ProviderConfig{IPv4Address: ...}`
- `tun_desktop.go` applies defaults: empty → `defaultIPv4Address` (`198.18.0.7/15`)

**Why defaults only at final layer**: Intermediate layers should pass empty values through unchanged. Provider's `Start()` method is the only place that knows the right platform default. Mobile uses platform-provided TUN (no address needed). Desktop uses `198.18.0.7/15`. A hypothetical future platform might use different defaults.

**Validating tests**: `k2/config/config_test.go` — `TestTunConfig_JSONRoundTrip`, `TestClientConfig_WithTun_YAML`, `TestClientConfig_EmptyTun_Defaults`; `k2/provider/provider_test.go` — `TestProviderConfig_TunAddresses_Preserved`, `TestProviderConfig_TunAddresses_Empty`; `k2/daemon/daemon_test.go` — `TestEngineConfigFromClientConfig_TunFields`, `TestEngineConfigFromClientConfig_TunDefaults`, `TestEngineConfigFromClientConfig_ProxyModeUnchanged`

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

## Desktop Window Management: Hidden→Size→Show Lifecycle (2026-02-20, desktop-window-management)

**Decision**: Window created hidden (`visible: false` in `tauri.conf.json`), dynamically sized based on screen in Rust setup, then shown only after frontend JavaScript completes initialization via `invoke('show_window')` IPC call.

**Three-phase lifecycle**:
1. **Setup (Rust)**: Parse `--minimized` → `window::init_startup_state()`. If not minimized: `window::adjust_window_size()` calculates optimal size from monitor (80% screen height, 9:20 aspect ratio, capped at 85% max height), centers window. Window remains hidden.
2. **Frontend init (JS)**: `injectTauriGlobals()` runs → injects `_k2` + `_platform` → calls `invoke('show_window')` at the end. This ensures CSS, React, and MUI theme are loaded before the window appears.
3. **Runtime**: Close button → `api.prevent_close()` + `window::hide_window()`. Tray click/dock click → `window::show_window_user_action()`. Second instance → unminimize + show + focus.

**Why hidden-first**: Prevents two types of visual flash:
- **Size flash**: Window appears at `tauri.conf.json` default size (430×956), then resizes to fit screen. User sees a jump.
- **Content flash**: Window appears before React renders → white/empty window for ~200ms on slower machines.

**Platform-specific hide/show behavior**:
- **Windows**: `minimize()` instead of `hide()` — preserves taskbar icon. `set_always_on_top(true/false)` trick to bring window to front.
- **macOS/Linux**: `hide()` / `show()` — standard behavior. `RunEvent::Reopen` handles dock icon click.

**--minimized autostart**: `tauri-plugin-autostart` passes `Some(vec!["--minimized"])`. On autostart boot, window is never shown — user clicks tray icon to open. `IS_MINIMIZED_START` AtomicBool tracks this; `show_window_user_action()` clears it so future auto-shows work.

**Files**: `desktop/src-tauri/src/window.rs` (196 lines, self-contained), `desktop/src-tauri/src/main.rs` (setup + RunEvent handlers), `webapp/src/services/tauri-k2.ts` (show_window IPC at end of injection)

**Validating tests**: `cargo check` passes; `webapp/src/services/__tests__/tauri-k2.test.ts` — all 29 tests pass (show_window is part of injection flow). Manual: `make dev` — window appears correctly sized, no flash.

---

## Pre-Built Binary Dockerfiles for GFW Environments (2026-02-20, publish-docker)

**Decision**: Docker images for k2v5 and k2-sidecar use pre-built Go binaries instead of multi-stage builds with `golang:` base image. Binaries are cross-compiled locally (`CGO_ENABLED=0 GOOS=linux GOARCH=amd64`), then `COPY`'d into Alpine containers.

**Supersedes**: k2-sidecar's original multi-stage Dockerfile that used `FROM golang:1.22-alpine AS builder` + `go build` inside Docker.

**Why pre-built over multi-stage**:
- Docker Hub is unreachable behind GFW — `golang:1.22-alpine` cannot be pulled
- Even with mirrors, multi-stage builds are fragile (mirror auth issues, `--platform` incompatibility)
- Go cross-compilation is reliable and fast locally
- Smaller attack surface: final image only has Alpine + binary, no Go toolchain
- Build reproducibility: same binary tested locally is exactly what ships

**Pipeline** (`scripts/publish-docker.sh`):
```
Step 1: CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build → binary
Step 2: docker build --platform linux/amd64 (COPY binary into alpine:3.20)
Step 3: docker push to ECR
```

**Dockerfile pattern**:
```dockerfile
FROM alpine:3.20
RUN apk add --no-cache ca-certificates
COPY pre-built-binary /usr/local/bin/
ENTRYPOINT ["/usr/local/bin/pre-built-binary"]
```

**Trade-off**: Requires Go toolchain on build machine (not just Docker). Acceptable because k2 developers always have Go installed.

**`.gitignore` alignment**: Both `docker/k2s/.gitignore` and `docker/sidecar/.gitignore` ignore the built binaries (`k2s`, `k2-sidecar`) — they're build artifacts, not source.

**Validating tests**: `make publish-docker` succeeds; remote `docker compose up` starts all services.

---

## Viewport Scaling: CSS Transform for Narrow Desktop Windows (2026-02-20, desktop-window-management)

**Decision**: When the Tauri desktop window is narrower than 430px design width (e.g., Windows 1080p laptops, small screens), scale the entire UI proportionally using CSS `transform: scale()` on `<body>`.

**Why body, not #root**: MUI Portals (Dialogs, Popovers, Snackbars) render as direct children of `<body>`, outside `#root`. Scaling `#root` would leave Portal content at full size — misaligned overlays, clipped dialogs. Scaling `<body>` affects everything.

**Implementation** (`webapp/src/main.tsx`, Tauri-only):
```typescript
const DESIGN_WIDTH = 430;
function setupViewportScaling() {
  function applyScale() {
    const scale = Math.min(window.innerWidth / DESIGN_WIDTH, 1); // Never scale up
    if (scale < 1) {
      document.body.style.width = `${DESIGN_WIDTH}px`;
      document.body.style.height = `${window.innerHeight / scale}px`;
      document.body.style.transform = `scale(${scale})`;
      document.body.style.transformOrigin = "top left";
    } else {
      // Clear all scaling styles
    }
  }
  applyScale();
  window.addEventListener("resize", applyScale);
}
```

**Height calculation**: `windowHeight / scale` — compensates for the CSS transform shrinking the visible area. Without this, content at the bottom would be cut off.

**Tauri-only**: Called inside `if (window.__TAURI__)` block. Capacitor (mobile) has fixed viewport set by the OS. Standalone (web) runs in a browser where users control window size via browser zoom.

**Supporting CSS** (`webapp/index.html`):
- `width: 100%` on html/body — explicit for scaling calculations
- `background: #0f0f13` on html/body — dark background prevents white flash
- `overflow: hidden` + `display: flex; flex-direction: column` on `#root` — prevents scroll and ensures flex layout

**Validating tests**: `npx tsc --noEmit` passes; all 305 vitest tests pass. Manual: resize Tauri window below 430px → UI scales smoothly.

---

## MCP + Skill Layered Architecture: Atomic Tools + Knowledge File (2026-02-20, kaitu-ops-mcp)

**Decision**: MCP server provides atomic tool capabilities (what the model can do) + technical security via stdout redaction. A companion Skill file encodes domain knowledge and operational safety guardrails (how the model should behave).

**Two layers**:
- **MCP layer** (`tools/kaitu-ops-mcp/`): TypeScript + `@modelcontextprotocol/sdk`. Two tools: `list_nodes` (Center API discovery) + `exec_on_node` (SSH execution). Stdout redaction runs automatically before every return — technical security backstop.
- **Skill layer** (`.claude/skills/kaitu-node-ops.md`): YAML front-matter with `triggers:` array for topic-based activation. Encodes: dual-architecture identification flow, container dependency chain, `.env` variable semantics, standard operations table, 7 safety guardrails, script execution modes.

**Why split, not merge**:
- MCP tool descriptions are short strings (1–2 sentences) — cannot carry operational knowledge
- Skill file is unstructured prose with markdown tables — cannot enforce technical behavior
- MCP = hands (capability + redaction safety), Skill = experience (knowledge + behavioral guardrails)
- Skill guardrails are best-practice guides, not security boundaries. MCP redaction provides the actual security backstop.

**Skill positioning (important)**: Skill is explicitly NOT a security boundary — admin has full SSH root access. Rules like "never read K2_NODE_SECRET" are *accidental damage prevention*, not access control. The MCP layer's `redactStdout()` is the technical guarantee.

**Tool registration pattern** (TypeScript + `@modelcontextprotocol/sdk`):
```typescript
export function registerListNodes(server: McpServer, apiClient: CenterApiClient): void {
  server.tool('list_nodes', 'description', { country: z.string().optional(), name: z.string().optional() }, async (params) => {
    const raw = await apiClient.request('/app/nodes/batch-matrix')
    const nodes = filterNodes(raw, params)
    return { content: [{ type: 'text', text: JSON.stringify(nodes, null, 2) }] }
  })
}
```

**Entry point guard** (`index.ts` pattern):
```typescript
const isEntryPoint = process.argv[1] !== undefined &&
  import.meta.url === new URL(`file://${process.argv[1]}`).href
if (isEntryPoint) { main().catch(...) }
```
Prevents `main()` from running when module is imported by tests. Required for `createServer(config)` to be testable.

**Validating tests**: `tools/kaitu-ops-mcp/src/tools/list-nodes.test.ts` (6 tests — AC1), `tools/kaitu-ops-mcp/src/tools/exec-on-node.test.ts` (AC2–5), `tools/kaitu-ops-mcp/src/redact.test.ts` (AC4), `tools/kaitu-ops-mcp/src/config.test.ts` (AC6–8)

---

## SSH Execution Module: ssh2 Library Pattern (2026-02-20, kaitu-ops-mcp)

**Decision**: SSH command execution uses the `ssh2` npm library. Single shared `_sshExecCore()` function handles both plain exec and stdin-piped execution. Each tool call creates a new connection — no connection pool.

**Why no connection pool**: Tool calls happen at human interaction pace (one per response cycle). Connection pool complexity (keep-alive detection, stale connection handling) outweighs benefits. Reconnect on every call is simpler and more robust.

**Key implementation details**:
- `settle()` guard function prevents double-resolution: `if (settled) return; settled = true`
- Error classification at `client.on('error')`: `err.level === 'client-authentication'` → auth failed; `err.code === 'ECONNREFUSED'` → connection refused; else generic
- Timeout via `setTimeout` → channel `destroy()` or `close()` → resolve with `exitCode: -1`
- stdin pipe: `channel.write(stdinData, 'utf-8', () => { channel.end() })` — write then close stdin

**stdin pipe vs heredoc**: Binary-safe stdin pipe avoids shell escaping issues entirely. Heredoc requires escaping `$`, backticks, single quotes — AI-generated scripts frequently break. Pattern: local file → `sshExecWithStdin(ip, config, 'bash -s', fileContent)`.

**Error signals from ssh2**: `err.level` property (not `err.code`) distinguishes auth failures. `err.level === 'client-authentication'` is the ssh2-specific value. Generic connection errors use `err.code === 'ECONNREFUSED'` or string match on `err.message`.

**Validating tests**: `tools/kaitu-ops-mcp/src/ssh.test.ts` — mock SSH server tests (AC2, AC5, AC9); `tools/kaitu-ops-mcp/src/tools/exec-on-node.test.ts` — truncation, redaction integration (AC3, AC4)

---
