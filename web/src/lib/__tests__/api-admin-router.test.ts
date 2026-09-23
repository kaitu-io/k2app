import { describe, it, expect, vi, beforeEach } from 'vitest';
import { api } from '../api';

describe('router edition admin api methods', () => {
  beforeEach(() => {
    vi.restoreAllMocks();
  });

  it('listRouterFulfillments 带全部参数拼接查询串', async () => {
    const spy = vi.spyOn(api, 'request').mockResolvedValue({ items: [] } as never);
    await api.listRouterFulfillments({ page: 2, pageSize: 20, stage: 'ready', userId: 7 });
    expect(spy).toHaveBeenCalledWith('/app/router/fulfillments?page=2&pageSize=20&stage=ready&userId=7');
  });

  it('listRouterFulfillments 不带参数时不拼接查询串', async () => {
    const spy = vi.spyOn(api, 'request').mockResolvedValue({ items: [] } as never);
    await api.listRouterFulfillments();
    expect(spy).toHaveBeenCalledWith('/app/router/fulfillments');
  });

  it('shipRouterFulfillment POST /app/router/fulfillments/:id/stage', async () => {
    const spy = vi.spyOn(api, 'request').mockResolvedValue(undefined as never);
    await api.shipRouterFulfillment(3, { trackingNo: 'SF1', carrier: '顺丰' });
    expect(spy).toHaveBeenCalledTimes(1);
    const [path, options] = spy.mock.calls[0];
    expect(path).toBe('/app/router/fulfillments/3/stage');
    expect(options?.method).toBe('POST');
    expect(JSON.parse(options!.body as string)).toEqual({
      stage: 'shipped',
      trackingNo: 'SF1',
      carrier: '顺丰',
    });
  });

  it('updateRouterFulfillmentNote 用空 stage 只改备注', async () => {
    const spy = vi.spyOn(api, 'request').mockResolvedValue(undefined as never);
    await api.updateRouterFulfillmentNote(3, 'n');
    expect(spy).toHaveBeenCalledTimes(1);
    const [path, options] = spy.mock.calls[0];
    expect(path).toBe('/app/router/fulfillments/3/stage');
    expect(options?.method).toBe('POST');
    expect(JSON.parse(options!.body as string)).toEqual({ stage: '', note: 'n' });
  });

  it('mintRouterCredential POST /app/router/fulfillments/:id/credential', async () => {
    const spy = vi.spyOn(api, 'request').mockResolvedValue({ url: 'k2r://x', deviceId: 1 } as never);
    await api.mintRouterCredential(3);
    expect(spy).toHaveBeenCalledWith('/app/router/fulfillments/3/credential', { method: 'POST' });
  });

  it('extendPrivateNodeSubscription POST /app/private-node-subscriptions/:id/extend', async () => {
    const spy = vi.spyOn(api, 'request').mockResolvedValue({} as never);
    await api.extendPrivateNodeSubscription(9, { months: 1, reason: 'r' });
    expect(spy).toHaveBeenCalledTimes(1);
    const [path, options] = spy.mock.calls[0];
    expect(path).toBe('/app/private-node-subscriptions/9/extend');
    expect(options?.method).toBe('POST');
    expect(JSON.parse(options!.body as string)).toEqual({ months: 1, reason: 'r' });
  });

  it('listPrivateNodeSubscriptions 拼接 status 查询串', async () => {
    const spy = vi.spyOn(api, 'request').mockResolvedValue({ items: [] } as never);
    await api.listPrivateNodeSubscriptions({ status: 'active' });
    expect(spy).toHaveBeenCalledWith('/app/private-node-subscriptions?status=active');
  });

  it('listRouterDevices 拼接 userId 查询串', async () => {
    const spy = vi.spyOn(api, 'request').mockResolvedValue({ items: [] } as never);
    await api.listRouterDevices({ userId: 5 });
    expect(spy).toHaveBeenCalledWith('/app/router-devices?userId=5');
  });

  it('getRouterStats 请求 /app/router/stats', async () => {
    const spy = vi.spyOn(api, 'request').mockResolvedValue({
      stageCounts: {},
      stuck: 0,
      onlineRouters: 0,
      expiringSoon: 0,
    } as never);
    await api.getRouterStats();
    expect(spy).toHaveBeenCalledWith('/app/router/stats');
  });
});
