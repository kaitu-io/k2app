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

## Go json.Marshal Escapes & as \u0026 in URL Strings (2026-02-20, k2v5-tunnel-expression)

**Problem**: `json.Marshal` in Go escapes `&` as `\u0026` in JSON output. Tests that use `assert.Contains(t, string(jsonBytes), "ech=AABB&pin=sha256:abc")` fail because the actual JSON contains `ech=AABB\u0026pin=sha256:abc`.

**Root cause**: Go's `encoding/json` escapes `<`, `>`, and `&` for HTML safety by default. This is correct behavior — the JSON is semantically equivalent — but string-level assertions break.

**Solution**: Unmarshal back to `map[string]any` and assert on the deserialized value, not the raw JSON string:
```go
var parsed map[string]any
require.NoError(t, json.Unmarshal(data, &parsed))
assert.Equal(t, expectedURL, parsed["serverUrl"])
```

**Applies to**: Any Go test asserting URL strings (or any string containing `&`) in JSON output.

**Validating tests**: `docker/sidecar/sidecar/connect_url_test.go` — `TestTunnelConfig_MarshalWithServerURL`

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

## sing-tun logger.Logger Interface Requires 7 No-Op Methods (2026-02-18, network-change-reconnect)

**Problem**: `tun.NewNetworkUpdateMonitor(logger)` and `tun.NewDefaultInterfaceMonitor(netMon, logger, opts)` require a `logger.Logger` from `github.com/sagernet/sing/common/logger`. This is NOT the stdlib `log.Logger` — it has 7 methods: `Trace`, `Debug`, `Info`, `Warn`, `Error`, `Fatal`, `Panic`.

**Solution**: Define a `nopLogger` struct that satisfies all 7 methods with no-ops. Pass as nil pointer to the struct type:
```go
type nopLogger struct{}
func (l *nopLogger) Trace(args ...any) {}
func (l *nopLogger) Debug(args ...any) {}
func (l *nopLogger) Info(args ...any)  {}
func (l *nopLogger) Warn(args ...any)  {}
func (l *nopLogger) Error(args ...any) {}
func (l *nopLogger) Fatal(args ...any) {}
func (l *nopLogger) Panic(args ...any) {}

// Usage:
netMon, err := tun.NewNetworkUpdateMonitor((*nopLogger)(nil))
```

