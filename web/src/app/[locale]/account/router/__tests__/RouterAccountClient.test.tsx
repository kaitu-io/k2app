/**
 * `/account/router`「我的路由器」页面行为测试（spec §4.4）。
 *
 * next-intl 用恒等函数替身（`t.has` 恒为 false，region 回落显示 slug 不影响这里的用例），
 * 所以断言全部锚定 i18n 键的完整点分路径——这比断言本地化文案更强，能同时抓组件和
 * messages/*.json 里的层级 typo。
 */
import { render, screen, waitFor, fireEvent, act } from '@testing-library/react';
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import RouterAccountClient from '../RouterAccountClient';
import { buildInstallCommand } from '@/lib/router-edition';
import type { UserRouterFulfillment, UserRouterLine } from '@/lib/api';

const mockGetUserRouter = vi.fn();
const mockMintGatewayCredential = vi.fn();

vi.mock('next-intl', () => {
  const t = (key: string, values?: Record<string, unknown>) =>
    values ? `${key}:${JSON.stringify(values)}` : key;
  t.has = () => false;
  return {
    useTranslations: () => t,
    useLocale: () => 'zh-CN',
  };
});

vi.mock('@/i18n/routing', () => ({
  Link: ({ href, children, ...rest }: { href: string; children: React.ReactNode }) => (
    <a href={href} {...rest}>
      {children}
    </a>
  ),
}));

vi.mock('sonner', () => ({ toast: { success: vi.fn(), error: vi.fn(), info: vi.fn() } }));

vi.mock('@/lib/api', async (importOriginal) => {
  const actual = await importOriginal<typeof import('@/lib/api')>();
  return {
    ...actual,
    api: {
      getUserRouter: (...a: unknown[]) => mockGetUserRouter(...a),
      mintGatewayCredential: (...a: unknown[]) => mockMintGatewayCredential(...a),
    },
  };
});

const BYO_FULFILLMENT: UserRouterFulfillment = {
  id: 2,
  orderId: 2,
  hardwareSku: '',
  stage: 'ready',
  shippedAt: 0,
  activatedAt: 0,
  credentialMinted: false,
  canMintCredential: true,
  createdAt: 1786000000,
};

const HARDWARE_FULFILLMENT: UserRouterFulfillment = {
  id: 1,
  orderId: 1,
  hardwareSku: 'redmi-ax6s',
  stage: 'shipped',
  trackingNo: 'SF123',
  carrier: 'SF',
  shippedAt: 1786000000,
  activatedAt: 0,
  credentialMinted: false,
  canMintCredential: false,
  createdAt: 1786000000,
};

const LINE_NORMAL: UserRouterLine = {
  id: 1,
  status: 'active',
  isServiceable: true,
  region: 'ap-tokyo',
  ipType: 'non_residential',
  trafficTotalBytes: 2 * 1024 ** 4,
  trafficUsedBytes: 1024 ** 4,
  purchasedAt: 1786000000,
  expiresAt: 1786000000 + 365 * 86400,
  graceUntil: 0,
  suspendUntil: 0,
  planLabel: 'router-std-1y',
  quotaExhausted: false,
  quotaResetAt: 0,
};

const LINE_DANGER: UserRouterLine = {
  ...LINE_NORMAL,
  trafficTotalBytes: 1000,
  trafficUsedBytes: 960,
};

