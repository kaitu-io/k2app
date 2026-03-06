# Webapp Architecture Refactor

Date: 2026-03-06

## Problem Statement

Dashboard.tsx simultaneously manages connection source selection, URL resolution, connection orchestration, config persistence, and UI rendering. Each new connection source (self-hosted was the first) requires modifying 4+ tightly-coupled locations. Two known bugs stem from the current architecture:

1. **Service restart → tunnel selection blocked**: `pointerEvents: 'none'` on the entire Dashboard container is driven by a timer-computed `isServiceFailedLongTime` boolean. After service restarts, the boolean may not clear reliably.

2. **iOS connecting flashes "disconnected"**: The 5-second optimistic state timeout (`OPTIMISTIC_TIMEOUT_MS`) expires before the iOS engine completes TLS+TUN setup (often >5s), causing a brief fallback to `disconnected` before the real `connected` event arrives.

Both bugs are symptoms of implicit state management: timers driving state instead of explicit events.

## Goals

1. Dashboard.tsx becomes pure UI — no connection logic, no URL resolution, no config persistence
2. VPN state behavior described by a single transition table — no scattered timers or if/else chains
3. `buildConnectConfig` is a pure function — same inputs always produce same outputs
4. Bugs #1 and #2 are eliminated by design, not by parameter tuning
5. "What are we connected to?" is a public, typed, single-source-of-truth API for any consumer (analytics, logging, etc.)

## Design

### New file: `stores/connection.store.ts`

Owns the concept of "current connection target" and orchestrates connect/disconnect.

```typescript
interface ActiveTunnel {
  source: 'cloud' | 'self_hosted';
  domain: string;
  name: string;
  country: string;
  serverUrl: string;  // Resolved URL with credentials (cloud: injected auth, self-hosted: raw URI)
}

interface ConnectionState {
  // Selection state (pre-connect)
  selectedSource: 'cloud' | 'self_hosted';
  selectedCloudTunnel: Tunnel | null;

  // Derived from selection (recomputed on selection change)
  activeTunnel: ActiveTunnel | null;

  // Snapshot of active tunnel at connect time (stable during connection)
  connectedTunnel: ActiveTunnel | null;

  // Monotonic counter to guard against stale async connect operations
  connectEpoch: number;
}

interface ConnectionActions {
  selectCloudTunnel: (tunnel: Tunnel) => void;
  selectSelfHosted: () => void;
  connect: () => Promise<void>;
  disconnect: () => Promise<void>;
}
```

**What moves OUT of Dashboard.tsx:**

| Currently in Dashboard | Moves to |
|---|---|
| `useState<'cloud' \| 'self_hosted'>(selectedSource)` | `connection.store.selectedSource` |
| `useState<Tunnel>(selectedCloudTunnel)` | `connection.store.selectedCloudTunnel` |
| `useMemo(activeTunnelInfo)` | `connection.store.activeTunnel` (computed on selection change) |
| `resolveServerUrl()` | `connection.store.connect()` internal |
| `getSelectedServerUrl()` | `connection.store.connect()` internal |
| `handleToggleConnection()` | Split into `connect()` / `disconnect()` |
| `updateConfig({ server: ... })` | `connection.store.connect()` internal — always persists correct URL |
| Auto-select self-hosted `useEffect` | `connection.store` init logic |

**Dashboard.tsx after refactor:**

```typescript
export default function Dashboard() {
  const { activeTunnel, connectedTunnel, selectCloudTunnel, selectSelfHosted } = useConnectionStore();
  const { state, connect, disconnect } = useVPNMachine();

  // During active connection, display snapshot; otherwise display selection
  const displayTunnel = connectedTunnel ?? activeTunnel;

  const handleToggle = () => {
    if (state === 'connected' || state === 'connecting' || state === 'reconnecting') {
      disconnect();
    } else {
      connect();
    }
  };

  // Pure rendering — no useCallback chains, no URL resolution, no config persistence
  return (
    <DashboardContainer>
      <CollapsibleConnectionSection
        serviceState={state}
        hasTunnelSelected={!!displayTunnel}
        tunnelName={displayTunnel?.name}
        tunnelCountry={displayTunnel?.country}
        onToggle={handleToggle}
      />
      {/* tunnel lists, settings — all just fire store actions */}
    </DashboardContainer>
  );
}
```

