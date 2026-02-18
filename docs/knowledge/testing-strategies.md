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
