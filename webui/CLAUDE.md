# Webui Layer (webui/)

Shared webapp-embedding + storage + platform + OTA upgrade package, consumed
by both `cmd/k2r` (via the `gateway` package) and `cmd/k2` Linux desktop
(via the `daemon` package). Linux only (`//go:build linux`).

## Why this package exists

k2r was the first Kaitu Linux binary to embed the React webapp and serve it
over HTTP on `127.0.0.1:1777` (see `docs/plans/`). When `cmd/k2` Linux pivoted
off Tauri and onto the same "Go binary + embedded webapp" model, these
pieces were extracted out of `gateway/` so both binaries could share one
implementation instead of two copies drifting.

- Each binary provides its own `*Upgrader` config (CDN URL, state dir,
  restart hook) and its own `*Storage` path.
- The webapp itself is ~2 MB of static files embedded at build time.
- The encryption key is derived from `/etc/machine-id` via HKDF-SHA256,
  matching the `desktop/src-tauri` scheme, so tokens persist across
  binary upgrades without re-login.

## Files

- `embed.go` / `embed_nop.go` — `//go:embed all:dist` for the webapp
  bundle; nop variant is used with `-tags nowebapp` for headless builds.
  **k2r production builds always use `-tags nowebapp` (headless since
  2026-07)** — `NewWebappHandler` returns nil and `gateway.go` falls back
  to a static notice page (`gateway/notice.go`) on `/`. The embed remains
  live for `cmd/k2` Linux desktop, which still ships the webapp bundle.
- `serve.go` — `NewWebappHandler(version, commit, arch)`: injects
  `window.__K2_GATEWAY__`, serves SPA with cache-control split between
  HTML (no-cache) and fingerprinted assets (1 year immutable).
- `storage.go` — `*Storage` (AES-256-GCM per-hardware-ID) + `NewStorage`
  / `NewStorageWithKey` (tests use the With-key constructor).
- `storage_http.go` — `NewStorageHandler(*Storage)` returns the `/api/storage`
  handler; supports `get`, `set`, `remove`, `has`, `keys`, `clear`.
- `platform.go` — `PlatformInfo` struct + `NewPlatformHandler`. Callers
  set `PlatformType` ("gateway" or "desktop") and the version trio.
- `upgrade.go` — `UpgradeConfig`, `*Upgrader`, `FetchLatest`, `BinaryURL`,
  `ReplaceBinary`, `Restart`, `DownloadAndReplace`, `IsOpenWRT`. The
  `Upgrader` is parameterized entirely via `UpgradeConfig` — no k2r-
  specific constants in this file.
- `updater.go` — `*Upgrader.Apply` with `/etc/<cfg.StateDir>/update-state.json`
  persistence for crash recovery (4 stages: downloading, replacing,
  restarting, + rollback).
- `upgrade_http.go` — `NewUpgradeHandler(*Upgrader)` returns the
  `/api/upgrade` handler with `check` / `apply` actions.
- `response.go` — shared JSON envelope + `writeJSON` helper (unexported,
  package-local so the envelope stays in sync across handlers).
- `dist/.gitkeep` — placeholder; the real webapp dist is copied in from
  `webapp/` by the outer-repo Makefile before `go build`.

## Usage pattern

```go
// In gateway.New():
storage := webui.NewStorage(filepath.Join(stateDir(), "storage.json"))
upgrader := webui.NewUpgrader(version, webui.UpgradeConfig{
    BinaryName:       "k2r",
    StateDir:         "/etc/k2r",
    ReleaseEndpoints: []string{"https://.../kaitu/k2r", ...},
    RestartFunc: func() {
        if webui.IsOpenWRT() {
            exec.Command("/etc/init.d/k2r", "restart").Run()
        } else {
            exec.Command("systemctl", "restart", "k2r").Run()
        }
    },
})

// In gateway.Run():
upgrader.RecoverOnStartup()
// storage is NOT exposed as /api/storage in the gateway package (headless
// k2r retired that HTTP handler) — it is read internally, e.g. by
// RouterDeviceManager for allowlist/mode persistence. cmd/k2 Linux desktop
// (daemon package) still registers webui.NewStorageHandler(storage) on
// /api/storage for the embedded webapp.
mux.HandleFunc("/api/platform", webui.NewPlatformHandler(webui.PlatformInfo{
    PlatformType: "gateway",
    Version: version, Commit: commit, Arch: arch,
}))
mux.HandleFunc("/api/upgrade", webui.NewUpgradeHandler(upgrader))
if webapp := webui.NewWebappHandler(version, commit, arch); webapp != nil {
    mux.Handle("/", webapp)
} else {
    mux.HandleFunc("/", handleRootNotice) // k2r nowebapp builds: static notice page
}
```

## Testing

```bash
GOOS=linux go vet ./webui/...                 # Static analysis (any OS)
GOOS=linux go build ./webui/...               # Cross-compile check
go test ./webui/...                           # Unit tests (Linux only)
```

Tests that exercise `NewWebappHandler` need a populated `dist/` directory
(copy from `webapp/dist/` before running). Tests for SPA routing, injection
of `window.__K2_GATEWAY__`, cache headers, and 404 fallback live in
`serve_test.go`. Storage encryption and HTTP handler round-trips live in
`storage_test.go` and `storage_http_test.go`.