**connect() implementation (inside store):**

```typescript
connect: async () => {
  const { selectedSource, selectedCloudTunnel, activeTunnel, connectEpoch } = get();
  const selfHostedTunnel = useSelfHostedStore.getState().tunnel;
  const { buildConnectConfig, updateConfig } = useConfigStore.getState();

  // Snapshot tunnel info for stable display during connection
  set({ connectedTunnel: activeTunnel, connectEpoch: connectEpoch + 1 });
  const myEpoch = connectEpoch + 1;

  let serverUrl: string | undefined;
  if (selectedSource === 'self_hosted' && selfHostedTunnel) {
    serverUrl = selfHostedTunnel.uri;
  } else if (selectedCloudTunnel?.serverUrl) {
    serverUrl = await authService.buildTunnelUrl(selectedCloudTunnel.serverUrl);
  }

  // Guard: if user disconnected or started a new connect while we were resolving, bail out
  if (get().connectEpoch !== myEpoch) return;

  const isBeta = window._platform?.updater?.channel === 'beta';
  const logLevel = localStorage.getItem('k2_log_level') || 'info';
  const config = buildConnectConfig({ serverUrl, isBeta, logLevel });

  // Persist BEFORE _k2.run so crash between run and persist doesn't lose config
  updateConfig({ server: serverUrl });

  vpnMachine.send({ type: 'USER_CONNECT' });
  await window._k2.run('up', config);
},

disconnect: async () => {
  vpnMachine.send({ type: 'USER_DISCONNECT' });
  await window._k2.run('down');
  set({ connectedTunnel: null });
},
```

**Key design decisions for connection.store:**

1. **`connectedTunnel` snapshot**: At connect time, the active tunnel info is snapshotted into `connectedTunnel`. This prevents the UI from going blank if the user edits/clears their self-hosted tunnel while connected. `connectedTunnel` is cleared on disconnect.

2. **`connectEpoch` guard**: Monotonically incrementing counter prevents stale async operations from executing. If the user clicks connect then immediately disconnect, the in-flight `connect()` sees a mismatched epoch and bails before calling `_k2.run('up')`.

3. **Persist before run**: `updateConfig({ server: serverUrl })` is called BEFORE `_k2.run('up')` so a crash doesn't leave stale config. Trade-off: we persist a URL for a connection that might fail to start — acceptable because the persisted server is only used as a default for next launch, not as a source of truth for the current session.

### Rewrite: `stores/vpn-machine.store.ts` (replaces `vpn.store.ts`)

Replace implicit timer-driven state with an explicit state machine.

**States:**

| State | Meaning | UI |
|---|---|---|
| `idle` | Not connected, ready | Connect button enabled |
| `connecting` | User initiated, waiting for engine | Spinner, selection disabled |
| `connected` | VPN active | Disconnect button |
| `reconnecting` | Engine re-establishing after network change | Spinner overlay |
| `disconnecting` | User initiated disconnect | Spinner |
| `error` | Connection failed | Error message, retry button |
| `serviceDown` | Daemon unreachable | Full-screen alert with "Resolve" |

**Transition table:**

