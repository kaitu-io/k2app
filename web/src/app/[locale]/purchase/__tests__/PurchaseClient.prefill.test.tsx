/**
 * Trial-recall links land here as ?campaign=&plan=#email= (email only in the fragment). The page must honor them:
 * campaign code prefilled + expanded (preview priced with it), plan pre-selected when it
 * exists, login email prefilled — and none of it lost by the inline login.
 */
import { render, screen, waitFor, fireEvent } from '@testing-library/react';
import { describe, it, expect, vi, beforeEach } from 'vitest';
import PurchaseClient from '../PurchaseClient';
import { ApiError, ErrorCode } from '@/lib/api';

const mockGetUserProfile = vi.fn();
const mockGetPlans = vi.fn();
const mockGetDelegate = vi.fn();
const mockCreateOrder = vi.fn();
let mockQuery = '';

vi.mock('next/navigation', () => ({
  useSearchParams: () => new URLSearchParams(mockQuery),
}));
// Stable identity like the real next-intl `t`: a fresh function per render would re-run every
// effect that depends on `t` (plan fetch → preview) and mask missing dependencies.
const stableT = (key: string) => key;
vi.mock('next-intl', () => ({
  useTranslations: () => stableT,
  useLocale: () => 'zh-CN',
}));
vi.mock('@/i18n/routing', () => ({
  useRouter: () => ({ push: vi.fn(), replace: vi.fn() }),
  Link: ({ href, children }: { href: string; children: React.ReactNode }) => <a href={href}>{children}</a>,
}));
let mockAuthed = false;
vi.mock('@/contexts/AuthContext', () => ({
  useAuth: () => ({ isAuthenticated: mockAuthed, isAuthLoading: false }),
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
vi.mock('@/components/PurchaseStep1', () => ({
  default: ({ initialEmail, onLoginSuccess }: { initialEmail?: string; onLoginSuccess: () => void }) => (
    <>
      <input data-testid="email" value={initialEmail ?? ''} readOnly />
      <button data-testid="login" onClick={onLoginSuccess} />
    </>
  ),
}));
vi.mock('@/components/PurchaseStep2', () => ({
  default: ({ selectedPlan }: { selectedPlan: string }) => <div data-testid="selected-plan">{selectedPlan}</div>,
}));
vi.mock('@/components/PurchaseStep3', () => ({
  default: ({ showCampaign, campaignCode, campaignError }: { showCampaign: boolean; campaignCode: string; campaignError?: string }) => (
    <>
      <input data-testid="campaign" data-shown={String(showCampaign)} value={campaignCode} readOnly />
      <div data-testid="campaign-error">{campaignError}</div>
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
    },
  };
});

describe('PurchaseClient URL prefill', () => {
  beforeEach(() => {
    vi.clearAllMocks();
    mockQuery = '';
    mockAuthed = false;
    window.history.replaceState(null, '', '/zh-CN/purchase');
    mockGetPlans.mockResolvedValue({ items: [{ pid: 'p1', highlight: true }, { pid: 'p12' }] });
    mockGetDelegate.mockResolvedValue(null);
    mockGetUserProfile.mockResolvedValue({ expiredAt: 0, isFirstOrderDone: false });
    mockCreateOrder.mockResolvedValue({ order: { uuid: 'o1', payAmount: 100 } });
  });

  it('prefills and expands campaign code from ?campaign=', async () => {
    mockQuery = 'campaign=TRIAL80';
    render(<PurchaseClient />);
    const input = (await screen.findByTestId('campaign')) as HTMLInputElement;
    expect(input.value).toBe('TRIAL80');
    expect(input.dataset.shown).toBe('true');
    await waitFor(() => expect(mockCreateOrder).toHaveBeenCalled());
    expect(mockCreateOrder.mock.calls[0][0]).toMatchObject({ preview: true, campaignCode: 'TRIAL80' });
  });

  it('selects ?plan= when it exists in the list', async () => {
    mockQuery = 'plan=p12';
    render(<PurchaseClient />);
    await waitFor(() => expect(screen.getByTestId('selected-plan').textContent).toBe('p12'));
  });

  it('falls back to default plan when ?plan= is unknown', async () => {
    mockQuery = 'plan=nope';
    render(<PurchaseClient />);
    await waitFor(() => expect(screen.getByTestId('selected-plan').textContent).toBe('p1'));
  });

  it('prefills login email from #email= and strips it from the URL', async () => {
    mockQuery = 'campaign=TRIAL80';
    window.history.replaceState(null, '', '/zh-CN/purchase?campaign=TRIAL80#email=a%2Bb%40example.com');
    render(<PurchaseClient />);
    await waitFor(() =>
      expect((screen.getByTestId('email') as HTMLInputElement).value).toBe('a+b@example.com'),
    );
    expect(window.location.hash).toBe('');
    expect(window.location.search).toBe('?campaign=TRIAL80');
  });

  it('ignores ?email= in the query (never read PII from the query)', async () => {
    mockQuery = 'email=a%40example.com';
    render(<PurchaseClient />);
    expect(((await screen.findByTestId('email')) as HTMLInputElement).value).toBe('');
  });

  it('keeps campaign code after inline login success', async () => {
    mockQuery = 'campaign=TRIAL80&plan=p12';
    render(<PurchaseClient />);
    await waitFor(() => expect(screen.getByTestId('selected-plan').textContent).toBe('p12'));
    fireEvent.click(screen.getByTestId('login'));
    expect((screen.getByTestId('campaign') as HTMLInputElement).value).toBe('TRIAL80');
    expect(screen.getByTestId('selected-plan').textContent).toBe('p12');
  });

  it('re-prices with the campaign code once the visitor signs in', async () => {
    // Signed-out preview is rejected (401); the page must ask again after login,
    // otherwise a recall link keeps showing the undiscounted price.
    mockQuery = 'campaign=TRIAL80&plan=p12';
    mockCreateOrder.mockRejectedValue(new ApiError(401, 'authentication failed'));
    const { rerender } = render(<PurchaseClient />);
    await waitFor(() => expect(screen.getByTestId('selected-plan').textContent).toBe('p12'));
    await new Promise((r) => setTimeout(r, 600)); // let the debounced signed-out previews settle
    const before = mockCreateOrder.mock.calls.length;
    expect(before).toBeGreaterThan(0);
    mockCreateOrder.mockResolvedValue({ order: { uuid: 'o1', payAmount: 100 } });
    mockAuthed = true;
    rerender(<PurchaseClient />);
    await waitFor(() => expect(mockCreateOrder.mock.calls.length).toBe(before + 1));
    expect(mockCreateOrder.mock.calls[before][0]).toMatchObject({ preview: true, plan: 'p12', campaignCode: 'TRIAL80' });
  });

  it('shows the invalid-campaign message for a prefilled bad code', async () => {
    mockQuery = 'campaign=BADCODE';
    mockCreateOrder.mockRejectedValue(new ApiError(ErrorCode.InvalidCampaignCode, 'invalid'));
    render(<PurchaseClient />);
    await waitFor(() =>
      expect(screen.getByTestId('campaign-error').textContent).toBe('purchase.purchase.invalidCampaignCode'),
    );
    expect(mockCreateOrder.mock.calls[0][0]).toMatchObject({ preview: true, campaignCode: 'BADCODE' });
    expect((screen.getByTestId('campaign') as HTMLInputElement).value).toBe('BADCODE');
  });
});
