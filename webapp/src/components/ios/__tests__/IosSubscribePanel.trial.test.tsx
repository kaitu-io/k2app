/**
 * 付费页（设计稿 ①）：试用资格驱动 CTA / 时间线 / 披露文案。
 * 用真实 i18n（zh-CN）断言文案，hero 走 t()，品牌自适应（kaitu overlay / 基础 locale）。
 */
import { describe, it, expect, vi, beforeAll, beforeEach, afterEach } from 'vitest';
import { render, screen, fireEvent } from '@testing-library/react';
import { I18nextProvider } from 'react-i18next';
import i18n, { i18nPromise, changeLanguage } from '../../../i18n/i18n';

vi.mock('../../../services/stats', () => ({
  statsService: {
    trackFunnel: vi.fn(), trackFunnelOnce: vi.fn(), trackFunnelDaily: vi.fn(),
    trackAppOpen: vi.fn(), trackConnect: vi.fn(), trackDisconnect: vi.fn(),
  },
}));
vi.mock('../../../hooks/useAppLinks', () => ({
  useAppLinks: () => ({ links: { termsOfServiceUrl: 'https://example.test/terms', privacyPolicyUrl: 'https://example.test/privacy' }, loading: false, error: null, currentLang: 'zh-CN' }),
}));
vi.mock('../../../stores/alert.store', () => ({ useAlert: () => ({ showAlert: vi.fn() }) }));
let mockUser: any = null;
vi.mock('../../../hooks/useUser', () => ({ useUser: () => ({ user: mockUser, fetchUser: vi.fn() }) }));
vi.mock('../../EmailLoginForm', () => ({ default: () => <div data-testid="email-login-form" /> }));
const mockOpenLogin = vi.fn();
vi.mock('../../../stores/login-dialog.store', () => ({
  useLoginDialogStore: (selector: (s: any) => any) => selector({ open: mockOpenLogin }),
}));

const mockPurchase = vi.fn();
let hookState: any;
vi.mock('../../../hooks/useIapPurchase', () => ({
  IAP_PRODUCT_IDS: ['io.kaitu.sub.basic.1y'],
  useIapPurchase: () => hookState,
}));

import IosSubscribePanel from '../IosSubscribePanel';
import { statsService } from '../../../services/stats';

const ID = 'io.kaitu.sub.basic.1y';

function product(extra: Record<string, unknown> = {}) {
  return {
    id: ID, displayName: 'Kaitu Annual', description: 'x', displayPrice: '$12.99',
    price: 88, periodUnit: 'year', periodValue: 1, ...extra,
  };
}
const ELIGIBLE = {
  isEligibleForIntroOffer: true,
  introOffer: { paymentMode: 'freeTrial', periodUnit: 'week', periodValue: 1, displayPrice: '$0.00' },
};

function setProducts(products: any[]) {
  hookState = {
    products, loadProducts: vi.fn(), productsLoading: false, purchase: mockPurchase, restore: vi.fn(),
    purchasing: false, restoring: false, purchaseError: null, lastGrantedUser: null, clearError: vi.fn(),
  };
}

function renderPanel(props: Record<string, unknown> = {}) {
  return render(
    <I18nextProvider i18n={i18n}>
      <IosSubscribePanel isAuthenticated accountToken="tok" isMembership={false} isExpired={false} {...props} />
    </I18nextProvider>,
  );
}

