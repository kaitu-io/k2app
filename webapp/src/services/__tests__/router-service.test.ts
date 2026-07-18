import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';

vi.mock('../cloud-api', () => ({
  cloudApi: { post: vi.fn(), get: vi.fn() },
}));

import { cloudApi } from '../cloud-api';
import { ROUTER_ANCHOR, probeRouter, getControlKey, routerCore } from '../router-service';

const PING_OK = JSON.stringify({ k2r: true, version: '0.4.7', configured: true, name: 'r1' });

const mockStorage = { get: vi.fn(), set: vi.fn(), remove: vi.fn() };
const mockRouterRequest = vi.fn();

beforeEach(() => {
  // vi.clearAllMocks 会清实现——每个 beforeEach 重设
  vi.clearAllMocks();
  (window as any)._platform = {
    routerRequest: mockRouterRequest,
    storage: mockStorage,
  };
  mockStorage.get.mockResolvedValue(null);
  mockStorage.set.mockResolvedValue(undefined);
});
afterEach(() => {
  delete (window as any)._platform;
});

describe('probeRouter (anchor)', () => {
  it('accepts a k2r signature at the anchor URL', async () => {
    mockRouterRequest.mockResolvedValue({ status: 200, body: PING_OK });
    const info = await probeRouter();
    expect(info).toMatchObject({ configured: true, name: 'r1', version: '0.4.7' });
    expect(mockRouterRequest).toHaveBeenCalledWith(
      expect.objectContaining({ url: `${ROUTER_ANCHOR}/ping` }),
    );
  });
  it('rejects non-k2r signature (anchor collided with a real LAN host)', async () => {
    mockRouterRequest.mockResolvedValue({ status: 200, body: '{"hello":1}' });
    expect(await probeRouter()).toBeNull();
  });
  it('returns null on timeout/network error (no k2r on path)', async () => {
    mockRouterRequest.mockRejectedValue(new Error('timeout'));
    expect(await probeRouter()).toBeNull();
  });
});

describe('getControlKey', () => {
  it('prefers cached key', async () => {
    mockStorage.get.mockResolvedValue('rck_cached');
    expect(await getControlKey()).toBe('rck_cached');
    expect(cloudApi.post).not.toHaveBeenCalled();
  });
  it('fetches from center and caches', async () => {
    (cloudApi.post as any).mockResolvedValue({ code: 0, data: { controlKey: 'rck_new' } });
    expect(await getControlKey()).toBe('rck_new');
    expect(mockStorage.set).toHaveBeenCalledWith('k2.router.control_key', 'rck_new');
  });
});

describe('routerCore', () => {
  it('retries once with refreshed key on 401', async () => {
    mockStorage.get.mockResolvedValue('rck_stale');
    (cloudApi.post as any).mockResolvedValue({ code: 0, data: { controlKey: 'rck_fresh' } });
    mockRouterRequest
      .mockResolvedValueOnce({ status: 401, body: '' })
      .mockResolvedValueOnce({ status: 200, body: '{"code":0,"message":"ok"}' });
    const resp = await routerCore('status');
    expect(resp.code).toBe(0);
    expect(mockRouterRequest).toHaveBeenCalledTimes(2);
    const secondCall = mockRouterRequest.mock.calls[1][0];
    expect(secondCall.url).toBe(`${ROUTER_ANCHOR}/api/core`);
    expect(secondCall.headers.Authorization).toBe('Bearer rck_fresh');
  });
});

// I4 fix: routerFetch reacted to every 401 with a force-refresh POST to
// Center (DB conditional-UPDATE + an audit-log row per call). With the 2s
// poll and a configured-but-unbound k2r (legacy-upgrade window, up to
// ~30min), that hammered Center every 2s per parked user. A 60s negative
// cache after a force-refresh that still 401s must suppress further
// force-refreshes without breaking the happy-path single-retry above.
//
// Uses its own freshly re-imported router-service module (vi.resetModules +
// dynamic import, same pattern as stores/__tests__/router.store.test.ts
// freshStore()) so the module-scoped backoff timestamp doesn't leak into/out
// of other tests in this file — the mocked ../cloud-api singleton and the
// window._platform stubs are unaffected by resetModules and continue to
// apply to the freshly-imported instance.
describe('routerFetch 401 backoff (I4 — negative cache)', () => {
  async function freshService() {
    vi.resetModules();
    return import('../router-service');
  }

  it('a second 401 within 60s does not call cloudApi.post again', async () => {
    mockStorage.get.mockResolvedValue('rck_stale');
    (cloudApi.post as any).mockResolvedValue({ code: 0, data: { controlKey: 'rck_fresh' } });
    // k2r never accepts any key — every attempt (initial + force-refreshed) 401s.
    mockRouterRequest.mockResolvedValue({ status: 401, body: '' });
    const svc = await freshService();

    const first = await svc.routerCore('status');
    expect(first.code).toBe(401);
    expect(cloudApi.post).toHaveBeenCalledTimes(1); // one force-refresh POST

    const second = await svc.routerCore('status');
    expect(second.code).toBe(401);
    // Still 1 — the second 401 hit the backoff window and skipped Center entirely.
    expect(cloudApi.post).toHaveBeenCalledTimes(1);
  });

  it('clears the backoff once a force-refresh succeeds (happy path unaffected)', async () => {
    mockStorage.get.mockResolvedValue('rck_stale');
    (cloudApi.post as any).mockResolvedValue({ code: 0, data: { controlKey: 'rck_fresh' } });
    mockRouterRequest
      .mockResolvedValueOnce({ status: 401, body: '' }) // stale key
      .mockResolvedValueOnce({ status: 200, body: '{"code":0,"message":"ok"}' }); // refreshed key accepted
    const svc = await freshService();

    const resp = await svc.routerCore('status');
    expect(resp.code).toBe(0);
    expect(cloudApi.post).toHaveBeenCalledTimes(1);

    // A later, independent 401 (e.g. after another rotation) must still
    // force-refresh — the backoff from the earlier successful recovery
    // must not still be armed.
    mockRouterRequest.mockResolvedValueOnce({ status: 401, body: '' });
    mockRouterRequest.mockResolvedValueOnce({ status: 200, body: '{"code":0,"message":"ok"}' });
    const resp2 = await svc.routerCore('status');
    expect(resp2.code).toBe(0);
    expect(cloudApi.post).toHaveBeenCalledTimes(2);
  });
});
