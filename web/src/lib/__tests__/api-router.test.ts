import { describe, it, expect, vi, beforeEach } from 'vitest';
import { api } from '../api';

describe('router edition api methods', () => {
  beforeEach(() => {
    vi.restoreAllMocks();
  });

  it('getProductPlans 请求 /api/products/router/plans', async () => {
    const spy = vi.spyOn(api, 'request').mockResolvedValue({ items: [] } as never);
    await api.getProductPlans('router', { autoRedirectToAuth: false });
    expect(spy).toHaveBeenCalledWith('/api/products/router/plans', { autoRedirectToAuth: false });
  });

  it('getUserRouter 请求 /api/user/router', async () => {
    const spy = vi.spyOn(api, 'request').mockResolvedValue({ hasRouter: false } as never);
    await api.getUserRouter();
    expect(spy).toHaveBeenCalledWith('/api/user/router', undefined);
  });

  it('mintGatewayCredential POST /api/user/gateway-credential', async () => {
    const spy = vi.spyOn(api, 'request').mockResolvedValue({ url: 'k2subs://x' } as never);
    const res = await api.mintGatewayCredential();
    expect(spy).toHaveBeenCalledWith('/api/user/gateway-credential', { method: 'POST' });
    expect(res.url).toBe('k2subs://x');
  });
});
