# Framework Gotchas

Platform-specific issues and workarounds discovered during implementation.

---

## WebKit Mixed Content Blocking on macOS (2026-02-14, k2app-rewrite)

**Problem**: macOS WebKit blocks `https://` → `http://` requests even for loopback. Tauri default origin `https://tauri.localhost` cannot call daemon at `http://127.0.0.1:1777`.

**Root cause**: WebKit enforces mixed content strictly (no loopback exception). Chromium (Windows) allows it.

**Solution**: `tauri-plugin-localhost` serves webapp from `http://localhost:14580`. HTTP→HTTP calls are allowed. See `desktop/src-tauri/src/main.rs` — `Builder::new(14580).build()`.

**Security**: localhost port accessible to local processes, but daemon already exposes :1777 (CORS protected). Attack surface unchanged.

**Applies to**: macOS + Linux WebKitGTK. Not Windows (Chromium-based).

**Validation**: fetch `/ping` from webview succeeds; no console mixed content errors.

---

## Go json.Marshal snake_case vs JavaScript camelCase (2026-02-14, mobile-rewrite)

**Problem**: Go `json.Marshal` outputs `connected_at`, TypeScript expects `connectedAt`. Raw Go JSON passed through native bridges causes silent `undefined` values — no runtime error, just missing data.

**Discovery**: Code review caught mismatch between Go `Engine.StatusJSON()` output keys and `K2PluginInterface` TypeScript definitions.

**Solution**: `remapStatusKeys()` in both K2Plugin.swift and K2Plugin.kt transforms keys at the native bridge boundary. Convention added to CLAUDE.md.

**Prevention rule**: When passing Go JSON to JavaScript, always remap keys at the bridge.

**Cross-reference**: See Bugfix Patterns → "Go→JS JSON Key Mismatch" for discovery details. See Architecture Decisions → "Go→JS JSON Key Remapping" for the decision rationale.

---

## .gitignore Overbroad Patterns Hide Source Files (2026-02-14, mobile-rewrite)

**Problem**: `.gitignore` patterns like `mobile/ios/` ignore ALL files including source files.

**Symptom**: Source files created by agents invisible to git. `git status` shows nothing. Completely silent.

**Solution**: Replace directory patterns with targeted build artifact patterns (`Pods/`, `build/`, `.gradle/`, `libs/`). Convention added to CLAUDE.md.

**Verification**: `git check-ignore <source-file>` should return nothing.

**Cross-reference**: See Bugfix Patterns → "Overbroad .gitignore" for the original discovery.

---

## Capacitor Plugin Loading: registerPlugin, Not npm Import (2026-02-16, android-aar-fix)

**Problem**: Dynamic `import('k2-plugin')` fails at runtime in Capacitor WebView. The npm package installs native bridge code but isn't a standard ES module that WebView can resolve.

**Solution**: Use `registerPlugin('K2Plugin')` from `@capacitor/core`. Capacitor's native loader registers plugins at app startup; JS side just calls `registerPlugin(name)` to get the bridge proxy.

**Previous approach** (2026-02-14): Variable indirection `const pluginModule = 'k2-plugin'; await import(pluginModule)` — this only worked during Vite dev but broke in production WebView.

**Current approach**: `const { registerPlugin } = await import('@capacitor/core'); const K2Plugin = registerPlugin('K2Plugin');` — works in all environments.

**Trade-off**: Type safety maintained via `as any` cast + local `K2PluginInterface` in `native-client.ts`.

**Cross-reference**: See Bugfix Patterns → "Capacitor registerPlugin vs npm Dynamic Import"

---

## gomobile Swift Bridging: Throws, Not NSError Out-Parameter (2026-02-16, android-aar-fix)

**Problem**: gomobile generates ObjC methods with `NSError**` out-parameter (e.g., `start:fd:error:`). Swift automatically bridges these to throwing methods (`start(_:fd:) throws`). Writing code using the ObjC-style error pattern causes compile errors.

**Correct Swift usage**:
```swift
do {
    try engine?.start(wireUrl, fd: Int(fd))
} catch {
    // handle error
}
```

**Wrong Swift usage** (ObjC style):
```swift
var error: NSError?
engine?.start(wireUrl, fd: Int(fd), error: &error)  // COMPILE ERROR
```

**Applies to**: All gomobile-generated Go methods that return `error`. Swift bridges them as `throws`.

**Cross-reference**: See Bugfix Patterns → "gomobile Swift API Uses Throws Pattern"

---

## iOS Extension Targets Don't Inherit Project Version Settings (2026-02-16, android-aar-fix)

**Problem**: Extension target's Info.plist uses `$(CURRENT_PROJECT_VERSION)` and `$(MARKETING_VERSION)`, but these expand to empty strings because the extension target doesn't inherit them from the project-level build settings.

