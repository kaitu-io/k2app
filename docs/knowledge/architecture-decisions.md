# Architecture Decisions

Knowledge distilled from executed features. Links to validating tests.

---

## VpnClient Abstraction Pattern (2026-02-14, k2app-rewrite)

**Decision**: Webapp communicates with VPN backends through a unified `VpnClient` interface, never making direct HTTP calls to the daemon outside the `vpn-client/` module.

**Implementations**:
- `HttpVpnClient` (desktop): HTTP to `http://127.0.0.1:1777` + internal 2s polling converted to events
- `MockVpnClient` (testing): Controllable test double
- `NativeVpnClient` (mobile): Capacitor Plugin bridge via constructor-injected K2Plugin

**Key patterns**:
- Factory function `createVpnClient(override?)` for dependency injection
- All commands (`connect`, `disconnect`) resolve when accepted, NOT when operation completes
- Event subscription with automatic poll management (start on first subscriber, stop on zero)
- State deduplication in polling loop prevents redundant event emissions

**Validating tests**:
- `webapp/src/vpn-client/__tests__/http-client.test.ts` — HTTP calls, polling, dedup
- `webapp/src/vpn-client/__tests__/index.test.ts` — factory injection
- `webapp/src/vpn-client/__tests__/mock-client.test.ts` — test double behavior

**Why it works**:
- Single abstraction supports desktop HTTP, mobile native bridge, and test mocks
- Webapp code is platform-agnostic (same code on desktop/mobile/web)
- Polling-to-event transformation hides desktop's lack of native push events
- Deduplication prevents UI flicker from redundant state updates

---

## Service Version Matching with Build Metadata (2026-02-14, k2app-rewrite)

**Decision**: Compare service and app versions by stripping build metadata after `+` character.

**Implementation** (Rust):
```rust
pub fn versions_match(app_version: &str, service_version: &str) -> bool {
    let app_base = app_version.split('+').next().unwrap_or(app_version);
    let service_base = service_version.split('+').next().unwrap_or(service_version);
    app_base == service_base
}
```

**Why**:
- k2 binary receives version from `package.json` (e.g., `0.4.0`) via ldflags
- k2 also receives commit hash as build metadata (e.g., `0.4.0+abc123`)
- Service version from daemon includes commit: `0.4.0+abc123`
- App version from Tauri config is clean: `0.4.0`
- Semantic versioning specifies build metadata (after `+`) should be ignored for precedence

**Validating tests**:
- `desktop/src-tauri/src/service.rs` — `test_versions_match_with_build_metadata`
- Validates `versions_match("0.4.0", "0.4.0+abc123")` returns true

---

## Antiblock Entry URL Resolution (2026-02-14, k2app-rewrite)

**Decision**: Webapp resolves Cloud API entry URL through multi-source fallback chain with localStorage cache.

**Flow**:
1. Check localStorage cache (instant, non-blocking)
2. Background: JSONP fetch from npm CDN mirrors (jsDelivr, unpkg)
3. Decode base64-obfuscated entry URLs
4. Fallback to hardcoded default if all fail

**Why**:
- No custom CA needed (standard HTTPS)
- No frontend encryption (any JS key is extractable anyway)
- Fast rotation: publish new npm package, clients update within minutes
- Multi-CDN approach prevents single point of failure
- Base64 obfuscation prevents automated text scanning (not security, just evasion)

**Validating tests**:
- `webapp/src/api/__tests__/antiblock.test.ts` — cache, CDN fallback, decoding

---

## Old Service Cleanup on Upgrade (2026-02-14, k2app-rewrite)

**Decision**: Detect and remove old kaitu-service on first launch of k2app.

**Detection patterns**:
- macOS: check for plists in `/Library/LaunchDaemons/` (`io.kaitu.service.plist`, `com.kaitu.service.plist`)
- Windows: `sc query kaitu-service` exit code

**Cleanup**:
- macOS: `launchctl unload` + delete plist files
- Windows: `sc stop kaitu-service && sc delete kaitu-service`

**Why**:
- kaitu 0.3.22 used `kaitu-service` (Go binary managed by Tauri)
- k2app 0.4.0 uses `k2` daemon (self-managing via `k2 run --install`)
- Old service conflicts with new service (both listen on :1777)
- Seamless upgrade requires automatic cleanup

**Validating tests**:
- `desktop/src-tauri/src/service.rs` — `test_detect_old_kaitu_service_no_crash`
- Manual upgrade testing: 0.3.22 → 0.4.0 on macOS and Windows

---

## tauri-plugin-localhost for Mixed Content (2026-02-14, k2app-rewrite)

**Decision**: Use `tauri-plugin-localhost` to serve webapp from `http://localhost:{port}` instead of `https://tauri.localhost`.

**Problem**:
- WebKit (macOS, Linux) blocks `https://` → `http://` mixed content, even for loopback
- Webapp origin: `https://tauri.localhost`
- k2 daemon: `http://127.0.0.1:1777`
- Mixed content = blocked API calls

**Solution**:
- tauri-plugin-localhost serves webapp via HTTP instead of HTTPS
- Origin becomes `http://localhost:{port}`
- HTTP→HTTP calls to daemon are allowed (no mixed content)

**Security impact**:
- localhost port accessible to other local processes
- k2 daemon already listens on public :1777 (CORS whitelist protects it)
- Security model unchanged from daemon's perspective

**Validating tests**:
- Integration test: fetch `/ping` from webview succeeds on macOS (no console errors)
- `make dev` verification: HMR works, API calls succeed

