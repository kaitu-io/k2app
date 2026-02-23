# Webapp ↔ Daemon API Contract

Daemon HTTP server: `http://127.0.0.1:1777`

All APIs are JSON over HTTP.

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

## CORS

The daemon adds CORS headers for these origins:

- `http://127.0.0.1:1777`
- `http://localhost:1777`
- `tauri://localhost`
- `https://tauri.localhost`

## User Flows

### First Use

1. User logs in via k2app → app sends credentials to Cloud API
2. Cloud returns auth token + server list
3. User selects a server → app gets `k2v5://` URL from Cloud
4. `POST /api/core` `{action:"up", params:{wire_url:"k2v5://..."}}`
5. Poll `{action:"status"}` until `state:"connected"`

### Daily Use

1. App loads → read token from localStorage
2. Validate token with Cloud API
3. `POST /api/core` `{action:"status"}` → show current state
4. If stopped → show server list + connect button
5. If connected → show status, uptime, disconnect button

### Daemon Not Running

1. `GET /ping` fails (connection refused)
2. App shows "Daemon not running. Start with: k2 run" message
