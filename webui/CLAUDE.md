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
  `NewWebappHandlerWithOverride(version, commit, arch, overrideDir)` is
  the same handler plus an optional disk override directory (a web OTA
  `current/` bundle), consulted per request so an atomic directory swap
  takes effect on the next page load without reconstructing the handler.
  `?ui=embedded` forces the embedded index for that request — the escape
  hatch when an OTA bundle is broken. `overrideDir == ""` disables the
  override (`NewWebappHandler` is this with `overrideDir=""`).
  `NewWebappHandlerWithOTA(..., onIndexServed func())` adds a hook fired
  only when an **OTA bundle's document** is served — not the embedded
  fallback, not `?ui=embedded`, never assets. That is the Linux notion of
  "the bundle was exercised", feeding boot verification below.
- `webota_boot.go` / `webota_boot_http.go` — Web OTA boot verification.
  `apply()` arms a `.boot-pending` marker (JSON: version + first-serve time
  + serve count) directly under `cfg.Dir` — **never inside `current/`**,
  which rollback deletes along with the version it needs. The webapp clears
  it by POSTing `/api/ui-boot-ok` (`NewUIBootOkHandler`) **after ReactDOM
  has rendered**; confirming during bridge/platform init is what silently
  disabled this whole mechanism on the mobile shells for months (a bundle
  that loaded its JS then died before painting still reported success).
  `NoteIndexServed` accumulates serves and, once `shouldRollbackBoot` is
  satisfied, restores `previous/` over `current/` and records the failed
  version in `quarantined-version.txt`, which `shouldApply` then refuses —
  mirroring desktop `evaluate_manifest` and mobile `planAutoUpdate`.
  **The verdict deliberately differs from the app shells**: they judge at
  process start, but a daemon runs for weeks and only sees the bundle when
  a browser loads it. So the rule is two-part — served at least
  `bootRollbackMinServes` (2) times AND at least `bootRollbackMinAgeSec`
  (20s) past the first serve. The count alone would roll back a good bundle
  when several tabs open at once; the age alone would roll back one nobody
  has visited. Linux also keeps `?ui=embedded`, so this errs toward not
  firing. Emits `DIAG: webota-boot-ok` / `webota-boot-fail` /
  `webota-rolled-back`. Backward-compatible by construction: only `apply()`
  writes the marker, so a `current/` left by an older daemon is never
  judged, and any bundle this daemon installs is newer than its own release
  and therefore carries the webapp that confirms.
- `webota.go` — Web OTA poller (`WebOTA`, `NewWebOTA(WebOTAConfig)`,
  `(*WebOTA).Start(ctx)`, `(*WebOTA).CurrentDir()`): polls each brand
  CDN endpoint for `<endpoint>/web/latest.json`, gates on three checks
  in order (`min_linux` vs `cfg.ShellVersion`, `min_bridge` vs
  `LinuxBridgeVersion`, `isNewerVersion` vs the local `current/version.txt`),
  then downloads `web.zip`, verifies sha256 + minisign, unpacks into
  `pending/`, and atomically renames `pending/` → `current/` (old
  `current/` rotates to `previous/`). `LinuxBridgeVersion` (this file)
  is this shell's mirror of the outer repo's `BRIDGE_API_VERSION`
  (`webapp/src/types/bridge-version.ts`) — one integer versioning the whole
  webapp↔shell surface, also mirrored by Android, iOS and Tauri desktop.
  It tracks that value even when a bump's additions belong to another shell
  (v2 = five Tauri commands a Go daemon can never have): the number means
  "current with the surface", and holding it back does not describe the gap
  honestly — it only excludes Linux from every bundle once the published
  support floor reaches that version. **The outer repo's contract gate does
  not check this mirror** (k2 is a submodule a fresh worktree may not have
  checked out), so nothing catches drift: a `BRIDGE_API_VERSION` bump must
  carry a k2 commit + submodule pointer bump in the same change set.
  The minisign public key is the same
  key pair as the Tauri desktop updater (`desktop/src-tauri/tauri.conf.json`
  `plugins.updater.pubkey`, key ID `40D7545FD7CEDE83`) — one signing key
  covers both the desktop binary OTA and the web OTA. Emits `DIAG:
  webota-applied` (INFO, `from`/`to`) on a successful swap and `DIAG:
  webota-fail` (WARN, `err`) on a failed poll cycle; see root `CLAUDE.md`
  DIAG event table.
- `webota_version.go` — Version-comparison pure functions
  (`isNewerVersion`, `meetsMinVersion`, `meetsMinBridge`, `splitVersion`,
  `compareSegments`) backing the three `shouldApply` gates above.
  Semantics are deliberately aligned with the mobile native gate
  (`K2PluginUtils.isNewerVersion` / `isCompatibleNativeVersion`) so a
  manifest's `min_linux`/pre-release comparisons behave the same way
  across platforms.
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

Tests that exercise `NewWebappHandler` call `fakeEmbed(t)` (in
`serve_test.go`) to swap `webappFS` for an in-memory FS, so they need no
populated `dist/` and run identically on a clean checkout. **Do not guard
such a test with `if webappFS == nil { t.Skip() }`** — `dist/` is committed
as a lone `.gitkeep`, so the embed succeeds and `webappFS` is non-nil even
with no webapp built; the handler then returns nil for a *different* reason
(no `index.html`) and the test fails instead of skipping. That guard only
catches `-tags nowebapp`. Two tests were written that way and passed for
months solely on machines that had built the webapp.

Tests for SPA routing, injection of `window.__K2_GATEWAY__`, cache headers,
and 404 fallback live in `serve_test.go`. Storage encryption and HTTP
handler round-trips live in `storage_test.go` and `storage_http_test.go`.

This package is `//go:build linux`: on macOS `go test ./...` excludes it
outright — no SKIP line, no output at all. In CI it runs only in the
`test-gateway-uat` job's explicit linux-only package list (outer repo
`.github/workflows/ci.yml`). To run it from macOS:

```bash
docker run --rm --platform linux/amd64 -v "$PWD":/src \
  -v "$(go env GOMODCACHE)":/go/pkg/mod -w /src golang:1.25 \
  go test ./webui/ -count=1
```
