# Program: k2app Rewrite

## Meta

| Field     | Value                                    |
|-----------|------------------------------------------|
| Program   | k2app-rewrite                            |
| Status    | implemented                              |
| Created   | 2026-02-14                               |
| Updated   | 2026-02-16                               |

## Version History

| Version | Date       | Summary                                              |
|---------|------------|------------------------------------------------------|
| v1      | 2026-02-14 | Initial: full rewrite from kaitu to k2app            |

## Feature Map

| Feature | Spec | Status |
|---------|------|--------|
| Webapp feature migration | kaitu-feature-migration.md | implemented |
| Webapp state alignment | webapp-state-alignment.md | implemented |
| Desktop build unification | build-unification.md | implemented |
| Mobile webapp bridge | mobile-webapp-bridge.md | implemented |
| Mobile VPN — iOS | mobile-vpn-ios.md | implemented |
| Mobile VPN — Android | mobile-vpn-android.md | implemented |
| Unified engine + rule mode | mobile-rule-storage.md | implemented |
| Mobile debug tool | mobile-debug.md | implemented |
| Antiblock encrypted config | antiblock-encrypted-config.md | implemented |
| Mobile update system | mobile-updater.md | draft |

## Overview

Rewrite the kaitu desktop + mobile app using the new k2 Go core, replacing the
old Rust 12-crate workspace (kaitu-service, kaitu-protocol k2v4, etc.) with a
drastically simplified architecture.

- **Old stack**: kaitu 0.3.22 — React 18 + MUI 5 + Emotion + Tauri v2 (full Rust backend) + kaitu-service (Go) + k2v4
- **New stack**: k2app 0.4.0 — React 19 + Tailwind + Radix + Tauri v2 (thin shell) + k2 daemon (Go) + k2v5
- **k2 repo**: `github.com/kaitu-io/k2` (open source, Go)
- **k2app repo**: `github.com/kaitu-io/k2app` (proprietary, app layer)

## Architecture

### Core Principle

Webapp is always **embedded in the native shell** (Tauri / Capacitor), NOT loaded
from k2 daemon. This ensures:
- Tauri IPC (`window.__TAURI__`) available for updater, tray, service management
- Standard Vite HMR dev workflow works

### VpnClient Abstraction

Webapp communicates with VPN backends through a `VpnClient` interface. Desktop uses
HTTP calls to k2 daemon; mobile uses Capacitor native bridge to gomobile Engine.
UI code never calls HTTP or Capacitor directly — only through VpnClient.

```typescript
interface VpnClient {
  // Commands — resolve means "command accepted", NOT "operation complete"
  connect(wireUrl: string): Promise<void>
  disconnect(): Promise<void>

  // Queries — strongly typed returns
  checkReady(): Promise<ReadyState>
  getStatus(): Promise<VpnStatus>
  getVersion(): Promise<VersionInfo>
  getUDID(): Promise<string>
  getConfig(): Promise<VpnConfig>

  // Events — unified push model (desktop: internal poll->event, mobile: native push)
  subscribe(listener: (event: VpnEvent) => void): () => void

  // Lifecycle
  destroy(): void
}

type ReadyState =
  | { ready: true; version: string }
  | { ready: false; reason: 'not_running' | 'version_mismatch' | 'not_installed' }

type VpnEvent =
  | { type: 'state_change'; state: VpnState }
  | { type: 'error'; message: string }
  | { type: 'stats'; tx: number; rx: number }  // mobile only

// Factory — supports injection for testing
function createVpnClient(override?: VpnClient): VpnClient
```

Three implementations:
- **`HttpVpnClient`** (desktop): HTTP calls to `http://127.0.0.1:1777` + internal
  `setInterval` polling converted to events. Deduplicates consecutive identical states.
- **`NativeVpnClient`** (mobile): Capacitor Plugin calls + gomobile EventHandler
  push via Capacitor Events. Zero polling.
- **`MockVpnClient`** (test): injectable for unit tests.

### Mixed Content Solution: tauri-plugin-localhost

