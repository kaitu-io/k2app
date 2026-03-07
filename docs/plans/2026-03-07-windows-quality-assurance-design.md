# Windows Quality Assurance — Layered Testing Strategy

**Date**: 2026-03-07
**Status**: Approved
**Problem**: macOS-first development with zero Windows CI coverage leads to repeated Windows-specific bugs discovered only at release time.

## Problem Analysis

### Current State

| Dimension | Status | Issue |
|-----------|--------|-------|
| CI | `ubuntu-latest` only | Zero Windows code path coverage |
| Go | 9 `_windows.go` files | 0 `_windows_test.go` files |
| Rust | 5 files with `#[cfg(windows)]` | 15 tests all platform-independent |
| NSIS | 370-line installer hooks | Zero tests, manual-only validation |
| Manual | `test-windows-service.md` | 6 scenarios, no automation |

### Historical Windows Bugs (from commit history)

| Category | Commits | Root Cause |
|----------|---------|------------|
| **Service Lifecycle** | 31ace38 (Error 1053), 4982946 (file locks) | No automated testing of SCM integration |
| **Platform API** | 9744ea2 (WebView2 CORS), a9a00e1 (wmic ARM64) | Platform API differences invisible until runtime |
| **UI Behavior** | 0990941 (tray click), a874d8e (menu flash) | Tauri cross-platform API behavioral differences |
| **Build/Packaging** | 53c1f22 (signing), ebe1b82 (config), b40e0bf (line endings) | Build pipeline only runs at release time |
| **File System** | log truncate vs delete, DNS crash recovery | Windows file lock semantics differ from Unix |

## Design: 5-Layer Defense

### L1: CI Gate (compile + existing tests on Windows)

**Goal**: Catch compilation errors and platform-conditional code breakage on every PR.

**Changes to `.github/workflows/ci.yml`**:

```yaml
test-windows:
  runs-on: [self-hosted, Windows]
  steps:
    # Setup (reuse release-desktop.yml patterns)
    - Node.js via nvm (self-hosted)
    - Go 1.24 via actions/setup-go
    - Rust stable via dtolnay/rust-toolchain (target: x86_64-pc-windows-msvc)

    # Tests
    - yarn install --frozen-lockfile
    - cargo check (desktop/src-tauri)           # Rust compile check
    - cargo test (desktop/src-tauri)             # Rust unit tests
    - go test ./daemon/... ./cmd/k2/... ./provider/... ./config/... ./sniff/... ./core/... ./engine/...
    - yarn test (webapp)                         # vitest on Windows

    # Build verification
    - make build-k2-windows                     # Go binary + wintun embed
    - Verify: k2-x86_64-pc-windows-msvc.exe exists
```

**What it catches**:
- `CREATE_NO_WINDOW` constant and `CommandExt` import
- WMI crate (`wmi`) compilation with COM APIs
- All `#[cfg(target_os = "windows")]` Rust blocks
- All `//go:build windows` Go files
- Go `_windows.go` build tag and dependency compilation
- wintun `gen.go` embed step

**Admin not required**: `cargo test` and `go test` don't need elevation (SCM operations skip or mock in tests).

### L2: Platform Unit Tests (prevent Windows logic regressions)

**Goal**: Test Windows-specific logic paths that compile on any platform but behave differently.

#### Rust Tests (`desktop/src-tauri/src/`)

| Module | Test | What It Validates |
|--------|------|-------------------|
| **service.rs** | `test_windows_udid_wmi_available` | WMI query returns non-empty, non-FFFFFFFF UUID (cfg(windows) only) |
| **service.rs** | `test_windows_service_path_resolution` | `current_exe().parent().join("k2.exe")` path format is correct |
| **service.rs** | `test_powershell_command_construction` | PowerShell `-Verb RunAs` command string has no injection risks |
| **window.rs** | `test_calculate_window_size_1080p` | 1920x1080 → correct window dimensions |
| **window.rs** | `test_calculate_window_size_768p` | 1366x768 → respects MAX_HEIGHT_RATIO (common Windows laptop) |
| **window.rs** | `test_calculate_window_size_4k` | 3840x2160 → respects MAX_WIDTH |
| **log_upload.rs** | `test_windows_log_dir_programdata` | Path contains `ProgramData\kaitu` (cfg(windows) only) |
| **log_upload.rs** | `test_cleanup_truncates_on_windows` | File truncated to 0 bytes, not deleted (cfg(windows) only) |

