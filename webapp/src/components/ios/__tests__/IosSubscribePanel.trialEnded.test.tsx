/**
 * 试用结束用户的订阅面板文案：用真实 i18n（zh-CN）断言。
 */
import { describe, it, expect, vi, beforeAll, beforeEach, afterEach } from 'vitest';
import { readFileSync } from 'node:fs';
import { resolve } from 'node:path';
import { render, screen } from '@testing-library/react';
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
vi.mock('../../../stores/login-dialog.store', () => ({
  useLoginDialogStore: (selector: (s: any) => any) => selector({ open: vi.fn() }),
}));

let hookState: any;
vi.mock('../../../hooks/useIapPurchase', () => ({
  IAP_PRODUCT_IDS: ['io.kaitu.sub.basic.1y'],
  useIapPurchase: () => hookState,
}));

import IosSubscribePanel from '../IosSubscribePanel';

const ID = 'io.kaitu.sub.basic.1y';
const ELIGIBLE = {
  isEligibleForIntroOffer: true,
  introOffer: { paymentMode: 'freeTrial', periodUnit: 'week', periodValue: 1, displayPrice: '$0.00' },
};
const USED = { ...ELIGIBLE, isEligibleForIntroOffer: false };

function setProducts(extra: Record<string, unknown>) {
  hookState = {
    products: [{ id: ID, displayName: 'Kaitu Annual', description: 'x', displayPrice: '$12.99', price: 88, periodUnit: 'year', periodValue: 1, ...extra }],
    loadProducts: vi.fn(), productsLoading: false, purchase: vi.fn(), restore: vi.fn(),
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

describe('IosSubscribePanel trial ended', () => {
  let originalPlatform: any;
  beforeAll(async () => {
    await i18nPromise;
    await changeLanguage('zh-CN');
  });
  beforeEach(() => {
    vi.clearAllMocks();
    mockUser = { isFirstOrderDone: false, subscriptions: [], loginIdentifies: [{ type: 'email', value: 'a@b.com' }] };
    originalPlatform = window._platform;
    (window as any)._platform = { iap: {}, openExternal: vi.fn() };
  });
  afterEach(() => { (window as any)._platform = originalPlatform; });

  it('shows trial-ended headline and no free-trial wording', () => {
    setProducts(USED);
    const { container } = renderPanel();
    expect(screen.getByTestId('iap-trial-ended').textContent).toBe('你的 7 天试用已结束，订阅后立即恢复使用');
    expect(screen.getByTestId('iap-cancel-anytime').textContent).toContain('App Store 关闭自动续订');
    expect(container.textContent).not.toContain('免费');
    expect(screen.getByTestId('iap-subscribe-btn').textContent).toBe('立即订阅');
  });

  it('locale copy: zh-TW/zh-HK are Traditional (differ from zh-CN), ja has no free-trial wording', () => {
    // 非当前语言不一定已加载，直接读 locale 文件。
    const copy = (lng: string) =>
      (JSON.parse(readFileSync(resolve(__dirname, `../../../i18n/locales/${lng}/purchase.json`), 'utf8')) as any)
        .purchase.iap.trialEnded as string;
    for (const lng of ['zh-TW', 'zh-HK']) {
      expect(copy(lng)).toContain('試用已結束');
      expect(copy(lng)).not.toBe(copy('zh-CN'));
    }
    expect(copy('ja')).not.toContain('無料');
  });

  it('hidden when eligible', () => {
    setProducts(ELIGIBLE);
    renderPanel();
    expect(screen.queryByTestId('iap-trial-ended')).toBeNull();
    expect(screen.queryByTestId('iap-cancel-anytime')).toBeNull();
  });

  it('hidden when paid before', () => {
    setProducts(USED);
    mockUser = { ...mockUser, isFirstOrderDone: true };
    renderPanel();
    expect(screen.queryByTestId('iap-trial-ended')).toBeNull();
  });

  it('hidden with an active apple subscription', () => {
    setProducts(USED);
    mockUser = { ...mockUser, subscriptions: [{ provider: 'apple' }] };
    renderPanel();
    expect(screen.queryByTestId('iap-trial-ended')).toBeNull();
  });

  it('hidden when logged out', () => {
    setProducts(USED);
    mockUser = null;
    renderPanel({ isAuthenticated: false, accountToken: '' });
    expect(screen.queryByTestId('iap-trial-ended')).toBeNull();
  });
});
