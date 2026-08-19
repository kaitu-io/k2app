/**
 * VPN Machine Store — explicit state machine for VPN lifecycle
 *
 * Replaces vpn.store.ts. State transitions are defined by a single
 * lookup table (TRANSITIONS). No optimistic timeouts, no scattered timers.
 *
 * The only timer: a 3-second debounce for connected → reconnecting,
 * which delays (never forces) that specific transition.
 *
 * Usage:
 * ```tsx
 * const { state, isConnected, error } = useVPNMachine();
 * ```
 */

import { create } from 'zustand';
import { subscribeWithSelector } from 'zustand/middleware';
import type { StatusResponseData, ControlError, InitializationStatus } from '../services/vpn-types';

// ============ Types ============

export type VPNState =
  | 'idle'
  | 'connecting'
  | 'connected'
  | 'reconnecting'
  | 'disconnecting'
  | 'serviceDown';

export type VPNEvent =
  | 'USER_CONNECT'
  | 'USER_DISCONNECT'
  | 'BACKEND_CONNECTED'
  | 'BACKEND_DISCONNECTED'
  | 'BACKEND_RECONNECTING'
  | 'BACKEND_ERROR'
  | 'SERVICE_REACHABLE'
  | 'SERVICE_UNREACHABLE';

export interface VPNMachineState {
  state: VPNState;
  error: ControlError | null;
  isRetrying: boolean;
  networkAvailable: boolean;
  initialization: InitializationStatus | null;
  // Engine's session start (Unix seconds). Authoritative session timestamp —
  // survives webapp reload/cold-start. Read by disconnect() and analytics
  // to compute durationSec. null when no active session.
  startAt: number | null;
  /**
   * Monotonic counter, bumped on every *applied* state transition (including
   * self-transitions) and on `initializeVPNMachine()` teardown. Never reset, so
   * a request issued before teardown can neither apply to nor alias the machine
   * that comes after it.
   *
   * Exists because async status responses have no intrinsic ordering against
   * push events: a `run('status')` issued while connected can land *after* an
   * SSE `disconnected` event already drove the machine to idle, and
   * `idle + BACKEND_CONNECTED → connected` would then resurrect a dead session
   * (UI claims "protected" while traffic is in the clear). Callers snapshot
   * `currentRevision()` at request time and hand it to `applyStatus()`, which
   * drops the response if anything moved the machine in the meantime.
   *
   * Strictly stronger than `connection.store`'s `connectEpoch`, which (a) only
   * bumps on user intent — blind to backend-initiated drops — and (b) cannot be
   * read from here without a module cycle (connection.store imports this file).
   */
  revision: number;
}

// ============ Transition Table ============

const TRANSITIONS: Record<VPNState, Partial<Record<VPNEvent, VPNState>>> = {
  idle: {
    USER_CONNECT:         'connecting',
    BACKEND_CONNECTED:    'connected',
    BACKEND_DISCONNECTED: 'idle',           // self-transition: clears stale error via auto-clear
    BACKEND_ERROR:        'idle',
    BACKEND_RECONNECTING: 'reconnecting',
    SERVICE_UNREACHABLE:  'serviceDown',
  },
  connecting: {
    USER_DISCONNECT:      'disconnecting',
    BACKEND_RECONNECTING: 'connecting',
    BACKEND_CONNECTED:    'connected',
    BACKEND_DISCONNECTED: 'idle',
    BACKEND_ERROR:        'idle',
    SERVICE_UNREACHABLE:  'serviceDown',
  },
  connected: {
    USER_DISCONNECT:      'disconnecting',
    BACKEND_RECONNECTING: 'reconnecting', // debounced — see dispatch()
    BACKEND_DISCONNECTED: 'idle',
    BACKEND_ERROR:        'idle',
    SERVICE_UNREACHABLE:  'serviceDown',
  },
  reconnecting: {
    BACKEND_CONNECTED:    'connected',
    BACKEND_DISCONNECTED: 'idle',
    BACKEND_ERROR:        'idle',
    USER_DISCONNECT:      'disconnecting',
    SERVICE_UNREACHABLE:  'serviceDown',
  },
  disconnecting: {
    USER_DISCONNECT:      'disconnecting',
    BACKEND_RECONNECTING: 'disconnecting',
    BACKEND_DISCONNECTED: 'idle',
    BACKEND_CONNECTED:    'disconnecting',
    BACKEND_ERROR:        'idle',
    SERVICE_UNREACHABLE:  'serviceDown',
  },
  serviceDown: {
    SERVICE_REACHABLE:    'idle',
  },
};

