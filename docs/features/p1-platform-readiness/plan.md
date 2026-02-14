# Plan: P1 Platform Readiness

## Dependency Graph

```
F1 (cleanup) ──────┐
T1 (wintun-embed) ─┼──→ T4 (integration + Makefile)
T2 (win daemon) ───┤
T3 (mobile build) ─┘
```

F1, T1, T2, T3 are parallel (no file overlap). T4 depends on all.

## Tasks

### F1: Cleanup Obsolete TODOs and Reconnect API
- **AC**: AC7, AC8, AC9
- **depends_on**: []
- **files**:
  - MODIFY `daemon/api.go` — remove `case "reconnect"` + `handleReconnect` function
  - MODIFY `daemon/api_test.go` — remove `TestHandleCoreReconnectNoConfig`
  - MODIFY `daemon/daemon.go` — remove `StateReconnecting` constant (verify unused after reconnect removal)
  - DELETE `docs/todos/p1-mobile-api-server.md`
- **TDD**:
  - RED: Verify `TestHandleCoreReconnectNoConfig` exists and passes
  - GREEN: Remove reconnect handler + test + StateReconnecting. Verify `tryAutoReconnect` untouched
  - REFACTOR: Verify `go test ./daemon/...` passes, no dead references to StateReconnecting

### T1: wintun-embed Package
- **AC**: AC3
- **depends_on**: []
- **files**:
  - NEW `wintun/gen.go` — `//go:build ignore`, downloads wintun-0.14.1.zip, verifies SHA-256, extracts amd64 DLL
  - NEW `wintun/embed_windows.go` — `//go:build windows`, `//go:embed wintun.dll`, `EnsureExtracted()` extracts next to exe
  - NEW `wintun/embed_other.go` — `//go:build !windows`, no-op `EnsureExtracted() (string, error)`
  - NEW `wintun/.gitignore` — ignore `wintun.dll`
  - MODIFY `.gitignore` — add `wintun/wintun.dll` if not covered by wintun/.gitignore
