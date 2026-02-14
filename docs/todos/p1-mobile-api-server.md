# P1: Mobile API Server

## Background

k2app mobile (iOS/Android) embeds the same webapp as desktop. Webapp always calls
`http://127.0.0.1:1777` for API. On desktop, the k2 daemon handles this. On mobile,
the gomobile Engine needs to expose an equivalent HTTP API server in-process.

## Current Mobile API Surface

```go
// mobile/mobile.go - only 5 methods
NewEngine() *Engine
Engine.SetEventHandler(h EventHandler)
Engine.Start(url string, fd int) error
Engine.Stop() error
Engine.Status() string  // returns only state string
```

## Gap Analysis

| Daemon endpoint | Mobile equivalent | Gap |
|-----------------|-------------------|-----|
| GET /ping | - | Missing |
| POST /api/core action:up | Engine.Start() | Partial (different params) |
| POST /api/core action:down | Engine.Stop() | OK |
| POST /api/core action:status | Engine.Status() | Insufficient (only state string) |
| POST /api/core action:reconnect | - | Missing (no config persistence) |
| POST /api/core action:get_config | - | Missing |
| POST /api/core action:version | - | Missing |
| POST /api/core action:speedtest | - | Missing (phase 2) |
| GET /api/device/udid | - | Missing (need platform-specific impl) |

## Required Changes

### 1. New file: `mobile/api.go`

HTTP server that wraps Engine methods into daemon-compatible API:

```go
// StartAPIServer starts an HTTP server on the given port.
// The server exposes the same API as the k2 daemon.
func (e *Engine) StartAPIServer(port int) error

// StopAPIServer stops the HTTP API server.
func (e *Engine) StopAPIServer() error
```

Endpoints to implement:
- `GET /ping` → `{"code":0,"message":"pong"}`
- `POST /api/core` → route by action (same as daemon/api.go)
- `GET /api/device/udid` → platform UDID (see below)
- CORS: allow `capacitor://localhost`, `http://localhost`

### 2. Extend Engine status

`Engine.Status()` currently returns a bare string. Need rich status:

```go
type StatusInfo struct {
    State        string  `json:"state"`
    ConnectedAt  string  `json:"connected_at,omitempty"`
    UptimeSeconds int64  `json:"uptime_seconds"`
    WireURL      string  `json:"wire_url,omitempty"`
    Error        string  `json:"error,omitempty"`
}
```

### 3. Config persistence in Engine

Engine needs to remember `lastWireURL` for reconnect and get_config:

```go
// In Engine struct:
lastWireURL string
```

### 4. Mobile UDID

New file: `mobile/udid.go` — platform UDID via gomobile.

On mobile, UDID cannot use the same approach as desktop (ioreg, machine-id).
Options:
- (A) Expose a `SetUDID(udid string)` method — native side provides UDID
  (iOS: identifierForVendor, Android: Settings.Secure.ANDROID_ID)
- (B) Generate and persist a random UUID on first launch

Recommend option A — native side knows the real device ID.

## Not Needed

- `GET /metrics` — not useful on mobile
- `GET /` webapp serving — webapp loaded from Capacitor local assets
- PID monitoring — mobile app lifecycle managed by OS
- Speedtest — phase 2
