# Testing Strategies

Patterns and lessons from test implementation.

---

## Rust Service Module Testing (2026-02-14, k2app-rewrite)

**Pattern**: Unit tests embedded in Rust module verify pure functions (version matching, old service detection) without requiring running daemon. Fast, no I/O, runs in CI.

**Validating tests**: `desktop/src-tauri/src/service.rs` — `mod tests` block (4 tests)

---

## Integration Test Script Pattern (2026-02-14, k2app-rewrite)

**Pattern**: Bash scripts for end-to-end build verification. Structure: extract expected value → trigger build step → verify output → exit non-zero on failure.

**Why scripts over unit tests**: Build verification requires real file I/O, process execution, and platform-specific tools (codesign, signtool). Scripts run identically in CI and locally.

**Best practices**: `set -euo pipefail`, print verification steps, return non-zero on failure, verify artifacts exist before checking content.

**Validating tests**: `scripts/test_build.sh` (14 checks), `scripts/test_version_propagation.sh`

---

## Delivery Gate: tsc + vitest After Every Merge (2026-02-14, k2app-rewrite)

**Pattern**: After merging parallel worktree branches, always run: `npx tsc --noEmit` (catch unused imports, type errors from combined code) then `yarn test` (catch broken interactions).

**Why**: Parallel branches compile independently but may conflict after merge — unused imports, duplicate declarations, incompatible type changes. Delivery gate catches these before commit.

**Cross-reference**: See Bugfix Patterns → "Unused Import Causes TS6133 After Merge"

---

## Hidden Entry Point Testing with Fake Timers (2026-02-16, mobile-debug)

**Pattern**: Hidden UI entries (e.g., "tap version 5 times") are testable with `vi.useFakeTimers()`. Test clicks in rapid succession (within timeout), then verify navigation. Test timeout reset by advancing time past threshold, then clicking again.

**Key technique**: Mock `window.location.href` assignment with `Object.defineProperty` or by spying on the setter. Direct assignment `window.location.href = '/x'` triggers navigation in jsdom — intercept it.

**Why test this**: Hidden entries are easy to break silently (no visible UI, no user-facing flow). A unit test ensures the tap counter + timeout logic stays correct across refactors.

**Validating tests**: `webapp/src/pages/__tests__/Settings.test.tsx` — `test_settings_debug_tap_counter`, `test_settings_tap_counter_resets`

---

## Debug Tools Don't Need Unit Tests for Themselves (2026-02-16, mobile-debug)

**Pattern**: When the feature IS a test tool (debug page for native bridge validation), its own testing strategy is manual on-device. Unit-testing a debug page's DOM manipulation adds maintenance cost without real value.

**What to test instead**: (1) Build verification — build produces the expected output files. (2) Entry point — the navigation path to reach the debug page. (3) The debug page itself validates manually on device.

**Validating tests**: Build output check (dist/debug.html exists); Settings entry point tests.

---

## Component Test Pattern: Mock Stores + Testing Library (2026-02-16, kaitu-feature-migration)

**Pattern**: Component tests use `@testing-library/react` with Zustand store mocking via `vi.mock()`. Mock store returns controllable state.

**Structure**:
```typescript
vi.mock('@/stores/auth.store', () => ({
  useAuthStore: vi.fn(() => ({ /* mock state */ }))
}));

test('renders user info', () => {
  render(<Account />);
  expect(screen.getByText('email@example.com')).toBeInTheDocument();
});
```

**Why mock stores**: Isolates component logic from store logic. Store tests verify state management; component tests verify rendering/interaction.

**Trade-off**: Not full integration test. Covered by separate integration test suite (login flow, purchase flow).

**Validating tests**: `webapp/src/pages/__tests__/Purchase.test.tsx`, `Account.test.tsx`, etc. — 49 test files, 279 tests total

---

## Feature Coverage via AC-to-Test Mapping (2026-02-16, kaitu-feature-migration)

