# Webapp ↔ Daemon API Contract

Daemon HTTP server: `http://127.0.0.1:1777`

Webapp is embedded in the k2 binary and served from the daemon. All APIs are JSON over HTTP.

## Response Format

All endpoints return:

```json
{
  "code": 0,
  "message": "ok",
  "data": {}
}
```

`code=0` is success. Non-zero is error.

## Endpoints

### GET /api/device/udid

Returns a stable device fingerprint. Call this on startup to identify the device when talking to Cloud API.

```json
{
  "code": 0,
  "message": "ok",
  "data": {
    "udid": "a1b2c3d4e5f6...64-char hex string"
  }
}
```

UDID properties:
- 64-character hex string (SHA-256)
- Stable across reboots, app restarts, peripheral changes (USB, network adapters)
- Changes only on OS reinstall or hardware migration
- Generated from OS-level machine identifier (not user-controllable)

### POST /api/core

Tunnel control. All actions use the same endpoint with different `action` values.

**Request body**: `{"action": "...", "params": {...}}`

#### action: "up" — Connect tunnel

```json
{
  "action": "up",
  "params": {
    "wire_url": "k2v5://udid:token@host:port?ech=...&pin=..."
  }
}
```

Response: `{"code": 0, "message": "connecting"}`

Error codes:
- `510`: No configuration provided
- `511`: Connect failed (check `message` for details)

#### action: "down" — Disconnect tunnel

```json
{ "action": "down" }
```

Response: `{"code": 0, "message": "disconnected"}`

#### action: "status" — Connection state

```json
{ "action": "status" }
```

Response data:

```json
{
  "state": "connected",
  "connected_at": "2026-02-14T10:30:00Z",
  "uptime_seconds": 3600,
  "wire_url": "k2v5://...",
  "error": ""
}
```

Possible `state` values: `stopped`, `connecting`, `connected`, `reconnecting`, `disconnecting`, `error`.

#### action: "reconnect" — Reconnect with saved config

```json
{ "action": "reconnect" }
```

Response: `{"code": 0, "message": "reconnecting"}`

Error: `510` if no saved config.

#### action: "speedtest" — Start bandwidth test

```json
{
  "action": "speedtest",
  "params": { "server_id": "optional-server-id" }
}
```

Response: `{"code": 0, "message": "speedtest started"}`

Error: `512` if speedtest already running.

#### action: "get_speedtest_status" — Poll speedtest progress

```json
{ "action": "get_speedtest_status" }
```

Response data: speedtest progress object (download/upload Mbps, latency, state).

#### action: "get_config" — Saved config

```json
{ "action": "get_config" }
```

Response data:

```json
{
  "wire_url": "k2v5://...",
  "config_path": "/path/to/config.yaml"
}
```

#### action: "version" — Binary info

```json
{ "action": "version" }
```

Response data:

```json
{
  "version": "1.0.0",
  "go": "go1.25",
  "os": "darwin",
  "arch": "arm64"
}
```

### GET /ping

Health check. Returns `{"code": 0, "message": "pong"}`.

Use this to detect if the daemon is running before showing the full UI.

### GET /metrics

Runtime metrics. Returns goroutine count, heap allocation, GC stats.

## Webapp Build Requirements

| Item | Requirement |
|------|-------------|
| Entry point | `dist/index.html` |
| SPA routing | All frontend routes must work with server-side index.html fallback |
| Version file | `dist/version.json`: `{"version":"0.1.0"}` |
| Release artifact | `webapp-{version}.zip` — zip contents are the `dist/` root (no wrapper directory) |
| Cloud API | Webapp calls Cloud API directly via fetch() |

## CORS

The daemon adds CORS headers for these origins:

- `http://127.0.0.1:1777`
- `http://localhost:1777`
- `tauri://localhost`
- `https://tauri.localhost`

Cloud API server must also allow these origins for cross-origin requests from the webapp.

## User Flows

### First Use

1. Webapp loads at `http://localhost:1777`
2. `GET /api/device/udid` → save UDID
3. User logs in → webapp sends credentials + UDID to Cloud API
4. Cloud returns auth token + server list
5. User selects a server → webapp gets `k2v5://` URL from Cloud
6. `POST /api/core` `{action:"up", params:{wire_url:"k2v5://..."}}`
7. Poll `{action:"status"}` until `state:"connected"`

### Daily Use

1. Webapp loads → read token from localStorage
2. Validate token with Cloud API (include UDID)
3. `POST /api/core` `{action:"status"}` → show current state
4. If stopped → show server list + connect button
5. If connected → show status, uptime, disconnect button

### Device Change (OS Reinstall)

1. `GET /api/device/udid` → new UDID (different from before)
2. Login with existing credentials + new UDID → Cloud detects new device
3. If device slots not full → Cloud auto-binds new device
4. If device slots full → Cloud returns error with device list → webapp shows device management UI for user to remove old device

### Daemon Not Running

1. `GET /ping` fails (connection refused)
2. Webapp shows "Daemon not running. Start with: k2 run" message
