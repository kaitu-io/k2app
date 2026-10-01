/**
 * Funnel instrumentation on the purchase page: pricing_view once, plan_select de-duplicated,
 * checkout_start only for REAL orders (never for the preview request the page fires on its own).
 * Real PurchaseClient + real lib/funnel; only the heavy children and the network are replaced.
 */
import { render, screen, waitFor, fireEvent } from '@testing-library/react';
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import PurchaseClient from '../PurchaseClient';

const mockGetUserProfile = vi.fn();
const mockGetPlans = vi.fn();
const mockGetDelegate = vi.fn();
const mockCreateOrder = vi.fn();
const mockNotifyDelegate = vi.fn();
const mockSetDelegate = vi.fn();

vi.mock('next-intl', () => ({
  useTranslations: () => (key: string) => key,
  useLocale: () => 'zh-CN',
}));
vi.mock('@/i18n/routing', () => ({
  useRouter: () => ({ push: vi.fn(), replace: vi.fn() }),
  Link: ({ href, children }: { href: string; children: React.ReactNode }) => <a href={href}>{children}</a>,
}));
vi.mock('@/contexts/AuthContext', () => ({
  useAuth: () => ({ isAuthenticated: true, isAuthLoading: false }),
}));
vi.mock('@/contexts/AppConfigContext', () => ({
  useAppConfig: () => ({ appConfig: null, isLoading: false }),
}));
vi.mock('@/hooks/useEmbedMode', () => ({
  useEmbedMode: () => ({ showNavigation: false, showFooter: false }),
}));
vi.mock('sonner', () => ({ toast: { success: vi.fn(), error: vi.fn() } }));
vi.mock('@/components/Header', () => ({ default: () => null }));
vi.mock('@/components/Footer', () => ({ default: () => null }));
vi.mock('@/components/MembershipBenefits', () => ({ default: () => null }));
vi.mock('@/components/PurchaseStep1', () => ({ default: () => null }));
vi.mock('@/components/PurchaseStep2', () => ({
  default: ({ onPlanChange }: { onPlanChange: (id: string) => void }) => (
    <button
      data-testid="pick-p2"
      onClick={() => {
        // RadioGroup onValueChange + card onClick both fire for one real click
        onPlanChange('p2');
        onPlanChange('p2');
      }}
    />
  ),
}));
vi.mock('@/components/PurchaseStep3', () => ({
  default: ({
    onPurchase,
    onDelegatePay,
    onEmptyStateDelegatePay,
  }: {
    onPurchase: () => void;
    onDelegatePay: () => void;
    onEmptyStateDelegatePay: (email: string) => void;
  }) => (
    <>
      <button data-testid="buy" onClick={onPurchase} />
      <button data-testid="delegate-pay" onClick={onDelegatePay} />
      <button data-testid="delegate-new" onClick={() => onEmptyStateDelegatePay('payer@example.com')} />
    </>
  ),
}));
vi.mock('@/lib/pay-link', () => ({ payLink: () => '/pay', openPayLink: vi.fn() }));
vi.mock('@/lib/api', async (importOriginal) => {
  const actual = await importOriginal<typeof import('@/lib/api')>();
  return {
    ...actual,
    api: {
      getUserProfile: (...a: unknown[]) => mockGetUserProfile(...a),
      getPlans: (...a: unknown[]) => mockGetPlans(...a),
      getDelegate: (...a: unknown[]) => mockGetDelegate(...a),
      createOrder: (...a: unknown[]) => mockCreateOrder(...a),
      notifyDelegate: (...a: unknown[]) => mockNotifyDelegate(...a),
      setDelegate: (...a: unknown[]) => mockSetDelegate(...a),
    },
  };
});

