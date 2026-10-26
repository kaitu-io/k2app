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
vi.mock('../../../hooks/useUser', () => ({
  useUser: () => ({ user: { loginIdentifies: [{ type: 'email', value: 'a@b.com' }] }, fetchUser: vi.fn() }),
}));
vi.mock('../../EmailLoginForm', () => ({ default: () => <div /> }));
vi.mock('../../../stores/login-dialog.store', () => ({
  useLoginDialogStore: (selector: (s: any) => any) => selector({ open: vi.fn() }),
}));
let hookState: any;
vi.mock('../../../hooks/useIapPurchase', () => ({
  IAP_PRODUCT_IDS: ['io.kaitu.sub.basic.1y'],
  useIapPurchase: () => hookState,
}));

import IosSubscribePanel from '../IosSubscribePanel';
import ConsumptionConsentSheet from '../ConsumptionConsentSheet';

const API = '/api/user/apple-consumption-consent';

function panelState(lastGrantedUser: any) {
  hookState = {
    products: [], loadProducts: vi.fn(), productsLoading: false, purchase: vi.fn(), restore: vi.fn(),
    purchasing: false, restoring: false, purchaseError: null, lastGrantedUser, clearError: vi.fn(),
  };
}
function renderPanel() {
  return render(
    <I18nextProvider i18n={i18n}>
      <IosSubscribePanel isAuthenticated accountToken="tok" isMembership={false} isExpired={false} />
    </I18nextProvider>,
  );
}

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

  it('consent null after a successful grant opens the sheet with the approved copy', () => {
    panelState({ appleConsumptionConsent: null });
    renderPanel();
    expect(screen.getByText('退款时，是否允许提供使用情况？')).toBeDefined();
    expect(screen.getByText('订阅已使用的天数')).toBeDefined();
    expect(screen.getByText('大致使用的流量')).toBeDefined();
    expect(screen.getByRole('button', { name: '允许' })).toBeDefined();
    expect(screen.getByRole('button', { name: '不允许' })).toBeDefined();
  });

  it('missing field (older server) also counts as never asked', () => {
    panelState({});
    renderPanel();
    expect(screen.getByText('退款时，是否允许提供使用情况？')).toBeDefined();
  });

  it('consent already false or true does not open the sheet', () => {
    panelState({ appleConsumptionConsent: false });
    const { unmount } = renderPanel();
    expect(screen.queryByText('退款时，是否允许提供使用情况？')).toBeNull();
    unmount();
    panelState({ appleConsumptionConsent: true });
    renderPanel();
    expect(screen.queryByText('退款时，是否允许提供使用情况？')).toBeNull();
  });

  it('Allow PUTs consented:true, syncs the user cache and closes', async () => {
    panelState({ appleConsumptionConsent: null });
    renderPanel();
    fireEvent.click(screen.getByRole('button', { name: '允许' }));
    await waitFor(() => expect(m.request).toHaveBeenCalledWith('PUT', API, { consented: true }));
    await waitFor(() =>
      expect(m.cacheSet).toHaveBeenCalledWith('api:user_info', { appleConsumptionConsent: true }, { ttl: 3600 }),
    );
    await waitFor(() => expect(screen.queryByText('退款时，是否允许提供使用情况？')).toBeNull());
  });

  it('Deny PUTs consented:false and closes', async () => {
    m.request.mockResolvedValue({ code: 0, data: { appleConsumptionConsent: false } });
    panelState({ appleConsumptionConsent: null });
    renderPanel();
    fireEvent.click(screen.getByRole('button', { name: '不允许' }));
    await waitFor(() => expect(m.request).toHaveBeenCalledWith('PUT', API, { consented: false }));
    await waitFor(() => expect(screen.queryByText('退款时，是否允许提供使用情况？')).toBeNull());
  });

  it('request failure closes the sheet without writing the cache', async () => {
    m.request.mockResolvedValue({ code: 500, message: 'x' });
    panelState({ appleConsumptionConsent: null });
    renderPanel();
    fireEvent.click(screen.getByRole('button', { name: '允许' }));
    await waitFor(() => expect(screen.queryByText('退款时，是否允许提供使用情况？')).toBeNull());
    expect(m.cacheSet).not.toHaveBeenCalled();
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