// ============ Store ============

export const useVPNMachineStore = create<VPNMachineState>()(
  subscribeWithSelector((): VPNMachineState => ({
    state: 'idle',
    error: null,
    isRetrying: false,
    networkAvailable: true,
    initialization: null,
    startAt: null,
    revision: 0,
  })),
);

// ============ Reconnect Debounce ============

const RECONNECT_DEBOUNCE_MS = 3000;
let reconnectDebounceTimer: ReturnType<typeof setTimeout> | null = null;

function clearReconnectDebounce() {
  if (reconnectDebounceTimer) {
    clearTimeout(reconnectDebounceTimer);
    reconnectDebounceTimer = null;
  }
}

// ============ Dispatch ============

export interface DispatchPayload {
  error?: ControlError | null;
  isRetrying?: boolean;
  networkAvailable?: boolean;
  initialization?: InitializationStatus | null;
}

export function dispatch(event: VPNEvent, payload?: DispatchPayload): void {
  const { state: currentState } = useVPNMachineStore.getState();

  // Cancel pending reconnect debounce on any terminal/resolving event
  if (event === 'BACKEND_CONNECTED' || event === 'BACKEND_DISCONNECTED' ||
      event === 'BACKEND_ERROR' || event === 'USER_DISCONNECT' ||
      event === 'SERVICE_UNREACHABLE') {
    if (reconnectDebounceTimer) {
      console.debug('[VPNMachine] dispatch: reconnect debounce cancelled by', event);
    }
    clearReconnectDebounce();
  }

  // Special: debounce connected → reconnecting
  if (currentState === 'connected' && event === 'BACKEND_RECONNECTING') {
    if (reconnectDebounceTimer) return; // already debouncing
    console.debug('[VPNMachine] dispatch: reconnect debounce started (3s)');
    reconnectDebounceTimer = setTimeout(() => {
      reconnectDebounceTimer = null;
      const s = useVPNMachineStore.getState().state;
      console.debug('[VPNMachine] dispatch: debounced reconnect fired, state=' + s);
      if (s === 'connected') {
        useVPNMachineStore.setState({ state: 'reconnecting', revision: useVPNMachineStore.getState().revision + 1 });
      }
    }, RECONNECT_DEBOUNCE_MS);
    return;
  }

  const nextStateLookup = TRANSITIONS[currentState]?.[event];
  if (!nextStateLookup) {
    // TRACE: log all no-transition cases involving connecting/idle for flash diagnosis
    if (currentState === 'connecting' || currentState === 'idle') {
      console.warn('[VPNMachine] TRACE t=' + Date.now() + ': ' + currentState + ' + ' + event + ' → (no transition)');
    } else {
      console.warn('[VPNMachine] dispatch: ' + currentState + ' + ' + event + ' → (no transition)');
    }
    return;
  }

  let nextState = nextStateLookup;

  // BACKEND_ERROR + retrying: engine actively retrying → reconnecting
  // Exception: disconnecting (user initiated teardown, honor disconnect)
  if (event === 'BACKEND_ERROR' && payload?.isRetrying && nextState === 'idle' && currentState !== 'disconnecting') {
    nextState = 'reconnecting';
  }

  // TRACE: log all transitions involving connecting/idle for flash diagnosis
  if (currentState === 'connecting' || nextState === 'connecting' || currentState === 'idle' || nextState === 'idle') {
    console.warn('[VPNMachine] TRACE t=' + Date.now() + ': ' + currentState + ' + ' + event + ' → ' + nextState);
  }

  // Build state update. Every applied transition bumps `revision` — this is the
  // ordering token that lets async status responses detect that they are stale.
  const update: Partial<VPNMachineState> = {
    state: nextState,
    revision: useVPNMachineStore.getState().revision + 1,
  };

  // Clear error on transitions to idle or connected (default)
  if (nextState === 'idle' || nextState === 'connected') {
    update.error = null;
    update.isRetrying = false;
  }

  // Carry payload from backend events (AFTER auto-clear, so payload wins)
  if (payload?.error !== undefined) update.error = payload.error;
  if (payload?.isRetrying !== undefined) update.isRetrying = payload.isRetrying;
  if (payload?.networkAvailable !== undefined) update.networkAvailable = payload.networkAvailable;
  if (payload?.initialization !== undefined) update.initialization = payload.initialization;

  console.debug('[VPNMachine] dispatch: ' + currentState + ' + ' + event + ' → ' + nextState);
  useVPNMachineStore.setState(update);
}

