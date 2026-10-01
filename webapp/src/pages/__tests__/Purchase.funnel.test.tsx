import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { render, screen, fireEvent, waitFor } from '@testing-library/react';
import { I18nextProvider } from 'react-i18next';
import { MemoryRouter } from 'react-router-dom';
import { act } from '@testing-library/react';
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

vi.mock('../../services/stats', () => ({
  statsService: {
    trackFunnel: vi.fn(), trackFunnelOnce: vi.fn(), trackAppOpen: vi.fn(),
    trackConnect: vi.fn(), trackDisconnect: vi.fn(),
  },
}));

vi.mock('../../services/cloud-api', () => ({
  cloudApi: { get: vi.fn(), post: vi.fn() },
}));

// Per-test login state (reset to signed-in in beforeEach).
const auth = vi.hoisted(() => ({ isAuthenticated: true }));

// useUser 直接 import 这两个 store（不经 stores/index）。
vi.mock('../../stores/auth.store', () => ({
  useAuthStore: (selector: (s: { isAuthenticated: boolean }) => unknown) =>
    selector({ isAuthenticated: auth.isAuthenticated }),
}));
vi.mock('../../stores/config.store', () => ({
  useConfigStore: { getState: () => ({ setDetectedProfile: vi.fn() }) },
}));

const showAlert = vi.fn();
vi.mock('../../stores', () => ({
  useAlert: () => ({ showAlert }),
  useAuthStore: (selector: (s: any) => any) => selector({ isAuthenticated: auth.isAuthenticated }),
}));

// iOS panels pull in StoreKit hooks; here only "which panel rendered" matters.
vi.mock('../../components/ios', () => ({
  IosSubscribePanel: () => <div data-testid="ios-subscribe-panel" />,
  IosMembershipPanel: () => <div data-testid="ios-membership-panel" />,
}));
vi.mock('../../components/EmailLoginForm', () => ({ default: () => <div data-testid="email-login-form" /> }));
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
import { statsService } from '../../services/stats';

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


const calls = (event: string) =>
  (statsService.trackFunnel as any).mock.calls.filter((c: any[]) => c[0] === event);

const settle = (ms = 300) => new Promise((r) => setTimeout(r, ms));

function renderPurchase(state?: unknown) {
  const entry = state === undefined ? { pathname: '/purchase' } : { pathname: '/purchase', state };
  const ui = (
    <MemoryRouter initialEntries={[entry]}>
      <I18nextProvider i18n={i18n}>
        <Purchase />
      </I18nextProvider>
    </MemoryRouter>
  );
  const r = render(ui);
  return { ...r, again: () => r.rerender(ui) };
}

const USER_PROSPECT = {
  uuid: 'u-1', expiredAt: 1, isFirstOrderDone: false, loginIdentifies: [], deviceCount: 0, hasPassword: false,
};
const FAR_FUTURE = Math.floor(Date.now() / 1000) + 300 * 86400;
/** Member with an active auto-renewing subscription → affordance 'manage'. */
const USER_SUBSCRIBER = (provider: string) => ({
  ...USER_PROSPECT,
  expiredAt: FAR_FUTURE,
  isFirstOrderDone: true,
  subscriptions: [{
    provider, tier: 'basic', currentPeriodEnd: FAR_FUTURE, autoRenew: true,
    manage: { kind: 'url', url: 'https://example.test/manage' },
  }],
});

function mockUser(user: unknown) {
  (cloudApi.get as any).mockImplementation((path: string) => {
    if (path === '/api/plans') return Promise.resolve({ code: 0, data: { items: [PLAN_1M, PLAN_12M] } });
    if (path === '/api/user/info') return user instanceof Promise ? user : Promise.resolve({ code: 0, data: user });
    return Promise.resolve({ code: 0, data: {} });
  });
}

const savedPlatform = (window as any)._platform;
afterEach(() => { (window as any)._platform = savedPlatform; });

beforeEach(() => {
  auth.isAuthenticated = true;
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
  (cloudApi.post as any).mockImplementation((_path: string, body: any) => {
    const price = body?.plan === PLAN_12M.pid ? PLAN_12M.price : PLAN_1M.price;
    return Promise.resolve({
      code: 0,
      data: { order: { uuid: 'ord-1', payAmount: price, originAmount: price }, payUrl: '' },
    });
  });
});

describe('Purchase funnel: paywall_view (both brands)', () => {
  it('reports the entry point passed through router state, exactly once', async () => {
    renderPurchase({ from: 'account_expired' });
    await settle();
    expect(calls('paywall_view')).toEqual([['paywall_view', { source: 'account_expired' }]]);
  });

  it('no router state -> direct', async () => {
    renderPurchase();
    await settle();
    expect(calls('paywall_view')).toEqual([['paywall_view', { source: 'direct' }]]);
  });

  it('a source outside PAYWALL_SOURCES -> direct', async () => {
    renderPurchase({ from: 'evil' });
    await settle();
    expect(calls('paywall_view')).toEqual([['paywall_view', { source: 'direct' }]]);
  });

  it('re-rendering does not report again', async () => {
    const { again } = renderPurchase({ from: 'tunnel_locked' });
    await settle();
    again(); again(); again();
    await settle();
    expect(calls('paywall_view')).toHaveLength(1);
  });
});