WebKit (macOS) blocks `https://` to `http://` mixed content, even for loopback.
Solution: use `tauri-plugin-localhost` to serve webapp from `http://localhost:{port}`
instead of `https://tauri.localhost`.

| Platform | Webview Engine | Without plugin | With plugin |
|----------|---------------|----------------|-------------|
| macOS | WebKit | BLOCKED (mixed content) | OK (HTTP to HTTP) |
| Windows | WebView2 (Chromium) | OK (Chrome allows loopback) | OK |
| Linux | WebKitGTK | BLOCKED | OK |

- `window.__TAURI__` IPC still works with localhost plugin
- Security: localhost port is accessible to other local processes,
  but k2 daemon already listens on 1777, so security model is unchanged

### Desktop Architecture (macOS / Windows)

```
+------------------------------------------+
| Tauri v2 + tauri-plugin-localhost        |
|  +- Webapp (frontendDist, local embed)   |  origin: http://localhost:{port}
|  |   +- fetch("http://127.0.0.1:1777/") |  HTTP to HTTP, no mixed content
|  +- Tauri IPC: updater, tray, svc mgmt  |  window.__TAURI__ available
|  +- k2 binary (bundled as externalBin)   |
+------------------+-----------------------+
                   | HTTP (cross-origin)
                   v
+------------------------------------------+
| k2 daemon (Go binary, port 1777)         |
|  +- HTTP API (/api/core, /ping, etc.)    |
|  +- No webapp serving (built with        |
|  |   -tags nowebapp)                     |
|  +- Service manager (run --install)      |
|  +- Auto-reconnect (state file)          |
|  +- wintun.dll embedded (Windows only)   |
|  +- k2v5 tunnel engine                   |
+------------------------------------------+
```

**Tauri responsibilities**: window, tray, updater, service lifecycle (`k2 run --install`).
**k2 daemon responsibilities**: tunnel control, status, speedtest, config, UDID, wintun, auto-reconnect.

### k2 Daemon API

Base: `http://127.0.0.1:1777`

| Endpoint | Method | Description |
|----------|--------|-------------|
| `/ping` | GET | Health check -> `{"code":0,"message":"pong"}` |
| `/metrics` | GET | Memory stats, goroutines, GC info |
| `/api/core` | POST | Action router (see below) |
| `/api/device/udid` | GET | Device UDID (64-char hex SHA-256) |
| `/` | GET | Webapp SPA handler (fallback to index.html) |

Core actions (POST `/api/core` with `{"action":"...","params":{...}}`):

| Action | Params | Notes |
|--------|--------|-------|
| `up` | `wire_url`, `config_path`, `pid` (all optional) | No params = reconnect with last saved config |
| `down` | - | Disconnect tunnel |
| `status` | - | Returns: `state`, `connected_at`, `uptime_seconds`, `error`, `wire_url`, `config_path` |
| `get_config` | - | Returns: `wire_url`, `config_path` |
| `version` | - | Returns: `version`, `go`, `os`, `arch` |

Daemon states: `stopped` -> `connecting` -> `connected` -> `disconnecting` -> `stopped` (or `error`).

Auto-reconnect: daemon persists state to `/tmp/k2/state.json`. On restart, if last
state was `connected` and <1 hour old, auto-reconnects after 5s delay. No API needed.

CORS whitelist:
```
http://localhost[:port], http://127.0.0.1[:port],
https://localhost[:port], https://127.0.0.1[:port],
tauri://localhost, https://tauri.localhost
```

### k2 Service Management

k2 manages itself as a system service:

| Platform | Install command | Service name | Plist/Service location |
|----------|----------------|--------------|----------------------|
| macOS | `k2 run --install` | `io.kaitu.k2` | `/Library/LaunchDaemons/io.kaitu.k2.plist` |
| Windows | `k2 run --install` | `k2` | Windows Service (sc create) |
| Linux | `k2 run --install` | `k2` | systemd unit |

**Important**: k2 uses `run --install` (NOT `svc up`). The old kaitu Tauri shell
referenced `svc up` but that command does not exist in k2.

### Mobile Architecture