**Pattern**: Plan document maps each Acceptance Criteria (AC1–AC53) to specific test function names. Execution writes tests named exactly as mapped.

**Benefits**:
- Traceability: AC ↔ test name ↔ validating code
- Review efficiency: "AC25 passes" = verifiable claim
- Gap detection: missing test = missing AC coverage

**Example**: AC25 "Account membership status card with expiry" → `test_account_membership_card` in `Account.test.tsx`

**Baseline tracking**: 129 tests (k2app-rewrite) → 279 tests (kaitu-feature-migration) → 284 tests (unified-engine) → 244 tests (webapp-v2 migration, old tests removed, new tests added)

**Validating artifact**: Plan AC Mapping table in `.word9f/kaitu-feature-migration/plan.md` lines 536–593

---

## Bridge transformStatus() Unit Tests: Mock IPC, Assert Normalized Output (2026-02-17, vpn-error-reconnect)

**Pattern**: Test `transformStatus()` logic by mocking the IPC invoke and verifying that the normalized `StatusResponseData` structure is returned — not the raw backend string. Each test case covers one transformation rule.

**Test cases to cover for any bridge**:
1. `"stopped"` → `"disconnected"` normalization (Tauri-specific)
2. `"disconnected" + error string` → `state: "error"` synthesis
3. `"connected" + error string` → error recorded but `state` unchanged (error clears on reconnect)
4. `connected_at` ISO string → `startAt` Unix seconds
5. Normal `"connected"` → `running: true`, no error
6. Normal `"disconnected"` (no error) → `running: false`, no error

**Vitest mock pattern for Tauri bridge**:
```typescript
vi.mock('@tauri-apps/api/core', () => ({
  invoke: vi.fn()
}));

test('stopped normalized to disconnected', async () => {
  vi.mocked(invoke).mockResolvedValue({
    code: 0, data: { state: 'stopped' }
  });
  const result = await _k2.run('status');
  expect(result.data.state).toBe('disconnected');
  expect(result.data.running).toBe(false);
});
```

**Why test transformation separately from store**: `transformStatus()` is pure logic. Testing it in isolation (not through the full store → component chain) gives precise failure messages: "error synthesis broken" rather than "button doesn't show red".

**Bidirectional link**: Architecture Decisions → "Bridge as State Contract Translation Layer" explains why `transformStatus()` must exist.

**Validating tests**: `webapp/src/services/__tests__/tauri-k2.test.ts` (6 tests), `webapp/src/services/__tests__/capacitor-k2.test.ts` (3 tests)

---

## Go Engine OnNetworkChanged TDD: State Guard + Signal Sequence (2026-02-17, vpn-error-reconnect)

**Pattern**: Engine behavioral tests use mock EventHandler and mock wire transport to verify the sequence of state signals emitted and whether `ResetConnections()` was called.

**Critical test case — guard condition**: `OnNetworkChanged()` must be a no-op when engine is not in `StateConnected`. Test must verify that calling it in `StateDisconnected` or `StateConnecting` emits no signals and calls no reset.

**Critical test case — signal sequence**: When connected, expect exactly: `["reconnecting", "connected"]` in that order. A signal recorder mock captures all `OnStateChange` calls.

**Mock pattern (Go)**:
```go
type mockHandler struct {
    states []string
}
func (m *mockHandler) OnStateChange(state string) { m.states = append(m.states, state) }

type mockWire struct {
    resetCalled bool
}
func (m *mockWire) ResetConnections() { m.resetCalled = true }
```

**Test structure for Resettable interface**:
```go
// Wire that implements Resettable
engine.wire = &mockWire{}
engine.state = StateConnected
engine.OnNetworkChanged()
assert.Equal(t, []string{"reconnecting", "connected"}, handler.states)
assert.True(t, wire.resetCalled)

// Wire that does NOT implement Resettable (plain interface)
engine.wire = &mockWireNoReset{}
// No panic, no reset, signals still emitted
```