describe('Purchase funnel: paywall_view only on a purchase surface (both brands)', () => {
  it('entry from the navigation tab -> nav', async () => {
    renderPurchase({ from: 'nav' });
    await settle();
    expect(calls('paywall_view')).toEqual([['paywall_view', { source: 'nav' }]]);
  });

  it('a signed-out visitor is at the paywall: exactly one', async () => {
    auth.isAuthenticated = false;
    renderPurchase({ from: 'nav' });
    await settle();
    expect(calls('paywall_view')).toEqual([['paywall_view', { source: 'nav' }]]);
  });

  it('a member in manage mode is not at a paywall: no paywall_view', async () => {
    mockUser(USER_SUBSCRIBER('stripe'));
    const { again } = renderPurchase({ from: 'nav' });
    await settle();
    again();
    await settle();
    expect(cloudApi.get).toHaveBeenCalledWith('/api/user/info');
    expect(calls('paywall_view')).toHaveLength(0);
  });

  it('nothing is reported while the user record is still loading; a member then stays silent', async () => {
    let resolveUser!: (v: unknown) => void;
    mockUser(new Promise((res) => { resolveUser = res; }));
    renderPurchase({ from: 'account' });
    await settle();
    expect(calls('paywall_view')).toHaveLength(0);

    await act(async () => { resolveUser({ code: 0, data: USER_SUBSCRIBER('stripe') }); });
    await settle();
    expect(calls('paywall_view')).toHaveLength(0);
  });

  it('…and a non-member is reported once the user record resolves', async () => {
    let resolveUser!: (v: unknown) => void;
    mockUser(new Promise((res) => { resolveUser = res; }));
    renderPurchase({ from: 'account' });
    await settle();
    expect(calls('paywall_view')).toHaveLength(0);

    await act(async () => { resolveUser({ code: 0, data: USER_PROSPECT }); });
    await settle();
    expect(calls('paywall_view')).toEqual([['paywall_view', { source: 'account' }]]);
  });

  it('iOS: subscribe panel -> one paywall_view', async () => {
    (window as any)._platform = { os: 'ios', iap: {} };
    renderPurchase({ from: 'tunnel_locked' });
    expect(await screen.findByTestId('ios-subscribe-panel')).toBeTruthy();
    await settle();
    expect(calls('paywall_view')).toEqual([['paywall_view', { source: 'tunnel_locked' }]]);
  });

  it('iOS: membership panel (manage) -> no paywall_view', async () => {
    (window as any)._platform = { os: 'ios', iap: {} };
    mockUser(USER_SUBSCRIBER('apple'));
    renderPurchase({ from: 'nav' });
    expect(await screen.findByTestId('ios-membership-panel')).toBeTruthy();
    await settle();
    expect(calls('paywall_view')).toHaveLength(0);
  });

  it('iOS: membership panel (status, unexpired one-time member) -> no paywall_view', async () => {
    (window as any)._platform = { os: 'ios', iap: {} };
    mockUser({ ...USER_PROSPECT, expiredAt: FAR_FUTURE });
    renderPurchase({ from: 'nav' });
    expect(await screen.findByTestId('ios-membership-panel')).toBeTruthy();
    await settle();
    expect(calls('paywall_view')).toHaveLength(0);
  });
});

describe.runIf(brandConfig.features.wordgatePurchase)('Purchase funnel: WordGate flow', () => {
  it('clicking a plan reports plan_select with its pid', async () => {
    renderPurchase();
    fireEvent.click(await screen.findByText('$120.00'));
    expect(calls('plan_select')).toEqual([['plan_select', { plan: PLAN_12M.pid }]]);
  });

  it('the default selection and preview orders do not report plan_select / checkout_start', async () => {
    renderPurchase();
    await waitFor(() =>
      expect((cloudApi.post as any).mock.calls.some(([, b]: [string, any]) => b?.preview === true)).toBe(true),
    );
    await settle();
    expect(calls('plan_select')).toHaveLength(0);
    expect(calls('checkout_start')).toHaveLength(0);
  });

  it('paying (logged in) reports checkout_start exactly once, with no channel', async () => {
    renderPurchase();
    const pay = await screen.findByText(i18n.t('purchase:purchase.payNow'));
    await waitFor(() => expect(pay.closest('button')).not.toBeDisabled());
    await act(async () => { fireEvent.click(pay); });
    await waitFor(() =>
      expect((cloudApi.post as any).mock.calls.some(([, b]: [string, any]) => b?.preview === false)).toBe(true),
    );
    expect(calls('checkout_start')).toEqual([['checkout_start', { plan: PLAN_1M.pid }]]);
  });
});
