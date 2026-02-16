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

## Vite Dev Proxy vs Production baseUrl (2026-02-14, k2app-rewrite)

**Problem**: HttpVpnClient needs different baseUrl in dev (Vite proxy, relative URLs) vs production (no proxy, absolute `http://127.0.0.1:1777`).

**Solution**: `import.meta.env.DEV` compile-time switch. Dev: empty baseUrl (relative, proxied via `vite.config.ts`). Prod: absolute daemon URL. See `webapp/src/vpn-client/http-client.ts:16`.

**Why**: Vite proxy is dev-only. Relative URLs avoid CORS preflight. Absolute URLs required in prod (Tauri serves static files, no proxy).

---

## Tauri Version Reference from Parent package.json (2026-02-14, k2app-rewrite)

**Problem**: Tauri `version` must match root `package.json` to prevent drift.

**Solution**: `"version": "../../package.json"` in `desktop/src-tauri/tauri.conf.json`. Tauri CLI resolves paths ending in `.json` and reads the `version` field.

**Gotcha**: Path is relative from `desktop/src-tauri/` to root — hence `../../` not `../`.

---

## Zustand Store Initialization with Async VpnClient (2026-02-14, k2app-rewrite)

**Problem**: Zustand stores are synchronous — `await` not allowed in `create()` callback.

**Solution**: Separate `init()` async action called from React `useEffect`. Store created with `ready: null` initial state; `init()` calls async VpnClient methods and updates via `set()`.

**Pattern**: Used by all stores — `useVpnStore.init()`, `useAuthStore.restoreSession()`, `useServersStore.fetchServers()`. Getter actions use `get()` closure for current state without subscription.

**Validating tests**: `webapp/src/stores/__tests__/vpn.store.test.ts`

---

## Git Submodule in Monorepo Workspace (2026-02-14, k2app-rewrite)

**Problem**: k2 is a Git submodule (Go), but yarn workspaces expects package.json in each workspace.

**Solution**: Only include actual yarn packages in workspaces: `["webapp", "desktop", "mobile"]` — NOT `"k2"`. k2 is built via Makefile (`cd k2 && go build`), initialized via `git submodule update --init`.

**CI gotcha**: Private submodule requires SSH agent setup in GitHub Actions workflows.

---

## Service Readiness Retry on Startup (2026-02-14, k2app-rewrite)

**Problem**: k2 daemon takes variable time to start after Tauri app launches. Immediate readiness check fails.

**Solution**: `ServiceReadiness.tsx` retries up to 20 times at 500ms intervals (10s total). Shows "Starting service..." during retry, "Service not available" with manual retry button on timeout.

**State machine**: Loading → Retrying (20×500ms) → Ready | Failed → (manual retry) → Retrying.

**Why not just wait longer**: 10s covers 99% of cases. Manual retry button handles edge cases without blocking all users with a long spinner.

**Validating tests**: `webapp/src/components/__tests__/ServiceReadiness.test.tsx` (if exists), manual dev testing.

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

**Why not PlatformApi**: Version is a build-time constant, not a runtime platform capability. Tauri/Capacitor each have their own version APIs (`@tauri-apps/api/app`, `@capacitor/app`) but they're async and require native runtime. Vite define is sync, available everywhere, zero runtime cost.

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
