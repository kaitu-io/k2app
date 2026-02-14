# Feature Spec: k2app Rewrite

> **Status**: Draft v7
> **Created**: 2026-02-14
> **Feature**: Rewrite kaitu app on top of k2 core

## Overview

Rewrite the kaitu desktop + mobile app using the new k2 Go core, replacing the
old Rust 12-crate workspace (kaitu-service, kaitu-protocol k2v4, etc.) with a
drastically simplified architecture.

## Context

- **Old stack**: kaitu 0.3.22 — React 18 + MUI 5 + Emotion + Tauri v2 (full Rust backend) + kaitu-service (Go) + k2v4
- **New stack**: k2app 0.4.0 — React 18 + Tailwind + Radix + Tauri v2 (thin shell) + k2 daemon (Go) + k2v5
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

  // Events — unified push model (desktop: internal poll→event, mobile: native push)
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

Two implementations:
- **`HttpVpnClient`** (desktop): HTTP calls to `http://127.0.0.1:1777` + internal
  `setInterval` polling converted to events. Deduplicates consecutive identical states.
- **`NativeVpnClient`** (mobile): Capacitor Plugin calls + gomobile EventHandler
  push via Capacitor Events. Zero polling.

### Mixed Content Solution: tauri-plugin-localhost

WebKit (macOS) blocks `https://` → `http://` mixed content, even for loopback.
Solution: use `tauri-plugin-localhost` to serve webapp from `http://localhost:{port}`
instead of `https://tauri.localhost`.

| Platform | Webview Engine | Without plugin | With plugin |
|----------|---------------|----------------|-------------|
| macOS | WebKit | BLOCKED (mixed content) | OK (HTTP→HTTP) |
| Windows | WebView2 (Chromium) | OK (Chrome allows loopback) | OK |
| Linux | WebKitGTK | BLOCKED | OK |

- `window.__TAURI__` IPC still works with localhost plugin
- Security: localhost port is accessible to other local processes,
  but k2 daemon already listens on 1777, so security model is unchanged

### Desktop (macOS DMG / Windows NSIS)

```
┌──────────────────────────────────────────┐
│ Tauri v2 + tauri-plugin-localhost        │
│  ├─ Webapp (frontendDist, local embed)   │  origin: http://localhost:{port}
│  │   └─ fetch("http://127.0.0.1:1777/…")│  HTTP→HTTP, no mixed content
│  ├─ Tauri IPC: updater, tray, svc mgmt  │  window.__TAURI__ available
│  └─ k2 binary (bundled as externalBin)   │
└──────────────────┬───────────────────────┘
                   │ HTTP (cross-origin)
                   ↓
┌──────────────────────────────────────────┐
│ k2 daemon (Go binary, port 1777)         │
│  ├─ HTTP API (/api/core, /ping, etc.)    │
│  ├─ No webapp serving (built with        │
│  │   -tags nowebapp)                     │
│  ├─ Service manager (run --install)      │
│  ├─ Auto-reconnect (state file)          │
│  ├─ wintun.dll embedded (Windows only)   │
│  └─ k2v5 tunnel engine                  │
└──────────────────────────────────────────┘
```

**Tauri responsibilities**: window, tray, updater, service lifecycle (`k2 run --install`).
**k2 daemon responsibilities**: tunnel control, status, speedtest, config, UDID, wintun, auto-reconnect.

### k2 Daemon API (current)

Base: `http://127.0.0.1:1777`

| Endpoint | Method | Description |
|----------|--------|-------------|
| `/ping` | GET | Health check → `{"code":0,"message":"pong"}` |
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
| `speedtest` | `server_id` (optional) | Async, returns immediately |
| `get_speedtest_status` | - | Speedtest progress |
| `get_config` | - | Returns: `wire_url`, `config_path` |
| `version` | - | Returns: `version`, `go`, `os`, `arch` |

