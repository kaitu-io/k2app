# k2r Router Release — Build & Distribution Design

## Overview

Ship k2r (gateway binary) as a standalone product for OpenWrt routers and soft-router/Linux devices. The binary embeds the React webapp for browser-based management, uses TPROXY for transparent LAN proxy, and supports one-line installation.

**Target users:**
- OpenWrt router owners (aarch64/armv7/mipsle)
- Soft-router / x86 Linux users (amd64, NAS, VM)

## Architecture

```
User's phone/laptop browser
       │
       │  http://192.168.1.1:1779
       ▼
┌──────────────────────────────────────────┐
│ k2r binary (single static binary)        │
│                                          │
│  ┌─────────────┐  ┌──────────────────┐   │
│  │ Embedded     │  │ Gateway HTTP API │   │
│  │ webapp (SPA) │  │ /api/core        │   │
│  │ //go:embed   │  │ /api/events SSE  │   │
│  │ dist/*       │  │ /api/storage     │   │
│  └─────────────┘  │ /api/platform    │   │
│                    │ /api/log-level   │   │
│                    │ /ping            │   │
│                    └──────────────────┘   │
│                                          │
│  ┌──────────────────────────────────┐    │
│  │ Gateway Engine                    │    │
│  │ TPROXY (nftables/iptables)       │    │
│  │ LAN subnet auto-discovery        │    │
│  │ engine → wire → k2s → internet   │    │
│  └──────────────────────────────────┘    │
│                                          │
│  ┌──────────────────────────────────┐    │
│  │ Server-Side Storage              │    │
│  │ /etc/k2r/storage.json            │    │
│  │ AES-256-GCM (machine-id key)     │    │
│  └──────────────────────────────────┘    │
└──────────────────────────────────────────┘
       │
       │  TPROXY intercept
       ▼
  LAN devices (all traffic proxied transparently)
```

## Components

### 1. Webapp Embedding (Go side)

**Files:** `k2/gateway/webapp_embed.go`, `k2/gateway/webapp_embed_nop.go`

Conditional embedding via build tags:

```go
// webapp_embed.go (default — webapp included)
//go:build !nowebapp

package gateway

import "embed"

//go:embed dist/*
var webappFS embed.FS
```

```go
// webapp_embed_nop.go (headless mode)
//go:build nowebapp

package gateway

import "io/fs"

var webappFS fs.FS // nil — no webapp
```

**SPA serving** in `gateway.go` HTTP mux:
- All non-API paths → serve from `webappFS`
- Unknown paths → fallback to `index.html` (SPA routing)
- Gateway injects `<script>window.__K2_GATEWAY__={version,commit,arch}</script>` into `index.html` at serve time

**Build flow:**
1. `cd webapp && yarn build` → `webapp/dist/`
2. `cp -r webapp/dist k2/gateway/dist/`
3. `CGO_ENABLED=0 GOOS=linux GOARCH={arch} go build ./cmd/k2r`

### 2. Server-Side Encrypted Storage (Go side)

**File:** `k2/gateway/storage.go`

JSON file at `/etc/k2r/storage.json` with AES-256-GCM encryption. Matches Rust `storage_crypto.rs` algorithm for consistency across platforms.

**Encryption:**
- Key derivation: HKDF-SHA256 from Linux `/etc/machine-id`
- Salt: `"kaitu-gateway-storage-v1"` (distinct from desktop `"kaitu-desktop-storage-v1"`)
- Encrypted values: `ENC1:` prefix + base64(nonce ‖ ciphertext ‖ tag)
- Plaintext values read transparently (backward compat, same as desktop)

**HTTP API (`/api/storage`):**

```
POST /api/storage
{
  "action": "get",     // get | set | remove | has | keys | clear
  "key": "auth-token",
  "value": "..."       // for set only
}

Response:
{ "code": 0, "data": <value> }
```

- Thread-safe: `sync.RWMutex` protecting in-memory map
- Atomic writes: write `.tmp` then `os.Rename` (same as desktop `storage.rs`)
- No CGO dependency (Go stdlib `crypto/aes`, `crypto/cipher`, `golang.org/x/crypto/hkdf`)

### 3. Gateway Platform Bridge (Webapp side)

**New files:**
- `webapp/src/services/gateway-k2.ts` — IK2Vpn + IPlatform implementation
- `webapp/src/services/gateway-storage.ts` — ISecureStorage via HTTP

**Platform detection** in `main.tsx`:
```typescript
// Order: Tauri → Capacitor → Gateway → Standalone
if (window.__TAURI__) { ... }
else if (Capacitor.isNativePlatform()) { ... }
else if (window.__K2_GATEWAY__) {
  const { injectGatewayGlobals } = await import('./services/gateway-k2');
  await injectGatewayGlobals();
}
else { ensureK2Injected(); }
```

**IK2Vpn implementation (`gatewayK2`):**
- `run(action, params)` → HTTP POST `/api/core` (same protocol as daemon)
- `onStatusChange(callback)` → SSE `/api/events` (gateway already has this endpoint)
- `onServiceStateChange(callback)` → SSE connection state (connected = service available)