---

## Single Source of Truth for Versioning (2026-02-14, k2app-rewrite)

**Decision**: Root `package.json` version field is the single source of truth. All other version references are derived.

**Propagation**:
- `package.json` → `tauri.conf.json` (via `"version": "../../package.json"` reference)
- `package.json` → k2 binary ldflags (via Makefile: `-X main.version=$(VERSION)`)
- `package.json` → webapp `public/version.json` (generated by `make pre-build`)
- `package.json` → git release tags (`v$(VERSION)`)

**Makefile extraction**:
```makefile
VERSION := $(shell node -p "require('./package.json').version")
COMMIT  := $(shell cd k2 && git rev-parse --short HEAD)
```

**Why**:
- Prevents version drift between components
- Single place to update for releases
- Tauri native reference ensures Tauri sees same version
- k2 binary version matches app version for compatibility checks

**Validating tests**:
- `scripts/test_version_propagation.sh` — verifies VERSION extraction, version.json generation
- Build verification: all components report same version

---

## NativeVpnClient Mobile Bridge (2026-02-14, mobile-rewrite)

**Decision**: NativeVpnClient wraps Capacitor K2Plugin calls behind the same VpnClient interface. The plugin is injected via constructor, loaded asynchronously via dynamic import to avoid bundling mobile-only dependencies in desktop builds.

**Architecture**:
- `NativeVpnClient(plugin)` — constructor injection of K2Plugin
- `initVpnClient()` — async factory: detects `Capacitor.isNativePlatform()`, dynamically imports `NativeVpnClient` + `K2Plugin`
- `createVpnClient()` — sync factory: throws on native (must use `initVpnClient()`)
- `getVpnClient()` — accessor after initialization

**State mapping**: Go Engine uses `"disconnected"` as idle state. The `mapState()` function in `native-client.ts` maps: `"disconnected"` → `"stopped"`, `"connecting"` → `"connecting"`, `"connected"` → `"connected"`, unknown → `"stopped"`.

**Three-layer state mapping** (defense in depth):
1. **Go Engine** outputs: `"disconnected"`, `"connecting"`, `"connected"`
2. **K2Plugin native** (Swift/Kotlin `remapStatusKeys`): maps `"disconnected"` → `"stopped"` for `getStatus()` and event handlers
3. **NativeVpnClient TS** (`mapState`): maps `"disconnected"` → `"stopped"` as safety net

**Why constructor injection**:
- Enables testing with mock plugin (no Capacitor dependency in tests)
- Avoids module-level mocking (`vi.mock`)
- K2Plugin is Capacitor-specific — cannot be imported outside native runtime

**Validating tests**:
- `webapp/src/vpn-client/__tests__/native-client.test.ts` — 18 tests covering all methods, state mapping, subscribe/unsubscribe, destroy
- `webapp/src/vpn-client/__tests__/index.test.ts` — factory detection tests

---

## iOS Two-Process vs Android Single-Process VPN (2026-02-14, mobile-rewrite)

**Decision**: iOS uses NEPacketTunnelProvider (separate NE process) + K2Plugin (main process). Android runs K2VpnService + K2Plugin + Engine all in the same process.

**iOS architecture** (two processes):
- Main App Process: Capacitor + K2Plugin → manages NETunnelProviderManager
- NE Process: PacketTunnelProvider → gomobile Engine (Start/Stop/StatusJSON)
- Communication: `sendProviderMessage()` for status RPC, `NEVPNStatusDidChange` for events, App Group UserDefaults for shared state

**Android architecture** (single process):
- Single Process: Capacitor + K2Plugin → K2VpnService → gomobile Engine
- Communication: Direct method calls (Engine in same process), ServiceConnection binding
- K2Plugin binds to K2VpnService via `bindService()` for lifecycle management

**Why different**:
- Apple requires VPN tunnels run in NE extension (separate process, sandboxed)
- Android VpnService runs in app process (no process boundary)
- This fundamentally changes status query pattern: iOS needs IPC (sendProviderMessage), Android calls Engine directly

**Validating tests**:
- Manual: iOS device test — connect, status via sendProviderMessage, disconnect
- Manual: Android device test — connect, status via direct Engine call, disconnect

---

## Go→JS JSON Key Remapping at Native Bridge (2026-02-14, mobile-rewrite)

**Decision**: Go `json.Marshal` outputs snake_case keys (`connected_at`, `uptime_seconds`, `wire_url`). Native bridge layers (K2Plugin.swift, K2Plugin.kt) remap to camelCase at the boundary before passing to webapp.

**Key mapping** (in both Swift and Kotlin `remapStatusKeys`):
| Go snake_case | JS camelCase |
|---------------|-------------|
| `connected_at` | `connectedAt` |
| `uptime_seconds` | `uptimeSeconds` |
| `wire_url` | `wireUrl` |

**Additional mapping**: `"disconnected"` → `"stopped"` for state field.

**Why remap at native bridge, not in Go**:
- Go's `json.Marshal` convention is snake_case — changing it requires struct tags across all Go code
- The native bridge is the natural boundary between Go and JS worlds
- Same pattern used by all Go→JS bridge layers (consistent convention)
- TypeScript definitions expect camelCase (standard JS convention)

**Validating tests**:
- `webapp/src/vpn-client/__tests__/native-client.test.ts` — `getStatus()` tests verify camelCase keys arrive correctly
- Code review verification: Swift `remapStatusKeys()` and Kotlin `remapStatusKeys()` have identical key maps

---