**Validating tests**: `k2/engine/engine_test.go` — 4 tests (connected triggers reset, disconnected no-op, connecting no-op, signal order verified), `k2/wire/transport_test.go` — 5 subtests

---

## Stale Mock Properties Survive tsc When Typed as `any` (2026-02-18, platform-interface-cleanup)

**Pattern**: After deleting `isDesktop`/`isMobile` from `IPlatform`, 6 test files still had mock platform objects with `{ isDesktop: true, isMobile: false }`. These passed tsc because mock objects are typed as `any` (e.g., `(window as any)._platform = { ... }`).

**Risk**: Low — extra properties on mocks don't affect test behavior. Tests pass because they test specific interactions, not the shape of mock objects.

**Cleanup approach**: Cosmetic — remove stale properties from mocks when touching those test files for other reasons. Not worth a dedicated cleanup pass.

**Prevention**: When interface-breaking changes delete properties, grep for the property names in test files. Fix production code first (tsc catches those). Then clean test mocks if convenient.

**Affected files**: `consumer-migration.test.ts`, `auth-service-v2.test.ts`, `kaitu-core.test.ts`, `Dashboard.test.tsx`, `config.store.test.ts`

**Tests**: All 285 tests pass with stale mock properties present.
**Source**: platform-interface-cleanup (2026-02-18)
**Status**: verified

---

## vi.clearAllMocks() Clears Mock Implementations in Nested Describe Blocks (2026-02-18, tauri-updater-and-logs)

**Problem**: `vi.clearAllMocks()` in a parent `beforeEach` clears mock implementations set by `vi.mock()` factories at the top of the file. When nested `describe` blocks call `vi.clearAllMocks()` and then need mock implementations (e.g., `mockListen.mockResolvedValue(() => {})`), they must re-set them in their own `beforeEach`.

**Distinction from restoreAllMocks**: `vi.clearAllMocks()` clears call history AND implementations but keeps the mock object. `vi.restoreAllMocks()` restores original functions entirely (removing the mock wrapper). Both destroy factory implementations.

**Pattern**: When multiple describe blocks share a parent `beforeEach` with `vi.clearAllMocks()`, each nested describe that depends on specific mock behavior must re-establish those mocks:

```typescript
vi.mock('@tauri-apps/api/event', () => ({
  listen: vi.fn().mockResolvedValue(() => {}),
}));

describe('parent', () => {
  beforeEach(() => {
    vi.clearAllMocks(); // Clears listen's mockResolvedValue
  });

  describe('updater', () => {
    beforeEach(async () => {
      // MUST re-set because parent cleared it
      mockListen.mockResolvedValue(() => {});
      mockInvoke.mockImplementation(async (cmd) => { ... });
      await injectTauriGlobals();
    });
  });
});
```

**Why this matters**: Without re-setting, `listen()` returns `undefined` (not a Promise), causing `listen(...).then(...)` to throw `TypeError: Cannot read properties of undefined`. The error points to an unrelated line, making debugging difficult.

**Bidirectional link**: See Framework Gotchas → "Vitest vi.restoreAllMocks() Clears vi.mock() Factory Implementations" for the related `restoreAllMocks` variant.

**Validating tests**: `webapp/src/services/__tests__/tauri-k2.test.ts` — updater describe block with re-set mockListen in beforeEach.

---

## Tauri Updater + IPC Command Testing: Mock invoke Dispatch Pattern (2026-02-18, tauri-updater-and-logs)

**Pattern**: When testing a Tauri bridge that calls multiple IPC commands during initialization (e.g., `get_platform_info` then `get_update_status`), use `mockImplementation` with command name dispatch instead of `mockResolvedValueOnce` chains. The dispatch pattern survives test reordering and additional invocations.

