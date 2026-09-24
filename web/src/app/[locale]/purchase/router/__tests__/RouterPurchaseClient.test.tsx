/**
 * `/purchase/router` 结账页 — 新购只卖成品（无「只买服务」开关）、地区下拉、收货信息、
 * 预览 debounce、一户一台门（InvalidOperation）、?plan=svc 续费模式（仅已有路由器版的客户）的行为测试。
 *
 * 外围重依赖（Header/Footer/PurchaseStep1/next-intl/next/navigation/Select）替身掉，
 * 被测的选择/收货/预览/下单/错误处理逻辑本身跑真实代码。
 */
import { render, screen, fireEvent, waitFor } from '@testing-library/react';
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import RouterPurchaseClient from '../RouterPurchaseClient';
import type { Plan } from '@/lib/api';

const mockGetProductPlans = vi.fn();
const mockCreateOrder = vi.fn();
const mockGetUserRouter = vi.fn();

const authState = vi.hoisted(() => ({ current: { isAuthenticated: true, isAuthLoading: false } }));
const routerState = vi.hoisted(() => ({ current: { push: vi.fn(), replace: vi.fn() } }));
const searchParamsState = vi.hoisted(() => ({ current: new URLSearchParams() }));

vi.mock('next-intl', () => {
  const t = (key: string, values?: Record<string, unknown>) =>
    values ? `${key}:${JSON.stringify(values)}` : key;
  t.has = () => true;
  return {
    useTranslations: () => t,
    useLocale: () => 'zh-CN',
  };
});

vi.mock('next/navigation', () => ({
  useSearchParams: () => searchParamsState.current,
}));

vi.mock('@/i18n/routing', () => ({
  useRouter: () => routerState.current,
  Link: ({ href, children, ...rest }: { href: string; children: React.ReactNode }) => (
    <a href={href} {...rest}>
      {children}
    </a>
  ),
}));

vi.mock('@/contexts/AuthContext', () => ({
  useAuth: () => authState.current,
}));

vi.mock('@/hooks/useEmbedMode', () => ({
  useEmbedMode: () => ({ showNavigation: false, showFooter: false }),
}));

vi.mock('sonner', () => ({ toast: { success: vi.fn(), error: vi.fn(), info: vi.fn() } }));

vi.mock('@/components/Header', () => ({ default: () => null }));
vi.mock('@/components/Footer', () => ({ default: () => null }));
vi.mock('@/components/PurchaseStep1', () => ({ default: () => null }));

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
    <select aria-label="region" value={value} onChange={(e) => onValueChange(e.target.value)}>
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
      getProductPlans: (...a: unknown[]) => mockGetProductPlans(...a),
      createOrder: (...a: unknown[]) => mockCreateOrder(...a),
      getUserRouter: (...a: unknown[]) => mockGetUserRouter(...a),
    },
  };
});

// Import after the mock is registered so we get the real ApiError/ErrorCode.
import { ApiError, ErrorCode } from '@/lib/api';

const HW: Plan = {
  pid: 'router-std-1y',
  label: 'x',
  price: 39900,
  originPrice: 39900,
  month: 12,
  highlight: true,
  product: 'router',
  hardwareSku: 'redmi-ax6s',
  privateNode: {
    ipType: 'non_residential',
    allowedRegions: ['ap-tokyo', 'ap-singapore'],
    trafficTotalBytes: 2 * 1024 ** 4,
  },
};
const SVC: Plan = { ...HW, pid: 'router-svc-1y', price: 29900, highlight: false, hardwareSku: '' };

const HW_ONE_REGION: Plan = {
  ...HW,
  privateNode: { ...HW.privateNode!, allowedRegions: ['ap-tokyo'] },
};
const SVC_ONE_REGION: Plan = { ...SVC, privateNode: { ...SVC.privateNode!, allowedRegions: ['ap-tokyo'] } };

function fakeOrder(payAmount: number) {
  return { uuid: 'o-1', title: 't', originAmount: payAmount, campaignReduceAmount: 0, payAmount };
}