**Symptom**: `CFBundleVersion` is null in built appex → device refuses to install ("does not have a CFBundleVersion key with a non-zero length string value").

**Fix**: Explicitly set `CURRENT_PROJECT_VERSION` and `MARKETING_VERSION` in the extension target's build settings (both Debug and Release configurations).

**Prevention**: When adding extension targets, always verify version build settings are set per-target, not just at project level.

---

## Tauri Version Reference from Parent package.json (2026-02-14, k2app-rewrite)

**Problem**: Tauri `version` must match root `package.json` to prevent drift.

**Solution**: `"version": "../../package.json"` in `desktop/src-tauri/tauri.conf.json`. Tauri CLI resolves paths ending in `.json` and reads the `version` field.

**Gotcha**: Path is relative from `desktop/src-tauri/` to root — hence `../../` not `../`.

---

## Zustand Store Initialization Pattern (2026-02-14→2026-02-17, k2app-rewrite + webapp-v2)

**Problem**: Zustand stores are synchronous — `await` not allowed in `create()` callback.

**Solution**: Separate `init()` async action called during app bootstrap. Store created with initial state; `init()` calls async methods and updates via `set()`.

**Pattern (v2)**: `initializeAllStores()` in `webapp/src/stores/index.ts` calls layout → auth → vpn store init in order. Stores use `init()` action.

**Validating tests**: `webapp/src/stores/__tests__/vpn.store.test.ts`

---

## Git Submodule in Monorepo Workspace (2026-02-14, k2app-rewrite)

**Problem**: k2 is a Git submodule (Go), but yarn workspaces expects package.json in each workspace.

**Solution**: Only include actual yarn packages in workspaces: `["webapp", "desktop", "mobile"]` — NOT `"k2"`. k2 is built via Makefile (`cd k2 && go build`), initialized via `git submodule update --init`.

**CI gotcha**: Private submodule requires SSH agent setup in GitHub Actions workflows.

---

## Service Readiness on Startup (2026-02-14→2026-02-17, k2app-rewrite + webapp-v2)

**Problem**: k2 daemon takes variable time to start after Tauri app launches. Immediate readiness check fails.

**Solution (v2)**: `AuthGate.tsx` wraps all routes — checks service readiness + version match before rendering app content. Replaces the old `ServiceReadiness.tsx` component.

**Validating tests**: `webapp/src/components/__tests__/AuthGate.test.tsx`

---

## Capacitor Bridge is WebView-Level, Not Page-Level (2026-02-16, mobile-debug)

**Discovery**: When navigating from `index.html` (React app) to `debug.html` (standalone) within the same Capacitor WebView via `window.location.href`, `window.Capacitor.Plugins.K2Plugin` remains available. The bridge is injected at WebView initialization, not per-HTML-page.

**Implication**: Any HTML file in the Capacitor `webDir` (or accessible via the dev server) can access native plugins. This enables standalone debug/diagnostic pages that bypass the main app's framework stack entirely.

**Caveat**: The Capacitor `native-bridge.js` must be present. In production builds, Capacitor injects it automatically. In dev mode (livereload), the Vite dev server must serve the page — Capacitor sets `server.url` to the dev server address, so `/debug.html` resolves correctly via Vite multi-page.

**Validating tests**: Manual device testing — debug.html successfully calls K2Plugin methods.

---

## Android VpnService.prepare() Requires Activity Context (2026-02-16, mobile-debug)

**Problem**: `VpnService.prepare(context)` with Application context (from Capacitor `Plugin.context`) returns `null` on Android 15 (API 35), suggesting VPN is "already prepared". But `VpnService.Builder().establish()` then returns `null` — the VPN subsystem didn't actually register the app.

**Solution**: Always use Activity context: `VpnService.prepare(activity)` where `activity` is from Capacitor `Plugin.getActivity()`.

**Why Activity context matters**: `VpnService.prepare()` checks the calling UID against the VPN owner. With Activity context, the system properly associates the VPN consent with the foreground app. Application context may not trigger the correct VPN preparation path on newer Android versions.

**Capacitor pattern for VPN consent**:
```kotlin
val act = activity ?: run { call.reject("No activity"); return }
val prepareIntent = VpnService.prepare(act)
if (prepareIntent != null) {
    startActivityForResult(call, prepareIntent, "vpnPermissionResult")
} else {
    startVpnService(wireUrl)
    call.resolve()
}

@ActivityCallback
private fun vpnPermissionResult(call: PluginCall, result: ActivityResult) {
    if (result.resultCode == Activity.RESULT_OK) {
        startVpnService(call.getString("wireUrl")!!)
        call.resolve()
    } else {
        call.reject("VPN permission denied")
    }
}
```