**Dispatch pattern**:
```typescript
mockInvoke.mockImplementation(async (cmd: string) => {
  if (cmd === 'get_platform_info') return { os: 'macos', version: '0.4.0' };
  if (cmd === 'get_update_status') return null;
  return { code: 0, message: 'ok', data: {} };
});
await injectTauriGlobals();
```

**Why not mockResolvedValueOnce**: `injectTauriGlobals()` calls `invoke` multiple times internally (platform info + update status). `mockResolvedValueOnce` chains are order-dependent and break if the implementation adds another invoke call. The dispatch pattern is resilient to implementation changes.

**Testing IPC commands after initialization**: Use `mockResolvedValueOnce` for the specific command under test, after the dispatch-based initialization:
```typescript
beforeEach(async () => {
  mockInvoke.mockImplementation(async (cmd) => { /* dispatch */ });
  await injectTauriGlobals();
});

it('applyUpdateNow calls invoke', async () => {
  mockInvoke.mockResolvedValueOnce(undefined); // This one call
  await window._platform.updater!.applyUpdateNow();
  expect(mockInvoke).toHaveBeenCalledWith('apply_update_now');
});
```

**Validating tests**: `webapp/src/services/__tests__/tauri-k2.test.ts` — all describe blocks use dispatch pattern for initialization.

---

## Go Error Classification Testing: Table-Driven Subtests with net.Error Mock (2026-02-18, structured-error-codes)

**Pattern**: `ClassifyError()` test uses Go table-driven subtests with a custom `mockTimeoutError` struct to exercise the `net.Error.Timeout()` priority path:

```go
type mockTimeoutError struct{}
func (e *mockTimeoutError) Error() string   { return "mock timeout" }
func (e *mockTimeoutError) Timeout() bool   { return true }
func (e *mockTimeoutError) Temporary() bool { return false }

tests := []struct {
    name     string
    err      error
    wantCode int
}{
    {"timeout net.Error", &mockTimeoutError{}, 408},
    {"connection refused", errors.New("wire: TCP dial: connection refused"), 503},
    {"stream rejected", errors.New("wire: stream rejected by server: 401"), 401},
    // ...19 subtests total
}

for _, tt := range tests {
    t.Run(tt.name, func(t *testing.T) {
        got := ClassifyError(tt.err)
        assert.Equal(t, tt.wantCode, got.Code)
    })
}
```

**Key test cases for any priority-chain classifier**:
1. Timeout wrapped in custom error type (net.Error interface, not string match)
2. Each string pattern at least once
3. Fallback (unrecognized string → 570)
4. nil error → nil return (safety)
5. Structured JSON output (StatusJSON uses EngineError)

**Why priority chain matters**: A timeout error message often ALSO contains words like "connection refused" or "TCP dial" (e.g., `"context deadline exceeded: TCP dial: ..."` on Go 1.21+). The `net.Error.Timeout()` check must run FIRST or it gets misclassified as 503.

**Validating tests**: `k2/engine/error_test.go` — 22 tests (19 classification subtests + StatusJSON structured/no-error + OnError interface preservation)

---

## Engine NetworkChangeNotifier Testing: Triggerable Mock + Lifecycle Assertions (2026-02-18, network-change-reconnect)

**Pattern**: `NetworkChangeNotifier` mock for engine tests needs to be triggerable from the test — not just passively callable. Use a channel-based design so the test can synchronously verify callbacks without timing issues.

**Mock design** (`k2/engine/engine_test.go`):
```go
type mockNetworkMonitor struct {
    mu       sync.Mutex
    started  bool
    closed   bool
    callback func()
}

func (m *mockNetworkMonitor) Start(callback func()) error {
    m.mu.Lock()
    defer m.mu.Unlock()
    m.started = true
    m.callback = callback
    return nil
}

func (m *mockNetworkMonitor) Close() error {
    m.mu.Lock()
    defer m.mu.Unlock()
    m.closed = true
    return nil
}

func (m *mockNetworkMonitor) triggerCallback() {
    m.mu.Lock()
    cb := m.callback
    m.mu.Unlock()
    if cb != nil {
        cb()
    }
}
```