#### Go Tests (`k2/` — new `_windows_test.go` files)

| File | Test | What It Validates |
|------|------|-------------------|
| **provider/dns_windows_test.go** | `TestDNSStateFilePath` | Path includes `ProgramData\k2` |
| **provider/dns_windows_test.go** | `TestSaveAndRemoveDNSState` | Marker file write + read + delete roundtrip |
| **provider/dns_windows_test.go** | `TestCleanupStaleDNS_NoMarker` | No marker → no-op, no panic |
| **provider/dns_windows_test.go** | `TestCleanupStaleDNS_FreshMarker` | Fresh marker (<24h) + TUN exists → keeps marker |
| **cmd/k2/signal_windows_test.go** | `TestRunSignal_RejectReload` | `reload` returns "not supported on Windows" error |
| **cmd/k2/signal_windows_test.go** | `TestRunSignal_StopNonexistentPID` | PID not found → cleans stale PID file |
| **daemon/service_windows_test.go** | `TestInstallServiceRetry` | Retry logic handles ERROR_SERVICE_MARKED_DELETE |
| **config/log_windows_test.go** | `TestIsRoot_NonElevated` | Non-admin process → returns false |

#### TypeScript Tests (`webapp/src/`)

| File | Test | What It Validates |
|------|------|-------------------|
| **services/__tests__/cloud-api.test.ts** | `windows UA header` | Already exists ✓ |
| **services/__tests__/tauri-k2.test.ts** | `WebView2 origin handling` | `tauri.localhost` treated as valid origin |

### L3: Build Verification (NSIS builds + artifacts are correct)

**Goal**: Verify the Windows installer package can be built and artifacts have correct structure.

**Trigger**: Only on `main` branch push or `workflow_dispatch` (not every PR — too slow).

```yaml
build-windows:
  runs-on: [self-hosted, Windows]
  if: github.ref == 'refs/heads/main' || github.event_name == 'workflow_dispatch'
  needs: test-windows
  steps:
    - # Full NSIS build (unsigned)
    - yarn tauri build --target x86_64-pc-windows-msvc --ci

    - # Verify artifacts
    - name: Verify NSIS output
      shell: bash
      run: |
        VERSION=$(node -p "require('./package.json').version")
        NSIS_DIR="desktop/src-tauri/target/x86_64-pc-windows-msvc/release/bundle/nsis"

        # Installer exists
        test -f "${NSIS_DIR}/Kaitu_${VERSION}_x64-setup.exe"

        # k2 binary was bundled
        test -f "desktop/src-tauri/binaries/k2-x86_64-pc-windows-msvc.exe"

        # Version in Cargo.toml matches package.json
        CARGO_VERSION=$(grep '^version' desktop/src-tauri/Cargo.toml | head -1 | sed 's/.*"\(.*\)".*/\1/')
        PKG_VERSION=$(node -p "require('./package.json').version")
        [ "$CARGO_VERSION" = "$PKG_VERSION" ] || { echo "Version mismatch: Cargo=$CARGO_VERSION, package.json=$PKG_VERSION"; exit 1; }
```

### L4: Service Integration Tests (k2 service lifecycle)

**Goal**: Verify k2 Windows Service lifecycle (install → start → ping → stop → uninstall).

**Trigger**: `workflow_dispatch` only (requires admin privileges).

#### 4a. Go Integration Tests (build tag: `integration`)

```go
// daemon/service_integration_windows_test.go
//go:build windows && integration

package daemon

func TestServiceInstallAndStart(t *testing.T) {
    // Skip if not elevated
    if !config.IsRoot() { t.Skip("requires admin") }

    // Cleanup any previous test service
    t.Cleanup(func() { uninstallService() })

    // Install
    err := installService()
    require.NoError(t, err)

    // Query status
    installed := isServiceInstalled()
    assert.True(t, installed)

    // Verify daemon starts and responds to ping
    // ...

    // Stop and uninstall
    err = uninstallService()
    require.NoError(t, err)
}
```

