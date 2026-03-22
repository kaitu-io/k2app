/**
 * VPN Machine Store Tests
 *
 * Tests the explicit state machine transition table.
 * Each test case maps to a row in the TRANSITIONS table.
 *
 * Run: cd webapp && npx vitest run src/stores/__tests__/vpn-machine.store.test.ts
 */

import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest';
import { act } from '@testing-library/react';

// Reset module between tests to get fresh store instances
beforeEach(() => {
  vi.useFakeTimers();
  vi.resetModules();
});

afterEach(() => {
  vi.useRealTimers();
  vi.restoreAllMocks();
});

async function getStore() {
  const mod = await import('../vpn-machine.store');
  return mod;
}

// ==================== Transition Table Tests ====================

describe('VPN Machine Transitions', () => {
  describe('idle state', () => {
    it('USER_CONNECT → connecting', async () => {
      const { useVPNMachineStore, dispatch } = await getStore();
      expect(useVPNMachineStore.getState().state).toBe('idle');

      dispatch('USER_CONNECT');
      expect(useVPNMachineStore.getState().state).toBe('connecting');
    });

    it('BACKEND_CONNECTED → connected (app restart sync)', async () => {
      const { useVPNMachineStore, dispatch } = await getStore();

      dispatch('BACKEND_CONNECTED');
      expect(useVPNMachineStore.getState().state).toBe('connected');
    });

    it('BACKEND_ERROR → idle with error (app restart sync)', async () => {
      const { useVPNMachineStore, dispatch } = await getStore();

      dispatch('BACKEND_ERROR', { error: { code: 503, message: 'unreachable' } });
      expect(useVPNMachineStore.getState().state).toBe('idle');
      expect(useVPNMachineStore.getState().error).toEqual({ code: 503, message: 'unreachable' });
    });

    it('BACKEND_RECONNECTING → reconnecting (app restart sync)', async () => {
      const { useVPNMachineStore, dispatch } = await getStore();

      dispatch('BACKEND_RECONNECTING');
      expect(useVPNMachineStore.getState().state).toBe('reconnecting');
    });

    it('SERVICE_UNREACHABLE → serviceDown', async () => {
      const { useVPNMachineStore, dispatch } = await getStore();

      dispatch('SERVICE_UNREACHABLE');
      expect(useVPNMachineStore.getState().state).toBe('serviceDown');
    });

    it('BACKEND_DISCONNECTED in idle → no-op', async () => {
      const { useVPNMachineStore, dispatch } = await getStore();

      dispatch('BACKEND_DISCONNECTED');
      expect(useVPNMachineStore.getState().state).toBe('idle');
    });
  });

  describe('connecting state', () => {
    async function enterConnecting() {
      const mod = await getStore();
      mod.dispatch('USER_CONNECT');
      expect(mod.useVPNMachineStore.getState().state).toBe('connecting');
      return mod;
    }

    it('BACKEND_CONNECTED → connected', async () => {
      const { useVPNMachineStore, dispatch } = await enterConnecting();

      dispatch('BACKEND_CONNECTED');
      expect(useVPNMachineStore.getState().state).toBe('connected');
    });

    it('BACKEND_DISCONNECTED → idle', async () => {
      const { useVPNMachineStore, dispatch } = await enterConnecting();

      dispatch('BACKEND_DISCONNECTED');
      expect(useVPNMachineStore.getState().state).toBe('idle');
    });

    it('BACKEND_ERROR → idle', async () => {
      const { useVPNMachineStore, dispatch } = await enterConnecting();

      dispatch('BACKEND_ERROR', { error: { code: 401, message: 'auth failed' } });
      expect(useVPNMachineStore.getState().state).toBe('idle');
    });

    it('SERVICE_UNREACHABLE → serviceDown', async () => {
      const { useVPNMachineStore, dispatch } = await enterConnecting();

      dispatch('SERVICE_UNREACHABLE');
      expect(useVPNMachineStore.getState().state).toBe('serviceDown');
    });

    it('BACKEND_RECONNECTING in connecting → ignored (iOS NWPathMonitor)', async () => {
      const { useVPNMachineStore, dispatch } = await enterConnecting();

      dispatch('BACKEND_RECONNECTING');
      expect(useVPNMachineStore.getState().state).toBe('connecting');
    });

    it('USER_CONNECT in connecting → ignored (double-click)', async () => {
      const { useVPNMachineStore, dispatch } = await enterConnecting();

      dispatch('USER_CONNECT');
      expect(useVPNMachineStore.getState().state).toBe('connecting');
    });
  });

  describe('connected state', () => {
    async function enterConnected() {
      const mod = await getStore();
      mod.dispatch('USER_CONNECT');
      mod.dispatch('BACKEND_CONNECTED');
      expect(mod.useVPNMachineStore.getState().state).toBe('connected');
      return mod;
    }

    it('USER_DISCONNECT → disconnecting', async () => {
      const { useVPNMachineStore, dispatch } = await enterConnected();

      dispatch('USER_DISCONNECT');
      expect(useVPNMachineStore.getState().state).toBe('disconnecting');
    });

    it('BACKEND_DISCONNECTED → idle', async () => {
      const { useVPNMachineStore, dispatch } = await enterConnected();

      dispatch('BACKEND_DISCONNECTED');
      expect(useVPNMachineStore.getState().state).toBe('idle');
    });

    it('BACKEND_ERROR → idle', async () => {
      const { useVPNMachineStore, dispatch } = await enterConnected();

      dispatch('BACKEND_ERROR', { error: { code: 570, message: 'fatal' } });
      expect(useVPNMachineStore.getState().state).toBe('idle');
    });

    it('SERVICE_UNREACHABLE → serviceDown', async () => {
      const { useVPNMachineStore, dispatch } = await enterConnected();

      dispatch('SERVICE_UNREACHABLE');
      expect(useVPNMachineStore.getState().state).toBe('serviceDown');
    });

    it('BACKEND_RECONNECTING → debounced (stays connected for 3s)', async () => {
      const { useVPNMachineStore, dispatch } = await enterConnected();

      dispatch('BACKEND_RECONNECTING');
      // Should still be connected (debounce pending)
      expect(useVPNMachineStore.getState().state).toBe('connected');

      // After 3s, transitions to reconnecting
      act(() => { vi.advanceTimersByTime(3000); });
      expect(useVPNMachineStore.getState().state).toBe('reconnecting');
    });

    it('BACKEND_RECONNECTING then BACKEND_CONNECTED within 3s → stays connected', async () => {
      const { useVPNMachineStore, dispatch } = await enterConnected();

      dispatch('BACKEND_RECONNECTING');
      expect(useVPNMachineStore.getState().state).toBe('connected');

      // Backend recovers within debounce window
      act(() => { vi.advanceTimersByTime(1000); });
      dispatch('BACKEND_CONNECTED');
      expect(useVPNMachineStore.getState().state).toBe('connected');

      // After full 3s, still connected (timer was cancelled)
      act(() => { vi.advanceTimersByTime(3000); });
      expect(useVPNMachineStore.getState().state).toBe('connected');
    });
  });

  describe('reconnecting state', () => {
    async function enterReconnecting() {
      const mod = await getStore();
      mod.dispatch('USER_CONNECT');
      mod.dispatch('BACKEND_CONNECTED');
      mod.dispatch('BACKEND_RECONNECTING');
      act(() => { vi.advanceTimersByTime(3000); }); // let debounce fire
      expect(mod.useVPNMachineStore.getState().state).toBe('reconnecting');
      return mod;
    }

    it('BACKEND_CONNECTED → connected', async () => {
      const { useVPNMachineStore, dispatch } = await enterReconnecting();

      dispatch('BACKEND_CONNECTED');
      expect(useVPNMachineStore.getState().state).toBe('connected');
    });

    it('BACKEND_DISCONNECTED → idle', async () => {
      const { useVPNMachineStore, dispatch } = await enterReconnecting();

      dispatch('BACKEND_DISCONNECTED');
      expect(useVPNMachineStore.getState().state).toBe('idle');
    });

    it('BACKEND_ERROR → idle', async () => {
      const { useVPNMachineStore, dispatch } = await enterReconnecting();

      dispatch('BACKEND_ERROR');
      expect(useVPNMachineStore.getState().state).toBe('idle');
    });

    it('USER_DISCONNECT → disconnecting', async () => {
      const { useVPNMachineStore, dispatch } = await enterReconnecting();

      dispatch('USER_DISCONNECT');
      expect(useVPNMachineStore.getState().state).toBe('disconnecting');
    });

    it('SERVICE_UNREACHABLE → serviceDown', async () => {
      const { useVPNMachineStore, dispatch } = await enterReconnecting();

      dispatch('SERVICE_UNREACHABLE');
      expect(useVPNMachineStore.getState().state).toBe('serviceDown');
    });
  });

  describe('disconnecting state', () => {
    async function enterDisconnecting() {
      const mod = await getStore();
      mod.dispatch('USER_CONNECT');
      mod.dispatch('BACKEND_CONNECTED');
      mod.dispatch('USER_DISCONNECT');
      expect(mod.useVPNMachineStore.getState().state).toBe('disconnecting');
      return mod;
    }

    it('BACKEND_DISCONNECTED → idle', async () => {
      const { useVPNMachineStore, dispatch } = await enterDisconnecting();

      dispatch('BACKEND_DISCONNECTED');
      expect(useVPNMachineStore.getState().state).toBe('idle');
    });

    it('BACKEND_CONNECTED → disconnecting (abort: stay disconnecting until backend confirms down)', async () => {
      const { useVPNMachineStore, dispatch } = await enterDisconnecting();

      dispatch('BACKEND_CONNECTED');
      expect(useVPNMachineStore.getState().state).toBe('disconnecting');
    });

    it('BACKEND_ERROR → idle (error during shutdown)', async () => {
      const { useVPNMachineStore, dispatch } = await enterDisconnecting();

      dispatch('BACKEND_ERROR');
      expect(useVPNMachineStore.getState().state).toBe('idle');
    });

    it('SERVICE_UNREACHABLE → serviceDown (daemon crash mid-disconnect)', async () => {
      const { useVPNMachineStore, dispatch } = await enterDisconnecting();

      dispatch('SERVICE_UNREACHABLE');
      expect(useVPNMachineStore.getState().state).toBe('serviceDown');
    });
  });

  describe('idle with error (replaces error state)', () => {
    async function enterIdleWithError() {
      const mod = await getStore();
      mod.dispatch('USER_CONNECT');
      mod.dispatch('BACKEND_ERROR', { error: { code: 503, message: 'unreachable' } });
      expect(mod.useVPNMachineStore.getState().state).toBe('idle');
      expect(mod.useVPNMachineStore.getState().error).not.toBeNull();
      return mod;
    }

    it('USER_CONNECT → connecting (retry from idle with error)', async () => {
      const { useVPNMachineStore, dispatch } = await enterIdleWithError();

      dispatch('USER_CONNECT');
      expect(useVPNMachineStore.getState().state).toBe('connecting');
    });

    it('BACKEND_CONNECTED → connected (auto-retry succeeded)', async () => {
      const { useVPNMachineStore, dispatch } = await enterIdleWithError();

      dispatch('BACKEND_CONNECTED');
      expect(useVPNMachineStore.getState().state).toBe('connected');
      expect(useVPNMachineStore.getState().error).toBeNull();
    });

    it('BACKEND_DISCONNECTED clears error via payload', async () => {
      const { useVPNMachineStore, dispatch } = await enterIdleWithError();

      dispatch('BACKEND_DISCONNECTED', { error: null });
      expect(useVPNMachineStore.getState().state).toBe('idle');
      expect(useVPNMachineStore.getState().error).toBeNull();
    });

    it('SERVICE_UNREACHABLE → serviceDown', async () => {
      const { useVPNMachineStore, dispatch } = await enterIdleWithError();

      dispatch('SERVICE_UNREACHABLE');
      expect(useVPNMachineStore.getState().state).toBe('serviceDown');
    });
  });

  describe('BACKEND_ERROR + isRetrying override', () => {
    it('idle + BACKEND_ERROR + isRetrying → reconnecting', async () => {
      const { useVPNMachineStore, dispatch } = await getStore();

      dispatch('BACKEND_ERROR', {
        error: { code: 503, message: 'unreachable' },
        isRetrying: true,
      });
      expect(useVPNMachineStore.getState().state).toBe('reconnecting');
      expect(useVPNMachineStore.getState().error).toEqual({ code: 503, message: 'unreachable' });
      expect(useVPNMachineStore.getState().isRetrying).toBe(true);
    });

    it('connecting + BACKEND_ERROR + isRetrying → reconnecting', async () => {
      const mod = await getStore();
      mod.dispatch('USER_CONNECT');
      mod.dispatch('BACKEND_ERROR', { error: { code: 503, message: 'fail' }, isRetrying: true });
      expect(mod.useVPNMachineStore.getState().state).toBe('reconnecting');
    });

    it('connected + BACKEND_ERROR + isRetrying → reconnecting', async () => {
      const mod = await getStore();
      mod.dispatch('USER_CONNECT');
      mod.dispatch('BACKEND_CONNECTED');
      mod.dispatch('BACKEND_ERROR', { error: { code: 503, message: 'fail' }, isRetrying: true });
      expect(mod.useVPNMachineStore.getState().state).toBe('reconnecting');
    });

    it('reconnecting + BACKEND_ERROR + isRetrying → stays reconnecting', async () => {
      const mod = await getStore();
      mod.dispatch('USER_CONNECT');
      mod.dispatch('BACKEND_CONNECTED');
      mod.dispatch('BACKEND_RECONNECTING');
      act(() => { vi.advanceTimersByTime(3000); });
      expect(mod.useVPNMachineStore.getState().state).toBe('reconnecting');
      mod.dispatch('BACKEND_ERROR', { isRetrying: true });
      expect(mod.useVPNMachineStore.getState().state).toBe('reconnecting');
    });

    it('disconnecting + BACKEND_ERROR + isRetrying → idle (honor disconnect)', async () => {
      const mod = await getStore();
      mod.dispatch('USER_CONNECT');
      mod.dispatch('BACKEND_CONNECTED');
      mod.dispatch('USER_DISCONNECT');
      expect(mod.useVPNMachineStore.getState().state).toBe('disconnecting');
      mod.dispatch('BACKEND_ERROR', { isRetrying: true });
      expect(mod.useVPNMachineStore.getState().state).toBe('idle');
    });

    it('BACKEND_ERROR + isRetrying=false → idle (negative case)', async () => {
      const mod = await getStore();
      mod.dispatch('USER_CONNECT');
      mod.dispatch('BACKEND_ERROR', { error: { code: 401, message: 'auth' }, isRetrying: false });
      expect(mod.useVPNMachineStore.getState().state).toBe('idle');
      expect(mod.useVPNMachineStore.getState().error).toEqual({ code: 401, message: 'auth' });
    });
  });

  describe('serviceDown state', () => {
    async function enterServiceDown() {
      const mod = await getStore();
      mod.dispatch('SERVICE_UNREACHABLE');
      expect(mod.useVPNMachineStore.getState().state).toBe('serviceDown');
      return mod;
    }

    it('SERVICE_REACHABLE → idle', async () => {
      const { useVPNMachineStore, dispatch } = await enterServiceDown();

      dispatch('SERVICE_REACHABLE');
      expect(useVPNMachineStore.getState().state).toBe('idle');
    });

    it('all other events ignored in serviceDown', async () => {
      const { useVPNMachineStore, dispatch } = await enterServiceDown();

      const events = [
        'USER_CONNECT', 'USER_DISCONNECT',
        'BACKEND_CONNECTED', 'BACKEND_DISCONNECTED',
        'BACKEND_ERROR', 'BACKEND_RECONNECTING',
      ] as const;

      for (const event of events) {
        dispatch(event);
        expect(useVPNMachineStore.getState().state).toBe('serviceDown');
      }
    });
  });
});