**Key test cases for any NetworkChangeNotifier-aware component**:
1. `nil` monitor → no panic, engine starts normally (mobile path)
2. Non-nil monitor → `Start()` called on engine start, callback registered
3. Callback triggered → `OnNetworkChanged()` is called (verify via state signals or reset calls)
4. Monitor closed on `engine.Stop()` — verify `isClosed() == true`
5. Monitor closed on engine failure — verify cleanup even on error path

**Daemon testability via MonitorFactory**:
```go
d.MonitorFactory = func() (engine.NetworkChangeNotifier, any, error) {
    return &mockMonitor{}, "fake-iface-mon", nil
}
```
This mirrors the `EngineStarter` testability pattern — same dependency injection for the production-replaceable factory.

**Validating tests**: `k2/engine/engine_test.go` — 5 `TestEngine_NetworkMonitor_*` tests; `k2/daemon/network_monitor_test.go` — 3 tests

---

## Shell Script Testing with Local Filesystem Mock (2026-02-18, updater-android-router)

**Pattern**: Bash scripts that interact with cloud infrastructure (S3, AWS CLI) are tested by adding a `--s3-base=PATH` flag that switches from real AWS to local filesystem operations. Test runner creates a temp directory as mock S3, plants fake artifact files, then verifies script output.

**Script design for testability**:
```bash
use_local() { [ -n "$S3_BASE" ]; }

check_artifact() {
    if use_local; then [ -f "$S3_BASE/$path" ]
    else aws s3 ls "s3://$S3_BUCKET/$path" >/dev/null 2>&1; fi
}

download_artifact() {
    if use_local; then cp "$S3_BASE/$path" "$dest"
    else aws s3 cp "s3://$S3_BUCKET/$path" "$dest"; fi
}
```

**Test runner creates mock data**:
```bash
MOCK_S3="$WORK_TMPDIR/mock-s3"
mkdir -p "$MOCK_S3/kaitu/android/0.5.0"
echo "fake-apk" > "$MOCK_S3/kaitu/android/0.5.0/Kaitu-0.5.0.apk"
"$PUBLISH_SCRIPT" "0.5.0" --s3-base="$MOCK_S3/kaitu" --dry-run
```

**JSON output validation via Python**: Shell tests use `python3 -c "import json; m = json.load(open(...)); ..."` to validate manifest field presence, types, and format constraints. Python is always available on CI and macOS developer machines.

**Non-zero exit code testing**: `set -uo pipefail` (NOT `-e`) in test runner allows capturing non-zero exit codes from the script under test. `-e` in the test runner would cause the test runner itself to exit on the first expected-failure test.

**What to test in the script (beyond build output)**:
1. Missing artifact → exit 1 (gate condition)
2. Required JSON fields present (`version`, `url`, `hash`, `size`, `released_at`)
3. URL format (relative, not absolute)
4. Hash format (`sha256:` prefix + 64-char hex)
5. Version consistency (manifest version matches requested version)
6. CI workflow references the upload script (structural check)
7. CI upload uses versioned paths (`/{VERSION}/`, not root)

**Validating tests**: `scripts/test-publish-mobile.sh` — 10 tests; all run without AWS credentials.

---

## TDD Adaptation for Native Code Without Test Harness (2026-02-18, updater-android-router)

**Context**: K2Plugin Swift and Kotlin code has no unit test harness in this project (no XCTest, no JUnit setup). The TDD protocol requires RED → GREEN, but there is nothing to make red for native code.

**Adaptation**: For native platforms (iOS Swift, Android Kotlin), skip the RED phase and document manual test scenarios in the plan. Write the implementation directly (GREEN), then verify manually on device.

**Manual test documentation in plan**: Plan AC mapping lists test function names like `test_fetchManifest_primary_success` even though these are manual, not automated. This preserves the AC↔test bidirectional link format while being honest about what "test" means for native code.

