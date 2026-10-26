/**
 * 退款同意弹层（设计稿 ④）：首次成功授予后询问；允许 / 不允许写服务端并同步用户缓存。
 * 用真实 i18n（zh-CN）断言文案。
 */
import { describe, it, expect, vi, beforeAll, beforeEach, afterEach } from 'vitest';
import { render, screen, fireEvent, waitFor } from '@testing-library/react';
import { I18nextProvider } from 'react-i18next';
import i18n, { i18nPromise, changeLanguage } from '../../../i18n/i18n';

const m = vi.hoisted(() => ({ request: vi.fn(), cacheSet: vi.fn() }));
vi.mock('../../../services/cloud-api', () => ({ cloudApi: { request: m.request, post: vi.fn(), get: vi.fn() } }));
vi.mock('../../../services/cache-store', () => ({
  cacheStore: { get: vi.fn(), set: m.cacheSet, subscribe: vi.fn(() => () => {}) },
}));
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
let mockUser: any;
vi.mock('../../../hooks/useUser', () => ({
  useUser: () => ({ user: mockUser, fetchUser: vi.fn() }),
}));
vi.mock('../../../hooks/useStripeCheckout', () => ({ useStripeCheckout: () => ({ openPortal: vi.fn() }) }));
vi.mock('../../../hooks/useAppConfig', () => ({ useAppConfig: () => ({ appConfig: null, loading: false, error: null }) }));
vi.mock('react-router-dom', () => ({ useNavigate: () => vi.fn() }));
let hookState: any;
vi.mock('../../../hooks/useIapPurchase', () => ({
  IAP_PRODUCT_IDS: ['io.kaitu.sub.basic.1y'],
  useIapPurchase: () => hookState,
}));

import IosMembershipPanel from '../IosMembershipPanel';
import ConsumptionConsentSheet from '../ConsumptionConsentSheet';

const API = '/api/user/apple-consumption-consent';

const APPLE_SUB = {
  provider: 'apple', tier: 'basic', currentPeriodEnd: 4102444800, autoRenew: true,
  manage: { kind: 'apple_settings' }, isTrial: false, trialEndsAt: 0,
};
function setUser(extra: Record<string, unknown>, subs: any[] = [APPLE_SUB]) {
  mockUser = { expiredAt: 4102444800, maxDevice: 5, subscriptions: subs, ...extra };
}
function panelState() {
  hookState = {
    products: [], loadProducts: vi.fn(), productsLoading: false, purchase: vi.fn(), restore: vi.fn(),
    restoring: false, purchaseError: null, lastGrantedUser: null, clearError: vi.fn(),
  };
}
function panel() {
  return (
    <I18nextProvider i18n={i18n}>
      <IosMembershipPanel mode="manage" activeSub={APPLE_SUB as any} />
    </I18nextProvider>
  );
}
function renderPanel() {
  return render(panel());
}
const TITLE = '退款时，是否允许提供使用情况？';