// ==================== Payload Tests ====================

describe('VPN Machine Payload', () => {
  it('BACKEND_ERROR carries error data', async () => {
    const { useVPNMachineStore, dispatch } = await getStore();

    dispatch('BACKEND_ERROR', {
      error: { code: 401, message: 'Unauthorized' },
      isRetrying: false,
      networkAvailable: true,
    });

    const state = useVPNMachineStore.getState();
    expect(state.state).toBe('idle');
    expect(state.error).toEqual({ code: 401, message: 'Unauthorized' });
    expect(state.isRetrying).toBe(false);
    expect(state.networkAvailable).toBe(true);
  });

  it('BACKEND_CONNECTED clears error', async () => {
    const { useVPNMachineStore, dispatch } = await getStore();

    dispatch('BACKEND_ERROR', { error: { code: 503, message: 'down' } });
    expect(useVPNMachineStore.getState().error).not.toBeNull();

    dispatch('BACKEND_CONNECTED');
    expect(useVPNMachineStore.getState().error).toBeNull();
    expect(useVPNMachineStore.getState().state).toBe('connected');
  });

  it('transition to idle clears error', async () => {
    const { useVPNMachineStore, dispatch } = await getStore();

    dispatch('BACKEND_ERROR', { error: { code: 503, message: 'down' } });
    dispatch('BACKEND_DISCONNECTED');

    expect(useVPNMachineStore.getState().state).toBe('idle');
    expect(useVPNMachineStore.getState().error).toBeNull();
  });

  it('networkAvailable and isRetrying update on backend events', async () => {
    const { useVPNMachineStore, dispatch } = await getStore();

    dispatch('BACKEND_ERROR', {
      error: { code: 570, message: 'fatal' },
      isRetrying: true,
      networkAvailable: false,
    });

    expect(useVPNMachineStore.getState().state).toBe('reconnecting');
    expect(useVPNMachineStore.getState().isRetrying).toBe(true);
    expect(useVPNMachineStore.getState().networkAvailable).toBe(false);
  });
});