**IPlatform implementation (`gatewayPlatform`):**

| Field | Value |
|-------|-------|
| `os` | `'linux'` |
| `platformType` | `'gateway'` |
| `version` | from `window.__K2_GATEWAY__.version` |
| `arch` | from `window.__K2_GATEWAY__.arch` |
| `commit` | from `window.__K2_GATEWAY__.commit` |
| `storage` | `gatewayStorage` (HTTP-backed) |
| `openExternal` | `window.open(url, '_blank')` |
| `writeClipboard` | `navigator.clipboard.writeText()` |
| `readClipboard` | `navigator.clipboard.readText()` |
| `syncLocale` | no-op |
| `updater` | `undefined` (future: OTA) |
| `reinstallService` | `undefined` |
| `getPid` | `undefined` |
| `setLogLevel` | POST `/api/log-level` |
| `uploadLogs` | `undefined` (future) |

**ISecureStorage implementation (`gatewayStorage`):**
```typescript
// All operations delegate to server-side storage
async get<T>(key: string): Promise<T | null> {
  const resp = await fetch('/api/storage', {
    method: 'POST',
    body: JSON.stringify({ action: 'get', key }),
  });
  const { data } = await resp.json();
  return data ?? null;
}
// set, remove, has, keys, clear — same pattern
```

### 4. Platform Type in IPlatform

**File:** `webapp/src/types/kaitu-core.ts`

Add `platformType` to `IPlatform`:

```typescript
interface IPlatform {
  os: 'windows' | 'macos' | 'linux' | 'ios' | 'android' | 'web';
  platformType: 'desktop' | 'mobile' | 'gateway' | 'web';
  // ... rest unchanged
}
```

Update all existing bridges:
- `tauri-k2.ts`: `platformType: 'desktop'`
- `capacitor-k2.ts`: `platformType: 'mobile'`
- `gateway-k2.ts`: `platformType: 'gateway'`
- `standalone-k2.ts`: `platformType: 'web'`

UI conditional rendering uses `platformType` exclusively:
```typescript
const isGateway = window._platform.platformType === 'gateway';
// Hide: updater, service reinstall, adb helper
// Show: LAN config, TPROXY port, DNS redirect
```

### 5. Gateway API Extensions

**New endpoints in `k2/gateway/api.go`:**

| Endpoint | Method | Purpose |
|----------|--------|---------|
| `/api/storage` | POST | Server-side KV storage (get/set/remove/has/keys/clear) |
| `/api/platform` | GET | Platform info `{os, arch, version, commit, platformType}` |

**Extend existing `/api/core` status action:**
- Add gateway-specific fields to status response:
  - `lanSubnets: []string` — discovered LAN subnets
  - `interceptor: string` — "nftables" or "iptables"
  - `listenPort: int` — TPROXY port

**Extend `/api/core` up action:**
- Accept gateway-specific config fields in params:
  - `lanSubnets`, `listenPort`, `dnsRedirect`

### 6. OpenWrt Packaging & Install Script

**Install script: `k2/scripts/install-k2r.sh`**

Pattern follows `install-k2s.sh`:

```bash
# Usage:
#   wget -qO- https://kaitu.io/i/k2r | sh          # install only
#   wget -qO- https://kaitu.io/i/k2r | sh -s <URL> # install + setup
```

Flow:
1. Detect arch (aarch64/x86_64/armv7/mipsle) via `uname -m`
2. Fetch `LATEST` version from CDN
3. Download binary + verify SHA256 checksum
4. Install to `/usr/bin/k2r`
5. Detect init system:
   - OpenWrt (`/etc/openwrt_release`) → install procd init.d script + LuCI integration
   - systemd → install systemd unit
6. Enable + start service
7. Print access URL: `http://<lan-ip>:1779`

**LuCI integration** (installed by script when LuCI detected):
- `luci-app-k2r/controller/k2r.lua` — menu entry under Services
- `luci-app-k2r/view/k2r.htm` — iframe to `http://127.0.0.1:1779`

**Upgrade path:**
- Re-run install script → detects existing install → downloads new version → restarts service
- Future: `k2r upgrade` CLI command (self-update from CDN)

### 7. CDN Structure

```
kaitu/k2r/
├── LATEST                        # "0.4.2"
├── install-k2r.sh                # Install script (also served at kaitu.io/i/k2r)
├── 0.4.2/
│   ├── k2r-linux-arm64           # Binary (aarch64, ~15-25MB with embedded webapp)
│   ├── k2r-linux-amd64           # Binary (x86_64)
│   ├── k2r-linux-armv7           # Binary (armv7)
│   ├── k2r-linux-mipsle          # Binary (mipsle)
│   ├── checksums.txt             # SHA256 checksums
│   └── luci-app-k2r.tar.gz      # Optional LuCI integration package
```

CDN mirrors:
- Primary: `https://dl.kaitu.io/kaitu/k2r/`
- Backup: `https://d13jc1jqzlg4yt.cloudfront.net/kaitu/k2r/`