describe('ConsumptionConsentSheet', () => {
  let originalPlatform: any;
  beforeAll(async () => {
    await i18nPromise;
    await changeLanguage('zh-CN');
  });
  beforeEach(() => {
    vi.clearAllMocks();
    // clearAllMocks 清掉了 setup.ts 的 getComputedStyle 实现；MUI Drawer(Modal) 要读 paddingRight。
    const styles: Record<string, string> = { visibility: 'visible', display: 'block', paddingRight: '0px', overflow: 'visible' };
    (window.getComputedStyle as any).mockImplementation(() =>
      new Proxy(styles, {
        get(target, prop) {
          if (prop === 'getPropertyValue') return (name: string) => target[name] || '';
          return typeof prop === 'string' ? target[prop] || '' : undefined;
        },
      }),
    );
    originalPlatform = window._platform;
    (window as any)._platform = { iap: {}, openExternal: vi.fn() };
    m.request.mockResolvedValue({ code: 0, data: { appleConsumptionConsent: true } });
  });
  afterEach(() => { (window as any)._platform = originalPlatform; });

  it('apple subscription + consent null opens the sheet with the approved copy', () => {
    setUser({ appleConsumptionConsent: null }); panelState();
    renderPanel();
    expect(screen.getByText(TITLE)).toBeDefined();
    expect(screen.getByText('服务已正常提供')).toBeDefined();
    expect(screen.getByText('这笔订阅是否含免费试用')).toBeDefined();
    expect(screen.getByText('我们建议按剩余时长比例退款')).toBeDefined();
    expect(screen.queryByText('大致使用的流量')).toBeNull();
    expect(screen.getByRole('button', { name: '允许' })).toBeDefined();
    expect(screen.getByRole('button', { name: '不允许' })).toBeDefined();
  });

  it('missing field (older server) also counts as never asked', () => {
    setUser({}); panelState();
    renderPanel();
    expect(screen.getByText(TITLE)).toBeDefined();
  });

  it('consent already true or false does not open the sheet', () => {
    setUser({ appleConsumptionConsent: false }); panelState();
    const { unmount } = renderPanel();
    expect(screen.queryByText(TITLE)).toBeNull();
    unmount();
    setUser({ appleConsumptionConsent: true });
    renderPanel();
    expect(screen.queryByText(TITLE)).toBeNull();
  });

  it('no apple subscription does not open the sheet', () => {
    setUser({ appleConsumptionConsent: null }, [{ ...APPLE_SUB, provider: 'stripe' }]); panelState();
    renderPanel();
    expect(screen.queryByText(TITLE)).toBeNull();
  });

  it('Allow PUTs consented:true, syncs the cache, and a re-render with the updated user does not reopen', async () => {
    setUser({ appleConsumptionConsent: null }); panelState();
    const { rerender } = renderPanel();
    fireEvent.click(screen.getByRole('button', { name: '允许' }));
    await waitFor(() => expect(m.request).toHaveBeenCalledWith('PUT', API, { consented: true }));
    await waitFor(() =>
      expect(m.cacheSet).toHaveBeenCalledWith('api:user_info', { appleConsumptionConsent: true }, { ttl: 3600 }),
    );
    await waitFor(() => expect(screen.queryByText(TITLE)).toBeNull());
    setUser({ appleConsumptionConsent: true });
    rerender(panel());
    expect(screen.queryByText(TITLE)).toBeNull();
  });

  it('Deny PUTs consented:false and closes', async () => {
    m.request.mockResolvedValue({ code: 0, data: { appleConsumptionConsent: false } });
    setUser({ appleConsumptionConsent: null }); panelState();
    renderPanel();
    fireEvent.click(screen.getByRole('button', { name: '不允许' }));
    await waitFor(() => expect(m.request).toHaveBeenCalledWith('PUT', API, { consented: false }));
    await waitFor(() => expect(screen.queryByText(TITLE)).toBeNull());
  });

  it('request failure closes without writing the cache, and does not reopen on re-render', async () => {
    m.request.mockResolvedValue({ code: 500, message: 'x' });
    setUser({ appleConsumptionConsent: null }); panelState();
    const { rerender } = renderPanel();
    fireEvent.click(screen.getByRole('button', { name: '允许' }));
    await waitFor(() => expect(screen.queryByText(TITLE)).toBeNull());
    expect(m.cacheSet).not.toHaveBeenCalled();
    rerender(panel());
    expect(screen.queryByText(TITLE)).toBeNull();
  });

  it('the privacy link opens the brand privacy page via openExternal', () => {
    const onClose = vi.fn();
    render(
      <I18nextProvider i18n={i18n}>
        <ConsumptionConsentSheet open onClose={onClose} />
      </I18nextProvider>,
    );
    fireEvent.click(screen.getByText('隐私政策'));
    expect((window as any)._platform.openExternal).toHaveBeenCalledWith('https://example.test/privacy');
  });
});
