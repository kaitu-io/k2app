import { describe, it, expect, vi, beforeEach } from 'vitest';
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
  beforeEach(() => {
    checkoutMock.mockReset().mockResolvedValue(true);
    portalMock.mockReset().mockResolvedValue(true);
    affordanceMock = { mode: 'subscribe' };
    userMock = { user: { uuid: 'u1' }, fetchUser: vi.fn() };
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
