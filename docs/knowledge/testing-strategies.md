# Testing Strategies

Patterns and lessons from test implementation.

---

## VpnClient Dependency Injection for Testing (2026-02-14, k2app-rewrite)

**Pattern**: Factory function `createVpnClient(override?)` enables test injection without mocking modules. Tests call `createVpnClient(mock)` to inject `MockVpnClient`; production code gets `HttpVpnClient` by default.

**Benefits**: No module mocking, type-safe injection, works with any test framework, no test-specific logic in production code.

**Validating tests**: `webapp/src/vpn-client/__tests__/index.test.ts`, all store tests

---

## State Deduplication Testing (2026-02-14, k2app-rewrite)

**Challenge**: Verify that polling loop does NOT emit events for unchanged state.

**Approach**: Use `vi.useFakeTimers()` to control poll intervals. Mock returns same state twice, advance time past two poll cycles, assert `events.length === 1`. Tests both positive (state change → event) and negative (no change → no event).

**Validating tests**: `webapp/src/vpn-client/__tests__/http-client.test.ts`

---

## Mock Client Observable State Pattern (2026-02-14, k2app-rewrite)

**Pattern**: Plain TypeScript class implementing `VpnClient` with setter methods (`setStatus()`) and public call-tracking arrays (`connectCalls`). Manual event emission via `emitEvent()`. No mocking library needed.

**Benefits**: State observable, controllable, type-safe. Each test gets fresh instance (no shared state to reset).

**Validating tests**: `webapp/src/vpn-client/__tests__/mock-client.test.ts`, all store tests

---

## NativeVpnClient Constructor Injection Testing (2026-02-14, mobile-rewrite)

**Pattern**: `NativeVpnClient(plugin)` takes K2Plugin as constructor param. Tests create mock plugin with `vi.fn()` methods — no module mocking needed. `vi.mock('k2-plugin')` doesn't work because K2Plugin is a Capacitor native module.

**Testing plugin events**: Capture `addListener` mock calls, find the handler for `'vpnStateChange'`, invoke it directly. Handler is sync even though listener setup is async.

**Validating tests**: `webapp/src/vpn-client/__tests__/native-client.test.ts` — 18 tests

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
