/**
 * Purchase 预览订单死循环 —— 回归测试。
 *
 * 缺陷（2026-09-11，生产 center 日志暴露）：useUser 的 fetchUser 每次渲染都是新函数，
 * Purchase 把它放进 handleOrder 的依赖（3b584d61，409 互斥兜底要调它），于是
 * 「套餐/优惠码变化时重新预览」的 effect 每次渲染都重跑；预览请求自己又 setIsLoading /
 * setOrderData 触发渲染 → 自激循环。0.4.9 起全平台只要停在购买页就持续打
 * POST /api/user/orders（preview），单台 macOS 设备 5 分钟打了 2 万次。
 *
 * 必须用**真实的 useUser**：其余 Purchase 测试把 useUser 换成返回模块级常量
 * fetchUser 的替身，引用天然稳定，结构上测不出这个 bug。只 mock 网络与 store。
 */

import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen, fireEvent, waitFor } from '@testing-library/react';
import { I18nextProvider } from 'react-i18next';
import { MemoryRouter } from 'react-router-dom';
import i18n from '../../i18n/i18n';
import type { Plan } from '../../services/api-types';

// MUI Dialog / Select 在 jsdom 下会崩（见 Purchase.privateNode.test.tsx）。
vi.mock('@mui/material', async () => {
  const actual = await vi.importActual<typeof import('@mui/material')>('@mui/material');
  return {
    ...actual,
    Dialog: ({ open, children }: any) => (open ? <div role="dialog">{children}</div> : null),
    DialogTitle: ({ children }: any) => <div>{children}</div>,
    DialogContent: ({ children }: any) => <div>{children}</div>,
    DialogActions: ({ children }: any) => <div>{children}</div>,
  };
});

vi.mock('../../services/cloud-api', () => ({
  cloudApi: { get: vi.fn(), post: vi.fn() },
}));

// useUser 直接 import 这两个 store（不经 stores/index）。
vi.mock('../../stores/auth.store', () => ({
  useAuthStore: (selector: (s: { isAuthenticated: boolean }) => unknown) =>
    selector({ isAuthenticated: true }),
}));
vi.mock('../../stores/config.store', () => ({
  useConfigStore: { getState: () => ({ setDetectedProfile: vi.fn() }) },
}));

const showAlert = vi.fn();
vi.mock('../../stores', () => ({
  useAlert: () => ({ showAlert }),
  useAuthStore: (selector: (s: any) => any) => selector({ isAuthenticated: true }),
}));
// 这个 mock 的函数必须**引用稳定**：生产里 open 是 zustand 的 store action，
// selector 每次取到同一个引用。若这里每次渲染现造一个 vi.fn()，handleOrder 的依赖
// 就被 mock 自己搅动了，测出来的"循环"与被测缺陷无关（会掩盖修复是否生效）。
// 在 factory 内部建一次即可 —— factory 只执行一次。
vi.mock('../../stores/login-dialog.store', () => {
  const open = vi.fn();
  return { useLoginDialogStore: (selector: (s: any) => any) => selector({ open }) };
});

// overleap 构建走 StripePurchasePanel；预览 effect 在分支 return 之前，照样会跑。
vi.mock('../../hooks/useStripeCheckout', () => ({
  useStripeCheckout: () => ({
    checkout: vi.fn(),
    openPortal: vi.fn(),
    loading: false,
    error: null,
    clearError: vi.fn(),
  }),
}));

import Purchase from '../Purchase';
import { cloudApi } from '../../services/cloud-api';
import { cacheStore } from '../../services/cache-store';
import { brandConfig } from '../../brands';
import { previewOrderEnabled } from '../../utils/purchase-preview';

// jsdom 下 window._platform 不存在 → 非 iOS 轨。品牌能力由构建时烘焙。
const PREVIEW_ENABLED = previewOrderEnabled({
  iap: false,
  wordgatePurchase: brandConfig.features.wordgatePurchase === true,
  stripeCheckout: brandConfig.features.stripeCheckout === true,
});

const PLAN_1M: Plan = {
  pid: 'p-1m', tier: 'basic', label: '1 个月', price: 1900, originPrice: 1900, month: 1,
  highlight: true, maxDevice: 5, maxRouterDevice: 0, maxLanClient: 0, product: 'app',
};
// originPrice 必须 > price：年付卡片把「总价」与「划线原价」都渲染成 $xx.xx，
// 两者相等时 $120.00 在同一张卡里出现两次，findByText 会因多命中而失败。
const PLAN_12M: Plan = {
  pid: 'p-12m', tier: 'basic', label: '12 个月', price: 12000, originPrice: 22800, month: 12,
  highlight: false, maxDevice: 5, maxRouterDevice: 0, maxLanClient: 0, product: 'app',
};

