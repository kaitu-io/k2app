# Plan: Engine.StatusJSON() for Mobile

## Source

`docs/todos/p0-mobile-statusjson.md`

## Complexity: Simple

- 2 files modified: `mobile/mobile.go`, `mobile/mobile_test.go`
- No new packages, no refactoring, no cross-package changes
- Single task — all changes are in the same package and tightly coupled

## Task T1: Add StatusJSON() to mobile Engine

**Branch**: `feat/mobile-statusjson`
**Files**: `mobile/mobile.go`, `mobile/mobile_test.go`

### What

Add rich status reporting to the mobile Engine so mobile clients (iOS NE / Android VpnService) can get state + connectedAt + uptimeSeconds + wireUrl + error on cold start. The JSON format must match `daemon.statusInfo()` (`daemon/daemon.go:268`).

### Implementation Steps

#### 1. Add fields to Engine struct (`mobile/mobile.go:20`)

```go
type Engine struct {
    mu          sync.Mutex
    state       string
    wireUrl     string     // saved from Start(url, fd)
    connectedAt time.Time  // set when state becomes connected
    lastError   string     // set on error, cleared on connect
    handler     EventHandler
    cancel      context.CancelFunc
    tunnel      *core.ClientTunnel
    tm          *wire.TransportManager
    prov        provider.Provider
}
```

New imports needed: `"encoding/json"`, `"time"` (time already imported via existing code — verify).

#### 2. Save wireUrl + clear lastError in Start() (`mobile/mobile.go:56`)

Before `setState(StateConnecting)`, add:
```go
e.wireUrl = url
e.lastError = ""
```

#### 3. Set connectedAt in setState() (`mobile/mobile.go:160`)

```go
func (e *Engine) setState(s string) {
    if s == StateConnected {
        e.connectedAt = time.Now()
    }
    e.state = s
    // ... existing handler notification
}
```

#### 4. Save lastError in fail() (`mobile/mobile.go:168`)

Before the handler notification:
```go
e.lastError = err.Error()
```

#### 5. Clear fields in Stop() (`mobile/mobile.go:132`)

Before `setState(StateDisconnected)`:
```go
e.wireUrl = ""
e.connectedAt = time.Time{}
```

Note: do NOT clear `lastError` in Stop() — user may want to see the last error after stopping.

#### 6. Implement StatusJSON() method

```go
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

### TDD Steps

#### RED: Write tests first (`mobile/mobile_test.go`)

Add 4 test functions:

1. **TestEngine_StatusJSON_Disconnected** — fresh engine returns `{"state":"disconnected"}`, no extra keys
2. **TestEngine_StatusJSON_AfterFailedStart** — `Start("garbage", 0)` then check `state=disconnected` + `error` is set
3. **TestEngine_StatusJSON_Connected** — simulate connected state (set fields under lock), verify `state`, `wire_url`, `connected_at`, `uptime_seconds >= 9`
4. **TestEngine_StatusJSON_MatchesDaemonFormat** — verify all 5 expected keys present: `state`, `connected_at`, `uptime_seconds`, `wire_url`, `error`

New imports in test file: `"encoding/json"` (time already imported).

Run: `cd mobile && go test -v -run TestEngine_StatusJSON` — all 4 must FAIL (method doesn't exist).

#### GREEN: Implement all 6 steps above

Run: `cd mobile && go test -v -run TestEngine_StatusJSON` — all 4 must PASS.

#### REFACTOR: None expected

The implementation is minimal. Verify no regressions: `cd mobile && go test -v ./...`

### AC Coverage

| AC | Test | Verified |
|----|------|----------|
| StatusJSON returns valid JSON with state | TestEngine_StatusJSON_Disconnected | state field present, valid JSON |
| connected_at + uptime_seconds present when connected | TestEngine_StatusJSON_Connected | both fields checked |
| error field set after failed start | TestEngine_StatusJSON_AfterFailedStart | error non-empty |
| wire_url preserved from Start() | TestEngine_StatusJSON_Connected | wire_url matches input |
| JSON keys match daemon.statusInfo() format | TestEngine_StatusJSON_MatchesDaemonFormat | all 5 keys verified |
| No extra fields when disconnected | TestEngine_StatusJSON_Disconnected | connected_at/uptime absent |
| Existing tests still pass | `go test ./...` | full regression |

### Verification

```bash
cd mobile && go test -v -run TestEngine_StatusJSON  # new tests
cd mobile && go test -v ./...                        # regression
```
