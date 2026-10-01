/**
 * 转化漏斗看板：路径/时段/分组选择、漏斗条、分组表、留存表、错误态。
 */
import { render, screen, fireEvent, waitFor, within } from '@testing-library/react';
import { describe, it, expect, vi, beforeEach } from 'vitest';
import { ManagerBrandProvider, MANAGER_BRAND_STORAGE_KEY, useManagerBrand } from '@/components/manager/brand';

const mockPaths = vi.fn();
const mockFunnel = vi.fn();
const mockRetention = vi.fn();

// Radix Select 在 jsdom 下的指针事件不可靠，替身为原生 <select>，以 name 作 aria-label。
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
      getRetention: (...a: unknown[]) => mockRetention(...a),
    },
  };
});

import { ApiError, ErrorCode } from '@/lib/api';
import FunnelsPage from '../page.kaitu';

const PATHS = {
  paths: [
    { key: 'acquisition_to_paid', title: '获客到付费', question: '来的人最后付费了吗', steps: ['访问', '注册', '付费'], windowHours: 168 },
    { key: 'install_to_connect', title: '安装到连接', question: '装了能连上吗', steps: ['安装', '连接'], windowHours: 24 },
  ],
  groupDims: ['source', 'country'],
};

function funnel(counts: number[], extra: Partial<{ groups: { key: string; steps: number[] }[]; daily: { date: string; entered: number; completed: number }[] }> = {}) {
  return {
    steps: counts.map((c, i) => ({
      label: `步骤${i + 1}`,
      count: c,
      rateFromPrev: i === 0 ? 1 : counts[i - 1] ? c / counts[i - 1] : 0,
      rateFromFirst: counts[0] ? c / counts[0] : 0,
      medianSecFromPrev: i === 1 ? 90 : null,
    })),
    daily: extra.daily ?? [],
    groups: extra.groups ?? [],
  };
}

function renderPage() {
  return render(
    <ManagerBrandProvider>
      <FunnelsPage />
    </ManagerBrandProvider>,
  );
}