Daemon states: `stopped` → `connecting` → `connected` → `disconnecting` → `stopped` (or `error`).

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

Uninstall: `ServiceManager.Uninstall()` (stops + removes service).
IsInstalled: `ServiceManager.IsInstalled()` (checks plist/service existence).

**Important**: k2 uses `run --install` (NOT `svc up`). The old kaitu Tauri shell
referenced `svc up` but that command does not exist in k2. k2app must use the
correct command.

### k2 CLI Commands (reference)

```
k2 up [URL|config.yaml]     Connect (auto-manages daemon)
k2 down                     Disconnect
k2 status                   Show connection status
k2 speedtest                Run speed test
k2 run                      Start daemon (called by service manager)
k2 run --install            Install as system service + start
k2 run -c <config.yaml>     Client foreground mode (dev/debug, needs sudo)
k2 open                     Open webapp in browser
k2 upgrade [--check]        Download and install latest version from CDN
k2 version                  Show version + commit hash
k2 demo-config              Print example client config
```

### Mobile (iOS / Android) — Deferred

Mobile uses **Capacitor native bridge** instead of HTTP. No HTTP API server on
mobile. Webapp uses the same `VpnClient` interface — `NativeVpnClient` calls
Capacitor Plugin which calls gomobile Engine through native code.

#### Why Not HTTP on Mobile

Flow simulation revealed that an HTTP server approach has critical flaws:
1. **iOS**: Engine runs in NE (Network Extension) separate process. HTTP server
   in NE can't serve before VPN starts. HTTP server in main app requires
   bridging anyway — no simpler than Capacitor Plugin.
2. **Android**: `VpnService.establish()` is a Java API. Go HTTP server can't
   call it directly — needs native callback regardless.
3. **Security**: localhost HTTP server accessible to all apps on device.
4. **Battery**: 2s polling on mobile drains battery. Native events are free.

#### Fundamental Platform Difference

iOS and Android have **different VPN process models**. The Capacitor Plugin
is a thin shell on both, but the underlying mechanics differ:

```
iOS:    Capacitor Plugin (Swift) → NEVPNManager IPC → NE Process → Engine
Android: Capacitor Plugin (Kotlin) → direct gomobile call → Engine (same process)
```

#### Android Architecture

```
┌──────────────────── App Process (single) ─────────────────┐
│                                                            │
│  Capacitor + Webapp                                        │
│       │ NativeVpnClient                                    │
│       ▼                                                    │
│  Capacitor Plugin (Kotlin, thin shell)                     │
│       │ direct call                                        │
│       ▼                                                    │
│  MobileAPI (Go, mobile/api.go)                             │
│       │ unified action dispatch in Go                      │
│       ▼                                                    │
│  Engine.Start(url, fd) / Stop() / StatusJSON()             │
│       ▲                                                    │
│       │ TUN fd via callback                                │
│  VpnService (Kotlin)                                       │
│       └── establish() → ParcelFileDescriptor → fd          │
│                                                            │
│  EventHandler.OnStateChange()                              │
│       → MobileAPI → Capacitor notifyListeners()            │
│       → webapp subscribe listener                          │
└────────────────────────────────────────────────────────────┘
```

- Capacitor Plugin is **pure forwarding** — no action dispatch logic
- `MobileAPI` in Go handles all action dispatch (written once, shared)
- TUN fd obtained via Go→Kotlin callback (`TUNProvider` interface)
- Events: `EventHandler` → `MobileAPI` → Capacitor `notifyListeners`
- `connect()` is synchronous (blocks until Engine.Start completes)

#### iOS Architecture

