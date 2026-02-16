# Feature: Webapp State Alignment

## Meta

| Field     | Value                                    |
|-----------|------------------------------------------|
| Feature   | webapp-state-alignment                   |
| Version   | v1                                       |
| Status    | implemented                              |
| Created   | 2026-02-14                               |
| Updated   | 2026-02-14                               |

## Version History

| Version | Date       | Summary                                                    |
|---------|------------|------------------------------------------------------------|
| v1      | 2026-02-14 | Initial: 5-state → 3-state VpnState, error as orthogonal  |

## Overview

Simplify webapp's `VpnState` from 5 states to 3, matching k2 daemon commit
`bfeb06c` which removed `disconnecting` and `error` as discrete states. Error
is now orthogonal — tracked independently via `lastError` field, not as a state
value. This eliminates impossible state combinations and simplifies UI logic.

## Context

- **k2 daemon change** (commit `bfeb06c`): Reduced state machine from
  `stopped | connecting | connected | disconnecting | error` to
  `stopped | connecting | connected`.
- **Rationale**: `error` is not a state — it's a condition that can occur in
  any state. `disconnecting` is transient and not observable by clients in
  practice (disconnect is effectively instant from the API perspective).
- **Impact**: All webapp code referencing `VpnState` must align. Error display
  is already decoupled (uses `error` field in store, not state value).

## Architecture

### State Model (before → after)

```
Before (5 states):
  stopped → connecting → connected → disconnecting → stopped
                 ↓                       ↓
               error                   error

After (3 states):
  stopped → connecting → connected → stopped
    │            │
    └── error field set independently (orthogonal)
```

### Error Handling Model

```typescript
// Error is orthogonal to state — tracked separately
interface VpnStore {
  state: VpnState;       // 'stopped' | 'connecting' | 'connected'
  error: string | null;  // independent, cleared on next connect attempt
}

// Connect failure: state goes to 'stopped', error is set
connect: async (wireUrl) => {
  set({ state: 'connecting', error: null });
  try {
    await client.connect(wireUrl);
  } catch (e) {
    set({ state: 'stopped', error: e.message }); // NOT state: 'error'
  }
}

// Disconnect: no optimistic state — daemon event drives state change
disconnect: async () => {
  await client.disconnect();  // state change arrives via subscribe event
}
```

### UI Mapping

ConnectionButton has exactly 3 variants (CVA):

| VpnState | Button Color | Label | Clickable |
|----------|-------------|-------|-----------|
| `stopped` | Blue | "Connect" | Yes |
| `connecting` | Yellow (pulse) | "Connecting..." | No (disabled) |
| `connected` | Green → Red on hover | "Connected" | Yes (disconnect) |

Error display is handled by Dashboard component using the `error` field,
independent of which state the VPN is in.

## Technical Decisions

### TD1: Remove `disconnecting` State

The `disconnecting` state existed for optimistic UI feedback ("Disconnecting..."
shown briefly). In practice:

1. k2 daemon disconnect is synchronous — HTTP 200 means already disconnected
2. Mobile Engine.Stop() is also synchronous
3. The state transition `connected → disconnecting → stopped` happens within
   one event loop tick — users never see it

Removing it simplifies:
- CVA variants: 5 → 3
- `isTransitional` check: `state === 'connecting' || state === 'disconnecting'` → `state === 'connecting'`
- Label map: 5 entries → 3 entries

### TD2: Error as Orthogonal Field

The old `error` state conflated "what happened" (error) with "where we are"
(state). This caused problems:

- After an error, what's the state? Need to track "previous state" separately
- UI needed special handling: "show error" AND "allow reconnect" simultaneously
- Mobile platforms have no `error` state in native VPN APIs (NEVPNStatus, VpnService)

New model: `error: string | null` is a separate store field. State always
reflects the actual VPN state. Error persists until the next connect attempt
clears it.

## Acceptance Criteria

- [x] `VpnState` type has exactly 3 states: `'stopped' | 'connecting' | 'connected'`
- [x] Connect failure sets `state: 'stopped'` + `error: message` (not state: 'error')
- [x] Disconnect does not set optimistic state — daemon event drives transition
- [x] ConnectionButton has no `disconnecting` or `error` CVA variants
- [x] `isTransitional` checks only `state === 'connecting'`
- [x] Error display in Dashboard uses `error` field, independent of `state`
- [x] TypeScript strict mode compiles (`cd webapp && npx tsc --noEmit`)
- [x] All webapp tests pass (`cd webapp && yarn test`)
- [x] No references to `'disconnecting'` or `'error'` as VpnState values in webapp code