// ==================== Error Lifecycle Tests ====================

describe('Error lifecycle', () => {
  it('error persists in idle until new connect succeeds', async () => {
    const { useVPNMachineStore, dispatch } = await getStore();
    dispatch('BACKEND_ERROR', { error: { code: 503, message: 'fail' } });
    expect(useVPNMachineStore.getState().state).toBe('idle');
    expect(useVPNMachineStore.getState().error).toEqual({ code: 503, message: 'fail' });

    dispatch('USER_CONNECT');
    expect(useVPNMachineStore.getState().error).toEqual({ code: 503, message: 'fail' });

    dispatch('BACKEND_CONNECTED');
    expect(useVPNMachineStore.getState().error).toBeNull();
  });

  it('BACKEND_DISCONNECTED with no error clears stale error', async () => {
    const { useVPNMachineStore, dispatch } = await getStore();
    dispatch('BACKEND_ERROR', { error: { code: 503, message: 'fail' } });
    expect(useVPNMachineStore.getState().error).not.toBeNull();

    dispatch('BACKEND_DISCONNECTED', { error: null });
    expect(useVPNMachineStore.getState().state).toBe('idle');
    expect(useVPNMachineStore.getState().error).toBeNull();
  });
});

// ==================== backendStatusToEvent Tests ====================