```
┌──────────────────── Main App Process ─────────────────────┐
│                                                            │
│  Capacitor + Webapp                                        │
│       │ NativeVpnClient                                    │
│       ▼                                                    │
│  Capacitor Plugin (Swift, NE bridge layer)                 │
│       │                                                    │
│       ├── checkReady / getUDID / getVersion                │
│       │   → local, no NE needed (always available)         │
│       │                                                    │
│       ├── connect(wireUrl)                                 │
│       │   → NEVPNManager.startVPNTunnel(options: wireUrl)  │
│       │   → Promise resolves = command sent (async)        │
│       │                                                    │
│       ├── disconnect()                                     │
│       │   → connection.stopVPNTunnel()                     │
│       │                                                    │
│       ├── getStatus()                                      │
│       │   → sendProviderMessage("status")                  │
│       │   → timeout 5s, fallback to NEVPNConnection.status │
│       │     mapped to VpnStatus (coarse)                   │
│       │                                                    │
│       └── subscribe(listener)                              │
│           → observe NEVPNStatusDidChange notification       │
│           → on change: sendProviderMessage for rich data   │
│           → emit VpnEvent to webapp                        │
│                                                            │
└────────────────────────┬──────────────────────────────────┘
                         │ NEVPNManager / sendProviderMessage
                         │ (cross-process RPC, ≤1MB, 30s timeout)
                         ▼
┌──────────────────── NE Process ───────────────────────────┐
│                                                            │
│  NEPacketTunnelProvider (Swift)                             │
│       ▼                                                    │
│  MobileAPI (Go, same mobile/api.go)                        │
│       ▼                                                    │
│  Engine.Start(url, fd) / Stop() / StatusJSON()             │
│       │ fd provided by system                              │
│                                                            │
│  handleAppMessage(data:) → route to MobileAPI              │
│       → parse "status" request                             │
│       → MobileAPI.CoreAction("status", "")                 │
│       → serialize JSON response back                       │
│                                                            │
│  EventHandler.OnStateChange()                              │
│       → write to App Group UserDefaults                    │
│       → post Darwin Notification                           │
│         (main app observes → triggers subscribe event)     │
│                                                            │
└────────────────────────────────────────────────────────────┘
```

- `checkReady`/`getUDID`/`getVersion` run locally — **no NE needed at launch**
- `connect()` resolves when command is sent, NOT when VPN is up (async)
- Rich status via `sendProviderMessage` with 5s timeout; fallback to
  `NEVPNConnection.status` coarse mapping (connected/disconnecting/etc.)
- Events via dual channel: `NEVPNStatusDidChange` (system, reliable) +
  Darwin Notification (custom data from Engine EventHandler)
- NE cross-process RPC has explicit error handling: timeout, NE killed,
  data size limit

#### iOS NE Communication Layer

The cross-process bridge between main app and NE deserves explicit design:

| Mechanism | Direction | Use |
|-----------|-----------|-----|
| `NEVPNManager.startVPNTunnel(options:)` | App → NE | Start VPN, pass wire_url in providerConfiguration |
| `connection.stopVPNTunnel()` | App → NE | Stop VPN |
| `NETunnelProviderSession.sendProviderMessage()` | App → NE → App | Request/response RPC (status query) |
| `NEVPNStatusDidChange` notification | NE → App | Coarse state changes (system-level) |
| Darwin Notification | NE → App | Custom event signal (Engine state change) |
| App Group UserDefaults | NE ↔ App | Shared persistent data (last wire_url, rich status) |

Constraints:
- `sendProviderMessage` payload ≤ ~1MB, timeout 30s
- NE process may be killed by system at any time — always handle failure
- Darwin Notifications carry no payload — use App Group for data

#### MobileAPI (k2 side, new: `mobile/api.go`)

Unified Go-side API for both platforms. Capacitor Plugin (Kotlin/Swift) calls
into MobileAPI. No HTTP server needed.

