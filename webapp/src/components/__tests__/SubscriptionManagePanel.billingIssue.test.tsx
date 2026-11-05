import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen } from '@testing-library/react';
import SubscriptionManagePanel from '../SubscriptionManagePanel';

vi.mock('../../services/stats', () => ({ statsService: { trackFunnel: vi.fn() } }));
vi.mock('../../hooks/useStripeCheckout', () => ({
  useStripeCheckout: () => ({ openPortal: vi.fn(), loading: false, error: null }),
}));
vi.mock('../../hooks/useUser', () => ({ useUser: () => ({ user: { uuid: 'u1' } }) }));
vi.mock('react-i18next', () => ({
  useTranslation: () => ({ t: (k: string) => k, i18n: { language: 'en-US' } }),
}));

const sub = (billingIssue?: boolean) => ({
  provider: 'stripe', tier: 'basic', currentPeriodEnd: 2000000000, autoRenew: true,
  manage: { kind: 'stripe_portal' }, isTrial: false, trialEndsAt: 0, billingIssue,
}) as any;

describe('SubscriptionManagePanel billing issue', () => {
  beforeEach(() => { (window as any)._platform = { openExternal: vi.fn(), os: 'macos' }; });

  it('shows the card when billingIssue is true', () => {
    render(<SubscriptionManagePanel activeSub={sub(true)} />);
    expect(screen.getByTestId('billing-issue-card')).toBeTruthy();
  });
  it('no card when false or absent', () => {
    const { unmount } = render(<SubscriptionManagePanel activeSub={sub(false)} />);
    expect(screen.queryByTestId('billing-issue-card')).toBeNull();
    unmount();
    render(<SubscriptionManagePanel activeSub={sub(undefined)} />);
    expect(screen.queryByTestId('billing-issue-card')).toBeNull();
  });
});
