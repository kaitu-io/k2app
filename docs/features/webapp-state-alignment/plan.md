# Plan: Webapp State Alignment (k2 3-state model)

## Meta

| Field | Value |
|-------|-------|
| Feature | webapp-state-alignment |
| Spec | N/A (driven by k2 submodule update bfeb06c) |
| Date | 2026-02-14 |
| Complexity | Simple (<5 feature files, no new abstractions) |

## Context

k2 daemon commit `bfeb06c` simplified the state machine from 5 states to 3:

| Before | After |
|--------|-------|
| `stopped`, `connecting`, `connected`, `disconnecting`, `error` | `stopped`, `connecting`, `connected` |

**Key change**: `error` is now orthogonal to state. `lastError` is tracked
independently and persists until cleared by a successful connect. The daemon
no longer enters an `error` or `disconnecting` state — it goes directly from
`connected` → `stopped` on disconnect, and stays `stopped` with `lastError`
set on failure.

The webapp's `VpnState` type must align. Error display is already handled
separately via the `error` field in `vpn.store.ts` — only the state type
and optimistic state-setting need to change.

## AC Mapping

| AC | Test | Task |
|----|------|------|
| VpnState has exactly 3 states | vitest: ConnectionButton renders for all 3 states | T1 |
| Connect failure sets error without invalid state | vitest: vpn.store sets state:'stopped' + error on failure | T1 |
| Disconnect does not set optimistic state | vitest: vpn.store calls disconnect without state change | T1 |
| ConnectionButton has no disconnecting/error variants | vitest: button disabled only during connecting | T1 |
| Error display independent of state | vitest: Dashboard shows error when error is set + state is stopped | T1 |
| TypeScript compiles with strict mode | `cd webapp && npx tsc --noEmit` passes | T1 |

## Feature Tasks

### T1: Align VpnState with 3-state model

**Scope**: Remove `disconnecting` and `error` from VpnState union type. Update
store, components, and tests to match.

**Files**:
- `webapp/src/vpn-client/types.ts` (modify)
- `webapp/src/stores/vpn.store.ts` (modify)
- `webapp/src/components/ConnectionButton.tsx` (modify)
- `webapp/src/components/__tests__/ConnectionButton.test.tsx` (modify)
- `webapp/src/stores/__tests__/vpn.store.test.ts` (modify)
- `webapp/src/pages/__tests__/Dashboard.test.tsx` (modify)

**Depends on**: none (k2 submodule already updated)

**Changes**:

1. **`types.ts`**: `VpnState = 'stopped' | 'connecting' | 'connected'`
   - Remove `'disconnecting' | 'error'` from union
   - `VpnEvent` error type unchanged (error events still exist, they're orthogonal)

2. **`vpn.store.ts`**:
   - `connect()` catch: `set({ state: 'stopped', error: ... })` instead of `set({ state: 'error', ... })`
   - `disconnect()`: remove `set({ state: 'disconnecting', error: null })` — let daemon events drive state

3. **`ConnectionButton.tsx`**:
   - Remove `disconnecting` and `error` CVA variants
   - `isTransitional` = `state === 'connecting'` only
   - Label map: 3 entries (stopped→connect, connecting→connecting, connected→connected)

4. **Tests**: Update all assertions that reference `'error'` or `'disconnecting'` state values

**TDD**:
- RED: Change `VpnState` to 3 states → TypeScript errors in store, component, tests
- GREEN: Update store (connect catch → stopped, remove disconnecting), update
  ConnectionButton (remove 2 CVA variants, simplify isTransitional + label map)
- REFACTOR: Update all tests to assert new behavior, verify `yarn test` green

**Acceptance**:
- `cd webapp && npx tsc --noEmit` passes
- `cd webapp && yarn test` all green
- No references to `'disconnecting'` or `'error'` as state values in webapp code

**Knowledge**: Architecture decisions → VpnClient Abstraction Pattern

---

## Execution Notes

- **Single task**: All files are tightly coupled via the VpnState type. Changing
  the type breaks everything — must be fixed atomically.
- **No VpnEvent changes**: `{ type: 'error'; message: string }` event is still
  valid. Error *events* still exist (they set the `error` field in store).
  Only the `error` *state* is removed.
- **Dashboard.tsx**: No code changes needed. Error display already uses the `error`
  field (`{error && <div>...{error}</div>}`), not the state value. The `disconnect`
  function destructured from store is the function, not the state string.
- **MockVpnClient**: No changes needed. Mock doesn't reference state constants.
- **HttpVpnClient**: No changes needed. It receives state strings from daemon and
  passes them through — daemon now sends only 3 valid states.
- **Downstream**: Mobile plan (T4 NativeVpnClient) depends on this being complete,
  since NativeVpnClient uses the VpnState type.