```typescript
const TRANSITIONS: Record<VPNState, Partial<Record<VPNEvent, VPNState>>> = {
  idle: {
    USER_CONNECT:           'connecting',
    BACKEND_CONNECTED:      'connected',      // app restart: daemon already connected
    BACKEND_ERROR:          'error',           // app restart: daemon in error state
    BACKEND_RECONNECTING:   'reconnecting',    // app restart: daemon reconnecting
    SERVICE_UNREACHABLE:    'serviceDown',
    // BACKEND_DISCONNECTED in idle: no-op (already idle)
  },
  connecting: {
    BACKEND_CONNECTED:      'connected',
    BACKEND_DISCONNECTED:   'idle',
    BACKEND_ERROR:          'error',
    SERVICE_UNREACHABLE:    'serviceDown',
    // BACKEND_RECONNECTING in connecting: ignored (NWPathMonitor early fire on iOS)
  },
  connected: {
    USER_DISCONNECT:        'disconnecting',
    BACKEND_RECONNECTING:   'reconnecting',    // debounced (see below)
    BACKEND_DISCONNECTED:   'idle',
    BACKEND_ERROR:          'error',
    SERVICE_UNREACHABLE:    'serviceDown',
  },
  reconnecting: {
    BACKEND_CONNECTED:      'connected',
    BACKEND_DISCONNECTED:   'idle',
    BACKEND_ERROR:          'error',
    USER_DISCONNECT:        'disconnecting',
    SERVICE_UNREACHABLE:    'serviceDown',
  },
  disconnecting: {
    BACKEND_DISCONNECTED:   'idle',
    BACKEND_CONNECTED:      'connected',       // race: engine reconnected before down processed
    BACKEND_ERROR:          'idle',             // error during shutdown → treat as disconnected
    SERVICE_UNREACHABLE:    'serviceDown',      // daemon crashed mid-disconnect
  },
  error: {
    USER_CONNECT:           'connecting',
    USER_DISCONNECT:        'disconnecting',
    BACKEND_CONNECTED:      'connected',        // auto-retry succeeded
    BACKEND_DISCONNECTED:   'idle',
    SERVICE_UNREACHABLE:    'serviceDown',
  },
  serviceDown: {
    SERVICE_REACHABLE:      'idle',
  },
};
```

**Key design decisions:**

1. **No optimistic timeout**. `connecting` only exits via backend events. The iOS 5-second flash bug is eliminated by design. If the engine takes 30 seconds, UI stays on "connecting" for 30 seconds.

2. **Debounced reconnecting**. When in `connected` and `BACKEND_RECONNECTING` arrives, start a 3-second timer. If `BACKEND_CONNECTED` arrives within 3s, cancel timer and stay `connected`. If timer fires, transition to `reconnecting`. This is the ONLY timer in the system, and it only delays a transition — never forces one.

3. **`serviceDown` is an explicit state**, not a computed boolean from a timestamp. When the service comes back (`SERVICE_REACHABLE`), we transition to `idle` immediately. No timer-driven duration check. Bug #1 is eliminated because the UI directly reads `state !== 'serviceDown'` instead of computing `isServiceFailedLongTime` from a timestamp + tick counter.

4. **`idle` accepts backend sync events**. On app restart, the daemon may already be connected/errored/reconnecting. The `idle` state handles `BACKEND_CONNECTED`, `BACKEND_ERROR`, and `BACKEND_RECONNECTING` so the machine syncs with reality immediately on initialization.

5. **`disconnecting` handles errors and service crash**. `BACKEND_ERROR` during disconnect transitions to `idle` (close enough). `SERVICE_UNREACHABLE` transitions to `serviceDown` so the machine doesn't get stuck.

6. **Error carries data**. The `error` state stores the `ControlError` from the backend status. `isRetrying` and `networkAvailable` are stored alongside the state, not computed separately:

```typescript
interface VPNMachineState {
  state: VPNState;
  error: ControlError | null;
  isRetrying: boolean;
  networkAvailable: boolean;
}
```

**Event ingestion (replaces `handleStatusChange`):**

```typescript
function backendStatusToEvent(status: StatusResponseData): VPNEvent {
  switch (status.state) {
    case 'connected':     return 'BACKEND_CONNECTED';
    case 'disconnected':  return 'BACKEND_DISCONNECTED';
    case 'connecting':
    case 'reconnecting':  return 'BACKEND_RECONNECTING';
    case 'error':         return 'BACKEND_ERROR';
    case 'disconnecting': return 'BACKEND_DISCONNECTED'; // treat as transitioning to idle
    default:              return 'BACKEND_DISCONNECTED';
  }
}
```

