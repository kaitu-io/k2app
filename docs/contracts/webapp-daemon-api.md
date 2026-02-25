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
    "config": {
      "server": "k2v5://udid:token@host:port?ech=...&pin=...",
      "mode": "tun"
    }
  }
}
```

Response: `{"code": 0, "message": "connecting"}`

Error codes:
- `510`: No configuration provided (or legacy `wire_url` used)
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
  "config": {
    "server": "k2v5://...",
    "mode": "tun"
  }
}
```

When an error has occurred:

```json
{
  "state": "disconnected",
  "error": {
    "code": 503,
    "message": "QUIC: wire: QUIC dial: ...; TCP-WS: wire: TCP dial: connection refused"
  }
}
```

**State values**: `disconnected`, `connecting`, `connected`, `reconnecting`, `paused`

**Error codes** (in `error.code`):

| Code | Meaning |
|------|---------|
| 400 | Bad config (parse URL, missing auth) |
| 401 | Auth rejected (server rejected token) |
| 403 | Forbidden (pin mismatch, blocked CA) |
| 408 | Timeout (dial timeout, deadline exceeded) |
| 502 | Protocol error (TLS/QUIC handshake failure) |
| 503 | Server unreachable (connection refused, network unreachable) |
| 570 | Unclassified error |

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

### GET /api/events — Server-Sent Events (SSE)

Real-time status push. Connect with `EventSource` or `curl -N`.

```
GET /api/events
Accept: text/event-stream
```

Events:

```
event: status
data: {"state":"connecting"}

event: status
data: {"state":"connected","connected_at":"2026-02-25T10:00:00Z","uptime_seconds":0}

event: status
data: {"state":"disconnected","error":{"code":503,"message":"..."}}
```

- Each `status` event is a full snapshot (idempotent — safe to receive duplicates)
- Heartbeat every 15s: `: heartbeat\n\n`
- On connect: sends current status immediately
- Buffered per client (cap 8), slow clients get dropped messages

### GET /ping

Health check. Returns `{"code": 0, "message": "pong"}`.

Use this to detect if the daemon is running before showing the full UI.

### GET /metrics

Runtime metrics. Returns goroutine count, heap allocation, GC stats.

## CORS

The daemon adds CORS headers for these origins:

- `http://127.0.0.1:*` (any port)
- `http://localhost:*` (any port)
- `tauri://localhost`
- `https://tauri.localhost`

## User Flows

### First Use

1. User logs in via k2app → app sends credentials to Cloud API
2. Cloud returns auth token + server list
3. User selects a server → app gets `k2v5://` URL from Cloud
4. `POST /api/core` `{action:"up", params:{config:{server:"k2v5://..."}}}`
5. Listen on `GET /api/events` for `state:"connected"` (or poll `{action:"status"}`)

### Daily Use

1. App loads → connect to `GET /api/events` for real-time status
2. `POST /api/core` `{action:"status"}` → show current state
3. If disconnected → show server list + connect button
4. If connected → show status, uptime, disconnect button

### Daemon Not Running

1. `GET /ping` fails (connection refused)
2. App shows "Daemon not running. Start with: k2 run" message
