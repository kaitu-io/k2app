# Spec: P1 Platform Readiness

Date: 2026-02-14
Status: Draft

## Goal

Make k2 usable on all three target platforms (Windows, iOS, Android) and clean up
obsolete P1 TODOs based on brainstorm decisions.

## Brainstorm Decisions

### Mobile API Server — NOT NEEDED (delete TODO)

Court-mode debate concluded:
- Current `mobile.Engine` API (Start/Stop/Status + EventHandler) is sufficient
- iOS Packet Tunnel Provider runs in separate sandboxed extension process — HTTP 127.0.0.1 impossible
- Android VpnService similar constraints
- Rich status, config persistence, reconnect, UDID — all native bridge layer concerns (k2app, not k2)
- UDID already embedded in k2v5:// URL

### Desktop Reconnect API — REMOVE

- `action:reconnect` is just `doDown()` + `doUp(savedConfig)` — webapp can do `status` + `up`
- Wire layer already handles per-connection recovery (TransportManager failover)
- `tryAutoReconnect` on daemon restart STAYS (internal logic, not API)
- Wire session health detection + auto-rebuild is P2 (not in scope)

### wintun-embed — IMPLEMENT per existing TODO design

- `go:generate` downloads wintun.dll, `go:embed` embeds it, runtime extracts next to exe
- sing-tun's `windows.NewLazyDLL("wintun.dll")` finds it via DLL search order

### Platform Support — ALL THREE

- Windows: daemon stubs (service, process, rlimit, recovery) + wintun embed
- iOS: fix gomobile build (`golang.org/x/mobile` missing from go.mod)
- Android: same gomobile fix

## Acceptance Criteria

- **AC1**: `GOOS=windows GOARCH=amd64 go build ./cmd/k2` succeeds
- **AC2**: `GOOS=windows GOARCH=amd64 go build ./cmd/k2s` succeeds (already works, regression guard)
- **AC3**: `wintun/` package exists with gen.go + embed_windows.go + embed_other.go
- **AC4**: `daemon/` has Windows stubs: service_windows.go, process_windows.go, rlimit_windows.go, recovery_windows.go
- **AC5**: `gomobile bind -target=ios ./mobile/` succeeds (golang.org/x/mobile in go.mod)
- **AC6**: `gomobile bind -target=android ./mobile/` succeeds
- **AC7**: `action:reconnect` removed from daemon HTTP API
- **AC8**: `tryAutoReconnect` still works (daemon startup recovery preserved)
- **AC9**: `p1-mobile-api-server.md` deleted
- **AC10**: Makefile has `build-windows` target
- **AC11**: `go test ./...` passes (no regression)

## Out of Scope

- Wire session health detection + auto-rebuild (P2)
- Mobile native bridge / Capacitor plugin (k2app concern)
- IPv6 reachability detection (P2)
- Speedtest / performance tuning (P2)