describe('backendStatusToEvent', () => {
  it('maps status states to events correctly', async () => {
    const { backendStatusToEvent } = await getStore();

    expect(backendStatusToEvent({ state: 'connected' } as any)).toBe('BACKEND_CONNECTED');
    expect(backendStatusToEvent({ state: 'disconnected' } as any)).toBe('BACKEND_DISCONNECTED');
    expect(backendStatusToEvent({ state: 'connecting' } as any)).toBe('BACKEND_RECONNECTING');
    expect(backendStatusToEvent({ state: 'reconnecting' } as any)).toBe('BACKEND_RECONNECTING');
    expect(backendStatusToEvent({ state: 'error' } as any)).toBe('BACKEND_ERROR');
    expect(backendStatusToEvent({ state: 'disconnecting' } as any)).toBe('BACKEND_DISCONNECTED');
  });
});

// ==================== useVPNMachine Hook Tests ====================

describe('useVPNMachine hook', () => {
  it('returns convenience booleans', async () => {
    const { renderHook } = await import('@testing-library/react');
    const { useVPNMachine, dispatch } = await getStore();

    const { result } = renderHook(() => useVPNMachine());

    expect(result.current.isDisconnected).toBe(true);
    expect(result.current.isConnected).toBe(false);
    expect(result.current.isServiceDown).toBe(false);
    expect(result.current.isTransitioning).toBe(false);

    act(() => { dispatch('USER_CONNECT'); });
    expect(result.current.state).toBe('connecting');
    expect(result.current.isTransitioning).toBe(true);

    act(() => { dispatch('BACKEND_CONNECTED'); });
    expect(result.current.isConnected).toBe(true);
    expect(result.current.isDisconnected).toBe(false);
  });

  it('isInteractive is true when connected, connecting, reconnecting, or error+retrying', async () => {
    const { renderHook } = await import('@testing-library/react');
    const { useVPNMachine, useVPNMachineStore, dispatch } = await getStore();

    const { result } = renderHook(() => useVPNMachine());

    // idle → not interactive
    expect(result.current.isInteractive).toBe(false);

    // connecting → interactive
    act(() => { dispatch('USER_CONNECT'); });
    expect(result.current.isInteractive).toBe(true);

    // connected → interactive
    act(() => { dispatch('BACKEND_CONNECTED'); });
    expect(result.current.isInteractive).toBe(true);

    // After connected → dispatch BACKEND_ERROR without retrying → idle
    act(() => { dispatch('BACKEND_ERROR', { isRetrying: false }); });
    expect(result.current.state).toBe('idle');
    expect(result.current.isInteractive).toBe(false);

    // BACKEND_ERROR with retrying → reconnecting → interactive
    act(() => { dispatch('BACKEND_ERROR', { isRetrying: true, error: { code: 503, message: 'x' } }); });
    expect(result.current.state).toBe('reconnecting');
    expect(result.current.isInteractive).toBe(true);
  });
});
