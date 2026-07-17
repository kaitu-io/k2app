import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { render, screen, fireEvent, waitFor } from '@testing-library/react';
import StripePurchasePanel from '../StripePurchasePanel';
import type { Plan } from '../../../services/api-types';

const checkoutMock = vi.fn();
const portalMock = vi.fn();
let affordanceMock: any = { mode: 'subscribe' };
let userMock: any = { user: { uuid: 'u1' }, fetchUser: vi.fn() };

vi.mock('../../../hooks/useStripeCheckout', () => ({
  useStripeCheckout: () => ({
    checkout: checkoutMock, openPortal: portalMock,
    loading: false, error: null, clearError: vi.fn(),
  }),
}));
vi.mock('../../../hooks/useSubscriptionAffordance', () => ({
  useSubscriptionAffordance: () => affordanceMock,
}));
vi.mock('../../../hooks/useUser', () => ({ useUser: () => userMock }));
vi.mock('../../../stores/login-dialog.store', () => ({
  useLoginDialogStore: (sel: any) => sel({ open: vi.fn() }),
}));
vi.mock('react-i18next', () => ({
  useTranslation: () => ({ t: (k: string) => k, i18n: { language: 'en-US' } }),
}));

const plans: Plan[] = [
  { pid: 'ol-basic-1y', tier: 'basic', label: 'Annual', price: 3999, originPrice: 3999,
    month: 12, highlight: true, maxDevice: 5, maxRouterDevice: 0, maxLanClient: 0, product: 'app' } as Plan,
];

describe('StripePurchasePanel', () => {
  let originalPlatform: typeof window._platform;

  beforeEach(() => {
    checkoutMock.mockReset().mockResolvedValue(true);
    portalMock.mockReset().mockResolvedValue(true);
    affordanceMock = { mode: 'subscribe' };
    userMock = { user: { uuid: 'u1' }, fetchUser: vi.fn() };
    originalPlatform = window._platform;
    (window as any)._platform = { openExternal: vi.fn() };
  });

  afterEach(() => {
    (window as any)._platform = originalPlatform;
  });

  it('subscribe mode: renders plans and fires checkout with selected pid', async () => {
    render(<StripePurchasePanel plans={plans} plansLoading={false} />);
    fireEvent.click(screen.getByTestId('stripe-subscribe-btn'));
    await waitFor(() => expect(checkoutMock).toHaveBeenCalledWith('ol-basic-1y'));
    // 打开外链后出现"完成支付后刷新"提示
    expect(await screen.findByTestId('stripe-opened-hint')).toBeInTheDocument();
  });

  it('manage mode: renders portal button and opens portal', async () => {
    affordanceMock = {
      mode: 'manage',
      activeSub: { provider: 'stripe', tier: 'basic', currentPeriodEnd: 2000000000,
        autoRenew: true, manage: { kind: 'stripe_portal' } },
    };
    render(<StripePurchasePanel plans={plans} plansLoading={false} />);
    fireEvent.click(screen.getByTestId('stripe-portal-btn'));
    await waitFor(() => expect(portalMock).toHaveBeenCalled());
  });

  // overleap also runs apple_iap — a user who subscribed via iOS sees
  // manage.kind === 'apple_settings' here (desktop/web), not stripe_portal.
  // The panel must never show Stripe portal copy/button for that subscription.
  it('manage mode (apple_settings): opens App Store subscriptions, no Stripe portal button', async () => {
    affordanceMock = {
      mode: 'manage',
      activeSub: { provider: 'apple', tier: 'basic', currentPeriodEnd: 2000000000,
        autoRenew: true, manage: { kind: 'apple_settings' } },
    };
    render(<StripePurchasePanel plans={plans} plansLoading={false} />);
    expect(screen.queryByTestId('stripe-portal-btn')).toBeNull();
    fireEvent.click(screen.getByTestId('stripe-manage-apple-btn'));
    await waitFor(() =>
      expect(window._platform!.openExternal).toHaveBeenCalledWith(
        'itms-apps://apps.apple.com/account/subscriptions',
      ),
    );
    expect(portalMock).not.toHaveBeenCalled();
  });

  // manage.kind missing/unknown: fail-safe — no dead button that would call
  // the wrong provider or crash. Status-only.
  it('manage mode (kind missing): shows no action button, never calls openPortal', () => {
    affordanceMock = {
      mode: 'manage',
      activeSub: { provider: 'stripe', tier: 'basic', currentPeriodEnd: 2000000000,
        autoRenew: true, manage: {} },
    };
    render(<StripePurchasePanel plans={plans} plansLoading={false} />);
    expect(screen.queryByTestId('stripe-portal-btn')).toBeNull();
    expect(screen.queryByTestId('stripe-manage-apple-btn')).toBeNull();
    expect(screen.queryByTestId('stripe-manage-url-btn')).toBeNull();
    expect(portalMock).not.toHaveBeenCalled();
  });

  it('empty plans: shows noPlans hint, no subscribe button', () => {
    render(<StripePurchasePanel plans={[]} plansLoading={false} />);
    expect(screen.getByText('purchase:purchase.stripe.noPlans')).toBeInTheDocument();
    expect(screen.queryByTestId('stripe-subscribe-btn')).toBeNull();
  });

  it('unauthenticated subscribe opens login dialog instead of checkout', async () => {
    userMock = { user: null, fetchUser: vi.fn() };
    render(<StripePurchasePanel plans={plans} plansLoading={false} />);
    fireEvent.click(screen.getByTestId('stripe-subscribe-btn'));
    await waitFor(() => expect(checkoutMock).not.toHaveBeenCalled());
  });
});
