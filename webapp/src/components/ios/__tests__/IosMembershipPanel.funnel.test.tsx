import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { render, screen, fireEvent } from '@testing-library/react';
import IosMembershipPanel from '../IosMembershipPanel';
import { statsService } from '../../../services/stats';
import type { DataSubscription } from '../../../services/api-types';

vi.mock('react-i18next', () => ({
  useTranslation: () => ({ t: (k: string) => k }),
  initReactI18next: { type: '3rdParty', init: () => {} },
}));
vi.mock('../../../services/stats', () => ({
  statsService: {
    trackFunnel: vi.fn(), trackFunnelOnce: vi.fn(), trackAppOpen: vi.fn(),
    trackConnect: vi.fn(), trackDisconnect: vi.fn(),
  },
}));
vi.mock('../../../hooks/useIapPurchase', () => ({
  useIapPurchase: () => ({ restore: vi.fn(), restoring: false, purchaseError: null, lastGrantedUser: null }),
}));
vi.mock('../../../hooks/useStripeCheckout', () => ({
  useStripeCheckout: () => ({ checkout: vi.fn(), openPortal: vi.fn(), loading: false, error: null, clearError: vi.fn() }),
}));
vi.mock('../../../hooks/useUser', () => ({
  useUser: () => ({ user: { expiredAt: 1_900_000_000, maxDevice: 5, maxRouterDevice: 0, maxLanClient: 0 }, fetchUser: vi.fn() }),
}));
vi.mock('../../../hooks/useAppConfig', () => ({
  useAppConfig: () => ({ appConfig: { inviteReward: { purchaseRewardDays: 7 } }, loading: false, error: null }),
}));
vi.mock('../../../stores/alert.store', () => ({ useAlert: () => ({ showAlert: vi.fn() }) }));
vi.mock('react-router-dom', () => ({ useNavigate: () => vi.fn() }));

const sub: DataSubscription = {
  provider: 'apple', tier: 'basic', currentPeriodEnd: 1_900_000_000, autoRenew: true,
  manage: { kind: 'apple_settings' },
};

describe('IosMembershipPanel funnel events', () => {
  let original: unknown;
  beforeEach(() => {
    vi.clearAllMocks();
    original = (window as { _platform?: unknown })._platform;
    (window as { _platform?: unknown })._platform = { openExternal: vi.fn() };
  });
  afterEach(() => { (window as { _platform?: unknown })._platform = original; });

  it('the manage action reports manage_click once per click', () => {
    render(<IosMembershipPanel mode="manage" activeSub={sub} />);
    expect(statsService.trackFunnel).not.toHaveBeenCalled();
    fireEvent.click(screen.getByTestId('ios-membership-manage-btn'));
    expect(statsService.trackFunnel).toHaveBeenCalledTimes(1);
    expect(statsService.trackFunnel).toHaveBeenCalledWith('manage_click');
  });
});