describe('PurchaseClient funnel events', () => {
  let srcs: string[];
  const events = () => srcs.map((s) => new URL(s, 'http://x').searchParams);
  const named = (e: string) => events().filter((p) => p.get('e') === e);

  beforeEach(() => {
    vi.clearAllMocks();
    srcs = [];
    vi.stubGlobal('Image', function (this: { src: string }) {
      Object.defineProperty(this, 'src', { set: (v: string) => srcs.push(v) });
    } as unknown as typeof Image);
    vi.stubGlobal('open', vi.fn(() => null));
    mockGetPlans.mockResolvedValue({
      items: [
        { pid: 'p1', highlight: true },
        { pid: 'p2' },
      ],
    });
    mockGetDelegate.mockResolvedValue(null);
    mockGetUserProfile.mockResolvedValue({ expiredAt: 0, isFirstOrderDone: false });
    mockCreateOrder.mockResolvedValue({ order: { uuid: 'o1', payAmount: 100 } });
    mockNotifyDelegate.mockResolvedValue({});
    mockSetDelegate.mockResolvedValue({ email: 'payer@example.com' });
  });
  afterEach(() => {
    vi.unstubAllGlobals();
  });

  it('pricing_view fires once after plans load; preview order does not fire checkout_start', async () => {
    render(<PurchaseClient />);
    await waitFor(() => expect(named('pricing_view')).toHaveLength(1));
    // the page requests a preview order by itself once a plan is selected
    await waitFor(() => expect(mockCreateOrder).toHaveBeenCalled());
    expect(mockCreateOrder.mock.calls[0][0]).toMatchObject({ preview: true });
    expect(named('checkout_start')).toHaveLength(0);
    expect(named('pricing_view')).toHaveLength(1);
  });

  it('selecting the same plan twice in one click sends plan_select once', async () => {
    render(<PurchaseClient />);
    await waitFor(() => expect(named('pricing_view')).toHaveLength(1));
    fireEvent.click(screen.getByTestId('pick-p2'));
    expect(named('plan_select')).toHaveLength(1);
    expect(named('plan_select')[0].get('p')).toBe('p2');
    fireEvent.click(screen.getByTestId('pick-p2'));
    expect(named('plan_select')).toHaveLength(1);
  });

  it('buying sends checkout_start with s=self', async () => {
    render(<PurchaseClient />);
    await waitFor(() => expect(named('pricing_view')).toHaveLength(1));
    await waitFor(() => expect(mockCreateOrder).toHaveBeenCalled());
    fireEvent.click(screen.getByTestId('buy'));
    await waitFor(() => expect(named('checkout_start')).toHaveLength(1));
    expect(named('checkout_start')[0].get('s')).toBe('self');
    expect(named('checkout_start')[0].get('p')).toBe('p1');
  });

  // The server derives the plan breakdown of the purchase paths from this event,
  // so every checkout_start must name the plan — whichever way the order starts.
  it('delegate pay sends checkout_start with the plan pid and s=delegate', async () => {
    mockGetDelegate.mockResolvedValue({ email: 'payer@example.com' });
    render(<PurchaseClient />);
    await waitFor(() => expect(named('pricing_view')).toHaveLength(1));
    await waitFor(() => expect(mockGetDelegate).toHaveBeenCalled());
    fireEvent.click(screen.getByTestId('pick-p2'));
    await waitFor(() => {
      fireEvent.click(screen.getByTestId('delegate-pay'));
      expect(named('checkout_start').length).toBeGreaterThan(0);
    });
    const start = named('checkout_start')[0];
    expect(start.get('s')).toBe('delegate');
    expect(start.get('p')).toBe('p2');
  });

  it('first-time delegate pay sends checkout_start with the plan pid and s=delegate', async () => {
    render(<PurchaseClient />);
    await waitFor(() => expect(named('pricing_view')).toHaveLength(1));
    fireEvent.click(screen.getByTestId('delegate-new'));
    await waitFor(() => expect(named('checkout_start')).toHaveLength(1));
    expect(named('checkout_start')[0].get('s')).toBe('delegate');
    expect(named('checkout_start')[0].get('p')).toBe('p1');
  });

  it('never sends a checkout_start without a plan pid', async () => {
    mockGetPlans.mockResolvedValue({ items: [] });
    render(<PurchaseClient />);
    await waitFor(() => expect(mockGetPlans).toHaveBeenCalled());
    await waitFor(() => expect(screen.getByTestId('buy')).toBeTruthy());
    fireEvent.click(screen.getByTestId('buy'));
    fireEvent.click(screen.getByTestId('delegate-new'));
    await waitFor(() => expect(mockSetDelegate).toHaveBeenCalled());
    expect(named('checkout_start')).toHaveLength(0);
  });
});
