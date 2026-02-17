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
