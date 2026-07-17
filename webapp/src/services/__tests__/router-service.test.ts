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