Run with: `go test -tags integration ./daemon/... -run TestServiceInstall -count=1`

#### 4b. PowerShell Smoke Script

```powershell
# scripts/test-windows-service-smoke.ps1
# Automates docs/test-windows-service.md scenarios 1-4
#
# 1. Fresh install: k2.exe service install → sc query kaitu = RUNNING
# 2. Ping: curl http://127.0.0.1:1777/ping → {"code":0}
# 3. Idempotent reinstall: k2.exe service install → no error
# 4. Stop: sc stop kaitu → STOPPED
# 5. Cleanup: sc delete kaitu
```

#### 4c. CORS Origin Test

Already covered by `api/middleware_cors_test.go` with `tauri.localhost` test case. Ensure this runs in Windows CI job.

### L5: Regression Guards (historical bugs never recur)

**Rule**: Every Windows-specific bug fix MUST include a regression test.

| Historical Bug | Regression Test | Status |
|----------------|-----------------|--------|
| 9744ea2: WebView2 `tauri.localhost` CORS | `api/middleware_cors_test.go` | ✓ Exists |
| a9a00e1: wmic ARM64 failure | `service.rs` WMI test (L2) | New |
| 7eee54a: UDID `unknown` fallback | `webapp/services/__tests__/stats.test.ts` | ✓ Exists |
| 0990941 + a874d8e: tray behavior | Platform behavior documented in `tray.rs` comments | Document-only (OS event loop dependency) |
| 31ace38: Error 1053 service start | `svc_windows_test.go` Execute() reports Running | New |
| 4982946: file lock on delete | `log_upload.rs` truncate test (L2) | New |
| b40e0bf: CRLF line endings | `.gitattributes` enforcement | ✓ Exists |
| e5b21d9: wintun gen.go missing | `make build-k2-windows` in CI (L1) | New |

## Implementation Phases

```
Phase 1 (1-2 days):  L1 — ci.yml Windows job
Phase 2 (3-5 days):  L2 — Platform unit tests (Rust + Go + TS)
Phase 3 (1-2 days):  L3 — Build verification in CI
Phase 4 (2-3 days):  L4 — Service integration tests + smoke script
Phase 5 (ongoing):   L5 — Regression guard discipline
```

## Files to Create/Modify

### New Files
- `k2/provider/dns_windows_test.go` — DNS state marker tests
- `k2/cmd/k2/signal_windows_test.go` — Signal handling tests
- `k2/daemon/service_windows_test.go` — SCM install/uninstall tests
- `k2/daemon/service_integration_windows_test.go` — Integration tests (admin required)
- `k2/config/log_windows_test.go` — Admin check test
- `scripts/test-windows-service-smoke.ps1` — Service lifecycle smoke test

### Modified Files
- `.github/workflows/ci.yml` — Add `test-windows` job
- `desktop/src-tauri/src/service.rs` — Add `#[cfg(test)] #[cfg(windows)]` tests
- `desktop/src-tauri/src/window.rs` — Add `calculate_window_size` tests (platform-independent)
- `desktop/src-tauri/src/log_upload.rs` — Add Windows cleanup test
- `webapp/src/services/__tests__/tauri-k2.test.ts` — Add WebView2 origin test

## Design Decisions

1. **k2 submodule is read-only** — Go test files for k2 will need to be contributed upstream or maintained in a fork. Alternative: test at the integration layer from k2app side.

2. **Admin tests gated by build tag** — `//go:build windows && integration` prevents accidental execution in regular CI.

3. **NSIS build-only in CI** — No silent install in CI to avoid side effects on the runner. Installation testing remains manual per `docs/test-windows-service.md`.

4. **Tray behavior not unit-testable** — Depends on OS window manager event loop. Documented as "verify manually" with clear expected behavior per platform.

5. **PR vs main branch scope** — PR CI runs L1 (compile + test). Main branch CI adds L3 (full build). L4 is manual/dispatch only.