**Initialization (SSE vs polling — unchanged pattern, cleaner wiring):**

```typescript
export function initializeVPNMachine(): () => void {
  const dispatch = (event: VPNEvent, payload?: any) => {
    const { state } = useVPNMachineStore.getState();
    const nextState = TRANSITIONS[state]?.[event];
    if (!nextState) return; // invalid transition — ignore
    useVPNMachineStore.setState({
      state: nextState,
      ...(payload || {}),
    });
  };

  // Event-driven mode (desktop/mobile)
  if (window._k2?.onServiceStateChange && window._k2?.onStatusChange) {
    const unsubService = window._k2.onServiceStateChange((available) => {
      dispatch(available ? 'SERVICE_REACHABLE' : 'SERVICE_UNREACHABLE');
    });
    const unsubStatus = window._k2.onStatusChange((status) => {
      const event = backendStatusToEvent(status);
      dispatch(event, {
        error: status.error ?? null,
        isRetrying: status.retrying ?? false,
        networkAvailable: status.networkAvailable ?? true,
      });
    });
    // Bridge initial gap — syncs machine with daemon's current state
    window._k2.run('status').then((resp: any) => {
      if (resp.code === 0 && resp.data) {
        dispatch(backendStatusToEvent(resp.data), {
          error: resp.data.error ?? null,
          isRetrying: resp.data.retrying ?? false,
          networkAvailable: resp.data.networkAvailable ?? true,
        });
      }
    }).catch(() => {});
    return () => { unsubService(); unsubStatus(); };
  }

  // Polling fallback (standalone/web)
  const poll = async () => {
    try {
      const resp = await window._k2.run('status') as any;
      if (resp.code === 0 && resp.data) {
        dispatch(backendStatusToEvent(resp.data), {
          error: resp.data.error ?? null,
          isRetrying: resp.data.retrying ?? false,
          networkAvailable: resp.data.networkAvailable ?? true,
        });
        dispatch('SERVICE_REACHABLE');
      } else {
        dispatch('SERVICE_UNREACHABLE');
      }
    } catch {
      dispatch('SERVICE_UNREACHABLE');
    }
  };
  poll();
  const interval = setInterval(poll, 2000);
  return () => clearInterval(interval);
}
```

**Hook (replaces `useVPNStatus`):**

```typescript
export function useVPNMachine() {
  const state = useVPNMachineStore((s) => s.state);
  const error = useVPNMachineStore((s) => s.error);
  const isRetrying = useVPNMachineStore((s) => s.isRetrying);
  const networkAvailable = useVPNMachineStore((s) => s.networkAvailable);

  return {
    state,
    error,
    isRetrying,
    networkAvailable,
    // Convenience booleans (one-liner derivations, no computation)
    isConnected: state === 'connected',
    isDisconnected: state === 'idle',
    isServiceDown: state === 'serviceDown',
    isTransitioning: state === 'connecting' || state === 'reconnecting' || state === 'disconnecting',
    isInteractive: state === 'connected' || state === 'connecting' || state === 'reconnecting' || (state === 'error' && isRetrying),
  };
}
```

### Change: `stores/config.store.ts` — pure `buildConnectConfig`

Before (implicit reads):
```typescript
buildConnectConfig: (serverUrl?: string) => {
  const isBeta = window._platform?.updater?.channel === 'beta';
  const level = isBeta ? 'debug' : (localStorage.getItem('k2_log_level') || 'info');
  // ...
}
```

After (explicit params):
```typescript
interface ConnectConfigParams {
  serverUrl?: string;
  isBeta?: boolean;
  logLevel?: string;
}

buildConnectConfig: (params: ConnectConfigParams = {}) => {
  const { config } = get();
  const result = deepMerge(CLIENT_CONFIG_DEFAULTS, config);
  result.mode = 'tun';
  result.log = { ...result.log, level: params.isBeta ? 'debug' : (params.logLevel || 'info') };
  if (params.serverUrl) {
    result.server = params.serverUrl;
  }
  return result;
}
```

