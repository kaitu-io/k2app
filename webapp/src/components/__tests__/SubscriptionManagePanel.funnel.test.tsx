import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen, fireEvent, waitFor } from '@testing-library/react';
import SubscriptionManagePanel from '../SubscriptionManagePanel';
import { statsService } from '../../services/stats';

const openPortal = vi.fn();
vi.mock('../../services/stats', () => ({
  statsService: {
    trackFunnel: vi.fn(), trackFunnelOnce: vi.fn(), trackAppOpen: vi.fn(),
    trackConnect: vi.fn(), trackDisconnect: vi.fn(),
  },
}));
vi.mock('../../hooks/useStripeCheckout', () => ({
  useStripeCheckout: () => ({ checkout: vi.fn(), openPortal, loading: false, error: null, clearError: vi.fn() }),
}));
vi.mock('../../hooks/useUser', () => ({ useUser: () => ({ user: { uuid: 'u1' } }) }));
vi.mock('react-i18next', () => ({
  useTranslation: () => ({ t: (k: string) => k, i18n: { language: 'en-US' } }),
}));

const sub = (manage: any) => ({ provider: 'x', tier: 'basic', currentPeriodEnd: 2000000000, autoRenew: true, manage }) as any;
const manageCalls = () => (statsService.trackFunnel as any).mock.calls.filter((c: any[]) => c[0] === 'manage_click');

describe('SubscriptionManagePanel manage_click', () => {
  beforeEach(() => {
    vi.clearAllMocks();
    openPortal.mockResolvedValue(true);
    (window as any)._platform = { openExternal: vi.fn(), os: 'macos' };
  });

  it.each([
    ['stripe_portal', { kind: 'stripe_portal' }, 'stripe-portal-btn'],
    ['apple_settings', { kind: 'apple_settings' }, 'stripe-manage-apple-btn'],
    ['url', { kind: 'url', url: 'https://example.test/manage' }, 'stripe-manage-url-btn'],
  ])('%s exit reports manage_click once', async (_k, manage, testId) => {
    render(<SubscriptionManagePanel activeSub={sub(manage)} />);
    expect(manageCalls()).toHaveLength(0);
    fireEvent.click(screen.getByTestId(testId));
    await waitFor(() => expect(manageCalls()).toHaveLength(1));
  });
});
