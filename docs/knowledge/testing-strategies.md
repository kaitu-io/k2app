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
