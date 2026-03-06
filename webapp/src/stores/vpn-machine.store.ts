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
  | 'error'
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
}

// ============ Transition Table ============

const TRANSITIONS: Record<VPNState, Partial<Record<VPNEvent, VPNState>>> = {
  idle: {
    USER_CONNECT:         'connecting',
    BACKEND_CONNECTED:    'connected',
    BACKEND_ERROR:        'error',
    BACKEND_RECONNECTING: 'reconnecting',
    SERVICE_UNREACHABLE:  'serviceDown',
  },
  connecting: {
    BACKEND_CONNECTED:    'connected',
    BACKEND_DISCONNECTED: 'idle',
    BACKEND_ERROR:        'error',
    SERVICE_UNREACHABLE:  'serviceDown',
  },
  connected: {
    USER_DISCONNECT:      'disconnecting',
    BACKEND_RECONNECTING: 'reconnecting', // debounced — see dispatch()
    BACKEND_DISCONNECTED: 'idle',
    BACKEND_ERROR:        'error',
    SERVICE_UNREACHABLE:  'serviceDown',
  },
  reconnecting: {
    BACKEND_CONNECTED:    'connected',
    BACKEND_DISCONNECTED: 'idle',
    BACKEND_ERROR:        'error',
    USER_DISCONNECT:      'disconnecting',
    SERVICE_UNREACHABLE:  'serviceDown',
  },
  disconnecting: {
    BACKEND_DISCONNECTED: 'idle',
    BACKEND_CONNECTED:    'connected',
    BACKEND_ERROR:        'idle',
    SERVICE_UNREACHABLE:  'serviceDown',
  },
  error: {
    USER_CONNECT:         'connecting',
    USER_DISCONNECT:      'disconnecting',
    BACKEND_CONNECTED:    'connected',
    BACKEND_DISCONNECTED: 'idle',
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
    clearReconnectDebounce();
  }

  // Special: debounce connected → reconnecting
  if (currentState === 'connected' && event === 'BACKEND_RECONNECTING') {
    if (reconnectDebounceTimer) return; // already debouncing
    reconnectDebounceTimer = setTimeout(() => {
      reconnectDebounceTimer = null;
      if (useVPNMachineStore.getState().state === 'connected') {
        useVPNMachineStore.setState({ state: 'reconnecting' });
      }
    }, RECONNECT_DEBOUNCE_MS);
    return;
  }

  const nextState = TRANSITIONS[currentState]?.[event];
  if (!nextState) return; // invalid transition — silently ignore

  // Build state update
  const update: Partial<VPNMachineState> = { state: nextState };

  // Carry payload from backend events
  if (payload?.error !== undefined) update.error = payload.error;
  if (payload?.isRetrying !== undefined) update.isRetrying = payload.isRetrying;
  if (payload?.networkAvailable !== undefined) update.networkAvailable = payload.networkAvailable;
  if (payload?.initialization !== undefined) update.initialization = payload.initialization;

  // Clear error on transitions to idle or connected
  if (nextState === 'idle' || nextState === 'connected') {
    update.error = null;
    update.isRetrying = false;
  }

  useVPNMachineStore.setState(update);
}

// ============ Backend Status → Event Mapping ============

export function backendStatusToEvent(status: StatusResponseData): VPNEvent {
  switch (status.state) {
    case 'connected':     return 'BACKEND_CONNECTED';
    case 'disconnected':  return 'BACKEND_DISCONNECTED';
    case 'connecting':
    case 'reconnecting':  return 'BACKEND_RECONNECTING';
    case 'error':         return 'BACKEND_ERROR';
    case 'disconnecting': return 'BACKEND_DISCONNECTED';
    default:              return 'BACKEND_DISCONNECTED';
  }
}

// ============ Initialization ============

export function initializeVPNMachine(): () => void {
  const dispatchStatus = (status: StatusResponseData) => {
    const event = backendStatusToEvent(status);
    dispatch(event, {
      error: status.error ?? null,
      isRetrying: status.retrying ?? false,
      networkAvailable: status.networkAvailable ?? true,
      initialization: status.initialization ?? null,
    });
  };

  // Event-driven mode (desktop/mobile with SSE or native events)
  if (window._k2?.onServiceStateChange && window._k2?.onStatusChange) {
    const unsubService = window._k2.onServiceStateChange((available) => {
      dispatch(available ? 'SERVICE_REACHABLE' : 'SERVICE_UNREACHABLE');
    });

    const unsubStatus = window._k2.onStatusChange((status) => {
      dispatchStatus(status);
    });

    // Bridge initial gap — one-time status query
    window._k2.run('status').then((resp: any) => {
      if (resp.code === 0 && resp.data) {
        dispatchStatus(resp.data);
      }
    }).catch(() => {});

    return () => {
      unsubService();
      unsubStatus();
      clearReconnectDebounce();
      useVPNMachineStore.setState({ state: 'idle', error: null, isRetrying: false, networkAvailable: true, initialization: null });
    };
  }

  // Polling fallback (standalone/web)
  const poll = async () => {
    try {
      const resp = await window._k2.run('status') as any;
      if (resp.code === 0 && resp.data) {
        dispatchStatus(resp.data);
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
    useVPNMachineStore.setState({ state: 'idle', error: null, isRetrying: false, networkAvailable: true, initialization: null });
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
    isInteractive: state === 'connected' || state === 'connecting' || state === 'reconnecting' || (state === 'error' && isRetrying),
  };
}
