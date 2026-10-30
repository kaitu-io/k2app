/**
 * 数据统计「试用统计」tab：读 /app/stats/trials，合计卡片、按开始日表格、点行展开名单。
 */
import { render, screen, fireEvent, waitFor, within } from '@testing-library/react';
import { describe, it, expect, vi, beforeEach } from 'vitest';
import type { TrialCohortResponse, TrialCohortRow } from '@/lib/api';

const mockGetTrialCohorts = vi.fn();

// Radix Select 在 jsdom 下不可靠，替身为原生 <select>（同 private-node-subscriptions 测试）。
vi.mock('@/components/ui/select', () => ({
  Select: ({
    value,
    onValueChange,
    children,
  }: {
    value: string;
    onValueChange: (v: string) => void;
    children: React.ReactNode;
  }) => (
    <select aria-label="时间范围" value={value} onChange={(e) => onValueChange(e.target.value)}>
      {children}
    </select>
  ),
  SelectTrigger: ({ children }: { children: React.ReactNode }) => <>{children}</>,
  SelectValue: () => null,
  SelectContent: ({ children }: { children: React.ReactNode }) => <>{children}</>,
  SelectItem: ({ value, children }: { value: string; children: React.ReactNode }) => (
    <option value={value}>{children}</option>
  ),
}));

vi.mock('@/lib/api', async (importOriginal) => {
  const actual = await importOriginal<typeof import('@/lib/api')>();
  return {
    ...actual,
    api: { getTrialCohorts: (...a: unknown[]) => mockGetTrialCohorts(...a) },
  };
});

import { TrialStatsTab } from '../trial-stats-tab';

function row(cohort: string, over: Partial<TrialCohortRow> = {}): TrialCohortRow {
  return {
    cohort,
    started: 0,
    inTrialRenewOn: 0,
    inTrialRenewOff: 0,
    awaitingCharge: 0,
    billingRetry: 0,
    converted: 0,
    endedUnpaid: 0,
    revoked: 0,
    settled: 0,
    settledConverted: 0,
    conversionRate: null,
    paidAmount: 0,
    refundedAmount: 0,
    ...over,
  };
}

function response(): TrialCohortResponse {
  return {
    rows: [
      row('2026-10-20', { started: 4, converted: 1, endedUnpaid: 3, settled: 4, settledConverted: 1, conversionRate: 0.25, paidAmount: 3990 }),
      row('2026-10-29', { started: 3, inTrialRenewOn: 2, inTrialRenewOff: 1 }),
    ],
    totals: row('total', {
      started: 7, inTrialRenewOn: 2, inTrialRenewOff: 1, converted: 1, endedUnpaid: 3,
      settled: 4, settledConverted: 1, conversionRate: 0.25, paidAmount: 3990,
    }),
    note: '口径说明',
  };
}

describe('TrialStatsTab', () => {
  beforeEach(() => {
    mockGetTrialCohorts.mockReset();
  });

  it('按品牌和 30 天请求，渲染合计与每批', async () => {
    mockGetTrialCohorts.mockResolvedValue(response());
    render(<TrialStatsTab brand="kaitu" />);

    await screen.findByText('2026-10-29');
    expect(mockGetTrialCohorts).toHaveBeenCalledWith({ brand: 'kaitu', days: 30 });
    expect(screen.getByText('续费开 2 | 已关 1')).toBeInTheDocument();
    expect(screen.getByText('已结算 4 条中付费 1')).toBeInTheDocument();
    expect(screen.getAllByText('25.0%').length).toBeGreaterThan(0);
    expect(screen.getByText('口径说明')).toBeInTheDocument();

    // 最新的批次排在前面；没有已结算链的批次转化率显示 "-"
    const rows = screen.getAllByRole('row');
    const first = rows.findIndex((r) => within(r).queryByText('2026-10-29'));
    const second = rows.findIndex((r) => within(r).queryByText('2026-10-20'));
    expect(first).toBeLessThan(second);
    expect(within(rows[first]).getByText('-')).toBeInTheDocument();
  });

  it('点一行加载该批名单，用户链接到详情页', async () => {
    mockGetTrialCohorts.mockImplementation(async (p: { cohort?: string }) =>
      p.cohort
        ? {
            ...response(),
            chains: [
              {
                userId: 9, userUuid: 'u-abc', startedAt: 1793000000, trialEndsAt: 1793600000,
                state: 'converted', autoRenew: true, paidAmount: 3990, refundedAmount: 0, planTitle: '年付',
              },
            ],
          }
        : response(),
    );
    render(<TrialStatsTab />);

    fireEvent.click(await screen.findByText('2026-10-20'));
    await waitFor(() =>
      expect(mockGetTrialCohorts).toHaveBeenCalledWith({ brand: undefined, days: 30, cohort: '2026-10-20' }),
    );
    const link = await screen.findByRole('link', { name: 'u-abc' });
    expect(link).toHaveAttribute('href', '/manager/users/detail?uuid=u-abc');
    expect(screen.getAllByText('已转付费')).toHaveLength(2); // 合计卡片 + 名单状态
    expect(screen.getByText('年付')).toBeInTheDocument();
  });

  it('切换时间范围重新请求', async () => {
    mockGetTrialCohorts.mockResolvedValue(response());
    render(<TrialStatsTab />);
    await screen.findByText('2026-10-29');
    fireEvent.change(screen.getByLabelText('时间范围'), { target: { value: '90' } });
    await waitFor(() => expect(mockGetTrialCohorts).toHaveBeenCalledWith({ brand: undefined, days: 90 }));
  });

  it('无权限时给出提示而不是崩溃', async () => {
    mockGetTrialCohorts.mockRejectedValue(new Error('403'));
    render(<TrialStatsTab />);
    expect(await screen.findByText('加载失败（需要营销权限）')).toBeInTheDocument();
  });
});
