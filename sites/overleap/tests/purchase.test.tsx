/**
 * /purchase (Stripe checkout) with real en-GB / en-US copy and a stubbed API.
 */
import React from 'react';
import { render, screen, waitFor, fireEvent } from '@testing-library/react';
import { describe, it, expect, vi, beforeEach } from 'vitest';
import { NextIntlClientProvider } from 'next-intl';
import fs from 'fs';
import path from 'path';
import { NAMESPACES } from '../messages/namespaces';

const mockGetPlans = vi.fn();
const mockCreateStripeCheckout = vi.fn();
const mockPush = vi.fn();
let mockAuth: { profile: unknown; loading: boolean } = { profile: null, loading: false };
let mockSearch = '';

vi.mock('next/navigation', () => ({ useSearchParams: () => new URLSearchParams(mockSearch) }));
vi.mock('@/i18n/routing', () => ({
  // eslint-disable-next-line @typescript-eslint/no-explicit-any
  Link: ({ href, children, ...rest }: any) => <a href={href} {...rest}>{children}</a>,
  useRouter: () => ({ push: mockPush, replace: vi.fn() }),
}));
vi.mock('@/contexts/AuthContext', () => ({ useAuth: () => mockAuth }));
vi.mock('@/lib/api', async (orig) => {
  const real = await orig<typeof import('@/lib/api')>();
  return {
    ...real,
    api: {
      getPlans: (...a: unknown[]) => mockGetPlans(...a),
      createStripeCheckout: (...a: unknown[]) => mockCreateStripeCheckout(...a),
    },
  };
});

import PurchaseClient from '../src/app/[locale]/purchase/PurchaseClient';
import { ApiError } from '@/lib/api';

function loadMessages(locale: string): Record<string, unknown> {
  const dir = path.resolve(__dirname, '../messages', locale);
  return Object.fromEntries(NAMESPACES.map((ns) => [ns, JSON.parse(fs.readFileSync(path.join(dir, `${ns}.json`), 'utf8'))]));
}
function renderAt(locale = 'en-GB') {
  return render(
    <NextIntlClientProvider locale={locale as never} messages={loadMessages(locale)} onError={(e) => { throw e; }}>
      <PurchaseClient />
    </NextIntlClientProvider>,
  );
}

const PLANS = [
  { pid: 'overleap-basic-1y', label: 'Annual', price: 7900, originPrice: 7900, month: 12, highlight: true, product: 'app', currencyPrices: { usd: 7900, gbp: 7900, eur: 8900 } },
  { pid: 'overleap-basic-1m', label: 'Monthly', price: 1199, originPrice: 1199, month: 1, highlight: false, product: 'app', currencyPrices: { usd: 1199, gbp: 999, eur: 1199 } },
];
const SIGNED_IN = { uuid: 'u', hasPassword: true, subscriptions: [] };

beforeEach(() => {
  vi.clearAllMocks();
  // clearAllMocks wipes implementations — set them again.
  mockGetPlans.mockResolvedValue({ items: PLANS });
  mockCreateStripeCheckout.mockResolvedValue({ url: 'https://checkout.stripe.com/c/x' });
  mockAuth = { profile: null, loading: false };
  mockSearch = '';
  Object.defineProperty(window, 'location', { value: { assign: vi.fn(), href: 'http://localhost/en-GB/purchase' }, writable: true });
});