// ============ Backend Status → Event Mapping ============

export function backendStatusToEvent(status: StatusResponseData): VPNEvent {
  let event: VPNEvent;
  switch (status.state) {
    case 'connected':     event = 'BACKEND_CONNECTED'; break;
    case 'disconnected':  event = 'BACKEND_DISCONNECTED'; break;
    case 'connecting':
    case 'reconnecting':  event = 'BACKEND_RECONNECTING'; break;
    // Mobile-only: engine tore down wire connections for memory conservation
    // but the tunnel is still alive and will resume on its own — treat like
    // reconnecting, not disconnected. See ServiceState 'paused' doc comment.
    case 'paused':         event = 'BACKEND_RECONNECTING'; break;
    case 'error':         event = 'BACKEND_ERROR'; break;
    case 'disconnecting': event = 'BACKEND_DISCONNECTED'; break;
    default:              event = 'BACKEND_DISCONNECTED'; break;
  }
  console.debug('[VPNMachine] statusToEvent: ' + status.state + ' → ' + event);
  return event;
}

// ============ Status Application (staleness-guarded) ============

/**
 * Snapshot the machine's ordering token. Take this *before* issuing an async
 * status request, then pass it to `applyStatus()` when the response lands.
 */
export function currentRevision(): number {
  return useVPNMachineStore.getState().revision;
}

/**
 * Apply a backend status snapshot to the machine.
 *
 * `issuedAtRevision` is the value `currentRevision()` returned when the request
 * that produced `status` was *issued*. If the machine advanced since then, this
 * snapshot describes a superseded past and is dropped whole — including
 * `startAt`, which would otherwise resurrect a finished session's start time.
 *
 * Push deliveries (SSE / native events) pass no revision: they carry no
 * in-flight window, so there is nothing to be stale against.
 */
export function applyStatus(status: StatusResponseData, issuedAtRevision?: number): void {
  if (issuedAtRevision !== undefined) {
    const now = currentRevision();
    if (issuedAtRevision !== now) {
      console.warn('[VPNMachine] applyStatus: stale response discarded (state=' + status.state
        + ', issuedAtRevision=' + issuedAtRevision + ', current=' + now
        + ', machineState=' + useVPNMachineStore.getState().state + ')');
      return;
    }
  }
  // Forward engine's session start. Engine emits startAt only when state=connected;
  // when omitted, clear local copy so consumers fall back to webapp-local timestamps.
  useVPNMachineStore.setState({ startAt: status.startAt ?? null });
  const event = backendStatusToEvent(status);
  dispatch(event, {
    error: status.error ?? null,
    isRetrying: status.retrying ?? false,
    networkAvailable: status.networkAvailable ?? true,
    initialization: status.initialization ?? null,
  });
}

