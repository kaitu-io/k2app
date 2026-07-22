import { render, screen, waitFor, fireEvent, act } from '@testing-library/react';
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import OverleapAccountClient from '../OverleapAccountClient';

const mockGetUserProfile = vi.fn();
const mockCreateStripePortal = vi.fn();
let mockSearch = '';

vi.mock('next-intl', () => ({
  useTranslations: () => (key: string, values?: Record<string, unknown>) =>
    values ? `${key}:${JSON.stringify(values)}` : key,
  useLocale: () => 'en-US',
}));
vi.mock('next/navigation', () => ({
  useSearchParams: () => new URLSearchParams(mockSearch),
}));
vi.mock('@/i18n/routing', () => ({
  Link: ({ href, children, ...rest }: { href: string; children: React.ReactNode }) => (
    <a href={href} {...rest}>{children}</a>
  ),
}));
vi.mock('@/lib/api', () => ({
  api: {
    getUserProfile: (...a: unknown[]) => mockGetUserProfile(...a),
    createStripePortal: (...a: unknown[]) => mockCreateStripePortal(...a),
  },
}));

const STRIPE_SUB = {
  provider: 'stripe',
  tier: 'basic',
  currentPeriodEnd: 1790000000,
  autoRenew: true,
  manage: { kind: 'stripe_portal' as const },
};

describe('OverleapAccountClient', () => {
  beforeEach(() => {
    vi.clearAllMocks();
    mockGetUserProfile.mockResolvedValue({ subscriptions: [STRIPE_SUB] });
    mockCreateStripePortal.mockResolvedValue({ url: 'https://billing.stripe.com/p/x' });
    mockSearch = '';
    Object.defineProperty(window, 'location', {
      value: { assign: vi.fn(), href: 'http://localhost/en-US/account' },
      writable: true,
    });
  });
  afterEach(() => {
    vi.useRealTimers();
  });

  it('renders subscription card with renewal date', async () => {
    render(<OverleapAccountClient />);
    await waitFor(() => expect(screen.getByTestId('subscription-card')).toBeTruthy());
    expect(screen.getByText(/stripe\.renewsOn/)).toBeTruthy();
  });

  it('manage with stripe_portal kind opens billing portal', async () => {
    render(<OverleapAccountClient />);
    await waitFor(() => screen.getByTestId('manage-btn'));
    fireEvent.click(screen.getByTestId('manage-btn'));
    await waitFor(() =>
      expect(mockCreateStripePortal).toHaveBeenCalledWith({ autoRedirectToAuth: false })
    );
    await waitFor(() =>
      expect(window.location.assign).toHaveBeenCalledWith('https://billing.stripe.com/p/x')
    );
  });

  it('manage with url kind navigates directly without portal call', async () => {
    mockGetUserProfile.mockResolvedValue({
      subscriptions: [{ ...STRIPE_SUB, provider: 'apple', manage: { kind: 'url', url: 'https://apps.apple.com/x' } }],
    });
    render(<OverleapAccountClient />);
    await waitFor(() => screen.getByTestId('manage-btn'));
    fireEvent.click(screen.getByTestId('manage-btn'));
    await waitFor(() =>
      expect(window.location.assign).toHaveBeenCalledWith('https://apps.apple.com/x')
    );
    expect(mockCreateStripePortal).not.toHaveBeenCalled();
  });

  it('shows empty state with plans link when no subscription', async () => {
    mockGetUserProfile.mockResolvedValue({ subscriptions: [] });
    render(<OverleapAccountClient />);
    await waitFor(() => expect(screen.getByTestId('no-subscription')).toBeTruthy());
  });

  it('checkout=success polls until subscription appears, then shows download guide', async () => {
    vi.useFakeTimers();
    mockSearch = 'checkout=success';
    mockGetUserProfile
      .mockResolvedValueOnce({ subscriptions: [] })   // 首载：webhook 未到
      .mockResolvedValueOnce({ subscriptions: [] })   // 第 1 次轮询
      .mockResolvedValue({ subscriptions: [STRIPE_SUB] }); // 第 2 次轮询：入账
    render(<OverleapAccountClient />);
    await act(async () => {
      await vi.advanceTimersByTimeAsync(100);
    });
    expect(screen.getByTestId('activating')).toBeTruthy();
    await act(async () => {
      await vi.advanceTimersByTimeAsync(3100); // 第 1 次轮询
      await vi.advanceTimersByTimeAsync(3100); // 第 2 次轮询
    });
    expect(screen.getByTestId('subscription-card')).toBeTruthy();
    expect(screen.getByTestId('download-guide')).toBeTruthy();
  });

  it('checkout=success shows delayed notice after polling exhausts', async () => {
    vi.useFakeTimers();
    mockSearch = 'checkout=success';
    mockGetUserProfile.mockResolvedValue({ subscriptions: [] });
    render(<OverleapAccountClient />);
    await act(async () => {
      await vi.advanceTimersByTimeAsync(100);
      for (let i = 0; i < 10; i++) {
        await vi.advanceTimersByTimeAsync(3100);
      }
    });
    expect(screen.getByTestId('activation-delayed')).toBeTruthy();
  });
});