```go
// MobileAPI provides unified action dispatch for mobile platforms.
// Android: called directly from Capacitor Plugin (Kotlin).
// iOS: called from NEPacketTunnelProvider via handleAppMessage.
type MobileAPI struct {
    engine *Engine
    udid   string
    mu     sync.Mutex
}

func NewMobileAPI(engine *Engine) *MobileAPI
func (m *MobileAPI) SetUDID(udid string)

// CoreAction dispatches an action and returns JSON response.
// Actions: up, down, status, get_config, version.
// Same action names as daemon for consistency.
func (m *MobileAPI) CoreAction(action, paramsJSON string) string

// HandleProviderMessage handles iOS sendProviderMessage data.
// Parses request, routes to CoreAction, returns serialized response.
func (m *MobileAPI) HandleProviderMessage(data []byte) []byte
```

Current gomobile Engine API (5 methods):
```go
NewEngine() *Engine
SetEventHandler(h EventHandler)
Status() string                   // bare state string
Start(url string, fd int) error
Stop() error
```

New methods needed:
```go
Engine.SetUDID(udid string)       // Native side provides device ID
Engine.StatusJSON() string        // Rich status as JSON (state, uptime, wire_url, error)
```

#### Platform Behavior Differences (transparent to webapp)

| Aspect | Desktop (HttpVpnClient) | Android (NativeVpnClient) | iOS (NativeVpnClient) |
|--------|------------------------|--------------------------|----------------------|
| Backend | k2 daemon HTTP :1777 | gomobile direct call | NEVPNManager IPC |
| connect() blocks? | Yes (daemon sync) | Yes (Engine.Start sync) | No (async, event-driven) |
| Status source | HTTP poll → event | Engine.StatusJSON() push | sendProviderMessage + fallback |
| Events | Internal poll (2s) | EventHandler → Capacitor | NEVPNStatusDidChange + Darwin |
| UDID source | daemon /api/device/udid | Kotlin computed → SetUDID | UIDevice.identifierForVendor |
| TUN fd source | N/A (daemon manages) | VpnService.establish() cb | System provides to NE |
| stats event | Not available | EventHandler.OnStats | EventHandler.OnStats (via NE) |

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
  ├── 1. Check localStorage cache (instant, non-blocking)
  ├── 2. Background: fetch entry config from CDN sources (JSONP)
  │    ├── https://cdn.jsdelivr.net/npm/unlock-it/config.js
  │    ├── https://unpkg.com/unlock-it/config.js
  │    └── https://registry.npmmirror.com/unlock-it/latest (JSONP or fetch)
  │    Response: __k2_entry(["d2FwcC41Mmoub WU=", ...])
  │                          ↑ base64 encoded entry URLs
  ├── 3. Decode + validate → store to localStorage
  └── 4. Use entry URL for all Cloud API calls
