# Plan: Cloud Webapp Integration

## Meta

| Field | Value |
|-------|-------|
| Feature | cloud-webapp |
| Spec | docs/features/cloud-webapp/spec.md |
| Date | 2026-02-14 |
| Complexity | moderate |

## AC Mapping

| AC | Test | Task |
|----|------|------|
| AC1: UDID generation (stable, per-platform) | TestUDID_Format, TestUDID_Stable, TestUDID_Platform | T1 |
| AC2: UDID API endpoint | TestHandleDeviceUDID | T2 |
| AC3: Webapp embedding (go generate + embed) | TestWebappFS, TestWebappFS_DefaultPlaceholder | T3 |
| AC4: Webapp serving (SPA fallback) | TestWebappServing, TestWebappServing_APIpriority | T2 |
| AC5: CLI commands (open, upgrade) | TestCmdOpen, TestCmdUpgrade | T4 |
| AC6: Kaitu CA always-fallback | TestBuildClientTLSConfig_PinWithKaituCA, TestBuildClientTLSConfig_InsecureWithKaituCA | T5 |
| AC7: Binary self-update | TestVersionCheck, TestUpgrade_Download | T4 |

## Dependency Graph

```
T1 (UDID) ──┐
             ├──> T2 (Daemon integration) ──> T4 (CLI commands)
T3 (Embed) ──┘
T5 (Kaitu CA) ── independent
```

## Foundation Tasks

### T1: UDID Generation — `cloud/` package

**Scope**: Create the `cloud` package with cross-platform UDID generation. Pure library code, no daemon integration.
**Files**:
- cloud/udid.go (new)
- cloud/udid_darwin.go (new)
- cloud/udid_linux.go (new)
- cloud/udid_windows.go (new)
- cloud/udid_test.go (new)
**Depends on**: none
**TDD**:
- RED: Write `TestUDID_Format` — UDID() returns 64-char hex string. Write `TestUDID_Stable` — two calls return same value. Write `TestUDID_NotEmpty` — UDID() != "".
- GREEN: Implement `UDID() string` in udid.go with SHA-256 hash. Implement platform-specific `platformID() string` in each `udid_{os}.go`. macOS: exec `ioreg -rd1 -c IOPlatformExpertDevice`, parse IOPlatformUUID. Linux: read `/etc/machine-id`. Windows: read registry `HKLM\SOFTWARE\Microsoft\Cryptography\MachineGuid`.
- REFACTOR: Extract hash logic to internal function if needed. Ensure error handling returns deterministic fallback (e.g., hostname-based) if platform ID unavailable.
**Acceptance**: `go test ./cloud/...` passes on current platform. UDID is stable across runs. Format is 64-char hex.

### T3: Webapp Embedding — `cloud/` embed + go generate

**Scope**: Add webapp embedding infrastructure: go:embed directive, go generate download script, default placeholder page.
**Files**:
- cloud/embed.go (new)
- cloud/gen.go (new, +build ignore)
- cloud/version.go (new)
- cloud/dist/index.html (new, default placeholder)
- cloud/embed_test.go (new)
- .gitignore (modify — add cloud/dist/ exclusion rules)
**Depends on**: none
**TDD**:
- RED: Write `TestWebappFS` — `cloud.FS()` returns fs.FS, can open "index.html". Write `TestWebappFS_DefaultPlaceholder` — index.html contains expected placeholder text.
- GREEN: Create `cloud/embed.go` with `//go:embed dist/*` and `FS()` function. Create `cloud/dist/index.html` with placeholder "Configure via CLI: k2 up k2v5://...". Create `cloud/gen.go` that downloads zip from `WebappDistURL`, extracts to `cloud/dist/`. Create `cloud/version.go` with `const WebappDistURL`.
- REFACTOR: Ensure gen.go handles errors cleanly (download failure, extract failure, permission). Add progress output during download.
**Acceptance**: `go build ./...` succeeds with default placeholder. `go generate ./cloud/...` (when URL set) downloads and extracts webapp. `TestWebappFS` passes.

## Feature Tasks

### T2: Daemon Integration — UDID API + webapp serving

