/**
 * Funnel: connect events. first_connect_ok is reported from the VPN-state
 * subscriber in stores/index.ts; first_connect_attempt from connection.store.
 * Dedup ("once per install") lives in statsService, so here we only pin that the
 * Once variant is what gets called.
 *
 * Run: cd webapp && npx vitest run src/stores/__tests__/funnel-connect.test.ts
 */
import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest';

const trackFunnel = vi.fn();
const trackFunnelOnce = vi.fn();
const trackFunnelDaily = vi.fn();

vi.mock('../../services/stats', () => ({
  statsService: {
    trackFunnel: (...a: unknown[]) => trackFunnel(...a),
    trackFunnelOnce: (...a: unknown[]) => trackFunnelOnce(...a),
    trackFunnelDaily: (...a: unknown[]) => trackFunnelDaily(...a),
    trackAppOpen: vi.fn(),
    trackConnect: vi.fn(),
    trackDisconnect: vi.fn(),
  },
}));

const flush = () => new Promise((r) => setTimeout(r, 0));

describe('funnel: first_connect_ok', () => {
  let cleanup: (() => void) | undefined;

  beforeEach(() => {
    vi.resetModules();
    trackFunnel.mockReset();
    trackFunnelOnce.mockReset();
    trackFunnelDaily.mockReset();
    const store = new Map<string, unknown>();
    (window as any)._platform = {
      os: 'macos', isDesktop: true, isMobile: false, version: '0.4.0',
      storage: {
        get: vi.fn(async (k: string) => store.get(k) ?? null),
        set: vi.fn(async (k: string, v: unknown) => { store.set(k, v); }),
        remove: vi.fn(), has: vi.fn(), clear: vi.fn(), keys: vi.fn(async () => []),
      },
    };
  });

  afterEach(() => {
    cleanup?.();
    cleanup = undefined;
    delete (window as any)._platform;
  });

  async function boot() {
    const stores = await import('../index');
    const machine = await import('../vpn-machine.store');
    cleanup = stores.initializeAllStores();
    await flush(); // the stats subscriber is attached after a dynamic import
    // Init probes a missing daemon and parks the machine in serviceDown; start from idle.
    machine.useVPNMachineStore.setState({ state: 'idle' });
    return machine;
  }

  it('→ connected reports first_connect_ok via trackFunnelOnce (not trackFunnel)', async () => {
    const { dispatch, useVPNMachineStore } = await boot();
    dispatch('BACKEND_CONNECTED');
    expect(useVPNMachineStore.getState().state).toBe('connected');
    expect(trackFunnelOnce).toHaveBeenCalledWith('first_connect_ok');
    expect(trackFunnel).not.toHaveBeenCalledWith('first_connect_ok');
  });

  it('a later reconnecting → connected still goes through trackFunnelOnce only', async () => {
    const { dispatch, useVPNMachineStore } = await boot();
    dispatch('BACKEND_CONNECTED');
    expect(useVPNMachineStore.getState().state).toBe('connected');
    // reconnect is debounced; force the state directly to exercise the subscriber
    useVPNMachineStore.setState({ state: 'reconnecting' });
    trackFunnelOnce.mockClear();
    dispatch('BACKEND_CONNECTED');
    expect(trackFunnelOnce).toHaveBeenCalledWith('first_connect_ok');
    expect(trackFunnel).not.toHaveBeenCalledWith('first_connect_ok');
  });

  it('does not report first_connect_ok for non-connected transitions', async () => {
    const { dispatch, useVPNMachineStore } = await boot();
    dispatch('USER_CONNECT'); // idle → connecting
    expect(useVPNMachineStore.getState().state).toBe('connecting');
    expect(trackFunnelOnce).not.toHaveBeenCalledWith('first_connect_ok');
    expect(trackFunnelDaily).not.toHaveBeenCalled();
  });

  // connect_ok is the repeatable action event; per-UTC-day dedup lives in
  // statsService.trackFunnelDaily, so every → connected edge must go through it.
  it('→ connected reports connect_ok via trackFunnelDaily on every edge', async () => {
    const { dispatch, useVPNMachineStore } = await boot();
    dispatch('BACKEND_CONNECTED');
    expect(trackFunnelDaily).toHaveBeenCalledTimes(1);
    expect(trackFunnelDaily).toHaveBeenCalledWith('connect_ok');
    expect(trackFunnel).not.toHaveBeenCalledWith('connect_ok');
    expect(trackFunnelOnce).not.toHaveBeenCalledWith('connect_ok');

    useVPNMachineStore.setState({ state: 'reconnecting' });
    dispatch('BACKEND_CONNECTED');
    expect(trackFunnelDaily).toHaveBeenCalledTimes(2);
  });
});
