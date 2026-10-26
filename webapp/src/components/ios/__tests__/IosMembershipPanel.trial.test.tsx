/**
 * 会员页（设计稿 ②）：isTrial 订阅渲染试用卡，文案走真实 zh-CN i18n 逐字断言。
 */
import { describe, it, expect, vi, beforeAll, beforeEach, afterEach } from 'vitest';
import { render, screen, fireEvent } from '@testing-library/react';
import { I18nextProvider } from 'react-i18next';
import i18n, { i18nPromise, changeLanguage } from '../../../i18n/i18n';
import type { DataSubscription } from '../../../services/api-types';
import { formatExpiryDate } from '../membership-format';

let hookState: any;
vi.mock('../../../hooks/useIapPurchase', () => ({
  IAP_PRODUCT_IDS: ['io.kaitu.sub.basic.1y'],
  useIapPurchase: () => hookState,
}));
vi.mock('../../../hooks/useStripeCheckout', () => ({
  useStripeCheckout: () => ({ openPortal: vi.fn() }),
}));
vi.mock('../../../hooks/useUser', () => ({
  useUser: () => ({ user: { expiredAt: 0, maxDevice: 5 }, fetchUser: vi.fn() }),
}));
vi.mock('../../../hooks/useAppConfig', () => ({
  useAppConfig: () => ({ appConfig: null, loading: false, error: null }),
}));
vi.mock('../../../stores/alert.store', () => ({ useAlert: () => ({ showAlert: vi.fn() }) }));
vi.mock('../../../services/stats', () => ({ statsService: { trackFunnel: vi.fn() } }));
vi.mock('react-router-dom', () => ({ useNavigate: () => vi.fn() }));

import IosMembershipPanel from '../IosMembershipPanel';

const nowSec = () => Math.floor(Date.now() / 1000);
// 4 天 23 小时后结束 → ceil → 剩 5 天
const endsIn5Days = () => nowSec() + 5 * 86400 - 3600;

const sub = (over: Partial<DataSubscription> = {}): DataSubscription => ({
  provider: 'apple',
  tier: 'basic',
  currentPeriodEnd: nowSec() + 365 * 86400,
  autoRenew: true,
  manage: { kind: 'apple_settings' },
  isTrial: false,
  trialEndsAt: 0,
  ...over,
});

const PRODUCT = {
  id: 'io.kaitu.sub.basic.1y', displayName: 'x', description: 'x', displayPrice: '$12.99',
  price: 88, periodUnit: 'year', periodValue: 1,
  // 试用进行中 StoreKit 报告的就是 eligible=false（已用过试用资格）。
  isEligibleForIntroOffer: false,
  introOffer: { paymentMode: 'freeTrial', periodUnit: 'week', periodValue: 1, displayPrice: '$0.00' },
};

function setHook(products: any[]) {
  hookState = {
    products, loadProducts: vi.fn(), restore: vi.fn(), restoring: false,
    purchaseError: null, lastGrantedUser: null,
  };
}

function renderPanel(activeSub: DataSubscription) {
  return render(
    <I18nextProvider i18n={i18n}>
      <IosMembershipPanel mode="manage" activeSub={activeSub} />
    </I18nextProvider>,
  );
}

describe('IosMembershipPanel trial card', () => {
  let originalPlatform: any;
  beforeAll(async () => {
    await i18nPromise;
    await changeLanguage('zh-CN');
  });
  beforeEach(() => {
    originalPlatform = window._platform;
    (window as any)._platform = { openExternal: vi.fn() };
    setHook([PRODUCT]);
  });
  afterEach(() => { (window as any)._platform = originalPlatform; });

  it('isTrial with 5 days left shows tag, days left, total, renew note and loads products', () => {
    const ends = endsIn5Days();
    renderPanel(sub({ isTrial: true, trialEndsAt: ends }));
    const card = screen.getByTestId('trial-status-card');
    expect(card.textContent).toContain('免费试用中');
    expect(card.textContent).toContain('试用还剩 5 天');
    expect(card.textContent).toContain('共 7 天');
    expect(card.textContent).toContain(
      `${formatExpiryDate(ends)} 起按 $12.99/年 自动续订。不想继续，在到期前至少 24 小时取消即可。`,
    );
    expect(screen.getByTestId('trial-progress')).toBeTruthy();
    expect(screen.queryByTestId('renewal-status-card')).toBeNull();
    expect(hookState.loadProducts).toHaveBeenCalled();
  });

  it('manage button opens the manage surface', () => {
    const openExternal = vi.fn();
    (window as any)._platform = { openExternal };
    renderPanel(sub({ isTrial: true, trialEndsAt: endsIn5Days() }));
    fireEvent.click(screen.getByTestId('trial-manage-btn'));
    expect(screen.getByTestId('trial-manage-btn').textContent).toBe('管理订阅');
    expect(openExternal).toHaveBeenCalledWith('itms-apps://apps.apple.com/account/subscriptions');
  });

  it('without a StoreKit trial offer: no total / progress, renew note without price', () => {
    setHook([]);
    const ends = endsIn5Days();
    renderPanel(sub({ isTrial: true, trialEndsAt: ends }));
    const card = screen.getByTestId('trial-status-card');
    expect(card.textContent).toContain('试用还剩 5 天');
    expect(card.textContent).not.toContain('共 ');
    expect(screen.queryByTestId('trial-progress')).toBeNull();
    expect(card.textContent).toContain(
      `${formatExpiryDate(ends)} 起自动续订。不想继续，在到期前至少 24 小时取消即可。`,
    );
  });

  it('days left floors at 0 once the trial end has passed', () => {
    renderPanel(sub({ isTrial: true, trialEndsAt: nowSec() - 100 }));
    expect(screen.getByTestId('trial-status-card').textContent).toContain('试用还剩 0 天');
  });

  it('isTrial=false keeps RenewalStatusCard and renders no trial card', () => {
    renderPanel(sub());
    expect(screen.queryByTestId('trial-status-card')).toBeNull();
    expect(screen.getByTestId('renewal-status-card')).toBeTruthy();
  });
});