**Compensating control**: The webapp bridge (T3) covers event wiring with real vitest tests. Since T1/T2 (native) and T3 (bridge) are parallel tasks, the bridge tests validate that the event contract works correctly — testing the integration point even if the native emitter isn't testable.

**When to use workaround**: This pattern applies to any code where:
- The platform has no test harness set up
- Adding a test harness is out of scope for the feature
- The implementation is relatively simple and the contract is tested at the boundary

**Validating tests**: `webapp/src/services/__tests__/capacitor-k2.test.ts` — updater describe block (8 tests) covers the bridge side of the native↔bridge contract.

---

## OpenWrt Docker Smoke Testing: Daemon Mode Without TUN (2026-02-18, openwrt-docker-testing)

**Pattern**: Test OpenWrt k2 binary in Docker by running daemon mode (`k2 run -l 0.0.0.0:1777`) without any TUN capabilities. The daemon starts HTTP server + embedded webapp without creating TUN — TUN is only created when user initiates VPN connection via API.

**Docker setup** (no Dockerfile needed):
```bash
docker run --rm -it -p 11777:1777 \
    --entrypoint "" \
    -v ./build/k2-linux-arm64:/usr/bin/k2:ro \
    alpine:latest /usr/bin/k2 run -l 0.0.0.0:1777
```

**Key design decisions**:
- `--entrypoint ""` — overrides base image's default entrypoint (needed for non-alpine images like redis:7-alpine)
- Port mapping `11777:1777` — avoids conflict with local daemon already running on 1777
- Volume mount `:ro` — binary is read-only, no need to copy into container
- `alpine:latest` default, configurable via env var — China can't pull from Docker Hub, use any cached alpine-based image

**Four smoke tests**:
1. `GET /ping` — daemon health check (returns `{"code":0,"message":"pong"}`)
2. `GET /` — webapp serves HTML (embedded via `go:embed dist/*`)
3. `k2 version` CLI — binary executes correctly on target arch
4. `POST /api/core` with `{"action":"version"}` — daemon API responds

**Build optimization**: `build-openwrt-docker` compiles only the host architecture (via `go env GOARCH`), unlike `build-openwrt` which cross-compiles all 4 architectures. ~4x faster for local dev iteration.

**Validating tests**: `scripts/test-openwrt.sh` (4 smoke tests), `make test-openwrt`

---

## Engine Package TDD: Config Combinations as Test Cases (2026-02-16, unified-engine)

**Pattern**: `engine_test.go` has 14 test functions covering all Config field combinations. Each test verifies one aspect of Engine behavior.

**Test structure**: RED → mock dependencies (wireCfg, k2rule, provider) → GREEN → call engine.Start(cfg) → REFACTOR → assert state.

**Key test cases**:
- `TestEngineStart_MobileConfig` — fd >= 0 triggers mobile path
- `TestEngineStart_DesktopConfig` — fd == -1, Mode "tun" triggers self-create TUN
- `TestEngineStart_ProxyMode` — fd == -1, Mode "proxy" uses ProxyProvider
- `TestEngineStart_RuleFromURL_Smart` — URL `?rule=smart` → k2rule IsGlobal: false
- `TestEngineStart_RuleFromURL_Global` — URL `?rule=global` → k2rule IsGlobal: true
- `TestEngineStart_DataDir_PassedToK2rule` — Config.DataDir sets k2rule CacheDir
- `TestEngineStart_WithDirectDialer` — non-nil DirectDialer passed to transports
- `TestEngineStart_PreferIPv6` — PreferIPv6 + wireCfg.IPv6 available → host replaced

**Why exhaustive combination testing**: Engine is the single tunnel lifecycle manager for both platforms. A bug in any Config path affects production. Exhaustive coverage ensures all platform paths work.

