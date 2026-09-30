/**
 * admin 列表/统计的 ?brand= 筛选：manager 从 kaitu.io 一个入口管两个品牌。
 * 选定品牌 → 带 brand；「全部」(undefined) → 不带（API 不过滤）。
 */
import { describe, it, expect, vi, beforeEach } from 'vitest';
import { api } from '../api';

function pathOf(spy: ReturnType<typeof vi.spyOn>): string {
  return spy.mock.calls[0][0] as string;
}
function brandOf(path: string): string | null {
  const q = path.includes('?') ? path.slice(path.indexOf('?') + 1) : '';
  return new URLSearchParams(q).get('brand');
}

type Case = { name: string; call: (brand?: 'kaitu' | 'overleap') => Promise<unknown>; prefix: string };

const cases: Case[] = [
  { name: 'getOrders', call: (b) => api.getOrders({ page: 1, brand: b }), prefix: '/app/orders' },
  { name: 'getCampaigns', call: (b) => api.getCampaigns({ brand: b }), prefix: '/app/campaigns' },
  { name: 'getAnnouncements', call: (b) => api.getAnnouncements({ brand: b }), prefix: '/app/announcements' },
  { name: 'getEmailTemplates', call: (b) => api.getEmailTemplates({ brand: b }), prefix: '/app/edm/templates' },
  { name: 'searchUsers', call: (b) => api.searchUsers({ email: 'a@b.c', brand: b }), prefix: '/app/users' },
  { name: 'listLicenseKeyBatches', call: (b) => api.listLicenseKeyBatches({ brand: b }), prefix: '/app/license-key-batches' },
  { name: 'listAdminLicenseKeys', call: (b) => api.listAdminLicenseKeys({ brand: b }), prefix: '/app/license-keys' },
  { name: 'getFeedbackTickets', call: (b) => api.getFeedbackTickets({ brand: b }), prefix: '/app/feedback-tickets' },
  { name: 'getDeviceStatistics', call: (b) => api.getDeviceStatistics({ brand: b }), prefix: '/app/devices/statistics' },
  { name: 'getUserStatistics', call: (b) => api.getUserStatistics({ brand: b }), prefix: '/app/users/statistics' },
  { name: 'getOrderStatistics', call: (b) => api.getOrderStatistics({ brand: b }), prefix: '/app/orders/statistics' },
  { name: 'listSlaveNodes', call: (b) => api.listSlaveNodes({ page: 1, pageSize: 200, brand: b }), prefix: '/app/nodes' },
];

describe('admin list/stat methods pass ?brand=', () => {
  beforeEach(() => {
    vi.restoreAllMocks();
  });

  it.each(cases)('$name 选定品牌时带 brand', async ({ call, prefix }) => {
    const spy = vi.spyOn(api, 'request').mockResolvedValue({ items: [], pagination: { total: 0, pageSize: 1 } } as never);
    await call('overleap');
    const path = pathOf(spy);
    expect(path.startsWith(prefix)).toBe(true);
    expect(brandOf(path)).toBe('overleap');
  });

  it.each(cases)('$name 全部品牌时不带 brand', async ({ call }) => {
    const spy = vi.spyOn(api, 'request').mockResolvedValue({ items: [], pagination: { total: 0, pageSize: 1 } } as never);
    await call(undefined);
    expect(brandOf(pathOf(spy))).toBeNull();
  });

  it('stat methods keep their bare path without a filter', async () => {
    const spy = vi.spyOn(api, 'request').mockResolvedValue({} as never);
    await api.getDeviceStatistics();
    expect(spy).toHaveBeenCalledWith('/app/devices/statistics');
  });
});

describe('admin create methods carry brand in the body', () => {
  beforeEach(() => {
    vi.restoreAllMocks();
  });

  it('createLicenseKeyBatch', async () => {
    const spy = vi.spyOn(api, 'request').mockResolvedValue(undefined as never);
    await api.createLicenseKeyBatch({
      name: 'n', sourceTag: 't', recipientMatcher: 'all', planDays: 30, quantity: 1, expiresInDays: 30, brand: 'overleap',
    });
    const [, opts] = spy.mock.calls[0];
    expect(JSON.parse(opts!.body as string).brand).toBe('overleap');
  });

  it('sendTemplatedEmails forwards the top-level brand', async () => {
    const spy = vi.spyOn(api, 'request').mockResolvedValue({} as never);
    await api.sendTemplatedEmails({ batchId: 'b', brand: 'overleap', items: [{ email: 'a@b.c', slug: 's' }] });
    const [path, opts] = spy.mock.calls[0];
    expect(path).toBe('/app/edm/send');
    expect(JSON.parse(opts!.body as string).brand).toBe('overleap');
  });
});
