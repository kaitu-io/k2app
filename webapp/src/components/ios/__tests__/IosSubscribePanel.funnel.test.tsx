import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { render, screen, fireEvent } from '@testing-library/react';

vi.mock('react-i18next', () => ({
  useTranslation: () => ({ t: (key: string, fallback?: string) => fallback ?? key }),
}));
vi.mock('../../../hooks/useAppLinks', () => ({
  useAppLinks: () => ({ links: { termsOfServiceUrl: 'https://example.test/terms', privacyPolicyUrl: 'https://example.test/privacy' }, loading: false, error: null, currentLang: 'en-US' }),
}));
vi.mock('../../../stores/alert.store', () => ({ useAlert: () => ({ showAlert: vi.fn() }) }));
vi.mock('../../../hooks/useUser', () => ({ useUser: () => ({ fetchUser: vi.fn() }) }));
vi.mock('../../MembershipBenefits', () => ({ default: () => <div /> }));
vi.mock('../../EmailLoginForm', () => ({ default: () => <div /> }));
vi.mock('../../../services/stats', () => ({
  statsService: {
    trackFunnel: vi.fn(), trackFunnelOnce: vi.fn(), trackAppOpen: vi.fn(),
    trackConnect: vi.fn(), trackDisconnect: vi.fn(),
  },
}));

const mockPurchase = vi.fn();
let hookState: any;
vi.mock('../../../hooks/useIapPurchase', () => ({
  IAP_PRODUCT_IDS: ['io.kaitu.sub.basic.1y'],
  useIapPurchase: () => hookState,
}));

import IosSubscribePanel from '../IosSubscribePanel';
import { statsService } from '../../../services/stats';

const BASIC_1Y = 'io.kaitu.sub.basic.1y';

describe('IosSubscribePanel funnel events', () => {
  let originalPlatform: any;
  let openExternal: ReturnType<typeof vi.fn>;

  beforeEach(() => {
    vi.clearAllMocks();
    originalPlatform = window._platform;
    openExternal = vi.fn();
    (window as any)._platform = { iap: {}, openExternal };
    mockPurchase.mockReset();
    hookState = {
      products: [{ id: BASIC_1Y, displayName: 'x', description: 'x', displayPrice: 'US$49.00', price: 49, periodUnit: 'year', periodValue: 1 }],
      loadProducts: vi.fn(), productsLoading: false, purchase: mockPurchase, restore: vi.fn(),
      purchasing: false, restoring: false, purchaseError: null, lastGrantedUser: null, clearError: vi.fn(),
    };
  });
  afterEach(() => { (window as any)._platform = originalPlatform; });

  const renderPanel = () =>
    render(<IosSubscribePanel isAuthenticated accountToken="tok" isMembership isExpired={false} />);

  it('purchase click reports checkout_start (apple, row id) exactly once, before purchase()', () => {
    renderPanel();
    fireEvent.click(screen.getByTestId('iap-subscribe-btn'));
    const tracked = (statsService.trackFunnel as any).mock.calls.filter((c: any[]) => c[0] === 'checkout_start');
    expect(tracked).toEqual([['checkout_start', { plan: BASIC_1Y, channel: 'apple' }]]);
    expect(tracked[0][1].plan).toBeTruthy();
    expect(mockPurchase).toHaveBeenCalledWith(BASIC_1Y, 'tok');
    expect((statsService.trackFunnel as any).mock.invocationCallOrder[0])
      .toBeLessThan(mockPurchase.mock.invocationCallOrder[0]);
  });

  it('manage click reports manage_click', () => {
    renderPanel();
    expect(statsService.trackFunnel).not.toHaveBeenCalledWith('manage_click');
    fireEvent.click(screen.getByTestId('iap-manage-btn'));
    expect(statsService.trackFunnel).toHaveBeenCalledWith('manage_click');
    expect(openExternal).toHaveBeenCalled();
  });
});
