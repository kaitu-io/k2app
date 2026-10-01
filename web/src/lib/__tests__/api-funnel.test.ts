import { describe, it, expect, vi, beforeEach, afterEach, type MockInstance } from 'vitest';
import { api } from '../api';

describe('funnel api URL building', () => {
  let spy: MockInstance<typeof api.request>;
  beforeEach(() => {
    spy = vi.spyOn(api, 'request').mockResolvedValue({} as never);
  });
  afterEach(() => spy.mockRestore());

  it('getFunnelPaths hits the list endpoint', async () => {
    await api.getFunnelPaths();
    expect(spy).toHaveBeenCalledWith('/app/stats/funnels');
  });

  it('getFunnel encodes the key and includes brand/from/to/groupBy', async () => {
    await api.getFunnel('a/b c', { brand: 'overleap', from: '2026-09-01', to: '2026-09-30', groupBy: 'source' });
    const url = spy.mock.calls[0][0] as string;
    expect(url.startsWith('/app/stats/funnels/a%2Fb%20c?')).toBe(true);
    const qs = new URLSearchParams(url.split('?')[1]);
    expect(Object.fromEntries(qs)).toEqual({ brand: 'overleap', from: '2026-09-01', to: '2026-09-30', groupBy: 'source' });
  });

  it('getFunnel omits empty brand and groupBy', async () => {
    await api.getFunnel('k', { from: '2026-09-01', to: '2026-09-30', groupBy: '' });
    const qs = new URLSearchParams((spy.mock.calls[0][0] as string).split('?')[1]);
    expect([...qs.keys()].sort()).toEqual(['from', 'to']);
  });

  it('getRetention sends metric, and brand/months only when set', async () => {
    await api.getRetention({ metric: 'paid' });
    expect(spy.mock.calls[0][0]).toBe('/app/stats/retention?metric=paid');
    await api.getRetention({ brand: 'kaitu', metric: 'active', months: 6 });
    const qs = new URLSearchParams((spy.mock.calls[1][0] as string).split('?')[1]);
    expect(Object.fromEntries(qs)).toEqual({ brand: 'kaitu', metric: 'active', months: '6' });
  });
});
