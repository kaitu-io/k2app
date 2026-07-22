import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';

vi.mock('../../services/router-service', () => ({
  probeRouter: vi.fn(),
  routerCore: vi.fn(),
  getControlKey: vi.fn(),
  saveLastRouter: vi.fn(),
  loadLastRouter: vi.fn(),
  clearLastRouter: vi.fn(),
  ROUTER_ANCHOR: 'http://10.17.79.1:1779',
}));
vi.mock('../../services/private-node-service', () => ({
  mintGatewayCredential: vi.fn(),
}));

import * as svc from '../../services/router-service';
import { mintGatewayCredential } from '../../services/private-node-service';
import { isRouterTakeover } from '../router.store';

const R1 = { name: 'r1', version: '0.4.7', configured: true };

async function freshStore() {
  vi.resetModules();
  const mod = await import('../router.store');
  return mod.useRouterStore;
}

beforeEach(() => {
  vi.clearAllMocks();
  (svc.probeRouter as any).mockResolvedValue(null);
  (svc.saveLastRouter as any).mockResolvedValue(undefined);
  (svc.routerCore as any).mockResolvedValue({ code: 0 });
});
afterEach(() => vi.useRealTimers());

describe('runDiscovery', () => {
  it('none → online when configured router found', async () => {
    (svc.probeRouter as any).mockResolvedValue(R1);
    const store = await freshStore();
    await store.getState().runDiscovery();
    expect(store.getState().phase).toBe('online');
    expect(svc.saveLastRouter).toHaveBeenCalledWith(R1);
  });
  it('unconfigured router → unconfigured phase', async () => {
    (svc.probeRouter as any).mockResolvedValue({ ...R1, configured: false });
    const store = await freshStore();
    await store.getState().runDiscovery();
    expect(store.getState().phase).toBe('unconfigured');
  });
  it('known router unreachable → offline (not none)', async () => {
    (svc.probeRouter as any).mockResolvedValueOnce(R1).mockResolvedValueOnce(null);
    const store = await freshStore();
    await store.getState().runDiscovery();
    await store.getState().runDiscovery();
    expect(store.getState().phase).toBe('offline');
    expect(store.getState().router).not.toBeNull();
  });
});

describe('runDiscovery feeds status (I3 — Dashboard banner/exclusion independent of the Router-tab poll)', () => {
  it('fetches status once after a successful configured discovery, so isRouterTakeover works without ever opening the Router tab', async () => {
    (svc.probeRouter as any).mockResolvedValue(R1);
    (svc.routerCore as any).mockResolvedValue({ code: 0, data: { state: 'connected' } });
    const store = await freshStore();
    await store.getState().runDiscovery();
    expect(svc.routerCore).toHaveBeenCalledWith('status');
    expect(store.getState().status).toEqual({ state: 'connected' });
    expect(store.getState().phase).toBe('online');
    expect(isRouterTakeover(store.getState())).toBe(true);
  });
  it('does not fetch status for an unconfigured router', async () => {
    (svc.probeRouter as any).mockResolvedValue({ ...R1, configured: false });
    const store = await freshStore();
    await store.getState().runDiscovery();
    expect(svc.routerCore).not.toHaveBeenCalled();
  });
  it('a failed status fetch does not flip phase away from the anchor-probe-confirmed online', async () => {
    (svc.probeRouter as any).mockResolvedValue(R1);
    (svc.routerCore as any).mockRejectedValue(new Error('timeout'));
    const store = await freshStore();
    await store.getState().runDiscovery();
    expect(store.getState().phase).toBe('online');
    expect(store.getState().status).toBeNull();
  });
  it('a 401 status fetch does not flip phase away from online', async () => {
    (svc.probeRouter as any).mockResolvedValue(R1);
    (svc.routerCore as any).mockResolvedValue({ code: 401 });
    const store = await freshStore();
    await store.getState().runDiscovery();
    expect(store.getState().phase).toBe('online');
    expect(store.getState().status).toBeNull();
  });
});

describe('setupRouter', () => {
  it('mint + key + set-credential happy path', async () => {
    (svc.probeRouter as any).mockResolvedValue({ ...R1, configured: false });
    (mintGatewayCredential as any).mockResolvedValue('k2subs://u:t@c.example.com/api/subs');
    (svc.getControlKey as any).mockResolvedValue('rck_1');
    const store = await freshStore();
    await store.getState().runDiscovery();
    expect(await store.getState().setupRouter()).toBe(true);
    expect(svc.routerCore).toHaveBeenCalledWith(
      'set-credential',
      { url: 'k2subs://u:t@c.example.com/api/subs', controlKey: 'rck_1' },
    );
    expect(store.getState().phase).toBe('online');
  });
  it('mint failure sets setupError', async () => {
    (svc.probeRouter as any).mockResolvedValue({ ...R1, configured: false });
    (mintGatewayCredential as any).mockResolvedValue('');
    const store = await freshStore();
    await store.getState().runDiscovery();
    expect(await store.getState().setupRouter()).toBe(false);
    expect(store.getState().setupError).toBe('mint_failed');
  });
});

describe('polling', () => {
  it('startPolling refreshes status and flips offline on failure', async () => {
    vi.useFakeTimers();
    (svc.probeRouter as any).mockResolvedValue(R1);
    (svc.routerCore as any).mockResolvedValue({ code: 0, data: { state: 'connected' } });
    const store = await freshStore();
    await store.getState().runDiscovery();
    store.getState().startPolling();
    await vi.advanceTimersByTimeAsync(2100);
    expect(store.getState().status?.state).toBe('connected');
    (svc.routerCore as any).mockRejectedValue(new Error('unreachable'));
    await vi.advanceTimersByTimeAsync(2100);
    expect(store.getState().phase).toBe('offline');
    store.getState().stopPolling();
  });
});