**Trade-off**: 14 tests for 1 package is high. But engine replaces 2 implementations (daemon/tunnel.go + mobile/mobile.go), so it needs their combined test coverage.

**Validating artifact**: `k2/engine/engine_test.go` — 500+ lines, 14 test functions

---

## MCP Tool Testing: Separate createServer() from main() for Testability (2026-02-20, kaitu-ops-mcp)

**Pattern**: MCP server tests import `createServer(config)` directly — never the `main()` function that starts stdio transport. This lets tests verify tool registration and behavior without blocking the test process on stdin.

**Test structure for MCP tools** (vitest):
```typescript
// list-nodes.test.ts
import { filterNodes } from './list-nodes.js'

describe('filterNodes', () => {
  const mockRaw = { code: 0, data: { nodes: [/* ... */] } }
  it('filters by country', () => {
    const result = filterNodes(mockRaw, { country: 'jp' })
    expect(result.every(n => n.country === 'jp')).toBe(true)
  })
  it('strips batch_script_results', () => {
    const result = filterNodes(mockRaw, {})
    expect(result[0]).not.toHaveProperty('batch_script_results')
  })
})
```

**Two-layer test separation**:
1. **Pure logic tests** (`filterNodes`, `redactStdout`, `loadConfig`) — no MCP server, no SSH, pure functions. Fast, deterministic.
2. **Integration tests** (`exec-on-node.test.ts`, `ssh.test.ts`) — mock SSH server or stub the ssh2 client. Verify stdout truncation at 10000 chars, redaction applied, timeout behavior.

**Config loading tests**: Use temp TOML files + environment variable manipulation (`process.env['KAITU_ACCESS_KEY'] = 'test'` / `delete process.env['..']`). Restore env vars in `afterEach`. Use `os.tmpdir()` for temp config files.

**What NOT to test for MCP tools**: Do not test the `server.tool()` registration call directly — the MCP SDK handles that. Test the handler logic (pure functions) and the tool callback behavior (integration tests).

**Validating tests**: `tools/kaitu-ops-mcp/src/tools/list-nodes.test.ts` (AC1), `tools/kaitu-ops-mcp/src/redact.test.ts` (AC4), `tools/kaitu-ops-mcp/src/config.test.ts` (AC6–8), `tools/kaitu-ops-mcp/src/tools/exec-on-node.test.ts` (AC2–5)

---

## Config Module Pattern: TOML + Env Var Fallback with Clear Error Aggregation (2026-02-20, kaitu-ops-mcp)

**Pattern**: Config loading collects ALL missing fields before throwing, so the user sees a complete error message in one go rather than fixing one field at a time.

**Implementation** (`tools/kaitu-ops-mcp/src/config.ts`):
```typescript
const missing: string[] = []
if (!centerUrl) missing.push('center.url (or KAITU_CENTER_URL env var)')
if (!accessKey) missing.push('center.access_key (or KAITU_ACCESS_KEY env var)')
// ... all required fields

if (missing.length > 0) {
  throw new Error(
    `Configuration missing required fields:\n${missing.map(f => `  - ${f}`).join('\n')}`
  )
}
```

**TOML parsing**: Use `smol-toml` (`parse as parseToml`). Catches parse errors explicitly — only `ENOENT` (file not found) is silently skipped; parse errors rethrow with context. Env vars override TOML values with `??` nullish coalescing.

**SSH key resolution priority chain**: env var → TOML config → `~/.ssh/id_rsa` (if exists) → `~/.ssh/id_ed25519` (if exists) → fall back to `id_rsa` path even if missing (produces clearer downstream error).

**Why `smol-toml` over `@iarna/toml`**: Smaller bundle, ESM-first, no transitive dependencies. Spec-compliant for TOML 1.0.

**Validating tests**: `tools/kaitu-ops-mcp/src/config.test.ts` — `test_config_missing_error` lists all missing fields; `test_config_env_overrides_toml`; `test_ssh_key_resolution_order` (AC6–8)

---