**Cross-reference**: See Bugfix Patterns → "Android VPN establish() Returns Null Silently"

**Validating tests**: Manual device testing — VPN TUN fd obtained, Go engine starts, state events flow to JS.

---

## iOS NE Engine Errors Invisible to Main App by Default (2026-02-16, ios-vpn-fixes)

**Problem**: When Go Engine in the NE process encounters an error (bad wireUrl, connection timeout, etc.), `EventBridge.onError()` wrote to App Group but never called `cancelTunnelWithError()`. The NE process stayed alive, the system reported VPN as still `.connected` or `.connecting`, and the main app never knew about the error. From JS perspective, `vpnError` events were completely lost.

**Root cause**: Two-process architecture. NE's `onError()` only wrote to UserDefaults. No mechanism existed to push errors to the main app in real-time. The main app only subscribed to `NEVPNStatusDidChange` — but without `cancelTunnelWithError()`, no status change was triggered.

**Solution**: `EventBridge.onError()` now writes error to App Group AND calls `cancelTunnelWithError(error)`. This triggers `NEVPNStatusDidChange → .disconnected` in the main app. K2Plugin's handler reads the `vpnError` key from App Group on `.disconnected` and pushes `vpnError` event to JS.

**Prevention**: In two-process VPN architectures, every error path must trigger `cancelTunnelWithError()` — otherwise the system doesn't know the tunnel failed.

**Files fixed**: `mobile/ios/App/PacketTunnelExtension/PacketTunnelProvider.swift`, `mobile/plugins/k2-plugin/ios/Plugin/K2Plugin.swift`

**Validating tests**: Manual device testing — no test yet.

---

## iOS TUN fd Must Be Acquired After setTunnelNetworkSettings (2026-02-16, ios-vpn-fixes)

**Problem**: Original code acquired TUN fd via `packetFlow.value(forKey: "socket")` before calling `setTunnelNetworkSettings()`. While this often works, the fd is not guaranteed to be valid until network settings are applied. On some iOS versions, `packetFlow` isn't fully initialized until `setTunnelNetworkSettings` completes.

**Solution**: Move `packetFlow.value(forKey: "socket")` into the `setTunnelNetworkSettings` completion handler, after settings are successfully applied. Add `fd >= 0` guard for extra safety.

**Correct order**:
```swift
setTunnelNetworkSettings(settings) { [weak self] error in
    guard error == nil else { completionHandler(error); return }
    guard let fd = self?.packetFlow.value(forKey: "socket") as? Int32, fd >= 0 else { ... }
    try self?.engine?.start(wireUrl, fd: Int(fd))
}
```

**Files fixed**: `mobile/ios/App/PacketTunnelExtension/PacketTunnelProvider.swift`

**Validating tests**: Manual device testing — no test yet.

---

## IPv6 Default Route Prevents DNS Leak in VPN (2026-02-16, ios-vpn-fixes)

**Problem**: Original `NEPacketTunnelNetworkSettings` only configured IPv4 routes. IPv6 traffic bypassed the tunnel entirely — DNS queries over IPv6 could leak the user's real IP address.

**Solution**: Add `NEIPv6Settings` with a ULA address (`fd00::2/64`) and default route (`NEIPv6Route.default()`). This captures all IPv6 traffic into the tunnel. Since the Go engine doesn't handle IPv6, it drops these packets — security (no leak) takes priority over IPv6 reachability.

**Trade-off**: IPv6-only services will not work while VPN is active. This is acceptable because k2 protocol tunnels over IPv4 and most services support IPv4 fallback.

**Files fixed**: `mobile/ios/App/PacketTunnelExtension/PacketTunnelProvider.swift`

**Validating tests**: Manual device testing — no test yet.

---

## Vite define for App Version Injection (2026-02-16, kaitu-feature-migration)

**Problem**: `ForceUpgradeDialog` needs the app version to compare against `appConfig.minClientVersion`. `PlatformApi` interface doesn't expose `version`. Importing `../../package.json` from `src/` fails — `tsconfig.json` `include` is `["src"]` only, and `resolveJsonModule` is not enabled.

**Solution**: Vite `define` in `vite.config.ts`:
```typescript
import pkg from "../package.json";
export default defineConfig({
  define: { __APP_VERSION__: JSON.stringify(pkg.version) },
});
```
In source: `declare const __APP_VERSION__: string;` then use directly.

**Why not runtime API**: Version is a build-time constant, not a runtime platform capability. Tauri/Capacitor each have their own version APIs but they're async and require native runtime. Vite define is sync, available everywhere, zero runtime cost.

**Validating tests**: `npx tsc --noEmit` passes; `ForceUpgradeDialog` renders in component tests.

---

## Capacitor file: Plugin Source Not Auto-Synced to node_modules (2026-02-16, mobile-debug)