describe('PurchaseClient', () => {
  it('renders both plans with real copy and preselects the highlighted one', async () => {
    renderAt();
    await waitFor(() => expect(screen.getByTestId('plan-card-overleap-basic-1y')).toBeTruthy());
    expect(screen.getByText('Choose your plan')).toBeInTheDocument();
    expect(screen.getByTestId('plan-card-overleap-basic-1y').getAttribute('data-selected')).toBe('true');
    expect(screen.getByTestId('plan-card-overleap-basic-1m').getAttribute('data-selected')).toBe('false');
    expect(screen.getByTestId('plan-includes').textContent).toContain('5 devices at once');
  });

  it('?plan= preselects that plan', async () => {
    mockSearch = 'plan=overleap-basic-1m';
    renderAt();
    await waitFor(() => expect(screen.getByTestId('plan-card-overleap-basic-1m').getAttribute('data-selected')).toBe('true'));
  });

  it('signed out: subscribe sends to login with next back to the chosen plan', async () => {
    renderAt();
    await waitFor(() => screen.getByTestId('plan-card-overleap-basic-1m'));
    fireEvent.click(screen.getByTestId('plan-card-overleap-basic-1m'));
    fireEvent.click(screen.getByTestId('subscribe-btn'));
    expect(mockPush).toHaveBeenCalledWith('/login?next=%2Fpurchase%3Fplan%3Doverleap-basic-1m');
    expect(mockCreateStripeCheckout).not.toHaveBeenCalled();
  });

  it('subscribe is disabled while the session is still being checked', async () => {
    mockAuth = { profile: null, loading: true };
    renderAt();
    await waitFor(() => screen.getByTestId('subscribe-btn'));
    expect(screen.getByTestId('subscribe-btn')).toBeDisabled();
  });

  it('signed in: creates a checkout session and navigates to it', async () => {
    mockAuth = { profile: SIGNED_IN, loading: false };
    renderAt();
    await waitFor(() => screen.getByTestId('subscribe-btn'));
    fireEvent.click(screen.getByTestId('subscribe-btn'));
    await waitFor(() => expect(mockCreateStripeCheckout).toHaveBeenCalledWith('overleap-basic-1y'));
    await waitFor(() => expect(window.location.assign).toHaveBeenCalledWith('https://checkout.stripe.com/c/x'));
  });

  it('an active subscription shows the manage card instead of plans', async () => {
    mockAuth = {
      profile: { ...SIGNED_IN, subscriptions: [{ provider: 'stripe', tier: 'basic', currentPeriodEnd: 1790000000, autoRenew: true, manage: { kind: 'stripe_portal' } }] },
      loading: false,
    };
    renderAt();
    expect(screen.getByTestId('subscribed-card').textContent).toContain('You already have an active subscription.');
    expect(screen.getByRole('link', { name: 'Manage subscription' }).getAttribute('href')).toBe('/account');
    expect(screen.queryByTestId('subscribe-btn')).toBeNull();
  });

  it('?checkout=cancelled shows the cancelled banner', async () => {
    mockSearch = 'checkout=cancelled';
    renderAt();
    expect((await screen.findByTestId('cancelled-banner')).textContent).toContain('Checkout was cancelled');
  });

  it('405001 maps to the channel-unavailable message, anything else to the generic one', async () => {
    mockAuth = { profile: SIGNED_IN, loading: false };
    mockCreateStripeCheckout.mockRejectedValueOnce(new ApiError(405001, 'nope'));
    renderAt();
    await waitFor(() => screen.getByTestId('subscribe-btn'));
    fireEvent.click(screen.getByTestId('subscribe-btn'));
    expect(await screen.findByText('This payment method is not available for your account.')).toBeInTheDocument();
    mockCreateStripeCheckout.mockRejectedValueOnce(new Error('network'));
    fireEvent.click(screen.getByTestId('subscribe-btn'));
    expect(await screen.findByText('Something went wrong. Please try again.')).toBeInTheDocument();
  });

  it('no plans → the empty state', async () => {
    mockGetPlans.mockResolvedValue({ items: [] });
    renderAt();
    expect(await screen.findByText('Plans are being prepared. Please check back soon.')).toBeInTheDocument();
  });
});

describe('PurchaseClient — display currency (currencyPrices from the API)', () => {
  it('en-GB shows pounds and the GBP note', async () => {
    renderAt('en-GB');
    expect((await screen.findByTestId('plan-price-overleap-basic-1y')).textContent).toBe('£79');
    expect(screen.getByTestId('plan-price-overleap-basic-1m').textContent).toContain('£9.99');
    expect(screen.getByText(/Prices in GBP\./)).toBeInTheDocument();
    expect(screen.getByText('Save 34%')).toBeInTheDocument();
  });

  it('en-US shows dollars', async () => {
    renderAt('en-US');
    expect((await screen.findByTestId('plan-price-overleap-basic-1y')).textContent).toBe('$79');
    expect(screen.getByTestId('plan-price-overleap-basic-1m').textContent).toContain('$11.99');
  });

  it('falls back to price (USD cents) when the API omits currencyPrices', async () => {
    mockGetPlans.mockResolvedValue({ items: PLANS.map((plan) => ({ ...plan, currencyPrices: undefined })) });
    renderAt('en-GB');
    expect((await screen.findByTestId('plan-price-overleap-basic-1y')).textContent).toBe('$79');
    expect(screen.getByText(/Prices in USD\./)).toBeInTheDocument();
  });
});