**Why nil pointer works**: Go interfaces store (type, value) pairs. `(*nopLogger)(nil)` has type `*nopLogger` and nil value — the interface is non-nil (type pointer exists), so the caller can invoke methods without panic (methods use pointer receiver but don't dereference anything).

**Alternative**: Pass daemon's real logger if structured log output is desired from sing-tun. nopLogger is appropriate when sing-tun's internal debug messages would be noise.

**Files**: `k2/daemon/network_monitor.go`
**Source**: network-change-reconnect (2026-02-18)

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

## K2Plugin definitions.ts Has Compiled dist/ — Must Rebuild After Editing (2026-02-18, updater-android-router)

**Problem**: `mobile/plugins/k2-plugin/src/definitions.ts` defines the TypeScript interface for K2Plugin methods and events. When new methods (e.g., `installNativeUpdate`) or event listeners (e.g., `nativeUpdateReady`, `nativeUpdateAvailable`) are added to the interface, the compiled output at `mobile/plugins/k2-plugin/dist/` must be rebuilt. Without rebuilding dist/, the webapp imports stale type definitions — new method signatures are invisible to TypeScript, causing type errors or missing autocompletion.

**Symptom**: After editing `definitions.ts`, `tsc --noEmit` in the webapp passes (because it sees the source definitions), but runtime behavior may differ from what TypeScript reports. More dangerously, if the dist/ mismatch causes mismatched method signatures, K2Plugin calls from the webapp can fail silently.

**Fix**: After editing `mobile/plugins/k2-plugin/src/definitions.ts`:
```bash
cd mobile/plugins/k2-plugin && npm run build
cd mobile
rm -rf node_modules/k2-plugin && yarn install --force
npx cap sync
```

**Why three steps**: (1) `npm run build` compiles TypeScript → `dist/`. (2) `rm + yarn install --force` replaces the stale node_modules copy (yarn `file:` protocol doesn't detect source file changes — see also "Capacitor file: Plugin Source Not Auto-Synced"). (3) `cap sync` propagates to native projects.

**This is additive to the existing local plugin sync gotcha**: The existing rule (`rm -rf node_modules/k2-plugin && yarn install --force`) covers Swift/Kotlin changes. When also editing TypeScript definitions, the `npm run build` step must precede the node_modules refresh.

**Validating tests**: `webapp/src/services/__tests__/capacitor-k2.test.ts` — updater describe block (tests pass only with correct type definitions in node_modules).

---

## Bash Arithmetic ((var++)) Exits Under set -e When var Is 0 (2026-02-18, openwrt-docker-testing)

**Problem**: `((PASS++))` in a bash script with `set -e` causes immediate exit when `PASS` is 0. The post-increment evaluates to 0 (the pre-increment value), which is falsy in arithmetic context, returning exit code 1.

**Symptom**: Test script runs first check successfully, increments counter, then exits silently. Only first test result is visible.

**Fix**: Use `PASS=$((PASS + 1))` instead of `((PASS++))`. The `$((..))` form is an expression, not a command — its exit code doesn't affect `set -e`.

**Alternative fixes**:
- `((PASS++)) || true` — suppress the exit code
- `: $((PASS++))` — colon command always succeeds

**Prevention**: Never use `((var++))` or `((var--))` when `var` could be 0 in scripts with `set -e`. Always use `VAR=$((VAR + 1))` form.

**Validating tests**: `scripts/test-openwrt.sh` — all 4 smoke tests pass after fix.

---

## Vite Multi-Page HTML: Globals Not Available on Load (2026-02-18, unified-debug-page)

**Problem**: `debug.html` is a Vite multi-page entry loaded outside React bootstrap. `window._k2` and `window._platform` are injected by the main app's platform detection (Tauri/Capacitor/standalone), which doesn't run for non-index pages. Accessing globals directly on DOMContentLoaded throws.

**Solution**: Poll `window._k2` every 200ms for up to 5s. If found, proceed. On timeout, show "Load Standalone Fallback" button that inlines minimal stubs. This preserves zero-framework-dependency (the page has no imports, no React, no bundler transforms).

**Key constraint**: Cannot `import` from `standalone-k2.ts` because that adds module bundler dependency. The fallback must be inlined vanilla JS. On Tauri/Capacitor, the platform bridge injects globals before page load via native WebView evaluation, so the poll finds them immediately.

**Tests**: No unit test — manual verification only.
**Source**: unified-debug-page (2026-02-18)
**Status**: verified (tested in Tauri dev mode)

---

## Desktop Window: show_window IPC Must Be Last Step in Bridge Injection (2026-02-20, desktop-window-management)

**Problem**: Tauri window is created hidden (`visible: false`) and sized by Rust during setup. If Rust calls `window.show()` directly in setup, the window appears before the WebView has loaded React, CSS, or MUI theme — user sees white flash or unstyled content for ~200ms.

**Solution**: Rust setup sizes the window but does NOT show it. The `show_window` IPC command is called at the very end of `injectTauriGlobals()` in `tauri-k2.ts`, after all globals are injected and the bridge is ready. By this point, React and MUI are about to render.

**Ordering in tauri-k2.ts**:
```typescript
export async function injectTauriGlobals(): Promise<void> {
  // ... all injection code ...
  (window as any)._k2 = tauriK2;
  (window as any)._platform = tauriPlatform;
  console.info(`[K2:Tauri] Injected`);

  // LAST: Show window after frontend is fully initialized
  try {
    await invoke('show_window');
  } catch (error) {
    console.warn('[TauriK2] Failed to show window:', error);
  }
}
```

**Why try/catch**: `show_window` failing should not prevent the app from working. The window may already be visible (e.g., user opened it from tray during init), or the command may not be registered in older Rust builds.

**Cross-reference**: See Architecture Decisions → "Desktop Window Management: Hidden→Size→Show Lifecycle"

**Validating tests**: `webapp/src/services/__tests__/tauri-k2.test.ts` — all 29 tests pass. Manual: no white flash on `make dev`.

---

## Dark-Only App: Use Direct Background Color, Not prefers-color-scheme (2026-02-20, desktop-window-management)

**Problem**: The kaitu/client source uses `@media (prefers-color-scheme: dark) { background: #0f0f13; }` in `index.html`. But k2app is a dark-only app (no light mode) — the media query means the background is white on light-mode OS, causing a white flash before MUI theme loads.

**Solution**: Use `background: #0f0f13` directly on `html, body` without any media query. Since the app is always dark, the background should always be dark.

**Why this matters**: On macOS with light appearance, the WebView loads with a white background. Without explicit dark background, the first 100–300ms shows white before React mounts and MUI theme applies `CssBaseline`. On fast machines this is barely visible; on Windows 1080p laptops it's noticeable.

**Files**: `webapp/index.html` — `background: #0f0f13` on `html, body`

**Validating tests**: Visual verification — no white flash on app start.

---

## Docker Hub Unreachable Behind GFW: DaoCloud Mirror + Local Tag (2026-02-20, publish-docker)

**Problem**: `docker build` with `FROM alpine:3.20` fails behind GFW — Docker Hub (`registry-1.docker.io`) times out. ECR public mirror (`public.ecr.aws/docker/library/alpine:3.20`) also blocked. DaoCloud mirror (`docker.m.daocloud.io/library/alpine:3.20`) works for `docker pull` but returns 401 when used in `docker build --platform linux/amd64`.

**Root cause**: DaoCloud mirror authenticates differently during multi-platform `docker build` (buildkit) vs simple `docker pull`. The `--platform` flag triggers buildkit which makes a fresh registry request that DaoCloud rejects.

**Solution**: Two-step workaround:
1. `docker pull docker.m.daocloud.io/library/alpine:3.20` (works)
2. `docker tag docker.m.daocloud.io/library/alpine:3.20 alpine:3.20` (local alias)
3. Dockerfile uses `FROM alpine:3.20` — resolves from local cache

**Alternative**: If DaoCloud is also blocked, try other China mirrors: `registry.cn-hangzhou.aliyuncs.com/library/alpine:3.20` (Aliyun), `mirror.ccs.tencentyun.com/library/alpine:3.20` (Tencent).

**Prevention**: Keep local images cached. Run `docker pull` for base images periodically when network is available.

**Applies to**: Any Docker build behind GFW that needs Docker Hub base images.

**Validating tests**: `make publish-docker` succeeds with locally tagged base image.

---

## Apple Silicon Docker Builds Default to arm64 — Use --platform linux/amd64 (2026-02-20, publish-docker)

**Problem**: `docker build` on Apple Silicon (M1/M2/M3) produces `linux/arm64` images by default. Server containers deployed to x86_64 Linux VMs crash with `exec format error` or fail to start silently.

**Symptom**: Image builds and pushes successfully. Remote `docker pull` succeeds. `docker compose up` fails with no clear error — container exits immediately.

**Solution**: Always pass `--platform linux/amd64` to `docker build` for server-targeted images:
```bash
docker build --platform linux/amd64 -t "${IMAGE}" docker/k2s/
```

**Go binary alignment**: Cross-compiled Go binaries must also target amd64:
```bash
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o binary .
```

**Both must match**: `--platform linux/amd64` Docker image + `GOARCH=amd64` binary. Mismatched architectures cause silent failure.

**Validating tests**: `docker inspect --format='{{.Architecture}}' image` returns `amd64`; remote deployment succeeds.

---

## ECR Public Repositories Must Be Created Before First Push (2026-02-20, publish-docker)

**Problem**: `docker push public.ecr.aws/d6n9t2r2/k2v5:latest` fails with "The repository with name 'k2v5' does not exist in the registry". ECR Public does not auto-create repositories on push (unlike Docker Hub).

**Solution**: Create repository before first push:
```bash
aws ecr-public create-repository --repository-name k2v5 --region us-east-1
```

**Note**: ECR Public repository creation MUST use `--region us-east-1` regardless of where you're deploying. ECR Public is a global service anchored to us-east-1.

**ECR login command** (also us-east-1):
```bash
aws ecr-public get-login-password --region us-east-1 | \
    docker login --username AWS --password-stdin public.ecr.aws
```

**Subsequent pushes**: Once the repo exists, `docker push` works without additional setup (just need valid ECR login).

**Validating tests**: `docker push` succeeds after repo creation.

---

## ECR Public Rate Limiting on Unauthenticated Pulls (2026-02-20, publish-docker)

**Problem**: Remote servers doing `docker compose pull` from ECR Public get rate limited: "toomanyrequests: Rate exceeded". ECR Public allows ~1 pull/sec unauthenticated, but `docker compose pull` with multiple images can exceed this.

**Solution**: Authenticate the remote Docker client with ECR before pulling:
```bash
# Get token locally (where AWS credentials exist)
TOKEN=$(aws ecr-public get-login-password --region us-east-1)

# Pass to remote server
ssh remote "echo '$TOKEN' | docker login --username AWS --password-stdin public.ecr.aws"
ssh remote "cd /apps/kaitu-slave && docker compose pull && docker compose up -d"
```

**Authenticated rate limits**: Much higher (~10 pulls/sec). Token lasts 12 hours.

**Alternative**: For production, consider ECR Private (higher limits, per-region) or cache images in a private registry.

**Validating tests**: Remote `docker compose pull` succeeds after ECR login.

---

## TypeScript NodeNext Module Resolution: Imports Must Use .js Extension (2026-02-20, kaitu-ops-mcp)

**Problem**: With `"module": "NodeNext"` and `"moduleResolution": "NodeNext"` in tsconfig.json, TypeScript requires all relative imports to use the `.js` extension — even though the source files are `.ts`.

**Symptom**: `import { loadConfig } from './config'` compiles but the emitted JavaScript fails at runtime with `Cannot find module './config'`. The `.js` file exists in `dist/` but Node.js cannot resolve it because the import has no extension.

**Root cause**: NodeNext module resolution emulates what Node.js ESM does at runtime. Node.js ESM requires explicit extensions. TypeScript emits the import exactly as written — writing `.js` in the source gets emitted as `.js` in the output (correct for the runtime), while writing nothing gets emitted as nothing (incorrect).

**Solution**: Write all relative imports with `.js` extension in `.ts` source files:
```typescript
// WRONG (fails at runtime with NodeNext)
import { loadConfig } from './config'

// CORRECT
import { loadConfig } from './config.js'
import { sshExec } from './ssh.js'
```

The `@modelcontextprotocol/sdk` package subpath imports also require `.js`: `'@modelcontextprotocol/sdk/server/mcp.js'`.

**Applies to**: Any TypeScript project with `"module": "NodeNext"` targeting Node.js ESM, including `"type": "module"` packages.

**Validating tests**: `tools/kaitu-ops-mcp/src/index.ts` and all tool files use `.js` extensions. `npm run build && node dist/index.js` starts MCP server successfully.

---

## MCP Server stdio Transport: Guard main() from Running on Import (2026-02-20, kaitu-ops-mcp)

**Problem**: `@modelcontextprotocol/sdk` `StdioServerTransport.connect()` starts reading from `process.stdin`. If `main()` is called during module import (e.g., test files importing from `./index.js`), the stdio transport starts and blocks the test process.

**Solution**: Guard `main()` with an entry point check:
```typescript
const isEntryPoint =
  process.argv[1] !== undefined &&
  import.meta.url === new URL(`file://${process.argv[1]}`).href

if (isEntryPoint) {
  main().catch(err => { console.error('Failed to start:', err); process.exit(1) })
}
```

**Why `import.meta.url` comparison**: In Node.js ESM, `import.meta.url` is the `file://` URL of the current module. `process.argv[1]` is the path of the running script. They match only when the module is the entry point. For imported modules, `import.meta.url` is the module's own URL.

**Extract `createServer(config)` for testability**: Tests call `createServer(config)` directly — gets a configured `McpServer` without starting stdio transport. The `main()` function calls `createServer()` then `server.connect(new StdioServerTransport())`.

**Applies to**: Any Node.js ESM tool using stdio as I/O channel (MCP servers, CLI tools that read stdin).

**Validating tests**: `tools/kaitu-ops-mcp/src/index.test.ts` — imports `createServer` and invokes it directly without blocking stdio.

---

## next-intl Strict Typing: Empty IntlMessages Interface for Split Namespace Files (2026-02-21, website-k2-redesign)

**Problem**: When next-intl is configured with split namespace JSON files loaded dynamically (e.g., `messages/{locale}/hero.json`, `messages/{locale}/k2.json`), TypeScript's `Messages` type inference breaks. The `IntlMessages` global interface tries to intersect all namespace shapes — but dynamic per-namespace loading means there is no single merged type to infer from.

**Symptom**: Adding a new namespace file (`k2.json`) causes TypeScript errors throughout the codebase because next-intl cannot infer the complete merged `Messages` type from the new file's shape. Adding `k2` to `namespaces.ts` triggers type re-inference that propagates failures.

**Root cause**: next-intl expects `IntlMessages` to be declared as a single merged interface. Dynamic per-namespace loading via `import(`./${lang}/${ns}.json`)` does not produce a statically inferrable merged type.

**Solution**: Use an empty `IntlMessages {}` interface (permissive typing). Cast the locale string when calling `setRequestLocale`:
```typescript
// web/src/types/i18n.d.ts
declare global {
  // Messages are split across namespace JSON files — use permissive typing
  // eslint-disable-next-line @typescript-eslint/no-empty-object-type
  interface IntlMessages {}
}
```
```typescript
// page.tsx — cast locale string to the routing locales union type
setRequestLocale(locale as (typeof routing.locales)[number]);
```

**Trade-off**: Loss of compile-time key checking for translation keys. Runtime errors for missing keys instead of compile-time errors. Acceptable when namespace files are managed centrally (`namespaces.ts` registry) and keys are tested via vitest.

**When this pattern applies**: Any next-intl project that:
1. Splits translations across multiple namespace JSON files
2. Loads namespaces dynamically (not imported as a single merged JSON)
3. Has more than one locale with per-namespace loading

**Validating tests**: `web/tests/homepage-ssr.test.ts` — `test_homepage_ssr_renders_content`, `test_homepage_generates_metadata`
**Source**: website-k2-redesign (2026-02-21)

---

## next-intl usePathname Must Use @/i18n/routing, Not next/navigation (2026-02-21, website-k2-redesign)

**Problem**: ESLint rule `@next/next/no-restricted-navigation` (or next-intl's own ESLint plugin) flags `import { usePathname } from 'next/navigation'` inside components that live in the `[locale]` App Router segment. The `next/navigation` pathname includes the locale prefix (e.g., `/zh-CN/k2/quickstart`); the `@/i18n/routing` version strips the locale prefix for cleaner comparisons.

**Symptom**: ESLint error "Use `usePathname` from `@/i18n/routing` instead of `next/navigation`" when writing a client component inside `web/src/components/` that needs the current path for active-link highlighting.

**Solution**: Always import `usePathname` (and `Link`) from `@/i18n/routing` in components used within the `[locale]` layout group:
```typescript
// CORRECT
import { usePathname } from '@/i18n/routing';
import { Link } from '@/i18n/routing';

// WRONG — ESLint error
import { usePathname } from 'next/navigation';
import Link from 'next/link';
```

**Why this matters for active-link detection**: `@/i18n/routing` `usePathname()` returns the path without locale prefix (e.g., `/k2/quickstart`), making slug comparison straightforward. `next/navigation` would return `/zh-CN/k2/quickstart` requiring manual locale stripping.

**Files**: `web/src/components/K2Sidebar.tsx` — uses `@/i18n/routing` for both `usePathname` and `Link`

**Validating tests**: `web/tests/k2-route.test.ts` — `test_k2_route_renders_sidebar`, `test_k2_sidebar_groups_by_section`
**Source**: website-k2-redesign (2026-02-21)

---

## Next.js Static Route Priority Over Catch-All (2026-02-21, website-k2-redesign)

**Problem**: The existing `[...slug]` catch-all route in `web/src/app/[locale]/[...slug]/page.tsx` handles all content pages including potential `/k2/*` paths. Adding a new `/k2/` section requires ensuring the new route takes priority without breaking existing content.

**Root cause**: Next.js App Router resolves routes with a static-first priority rule. A more-specific static route pattern always wins over a less-specific catch-all.

**Solution**: Create `web/src/app/[locale]/k2/[[...path]]/page.tsx` (optional catch-all). Next.js treats `k2/` as a more specific segment than `[...slug]`, so all `/k2/*` requests are intercepted by the new route. The existing `[...slug]` continues to handle all other paths unchanged.

**Verified behavior**:
- `/k2/` → `[[...path]]` page with `path = undefined` → renders `k2/index` post
- `/k2/quickstart` → `[[...path]]` page with `path = ['quickstart']` → renders `k2/quickstart` post
- `/changelog` → `[...slug]` catch-all → unchanged behavior

**Why `[[...path]]` (optional) not `[...path]` (required)**: Optional catch-all matches the root `/k2/` path (with no path segments). Required catch-all would need a separate `/k2/index/page.tsx` for the overview page.

**Validating tests**: `web/tests/k2-route.test.ts` — `test_k2_route_renders_content`, `test_k2_route_renders_sidebar`
**Source**: website-k2-redesign (2026-02-21)

---

## QUIC/TCP-WS Lazy Connection: First App Dial Triggers Wire Handshake (2026-02-21, k2-runtime-flow-audit)

**Observation**: Neither `QUICClient` nor `TCPWSClient` connects during `engine.Start()`. The wire connection is only established on the first `DialTCP`/`DialUDP` call from an app, inside the lazy `connect()` method. This means `engine.Start()` can succeed even if the server is unreachable — the error only surfaces when the first app tries to connect.

**Timeline**:
```
engine.Start()       → state="connected", no wire handshake yet
First app dial       → QUICClient.connect() → QUIC handshake + TLS + ECH
                       If fails: TransportManager tries TCPWSClient.connect()
                       If both fail: that one app connection is dropped
Subsequent app dials → reuse cached c.conn (fast, no handshake)
```

**Implications**:
1. **"Connected" doesn't mean wire is established**: Engine state `connected` means the TUN device and routing are set up. Wire connectivity is unknown until first dial.
2. **First connection is slower**: ~100-500ms QUIC handshake + TLS 1.3 + ECH on the first dial. Subsequent dials reuse the cached QUIC connection and just open a new stream (~5ms).
3. **Server-down detection is delayed**: If the server goes down after `Start()` but before any app dials, the error is only discovered on first use.
4. **No "pre-connect" option**: Engine has no explicit `wire.Connect()` call. If you need to verify server reachability before declaring success, you'd need to add a probe step.

**Why lazy connection**: Avoids blocking `Start()` on network I/O. TUN route setup should be fast and deterministic. Wire connectivity is best-effort — the per-dial fallback handles transient failures.

**Files**: `k2/wire/quic.go:137-205` (connect), `k2/wire/tcpws.go:100-133` (connect)

**Tests**: No dedicated test — verified by engine test suite.
**Source**: k2-runtime-flow-audit (2026-02-21)
**Status**: verified (code-level confirmation)

---

## Engine Start() Concurrent Stop() Race: Two Cancellation Checkpoints (2026-02-21, k2-runtime-flow-audit)

**Problem**: `engine.Start()` unlocks `e.mu` after setting state to `connecting` (to allow long-running operations like TUN creation). During this unlocked window, `Stop()` can be called by another goroutine. Without cancellation checks, `Start()` would commit resources (TUN, transport) that `Stop()` has already decided to tear down.

**Solution**: Two cancellation checkpoints using `ctx.Err()`:
1. **Step 8** (after transport init, before TUN start): `if ctx.Err() != nil → cleanup transport, return fail()`
2. **Step 10** (after TUN start, re-acquire lock): `if ctx.Err() != nil → cleanup provider+transport, return ctx.Err()`

**Why two checkpoints**: TUN creation (Step 9) is the most expensive operation — it creates a kernel device and installs routes. Checking before (Step 8) avoids unnecessary TUN creation. Checking after (Step 10) catches races during TUN creation itself.

**Context sharing**: `ctx, cancel` is created early (Step 1) and saved to `e.cancel` before unlocking. `Stop()` calls `e.cancel()` which sets `ctx.Err() != nil`, signaling `Start()` to abort.

**Files**: `k2/engine/engine.go:147-150` (Step 8 check), `k2/engine/engine.go:188-196` (Step 10 check)

**Tests**: No dedicated race test — verified by code inspection.
**Source**: k2-runtime-flow-audit (2026-02-21)
**Status**: verified (code-level confirmation)

---

## stdout Redaction: Global Regex Requires lastIndex Reset Between Calls (2026-02-20, kaitu-ops-mcp)

**Problem**: Redaction regex patterns defined with the `g` flag at module level maintain `lastIndex` state between calls. Reusing the same pattern object without resetting causes alternating text matches to fail — the regex starts searching from a non-zero offset on the second call.

**Root cause**: `RegExp.prototype.lastIndex` is mutated by `replace()` when the `g` flag is set. Module-level pattern objects persist this state across function invocations.

**Solution**: Reset `lastIndex = 0` before each `.replace()` call:
```typescript
const REDACTION_PATTERNS = [{ pattern: /pattern/g, replacement: '...' }]

export function redactStdout(text: string): string {
  let result = text
  for (const { pattern, replacement } of REDACTION_PATTERNS) {
    pattern.lastIndex = 0  // REQUIRED: reset before each use of a /g pattern
    result = result.replace(pattern, replacement)
  }
  return result
}
```

**Pattern ordering matters**: More specific patterns should run before broader ones. In `redact.ts`, key=value pattern runs before 64-char hex to avoid double-redaction of secrets.

**Redaction patterns for VPN node operations** (`tools/kaitu-ops-mcp/src/redact.ts`):
1. `([A-Z0-9_]*(?:SECRET|KEY|PASSWORD|TOKEN)[A-Z0-9_]*)=\S+` → `$1=[REDACTED]` — env var style
2. `(?<![0-9a-fA-F])[0-9a-fA-F]{64}(?![0-9a-fA-F])` → `[REDACTED]` — 64-char hex secrets

**Validating tests**: `tools/kaitu-ops-mcp/src/redact.test.ts` — `test_redact_node_secret`, `test_redact_hex_string_64`, `test_redact_preserves_normal`, `test_redact_multiline` (AC4)

---

## sing-tun Lifecycle: New → Start → NewSystem → stack.Start (2026-02-22, sing-tun-lifecycle-fix)

**Problem**: sing-tun `tun.New()` creates the TUN device but does NOT install routing table entries or register the interface for self-exclusion. Calling `tun.NewSystem()` + `stack.Start()` immediately after `New()` results in a functional gVisor stack attached to a TUN that receives no traffic (no OS routes point to it).

**Correct lifecycle**:
```
tun.New(opts)           → kernel TUN device created (utunN on macOS)
tunIf.Start()           → AutoRoute entries installed, InterfaceMonitor registered, DNS cache flushed
tun.NewSystem(stackOpts)→ gVisor user-space IP stack created
stack.Start()           → gVisor stack begins processing packets
```

**What `Start()` does internally** (sing-tun v0.7.11):
1. `autoRoute.Configure()` — installs split routes (1.0.0.0/8, 2.0.0.0/7, etc.) pointing to the TUN
2. `InterfaceMonitor.RegisterMyInterface(tunName)` — tells the monitor to ignore TUN-self route changes
3. `clearDNSCache()` — flushes OS DNS cache (macOS: `dscacheutil -flushcache`)

**Verification**: `netstat -rn | grep utunN` should show many route entries after `Start()`. Zero entries = `Start()` was not called.

**Applies to**: Desktop only (macOS/Linux/Windows). Mobile providers (`tun_ios.go`, `tun_android.go`) use platform-provided fd and OS-managed routes — `Start()` is not called (and would panic due to nil InterfaceMonitor).

**Cross-reference**: See Bugfix Patterns → "sing-tun Missing tunIf.Start()"

**Validating tests**: `go test ./provider/ -v` — `TestDefaultTunName`. Live: `netstat -rn | grep utun` shows routes.

---

## sing-tun CalculateInterfaceName: Use Instead of Manual utun Scanning (2026-02-22, sing-tun-lifecycle-fix)

**Problem**: k2 had a 30-line `nextAvailableUtun()` function that scanned `net.Interfaces()` for used utun indices. sing-tun already provides `tun.CalculateInterfaceName("")` that does exactly this (and handles edge cases better — e.g., system utun devices on newer macOS).

**Solution**: Replace manual scanning with `tun.CalculateInterfaceName("")` on darwin. Linux/Windows use hardcoded `"k2tun"` (no index needed).

```go
func defaultTunName() string {
    if runtime.GOOS == "darwin" {
        return tun.CalculateInterfaceName("")
    }
    return "k2tun"
}
```

**Why `""` argument**: Empty string tells sing-tun to use its default naming pattern. On darwin, it finds next available utun index. Passing a specific name (e.g., `"k2tun"`) would use that name literally.

**Files**: `k2/provider/tun_desktop.go`

**Validating tests**: `go test ./provider/ -v` — `TestDefaultTunName` verifies utun prefix on darwin.

---

## macOS launchd: Must Unload Before Overwriting plist (2026-02-22, macos-pkg-service-lifecycle)

**Problem**: Writing a new plist file while the service is loaded causes launchd to use stale config. The loaded service keeps running with old settings until explicitly unloaded.

**Solution**: Always `launchctl unload /Library/LaunchDaemons/kaitu.plist` before overwriting the file, then `launchctl load` after writing new content.

**k2 implementation**: `k2/daemon/service_darwin.go` `installService()` does unload → write plist → load in sequence.

**Applies to**: Any launchd daemon service that gets upgraded in-place.

**Source**: k2-cli-redesign (2026-02-22)
**Status**: verified (UAT — sudo service install overwrites cleanly)

---

## PKG preinstall Runs OLD Binary, postinstall Runs NEW (2026-02-22, macos-pkg-service-lifecycle)

**Problem**: macOS PKG installer copies files between preinstall and postinstall. Any binary executed in preinstall is the OLD version (before upgrade). Binary executed in postinstall is the NEW version.

**Implication**: If preinstall needs to call `k2 service uninstall`, it MUST use the old binary. If the old binary doesn't support `service uninstall` (e.g., old kaitu-service), preinstall needs a fallback (`launchctl unload` + `rm` plist directly).

**Pattern**:
```
preinstall:  old k2 → service uninstall → fallback cleanup
[file copy]
postinstall: new k2 → service install
```

**Contrast with NSIS (Windows)**: NSIS runs preinstall/postinstall custom sections in the same installer binary — not the installed binary. But the pattern is the same: uninstall before overwrite, install after.

**Files**: `scripts/pkg-scripts/preinstall`, `scripts/pkg-scripts/postinstall`
**Source**: macos-pkg-service-lifecycle (2026-02-22)
**Status**: verified (spec review)

---

## Android VpnService.protect() Required for ALL Outbound Sockets (2026-02-23, android-socket-protection)

**Problem**: Android `VpnService` does NOT auto-exclude the VPN app's own sockets from TUN routing (unlike iOS `NEPacketTunnelProvider` which self-excludes at kernel level). Every socket that should bypass TUN must be explicitly marked via `VpnService.protect(fd)`.

**Affected socket types**: Wire transport (QUIC UDP, TCP-WS TCP), direct DNS (raw UDP to 114.114.114.114), and direct tunnel connections (smart routing mode bypass). Missing ANY of these causes a routing loop → fd exhaustion → OOM kill.

**Solution pattern**: `syscall.RawConn.Control()` in Go's `net.Dialer.Control` / `net.ListenConfig.Control`. The Control function runs on the raw fd BEFORE the OS `connect()` call — the correct point to call `protect()`.

**miekg/dns Dialer integration**: `dns.Client{Dialer: &net.Dialer{Control: protectFunc}}` — miekg/dns copies the full `net.Dialer` struct (value copy, not pointer reference) and uses it for UDP socket creation. The `Control` func is preserved through the copy.

**gomobile type constraint**: `SocketProtector.Protect(fd int32)` uses `int32` not `int` — gomobile maps Go `int` inconsistently across platforms. `int32` maps to Java `int` (32-bit), which matches `VpnService.protect(int)`.

**Files**: `k2/engine/protect.go`, `k2/core/dns/direct.go`, `K2VpnService.kt`
**Source**: android-socket-protection (2026-02-23)
**Status**: verified (unit tests + code review)

---

## Capacitor iOS CapacitorRouter URL(fileURLWithPath:) Empty Path (2026-02-23, capacitor-ios-white-screen)

**Problem**: Capacitor 6.x `CapacitorRouter.route(for:)` in `Router.swift` fails when `path` is `""` (empty string). `URL(fileURLWithPath: "")` resolves to the current working directory, not an empty/error URL. If the cwd path has an extension (common inside `.app` bundles), `pathExtension.isEmpty` returns `false`, and the router returns the basePath directory instead of `basePath + "/index.html"`.

**Why path is empty**: `CAPInstanceConfiguration.m:44` constructs `serverURL` as `scheme://hostname` without trailing slash. When WebView loads `capacitor://localhost`, `url.path` is `""`. The trailing-slash variant `capacitor://localhost/` would yield `url.path == "/"`, which the router handles correctly.

**Why not fixed upstream**: As of Capacitor 6.2.1 (our version), this is unfixed. Capacitor 7+ may restructure the router. The bug requires specific cwd state to trigger, making it environment-dependent and hard to reproduce in CI.

**Fix pattern**: Override `open func router() -> Router` on `CAPBridgeViewController` subclass. Return a custom `Router` conformance that checks `path.isEmpty || path == "/"` before `URL(fileURLWithPath:)`. This is the framework's designed extension point.

```swift
struct FixedCapacitorRouter: Router {
    var basePath: String = ""
    func route(for path: String) -> String {
        if path.isEmpty || path == "/" {
            return basePath + "/index.html"
        }
        let pathUrl = URL(fileURLWithPath: path)
        if pathUrl.pathExtension.isEmpty {
            return basePath + "/index.html"
        }
        return basePath + path
    }
}
```

**Storyboard wiring**: Main.storyboard must reference the custom subclass (`AppBridgeViewController` in module `App`), not Capacitor's `CAPBridgeViewController`.

**Diagnostic trail**: Systematic debugging proved the router was the fault — a custom `WKURLSchemeHandler` serving the same `basePath + "/index.html"` worked perfectly (green diagnostic screen), while Capacitor's handler returned "Is a directory".

**Files**: `mobile/ios/App/App/AppBridgeViewController.swift`, `Main.storyboard`
**Source**: capacitor-ios-white-screen (2026-02-23)
**Status**: verified (real device test on iPhone 15)

---
