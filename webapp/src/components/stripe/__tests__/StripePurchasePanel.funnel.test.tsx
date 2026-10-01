import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen, fireEvent, waitFor } from '@testing-library/react';
import StripePurchasePanel from '../StripePurchasePanel';
import type { Plan } from '../../../services/api-types';
import { brandConfig } from '../../../brands';
import { statsService } from '../../../services/stats';

const checkoutMock = vi.fn();
const openLogin = vi.fn();
let userMock: any;

vi.mock('../../../services/stats', () => ({
  statsService: {
    trackFunnel: vi.fn(), trackFunnelOnce: vi.fn(), trackAppOpen: vi.fn(),
    trackConnect: vi.fn(), trackDisconnect: vi.fn(),
  },
}));
vi.mock('../../../hooks/useStripeCheckout', () => ({
  useStripeCheckout: () => ({
    checkout: checkoutMock, openPortal: vi.fn(),
    loading: false, error: null, clearError: vi.fn(),
  }),
}));
vi.mock('../../../hooks/useSubscriptionAffordance', () => ({
  useSubscriptionAffordance: () => ({ mode: 'subscribe' }),
}));
vi.mock('../../../hooks/useUser', () => ({ useUser: () => userMock }));
vi.mock('../../../stores/login-dialog.store', () => ({
  useLoginDialogStore: (sel: any) => sel({ open: openLogin }),
}));
vi.mock('react-i18next', () => ({
  useTranslation: () => ({ t: (k: string) => k, i18n: { language: 'en-US' } }),
}));

const plans: Plan[] = [
  { pid: 'ol-basic-1m', tier: 'basic', label: 'Monthly', price: 499, originPrice: 499,
    month: 1, highlight: false, maxDevice: 5, maxRouterDevice: 0, maxLanClient: 0, product: 'app' } as Plan,
  { pid: 'ol-basic-1y', tier: 'basic', label: 'Annual', price: 3999, originPrice: 3999,
    month: 12, highlight: true, maxDevice: 5, maxRouterDevice: 0, maxLanClient: 0, product: 'app' } as Plan,
];

const calls = (event: string) =>
  (statsService.trackFunnel as any).mock.calls.filter((c: any[]) => c[0] === event);

describe.runIf(brandConfig.features.stripeCheckout)('StripePurchasePanel funnel events', () => {
  beforeEach(() => {
    vi.clearAllMocks();
    checkoutMock.mockResolvedValue(true);
    userMock = { user: { uuid: 'u1' }, fetchUser: vi.fn() };
    (window as any)._platform = { openExternal: vi.fn(), os: 'macos' };
  });

  it('logged out: subscribe opens login and reports no checkout_start', async () => {
    userMock = { user: null, fetchUser: vi.fn() };
    render(<StripePurchasePanel plans={plans} plansLoading={false} />);
    fireEvent.click(screen.getByTestId('stripe-subscribe-btn'));
    await waitFor(() => expect(openLogin).toHaveBeenCalled());
    expect(checkoutMock).not.toHaveBeenCalled();
    expect(calls('checkout_start')).toHaveLength(0);
  });

  it('logged in: subscribe reports checkout_start with channel stripe and the plan', async () => {
    render(<StripePurchasePanel plans={plans} plansLoading={false} />);
    fireEvent.click(screen.getByTestId('stripe-subscribe-btn'));
    await waitFor(() => expect(checkoutMock).toHaveBeenCalledWith('ol-basic-1y'));
    expect(calls('checkout_start')).toEqual([
      ['checkout_start', { plan: 'ol-basic-1y', channel: 'stripe' }],
    ]);
  });

  it('clicking a plan card reports plan_select with its pid', () => {
    render(<StripePurchasePanel plans={plans} plansLoading={false} />);
    fireEvent.click(screen.getByText('Monthly'));
    expect(calls('plan_select')).toEqual([['plan_select', { plan: 'ol-basic-1m' }]]);
  });
});

describe.runIf(!brandConfig.features.stripeCheckout)('StripePurchasePanel funnel (stripe gate closed)', () => {
  it('the panel itself is brand-agnostic: it still reports when rendered directly', async () => {
    vi.clearAllMocks();
    checkoutMock.mockResolvedValue(true);
    userMock = { user: { uuid: 'u1' }, fetchUser: vi.fn() };
    (window as any)._platform = { openExternal: vi.fn(), os: 'macos' };
    render(<StripePurchasePanel plans={plans} plansLoading={false} />);
    fireEvent.click(screen.getByTestId('stripe-subscribe-btn'));
    await waitFor(() => expect(calls('checkout_start')).toHaveLength(1));
  });
});
