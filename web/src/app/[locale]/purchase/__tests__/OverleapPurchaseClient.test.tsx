import { render, screen, waitFor, fireEvent } from '@testing-library/react';
import { describe, it, expect, vi, beforeEach } from 'vitest';
import OverleapPurchaseClient from '../OverleapPurchaseClient';

const mockGetPlans = vi.fn();
const mockGetUserProfile = vi.fn();
const mockCreateStripeCheckout = vi.fn();
const mockRedirectToLogin = vi.fn();

let mockAuth = { isAuthenticated: false, isAuthLoading: false };
let mockSearch = '';

vi.mock('next-intl', () => ({
  useTranslations: () => {
    const t = (key: string, values?: Record<string, unknown>) =>
      values ? `${key}:${JSON.stringify(values)}` : key;
    return t;
  },
}));
vi.mock('next/navigation', () => ({
  useSearchParams: () => new URLSearchParams(mockSearch),
}));
vi.mock('@/i18n/routing', () => ({
  Link: ({ href, children, ...rest }: { href: string; children: React.ReactNode }) => (
    <a href={href} {...rest}>{children}</a>
  ),
}));
vi.mock('@/contexts/AuthContext', () => ({ useAuth: () => mockAuth }));
vi.mock('@/lib/auth', () => ({ redirectToLogin: (p: string) => mockRedirectToLogin(p) }));
vi.mock('@/lib/api', () => {
  class ApiError extends Error {
    code: number;
    constructor(code: number, message: string) {
      super(message);
      this.code = code;
    }
  }
  return {
    ApiError,
    ErrorCode: { ChannelUnavailable: 405001 },
    api: {
      getPlans: (...a: unknown[]) => mockGetPlans(...a),
      getUserProfile: (...a: unknown[]) => mockGetUserProfile(...a),
      createStripeCheckout: (...a: unknown[]) => mockCreateStripeCheckout(...a),
    },
  };
});

const PLANS = {
  items: [
    { pid: 'overleap-basic-1y', label: 'Annual', price: 8900, originPrice: 8900, month: 12, highlight: true, tier: 'basic', product: 'app' },
    { pid: 'overleap-basic-1m', label: 'Monthly', price: 1199, originPrice: 1199, month: 1, highlight: false, tier: 'basic', product: 'app' },
  ],
};

describe('OverleapPurchaseClient', () => {
  beforeEach(() => {
    vi.clearAllMocks();
    // vi.clearAllMocks 会清实现——必须重设（项目已知 gotcha）
    mockGetPlans.mockResolvedValue(PLANS);
    mockGetUserProfile.mockResolvedValue({ subscriptions: [] });
    mockCreateStripeCheckout.mockResolvedValue({ url: 'https://checkout.stripe.com/c/x' });
    mockAuth = { isAuthenticated: false, isAuthLoading: false };
    mockSearch = '';
    Object.defineProperty(window, 'location', {
      value: { assign: vi.fn(), href: 'http://localhost/en-US/purchase' },
      writable: true,
    });
  });

  it('renders both plans and preselects the highlighted one', async () => {
    render(<OverleapPurchaseClient />);
    await waitFor(() => expect(screen.getByTestId('plan-card-overleap-basic-1y')).toBeTruthy());
    expect(screen.getByTestId('plan-card-overleap-basic-1m')).toBeTruthy();
    expect(screen.getByTestId('plan-card-overleap-basic-1y').getAttribute('data-selected')).toBe('true');
  });

  it('unauthenticated subscribe redirects to login with next back to plan', async () => {
    render(<OverleapPurchaseClient />);
    await waitFor(() => screen.getByTestId('subscribe-btn'));
    fireEvent.click(screen.getByTestId('subscribe-btn'));
    expect(mockRedirectToLogin).toHaveBeenCalledWith('/purchase?plan=overleap-basic-1y');
    expect(mockCreateStripeCheckout).not.toHaveBeenCalled();
  });

  it('authenticated subscribe creates checkout session and navigates', async () => {
    mockAuth = { isAuthenticated: true, isAuthLoading: false };
    render(<OverleapPurchaseClient />);
    await waitFor(() => screen.getByTestId('subscribe-btn'));
    fireEvent.click(screen.getByTestId('subscribe-btn'));
    await waitFor(() =>
      expect(mockCreateStripeCheckout).toHaveBeenCalledWith('overleap-basic-1y', { autoRedirectToAuth: false })
    );
    await waitFor(() =>
      expect(window.location.assign).toHaveBeenCalledWith('https://checkout.stripe.com/c/x')
    );
  });

  it('shows subscribed card instead of plans when a subscription is active', async () => {
    mockAuth = { isAuthenticated: true, isAuthLoading: false };
    mockGetUserProfile.mockResolvedValue({
      subscriptions: [{ provider: 'stripe', tier: 'basic', currentPeriodEnd: 1790000000, autoRenew: true, manage: { kind: 'stripe_portal' } }],
    });
    render(<OverleapPurchaseClient />);
    await waitFor(() => expect(screen.getByTestId('subscribed-card')).toBeTruthy());
    expect(screen.queryByTestId('subscribe-btn')).toBeNull();
  });

  it('shows cancelled banner when checkout=cancelled', async () => {
    mockSearch = 'checkout=cancelled';
    render(<OverleapPurchaseClient />);
    await waitFor(() => expect(screen.getByTestId('cancelled-banner')).toBeTruthy());
  });

  it('maps 405001 to channel-unavailable message', async () => {
    mockAuth = { isAuthenticated: true, isAuthLoading: false };
    const { ApiError } = await import('@/lib/api');
    mockCreateStripeCheckout.mockRejectedValue(new ApiError(405001, 'nope'));
    render(<OverleapPurchaseClient />);
    await waitFor(() => screen.getByTestId('subscribe-btn'));
    fireEvent.click(screen.getByTestId('subscribe-btn'));
    await waitFor(() => expect(screen.getByText('stripe.channelUnavailable')).toBeTruthy());
  });
});
