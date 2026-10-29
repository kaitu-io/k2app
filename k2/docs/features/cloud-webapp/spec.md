# Feature Spec: Cloud Webapp Integration

## Meta

| Field | Value |
|-------|-------|
| Feature | cloud-webapp |
| Date | 2026-02-14 |
| Status | Draft |

## Problem

k2 core (client + server + wire protocol) is complete. Self-hosting users deploy k2s and configure k2v5:// URLs manually. But paid Cloud users need a way to log in, purchase subscriptions, manage devices, and obtain k2v5 connection URLs — all from within the k2 client.

## Solution

Embed a webapp (SPA) into the k2 binary. The daemon serves it on `127.0.0.1:1777`. The webapp handles all Cloud API interactions (login, purchase, device management). The Go layer provides device identity (UDID) and tunnel control. Webapp source lives in a separate private repo; only built artifacts are embedded.

## Architecture

```
k2 binary (Go, open source)
├─ cloud/           ← NEW: webapp embedding + UDID generation
│  ├─ embed.go      (//go:embed dist/*, go generate download)
│  ├─ udid.go       (SHA-256 hash of platform machine ID)
│  └─ dist/         (webapp build output, gitignored)
├─ daemon/          ← MODIFIED: serve webapp + UDID API
│  ├─ GET /*        (SPA file server with index.html fallback)
│  └─ GET /api/device/udid
├─ cmd/k2/          ← MODIFIED: new subcommands
│  ├─ k2 open       (open browser to localhost:1777)
│  └─ k2 upgrade    (self-update binary)
└─ wire/            ← MODIFIED: Kaitu CA always-fallback
```

Webapp (SPA, can be closed source)
- Served from localhost:1777
- Login/register, purchase/manage subscriptions
- Device management (view/remove bound devices)
- Fetch k2v5 URLs from Cloud, call daemon /api/core to connect
- Directly calls Cloud API over HTTPS (CORS handled by Cloud server)

## Acceptance Criteria

### AC1: UDID Generation
- UDID is a 64-char hex string (SHA-256)
- macOS: derived from IOPlatformUUID
- Linux: derived from /etc/machine-id
- Windows: derived from Registry MachineGuid
- Plugging USB drives, changing network adapters does NOT change UDID
- Reinstalling OS produces a new UDID (acceptable)

### AC2: UDID API Endpoint
- GET /api/device/udid returns `{"code":0,"message":"ok","data":{"udid":"..."}}`
- Uses existing Response struct format
- Available when daemon is running (any tunnel state)

### AC3: Webapp Embedding
- `go generate ./cloud/...` downloads webapp zip from hardcoded URL
- Zip filename includes version: `webapp-{version}.zip`
- Extracted to cloud/dist/, which is gitignored
- `go build` embeds cloud/dist/* into binary
- Without go generate, default dist/index.html shows "Configure via CLI"

### AC4: Webapp Serving
- GET / and all non-API paths serve the embedded SPA
- SPA routing: non-file paths return index.html
- API routes (/api/*, /ping, /metrics) take priority over SPA fallback
- Existing API behavior unchanged

### AC5: CLI Commands
- `k2 open` opens default browser to http://localhost:1777
- `k2 upgrade` downloads latest binary and replaces self
- Daemon startup prints version check: "New version available: vX.Y.Z"

### AC6: Kaitu CA Always-Fallback
- BuildClientTLSConfig always includes Kaitu CA in verification, even with pin= or insecure=1
- Cloud k2v5 servers using Kaitu CA signed certs are always verifiable
- Does not break existing pin/insecure behavior for non-Kaitu certs

### AC7: Binary Self-Update
- `k2 upgrade` fetches latest.json from releases URL
- Downloads platform-specific binary (os/arch)
- Replaces current binary atomically
- Prints success message with new version

## Webapp Contract

See [docs/contracts/webapp-daemon-api.md](../../contracts/webapp-daemon-api.md) — standalone document for the webapp team covering all daemon APIs, build requirements, CORS, and user flows.

## Security Model

- **Client-side (defense against casual copying)**: UDID from OS-level machine ID, SHA-256 hashed, not user-controllable
- **Server-side (real enforcement)**: Cloud enforces max N devices per account, UDID binding on first connect, concurrent connection limits, anomaly detection
- **Open source accepted**: determined users can fork and forge UDID — server-side limits are the real barrier

## Out of Scope

- Cloud API server implementation
- Webapp source code (separate private repo)
- Webapp hot-update (binary upgrade instead)
- Docker client UDID (edge case, defer)
- Mobile UDID (iOS/Android have own SDK)