// 修复前的循环在「请求立即返回」下是指数级自激；超过上限后让请求永不返回，
// 循环自然停下 —— 修复前的运行快速失败而不是把测试进程拖死。
const RUNAWAY_CAP = 50;

const previewCalls = () =>
  (cloudApi.post as any).mock.calls.filter(
    ([path, body]: [string, any]) => path === '/api/user/orders' && body?.preview === true,
  );

const settle = (ms = 300) => new Promise((r) => setTimeout(r, ms));

function renderPurchase() {
  return render(
    <MemoryRouter>
      <I18nextProvider i18n={i18n}>
        <Purchase />
      </I18nextProvider>
    </MemoryRouter>,
  );
}

describe('Purchase 预览订单不自激循环（真实 useUser）', () => {
  beforeEach(() => {
    vi.clearAllMocks();
    cacheStore.clear();
    localStorage.clear();
    (cloudApi.get as any).mockImplementation((path: string) => {
      if (path === '/api/plans') return Promise.resolve({ code: 0, data: { items: [PLAN_1M, PLAN_12M] } });
      if (path === '/api/user/info') {
        return Promise.resolve({
          code: 0,
          data: { uuid: 'u-1', expiredAt: 1, isFirstOrderDone: false, loginIdentifies: [], deviceCount: 0, hasPassword: false },
        });
      }
      return Promise.resolve({ code: 0, data: {} });
    });
    let n = 0;
    (cloudApi.post as any).mockImplementation((_path: string, body: any) => {
      if (++n > RUNAWAY_CAP) return new Promise(() => {});
      const price = body?.plan === PLAN_12M.pid ? PLAN_12M.price : PLAN_1M.price;
      return Promise.resolve({
        code: 0,
        data: { order: { uuid: `ord-${n}`, payAmount: price, originAmount: price }, payUrl: '' },
      });
    });
  });

  it.runIf(PREVIEW_ENABLED)('打开购买页后预览请求数收敛，不随渲染持续增长', async () => {
    renderPurchase();

    // 修复不能把预览本身弄没。
    await waitFor(() => expect(previewCalls().length).toBeGreaterThan(0));

    await settle();
    const settled = previewCalls().length;
    await settle();

    expect(previewCalls().length).toBe(settled);
    expect(settled).toBeLessThan(10);
  });

  // 预览结果（orderData）只有 WordGate 多套餐流会消费；Stripe 订阅品牌整页替换掉它，
  // 一个请求都不该发。事故期间 iOS 同样在白发请求 —— 组合判据见
  // utils/__tests__/purchase-preview.test.ts。
  it.runIf(!PREVIEW_ENABLED)('本品牌无人消费预览结果时，一个预览请求都不发', async () => {
    renderPurchase();
    // 必须先等到"套餐列表已到、默认套餐已选中"——预览 effect 的前置条件就是这个。
    // 直接 settle(300) 就断言 0 会在预览有机会发出之前通过：把 Purchase.tsx 的
    // previewEnabled 闸门摘掉，测试依旧全绿（已实测），等于没测。
    await waitFor(() => expect(cloudApi.get).toHaveBeenCalledWith('/api/plans'));
    await settle(1500);
    expect(previewCalls()).toHaveLength(0);
  });

  // 只有 WordGate 品牌渲染套餐列表可点；预览 effect 本身两个品牌都跑（上一个用例覆盖）。
  it.runIf(brandConfig.features.wordgatePurchase)('切换套餐仍会重新预览，且切换后同样收敛', async () => {
    renderPurchase();
    await waitFor(() => expect(previewCalls().length).toBeGreaterThan(0));
    await settle();

    fireEvent.click(await screen.findByText('$120.00'));

    await waitFor(() =>
      expect(previewCalls().some(([, body]: [string, any]) => body.plan === PLAN_12M.pid)).toBe(true),
    );
    await settle();
    const settled = previewCalls().length;
    await settle();

    expect(previewCalls().length).toBe(settled);
    expect(settled).toBeLessThan(10);
  });
});
