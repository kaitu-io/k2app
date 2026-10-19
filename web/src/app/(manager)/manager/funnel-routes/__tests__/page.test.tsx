/**
 * 转化路线：按品牌 × 路线分列、每步标出发生在哪、跨面那一跳标注且带说明、与漏斗页同一接口。
 */
import { render, screen, waitFor, within, fireEvent } from '@testing-library/react';
import { describe, it, expect, vi, beforeEach } from 'vitest';
import { ManagerBrandProvider, MANAGER_BRAND_STORAGE_KEY } from '@/components/manager/brand';
import { hopsOf } from '@/components/manager/funnel-format';

const mockPaths = vi.fn();
const mockFunnel = vi.fn();

vi.mock('@/components/ui/select', () => ({
  Select: ({ value, onValueChange, name, children }: { value: string; onValueChange: (v: string) => void; name?: string; children: React.ReactNode }) => (
    <select aria-label={name} value={value} onChange={(e) => onValueChange(e.target.value)}>
      {children}
    </select>
  ),
  SelectTrigger: ({ children }: { children: React.ReactNode }) => <>{children}</>,
  SelectValue: () => null,
  SelectContent: ({ children }: { children: React.ReactNode }) => <>{children}</>,
  SelectItem: ({ value, children }: { value: string; children: React.ReactNode }) => <option value={value}>{children}</option>,
}));

vi.mock('@/lib/api', async (importOriginal) => {
  const actual = await importOriginal<typeof import('@/lib/api')>();
  return {
    ...actual,
    api: {
      getFunnelPaths: (...a: unknown[]) => mockPaths(...a),
      getFunnel: (...a: unknown[]) => mockFunnel(...a),
    },
  };
});

import { ApiError, ErrorCode } from '@/lib/api';
import FunnelRoutesPage from '../page';

const PATHS = {
  paths: [
    { key: 'web_to_app', title: '网站下载到应用', question: 'Q1', steps: ['点下载', '首次打开', '注册 / 登录', '付款'], stepWhere: ['web', 'app', 'any', 'facts'], lane: 'web', hop: '网站 → App：推断说明', windowHours: 336 },
    { key: 'app_activation', title: '应用激活', question: 'Q2', steps: ['首次打开', '连上'], stepWhere: ['app', 'app'], lane: 'app', windowHours: 168 },
    { key: 'app_to_web', title: '应用跳到网站', question: 'Q3', steps: ['从 App 打开官网', '付款'], stepWhere: ['web', 'facts'], lane: 'app', hop: 'App → 网站：utm 标记', windowHours: 336 },
  ],
  groupDims: [],
};

function funnel(counts: number[]) {
  return {
    steps: counts.map((c, i) => ({ label: `s${i}`, count: c, rateFromPrev: i === 0 ? 1 : counts[i - 1] ? c / counts[i - 1] : 0, rateFromFirst: 0, medianSecFromPrev: null })),
    daily: [],
    groups: [],
  };
}

const renderPage = () =>
  render(
    <ManagerBrandProvider>
      <FunnelRoutesPage />
    </ManagerBrandProvider>,
  );

