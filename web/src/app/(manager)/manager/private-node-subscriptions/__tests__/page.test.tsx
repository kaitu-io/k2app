/**
 * `/manager/private-node-subscriptions` 从表页：筛选、延期、停机、跳转台账。
 */
import { render, screen, fireEvent, waitFor, within } from '@testing-library/react';
import { describe, it, expect, vi, beforeEach } from 'vitest';
import type { AdminPrivateNodeSubscriptionItem } from '@/lib/api';

const mockListPrivateNodeSubscriptions = vi.fn();
const mockExtendPrivateNodeSubscription = vi.fn();
const mockCreateNodeOperation = vi.fn();

const routerState = vi.hoisted(() => ({ current: { push: vi.fn() } }));
const searchParamsState = vi.hoisted(() => ({ current: new URLSearchParams() }));

vi.mock('next/navigation', () => ({
  useRouter: () => routerState.current,
  useSearchParams: () => searchParamsState.current,
}));

vi.mock('sonner', () => ({ toast: { success: vi.fn(), error: vi.fn() } }));

// Radix Select 在 jsdom 下的指针事件行为不可靠，替身为原生 <select>（与
// router-fulfillments/__tests__/page.test.tsx 同一约定）。这个页面有两个 Select
// 实例（状态筛选 / 延期月数），用 `name` 区分 aria-label。
vi.mock('@/components/ui/select', () => ({
  Select: ({
    value,
    onValueChange,
    name,
    children,
  }: {
    value: string;
    onValueChange: (v: string) => void;
    name?: string;
    children: React.ReactNode;
  }) => (
    <select
      aria-label={name === 'extendMonths' ? '延期月数' : '状态筛选'}
      value={value}
      onChange={(e) => onValueChange(e.target.value)}
    >
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
    api: {
      listPrivateNodeSubscriptions: (...a: unknown[]) => mockListPrivateNodeSubscriptions(...a),
      extendPrivateNodeSubscription: (...a: unknown[]) => mockExtendPrivateNodeSubscription(...a),
      createNodeOperation: (...a: unknown[]) => mockCreateNodeOperation(...a),
    },
  };
});

// Import after the mock so we get the real ApiError/ErrorCode.
import { ApiError, ErrorCode } from '@/lib/api';
import { toast } from 'sonner';
import PrivateNodeSubscriptionsPage from '../page.kaitu';

function items(): AdminPrivateNodeSubscriptionItem[] {
  return [
    {
      id: 1,
      status: 'active',
      isServiceable: true,
      region: 'ap-tokyo',
      ipType: 'non_residential',
      trafficTotalBytes: 1024,
      trafficUsedBytes: 512,
      purchasedAt: 1_700_000_000,
      expiresAt: 1_900_000_000,
      graceUntil: 0,
      suspendUntil: 0,
      planLabel: 'x',
      quotaExhausted: false,
      quotaResetAt: 0,
      userId: 11,
      email: 'user1@example.com',
      orderId: 501,
      boundIpv4: '1.2.3.4',
    },
    {
      id: 2,
      status: 'deprovisioned',
      isServiceable: false,
      region: 'ap-tokyo',
      ipType: 'non_residential',
      trafficTotalBytes: 1024,
      trafficUsedBytes: 1024,
      purchasedAt: 1_700_000_100,
      expiresAt: 1_800_000_000,
      graceUntil: 0,
      suspendUntil: 0,
      planLabel: 'x',
      quotaExhausted: true,
      quotaResetAt: 0,
      userId: 22,
      email: 'user2@example.com',
      orderId: 502,
    },
  ];
}

async function findRow(email: string) {
  const cell = await screen.findByText(email);
  return cell.closest('tr')!;
}

describe('/manager/private-node-subscriptions', () => {
  beforeEach(() => {
    vi.clearAllMocks();
    routerState.current = { push: vi.fn() };
    searchParamsState.current = new URLSearchParams();
    mockListPrivateNodeSubscriptions.mockResolvedValue({
      items: items(),
      pagination: { page: 1, pageSize: 50, total: 2 },
    });
  });

  it('渲染 active 与 deprovisioned 两行：active 行有延期/停机按钮，deprovisioned 行没有；两行都有台账链接', async () => {
    render(<PrivateNodeSubscriptionsPage />);
    const row1 = await findRow('user1@example.com');
    const row2 = await findRow('user2@example.com');

    expect(within(row1).getByRole('button', { name: '延期' })).toBeInTheDocument();
    expect(within(row1).getByRole('button', { name: '停机' })).toBeInTheDocument();
    expect(within(row2).queryByRole('button', { name: '延期' })).toBeNull();
    expect(within(row2).queryByRole('button', { name: '停机' })).toBeNull();

    expect(within(row1).getByRole('link', { name: '查看台账' })).toHaveAttribute(
      'href',
      '/manager/router-fulfillments?userId=11'
    );
    expect(within(row2).getByRole('link', { name: '查看台账' })).toHaveAttribute(
      'href',
      '/manager/router-fulfillments?userId=22'
    );
  });

  it('延期：原因为空时提交禁用；选 3 个月填原因提交后调用 extendPrivateNodeSubscription 并刷新列表', async () => {
    mockExtendPrivateNodeSubscription.mockResolvedValue({ ...items()[0], expiresAt: 1_950_000_000 });
    render(<PrivateNodeSubscriptionsPage />);
    const row1 = await findRow('user1@example.com');
    fireEvent.click(within(row1).getByRole('button', { name: '延期' }));

    const submit = await screen.findByRole('button', { name: '确认延期' });
    expect(submit).toBeDisabled();

    fireEvent.change(screen.getByLabelText('延期月数'), { target: { value: '3' } });
    fireEvent.change(screen.getByLabelText('原因'), { target: { value: '补偿' } });
    expect(submit).not.toBeDisabled();

    fireEvent.click(submit);

    await waitFor(() =>
      expect(mockExtendPrivateNodeSubscription).toHaveBeenCalledWith(1, { months: 3, reason: '补偿' })
    );
    await waitFor(() => expect(mockListPrivateNodeSubscriptions).toHaveBeenCalledTimes(2));
  });

  it('停机确认后调用 createNodeOperation({subId, action: stop})；Conflict 时提示未完成的运维任务', async () => {
    mockCreateNodeOperation.mockRejectedValueOnce(new ApiError(ErrorCode.Conflict, 'x'));
    render(<PrivateNodeSubscriptionsPage />);
    const row1 = await findRow('user1@example.com');
    fireEvent.click(within(row1).getByRole('button', { name: '停机' }));

    const confirm = await screen.findByRole('button', { name: '确认停机' });
    fireEvent.click(confirm);

    await waitFor(() => expect(mockCreateNodeOperation).toHaveBeenCalledWith({ subId: 1, action: 'stop' }));
    await waitFor(() => expect(toast.error).toHaveBeenCalledWith('该线路已有未完成的运维任务'));
  });

  it('状态筛选：选择宽限期后 URL 带 status=grace 且 page=1', async () => {
    render(<PrivateNodeSubscriptionsPage />);
    await screen.findByText('user1@example.com');

    fireEvent.change(screen.getByLabelText('状态筛选'), { target: { value: 'grace' } });

    await waitFor(() => expect(routerState.current.push).toHaveBeenCalled());
    const calls = (routerState.current.push as ReturnType<typeof vi.fn>).mock.calls;
    const url = calls[calls.length - 1][0] as string;
    const params = new URLSearchParams(url.split('?')[1] || '');
    expect(params.get('status')).toBe('grace');
    expect(params.get('page')).toBe('1');
  });
});
