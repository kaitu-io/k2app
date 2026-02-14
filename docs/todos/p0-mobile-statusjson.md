# P0: Engine.StatusJSON() for Mobile

## Problem

`Engine.Status()` returns a bare state string (`"connected"`). Mobile clients
need rich status on cold start — user opens app, VPN already running in
background (iOS NE / Android VpnService), webapp calls `getStatus()` and needs
state + connectedAt + uptimeSeconds + wireUrl + error.

Without `StatusJSON()`, the cold start scenario fails: webapp only gets
`"connected"` but can't show uptime, server name, or error details.

## Solution

Add `StatusJSON() string` to Engine. Track `wireUrl`, `connectedAt`, `lastError`
internally. Return JSON matching the daemon's `statusInfo()` format (daemon/daemon.go:268).

## Current State

```go
// mobile/mobile.go
type Engine struct {
    mu      sync.Mutex
    state   string          // already tracked
    handler EventHandler
    cancel  context.CancelFunc
    tunnel  *core.ClientTunnel
    tm      *wire.TransportManager
    prov    provider.Provider
}
```

## Required Changes

### 1. Add fields to Engine struct

```go
type Engine struct {
    mu          sync.Mutex
    state       string
    wireUrl     string        // NEW: saved from Start(url, fd)
    connectedAt time.Time     // NEW: set when state becomes connected
    lastError   string        // NEW: set on error, cleared on connect
    handler     EventHandler
    // ... existing fields
}
```

### 2. Save wireUrl in Start()

```go
func (e *Engine) Start(url string, fd int) error {
    e.mu.Lock()
    if e.state != StateDisconnected {
        e.mu.Unlock()
        return fmt.Errorf("engine: already started")
    }
+   e.wireUrl = url
+   e.lastError = ""
    e.setState(StateConnecting)
    e.mu.Unlock()
    // ...
```

### 3. Set connectedAt in setState()

```go
func (e *Engine) setState(s string) {
+   if s == StateConnected {
+       e.connectedAt = time.Now()
+   }
    e.state = s
    if e.handler != nil {
        e.handler.OnStateChange(s)
    }
}
```

### 4. Save lastError in fail()

```go
func (e *Engine) fail(err error) error {
    e.mu.Lock()
    defer e.mu.Unlock()
+   e.lastError = err.Error()
    if e.handler != nil {
        e.handler.OnError(err.Error())
    }
    e.setState(StateDisconnected)
    return err
}
```

### 5. Clear fields in Stop()

```go
func (e *Engine) Stop() error {
    // ... existing teardown ...
+   e.wireUrl = ""
+   e.connectedAt = time.Time{}
    e.setState(StateDisconnected)
    return nil
}
```

### 6. Implement StatusJSON()

```go
// StatusJSON returns rich status as JSON. The format matches
// daemon.statusInfo() (daemon/daemon.go:268) so the webapp
// can use the same type mapping for desktop and mobile.
func (e *Engine) StatusJSON() string {
    e.mu.Lock()
    defer e.mu.Unlock()

    info := map[string]any{
        "state": e.state,
    }
    if e.state == StateConnected && !e.connectedAt.IsZero() {
        info["connected_at"] = e.connectedAt.Format(time.RFC3339)
        info["uptime_seconds"] = int(time.Since(e.connectedAt).Seconds())
    }
    if e.lastError != "" {
        info["error"] = e.lastError
    }
    if e.wireUrl != "" {
        info["wire_url"] = e.wireUrl
    }
    b, _ := json.Marshal(info)
    return string(b)
}
```

Note: needs `"encoding/json"` and `"time"` imports (time already imported).

## Tests to Add (mobile/mobile_test.go)

```go
func TestEngine_StatusJSON_Disconnected(t *testing.T) {
    e := NewEngine()
    raw := e.StatusJSON()
    var m map[string]any
    if err := json.Unmarshal([]byte(raw), &m); err != nil {
        t.Fatalf("invalid JSON: %v", err)
    }
    if m["state"] != StateDisconnected {
        t.Fatalf("state = %v, want %q", m["state"], StateDisconnected)
    }
    // No connected_at, uptime_seconds, wire_url when disconnected
    if _, ok := m["connected_at"]; ok {
        t.Fatal("connected_at should not be set when disconnected")
    }
}

func TestEngine_StatusJSON_AfterFailedStart(t *testing.T) {
    e := NewEngine()
    _ = e.Start("garbage", 0) // fails
    raw := e.StatusJSON()
    var m map[string]any
    json.Unmarshal([]byte(raw), &m)
    if m["state"] != StateDisconnected {
        t.Fatalf("state = %v, want disconnected", m["state"])
    }
    if m["error"] == nil || m["error"] == "" {
        t.Fatal("error should be set after failed start")
    }
}

func TestEngine_StatusJSON_Connected(t *testing.T) {
    e := NewEngine()
    // Simulate connected state (can't do real Start without TUN fd)
    e.mu.Lock()
    e.wireUrl = "k2v5://test@127.0.0.1:1234"
    e.connectedAt = time.Now().Add(-10 * time.Second)
    e.state = StateConnected
    e.mu.Unlock()

    raw := e.StatusJSON()
    var m map[string]any
    json.Unmarshal([]byte(raw), &m)

    if m["state"] != StateConnected {
        t.Fatalf("state = %v, want connected", m["state"])
    }
    if m["wire_url"] != "k2v5://test@127.0.0.1:1234" {
        t.Fatalf("wire_url = %v", m["wire_url"])
    }
    if m["connected_at"] == nil {
        t.Fatal("connected_at should be set")
    }
    uptime, ok := m["uptime_seconds"].(float64) // JSON numbers are float64
    if !ok || uptime < 9 {
        t.Fatalf("uptime_seconds = %v, want >= 9", m["uptime_seconds"])
    }
}

func TestEngine_StatusJSON_MatchesDaemonFormat(t *testing.T) {
    // Verify the JSON keys match daemon/daemon.go statusInfo():
    // state, connected_at, uptime_seconds, error, wire_url
    e := NewEngine()
    e.mu.Lock()
    e.state = StateConnected
    e.connectedAt = time.Now()
    e.wireUrl = "k2v5://x@y:1"
    e.lastError = "prev error"
    e.mu.Unlock()

    raw := e.StatusJSON()
    var m map[string]any
    json.Unmarshal([]byte(raw), &m)

    expectedKeys := []string{"state", "connected_at", "uptime_seconds", "wire_url", "error"}
    for _, k := range expectedKeys {
        if _, ok := m[k]; !ok {
            t.Errorf("missing key %q in StatusJSON output", k)
        }
    }
}
```

## Verification

```bash
cd mobile && go test -v -run TestEngine_StatusJSON
```

## Downstream Consumer

k2app mobile spec: `k2app/docs/features/mobile-rewrite/spec.md`

Called from:
- iOS: `PacketTunnelProvider.handleAppMessage("status")` → `engine.statusJSON()`
- Android: `K2Plugin.getStatus()` → `engine.statusJSON()`
- Both parse the JSON into webapp's `VpnStatus` type
