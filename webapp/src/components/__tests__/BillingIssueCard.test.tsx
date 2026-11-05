import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen, fireEvent } from '@testing-library/react';
import BillingIssueCard from '../BillingIssueCard';
import { APPLE_BILLING_URL } from '../../utils/billing-issue';
import { statsService } from '../../services/stats';

const openPortal = vi.fn();
vi.mock('../../services/stats', () => ({
  statsService: { trackFunnel: vi.fn() },
}));
vi.mock('../../hooks/useStripeCheckout', () => ({
  useStripeCheckout: () => ({ openPortal, loading: false, error: null }),
}));
vi.mock('react-i18next', () => ({
  useTranslation: () => ({ t: (k: string) => k, i18n: { language: 'en-US' } }),
}));

const sub = (over: any = {}) => ({
  provider: 'apple', tier: 'basic', currentPeriodEnd: 1, autoRenew: true,
  manage: { kind: 'apple_settings' }, isTrial: false, trialEndsAt: 0, billingIssue: true, ...over,
}) as any;

describe('BillingIssueCard', () => {
  let openExternal: ReturnType<typeof vi.fn>;
  beforeEach(() => {
    vi.clearAllMocks();
    openExternal = vi.fn();
    (window as any)._platform = { openExternal, os: 'ios' };
  });

  it('apple_settings on ios: button opens billing url and tracks manage_click', () => {
    render(<BillingIssueCard sub={sub()} />);
    expect(screen.getByTestId('billing-issue-card')).toBeTruthy();
    fireEvent.click(screen.getByTestId('billing-issue-update'));
    expect(openExternal).toHaveBeenCalledWith(APPLE_BILLING_URL);
    expect(statsService.trackFunnel).toHaveBeenCalledWith('manage_click', { source: 'billing_issue' });
  });

  it('apple_settings on android: no button, shows elsewhere hint', () => {
    (window as any)._platform = { openExternal, os: 'android' };
    render(<BillingIssueCard sub={sub()} />);
    expect(screen.queryByTestId('billing-issue-update')).toBeNull();
    expect(screen.getByTestId('billing-issue-card').textContent).toContain('purchase:purchase.billingIssue.appleElsewhere');
  });

  it('stripe_portal: button calls openPortal', () => {
    render(<BillingIssueCard sub={sub({ provider: 'stripe', manage: { kind: 'stripe_portal' } })} />);
    fireEvent.click(screen.getByTestId('billing-issue-update'));
    expect(openPortal).toHaveBeenCalled();
  });

  it('url kind: button opens the url', () => {
    render(<BillingIssueCard sub={sub({ manage: { kind: 'url', url: 'https://x.test/m' } })} />);
    fireEvent.click(screen.getByTestId('billing-issue-update'));
    expect(openExternal).toHaveBeenCalledWith('https://x.test/m');
  });

  it('body text depends on isTrial', () => {
    const { unmount } = render(<BillingIssueCard sub={sub({ isTrial: true })} />);
    const t1 = screen.getByTestId('billing-issue-card').textContent!;
    expect(t1).toContain('purchase:purchase.billingIssue.trialBody');
    unmount();
    render(<BillingIssueCard sub={sub()} />);
    const t2 = screen.getByTestId('billing-issue-card').textContent!;
    expect(t2).toContain('purchase:purchase.billingIssue.body');
    expect(t2).not.toContain('trialBody');
  });

  it('compact still renders card and button', () => {
    render(<BillingIssueCard sub={sub()} compact />);
    expect(screen.getByTestId('billing-issue-update')).toBeTruthy();
  });
});
