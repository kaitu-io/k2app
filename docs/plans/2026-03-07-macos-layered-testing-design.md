# macOS Layered Testing Design

Date: 2026-03-07

## Goal

Prevent bugs from recurring, detect cross-platform regressions, and ensure production releases are safe. Modeled after the Windows 5-layer testing approach, adapted for macOS where we already develop on the target platform.

## Current State

**Existing test assets (120+ files):**
- 32 webapp vitest files (services, stores, components, pages)
- 43 Rust tests (updater, channel, log_upload, ne, service)
- 95+ k2 Go tests (tunnel, DNS, wire protocol, daemon, server)
- 44 API Go tests (handlers, auth, cloud provider, business logic)
- 11 web vitest files (SSR, content, SEO)
- 11 MCP server vitest files

**CI (`ci.yml`) runs on Ubuntu only:**
- webapp vitest + tsc
- cargo check + cargo test
- No Go tests
- No macOS runner

**Gaps:**
1. Rust/Go platform-conditional code (`#[cfg(target_os = "macos")]`, `//go:build darwin`) never tested in CI
2. Go tests (140+ files) not in CI at all
3. Playwright E2E configured but not integrated
4. `test_build.sh` (14 checks) is manual only
5. `release-desktop.yml` has no test gate -- tag triggers build directly
6. No regression test tracking convention

## Design

### L1: CI Gate (this week)

**Change `ci.yml`:** Add macOS runner to test matrix.

```yaml
jobs:
  test:
    strategy:
      matrix:
        include:
          - os: ubuntu-latest
            tasks: [webapp-test, webapp-tsc, k2plugin-tsc]
          - os: macos-latest
            tasks: [cargo-check, cargo-test, go-test-k2]
```

**What runs where:**
- **Ubuntu:** webapp vitest, webapp tsc, k2-plugin tsc (platform-independent)
- **macOS:** cargo check, cargo test, `go test ./...` on k2 submodule (platform-dependent)

**Why macOS runner for Rust/Go:** These have `#[cfg(target_os = "macos")]` and `//go:build darwin` gated code. Running on Ubuntu misses entire code paths (ne.rs, service.rs darwin lifecycle, k2 TUN darwin code).

**Go API tests:** Add unit-only tests (those using `SetupMockDB`, no real DB). Tests guarded by `skipIfNoConfig` will auto-skip gracefully.

**Cost:** ~3 min macOS runner time per PR. GitHub-hosted macOS runners are 10x cost but the job is small.

### L2: Platform Unit Test Coverage (short-term)

Supplement tests for high-risk macOS modules that currently lack coverage.

**Rust (desktop/src-tauri):**

| Module | Risk | What to test |
|--------|------|-------------|
| `status_stream.rs` | High -- SSE drives VPN state | SSE parse, reconnect on disconnect, event emission |
| `service.rs` (darwin) | High -- daemon lifecycle | launchctl start/stop, PID monitor recovery, process state transitions |
| `log_upload.rs` | Medium | S3 upload retry, file cleanup (macOS delete vs Windows truncate) |

**TypeScript (webapp):**

| Module | Risk | What to test |
|--------|------|-------------|
| `tauri-k2.ts` transformStatus() | High -- state mapping | `stopped->disconnected`, error synthesis (`disconnected + lastError -> error`), reconnecting passthrough |
| `vpn-machine.store.ts` | High -- state machine | All 7 states, transition table completeness, invalid transition rejection |

**Go (k2 submodule):** Already 95+ tests with good coverage. No additional tests needed.

**Principle:** Only add tests for modules that (a) are high-risk, (b) have had bugs, or (c) contain platform-conditional logic. Do not chase coverage numbers.

### L3: Build Verification Automation (mid-term)

**PR-level (lightweight, every PR):**

Extract from `test_build.sh` and add to `ci.yml`:
- Version consistency: package.json vs Cargo.toml vs mobile configs (5 checks)
- webapp `yarn build` succeeds + dist artifacts exist
- tsc --noEmit passes

These run on Ubuntu (no platform dependency).

**Release gate (before build):**

Modify `release-desktop.yml` to add a prerequisite test job:

```yaml
jobs:
  test-gate:
    runs-on: macos-latest
    steps:
      - cargo test
      - go test ./... (k2)
      - yarn test (webapp)
      - version consistency checks

  build-macos:
    needs: test-gate
    # ... existing build steps
```

If test-gate fails, build never starts. No more "tag and pray".

**Post-build verification:**

Add to the end of the macOS build job:
- PKG is valid xar archive (`xar -tf`)
- .app codesign is valid (`codesign --verify --deep --strict`)
- .app.tar.gz.sig exists
- Binary architectures correct (`lipo -info` for universal)

### L4: Service Integration Tests (later)

**Feasible in CI (proxy mode, no admin):**

Start k2 daemon with `k2-test-proxy-config.yml` (SOCKS5 on :1080, no TUN, no admin). Test the daemon HTTP API lifecycle:

```
POST /api/core {"action":"status"}  -> expect disconnected
POST /api/core {"action":"up"}      -> expect connected
POST /api/core {"action":"status"}  -> expect connected + config
POST /api/core {"action":"down"}    -> expect disconnected
```

This validates: config parsing, engine start/stop, API serialization, error codes.

**Tauri IPC integration:**

Mock the daemon HTTP endpoint, test the full chain:
`JS invoke('daemon_exec') -> Rust handler -> reqwest -> mock server -> response parsing -> JS callback`

This catches: IPC serialization bugs, Rust error handling, bridge transformStatus() in real flow.

**Not in CI (requires real hardware):**
- TUN mode (needs admin + virtual NIC)
- System Extension loading (needs signed binary + AMFI)
- Network change detection (needs real interface transitions)

These stay as manual verification per `docs/debug-sysext-steps.md`.

### L5: Regression Guard (ongoing)

**Convention for every bug fix PR:**

1. PR must include a test that reproduces the bug (fails without the fix, passes with it)
2. Test name includes `regression` keyword: `test_regression_<description>`
3. Commit message format already in use: `fix(scope): description`

**Seed regression tests from recent bugs:**

| Commit | Bug | Regression test |
|--------|-----|----------------|
| `7e8ca8a` | Missing VPN state machine transitions | `vpn-machine.store.test.ts`: verify all valid transitions, reject invalid |
| `a3aeb8e` | PID monitor disconnect | `service.rs`: test process death detection + recovery |
| SSE reconnect issues | Status stream drops | `status_stream.rs`: test reconnect after server disconnect |

**No need to backfill all history.** Start enforcing from now. Over time the regression suite grows organically with each bug fix.

## Implementation Priority

| Phase | Layer | Content | Timeline | Cost |
|-------|-------|---------|----------|------|
| 1 | L1 | ci.yml add macOS runner + Go tests | This week | Low -- CI config change |
| 2 | L2 | Add tests for status_stream, service, transformStatus | Short-term | Medium -- write test code |
| 3 | L3 | test_build.sh in CI + release gate job | Mid-term | Low -- CI workflow change |
| 4 | L4 | Daemon proxy mode integration test | Later | Medium -- test infra setup |
| 5 | L5 | Regression test convention | Ongoing | Zero -- development habit |

## macOS Runner Cost Consideration

GitHub-hosted macOS runners cost 10x Linux. Mitigation:
- Only run platform-dependent tests on macOS (Rust + Go)
- Platform-independent tests stay on Ubuntu (webapp, web, tsc)
- Cache Cargo registry + Go modules aggressively
- Consider self-hosted macOS runner if cost becomes an issue (you already have self-hosted Windows)