See dedicated feature specs:
- **mobile-webapp-bridge.md** — NativeVpnClient + K2Plugin TypeScript + Capacitor shell
- **mobile-vpn-ios.md** — K2Plugin.swift + PacketTunnelExtension + NE dual-process
- **mobile-vpn-android.md** — K2Plugin.kt + K2VpnService + foreground service + AAR
- **mobile-updater.md** — Web OTA + APK self-update + App Store CI

### Cloud API Access (Antiblock)

Webapp needs to call the Kaitu Cloud API (login, server list, user info, etc.).
In blocked regions the Cloud API domain may be unreachable.

**Old kaitu approach**: Go service proxied all Cloud API calls with AES-encrypted
NPM-based entry URL distribution + custom CA for domain fronting.

**k2app simplified approach**: Webapp handles everything directly. No Go proxy needed.

#### Design Principles

1. **No custom CA** — entry URLs use standard HTTPS
2. **No frontend encryption** — any key in JS is trivially extractable. Use base64
   obfuscation (prevent automated text scanning, nothing more)
3. **Un-blockable distribution** — entry URL config hosted on public CDNs that
   cannot be fully blocked (npm mirrors, jsDelivr, unpkg)
4. **Fast rotation** — when an entry is blocked, publish new config to npm package,
   all clients pick up within minutes
5. **Cloud API CORS** — fully controlled by us, allows `http://localhost:*`,
   `capacitor://localhost`, `tauri://localhost`

#### Entry URL Resolution

```
webapp startup
  +-- 1. Check localStorage cache (instant, non-blocking)
  +-- 2. Background: fetch entry config from CDN sources (JSONP)
  |    +-- https://cdn.jsdelivr.net/npm/unlock-it/config.js
  |    +-- https://unpkg.com/unlock-it/config.js
  |    +-- https://registry.npmmirror.com/unlock-it/latest (JSONP or fetch)
  |    Response: __k2_entry(["d2FwcC41Mmoub WU=", ...])
  |                          ^ base64 encoded entry URLs
  +-- 3. Decode + validate -> store to localStorage
  +-- 4. Use entry URL for all Cloud API calls
```

Fallback chain: localStorage cache -> CDN fetch -> hardcoded default entry.

#### Cloud API Call Flow

```
webapp                                          Cloud API
  |                                              (CORS: http://localhost:*)
  +-- vpnClient.getUDID()
  |    -> desktop: HTTP GET /api/device/udid
  |    -> mobile: Capacitor Plugin -> native UDID
  |
  +-- POST ${entry}/api/auth/login
  |    body: { email, code, udid }
  |    -> receives { accessToken, refreshToken }
  |
  +-- GET ${entry}/api/tunnels
  |    headers: { Authorization: Bearer ${token} }
  |    -> receives server list with wire_url per server
  |
  +-- vpnClient.connect(wire_url)
  |    -> desktop: HTTP POST /api/core action:up
  |    -> mobile: Capacitor Plugin -> Engine.Start
  |
  +-- vpnClient.subscribe(event => ...)
       -> state_change: connected
```

Token stored in localStorage. UDID fetched via `VpnClient.getUDID()` (platform-transparent).

k2 daemon / gomobile Engine is NOT involved in Cloud API calls. It stays pure:
VPN control + UDID only. Cloud API calls work identically on all platforms —
same webapp code, same antiblock module, same Cloud API client.

### Auth Flow

```
Old:  webapp -> Go service (proxy) -> antiblock entry -> Cloud API
New:  webapp -> antiblock entry -> Cloud API (direct)
              ^                    ^
        JSONP from CDN       CORS allowed
```

### Dev Mode

```
+------------------------------------------+
| Tauri dev                                |
|  +- devUrl: http://localhost:1420        |
|       +- Vite dev server + HMR           |
|            +- proxy /api/* -> 127.0.0.1:1777
|            +- proxy /ping  -> 127.0.0.1:1777
+------------------------------------------+
```

Standard Tauri + Vite workflow. `HttpVpnClient` uses relative URLs in dev
(`/api/core`), absolute URLs in prod (`http://127.0.0.1:1777/api/core`).

Cloud API calls always use the resolved entry URL (no proxy needed in dev).

