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