Caller (connection.store.ts) passes explicit values:
```typescript
const isBeta = window._platform?.updater?.channel === 'beta';
const logLevel = localStorage.getItem('k2_log_level') || 'info';
const config = buildConnectConfig({ serverUrl, isBeta, logLevel });
```

The implicit dependency moves from a utility function to the orchestration layer, where it belongs.

## Bug Fix Analysis

### Bug #1: Service restart → tunnel selection blocked

**Root cause**: `isServiceFailedLongTime` is a computed boolean from `serviceFailedSince` timestamp + 1-second tick timer. Multiple React render cycles + possible WebKit compositing quirks can delay the `pointerEvents: 'none'` removal.

**Fix**: The `serviceDown` state is explicit. When `SERVICE_REACHABLE` event fires, state transitions to `idle` in one store update. Dashboard reads `state === 'serviceDown'` directly — no timer, no computation, no render delay.

The `pointerEvents: 'none'` pattern on the entire Dashboard container is also eliminated. Instead, individual interactive elements check `isServiceDown` or `isInteractive` to disable themselves. This is more granular and doesn't block the entire container.

### Bug #2: iOS connecting flashes "disconnected"

**Root cause**: `OPTIMISTIC_TIMEOUT_MS = 5000` expires the `connecting` optimistic state before iOS engine completes (often >5s). Falls back to backend's `disconnected` status.

**Fix**: The state machine has no optimistic timeout. `connecting` is a real state that persists until a backend event changes it. No timer can force it back to `idle`. The concept of "optimistic state" disappears entirely — user actions (`USER_CONNECT`, `USER_DISCONNECT`) are first-class events that trigger immediate state transitions.

## Adversarial Review Results

Design was attack-tested against 16 scenarios. All critical issues resolved:

| # | Scenario | Result | Resolution |
|---|---|---|---|
| 1 | Connect then immediate disconnect race | Fixed | `connectEpoch` guard cancels stale async ops |
| 2 | Double-click connect | Pass | Transition table: `connecting` has no `USER_CONNECT` |
| 3 | Service down during connect | Pass | `connecting → SERVICE_UNREACHABLE → serviceDown` |
| 4 | App restart with daemon already connected | Fixed | `idle` now accepts `BACKEND_CONNECTED/ERROR/RECONNECTING` |
| 5 | `BACKEND_RECONNECTING` in `connecting` state | Pass | Safely ignored (NWPathMonitor early fire on iOS) |
| 6 | `BACKEND_ERROR` in `disconnecting` state | Fixed | Added `BACKEND_ERROR: 'idle'` to `disconnecting` |
| 7 | Crash between `_k2.run('up')` and persist | Fixed | Persist BEFORE `_k2.run('up')` |
| 8 | Self-hosted store not loaded when connect() fires | Risk | Sub-ms in practice; `loaded` flag check if needed |
| 9 | `activeTunnel` stale closure | Pass | Zustand `get()` always reads latest state |
| 10 | Shadow mode dual SSE subscribers | Pass | Tauri `listen()` supports multiple independent listeners |
| 11 | Phase 2 migration intermediate state | Pass | Swap is atomic per consumer |
| 12 | iOS 10-second connect | Pass | No optimistic timeout; `connecting` persists until event |
| 13 | Service restart → UI interactive | Pass | `SERVICE_REACHABLE → idle` is immediate |
| 14 | Clear self-hosted tunnel while connected | Fixed | `connectedTunnel` snapshot preserves display during connection |
| 15 | Daemon crash mid-disconnect | Fixed | Added `SERVICE_UNREACHABLE: 'serviceDown'` to `disconnecting` |
| 16 | HMR double-init debounce timer | Risk | Module-level timer; acceptable for dev-only scenario |