### Webapp Service Readiness

Webapp loads instantly from native shell, then checks backend readiness via VpnClient:

```
webapp loaded -> vpnClient.checkReady()
  +- { ready: true, version } -> show main UI, vpnClient.subscribe(...)
  +- { ready: false, reason: 'version_mismatch' } -> trigger service reinstall
  +- { ready: false, reason: 'not_running' }  -> show "Starting service..."
  |    +- Desktop: Tauri IPC invoke("ensure_service_running")
  |    |    +- detects old kaitu-service -> cleanup + k2 run --install
  |    |    +- k2 service not installed -> k2 run --install
  |    +- retry checkReady() every 500ms
  |    +- success within 10s -> show main UI
  |    +- timeout -> show error + "Retry" button
  |         +- click -> Tauri IPC: invoke("admin_reinstall_service")
  +- { ready: false, reason: 'not_installed' } -> (mobile: should not happen)
```

On mobile, `checkReady()` always succeeds (Capacitor Plugin + gomobile
are bundled in the app). If it fails, it indicates a native code bug.

## Platform Behavior Summary

| Aspect | Desktop (HttpVpnClient) | Android (NativeVpnClient) | iOS (NativeVpnClient) |
|--------|------------------------|--------------------------|----------------------|
| Backend | k2 daemon HTTP :1777 | gomobile direct call | NEVPNManager IPC |
| connect() blocks? | Yes (daemon sync) | Yes (Engine.Start sync) | No (async, event-driven) |
| Status source | HTTP poll -> event | Engine.StatusJSON() push | sendProviderMessage + fallback |
| Events | Internal poll (2s) | EventHandler -> Capacitor | NEVPNStatusDidChange + Darwin |
| UDID source | daemon /api/device/udid | Kotlin computed -> SetUDID | UIDevice.identifierForVendor |
| TUN fd source | N/A (daemon manages) | VpnService.establish() cb | System provides to NE |
| stats event | Not available | EventHandler.OnStats | EventHandler.OnStats (via NE) |

## Decisions

### Git Management
- **k2 as git submodule** in `k2/` directory
- k2 submodule stays clean — webapp is NOT copied into k2/cloud/dist/
- k2 binary for k2app built with `-tags nowebapp` (excludes placeholder webapp)
- k2 standalone build (CLI users) keeps placeholder webapp as-is

### Distribution
- **macOS**: PKG installer (universal binary, signed + notarized)
- **Windows**: NSIS installer (signed)
- **iOS**: Capacitor + NEPacketTunnelProvider (Network Extension)
- **Android**: Capacitor + VpnService

### Windows wintun.dll — k2 Owns It
k2 embeds wintun.dll via `wintun/embed_windows.go` (`//go:embed wintun.dll`) and
extracts it next to the executable at startup (`EnsureExtracted()`). k2app does
NOT need to bundle wintun.dll.

### Identity & Versioning
- **Bundle ID**: `io.kaitu.desktop` (unchanged, enables seamless upgrade)
- **Signing**: Wordgate LLC (NJT954Q3RH) — reuse all existing certs/keys
- **Version**: Start at 0.4.0 (continuing from kaitu 0.3.22)
- **Product name**: Kaitu (unchanged)

### Version Propagation

Single source of truth: root `package.json` -> `version` field.

```
package.json (version: "0.4.0")            <- SOURCE OF TRUTH
  +-> tauri.conf.json                       <- Tauri native: "version": "../../package.json"
  +-> k2 binary -ldflags                    <- Makefile: -X main.version=$(VERSION) -X main.commit=$(COMMIT)
  +-> webapp public/version.json            <- Makefile: echo to file before build
  +-> Cargo.toml version                    <- Makefile: sed replace before build
  +-> CI release tag                        <- git tag v$(VERSION)
```

### Smooth Upgrade Path (kaitu 0.3.22 -> k2app 0.4.0)
- Tauri updater pushes 0.4.0 to existing users as normal update
- On first launch of 0.4.0:
  1. Detect and stop old `kaitu-service` (Go binary)
     - macOS: check for old launchd plist in `~/Library/LaunchAgents/` or `/Library/LaunchDaemons/`
     - Windows: check for old `kaitu-service` Windows service
  2. Remove old launchd plist / Windows service registration
  3. Install new `k2` service (`k2 run --install` with admin privileges)
  4. Webapp loads from Tauri local bundle, calls k2 daemon API
