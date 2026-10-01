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

  // ---- daily chart ------------------------------------------------------

  function days(n: number, entered: (i: number) => number = (i) => i + 1) {
    const start = Date.UTC(2026, 6, 1);
    return Array.from({ length: n }, (_, i) => ({
      date: new Date(start + i * 86400000).toISOString(),
      entered: entered(i),
      completed: Math.floor(entered(i) / 2),
    }));
  }

  it('daily chart over 90 days: about 8 unrotated MM-DD labels, no per-bar counts, max as a caption, one summarising image', async () => {
    mockFunnel.mockResolvedValue(funnel([100, 40, 10], { daily: days(90) }));
    renderPage();
    const chart = await screen.findByTestId('daily-chart');
    expect(chart.getAttribute('role')).toBe('img');
    const label = chart.getAttribute('aria-label') || '';
    expect(label).toContain('07-01');
    expect(label).toContain('09-28');
    expect(label).toContain('90 天');
    expect(label).toContain(`${(90 * 91) / 2}`); // total entered
    expect(label).toContain('最高 90');

    const bars = within(chart).getAllByTestId('daily-bar');
    expect(bars).toHaveLength(90);
    const labels = within(chart).getAllByTestId('daily-label').map((n) => n.textContent).filter(Boolean);
    expect(labels.length).toBeGreaterThanOrEqual(6);
    expect(labels.length).toBeLessThanOrEqual(8);
    for (const l of labels) expect(l).toMatch(/^\d{2}-\d{2}$/);
    expect(labels[0]).toBe('07-01');
    expect(chart.innerHTML).not.toContain('rotate');
    expect(within(chart).queryAllByTestId('daily-count')).toHaveLength(0);
    expect(within(chart).getByText('单日最高 90 人')).toBeTruthy();
    // the detail stays reachable on hover
    expect(bars[89].closest('[title]')?.getAttribute('title')).toContain('进入 90');
    // bars share the width instead of scrolling
    expect(bars[0].parentElement?.className).toContain('flex-1');
    expect(bars[0].parentElement?.className).toContain('min-w-0');
    expect(chart.innerHTML).not.toContain('overflow-x-auto');
  });

  it('daily chart up to 14 days keeps the count above each bar; the tallest bar fills the bar area', async () => {
    mockFunnel.mockResolvedValue(funnel([100, 40, 10], { daily: days(14, (i) => (i === 3 ? 0 : (i + 1) * 5)) }));
    renderPage();
    const chart = await screen.findByTestId('daily-chart');
    const counts = within(chart).getAllByTestId('daily-count').map((n) => n.textContent);
    expect(counts).toHaveLength(13); // the zero day carries no label
    expect(counts).toContain('70');
    const bars = within(chart).getAllByTestId('daily-bar');
    expect(bars[13].style.height).toBe('100%');
    expect(bars[3].style.height).toBe('0%');
    const labels = within(chart).getAllByTestId('daily-label').map((n) => n.textContent).filter(Boolean);
    expect(labels).toHaveLength(7); // step 2 over 14 days
  });

  it('daily chart at 15 days drops the per-bar counts', async () => {
    mockFunnel.mockResolvedValue(funnel([100, 40, 10], { daily: days(15) }));
    renderPage();
    const chart = await screen.findByTestId('daily-chart');
    expect(within(chart).queryAllByTestId('daily-count')).toHaveLength(0);
  });

  // ---- funnel card ------------------------------------------------------

  it('funnel bars are images labelled with step, count and rate', async () => {
    renderPage();
    await screen.findByText('40.0%');
    const bars = screen.getAllByTestId('funnel-bar');
    expect(bars).toHaveLength(3);
    for (const b of bars) expect(b.getAttribute('role')).toBe('img');
    expect(bars[1].getAttribute('aria-label')).toBe('步骤2：40 人，占第 1 步 40.0%');
    expect(bars[2].getAttribute('aria-label')).toBe('步骤3：10 人，占第 1 步 10.0%');
  });

  it('shows the attribution window in days from 24 h up, in hours below, with the reading captions', async () => {
    mockPaths.mockResolvedValue({
      ...PATHS,
      paths: [
        ...PATHS.paths,
        { key: 'short', title: '短', question: 'q', steps: ['a', 'b'], windowHours: 12 },
        { key: 'long', title: '长', question: 'q', steps: ['a', 'b'], windowHours: 336 },
        { key: 'odd', title: '奇', question: 'q', steps: ['a', 'b'], windowHours: 36 },
      ],
    });
    renderPage();
    expect(await screen.findByText(/归因窗口 7 天/)).toBeTruthy();
    expect(screen.queryByText(/168 小时/)).toBeNull();
    expect(screen.getByText('最近进入的访客可能还没走完（归因窗口未结束），近几天的转化率会偏低')).toBeTruthy();
    expect(screen.getByText('每人按其在所选时间段内走得最远的一次进入计算')).toBeTruthy();

    for (const [key, text] of [['install_to_connect', /归因窗口 1 天/], ['short', /归因窗口 12 小时/], ['long', /归因窗口 14 天/], ['odd', /归因窗口 1\.5 天/]] as const) {
      fireEvent.change(screen.getByLabelText('path'), { target: { value: key } });
      expect(await screen.findByText(text)).toBeTruthy();
    }
  });

  it('renders whatever number of steps the path returns (nothing is tied to a step count)', async () => {
    for (const counts of [[100, 50], [100, 80, 40, 20], [100, 90, 80, 70, 60]]) {
      mockFunnel.mockResolvedValue(funnel(counts, { groups: [{ key: 'organic', steps: counts.map((c) => c / 10) }] }));
      const view = renderPage();
      await waitFor(() => expect(screen.getAllByTestId('funnel-bar')).toHaveLength(counts.length));
      fireEvent.change(screen.getByLabelText('group'), { target: { value: 'source' } });
      const row = (await screen.findByText('organic')).closest('tr') as HTMLElement;
      expect(row.querySelectorAll('td')).toHaveLength(counts.length + 2);
      expect(screen.getAllByRole('columnheader')).toHaveLength(counts.length + 2);
      expect(screen.getByText(new RegExp(`总转化率 ${((counts[counts.length - 1] / counts[0]) * 100).toFixed(1)}%`))).toBeTruthy();
      view.unmount();
    }
  });

  // ---- group table ------------------------------------------------------

  const GROUPS = [
    { key: '(other)', steps: [500, 50, 5] },
    { key: 'direct', steps: [60, 30, 6] },
    { key: 'unknown', steps: [40, 10, 4] },
    { key: 'twitter', steps: [20, 10, 1] },
  ];
  async function renderGrouped() {
    mockFunnel.mockResolvedValue(funnel([620, 100, 16], { groups: GROUPS }));
    renderPage();
    fireEvent.change(await screen.findByLabelText('group'), { target: { value: 'source' } });
    await screen.findByText('twitter');
  }
  const firstColumn = () =>
    Array.from(document.querySelectorAll('tbody tr')).map((tr) => tr.querySelector('td')?.textContent);

  it('group keys read in Chinese: direct / unknown / (other)', async () => {
    await renderGrouped();
    expect(screen.getByText('直接访问')).toBeTruthy();
    expect(screen.getByText('未知')).toBeTruthy();
    expect(screen.getByText('其他（第 50 名之后合并）')).toBeTruthy();
    expect(screen.queryByText('direct')).toBeNull();
    expect(screen.queryByText('(other)')).toBeNull();
  });

  it('(other) stays last whatever the sort column or direction', async () => {
    await renderGrouped();
    const OTHER = '其他（第 50 名之后合并）';
    // default: first step, descending — (other) has the largest count and still goes last
    expect(firstColumn()).toEqual(['直接访问', '未知', 'twitter', OTHER]);
    fireEvent.click(screen.getByRole('button', { name: /^步骤1/ }));
    expect(firstColumn()).toEqual(['twitter', '未知', '直接访问', OTHER]);
    fireEvent.click(screen.getByRole('button', { name: /^总转化率/ }));
    expect(firstColumn()[3]).toBe(OTHER);
    fireEvent.click(screen.getByRole('button', { name: /^总转化率/ }));
    expect(firstColumn()[3]).toBe(OTHER);
    fireEvent.click(screen.getByRole('button', { name: /^来源/ }));
    expect(firstColumn()[3]).toBe(OTHER);
    fireEvent.click(screen.getByRole('button', { name: /^来源/ }));
    expect(firstColumn()[3]).toBe(OTHER);
  });

  it('sortable headers are buttons inside the th, which carries aria-sort', async () => {
    await renderGrouped();
    const headers = screen.getAllByRole('columnheader');
    expect(headers).toHaveLength(5);
    for (const th of headers) expect(th.querySelector('button')).not.toBeNull();
    expect(headers.map((h) => h.getAttribute('aria-sort'))).toEqual(['none', 'descending', 'none', 'none', 'none']);
    fireEvent.click(within(headers[1]).getByRole('button'));
    expect(headers[1].getAttribute('aria-sort')).toBe('ascending');
    fireEvent.click(within(headers[4]).getByRole('button'));
    expect(headers.map((h) => h.getAttribute('aria-sort'))).toEqual(['none', 'none', 'none', 'none', 'descending']);
    fireEvent.click(within(headers[0]).getByRole('button'));
    expect(headers[0].getAttribute('aria-sort')).toBe('ascending');
  });

  // ---- retention tab ----------------------------------------------------

  it('the path / range / group selectors are gone on the retention tab and back on the funnel tab', async () => {
    renderPage();
    await screen.findByText('40.0%');
    expect(screen.getByLabelText('path')).toBeTruthy();
    fireEvent.click(screen.getByRole('button', { name: '留存' }));
    await screen.findByText('2026-09');
    expect(screen.queryByLabelText('path')).toBeNull();
    expect(screen.queryByLabelText('range')).toBeNull();
    expect(screen.queryByLabelText('group')).toBeNull();
    expect(screen.queryByText('来的人最后付费了吗')).toBeNull();
    fireEvent.click(screen.getByRole('button', { name: '漏斗' }));
    expect(screen.getByLabelText('path')).toBeTruthy();
    expect(screen.getByLabelText('range')).toBeTruthy();
    expect(screen.getByLabelText('group')).toBeTruthy();
  });

  it.each([
    ['paid', '2026-09-30', '2026-09'],
    ['active', '2026-09', '2026-09-30'],
  ])('a failing %s metric does not blank the other table', async (failing, shown, hidden) => {
    mockRetention.mockReset().mockImplementation(async (p: { metric: string }) => {
      if (p.metric === failing) throw new ApiError(ErrorCode.SystemError, 'raw retention boom');
      return p.metric === 'paid'
        ? { rows: [{ cohort: '2026-09', size: 20, retained: { m1: 0.5, m3: null, m6: null, m12: null }, refunded: 2 }] }
        : { rows: [{ cohort: '2026-09-30', size: 7, d1: 0.25, d7: null, d30: null }] };
    });
    renderPage();
    await waitFor(() => expect(mockFunnel).toHaveBeenCalled());
    fireEvent.click(screen.getByRole('button', { name: '留存' }));
    expect(await screen.findByText(shown)).toBeTruthy();
    expect(screen.queryByText(hidden)).toBeNull();
    const alert = await screen.findByRole('alert');
    expect(alert.textContent).not.toContain('raw retention boom');
    expect(/[一-龥]/.test(alert.textContent || '')).toBe(true);
    expect(screen.getAllByRole('alert')).toHaveLength(1);
    // the error sits in the card of the metric that failed
    const card = alert.closest('[data-testid]') as HTMLElement;
    expect(card.getAttribute('data-testid')).toBe(`retention-${failing}`);
  });

  it('one metric still loading does not hold the other back', async () => {
    const slowPaid = deferred<unknown>();
    mockRetention.mockReset().mockImplementation((p: { metric: string }) =>
      p.metric === 'paid' ? slowPaid.promise : Promise.resolve({ rows: [{ cohort: '2026-09-30', size: 7, d1: 0.25, d7: null, d30: null }] }),
    );
    renderPage();
    await waitFor(() => expect(mockFunnel).toHaveBeenCalled());
    fireEvent.click(screen.getByRole('button', { name: '留存' }));
    expect(await screen.findByText('2026-09-30')).toBeTruthy();
    expect(within(screen.getByTestId('retention-paid')).getByText('加载中…')).toBeTruthy();
    slowPaid.resolve({ rows: [{ cohort: '2026-09', size: 20, retained: { m1: 0.5, m3: null, m6: null, m12: null }, refunded: 2 }] });
    expect(await screen.findByText('2026-09')).toBeTruthy();
  });

  it('shows the note of each retention table, also when a table has no rows', async () => {
    mockRetention.mockReset().mockImplementation(async (p: { metric: string }) =>
      p.metric === 'paid' ? { rows: [], note: '付费留存口径说明' } : { rows: [{ cohort: '2026-09-30', size: 7, d1: 0.25, d7: null, d30: null }], note: '活跃留存口径说明' },
    );
    renderPage();
    await waitFor(() => expect(mockFunnel).toHaveBeenCalled());
    fireEvent.click(screen.getByRole('button', { name: '留存' }));
    expect(await within(await screen.findByTestId('retention-paid')).findByText('付费留存口径说明')).toBeTruthy();
    expect(await within(screen.getByTestId('retention-active')).findByText('活跃留存口径说明')).toBeTruthy();
  });
});
