/**
 * `/manager/router-fulfillments` 主台账页：看板、筛选、表格、发货 / 代铸凭证 / 备注对话框。
 */
import { render, screen, fireEvent, waitFor, within } from '@testing-library/react';
import { describe, it, expect, vi, beforeEach } from 'vitest';
import type { AdminRouterFulfillmentItem, AdminRouterStats } from '@/lib/api';

const mockListRouterFulfillments = vi.fn();
const mockGetRouterStats = vi.fn();
const mockShipRouterFulfillment = vi.fn();
const mockMintRouterCredential = vi.fn();
const mockUpdateRouterFulfillmentNote = vi.fn();

const routerState = vi.hoisted(() => ({ current: { push: vi.fn() } }));
const searchParamsState = vi.hoisted(() => ({ current: new URLSearchParams() }));

vi.mock('next/navigation', () => ({
  useRouter: () => routerState.current,
  useSearchParams: () => searchParamsState.current,
}));

vi.mock('sonner', () => ({ toast: { success: vi.fn(), error: vi.fn() } }));

// Radix Select 在 jsdom 下的指针事件行为不可靠，替身为原生 <select>（与
// RouterPurchaseClient.test.tsx 同一约定）。
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
    <select aria-label="阶段筛选" value={value} onChange={(e) => onValueChange(e.target.value)}>
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
      listRouterFulfillments: (...a: unknown[]) => mockListRouterFulfillments(...a),
      getRouterStats: (...a: unknown[]) => mockGetRouterStats(...a),
      shipRouterFulfillment: (...a: unknown[]) => mockShipRouterFulfillment(...a),
      mintRouterCredential: (...a: unknown[]) => mockMintRouterCredential(...a),
      updateRouterFulfillmentNote: (...a: unknown[]) => mockUpdateRouterFulfillmentNote(...a),
    },
  };
});

// Import after the mock so we get the real ApiError/ErrorCode.
import { ApiError, ErrorCode } from '@/lib/api';
import { toast } from 'sonner';
import RouterFulfillmentsPage from '../page.kaitu';

function items(): AdminRouterFulfillmentItem[] {
  return [
    {
      id: 1,
      orderId: 101,
      hardwareSku: 'redmi-ax6s',
      stage: 'ready',
      shippedAt: 0,
      activatedAt: 0,
      credentialMinted: false,
      canMintCredential: true,
      createdAt: 1_700_000_000,
      userId: 11,
      email: 'user1@example.com',
      subId: 501,
      note: '',
      updatedBy: '',
      updatedAt: 1_700_000_000,
      shipping: { name: '张三', phone: '13800000000', address: '某市某路 1 号' },
    },
    {
      id: 2,
      orderId: 102,
      hardwareSku: '',
      stage: 'online',
      shippedAt: 0,
      activatedAt: 0,
      credentialMinted: true,
      canMintCredential: true,
      createdAt: 1_700_000_100,
      userId: 22,
      email: 'user2@example.com',
      subId: 502,
      note: '',
      updatedBy: '',
      updatedAt: 1_700_000_100,
      line: {
        id: 9,
        status: 'active',
        isServiceable: true,
        region: 'ap-tokyo',
        ipType: 'non_residential',
        trafficTotalBytes: 1024,
        trafficUsedBytes: 512,
        purchasedAt: 0,
        expiresAt: 1_900_000_000,
        graceUntil: 0,
        suspendUntil: 0,
        planLabel: 'x',
        quotaExhausted: false,
        quotaResetAt: 0,
      },
      device: { udid: 'd2', appVersion: '1.0', appArch: 'arm64', lastSeenAt: 1_700_000_200, online: true },
    },
  ];
}

function stats(overrides: Partial<AdminRouterStats> = {}): AdminRouterStats {
  return {
    stageCounts: { paid: 0, provisioning: 0, ready: 1, shipped: 0, online: 1, expired: 0 },
    stuck: 2,
    onlineRouters: 3,
    expiringSoon: 1,
    ...overrides,
  };
}

async function findRow(email: string) {
  const cell = await screen.findByText(email);
  return cell.closest('tr')!;
}