describe('IosSubscribePanel trial paywall', () => {
  let originalPlatform: any;
  beforeAll(async () => {
    await i18nPromise;
    await changeLanguage('zh-CN');
  });
  beforeEach(() => {
    vi.clearAllMocks();
    mockUser = { loginIdentifies: [{ type: 'email', value: 'a@b.com' }] };
    originalPlatform = window._platform;
    (window as any)._platform = { iap: {}, openExternal: vi.fn() };
  });
  afterEach(() => { (window as any)._platform = originalPlatform; });

  it('eligible renders trial CTA, timeline and trial disclosure', () => {
    setProducts([product(ELIGIBLE)]);
    renderPanel();
    expect(screen.getByTestId('iap-subscribe-btn').textContent).toBe('免费试用 7 天');
    expect(screen.getByText('新用户免费体验 7 天')).toBeDefined();
    expect(screen.getByText('第 6 天前')).toBeDefined();
    expect(screen.getByText('第 7 天')).toBeDefined();
    expect(screen.getByText('开始按 $12.99/年 扣费，成为正式会员')).toBeDefined();
    expect(screen.getByText('前 7 天免费')).toBeDefined();
    expect(screen.getByTestId('iap-auto-renewal-disclosure').textContent).toBe(
      '免费试用 7 天，之后按 $12.99/年 自动续订，通过 Apple ID 扣费。试用结束前至少 24 小时在 App Store 取消，就不会扣费。',
    );
    expect(screen.getByText(i18n.t('purchase:purchase.iap.heroTitle'))).toBeDefined();
    // 旧的「试用中 / 首次充值」横幅已删除
    expect(screen.queryByText(i18n.t('purchase:purchase.currentlyInTrial'))).toBeNull();
    expect(screen.queryByText(i18n.t('purchase:purchase.completeFirstPurchase'))).toBeNull();
  });

  it('ineligible renders no free wording', () => {
    setProducts([product({ ...ELIGIBLE, isEligibleForIntroOffer: false })]);
    const { container } = renderPanel();
    expect(container.textContent).not.toContain('免费');
    expect(screen.getByTestId('iap-subscribe-btn').textContent).toBe('立即订阅');
    expect(screen.getByTestId('iap-auto-renewal-disclosure').textContent).toBe(
      '订阅按 $12.99/年 自动续订，通过 Apple ID 扣费。每期结束前至少 24 小时在 App Store 取消，下期就不再扣费。',
    );
  });

  it('old shell renders subscribe-now branch', () => {
    setProducts([product()]);
    const { container } = renderPanel();
    expect(container.textContent).not.toContain('免费');
    expect(screen.getByTestId('iap-subscribe-btn').textContent).toBe('立即订阅');
    expect(screen.getByText(i18n.t('purchase:purchase.iap.heroTitle'))).toBeDefined();
  });

  it('trial checkout funnel carries #trial plan', () => {
    setProducts([product(ELIGIBLE)]);
    renderPanel();
    fireEvent.click(screen.getByTestId('iap-subscribe-btn'));
    const tracked = (statsService.trackFunnel as any).mock.calls.filter((c: any[]) => c[0] === 'checkout_start');
    expect(tracked).toEqual([['checkout_start', { plan: `${ID}#trial`, channel: 'apple' }]]);
    expect(mockPurchase).toHaveBeenCalledWith(ID, 'tok');
  });

  it('non-trial checkout funnel keeps the plain row id', () => {
    setProducts([product()]);
    renderPanel();
    fireEvent.click(screen.getByTestId('iap-subscribe-btn'));
    const tracked = (statsService.trackFunnel as any).mock.calls.filter((c: any[]) => c[0] === 'checkout_start');
    expect(tracked).toEqual([['checkout_start', { plan: ID, channel: 'apple' }]]);
  });

  it('zero-identity device account sees "have account? log in" which opens LoginDialog', () => {
    mockUser = { loginIdentifies: [] };
    setProducts([product(ELIGIBLE)]);
    renderPanel();
    expect(screen.queryByTestId('email-login-form')).toBeNull();
    fireEvent.click(screen.getByText('已有账号？登录'));
    expect(mockOpenLogin).toHaveBeenCalledTimes(1);
    // 设备账号可直接试用
    const btn = screen.getByTestId('iap-subscribe-btn') as HTMLButtonElement;
    expect(btn.disabled).toBe(false);
  });

  it('account with a login identity sees no login link', () => {
    setProducts([product(ELIGIBLE)]);
    renderPanel();
    expect(screen.queryByText('已有账号？登录')).toBeNull();
  });

  it('unauthenticated old shell keeps the inline EmailLoginForm and gated button', () => {
    mockUser = null;
    setProducts([product()]);
    renderPanel({ isAuthenticated: false, accountToken: '' });
    expect(screen.getByTestId('email-login-form')).toBeDefined();
    expect(screen.queryByText('已有账号？登录')).toBeNull();
    expect((screen.getByTestId('iap-subscribe-btn') as HTMLButtonElement).disabled).toBe(true);
  });

  it('benefit devices count follows maxDevice (default 5)', () => {
    setProducts([product(ELIGIBLE)]);
    const { unmount } = renderPanel();
    expect(screen.getByText('5 台设备同时用')).toBeDefined();
    unmount();
    renderPanel({ maxDevice: 3 });
    expect(screen.getByText('3 台设备同时用')).toBeDefined();
  });

  it('zero-identity device account on a Keychain shell sees the bind clause on devices', () => {
    mockUser = { loginIdentifies: [] };
    (window as any)._platform = { iap: {}, openExternal: vi.fn(), keychainIdentity: true };
    setProducts([product(ELIGIBLE)]);
    renderPanel();
    expect(screen.getByText('绑定邮箱或 Apple 登录后，5 台设备同时用')).toBeDefined();
  });

  it('identity user on a Keychain shell gets the plain devices line', () => {
    (window as any)._platform = { iap: {}, openExternal: vi.fn(), keychainIdentity: true };
    setProducts([product(ELIGIBLE)]);
    const { container } = renderPanel();
    expect(screen.getByText('5 台设备同时用')).toBeDefined();
    expect(container.textContent).not.toContain('绑定邮箱或 Apple 登录后');
  });

  it('zero-identity user without Keychain shell gets the plain devices line', () => {
    mockUser = { loginIdentifies: [] };
    setProducts([product(ELIGIBLE)]);
    const { container } = renderPanel();
    expect(screen.getByText('5 台设备同时用')).toBeDefined();
    expect(container.textContent).not.toContain('绑定邮箱或 Apple 登录后');
  });

  it('all-nodes benefit carries the trial clause only when a trial is offered', () => {
    setProducts([product(ELIGIBLE)]);
    const { unmount } = renderPanel();
    expect(screen.getByText('全部会员节点，试用期间同样可用')).toBeDefined();
    unmount();
    setProducts([product({ ...ELIGIBLE, isEligibleForIntroOffer: false })]);
    const { container } = renderPanel();
    expect(screen.getByText('全部会员节点')).toBeDefined();
    expect(container.textContent).not.toContain('试用期间');
  });

  it('no products yet: disclosure has no price phrase and no placeholder price', () => {
    setProducts([]);
    const { container } = renderPanel();
    expect(screen.getByTestId('iap-auto-renewal-disclosure').textContent).toBe(
      '订阅按年自动续订，通过 Apple ID 扣费。每期结束前至少 24 小时在 App Store 取消，下期就不再扣费。',
    );
    expect(container.textContent).not.toContain('—');
    expect(container.textContent).not.toContain('/年');
  });

  it('eligible trial without a displayPrice: trial disclosure and timeline drop the price', () => {
    setProducts([product({ ...ELIGIBLE, displayPrice: '' })]);
    const { container } = renderPanel();
    expect(screen.getByTestId('iap-auto-renewal-disclosure').textContent).toBe(
      '免费试用 7 天，之后按年自动续订，通过 Apple ID 扣费。试用结束前至少 24 小时在 App Store 取消，就不会扣费。',
    );
    expect(screen.getByText('开始扣费，成为正式会员')).toBeDefined();
    expect(container.textContent).not.toContain('—');
    expect(container.textContent).not.toContain('/年');
  });
});