```

Fallback chain: localStorage cache → CDN fetch → hardcoded default entry.

#### Cloud API Call Flow

```
webapp                                          Cloud API
  │                                              (CORS: http://localhost:*)
  ├── vpnClient.getUDID()
  │    → desktop: HTTP GET /api/device/udid
  │    → mobile: Capacitor Plugin → native UDID
  │
  ├── POST ${entry}/api/auth/login
  │    body: { email, code, udid }
  │    → receives { accessToken, refreshToken }
  │
  ├── GET ${entry}/api/tunnels
  │    headers: { Authorization: Bearer ${token} }
  │    → receives server list with wire_url per server
  │
  └── vpnClient.connect(wire_url)
  │    → desktop: HTTP POST /api/core action:up
  │    → mobile: Capacitor Plugin → Engine.Start
  │
  └── vpnClient.subscribe(event => ...)
       → state_change: connected
```

Token stored in localStorage. UDID fetched via `VpnClient.getUDID()` (platform-transparent).

#### Architecture Difference from Old Kaitu

```
Old:  webapp → Go service (proxy) → antiblock entry → Cloud API
New:  webapp → antiblock entry → Cloud API (direct)
              ↑                    ↑
        JSONP from CDN       CORS allowed
```

k2 daemon / gomobile Engine is NOT involved in Cloud API calls. It stays pure:
VPN control + UDID only. Cloud API calls work identically on all platforms —
same webapp code, same antiblock module, same Cloud API client.

### Dev Mode

```
┌──────────────────────────────────────────┐
│ Tauri dev                                │
│  └─ devUrl: http://localhost:1420        │
│       └─ Vite dev server + HMR           │
│            └─ proxy /api/* → 127.0.0.1:1777
│            └─ proxy /ping  → 127.0.0.1:1777
└──────────────────────────────────────────┘
```

Standard Tauri + Vite workflow. `HttpVpnClient` uses relative URLs in dev
(`/api/core`), absolute URLs in prod (`http://127.0.0.1:1777/api/core`).

Cloud API calls always use the resolved entry URL (no proxy needed in dev).

```typescript
// HttpVpnClient (desktop) daemon base URL
const DAEMON_BASE = import.meta.env.DEV ? '' : 'http://127.0.0.1:1777';

// Cloud API base URL (resolved by antiblock, same on all platforms)
const CLOUD_BASE = resolveEntryUrl(); // from antiblock module

// VpnClient factory — auto-selects implementation
import { createVpnClient } from './vpn-client';
const vpnClient = createVpnClient();
// Desktop → HttpVpnClient, Mobile → NativeVpnClient, Test → MockVpnClient
```

## Decisions

### Git Management
- **k2 as git submodule** in `k2/` directory
- k2 submodule stays clean — webapp is NOT copied into k2/cloud/dist/
- k2 binary for k2app built with `-tags nowebapp` (excludes placeholder webapp)
- k2 standalone build (CLI users) keeps placeholder webapp as-is
- k2 `cloud/embed.go` has `//go:build !nowebapp` tag (done)
- k2 `cloud/embed_nowebapp.go` provides stub FS() for nowebapp builds (done)

### Distribution
- **macOS**: DMG only (no App Store / Network Extension for now)
- **Windows**: NSIS installer
- **iOS**: Capacitor + NEPacketTunnelProvider (Network Extension), deferred
- **Android**: Capacitor + VpnService, deferred

### Windows wintun.dll — k2 Owns It
k2 embeds wintun.dll via `wintun/embed_windows.go` (`//go:embed wintun.dll`) and
extracts it next to the executable at startup (`EnsureExtracted()`). k2app does
NOT need to bundle wintun.dll. Done in k2 repo.

### Identity & Versioning
- **Bundle ID**: `io.kaitu.desktop` (unchanged, enables seamless upgrade)
- **Signing**: Wordgate LLC (NJT954Q3RH) — reuse all existing certs/keys
- **Version**: Start at 0.4.0 (continuing from kaitu 0.3.22)
- **Product name**: Kaitu (unchanged)

### Version Propagation

Single source of truth: root `package.json` → `version` field.

```
package.json (version: "0.4.0")            ← SOURCE OF TRUTH
  ├─→ tauri.conf.json                       ← Tauri native: "version": "../../package.json"
  ├─→ k2 binary -ldflags                    ← Makefile: -X main.version=$(VERSION) -X main.commit=$(COMMIT)
  ├─→ webapp public/version.json            ← Makefile: echo to file before build
  ├─→ Cargo.toml version                    ← Makefile: sed replace before build
  └─→ CI release tag                        ← git tag v$(VERSION)
```

k2 binary has two ldflags: `version` (from package.json) and `commit` (from git).

```makefile
VERSION := $(shell node -p "require('./package.json').version")
COMMIT  := $(shell cd k2 && git rev-parse --short HEAD)

pre-build:
	echo '{"version":"$(VERSION)"}' > webapp/public/version.json

build-k2:
	cd k2 && go build -tags nowebapp \
		-ldflags "-X main.version=$(VERSION) -X main.commit=$(COMMIT)" \
		-o ../desktop/src-tauri/binaries/k2-$(TARGET) ./cmd/k2
```

### Webapp Tech Stack

| Concern | Choice | Replaces (old) |
|---------|--------|----------------|
| Framework | React 18 + Vite | same |
| CSS | Tailwind CSS | Emotion |
| Component primitives | Radix UI (Dialog, Popover, Switch) | MUI 5 (27 component types) |
| Class merging | cn() = clsx + tailwind-merge | - |
| Variants | CVA (Class Variance Authority) | - |
| Icons | Lucide React | @mui/icons-material |
| Toast | Sonner | custom |
| Forms | React Hook Form + Zod | custom |
| State management | Zustand (3-4 stores) | Zustand (7 stores) |
| Routing | React Router DOM | same |
| i18n | i18next + react-i18next | same |
| Error monitoring | Sentry (error boundary only) | same |
| Testing | Vitest + Testing Library | same |

**Expected bundle reduction**: ~1.9MB → ~600KB (MUI+Emotion removed).

### Smooth Upgrade Path (kaitu 0.3.22 → k2app 0.4.0)
- Tauri updater pushes 0.4.0 to existing users as normal update
- On first launch of 0.4.0:
  1. Detect and stop old `kaitu-service` (Go binary)
     - macOS: check for old launchd plist in `~/Library/LaunchAgents/` or `/Library/LaunchDaemons/`
     - Windows: check for old `kaitu-service` Windows service
  2. Remove old launchd plist / Windows service registration
  3. Install new `k2` service (`k2 run --install` with admin privileges)
     - macOS: osascript for admin prompt → `k2 run --install` → creates `io.kaitu.k2` plist
     - Windows: PowerShell RunAs → `k2.exe run --install` → creates `k2` Windows service
  4. Webapp loads from Tauri local bundle, calls k2 daemon API
- No user data migration needed (UDID logic has changed)

### CI/CD
- GitHub Actions (simplified from kaitu workflows)
- Go build replaces Rust cross-compilation (much faster)
- Note: Windows cross-compile currently blocked by k2rule syscall.Mmap
  (need native Windows build runner or upstream fix)

## Project Structure

```
k2app/
├── package.json                 # workspace root + version source of truth
├── k2/                          # git submodule → kaitu-io/k2
├── webapp/                      # React + Vite + Tailwind (rewritten)
│   ├── src/
│   │   ├── vpn-client/
│   │   │   ├── types.ts         # VpnClient interface, VpnEvent, VpnStatus, ReadyState
│   │   │   ├── http-client.ts   # HttpVpnClient (desktop: HTTP + poll→event)
│   │   │   ├── native-client.ts # NativeVpnClient (mobile: Capacitor Plugin)
│   │   │   ├── mock-client.ts   # MockVpnClient (testing)
│   │   │   └── index.ts         # createVpnClient() factory
│   │   ├── api/
│   │   │   ├── cloud.ts         # Cloud API client (login, servers, user)
│   │   │   ├── antiblock.ts     # Entry URL resolution (JSONP + CDN + cache)
│   │   │   └── types.ts         # Cloud API types
│   │   ├── components/          # shared components
│   │   ├── pages/               # route pages (~5-7, down from 20)
│   │   ├── stores/              # Zustand stores (3-4, down from 7)
│   │   ├── i18n/                # translations (zh-CN, en-US)
│   │   └── lib/                 # cn(), constants
│   ├── public/
│   │   └── version.json         # generated by build script
│   ├── vite.config.ts           # proxy /api/* → 1777 in dev
│   └── package.json
├── desktop/                     # Tauri v2 thin shell
│   ├── src-tauri/
│   │   ├── tauri.conf.json      # bundle: io.kaitu.desktop, frontendDist
│   │   ├── Cargo.toml           # tauri + updater + tray + single-instance + localhost
│   │   ├── src/
│   │   │   ├── main.rs          # setup, window
│   │   │   ├── service.rs       # k2 run --install, old kaitu-service cleanup
│   │   │   ├── tray.rs          # system tray
│   │   │   └── updater.rs       # auto-update
│   │   ├── binaries/            # k2 binary (go build output, per target triple)
│   │   ├── keys/                # signing keys (from kaitu)
│   │   └── icons/               # app icons (from kaitu)
│   └── package.json
├── mobile/                      # iOS + Android (deferred)
│   ├── ios/
│   │   ├── App/                 # Capacitor iOS app
│   │   ├── K2Plugin/            # Capacitor Plugin (Swift, NE bridge)
│   │   └── K2Tunnel/            # NEPacketTunnelProvider (NE target)
│   ├── android/
│   │   ├── app/                 # Capacitor Android app
│   │   └── k2plugin/            # Capacitor Plugin (Kotlin) + VpnService
│   └── package.json
├── scripts/
│   ├── build-k2.sh              # go build -tags nowebapp
│   └── dev.sh                   # start k2 daemon + vite + tauri dev
├── .github/workflows/
│   ├── release-desktop.yml      # macOS + Windows build/sign/publish
│   └── ci.yml                   # lint + test + build check
├── Makefile                     # top-level build orchestration
└── docs/
    ├── contracts/
    │   └── webapp-daemon-api.md
    └── features/
        └── k2app-rewrite/
            └── spec.md          # this file
```

Root `package.json` workspaces:
```json
{
  "name": "k2app",
  "version": "0.4.0",
  "private": true,
  "workspaces": ["webapp", "desktop", "mobile"]
}
```

## Build Flow

### Desktop Release
```
1. make pre-build                             # write version.json
2. yarn build                                 # webapp → webapp/dist/
3. make build-k2 TARGET=<target-triple>       # k2 binary → desktop/src-tauri/binaries/
4. cd desktop && yarn tauri build             # Tauri bundles webapp + k2 binary
5. Sign + notarize (macOS) / sign (Windows)
6. Upload to CDN, update latest.json
```

### Mobile Release (deferred)
```
1. make pre-build && yarn build               # webapp → webapp/dist/
2. cd k2 && gomobile bind -target=ios ...     # xcframework (includes MobileAPI)
3. cd k2 && gomobile bind -target=android ... # aar (includes MobileAPI)
4. cd mobile/ios && xcodebuild                # App + NE extension + Capacitor Plugin
5. cd mobile/android && ./gradlew assemble    # App + VpnService + Capacitor Plugin
```

### Dev Mode
```
1. cd k2 && go build -tags nowebapp -o build/k2 ./cmd/k2 && ./build/k2 run
2. cd desktop && yarn tauri dev               # Vite 1420 + Tauri shell
   (Vite proxies /api/* and /ping to 127.0.0.1:1777)
```

## Webapp Service Readiness

Webapp loads instantly from native shell, then checks backend readiness via VpnClient:

```
webapp loaded → vpnClient.checkReady()
  ├─ { ready: true, version } → show main UI, vpnClient.subscribe(...)
  ├─ { ready: false, reason: 'version_mismatch' } → trigger service reinstall
  ├─ { ready: false, reason: 'not_running' }  → show "Starting service..."
  │    ├─ Desktop: Tauri IPC invoke("ensure_service_running")
  │    │    ├─ detects old kaitu-service → cleanup + k2 run --install
  │    │    └─ k2 service not installed → k2 run --install
  │    ├─ retry checkReady() every 500ms
  │    ├─ success within 10s → show main UI
  │    └─ timeout → show error + "Retry" button
  │         └─ click → Tauri IPC: invoke("admin_reinstall_service")
  └─ { ready: false, reason: 'not_installed' } → (mobile: should not happen)
```

On mobile, `checkReady()` always succeeds (Capacitor Plugin + gomobile
are bundled in the app). If it fails, it indicates a native code bug.

## Acceptance Criteria

### Desktop
- [ ] k2 submodule configured, `git clone --recursive` works
- [ ] `make dev` starts k2 daemon + Vite + Tauri dev, HMR works
- [ ] tauri-plugin-localhost configured, webapp served via HTTP
- [ ] Webapp API calls to `http://127.0.0.1:1777` work on macOS (no mixed content)
- [ ] Tauri IPC available: updater, tray, service management
- [ ] k2 binary bundled as externalBin, built with `-tags nowebapp`
- [ ] Service readiness: webapp shows loading state until daemon responds
- [ ] Old kaitu-service cleanup on first launch after upgrade
- [ ] System tray with connect/disconnect/quit
- [ ] Auto-updater works with existing CDN endpoints
- [ ] `make build-macos` produces signed DMG
- [ ] `make build-windows` produces signed NSIS installer
- [ ] Version 0.4.0 detected as upgrade from 0.3.22
- [ ] Version propagation: single source from package.json

### Webapp VpnClient
- [ ] VpnClient interface with HttpVpnClient and NativeVpnClient implementations
- [ ] HttpVpnClient: HTTP calls + poll→event with deduplication
- [ ] NativeVpnClient: Capacitor Plugin calls + native event push
- [ ] MockVpnClient: injectable for unit tests
- [ ] createVpnClient() factory auto-selects by platform
- [ ] All UI code uses VpnClient, never direct HTTP or Capacitor calls
- [ ] connect()/disconnect() resolve on "command accepted" (not operation complete)
- [ ] subscribe() delivers VpnEvent for all state changes

### Mobile (deferred)
- [ ] Webapp loads from local assets (same build as desktop)
- [ ] k2 MobileAPI in Go with CoreAction() and HandleProviderMessage()
- [ ] Android: Capacitor Plugin (Kotlin) → MobileAPI → Engine (single process)
- [ ] Android: VpnService provides TUN fd via callback
- [ ] Android: EventHandler → Capacitor notifyListeners → webapp events
- [ ] iOS: Capacitor Plugin (Swift) → NEVPNManager → NE Process → Engine
- [ ] iOS: checkReady/getUDID/getVersion work without NE running
- [ ] iOS: sendProviderMessage for rich status, fallback to NEVPNConnection.status
- [ ] iOS: NEVPNStatusDidChange + Darwin Notification → webapp events
- [ ] iOS: App Group UserDefaults for NE↔App shared state

### CI/CD
- [ ] GitHub Actions pipeline builds and signs for macOS + Windows
- [ ] Go build with `-tags nowebapp` in CI

---

## k2 Repo Dependencies

Changes needed in k2 repo before k2app can be fully built:

| Change | File | Status | Priority |
|--------|------|--------|----------|
| `nowebapp` build tag | cloud/embed.go | Done | - |
| nowebapp stub | cloud/embed_nowebapp.go | Done | - |
| wintun.dll embed | wintun/ package | Done | - |
| UDID cross-platform | cloud/udid_{darwin,linux,windows}.go | Done | - |
| Daemon UDID endpoint | daemon/api.go | Done | - |
| Webapp SPA serving | daemon/api.go webappHandler() | Done | - |
| Service manager | daemon/service_{darwin,linux,windows}.go | Done | - |
| Makefile targets | build-nowebapp, build-windows, build-linux, mobile | Done | - |
| gomobile build | mobile/ (iOS xcframework + Android AAR) | Done | - |
| MobileAPI action dispatch | mobile/api.go (new) | Todo | P1 (deferred) |
| MobileAPI.HandleProviderMessage | mobile/api.go (iOS NE bridge) | Todo | P1 (deferred) |
| Engine.StatusJSON() | mobile/mobile.go (rich status) | Todo | P1 (deferred) |
| Engine.SetUDID() | mobile/mobile.go | Todo | P1 (deferred) |
| Windows cross-compile | k2rule syscall.Mmap upstream fix | Blocked | P2 |

All desktop-blocking k2 dependencies are resolved. Mobile is deferred.
Mobile no longer needs HTTP server or CORS — uses Capacitor native bridge instead.