describe('RouterPurchaseClient', () => {
  beforeEach(() => {
    vi.clearAllMocks();
    // 既有用例描述的是发售后的正常售卖（按钮「去支付」、无预售说明）。预售窗口按真实日期判断，
    // 这里把日期钉在发售之后；预售行为在下面的「预售」describe 里单独设时间。
    vi.useFakeTimers({ toFake: ['Date'] });
    vi.setSystemTime(new Date('2026-11-12T12:00:00+08:00'));
    authState.current = { isAuthenticated: true, isAuthLoading: false };
    searchParamsState.current = new URLSearchParams();
    mockCreateOrder.mockResolvedValue({ order: fakeOrder(39900), payUrl: 'https://pay.test/x' });
    mockGetUserRouter.mockResolvedValue({ hasRouter: false });
  });
  afterEach(() => {
    vi.useRealTimers();
  });

  it('默认选中成品：显示硬件名、$399、收货信息标题与含 $299 的续费说明', async () => {
    mockGetProductPlans.mockResolvedValue({ items: [HW, SVC] });
    render(<RouterPurchaseClient />);

    await screen.findByText('routers.edition.purchase.hardwareName');
    expect(screen.getAllByText(/\$399/).length).toBeGreaterThan(0);
    expect(screen.getByText('routers.edition.purchase.shippingTitle')).toBeTruthy();

    const renewNote = await screen.findByText(/routers\.edition\.purchase\.renewNote/);
    expect(renewNote).toHaveTextContent('$299');
  });

  it('新购只有成品：没有「只买服务」开关，也不展示服务套餐名', async () => {
    mockGetProductPlans.mockResolvedValue({ items: [HW, SVC] });
    render(<RouterPurchaseClient />);

    await screen.findByText('routers.edition.purchase.hardwareName');
    expect(screen.queryByText('routers.edition.purchase.switchToService')).toBeNull();
    expect(screen.queryByText('routers.edition.purchase.switchToHardware')).toBeNull();
    expect(screen.queryByText('routers.edition.purchase.serviceName')).toBeNull();
    expect(screen.queryByText('routers.edition.purchase.renewName')).toBeNull();
  });

  it('成品套餐收货信息未填时支付按钮 disabled，填好后 enabled', async () => {
    mockGetProductPlans.mockResolvedValue({ items: [HW, SVC] });
    render(<RouterPurchaseClient />);

    await screen.findByText('routers.edition.purchase.hardwareName');
    const payButton = screen.getByRole('button', { name: 'routers.edition.purchase.payButton' });
    expect(payButton).toBeDisabled();

    fireEvent.change(screen.getByLabelText('routers.edition.purchase.shippingName'), {
      target: { value: '张三' },
    });
    fireEvent.change(screen.getByLabelText('routers.edition.purchase.shippingPhone'), {
      target: { value: '13800000000' },
    });
    fireEvent.change(screen.getByLabelText('routers.edition.purchase.shippingAddress'), {
      target: { value: '某某路 1 号' },
    });

    await waitFor(() => expect(payButton).not.toBeDisabled());
  });

  it('点支付：createOrder 收到 trim 后的收货信息（成品套餐）', async () => {
    mockGetProductPlans.mockResolvedValue({ items: [HW, SVC] });
    render(<RouterPurchaseClient />);

    await screen.findByText('routers.edition.purchase.hardwareName');
    fireEvent.change(screen.getByLabelText('routers.edition.purchase.shippingName'), {
      target: { value: '  张三  ' },
    });
    fireEvent.change(screen.getByLabelText('routers.edition.purchase.shippingPhone'), {
      target: { value: ' 13800000000 ' },
    });
    fireEvent.change(screen.getByLabelText('routers.edition.purchase.shippingAddress'), {
      target: { value: ' 某某路 1 号 ' },
    });

    const payButton = screen.getByRole('button', { name: 'routers.edition.purchase.payButton' });
    await waitFor(() => expect(payButton).not.toBeDisabled());
    mockCreateOrder.mockClear();
    fireEvent.click(payButton);

    await waitFor(() =>
      expect(mockCreateOrder).toHaveBeenCalledWith(
        {
          preview: false,
          plan: 'router-std-1y',
          region: 'ap-tokyo',
          campaignCode: undefined,
          shipping: { name: '张三', phone: '13800000000', address: '某某路 1 号' },
        },
        { autoRedirectToAuth: false },
      ),
    );
  });

  it('预览请求不带 shipping：切换地区后 debounce 触发新预览', async () => {
    mockGetProductPlans.mockResolvedValue({ items: [HW, SVC] });
    render(<RouterPurchaseClient />);

    await screen.findByText('routers.edition.purchase.hardwareName');
    await waitFor(
      () =>
        expect(mockCreateOrder).toHaveBeenCalledWith(
          { preview: true, plan: 'router-std-1y', region: 'ap-tokyo' },
          { autoRedirectToAuth: false },
        ),
      { timeout: 3000 },
    );

    mockCreateOrder.mockClear();
    const select = screen.getByRole('combobox');
    fireEvent.change(select, { target: { value: 'ap-singapore' } });

    await waitFor(
      () =>
        expect(mockCreateOrder).toHaveBeenCalledWith(
          { preview: true, plan: 'router-std-1y', region: 'ap-singapore' },
          { autoRedirectToAuth: false },
        ),
      { timeout: 3000 },
    );
    const call = mockCreateOrder.mock.calls.find((c) => c[0].region === 'ap-singapore');
    expect(call?.[0]).not.toHaveProperty('shipping');
  });

  it('后端返回 InvalidOperation：显示 alreadyHasRouter 与指向 /account/router 的链接', async () => {
    mockGetProductPlans.mockResolvedValue({ items: [HW, SVC] });
    mockCreateOrder.mockImplementation((req: { preview: boolean }) => {
      if (req.preview) return Promise.resolve({ order: fakeOrder(39900), payUrl: '' });
      return Promise.reject(new ApiError(ErrorCode.InvalidOperation, 'x'));
    });
    render(<RouterPurchaseClient />);

    await screen.findByText('routers.edition.purchase.hardwareName');
    fireEvent.change(screen.getByLabelText('routers.edition.purchase.shippingName'), {
      target: { value: '张三' },
    });
    fireEvent.change(screen.getByLabelText('routers.edition.purchase.shippingPhone'), {
      target: { value: '13800000000' },
    });
    fireEvent.change(screen.getByLabelText('routers.edition.purchase.shippingAddress'), {
      target: { value: '某某路 1 号' },
    });
    const payButton = screen.getByRole('button', { name: 'routers.edition.purchase.payButton' });
    await waitFor(() => expect(payButton).not.toBeDisabled());
    fireEvent.click(payButton);

    await screen.findByText('routers.edition.purchase.alreadyHasRouter');
    const link = screen.getByRole('link', { name: 'routers.edition.purchase.goMyRouter' });
    expect(link).toHaveAttribute('href', '/account/router');
  });

  it('被一户一台拒单后，换出口地区会清掉旧提示', async () => {
    mockGetProductPlans.mockResolvedValue({ items: [HW, SVC] });
    mockCreateOrder.mockImplementation((req: { preview: boolean }) => {
      if (req.preview) return Promise.resolve({ order: fakeOrder(39900), payUrl: '' });
      return Promise.reject(new ApiError(ErrorCode.InvalidOperation, 'x'));
    });
    render(<RouterPurchaseClient />);

    await screen.findByText('routers.edition.purchase.hardwareName');
    fireEvent.change(screen.getByLabelText('routers.edition.purchase.shippingName'), {
      target: { value: '张三' },
    });
    fireEvent.change(screen.getByLabelText('routers.edition.purchase.shippingPhone'), {
      target: { value: '13800000000' },
    });
    fireEvent.change(screen.getByLabelText('routers.edition.purchase.shippingAddress'), {
      target: { value: '某某路 1 号' },
    });
    const payButton = screen.getByRole('button', { name: 'routers.edition.purchase.payButton' });
    await waitFor(() => expect(payButton).not.toBeDisabled());
    fireEvent.click(payButton);

    await screen.findByText('routers.edition.purchase.alreadyHasRouter');

    fireEvent.change(screen.getByRole('combobox'), { target: { value: 'ap-singapore' } });

    await waitFor(() =>
      expect(screen.queryByText('routers.edition.purchase.alreadyHasRouter')).toBeNull(),
    );
  });

  it('未登录：支付按钮 disabled 且显示 loginFirst', async () => {
    authState.current = { isAuthenticated: false, isAuthLoading: false };
    mockGetProductPlans.mockResolvedValue({ items: [HW, SVC] });
    render(<RouterPurchaseClient />);

    await screen.findByText('routers.edition.purchase.hardwareName');
    expect(screen.getByText('routers.edition.purchase.loginFirst')).toBeTruthy();
    expect(screen.getByRole('button', { name: 'routers.edition.purchase.payButton' })).toBeDisabled();
  });

  it('地区只有一个时不渲染下拉', async () => {
    mockGetProductPlans.mockResolvedValue({ items: [HW_ONE_REGION, SVC_ONE_REGION] });
    render(<RouterPurchaseClient />);

    await screen.findByText('routers.edition.purchase.hardwareName');
    expect(screen.queryByRole('combobox')).toBeNull();
  });

  describe('?plan=svc 续费模式', () => {
    async function payAndGetOrderCall() {
      const payButton = screen.getByRole('button', { name: 'routers.edition.purchase.payButton' });
      await waitFor(() => expect(payButton).not.toBeDisabled());
      mockCreateOrder.mockClear();
      fireEvent.click(payButton);
      let call: unknown[] | undefined;
      await waitFor(() => {
        call = mockCreateOrder.mock.calls.find((c) => c[0].preview === false);
        expect(call).toBeTruthy();
      });
      return call![0] as Record<string, unknown>;
    }

    it('已有台账（非 expired）：续费标题/方案名，隐藏改买成品与地区选择，下单不传 region', async () => {
      searchParamsState.current = new URLSearchParams('plan=svc');
      mockGetProductPlans.mockResolvedValue({ items: [HW, SVC] });
      mockGetUserRouter.mockResolvedValue({
        hasRouter: true,
        fulfillment: { id: 1, orderId: 1, hardwareSku: 'redmi-ax6s', stage: 'online', shippedAt: 1, activatedAt: 1, credentialMinted: true, canMintCredential: true, createdAt: 1 },
      });
      render(<RouterPurchaseClient />);

      await screen.findByText('routers.edition.purchase.renewTitle');
      expect(mockGetUserRouter).toHaveBeenCalledWith({ autoRedirectToAuth: false });
      expect(screen.queryByText('routers.edition.purchase.title')).toBeNull();
      expect(screen.getByText('routers.edition.purchase.renewSubtitle')).toBeTruthy();
      expect(screen.getByText('routers.edition.purchase.renewName')).toBeTruthy();
      expect(screen.getByText('routers.edition.purchase.renewDesc')).toBeTruthy();
      expect(screen.queryByText('routers.edition.purchase.serviceName')).toBeNull();
      expect(screen.queryByText('routers.edition.purchase.switchToHardware')).toBeNull();
      expect(screen.queryByText('routers.edition.purchase.regionLabel')).toBeNull();
      expect(screen.queryByRole('combobox')).toBeNull();

      const order = await payAndGetOrderCall();
      expect(order.plan).toBe('router-svc-1y');
      expect(order.region).toBeUndefined();
      expect(order).not.toHaveProperty('shipping');
      // 预览同样不带 region
      for (const c of mockCreateOrder.mock.calls) expect(c[0].region).toBeUndefined();
    });

    it('台账已 expired 但线路 suspended（可续）：同样进入续费模式', async () => {
      searchParamsState.current = new URLSearchParams('plan=svc');
      mockGetProductPlans.mockResolvedValue({ items: [HW, SVC] });
      mockGetUserRouter.mockResolvedValue({
        hasRouter: true,
        fulfillment: { id: 1, orderId: 1, hardwareSku: '', stage: 'expired', shippedAt: 0, activatedAt: 0, credentialMinted: true, canMintCredential: false, createdAt: 1 },
        line: { id: 9, status: 'suspended' },
      });
      render(<RouterPurchaseClient />);
      await screen.findByText('routers.edition.purchase.renewTitle');
    });

    it('成品台账已 expired 且线路已回收：硬件仍在客户手里，进入续费模式（与后端下单门同口径）', async () => {
      searchParamsState.current = new URLSearchParams('plan=svc');
      mockGetProductPlans.mockResolvedValue({ items: [HW, SVC] });
      mockGetUserRouter.mockResolvedValue({
        hasRouter: true,
        fulfillment: { id: 1, orderId: 1, hardwareSku: 'redmi-ax6s', stage: 'expired', shippedAt: 1, activatedAt: 1, credentialMinted: true, canMintCredential: false, createdAt: 1 },
      });
      render(<RouterPurchaseClient />);
      await screen.findByText('routers.edition.purchase.renewTitle');
      expect(screen.queryByText('routers.edition.purchase.shippingTitle')).toBeNull();
    });

    // 非续费客户带 ?plan=svc：服务套餐不对新客开放，按含路由器的新购处理。
    async function expectHardwareNewPurchase() {
      await screen.findByText('routers.edition.purchase.hardwareName');
      expect(screen.getByText('routers.edition.purchase.title')).toBeTruthy();
      expect(screen.getByText('routers.edition.purchase.shippingTitle')).toBeTruthy();
      expect(screen.queryByText('routers.edition.purchase.renewTitle')).toBeNull();
      expect(screen.queryByText('routers.edition.purchase.renewName')).toBeNull();
      expect(screen.queryByText('routers.edition.purchase.serviceName')).toBeNull();
      expect(screen.queryByText('routers.edition.purchase.switchToService')).toBeNull();
      expect(screen.queryByText('routers.edition.purchase.switchToHardware')).toBeNull();
    }

    it('无台账无线路：看到的是成品新购（含收货与地区），没有「只买服务」开关，下单买成品', async () => {
      searchParamsState.current = new URLSearchParams('plan=svc');
      mockGetProductPlans.mockResolvedValue({ items: [HW, SVC] });
      mockGetUserRouter.mockResolvedValue({ hasRouter: false });
      render(<RouterPurchaseClient />);

      await waitFor(() => expect(mockGetUserRouter).toHaveBeenCalled());
      await expectHardwareNewPurchase();
      expect(screen.getByText('routers.edition.purchase.regionLabel')).toBeTruthy();

      fireEvent.change(screen.getByLabelText('routers.edition.purchase.shippingName'), {
        target: { value: '张三' },
      });
      fireEvent.change(screen.getByLabelText('routers.edition.purchase.shippingPhone'), {
        target: { value: '13800000000' },
      });
      fireEvent.change(screen.getByLabelText('routers.edition.purchase.shippingAddress'), {
        target: { value: '某某路 1 号' },
      });
      const order = await payAndGetOrderCall();
      expect(order.plan).toBe('router-std-1y');
      expect(order.region).toBe('ap-tokyo');
      expect(order.shipping).toEqual({ name: '张三', phone: '13800000000', address: '某某路 1 号' });
      for (const c of mockCreateOrder.mock.calls) expect(c[0].plan).toBe('router-std-1y');
    });

    it('自备台账 expired 且线路已回收：不是续费客户，按成品新购（后端同样拒绝服务套餐）', async () => {
      searchParamsState.current = new URLSearchParams('plan=svc');
      mockGetProductPlans.mockResolvedValue({ items: [HW, SVC] });
      mockGetUserRouter.mockResolvedValue({
        hasRouter: true,
        fulfillment: { id: 1, orderId: 1, hardwareSku: '', stage: 'expired', shippedAt: 1, activatedAt: 1, credentialMinted: true, canMintCredential: false, createdAt: 1 },
        line: { id: 9, status: 'deprovisioned' },
      });
      render(<RouterPurchaseClient />);

      await waitFor(() => expect(mockGetUserRouter).toHaveBeenCalled());
      await expectHardwareNewPurchase();
    });

    it('查询失败当作无台账：按成品新购', async () => {
      searchParamsState.current = new URLSearchParams('plan=svc');
      mockGetProductPlans.mockResolvedValue({ items: [HW, SVC] });
      mockGetUserRouter.mockRejectedValue(new Error('boom'));
      render(<RouterPurchaseClient />);

      await waitFor(() => expect(mockGetUserRouter).toHaveBeenCalled());
      await expectHardwareNewPurchase();
    });

    it('检测进行中：即使收货信息已填，支付按钮仍 disabled', async () => {
      searchParamsState.current = new URLSearchParams('plan=svc');
      mockGetProductPlans.mockResolvedValue({ items: [HW, SVC] });
      mockGetUserRouter.mockReturnValue(new Promise(() => {}));
      render(<RouterPurchaseClient />);

      await screen.findByText('routers.edition.purchase.hardwareName');
      fireEvent.change(screen.getByLabelText('routers.edition.purchase.shippingName'), {
        target: { value: '张三' },
      });
      fireEvent.change(screen.getByLabelText('routers.edition.purchase.shippingPhone'), {
        target: { value: '13800000000' },
      });
      fireEvent.change(screen.getByLabelText('routers.edition.purchase.shippingAddress'), {
        target: { value: '某某路 1 号' },
      });
      // 给异步状态一点时间：若门漏掉 pending，按钮会在这里变成可点。
      await new Promise((r) => setTimeout(r, 50));
      expect(screen.getByRole('button', { name: 'routers.edition.purchase.payButton' })).toBeDisabled();
    });

    it('未登录：不请求 getUserRouter，按成品新购', async () => {
      authState.current = { isAuthenticated: false, isAuthLoading: false };
      searchParamsState.current = new URLSearchParams('plan=svc');
      mockGetProductPlans.mockResolvedValue({ items: [HW, SVC] });
      render(<RouterPurchaseClient />);

      await expectHardwareNewPurchase();
      expect(mockGetUserRouter).not.toHaveBeenCalled();
    });

    it('没有 ?plan=svc（默认成品）：不请求 getUserRouter', async () => {
      mockGetProductPlans.mockResolvedValue({ items: [HW, SVC] });
      render(<RouterPurchaseClient />);
      await screen.findByText('routers.edition.purchase.hardwareName');
      expect(mockGetUserRouter).not.toHaveBeenCalled();
    });
  });

  describe('预售', () => {
    const HW_PRESALE: Plan = { ...HW, price: 35900, originPrice: 39900 };

    it('预售期 + 套餐打折：划线原价 $399、实价 $359、预售说明带发货日、按钮为「预订并支付」', async () => {
      vi.useFakeTimers({ toFake: ['Date'] });
      vi.setSystemTime(new Date('2026-10-01T12:00:00+08:00'));
      mockGetProductPlans.mockResolvedValue({ items: [HW_PRESALE, SVC] });
      render(<RouterPurchaseClient />);

      await screen.findByText('routers.edition.purchase.hardwareName');
      expect(screen.getByLabelText(/routers\.edition\.purchase\.originPrice/)).toHaveTextContent('$399');
      expect(screen.getAllByText('$359').length).toBeGreaterThan(0);
      expect(screen.getByTestId('presale-notice')).toHaveTextContent('11月11日');
      expect(screen.getByRole('button', { name: 'routers.edition.purchase.preorderButton' })).toBeTruthy();
      expect(screen.queryByRole('button', { name: 'routers.edition.purchase.payButton' })).toBeNull();
    });

    it('发售后 + 套餐已恢复原价：没有划线、没有预售说明，按钮为「去支付」', async () => {
      vi.useFakeTimers({ toFake: ['Date'] });
      vi.setSystemTime(new Date('2026-11-12T12:00:00+08:00'));
      mockGetProductPlans.mockResolvedValue({ items: [HW, SVC] });
      render(<RouterPurchaseClient />);

      await screen.findByText('routers.edition.purchase.hardwareName');
      expect(screen.queryByLabelText(/routers\.edition\.purchase\.originPrice/)).toBeNull();
      expect(screen.queryByTestId('presale-notice')).toBeNull();
      expect(screen.getByRole('button', { name: 'routers.edition.purchase.payButton' })).toBeTruthy();
    });

    it('预售期的续费模式：不显示预售说明，按钮仍是「去支付」', async () => {
      vi.useFakeTimers({ toFake: ['Date'] });
      vi.setSystemTime(new Date('2026-10-01T12:00:00+08:00'));
      searchParamsState.current = new URLSearchParams('plan=svc');
      mockGetUserRouter.mockResolvedValue({ hasRouter: true, fulfillment: { stage: 'online', hardwareSku: 'redmi-ax6s' } });
      mockGetProductPlans.mockResolvedValue({ items: [HW_PRESALE, SVC] });
      render(<RouterPurchaseClient />);

      await screen.findByText('routers.edition.purchase.renewName');
      expect(screen.queryByTestId('presale-notice')).toBeNull();
      expect(screen.getByRole('button', { name: 'routers.edition.purchase.payButton' })).toBeTruthy();
    });
  });
});