describe('unbindRouter', () => {
  it('reset + clear cache + back to none', async () => {
    (svc.probeRouter as any).mockResolvedValue(R1);
    const store = await freshStore();
    await store.getState().runDiscovery();
    expect(await store.getState().unbindRouter()).toBe(true);
    expect(svc.routerCore).toHaveBeenCalledWith('reset', undefined);
    expect(svc.clearLastRouter).toHaveBeenCalled();
    expect(store.getState().phase).toBe('none');
  });
});

describe('poll/unbind resurrection race', () => {
  it('does not resurrect phase when unbindRouter completes while a status poll is in flight', async () => {
    (svc.probeRouter as any).mockResolvedValue(R1);
    const store = await freshStore();
    await store.getState().runDiscovery();
    expect(store.getState().phase).toBe('online');

    // Poll's status call hangs; unbindRouter's reset call still resolves via
    // the default beforeEach mock ({code:0}), since it's a separate call.
    let resolveStatus: (v: unknown) => void = () => {};
    const pendingStatus = new Promise((resolve) => {
      resolveStatus = resolve;
    });
    (svc.routerCore as any).mockImplementationOnce(() => pendingStatus);

    store.getState().startPolling();
    // startPolling's immediate poll() runs synchronously up to the await,
    // so routerCore('status') has already been called (and is now pending).

    expect(await store.getState().unbindRouter()).toBe(true);
    expect(store.getState().phase).toBe('none');
    expect(store.getState().router).toBeNull();

    // Now let the stale in-flight status response resolve — it must NOT
    // resurrect phase/status after the router was unbound out from under it.
    resolveStatus({ code: 0, data: { state: 'connected' } });
    await Promise.resolve();
    await Promise.resolve();

    expect(store.getState().phase).toBe('none');
    expect(store.getState().status).toBeNull();

    store.getState().stopPolling();
  });
});

describe('isRouterTakeover', () => {
  it('true when phase online and status connected', () => {
    expect(isRouterTakeover({ phase: 'online', status: { state: 'connected' } })).toBe(true);
  });
  it('false when phase is offline', () => {
    expect(isRouterTakeover({ phase: 'offline', status: { state: 'connected' } })).toBe(false);
  });
  it('false when status is null', () => {
    expect(isRouterTakeover({ phase: 'online', status: null })).toBe(false);
  });
  it('false when status.state is not connected', () => {
    expect(isRouterTakeover({ phase: 'online', status: { state: 'disconnected' } })).toBe(false);
  });
});

describe('enterprise slots selectors', () => {
  it('routerSlots returns slots array when status carries them, hasSlotAlarm on failClosed', async () => {
    const { routerSlots, hasSlotAlarm } = await import('../router.store');
    const s = {
      status: {
        state: 'connected',
        slots: [
          { slot: 1, ssid: 'line-ae-1', country: 'ae', index: 1, state: 'running' },
          { slot: 2, ssid: '', country: 'gn', index: 1, state: 'failClosed', downSince: '2026-07-22T02:00:00Z' },
        ],
      },
    } as any;
    expect(routerSlots(s)).toHaveLength(2);
    expect(hasSlotAlarm(s)).toBe(true);
  });

  it('routerSlots null for consumer-mode status / empty slots / null status', async () => {
    const { routerSlots, hasSlotAlarm } = await import('../router.store');
    expect(routerSlots({ status: { state: 'connected' } } as any)).toBeNull();
    expect(routerSlots({ status: { state: 'connected', slots: [] } } as any)).toBeNull();
    expect(routerSlots({ status: null } as any)).toBeNull();
    expect(hasSlotAlarm({ status: { state: 'connected' } } as any)).toBe(false);
  });

  it('hasSlotAlarm false when all slots running/disabled', async () => {
    const { routerSlots, hasSlotAlarm } = await import('../router.store');
    const s = {
      status: {
        state: 'connected',
        slots: [
          { slot: 1, ssid: 'a', country: 'ae', index: 1, state: 'running' },
          { slot: 2, ssid: '', country: '', index: 0, state: 'disabled' },
        ],
      },
    } as any;
    expect(routerSlots(s)).toHaveLength(2);
    expect(hasSlotAlarm(s)).toBe(false);
  });
});

describe('poll 401 → unauthorized flag', () => {
  it('sets unauthorized on 401 and clears it on a later 0-code status', async () => {
    vi.useFakeTimers();
    (svc.probeRouter as any).mockResolvedValue(R1);
    const store = await freshStore();
    await store.getState().runDiscovery();

    (svc.routerCore as any).mockResolvedValue({ code: 401, message: 'unauthorized' });
    store.getState().startPolling();
    await vi.advanceTimersByTimeAsync(0);
    expect(store.getState().unauthorized).toBe(true);
    expect(store.getState().status).toBeNull();

    (svc.routerCore as any).mockResolvedValue({ code: 0, data: { state: 'connected' } });
    await vi.advanceTimersByTimeAsync(2000);
    expect(store.getState().unauthorized).toBe(false);
    expect(store.getState().status).toEqual({ state: 'connected' });

    store.getState().stopPolling();
  });
});
