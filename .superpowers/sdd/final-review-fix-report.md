# Final-Review Fix Report — antiblock Phase 1

Date: 2026-06-25

## C1 — Open-Proxy Gate Wiring

### Problem
`validateControlPlaneRoutes` existed in `server/control_plane.go` and was unit-tested, but was never called from `New()`. A poisoned config (`control_plane_routes` pointing to a private/loopback/link-local IP) would be accepted silently at startup, turning the node into an open proxy to internal services.

### Fix
Added as the very first statements of `New()` in `server/server.go` (before `store := wire.NewTicketStore()`):

```go
if err := validateControlPlaneRoutes(cfg.ControlPlaneRoutes); err != nil {
    return nil, fmt.Errorf("control_plane_routes: %w", err)
}
```

`fmt` was already imported; `validateControlPlaneRoutes` is in the same package — no new imports needed.

### TDD: RED → GREEN

**RED** (before wiring — test added, `New` returns nil error for bad config):
```
=== RUN   TestNew_RejectsUnsafeControlPlaneRoute
    control_plane_test.go:196: New must reject a private/link-local control-plane origin (open-proxy gate)
--- FAIL: TestNew_RejectsUnsafeControlPlaneRoute (0.00s)
FAIL    github.com/kaitu-io/k2/server   0.016s
```

**GREEN** (after wiring):
```
=== RUN   TestNew_RejectsUnsafeControlPlaneRoute
--- PASS: TestNew_RejectsUnsafeControlPlaneRoute (0.00s)
PASS
ok      github.com/kaitu-io/k2/server   0.016s
```

### Integration Test Added (`server/control_plane_test.go`)

```go
func TestNew_RejectsUnsafeControlPlaneRoute(t *testing.T) {
    _, err := New(&config.ServerConfig{
        ControlPlaneRoutes: map[string]string{"k2.52j.me": "169.254.169.254:80"},
    })
    if err == nil {
        t.Fatal("New must reject a private/link-local control-plane origin (open-proxy gate)")
    }
}
```

Merged into the existing single import block (no new imports needed — `config` was already imported).

## Full Suite — No Regression

```
$ go test ./server/... ./config/...
ok      github.com/kaitu-io/k2/server   3.667s
ok      github.com/kaitu-io/k2/config   (cached)
```

All existing tests (including `New()` with nil ControlPlaneRoutes) pass — validating an empty/nil map returns nil and does not break the positive path.

## Build and Vet

```
$ go build ./...
BUILD OK
$ go vet ./server/
VET OK
```

## I1 — Documentation Bullet

Added to `server/CLAUDE.md` in the "Control-Plane Relay (antiblock Phase 1)" section:

> - Short request/response only: `reverseProxyHTTP` sets `ReadTimeout 30s` /
>   `WriteTimeout 60s`, so streaming, long-poll, and SSE endpoints are out of
>   scope and will be cut off — relay is intended for Center API calls only.

## Files Changed

- `server/server.go` — added 3-line validation block at top of `New()`
- `server/control_plane_test.go` — added `TestNew_RejectsUnsafeControlPlaneRoute`
- `server/CLAUDE.md` — added doc bullet on streaming/timeout limitation
