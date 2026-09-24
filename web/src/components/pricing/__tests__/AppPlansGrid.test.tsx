/**
 * 定价页 App 版卡片：静态快照先出，/api/plans 回来后按 pid 覆盖；拉取失败保持快照。
 */
import { render, screen, waitFor } from '@testing-library/react';
import { describe, it, expect, vi, beforeEach } from 'vitest';
import AppPlansGrid, { mergeLivePlans, type AppPlansLabels } from '../AppPlansGrid';
import type { AppPlanSnapshot } from '@/lib/site/types';

const mockGetPlans = vi.fn();
vi.mock('@/lib/api', async (importOriginal) => {
  const actual = await importOriginal<typeof import('@/lib/api')>();
  return { ...actual, api: { ...actual.api, getPlans: (...a: unknown[]) => mockGetPlans(...a) } };
});
vi.mock('@/i18n/routing', () => ({
  Link: ({ href, children }: { href: string; children: React.ReactNode }) => <a href={href}>{children}</a>,
}));

const SNAPSHOT: AppPlanSnapshot[] = [
  { pid: '1y', months: 12, price: 4900, originPrice: 5900, highlight: false },
  { pid: '2y', months: 24, price: 9490, originPrice: 12800, highlight: true },
];
const LABELS: AppPlansLabels = {
  planNames: { '1y': '1年套餐', '2y': '2年套餐' },
  popular: '最受欢迎',
  perMonth: '折合 {price}/月',
  origin: '原价 {price}',
  save: '省 {percent}%',
  cta: '选择套餐',
  includes: ['5 台设备'],
};

describe('mergeLivePlans', () => {
  it('pid 命中用线上价 / 原价 / 高亮覆盖，未命中保留快照', () => {
    const merged = mergeLivePlans(SNAPSHOT, [
      { pid: '2y', price: 8900, originPrice: 12800, month: 24, highlight: false },
      { pid: '5y', price: 19900, originPrice: 29500, month: 60, highlight: true },
    ]);
    expect(merged).toEqual([
      SNAPSHOT[0],
      { pid: '2y', months: 24, price: 8900, originPrice: 12800, highlight: false },
    ]);
  });
});

describe('AppPlansGrid', () => {
  beforeEach(() => {
    mockGetPlans.mockReset();
  });

  it('首屏渲染快照价与折算月价，线上价回来后覆盖', async () => {
    let resolve: (v: unknown) => void = () => {};
    mockGetPlans.mockReturnValue(new Promise((r) => { resolve = r; }));
    render(<AppPlansGrid initial={SNAPSHOT} labels={LABELS} />);

    expect(screen.getByText('$94.90')).toBeInTheDocument();
    expect(screen.getByText('折合 $3.95/月')).toBeInTheDocument();
    expect(screen.getByText('省 26%')).toBeInTheDocument();
    expect(screen.getByText('最受欢迎')).toBeInTheDocument();
    expect(screen.getAllByRole('link', { name: '选择套餐' })[0]).toHaveAttribute('href', '/purchase');

    resolve({ items: [{ pid: '2y', price: 8900, originPrice: 12800, month: 24, highlight: true }] });
    await waitFor(() => expect(screen.getByText('$89')).toBeInTheDocument());
    expect(screen.queryByText('$94.90')).toBeNull();
    expect(screen.getByText('$49')).toBeInTheDocument();
  });

  it('线上套餐拉取失败：保持快照，不抛错', async () => {
    mockGetPlans.mockRejectedValue(new Error('network'));
    const warn = vi.spyOn(console, 'warn').mockImplementation(() => {});
    render(<AppPlansGrid initial={SNAPSHOT} labels={LABELS} />);
    await waitFor(() => expect(warn).toHaveBeenCalled());
    expect(screen.getByText('$94.90')).toBeInTheDocument();
    warn.mockRestore();
  });

  it('线上返回空列表：保持快照', async () => {
    mockGetPlans.mockResolvedValue({ items: [] });
    render(<AppPlansGrid initial={SNAPSHOT} labels={LABELS} />);
    await waitFor(() => expect(mockGetPlans).toHaveBeenCalledTimes(1));
    expect(screen.getByText('$94.90')).toBeInTheDocument();
  });
});