describe('manager funnels page', () => {
  beforeEach(() => {
    window.localStorage.clear();
    mockPaths.mockReset().mockResolvedValue(PATHS);
    mockFunnel.mockReset().mockResolvedValue(funnel([100, 40, 10]));
    mockRetention.mockReset().mockImplementation(async (p: { metric: string }) =>
      p.metric === 'paid'
        ? { rows: [{ cohort: '2026-09', size: 20, retained: { m1: 0.5, m3: null, m6: null, m12: null }, refunded: 2 }], note: '仅统计已满期群组' }
        : { rows: [{ cohort: '2026-09-30', size: 7, d1: 0.25, d7: null, d30: null }] },
    );
  });

  it('loads paths then the first path funnel with the brand filter', async () => {
    window.localStorage.setItem(MANAGER_BRAND_STORAGE_KEY, 'overleap');
    renderPage();
    await waitFor(() => expect(mockFunnel).toHaveBeenCalled());
    expect(mockPaths).toHaveBeenCalledTimes(1);
    expect(mockFunnel.mock.calls[0][0]).toBe('acquisition_to_paid');
    expect(mockFunnel.mock.calls[0][1]).toMatchObject({ brand: 'overleap' });
    expect(mockFunnel.mock.calls[0][1].from).toMatch(/^\d{4}-\d{2}-\d{2}$/);
    expect(mockFunnel.mock.calls[0][1].to).toMatch(/^\d{4}-\d{2}-\d{2}$/);
    expect(mockFunnel.mock.calls[0][1].groupBy).toBeUndefined();
    expect(await screen.findByText('来的人最后付费了吗')).toBeTruthy();
  });

  it('shows step rates and drop-off counts', async () => {
    renderPage();
    expect(await screen.findByText('40.0%')).toBeTruthy();
    expect(screen.getByText('25.0%')).toBeTruthy();
    expect(screen.getByText('60')).toBeTruthy();
    expect(screen.getByText('30')).toBeTruthy();
    expect(screen.getByText('1.5 分')).toBeTruthy(); // 90s median
  });

  it('shows an empty notice when the first step is 0', async () => {
    mockFunnel.mockResolvedValue(funnel([0, 0, 0]));
    renderPage();
    expect(await screen.findByText('该时间段内没有数据')).toBeTruthy();
  });

  it('refetches with groupBy and renders the group table', async () => {
    renderPage();
    await waitFor(() => expect(mockFunnel).toHaveBeenCalledTimes(1));
    mockFunnel.mockResolvedValue(
      funnel([100, 40, 10], { groups: [{ key: 'organic', steps: [60, 30, 6] }, { key: 'twitter', steps: [40, 10, 2] }] }),
    );
    fireEvent.change(await screen.findByLabelText('group'), { target: { value: 'source' } });
    await waitFor(() => expect(mockFunnel).toHaveBeenCalledTimes(2));
    expect(mockFunnel.mock.calls[1][1]).toMatchObject({ groupBy: 'source' });
    expect(await screen.findByText('organic')).toBeTruthy();
    expect(screen.getByText('twitter')).toBeTruthy();
    expect(screen.getByText('10.0%')).toBeTruthy(); // organic 6/60
  });

  it('retention tab loads paid cohorts and renders null as a dash', async () => {
    renderPage();
    await waitFor(() => expect(mockFunnel).toHaveBeenCalled());
    fireEvent.click(screen.getByRole('button', { name: '留存' }));
    await waitFor(() => expect(mockRetention).toHaveBeenCalled());
    expect(mockRetention.mock.calls.some((c) => c[0].metric === 'paid')).toBe(true);
    expect(await screen.findByText('2026-09')).toBeTruthy();
    expect(screen.getByText('50.0%')).toBeTruthy();
    const row = screen.getByText('2026-09').closest('tr') as HTMLElement;
    expect(within(row).getAllByText('—').length).toBe(3);
    expect(screen.getByText('仅统计已满期群组')).toBeTruthy();
  });

  it('shows a Chinese error and does not crash when the API throws', async () => {
    mockFunnel.mockRejectedValue(new ApiError(ErrorCode.SystemError, 'raw backend boom'));
    renderPage();
    const alert = await screen.findByRole('alert');
    expect(alert.textContent).toBeTruthy();
    expect(alert.textContent).not.toContain('raw backend boom');
    expect(/[一-龥]/.test(alert.textContent || '')).toBe(true);
  });

  function deferred<T>() {
    let resolve!: (v: T) => void;
    const promise = new Promise<T>((r) => { resolve = r; });
    return { promise, resolve };
  }

  it('hides the previous numbers while a new selection loads, then shows the new ones', async () => {
    renderPage();
    expect(await screen.findByText('40.0%')).toBeTruthy();
    const d = deferred<ReturnType<typeof funnel>>();
    mockFunnel.mockReturnValueOnce(d.promise);
    fireEvent.change(screen.getByLabelText('path'), { target: { value: 'install_to_connect' } });
    await waitFor(() => expect(mockFunnel).toHaveBeenCalledTimes(2));
    expect(screen.queryByText('40.0%')).toBeNull();
    expect(screen.getByText('加载中…')).toBeTruthy();
    d.resolve(funnel([50, 5]));
    expect(await screen.findByText('10.0%')).toBeTruthy();
    expect(screen.queryByText('加载中…')).toBeNull();
  });

  it('clears the old brand numbers when the brand changes', async () => {
    function Switch() {
      const { setBrand } = useManagerBrand();
      return <button onClick={() => setBrand('overleap')}>switch</button>;
    }
    render(
      <ManagerBrandProvider>
        <Switch />
        <FunnelsPage />
      </ManagerBrandProvider>,
    );
    expect(await screen.findByText('40.0%')).toBeTruthy();
    const d = deferred<ReturnType<typeof funnel>>();
    mockFunnel.mockReturnValueOnce(d.promise);
    fireEvent.click(screen.getByText('switch'));
    await waitFor(() => expect(mockFunnel).toHaveBeenCalledTimes(2));
    expect(mockFunnel.mock.calls[1][1]).toMatchObject({ brand: 'overleap' });
    expect(screen.queryByText('40.0%')).toBeNull();
    d.resolve(funnel([20, 10]));
    expect(await screen.findByText('50.0%')).toBeTruthy();
  });

  it('an older response resolving after a newer one does not overwrite it', async () => {
    renderPage();
    await screen.findByText('40.0%');
    const slow = deferred<ReturnType<typeof funnel>>();
    const fast = deferred<ReturnType<typeof funnel>>();
    mockFunnel.mockReturnValueOnce(slow.promise).mockReturnValueOnce(fast.promise);
    fireEvent.change(screen.getByLabelText('path'), { target: { value: 'install_to_connect' } });
    await waitFor(() => expect(mockFunnel).toHaveBeenCalledTimes(2));
    fireEvent.change(screen.getByLabelText('range'), { target: { value: '7' } });
    await waitFor(() => expect(mockFunnel).toHaveBeenCalledTimes(3));
    fast.resolve(funnel([200, 100]));
    expect(await screen.findByText('50.0%')).toBeTruthy();
    slow.resolve(funnel([10, 9]));
    await new Promise((r) => setTimeout(r, 20));
    expect(screen.getByText('50.0%')).toBeTruthy();
    expect(screen.queryByText('90.0%')).toBeNull();
  });

  it('keeps funnel and retention errors on their own tab', async () => {
    mockFunnel.mockRejectedValue(new ApiError(ErrorCode.SystemError, 'x'));
    renderPage();
    await screen.findByRole('alert');
    fireEvent.click(screen.getByRole('button', { name: '留存' }));
    expect(await screen.findByText('2026-09')).toBeTruthy();
    expect(screen.queryByRole('alert')).toBeNull();
    mockRetention.mockReset().mockRejectedValue(new ApiError(ErrorCode.SystemError, 'y'));
    fireEvent.click(screen.getByRole('button', { name: '漏斗' }));
    expect(screen.getByRole('alert')).toBeTruthy(); // funnel error still there
  });

  it('a non-ApiError failure (network error) still shows a Chinese message', async () => {
    mockFunnel.mockRejectedValue(new TypeError('Failed to fetch'));
    renderPage();
    const alert = await screen.findByRole('alert');
    expect(alert.textContent).not.toContain('Failed to fetch');
    expect(/[\u4e00-\u9fa5]/.test(alert.textContent || '')).toBe(true);
  });

  it('never shows a negative drop-off', async () => {
    mockFunnel.mockResolvedValue(funnel([10, 15]));
    renderPage();
    await screen.findByText('1.5 分');
    expect(screen.queryByText('-5')).toBeNull();
    expect(screen.getByText('0')).toBeTruthy();
  });

  it('formats median durations in the largest fitting unit', async () => {
    const f = funnel([100, 90, 80, 70, 60, 50, 40]);
    const secs = [null, 0.4, 45, 60, 7200, 86400, 129600];
    f.steps.forEach((st, i) => { st.medianSecFromPrev = secs[i]; });
    mockFunnel.mockResolvedValue(f);
    renderPage();
    await screen.findByText('0.4 秒');
    expect(screen.getByText('45 秒')).toBeTruthy();
    expect(screen.getByText('1 分')).toBeTruthy();
    expect(screen.getByText('2 小时')).toBeTruthy();
    expect(screen.getByText('1 天')).toBeTruthy();
    expect(screen.getByText('1.5 天')).toBeTruthy();
  });
});