describe('/manager/router-fulfillments', () => {
  beforeEach(() => {
    vi.clearAllMocks();
    routerState.current = { push: vi.fn() };
    searchParamsState.current = new URLSearchParams();
    mockListRouterFulfillments.mockResolvedValue({
      items: items(),
      pagination: { page: 1, pageSize: 50, total: 2 },
    });
    mockGetRouterStats.mockResolvedValue(stats());
  });

  it('渲染看板：卡住数字与在线路由器数字', async () => {
    render(<RouterFulfillmentsPage />);
    await screen.findByText('user1@example.com');

    expect(within(screen.getByTestId('stat-stuck')).getByText('2')).toBeInTheDocument();
    expect(within(screen.getByTestId('stat-online')).getByText('3')).toBeInTheDocument();
  });

  it('表格显示两行邮箱；仅成品 ready 行有发货按钮；自备行机型显示自备', async () => {
    render(<RouterFulfillmentsPage />);
    const row1 = await findRow('user1@example.com');
    const row2 = await findRow('user2@example.com');

    expect(within(row1).getByRole('button', { name: '发货' })).toBeInTheDocument();
    expect(within(row2).queryByRole('button', { name: '发货' })).toBeNull();
    expect(within(row2).getByText('自备')).toBeInTheDocument();
  });

  it('发货：单号为空禁用提交；提交时省略空 note；成功后刷新列表', async () => {
    mockShipRouterFulfillment.mockResolvedValue(undefined);
    render(<RouterFulfillmentsPage />);
    const row1 = await findRow('user1@example.com');
    fireEvent.click(within(row1).getByRole('button', { name: '发货' }));

    const submit = await screen.findByRole('button', { name: '确认发货' });
    expect(submit).toBeDisabled();

    fireEvent.change(screen.getByLabelText('快递单号'), { target: { value: 'SF123' } });
    expect(submit).not.toBeDisabled();

    fireEvent.click(submit);

    await waitFor(() =>
      expect(mockShipRouterFulfillment).toHaveBeenCalledWith(1, { trackingNo: 'SF123', carrier: '顺丰' })
    );
    await waitFor(() => expect(mockListRouterFulfillments).toHaveBeenCalledTimes(2));
  });

  it('代铸凭证：确认前不调用后端，确认后展示 URL，关闭对话框后 URL 不再出现', async () => {
    mockMintRouterCredential.mockResolvedValue({ url: 'k2subs://u:t@h/api/subs', deviceId: 9 });
    render(<RouterFulfillmentsPage />);
    const row1 = await findRow('user1@example.com');
    fireEvent.click(within(row1).getByRole('button', { name: '代铸凭证' }));

    await screen.findByText(/已装好的路由器会立即断开/);
    expect(mockMintRouterCredential).not.toHaveBeenCalled();

    fireEvent.click(screen.getByRole('button', { name: '确认生成' }));

    await waitFor(() => expect(mockMintRouterCredential).toHaveBeenCalledWith(1));
    await screen.findByText('k2subs://u:t@h/api/subs');

    fireEvent.click(screen.getByRole('button', { name: '关闭' }));
    await waitFor(() => expect(screen.queryByText('k2subs://u:t@h/api/subs')).toBeNull());
  });

  it('发货失败：toast 显示后端错误码对应的中文提示', async () => {
    mockShipRouterFulfillment.mockRejectedValue(new ApiError(ErrorCode.InvalidOperation, 'x'));
    render(<RouterFulfillmentsPage />);
    const row1 = await findRow('user1@example.com');
    fireEvent.click(within(row1).getByRole('button', { name: '发货' }));
    fireEvent.change(screen.getByLabelText('快递单号'), { target: { value: 'SF123' } });
    fireEvent.click(screen.getByRole('button', { name: '确认发货' }));

    await waitFor(() => expect(toast.error).toHaveBeenCalledWith('请求参数错误'));
  });

  it('阶段筛选：选择「线路就绪」后 URL 带 stage=ready 且 page=1', async () => {
    render(<RouterFulfillmentsPage />);
    await screen.findByText('user1@example.com');

    fireEvent.change(screen.getByLabelText('阶段筛选'), { target: { value: 'ready' } });

    await waitFor(() => expect(routerState.current.push).toHaveBeenCalled());
    const calls = (routerState.current.push as ReturnType<typeof vi.fn>).mock.calls;
    const url = calls[calls.length - 1][0] as string;
    expect(url).toContain('stage=ready');
    expect(url).toContain('page=1');
  });
});