**Scope**: Wire the cloud package into the daemon: add UDID endpoint, serve embedded webapp as SPA.
**Files**:
- daemon/daemon.go (modify — add routes)
- daemon/api.go (modify — add handleDeviceUDID, add webappHandler)
- daemon/api_test.go (modify — add tests)
**Depends on**: [T1, T3]
**TDD**:
- RED: Write `TestHandleDeviceUDID` — GET /api/device/udid returns Response with 64-char hex udid in data. Write `TestWebappServing` — GET / returns index.html content. Write `TestWebappServing_APIpriority` — GET /api/core still works (not intercepted by SPA handler). Write `TestWebappServing_SPAFallback` — GET /dashboard returns index.html (SPA routing).
- GREEN: Add `handleDeviceUDID` to api.go using `cloud.UDID()`. Add `webappHandler()` function that serves `cloud.FS()` with SPA fallback (non-file paths → index.html). Register routes in daemon.go `Run()`: `/api/device/udid` and `/` (webapp fallback, lowest priority).
- REFACTOR: Ensure webapp handler doesn't conflict with existing routes. Verify CORS middleware covers new endpoints.
**Acceptance**: All daemon tests pass. `curl localhost:1777/api/device/udid` returns valid UDID. `curl localhost:1777/` returns index.html. Existing /api/core tests unchanged.
**Knowledge**: docs/knowledge/architecture-decisions.md — Provider Callback Model (daemon patterns)

### T4: CLI Commands — open + upgrade + version check

**Scope**: Add `k2 open` and `k2 upgrade` subcommands. Add async version check on daemon startup.
**Files**:
- cmd/k2/cmd_open.go (new)
- cmd/k2/cmd_upgrade.go (new)
- cmd/k2/main.go (modify — register subcommands)
- cmd/k2/cmd_open_test.go (new)
- cmd/k2/cmd_upgrade_test.go (new)
**Depends on**: [T2]
**TDD**:
- RED: Write `TestCmdOpen` — open command constructs correct URL. Write `TestVersionCheck` — given mock latest.json with newer version, prints update message. Write `TestVersionCheck_NoUpdate` — same version, no message. Write `TestUpgrade_Download` — given mock server, downloads and verifies binary.
- GREEN: Implement `cmd_open.go`: detect OS, exec `open`/`xdg-open`/`start` with `http://localhost:1777`. Implement `cmd_upgrade.go`: fetch `latest.json` from releases URL, compare semver, download platform binary (`k2-{os}-{arch}`), atomic rename to replace self. Add version check in daemon startup (goroutine, non-blocking, 5s timeout).
- REFACTOR: Extract version comparison to helper. Ensure upgrade handles permission errors (need sudo on some installs). Add `--check` flag for dry-run.
**Acceptance**: `k2 open` launches browser. `k2 upgrade` downloads and replaces binary (mock test). Version check prints message on startup when update available.

### T5: Kaitu CA Always-Fallback

**Scope**: Modify `BuildClientTLSConfig` so Kaitu CA is always in the verification chain, even for pin and insecure modes.
**Files**:
- wire/ech.go (modify — BuildClientTLSConfig)
- wire/ech_test.go (modify — add tests)
**Depends on**: none (independent, can parallel with T1-T4)
**TDD**:
- RED: Write `TestBuildClientTLSConfig_PinWithKaituCA` — config with pin= set still has VerifyPeerCertificate that accepts Kaitu CA signed certs. Write `TestBuildClientTLSConfig_InsecureWithKaituCA` — config with insecure=1 still accepts Kaitu CA certs (trivially true since insecure skips all, but verify the behavior is intentional).
- GREEN: Modify `BuildClientTLSConfig` pin branch: keep InsecureSkipVerify=true but add Kaitu CA verification as fallback within the pin verifier — if pin matches OR cert chains to Kaitu CA, accept. For insecure branch: no change needed (already accepts everything). Add comment documenting the intent.
- REFACTOR: Extract Kaitu CA cert parsing to package-level `var kaituCACert *x509.Certificate` (parse once at init). Ensure pin verifier logic is clear: pin match → accept, Kaitu CA chain → accept, else → reject.
**Acceptance**: All existing wire tests pass. New tests verify Kaitu CA certs accepted even with pin configured. Pin verification still rejects non-matching, non-Kaitu certs.
**Knowledge**: docs/knowledge/architecture-decisions.md — Chinese CA Blocklist via VerifyPeerCertificate

## Execution Order

1. **Parallel**: T1 (UDID), T3 (Embed), T5 (Kaitu CA) — no dependencies, no file overlap
2. **Sequential after T1+T3**: T2 (Daemon integration)
3. **Sequential after T2**: T4 (CLI commands)

```
Phase 1 (parallel):  T1  T3  T5
Phase 2:             T2
Phase 3:             T4
```

Total: 3 phases, 5 tasks.