describe('RouterAccountClient', () => {
  beforeEach(() => {
    vi.clearAllMocks();
    Object.defineProperty(navigator, 'clipboard', {
      value: { writeText: vi.fn().mockResolvedValue(undefined) },
      configurable: true,
    });
  });
  afterEach(() => {
    vi.useRealTimers();
  });

  it('hasRouter=false 时渲染空状态卡，按钮跳转 /routers', async () => {
    mockGetUserRouter.mockResolvedValue({ hasRouter: false });
    render(<RouterAccountClient />);

    await screen.findByText('routers.edition.account.emptyTitle');
    const link = screen.getByRole('link', { name: 'routers.edition.account.emptyCta' });
    expect(link).toHaveAttribute('href', '/routers');
  });

  it('成品 shipped：展示物流信息，不渲染安装卡', async () => {
    mockGetUserRouter.mockResolvedValue({ hasRouter: true, fulfillment: HARDWARE_FULFILLMENT });
    render(<RouterAccountClient />);

    await screen.findByText('routers.edition.account.stage.shipped');
    expect(screen.getByText(/SF123/)).toBeTruthy();
    expect(screen.queryByText('routers.edition.account.installTitle')).toBeNull();
  });

  it('自备 + 可生成凭证：点击生成后展示安装命令，并重新拉取一次路由器数据', async () => {
    mockGetUserRouter.mockResolvedValue({ hasRouter: true, fulfillment: BYO_FULFILLMENT });
    mockMintGatewayCredential.mockResolvedValue({ url: "k2subs://u:t@h/api/subs" });
    render(<RouterAccountClient />);

    const genButton = await screen.findByRole('button', { name: 'routers.edition.account.generate' });
    fireEvent.click(genButton);

    await waitFor(() => expect(mockMintGatewayCredential).toHaveBeenCalledTimes(1));
    const expectedCommand = buildInstallCommand(window.location.origin, "k2subs://u:t@h/api/subs");
    await screen.findByText(expectedCommand);
    await waitFor(() => expect(mockGetUserRouter).toHaveBeenCalledTimes(2));
  });

  it('自备 + 已生成凭证：点击重新生成先弹确认框，未确认不铸造，确认后铸造一次', async () => {
    mockGetUserRouter.mockResolvedValue({
      hasRouter: true,
      fulfillment: { ...BYO_FULFILLMENT, credentialMinted: true },
    });
    mockMintGatewayCredential.mockResolvedValue({ url: "k2subs://u2:t2@h/api/subs" });
    render(<RouterAccountClient />);

    const regenButton = await screen.findByRole('button', { name: 'routers.edition.account.regenerate' });
    fireEvent.click(regenButton);

    await screen.findByText('routers.edition.account.regenerateConfirmTitle');
    expect(mockMintGatewayCredential).not.toHaveBeenCalled();

    fireEvent.click(screen.getByRole('button', { name: 'routers.edition.account.confirm' }));
    await waitFor(() => expect(mockMintGatewayCredential).toHaveBeenCalledTimes(1));
  });

  it('自备 + 线路未就绪（canMintCredential=false）：展示 installPending，没有生成按钮', async () => {
    mockGetUserRouter.mockResolvedValue({
      hasRouter: true,
      fulfillment: { ...BYO_FULFILLMENT, canMintCredential: false },
    });
    render(<RouterAccountClient />);

    await screen.findByText('routers.edition.account.installPending');
    expect(screen.queryByRole('button', { name: 'routers.edition.account.generate' })).toBeNull();
  });

  it('用量达到总量 96%：用量条外层标记 data-level="danger"', async () => {
    mockGetUserRouter.mockResolvedValue({ hasRouter: true, line: LINE_DANGER });
    const { container } = render(<RouterAccountClient />);

    await waitFor(() => expect(container.querySelector('[data-level="danger"]')).toBeTruthy());
  });

  it('stage=provisioning：30 秒后静默轮询一次（第 2 次调用）', async () => {
    vi.useFakeTimers();
    mockGetUserRouter.mockResolvedValue({
      hasRouter: true,
      fulfillment: { ...BYO_FULFILLMENT, stage: 'provisioning', canMintCredential: false },
    });
    render(<RouterAccountClient />);

    await act(async () => {
      await vi.advanceTimersByTimeAsync(100);
    });
    expect(mockGetUserRouter).toHaveBeenCalledTimes(1);

    await act(async () => {
      await vi.advanceTimersByTimeAsync(30000);
    });
    expect(mockGetUserRouter).toHaveBeenCalledTimes(2);
  });

  it('stage=online：30 秒后仍只有 1 次调用（不轮询）', async () => {
    vi.useFakeTimers();
    mockGetUserRouter.mockResolvedValue({
      hasRouter: true,
      fulfillment: { ...BYO_FULFILLMENT, stage: 'online', canMintCredential: false },
    });
    render(<RouterAccountClient />);

    await act(async () => {
      await vi.advanceTimersByTimeAsync(100);
    });
    expect(mockGetUserRouter).toHaveBeenCalledTimes(1);

    await act(async () => {
      await vi.advanceTimersByTimeAsync(30000);
    });
    expect(mockGetUserRouter).toHaveBeenCalledTimes(1);
  });

  it('renewPlanPid 存在时，续费按钮链接到 /purchase/router?plan=svc', async () => {
    mockGetUserRouter.mockResolvedValue({
      hasRouter: true,
      line: LINE_NORMAL,
      renewPlanPid: 'router-svc-1y',
    });
    render(<RouterAccountClient />);

    const link = await screen.findByRole('link', { name: 'routers.edition.account.renew' });
    expect(link).toHaveAttribute('href', '/purchase/router?plan=svc');
  });

  it('加载失败：展示 loadFailed 与重试按钮，点击重试后再次请求', async () => {
    mockGetUserRouter.mockRejectedValueOnce(new Error('boom'));
    render(<RouterAccountClient />);

    await screen.findByText('routers.edition.account.loadFailed');

    mockGetUserRouter.mockResolvedValueOnce({ hasRouter: false });
    fireEvent.click(screen.getByRole('button', { name: 'routers.edition.account.retry' }));

    await waitFor(() => expect(mockGetUserRouter).toHaveBeenCalledTimes(2));
    await screen.findByText('routers.edition.account.emptyTitle');
  });
});
