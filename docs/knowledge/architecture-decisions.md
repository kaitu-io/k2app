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

## Tauri Desktop Bridge: IPC + HTTP Plugin + Fetch Override (2026-02-17, tauri-desktop-bridge)

**Decision**: Tauri desktop injects `window._k2` and `window._platform` from the webapp side (not Rust initialization scripts), using Tauri IPC for daemon communication and `@tauri-apps/plugin-http` for external HTTPS.

**Three-layer approach**:
1. **Daemon calls** (`_k2.run()`) -> `invoke('daemon_exec')` -> Rust `spawn_blocking` -> `reqwest` to `127.0.0.1:1777`
2. **Cloud API** (`cloudApi.request()`) -> patched `window.fetch` -> HTTP plugin `fetch()` -> Rust HTTP client
3. **Platform info** (`_platform.os`, `.version`) -> `invoke('get_platform_info')` -> Rust `std::env::consts::OS`

**Why IPC for daemon (not direct fetch)**: WebView on `localhost:14580` cannot fetch `http://127.0.0.1:1777` -- different port = cross-origin, daemon has no CORS headers. IPC bypasses WebView entirely.

**Why HTTP plugin for cloud API (not native fetch)**: WebKit WKWebView blocks external HTTPS fetch from HTTP localhost origin. The HTTP plugin makes requests from the Rust process, bypassing WebView restrictions.

**Why webapp-side injection (not Rust init script)**: Rust `webview.eval_script()` cannot use ES module imports (`@tauri-apps/plugin-http`). Webapp dynamic import + `window.__TAURI__` detection provides full TypeScript type safety and access to npm packages.

**Fetch override strategy**: `patchFetchForTauri()` replaces `window.fetch` -- local URLs (relative, `127.0.0.1`, `localhost`) use native fetch; external HTTPS routes through `tauriFetch`. This is transparent to `cloudApi.request()` (no cloudApi changes needed).

**Detection order in `main.tsx`**: `window.__TAURI__` -> Tauri bridge; `!_k2 || !_platform` -> standalone fallback; else -> host-injected (Capacitor).

**Validating tests**: `webapp/src/services/__tests__/tauri-k2.test.ts` -- 11 tests (IPC delegation, fetch routing, platform detection, standalone regression)

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