Short URL: `https://kaitu.io/i/k2r` → redirects to install script

### 8. CI Workflow

**File:** `.github/workflows/release-openwrt.yml`

Trigger: `v*` tags (re-enable) + `workflow_dispatch`

```yaml
strategy:
  matrix:
    include:
      - { goos: linux, goarch: arm64, name: arm64 }
      - { goos: linux, goarch: amd64, name: amd64 }
      - { goos: linux, goarch: arm, goarm: '7', name: armv7 }
      - { goos: linux, goarch: mipsle, name: mipsle }
```

Steps:
1. Checkout + init k2 submodule (SSH deploy key)
2. Setup Node.js 20 + Go 1.25
3. `yarn install` + `cd webapp && yarn build`
4. Copy `webapp/dist` → `k2/gateway/dist/`
5. Cross-compile `k2r` per architecture (`CGO_ENABLED=0`)
6. qemu smoke test (`k2r -v`)
7. Generate `checksums.txt`
8. Upload binaries + checksums to S3 (`kaitu/k2r/{VERSION}/`)
9. Upload install script to S3 (`kaitu/k2r/install-k2r.sh`)
10. Update `LATEST` file
11. CDN cache invalidation
12. Slack notification

### 9. Webapp UI Adaptations

**Gateway-specific UI changes:**

| Area | Desktop/Mobile | Gateway |
|------|---------------|---------|
| Updater section | Show | Hide |
| Service reinstall | Show | Hide |
| ADB helper | Show (desktop) | Hide |
| Proxy mode toggle | Show | Hide (always TPROXY) |
| TUN mode toggle | Show | Hide |
| LAN subnet config | Hide | Show |
| TPROXY port config | Hide | Show |
| DNS redirect toggle | Hide | Show |
| Interceptor status | Hide | Show (nft/ipt) |

**Implementation:** Conditional rendering based on `_platform.platformType === 'gateway'` in relevant components. No new pages — gateway settings integrate into existing Dashboard/Settings UI.

### 10. Init Scripts

**procd (OpenWrt) — `k2r.init`:**
```sh
#!/bin/sh /etc/rc.common
START=99
STOP=10
USE_PROCD=1

start_service() {
    procd_open_instance
    procd_set_param command /usr/bin/k2r
    procd_set_param respawn
    procd_set_param stdout 1
    procd_set_param stderr 1
    procd_close_instance
}
```

**systemd — `k2r.service`:**
```ini
[Unit]
Description=k2r Gateway
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
ExecStart=/usr/bin/k2r
Restart=always
RestartSec=5
LimitNOFILE=65535

[Install]
WantedBy=multi-user.target
```

Both generated by `k2r service install` (already implemented in `k2/cmd/k2r/service_linux.go`).

## Out of Scope (Future Iterations)

- **OTA self-update**: `k2r upgrade` command downloading new binary from CDN
- **Connected device list**: ARP/nftables conntrack to show LAN clients
- **Per-device traffic stats**: nftables counter per source IP
- **Multi-account**: Multiple k2v5 server connections
- **ipk packaging**: Native OpenWrt package format (opkg install)
- **Web install page**: Router tab on kaitu.io/install with arch auto-detection

## File Change Summary

### New files (k2 submodule)
- `k2/gateway/webapp_embed.go` — webapp `//go:embed`
- `k2/gateway/webapp_embed_nop.go` — nowebapp stub
- `k2/gateway/webapp_serve.go` — SPA static file handler + `__K2_GATEWAY__` injection
- `k2/gateway/storage.go` — server-side encrypted KV storage
- `k2/gateway/storage_test.go` — storage unit tests
- `k2/scripts/install-k2r.sh` — one-line install script

### New files (webapp)
- `webapp/src/services/gateway-k2.ts` — gateway platform bridge
- `webapp/src/services/gateway-storage.ts` — HTTP-backed ISecureStorage

### Modified files (k2 submodule)
- `k2/gateway/api.go` — add `/api/storage`, `/api/platform`, extend status response
- `k2/gateway/gateway.go` — add webapp serving to HTTP mux
- `k2/cmd/k2r/main.go` — add `version` action support

### Modified files (webapp)
- `webapp/src/types/kaitu-core.ts` — add `platformType` to IPlatform
- `webapp/src/main.tsx` — add gateway detection in bootstrap
- `webapp/src/services/tauri-k2.ts` — add `platformType: 'desktop'`
- `webapp/src/services/capacitor-k2.ts` — add `platformType: 'mobile'`
- `webapp/src/services/standalone-k2.ts` — add `platformType: 'web'`

### Modified files (CI/scripts)
- `.github/workflows/release-openwrt.yml` — rewrite for k2r
- `scripts/openwrt/` — update for k2r (rename, new init scripts)
- `scripts/build-openwrt.sh` — update for k2r

### Modified files (web)
- `web/src/lib/constants.ts` — add router download links
- `web/src/lib/downloads.ts` — add router channel