describe('manager funnel routes page', () => {
  beforeEach(() => {
    window.localStorage.clear();
    mockPaths.mockReset().mockResolvedValue(PATHS);
    mockFunnel.mockReset().mockImplementation(async (key: string) =>
      key === 'web_to_app' ? funnel([200, 10, 5, 1]) : key === 'app_activation' ? funnel([40, 10]) : funnel([0, 0]),
    );
  });

  it('“全部” queries every path for both brands and lays them out by lane', async () => {
    renderPage();
    await waitFor(() => expect(mockFunnel).toHaveBeenCalledTimes(6));
    const calls = mockFunnel.mock.calls.map((c) => `${c[1].brand}/${c[0]}`).sort();
    expect(calls).toEqual(['kaitu/app_activation', 'kaitu/app_to_web', 'kaitu/web_to_app', 'overleap/app_activation', 'overleap/app_to_web', 'overleap/web_to_app']);
    for (const c of mockFunnel.mock.calls) expect(c[1]).toMatchObject({ inferred: false, from: expect.stringMatching(/^\d{4}-\d{2}-\d{2}$/) });
    const appLane = screen.getByTestId('lane-kaitu-app');
    expect(within(appLane).getByText('应用激活')).toBeTruthy();
    expect(within(appLane).getByText('应用跳到网站')).toBeTruthy();
    expect(within(appLane).queryByText('网站下载到应用')).toBeNull();
    expect(within(screen.getByTestId('lane-kaitu-web')).getByText('网站下载到应用')).toBeTruthy();
  });

  it('a selected brand narrows the queries to that brand', async () => {
    window.localStorage.setItem(MANAGER_BRAND_STORAGE_KEY, 'overleap');
    renderPage();
    await waitFor(() => expect(mockFunnel).toHaveBeenCalledTimes(3));
    expect(mockFunnel.mock.calls.every((c) => c[1].brand === 'overleap')).toBe(true);
    expect(screen.queryByTestId('lane-kaitu-app')).toBeNull();
  });

  it('shows counts, step rates, where each step happens, the hop marker and its note', async () => {
    window.localStorage.setItem(MANAGER_BRAND_STORAGE_KEY, 'kaitu');
    renderPage();
    const card = await screen.findByTestId('route-web_to_app');
    await within(card).findByText('200');
    expect(within(card).getByText('5.0%')).toBeTruthy(); // 10 / 200
    expect(within(card).getByText('总转化率 0.5%', { exact: false })).toBeTruthy();
    expect(within(card).getAllByTestId('hop').map((n) => n.textContent)).toEqual(['↪ 跳到App']);
    expect(within(card).getByText('网站 → App：推断说明')).toBeTruthy();
    expect(within(card).getByText('网站')).toBeTruthy();
    expect(within(card).getByText('账户')).toBeTruthy();
    // 不跨面的路径没有跳转标记，也没有说明框。
    const act = screen.getByTestId('route-app_activation');
    await within(act).findByText('40');
    expect(within(act).queryByTestId('hop')).toBeNull();
    expect(within(act).queryByText('跨端说明：')).toBeNull();
    // 链到漏斗页明细。
    expect(within(card).getByRole('link', { name: '网站下载到应用' }).getAttribute('href')).toBe('/manager/funnels?path=web_to_app');
  });

  it('an empty path says so instead of showing 0% rates', async () => {
    window.localStorage.setItem(MANAGER_BRAND_STORAGE_KEY, 'kaitu');
    renderPage();
    const card = await screen.findByTestId('route-app_to_web');
    expect(await within(card).findByText('该时间段内没有人进入这条路径', { exact: false })).toBeTruthy();
    expect(within(card).getByText('—')).toBeTruthy();
    expect(within(card).getAllByTestId('hop').map((n) => n.textContent)).toEqual(['↪ 跳到网站']);
  });

  it('one failing path shows its own error, the others still render', async () => {
    window.localStorage.setItem(MANAGER_BRAND_STORAGE_KEY, 'kaitu');
    mockFunnel.mockImplementation(async (key: string) => {
      if (key === 'app_activation') throw new ApiError(ErrorCode.Forbidden, 'no');
      return funnel([200, 10, 5, 1]);
    });
    renderPage();
    expect(await within(await screen.findByTestId('route-app_activation')).findByRole('alert')).toBeTruthy();
    expect(await within(screen.getByTestId('route-web_to_app')).findByText('200')).toBeTruthy();
  });

  it('range and inference toggles refetch everything with the new parameters', async () => {
    window.localStorage.setItem(MANAGER_BRAND_STORAGE_KEY, 'kaitu');
    renderPage();
    await waitFor(() => expect(mockFunnel).toHaveBeenCalledTimes(3));
    fireEvent.click(screen.getByRole('switch', { name: '含推断归因' }));
    await waitFor(() => expect(mockFunnel).toHaveBeenCalledTimes(6));
    expect(mockFunnel.mock.calls.slice(3).every((c) => c[1].inferred === true)).toBe(true);
    fireEvent.change(screen.getByLabelText('range'), { target: { value: '7' } });
    await waitFor(() => expect(mockFunnel).toHaveBeenCalledTimes(9));
    const { from, to } = mockFunnel.mock.calls[8][1];
    expect((Date.parse(to) - Date.parse(from)) / 86400000).toBe(6);
  });
});

describe('hopsOf', () => {
  it('marks a step only where the surface actually changes; any / facts never do', () => {
    expect(hopsOf({ lane: 'web', stepWhere: ['web', 'app', 'any', 'facts'] })).toEqual([null, 'app', null, null]);
    expect(hopsOf({ lane: 'app', stepWhere: ['web', 'web', 'any', 'facts'] })).toEqual(['web', null, null, null]);
    expect(hopsOf({ lane: 'app', stepWhere: ['app', 'any', 'facts'] })).toEqual([null, null, null]);
    expect(hopsOf({ lane: 'web', stepWhere: ['web', 'app', 'web'] })).toEqual([null, 'app', 'web']);
  });
});