- No user data migration needed (UDID logic has changed)

### Webapp Tech Stack

| Concern | Choice | Replaces (old) |
|---------|--------|----------------|
| Framework | React 19 + Vite | React 18 + Vite |
| CSS | Tailwind CSS v4 | Emotion |
| Component primitives | Radix UI (Dialog, Popover, Switch) | MUI 5 (27 component types) |
| Class merging | cn() = clsx + tailwind-merge | - |
| Variants | CVA (Class Variance Authority) | - |
| Icons | Lucide React | @mui/icons-material |
| Toast | Sonner | custom |
| Forms | React Hook Form + Zod | custom |
| State management | Zustand (8 stores) | Zustand (7 stores) |
| Routing | React Router DOM | same |
| i18n | i18next + react-i18next | same |
| Error monitoring | Sentry (error boundary only) | same |
| Testing | Vitest + Testing Library | same |

## Desktop Acceptance Criteria

- [x] k2 submodule configured, `git clone --recursive` works
- [x] `make dev` starts k2 daemon + Vite + Tauri dev, HMR works
- [x] tauri-plugin-localhost configured, webapp served via HTTP
- [x] Webapp API calls to `http://127.0.0.1:1777` work on macOS (no mixed content)
- [x] Tauri IPC available: updater, tray, service management
- [x] k2 binary bundled as externalBin, built with `-tags nowebapp`
- [x] Service readiness: webapp shows loading state until daemon responds
- [x] Old kaitu-service cleanup on first launch after upgrade
- [x] System tray with connect/disconnect/quit
- [x] Auto-updater works with existing CDN endpoints
- [x] `make build-macos` produces signed PKG
- [x] `make build-windows` produces signed NSIS installer
- [x] Version 0.4.0 detected as upgrade from 0.3.22
- [x] Version propagation: single source from package.json
- [x] GitHub Actions pipeline builds and signs for macOS + Windows

### Webapp VpnClient
- [x] VpnClient interface with HttpVpnClient and MockVpnClient implementations
- [x] HttpVpnClient: HTTP calls + poll->event with deduplication
- [x] NativeVpnClient: Capacitor Plugin calls + native event push
- [x] MockVpnClient: injectable for unit tests
- [x] createVpnClient() factory auto-selects by platform
- [x] All UI code uses VpnClient, never direct HTTP or Capacitor calls
- [x] connect()/disconnect() resolve on "command accepted" (not operation complete)
- [x] subscribe() delivers VpnEvent for all state changes

### Mobile Acceptance Criteria

See individual feature specs: mobile-webapp-bridge.md, mobile-vpn-ios.md,
mobile-vpn-android.md, mobile-rule-storage.md, mobile-debug.md.

## k2 Repo Dependencies

Changes needed in k2 repo before k2app can be fully built:

| Change | File | Status |
|--------|------|--------|
| `nowebapp` build tag | cloud/embed.go | Done |
| nowebapp stub | cloud/embed_nowebapp.go | Done |
| wintun.dll embed | wintun/ package | Done |
| UDID cross-platform | cloud/udid_{darwin,linux,windows}.go | Done |
| Daemon UDID endpoint | daemon/api.go | Done |
| Webapp SPA serving | daemon/api.go webappHandler() | Done |
| Service manager | daemon/service_{darwin,linux,windows}.go | Done |
| Makefile targets | build-nowebapp, build-windows, build-linux, mobile | Done |
| gomobile build | mobile/ (iOS xcframework + Android AAR) | Done |
| Unified engine | engine/ (Start/Stop/Status lifecycle) | Done |
| Mobile API wrapper | mobile/api.go (gomobile type adapter) | Done |
| Windows cross-compile | k2rule syscall.Mmap upstream fix | Blocked |

All desktop-blocking and mobile-blocking k2 dependencies are resolved.