- **TDD**:
  - RED: `go build ./wintun/` fails (package doesn't exist)
  - GREEN: Create package. `go build ./wintun/` succeeds on all platforms. On non-windows, `EnsureExtracted()` returns `("", nil)`
  - REFACTOR: Verify embed_other.go compiles on darwin/linux. Verify gen.go has SHA-256 check
- **details**:
  - Follow design exactly from `docs/todos/p1-wintun-embed.md`
  - wintun URL: `https://www.wintun.net/builds/wintun-0.14.1.zip`
  - SHA-256: `07c256185d6ee3652e09fa55c0b673e2624b565e02c4b9091c79ca7d2f24ef51`
  - Extract: `wintun/bin/amd64/wintun.dll` → `./wintun.dll`
  - EnsureExtracted: sync.Once, skip if exists and same size, write to exe directory

### T2: Windows Daemon Stubs
- **AC**: AC1, AC4
- **depends_on**: []
- **files**:
  - NEW `daemon/service_windows.go` — `//go:build windows`, `installService()`, `uninstallService()`, `isServiceInstalled()` using `sc` commands (mirror cmd/k2s/service_windows.go pattern)
  - NEW `daemon/process_windows.go` — `//go:build windows`, `processExists(pid int) bool` using `os.FindProcess` + `Process.Signal(0)`
  - NEW `daemon/rlimit_windows.go` — `//go:build windows`, no-op `setMaxProcs()` + `setMaxFiles()`
  - NEW `daemon/recovery_windows.go` — `//go:build windows`, no-op `restoreNetworkPlatform()`
- **TDD**:
  - RED: `GOOS=windows go build ./daemon/` fails (missing processExists, setMaxProcs, etc.)
  - GREEN: Create stubs. `GOOS=windows go build ./daemon/` succeeds
  - REFACTOR: Verify function signatures match unix counterparts exactly
- **details**:
  - service_windows.go: service name `k2`, use `sc create/start/query/delete` pattern from cmd/k2s
  - process_windows.go: Windows has no `syscall.Kill(pid, 0)`. Use `os.FindProcess(pid)` then attempt signal — on Windows FindProcess always succeeds, so use `tasklist /FI "PID eq N"` or `windows.OpenProcess` with PROCESS_QUERY_LIMITED_INFORMATION
  - rlimit_windows.go: Windows doesn't have RLIMIT. Return nil (no-op)
  - recovery_windows.go: Log message like unix version. No manual cleanup needed

### T3: Mobile Gomobile Build Fix
- **AC**: AC5, AC6
- **depends_on**: []
- **files**:
  - MODIFY `go.mod` — add `golang.org/x/mobile` (required by gobind)
  - MODIFY `go.sum` — auto-updated
- **TDD**:
  - RED: `go list -m golang.org/x/mobile` fails (not in go.mod)
  - GREEN: `go get golang.org/x/mobile@latest`. Verify `gomobile bind -target=ios ./mobile/` compiles (or at minimum `go list` succeeds for mobile package with x/mobile present)
  - REFACTOR: Run `go mod tidy` to clean up

### T4: Makefile + Integration + Platform TODO Update
- **AC**: AC1, AC2, AC10, AC11
- **depends_on**: [F1, T1, T2, T3]
- **files**:
  - MODIFY `Makefile` — add `generate`, `build-windows`, `build-linux`, `build-nowebapp` targets
  - MODIFY `cmd/k2/main.go` or `daemon/daemon.go` — add `wintun.EnsureExtracted()` call before TUN operations (guarded by import, only active on Windows via build tag)
  - MODIFY `docs/todos/p1-platform.md` — check off completed items, update status
  - DELETE `docs/todos/p1-wintun-embed.md` — completed
- **TDD**:
  - RED: `make build-windows` fails (target doesn't exist)
  - GREEN: Add Makefile targets. Wire wintun extraction. Cross-compile succeeds
  - REFACTOR: `go test ./...` passes. `GOOS=windows go build ./cmd/k2` succeeds. `GOOS=windows go build ./cmd/k2s` succeeds
- **details**:
  Makefile additions:
  ```makefile
  generate:
  	go generate ./wintun/
  	go generate ./cloud/

  build-windows:
  	CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go build -o build/k2.exe ./cmd/k2
  	CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go build -o build/k2s.exe ./cmd/k2s

  build-linux:
  	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o build/k2-linux ./cmd/k2
  	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o build/k2s-linux ./cmd/k2s

  build-nowebapp:
  	go build -tags nowebapp -o build/k2-nowebapp ./cmd/k2
  ```

  wintun integration in daemon startup (or cmd/k2/main.go):
  ```go
  import "github.com/kaitu-io/k2/wintun"
  // In init or before TUN operations:
  if _, err := wintun.EnsureExtracted(); err != nil {
      log.Printf("wintun: %v", err)
  }
  ```
  On non-Windows this is a no-op (embed_other.go).

## AC Coverage

| AC | Test | Task |
|----|------|------|
| AC1 | `GOOS=windows go build ./cmd/k2` | T2, T4 |
| AC2 | `GOOS=windows go build ./cmd/k2s` | T4 (regression) |
| AC3 | `go build ./wintun/` on all platforms | T1 |
| AC4 | `GOOS=windows go build ./daemon/` | T2 |
| AC5 | gomobile bind ios | T3 |
| AC6 | gomobile bind android | T3 |
| AC7 | `go test ./daemon/` — no reconnect handler | F1 |
| AC8 | `TestAutoReconnectSkipsStale` still passes | F1 |
| AC9 | `p1-mobile-api-server.md` deleted | F1 |
| AC10 | `make build-windows` succeeds | T4 |
| AC11 | `go test ./...` passes | T4 |