**Problem**: Local Capacitor plugins referenced via `"file:./plugins/k2-plugin"` are **copied** (not symlinked) to `node_modules/`. Source edits are invisible to `cap sync` and Gradle. `yarn install` says "Already up-to-date" without detecting file changes.

**Symptom**: Plugin code changes have no effect after rebuild. No error. Multiple wasted deploy cycles.

**Solution**: After editing local plugin source:
```bash
rm -rf node_modules/k2-plugin && yarn install --force
npx cap sync android  # or ios
```

**Prevention**: Before any `cap sync` after plugin edits, always verify:
```bash
diff plugins/k2-plugin/android/.../K2Plugin.kt node_modules/k2-plugin/android/.../K2Plugin.kt
```

**Cross-reference**: See Bugfix Patterns → "Capacitor Local Plugin Stale Copy in node_modules"

---

## Gin Group Middleware Not Invoked for Unregistered HTTP Methods (2026-02-17, webapp-antiblock)

**Problem**: `api.Use(ApiCORSMiddleware())` on a Gin route group does NOT run for OPTIONS preflight requests unless an OPTIONS handler is explicitly registered. Gin skips group middleware entirely for HTTP methods with no matching handler.

**Symptom**: CORS preflight returns 404 (Gin's default) instead of 204 with CORS headers. Browser blocks the actual request. No error in server logs — Gin never enters the middleware chain.

**Solution**: Add explicit OPTIONS catch-all route to the group:
```go
api.OPTIONS("/*path", func(c *gin.Context) {})
```
The middleware aborts with 204 before this handler runs, but the handler's existence makes Gin enter the middleware chain.

**Why this is subtle**: GET/POST/PUT/DELETE all work because they have registered handlers. Only preflight (OPTIONS) is affected. Unit tests that only test GET/POST miss this entirely.

**Validating tests**: `api/middleware_cors_test.go` — `TestApiCORSMiddleware_PreflightReturns204`

---

## Vitest vi.restoreAllMocks() Clears vi.mock() Factory Implementations (2026-02-17, webapp-antiblock)

**Problem**: `vi.restoreAllMocks()` in `afterEach` resets mock implementations set via `vi.mock()` factory at the top of the file. On the next test, the mock returns `undefined` instead of the factory-defined value.

**Symptom**: `'undefined/api/auth/login'` — `resolveEntry()` returned `undefined` instead of `''` because the mock was cleared.

**Solution**: Re-set the default mock value in `beforeEach`:
```typescript
// Top of file
vi.mock('../antiblock', () => ({
  resolveEntry: vi.fn().mockResolvedValue(''),
}));

// In beforeEach — REQUIRED after vi.restoreAllMocks()
beforeEach(() => {
  mockedResolveEntry.mockResolvedValue('');
});
```

**Why this happens**: `vi.mock()` factory runs once at module load. `vi.restoreAllMocks()` restores the original function (before mocking), removing the factory implementation. The mock object still exists, but its implementation is gone.

**Alternative**: Use `vi.clearAllMocks()` instead of `vi.restoreAllMocks()` — it clears call history but preserves implementations. Only use `restoreAllMocks` when you actually need to undo spy wrapping.

**Validating tests**: `webapp/src/services/__tests__/cloud-api.test.ts` — all tests pass after adding `beforeEach` re-mock.

---

## Tauri plugin-shell Replaced by plugin-opener + plugin-clipboard-manager (2026-02-18, platform-interface-cleanup)

**Problem**: `@tauri-apps/plugin-shell` was used for `shell.open(url)` to open external URLs. But `plugin-shell` is a heavy dependency that also provides `Command` (subprocess execution) — unnecessary for just opening URLs. And it provides no clipboard support at all.

**Solution**: Replace with two focused plugins:
- `@tauri-apps/plugin-opener` — `openUrl(url)`. Lightweight, single-purpose.
- `@tauri-apps/plugin-clipboard-manager` — `writeText(text)`, `readText()`. Native OS clipboard.

**Capabilities file update required**: `shell:allow-open` → `opener:default` + `clipboard-manager:allow-write-text` + `clipboard-manager:allow-read-text` in `capabilities/default.json`.

**Cargo.toml update**: Remove `tauri-plugin-shell`, add `tauri-plugin-opener = "2"` + `tauri-plugin-clipboard-manager = "2"`.

**main.rs registration**: `.plugin(tauri_plugin_opener::init())` + `.plugin(tauri_plugin_clipboard_manager::init())`.

**Validating tests**: `webapp/src/services/__tests__/tauri-k2.test.ts` — openExternal, writeClipboard, readClipboard tests

---

## Capacitor @capacitor/browser and @capacitor/clipboard for Mobile Native APIs (2026-02-18, platform-interface-cleanup)

**Problem**: Mobile `capacitor-k2.ts` used `window.open()` for external URLs and `navigator.clipboard` for clipboard — both unreliable in WebView. Android WebView clipboard completely broken. `window.open()` may trigger popup blocker warnings.

**Solution**:
- `@capacitor/browser` — `Browser.open({ url })`. Uses system browser (Safari/Chrome).
- `@capacitor/clipboard` — `Clipboard.write({ string: text })`, `Clipboard.read()`. Native OS clipboard.

**Package.json**: Add to `mobile/package.json` dependencies, not webapp.

**Validating tests**: `webapp/src/services/__tests__/capacitor-k2.test.ts` — openExternal, writeClipboard, readClipboard tests

---

## Tauri v2 Restrictive Mode: Any Capability File Activates It (2026-02-17, tauri-desktop-bridge)

**Problem**: Once ANY capability JSON file exists in `src-tauri/capabilities/`, Tauri v2 switches from permissive mode (all APIs allowed) to restrictive mode (ONLY listed permissions active). A dev-only capability file (`mcp-bridge.json`) silently activated restrictive mode for all builds, blocking permissions not explicitly listed.

**Symptom**: External fetch fails, IPC commands rejected, plugins non-functional -- but only in builds where the capability file is present. No clear error message pointing to capabilities as the cause.

**Solution**: Create a production `default.json` capability file that lists all needed permissions: `core:default`, `shell:allow-open`, `updater:default`, `process:default`, `autostart:default`.

**Prevention**: When adding any capability file (even dev-only), immediately create a companion production capability file listing all permissions the app needs.

**Validating tests**: Runtime verification -- Tauri desktop app loads server list and connects to VPN.

---

## @tauri-apps/plugin-http Static Import Freezes WebKit JS Engine (2026-02-18, tauri-webkit-js-freeze)

**Problem**: `import { fetch as tauriFetch } from '@tauri-apps/plugin-http'` in `tauri-k2.ts` caused WebKit JS engine to completely freeze (main thread deadlock). Even if `tauriFetch` was never called — merely referencing it in a closure was enough to trigger the freeze. DevTools Console became unresponsive. No error logged.

**Discovery**: Binary search isolation — progressively added code back until freeze reproduced. The freeze occurred with any reference to `tauriFetch` in the `patchFetchForTauri()` closure, even behind an unreachable branch.

**Root cause**: Unknown WebKit-specific issue with `@tauri-apps/plugin-http` module initialization. The plugin's JS binding may perform synchronous operations during import that deadlock WebKit's JS engine in certain contexts.

**Resolution**: Deleted `@tauri-apps/plugin-http` entirely. Cloud API already has CORS configured (`ApiCORSMiddleware` allows localhost origins). Native `window.fetch` from `http://localhost:14580` to `https://` URLs works fine — HTTP→HTTPS is an upgrade, not mixed content, and the server sends proper CORS headers.

**Previous incorrect assumption**: "WebKit blocks external HTTPS fetch from HTTP localhost" — this was never verified because the JS freeze prevented any fetch from executing. The real issue was the plugin import, not WebKit's fetch behavior.

**Files deleted/modified**: `tauri-k2.ts` (removed import + patchFetchForTauri), `main.rs` (removed plugin), `Cargo.toml` (removed dep), `capabilities/default.json` (removed permission), `package.json` (removed dep)

**Validating tests**: `webapp/src/services/__tests__/tauri-k2.test.ts` — all tests pass without plugin-http mock

---

## Tauri v2 Command + Plugin Registration: Two Mandatory Steps (2026-02-17, tauri-desktop-bridge)

**Problem**: Defining `#[tauri::command]` or adding a plugin to `Cargo.toml` is not enough. Both require explicit registration in `main.rs` builder chain.

**Commands**: Every `#[tauri::command]` function must appear in `tauri::generate_handler![...]`. Missing commands silently fail — `invoke()` from JS rejects with an error, no Rust-side log. If the rejection happens during bootstrap `await`, the entire app fails to render (white screen).

**Plugins**: Every `tauri-plugin-*` crate in `Cargo.toml` must have a corresponding `.plugin(tauri_plugin_*::init())` call. Missing plugin registration means JS-side `@tauri-apps/plugin-*` imports will fail at runtime (the plugin API endpoint doesn't exist).

**Checklist when adding Tauri functionality**:
1. Add `#[tauri::command]` function in Rust module → add to `generate_handler![]` in `main.rs`
2. Add `tauri-plugin-*` to `Cargo.toml` → add `.plugin()` to builder in `main.rs`
3. Add permission to `capabilities/default.json` if needed
4. `cargo check` to verify compilation

**Why silent**: Tauri v2 doesn't warn about unregistered commands at build time. The mismatch only manifests at runtime when JS calls `invoke()`.

**Cross-reference**: See Bugfix Patterns → "Missing Tauri IPC Handler Registration Causes White Screen"

**Validating tests**: `cargo check`; runtime — all `invoke()` calls succeed.

---

## Engine Config FileDescriptor Discriminates Platform Behavior (2026-02-16, unified-engine)

**Pattern**: `engine.Config.FileDescriptor` uses numeric value to discriminate platform:
- `fd >= 0` → Mobile platform (TUN fd provided by system)
- `fd == -1` → Desktop platform (self-create TUN)

**Why discriminate by fd value**: Avoids adding separate platform enum field. The fd itself is the platform signal. Mobile always has a real fd (>=0); desktop always passes -1 as sentinel.

**Mobile path** (fd >= 0):
- Use provided TUN fd
- Start Provider with DNS middleware (`prov.Start(ctx, &dnsHandler{...})`)
- No route exclusion

**Desktop path** (fd == -1):
- Provider creates TUN device internally
- Start tunnel directly (`tunnel.Start(ctx)`)
- Route exclusion for DNS servers via `Config.DNSExclude`

**Gotcha**: Never pass fd 0 (stdin) from desktop — it triggers mobile path. Always use -1 for "no fd".

**Files**: `k2/engine/engine.go` Start() method branches on `cfg.FileDescriptor >= 0`

**Validating tests**: `engine_test.go` TestEngineStart_MobileConfig vs TestEngineStart_DesktopConfig

---

## Go Wire Layer: Cached Dead Connection Not Cleared on Network Change (2026-02-17, vpn-error-reconnect)

**Problem**: `QUICClient.connect()` and `TCPWSClient.connect()` cache their connections (`c.conn`, `c.sess`) after first success. When the network changes (WiFi→4G), the cached connection dies but `c.conn != nil` — so subsequent `connect()` calls return the dead connection. All new streams fail silently. Engine still reports `"connected"`.

**Why this is the fix shape**: The solution is `ResetConnections()` — close and nil the cached connection. The next `connect()` call (lazy) rebuilds from nil. TUN fd is not affected — it's a kernel interface independent of the physical network path.

**Go implementation pattern** (from vpn-error-reconnect):
```go
// In QUICClient
func (c *QUICClient) ResetConnections() {
    c.mu.Lock()
    defer c.mu.Unlock()
    if c.conn != nil {
        c.conn.CloseWithError(0, "network changed")
        c.conn = nil
    }
    if c.transport != nil {
        c.transport.Close()
        c.transport = nil
    }
    if c.udpMux != nil {
        c.udpMux.Close()
        c.udpMux = nil
    }
    // c.closed stays false — allows future reconnect
}
```

**Key invariant**: `closed=false` must be preserved so the lazy reconnect path in `connect()` is not blocked.

**Resettable interface** for type-safe optional capability:
```go
type Resettable interface { ResetConnections() }
// Usage:
if r, ok := e.wire.(Resettable); ok { r.ResetConnections() }
```

**Validating tests**: `k2/wire/transport_test.go` — 5 subtests verifying reset clears connections, allows lazy reconnect, and preserves closed=false

---

## iOS NWPathMonitor Must Run in NE Process, Not Main App (2026-02-17, vpn-error-reconnect)

**Context**: For mobile VPN network change detection, NWPathMonitor must run in the `PacketTunnelProvider` (Network Extension process), not in the main app. The main app has no direct access to the gomobile engine — it communicates with the NE via `sendProviderMessage()`. The NE process has the engine reference.

**Why NE process**: Engine's `onNetworkChanged()` is a direct method call. Only the process holding the engine reference can call it. On iOS, the engine runs in the NE sandbox (`PacketTunnelProvider`), which is a separate process from the main app.

**Implementation**:
```swift
// In PacketTunnelProvider.swift (NE process)
private let pathMonitor = NWPathMonitor()
private var pendingReconnectItem: DispatchWorkItem?

func startMonitoringNetwork() {
    pathMonitor.pathUpdateHandler = { [weak self] path in
        guard path.status == .satisfied else { return }
        self?.pendingReconnectItem?.cancel()
        let item = DispatchWorkItem { [weak self] in
            self?.engine?.onNetworkChanged()
        }
        self?.pendingReconnectItem = item
        DispatchQueue.global(qos: .utility).asyncAfter(deadline: .now() + 0.5, execute: item)
    }
    pathMonitor.start(queue: .global(qos: .utility))
}
```

**DispatchWorkItem for debounce**: Unlike `DispatchQueue.asyncAfter` (not cancellable), `DispatchWorkItem` can be cancelled. The pattern `pendingItem?.cancel()` + new item + asyncAfter implements a cancellable 500ms debounce.

**Contrast with Android**: Android uses `ConnectivityManager.NetworkCallback` in `K2VpnService` (which also holds the engine reference). Same architectural pattern — the component holding the engine drives reconnect.

**Validating tests**: Manual device testing — VPN maintains connection through WiFi→4G transition.

---

## Android Bans Netlink Sockets for Non-Root Apps (2026-02-17, android-vpn-audit)

**Problem**: sing-tun's `NewNetworkUpdateMonitor()` uses `netlink.RouteSubscribe` + `netlink.LinkSubscribe` on Linux/Android. Google bans netlink sockets for non-root apps — `NewNetworkUpdateMonitor` returns `ErrNetlinkBanned` on Android.

**Implication**: sing-tun's Go-level network monitoring CANNOT be used on Android. The engine layer is blind to network changes on Android. Must use Android-native `ConnectivityManager.NetworkCallback` instead.

**Contrast with desktop**: macOS (`AF_ROUTE`), Linux (`netlink` — works for root/privileged), Windows (`winipcfg`) all work. sing-tun's `DefaultInterfaceMonitor` is viable for desktop reconnection.

**Correct approach for Android network monitoring**: Use `ConnectivityManager.registerNetworkCallback()` in `K2VpnService.kt` with `NetworkRequest.Builder().addCapability(NET_CAPABILITY_INTERNET)`. On `onAvailable()` + `onLost()`, drive engine stop/restart from Kotlin side.

**Cross-reference**: See Architecture Decisions → "sing-tun Network Monitoring: Available But Unused by k2 Engine"

**Tests**: No test yet — discovery from code audit.
**Source**: android-vpn-audit (2026-02-17)
**Status**: verified (confirmed by reading sing-tun source)

---

## Tauri v2 Event Capability: core:event:default, Not event:default (2026-02-18, tauri-updater-and-logs)

**Problem**: Using `listen()` from `@tauri-apps/api/event` to subscribe to Tauri events fails silently when the capability file lists `event:default`. Tauri v2 requires the `core:` prefix for built-in event permissions.

**Symptom**: `listen('update-ready', callback)` registers without error but never fires. No console error, no Rust-side warning. The event is emitted by Rust (`app.emit("update-ready", payload)`) but the WebView listener is not permitted to receive it.

**Fix**: In `capabilities/default.json`, use `"core:event:default"` not `"event:default"`:
```json
{
  "permissions": [
    "core:default",
    "core:event:default",
    "updater:default"
  ]
}
```

**Why this is subtle**: `core:default` does NOT include event permissions. You need both `core:default` (for basic IPC) and `core:event:default` (for `listen`/`emit`). Third-party plugins use their own namespace (`updater:default`, `opener:default`), but built-in event system uses `core:event:default`.

**Validating tests**: Runtime verification — `listen('update-ready')` receives events after adding `core:event:default`.

---

## Tauri v2 Updater: Windows NSIS Requires Immediate Exit (2026-02-18, tauri-updater-and-logs)

**Problem**: On Windows, `update.install(&bytes)` launches the NSIS installer as a child process. If the app continues running, the NSIS installer cannot replace locked files (the running executable and DLLs). The update silently fails or produces file-in-use errors.

**Solution**: Call `app.exit(0)` immediately after `update.install()` on Windows. The NSIS installer handles the rest (extraction, service management, app relaunch).

**Platform divergence**:
- **Windows**: `update.install()` → `app.exit(0)` (NSIS takes over)
- **macOS/Linux**: `update.install()` → store info in static → emit event to frontend → apply on exit via `app.restart()`

**Implementation pattern** (conditional compilation):
```rust
#[cfg(target_os = "windows")]
{
    update.install(&bytes).map_err(|e| e.to_string())?;
    app.exit(0);
}

#[cfg(not(target_os = "windows"))]
{
    update.install(&bytes).map_err(|e| e.to_string())?;
    UPDATE_READY.store(true, Ordering::SeqCst);
    let _ = app.emit("update-ready", info);
}
```

**Validating tests**: `desktop/src-tauri/src/updater.rs` — unit tests for serialization; runtime verification for platform behavior.

---

## Tauri #[tauri::command] Async + spawn_blocking for Heavy I/O (2026-02-18, tauri-updater-and-logs)

**Problem**: `#[tauri::command]` handlers run on the Tokio async runtime. Using `reqwest::blocking::Client` (or any blocking I/O) inside them causes a panic: "Cannot start a runtime from within a runtime."

**Solution**: Wrap blocking code in `tokio::task::spawn_blocking()`. The `#[tauri::command]` function must be `async` and `.await` the spawn_blocking result.

**Pattern**:
```rust
// WRONG — panics at runtime
#[tauri::command]
pub fn upload_logs(params: Params) -> Result<(), String> {
    let client = reqwest::blocking::Client::new();  // PANIC
    client.put(url).send().map_err(|e| e.to_string())?;
    Ok(())
}

// CORRECT — runs blocking code on dedicated thread pool
#[tauri::command]
pub async fn upload_logs(params: Params) -> Result<(), String> {
    tokio::task::spawn_blocking(move || {
        let client = reqwest::blocking::Client::new();
        client.put(url).send().map_err(|e| e.to_string())
    })
    .await
    .map_err(|e| format!("Task failed: {}", e))?
}
```

**When to use**: Any `#[tauri::command]` that performs file I/O, HTTP requests with `reqwest::blocking`, or other synchronous operations that may block. The alternative is using `reqwest::Client` (async), but `spawn_blocking` is simpler when the internal logic is inherently synchronous (e.g., read file → compress → upload → notify).

**Cross-reference**: See Architecture Decisions → "Log Upload in Tauri Shell, Not Daemon" for the full pattern in context.

**Validating tests**: `desktop/src-tauri/src/log_upload.rs` — compiles and runs; `cargo test` passes.

---

## QUIC/smux Dead Connection Caching Causes Silent Tunnel Death (2026-02-17, android-vpn-audit)

**Problem**: `QUICClient.connect()` caches `c.conn` after first successful connection. When the network changes (WiFi→4G), the cached QUIC connection dies but is never cleared. All subsequent `DialTCP`/`DialUDP` calls reuse the dead connection, fail, and the engine still reports `"connected"`.

**Timeline**: Network change → QUIC keepalive fails → 30s `MaxIdleTimeout` → connection dead → `c.conn != nil` → cached dead connection returned by `connect()` → all new streams fail → tunnel effectively dead but `engine.state == "connected"`.

**Same issue in TCP-WS**: smux session caches similarly. `KeepAliveInterval: 10s`, `KeepAliveTimeout: 30s` — dead session detection takes 30s, but no reconnection.

**Fix direction**: `QUICClient.connect()` should check connection liveness and clear `c.conn = nil` on error, forcing lazy reconnection on next dial. This is the minimal wire-layer fix that works across all platforms without platform-specific code.

**Cross-reference**: See Architecture Decisions → "sing-tun Network Monitoring" for full reconnection architecture.

**Tests**: No test yet — discovery from code audit.
**Source**: android-vpn-audit (2026-02-17)
**Status**: verified (code-level confirmation in `k2/wire/quic.go`)

---

## Daemon Error Format Change Requires Bridge Backward Compat (2026-02-18, structured-error-codes)

**Context**: `structured-error-codes` changed the daemon status response `"error"` field from a plain string to a structured object: `{"code": 503, "message": "..."}`. Webapp and daemon are deployed independently.

**Problem**: If webapp bridge reads `raw.error.code` directly without checking type, it fails for users still running old daemon (returns string, not object).

**Solution**: Bridge checks type before accessing `.code`:
```typescript
if (typeof raw.error === 'object' && raw.error !== null && 'code' in raw.error) {
  error = { code: raw.error.code, message: raw.error.message || '' };
} else {
  // Old daemon: string error, fallback to code 570
  error = { code: 570, message: String(raw.error) };
}
```

**Why backward compat in bridge, not daemon**: Daemon is a binary that users may not update immediately. Webapp is a web asset that can be updated centrally. Bridge is the right place to absorb format evolution.

**Pattern applicability**: Any time a Go daemon API field changes type (string→object, number→object), the TypeScript bridge must handle both forms during the transition period.

**Validating tests**: `webapp/src/services/__tests__/tauri-k2.test.ts` — `maps stopped with structured error to error state` and `maps stopped with string error to error state (backward compat)`

---

## Vite Multi-Page HTML: Globals Not Available on Load (2026-02-18, unified-debug-page)

**Problem**: `debug.html` is a Vite multi-page entry loaded outside React bootstrap. `window._k2` and `window._platform` are injected by the main app's platform detection (Tauri/Capacitor/standalone), which doesn't run for non-index pages. Accessing globals directly on DOMContentLoaded throws.

**Solution**: Poll `window._k2` every 200ms for up to 5s. If found, proceed. On timeout, show "Load Standalone Fallback" button that inlines minimal stubs. This preserves zero-framework-dependency (the page has no imports, no React, no bundler transforms).

**Key constraint**: Cannot `import` from `standalone-k2.ts` because that adds module bundler dependency. The fallback must be inlined vanilla JS. On Tauri/Capacitor, the platform bridge injects globals before page load via native WebView evaluation, so the poll finds them immediately.

**Tests**: No unit test — manual verification only.
**Source**: unified-debug-page (2026-02-18)
**Status**: verified (tested in Tauri dev mode)

---