// ============ Initialization ============

export function initializeVPNMachine(): () => void {
  // Event-driven mode (desktop SSE or mobile native events)
  if (window._k2?.onServiceStateChange && window._k2?.onStatusChange) {
    console.info('[VPNMachine] Initializing in event-driven mode');
    const unsubService = window._k2.onServiceStateChange((available) => {
      console.debug('[VPNMachine] SSE service state: available=' + available);
      dispatch(available ? 'SERVICE_REACHABLE' : 'SERVICE_UNREACHABLE');
    });

    const unsubStatus = window._k2.onStatusChange((status) => {
      console.debug('[VPNMachine] SSE status event: state=' + status.state + ', error=' + (status.error?.code ?? 'none') + ', retrying=' + (status.retrying ?? false));
      applyStatus(status);
    });

    // Bridge initial gap — one-time status query. Revision-guarded like the poll:
    // a user can hit connect before this in-flight query resolves.
    const initialRevision = currentRevision();
    window._k2.run('status').then((resp: any) => {
      if (resp.code === 0 && resp.data) {
        console.debug('[VPNMachine] Initial status query: state=' + resp.data.state);
        applyStatus(resp.data, initialRevision);
      }
    }).catch(() => {});

    // Safety-net poll: on iOS, NEVPNStatusDidChange does not fire for engine-level error
    // overlay changes (connected+error ↔ connected). This poll is the only reliable path
    // to recover from a stale reconnecting state on iOS. Silent failures are intentional —
    // this is not a health probe; NE push events remain the primary delivery mechanism.
    const safetyNetInterval = setInterval(async () => {
      // Snapshot BEFORE issuing: the response races push events delivered while
      // it is in flight, and losing that race must not rewind the machine.
      const issuedAtRevision = currentRevision();
      try {
        const resp = await window._k2.run('status') as any;
        if (resp.code === 0 && resp.data) {
          applyStatus(resp.data, issuedAtRevision);
        }
        // resp.code !== 0: silent. Not a service health failure — handled by onServiceStateChange.
      } catch {
        // Silent: poll is eventual-consistency supplement, not primary health probe.
      }
    }, 15_000);

    return () => {
      unsubService();
      unsubStatus();
      clearInterval(safetyNetInterval);
      clearReconnectDebounce();
      // Bump revision: teardown is a state change, and any status request still
      // in flight must not apply to the reset machine.
      useVPNMachineStore.setState({ state: 'idle', error: null, isRetrying: false, networkAvailable: true, initialization: null, revision: currentRevision() + 1 });
    };
  }

  // Polling fallback (standalone/web)
  console.info('[VPNMachine] Initializing in polling mode (2s)');
  const poll = async () => {
    const issuedAtRevision = currentRevision();
    try {
      const resp = await window._k2.run('status') as any;
      if (resp.code === 0 && resp.data) {
        console.debug('[VPNMachine] Poll result: state=' + resp.data.state + ', error=' + (resp.data.error?.code ?? 'none'));
        applyStatus(resp.data, issuedAtRevision);
        // Reachability is deliberately NOT revision-guarded: a response arriving
        // *now* is fresh evidence the bridge is up, however stale its payload is.
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

  return () => {
    clearInterval(interval);
    clearReconnectDebounce();
    // Bump revision: teardown is a state change, and any status request still
    // in flight must not apply to the reset machine.
    useVPNMachineStore.setState({ state: 'idle', error: null, isRetrying: false, networkAvailable: true, initialization: null, revision: currentRevision() + 1 });
  };
}

// ============ Hook ============

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
    isConnected: state === 'connected',
    isDisconnected: state === 'idle',
    isServiceDown: state === 'serviceDown',
    isTransitioning: state === 'connecting' || state === 'reconnecting' || state === 'disconnecting',
    isInteractive: state === 'connected' || state === 'connecting' || state === 'reconnecting',
  };
}