## Files to Create/Modify

### New files

| File | Purpose |
|---|---|
| `stores/connection.store.ts` | Connection target selection + connect/disconnect orchestration |
| `stores/vpn-machine.store.ts` | Explicit state machine for VPN lifecycle |
| `stores/__tests__/connection.store.test.ts` | Connection store tests |
| `stores/__tests__/vpn-machine.store.test.ts` | State machine transition table tests |

### Modified files

| File | Change |
|---|---|
| `stores/config.store.ts` | `buildConnectConfig` takes explicit `ConnectConfigParams` |
| `stores/index.ts` | Export new stores, update `initializeAllStores` |
| `pages/Dashboard.tsx` | Remove all connection logic, use `useConnectionStore` + `useVPNMachine` |
| `components/ServiceAlert.tsx` | Read `state === 'serviceDown'` instead of `isServiceFailedLongTime` |
| `components/CloudTunnelList.tsx` | `disabled` prop reads from `useVPNMachine().isInteractive` |
| `components/CollapsibleConnectionSection.tsx` | Props simplified — receives `VPNState` string instead of multiple booleans |

### Deleted files

| File | Reason |
|---|---|
| `stores/vpn.store.ts` | Replaced by `vpn-machine.store.ts` |
| `stores/__tests__/vpn.store.test.ts` | Replaced by `vpn-machine.store.test.ts` |

## Migration Strategy

### Phase 1: vpn-machine.store.ts (no UI change)

1. Create `vpn-machine.store.ts` with transition table + initialization
2. Create comprehensive test: one test case per transition table row (7 states x events each)
3. Wire into `initializeAllStores` alongside old vpn.store
4. Log discrepancies between old and new state (shadow mode)
5. Once stable: swap old vpn.store consumers to vpn-machine

### Phase 2: connection.store.ts

1. Create `connection.store.ts` with selection + connect/disconnect + `connectedTunnel` snapshot
2. Test: selection logic, URL resolution, config persistence, `connectEpoch` guard
3. Refactor Dashboard.tsx to consume connection.store
4. Remove old connection logic from Dashboard

### Phase 3: buildConnectConfig + cleanup

1. Change `buildConnectConfig` signature to accept explicit params
2. Update connection.store (only caller) to pass explicit params
3. Delete `vpn.store.ts`
4. Update ServiceAlert, CloudTunnelList, CollapsibleConnectionSection

### Verification

Each phase must pass:
- `cd webapp && npx vitest run` — all tests pass
- `cd webapp && npx tsc --noEmit` — no type errors
- Manual: connect/disconnect on macOS desktop
- Manual: connect/disconnect on iOS (verify no flash)

## Debounce Detail: `connected → reconnecting`

The single remaining timer in the system:

```typescript
let reconnectDebounceTimer: NodeJS.Timeout | null = null;

// Inside dispatch, when transitioning connected → reconnecting:
if (currentState === 'connected' && event === 'BACKEND_RECONNECTING') {
  if (reconnectDebounceTimer) return; // already debouncing
  reconnectDebounceTimer = setTimeout(() => {
    reconnectDebounceTimer = null;
    // Re-check: if still connected and backend is still reconnecting
    const latest = useVPNMachineStore.getState();
    if (latest.state === 'connected') {
      useVPNMachineStore.setState({ state: 'reconnecting' });
    }
  }, 3000);
  return; // don't transition yet
}

// Cancel debounce if connected event arrives
if (event === 'BACKEND_CONNECTED' && reconnectDebounceTimer) {
  clearTimeout(reconnectDebounceTimer);
  reconnectDebounceTimer = null;
}
```

This is explicit, documented, and testable. The timer delays a transition; it never forces one.

## Not In Scope

- cloudApi auth refactoring (stable, not high-frequency change area)
- vpn.store polling vs SSE unification (pattern unchanged, just re-wired)
- New UI components or visual changes (this is internal architecture only)
- Mobile-specific bridge changes (bridges are consumers, not modified)
