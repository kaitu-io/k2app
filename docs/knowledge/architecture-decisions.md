# Architecture Decisions

Knowledge distilled from executed features. Links to validating tests.

---

## VpnClient Abstraction Pattern (2026-02-14, k2app-rewrite)

**Decision**: Webapp communicates with VPN backends through a unified `VpnClient` interface, never making direct HTTP calls to the daemon outside the `vpn-client/` module.

**Implementations**:
- `HttpVpnClient` (desktop): HTTP to `http://127.0.0.1:1777` + internal 2s polling converted to events
- `MockVpnClient` (testing): Controllable test double
- `NativeVpnClient` (mobile, deferred): Capacitor Plugin bridge

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
