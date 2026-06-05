import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { render, screen, fireEvent } from '@testing-library/react';

// ---- Mocks ----------------------------------------------------------------

vi.mock('react-i18next', () => ({
  useTranslation: () => ({
    t: (key: string, fallback?: string) => fallback ?? key,
  }),
}));

vi.mock('../../hooks/useAppLinks', () => ({
  useAppLinks: () => ({
    links: {
      termsOfServiceUrl: 'https://kaitu.io/terms',
      privacyPolicyUrl: 'https://kaitu.io/privacy',
    },
    loading: false,
    error: null,
    currentLang: 'en-US',
  }),
}));

const mockShowAlert = vi.fn();
vi.mock('../../stores/alert.store', () => ({
  useAlert: () => ({ showAlert: mockShowAlert }),
}));

const mockFetchUser = vi.fn();
vi.mock('../../hooks/useUser', () => ({
  useUser: () => ({ fetchUser: mockFetchUser }),
}));

// Stub child components — irrelevant to subscribe-flow assertions, and they pull
// in heavy stores (EmailLoginForm) / icons (MembershipBenefits).
vi.mock('../MembershipBenefits', () => ({
  default: () => <div data-testid="membership-benefits" />,
}));
vi.mock('../EmailLoginForm', () => ({
  default: () => <div data-testid="email-login-form" />,
}));

const mockRestore = vi.fn();
const mockPurchase = vi.fn();
const mockLoadProducts = vi.fn();
let hookState: any;
vi.mock('../../hooks/useIapPurchase', () => ({
  IAP_PRODUCT_IDS: ['io.kaitu.sub.basic.1y'],
  useIapPurchase: () => hookState,
}));

const BASIC_1Y = 'io.kaitu.sub.basic.1y';

function makeProduct() {
  return {
    id: BASIC_1Y,
    displayName: 'Basic Yearly',
    description: 'Kaitu Basic — billed yearly',
    displayPrice: 'US$49.00',
    price: 49,
    periodUnit: 'year' as const,
    periodValue: 1,
  };
}

import IosSubscribePanel from '../IosSubscribePanel';

function defaultHookState() {
  return {
    products: [],
    loadProducts: mockLoadProducts,
    productsLoading: false,
    purchase: mockPurchase,
    restore: mockRestore,
    purchasing: false,
    restoring: false,
    purchaseError: null,
    lastGrantedUser: null,
    clearError: vi.fn(),
  };
}

function renderPanel(props: Partial<React.ComponentProps<typeof IosSubscribePanel>> = {}) {
  return render(
    <IosSubscribePanel
      isAuthenticated
      accountToken="tok"
      isMembership
      isExpired={false}
      {...props}
    />,
  );
}

describe('IosSubscribePanel', () => {
  let originalPlatform: any;

  beforeEach(() => {
    originalPlatform = window._platform;
    (window as any)._platform = { iap: {}, openExternal: vi.fn() };
    mockRestore.mockReset();
    mockPurchase.mockReset();
    mockLoadProducts.mockReset();
    mockShowAlert.mockReset();
    mockFetchUser.mockReset();
    hookState = defaultHookState();
  });

  afterEach(() => {
    (window as any)._platform = originalPlatform;
    vi.clearAllMocks();
  });

  it('renders inline (no dialog) and loads products on mount', () => {
    renderPanel();
    expect(screen.getByTestId('ios-subscribe-panel')).toBeDefined();
    expect(screen.queryByRole('dialog')).toBeNull();
    expect(mockLoadProducts).toHaveBeenCalled();
  });

  it('Restore Purchases present and calls restore', () => {
    renderPanel();
    fireEvent.click(screen.getByText('purchase:purchase.iap.restorePurchases'));
    expect(mockRestore).toHaveBeenCalled();
  });

  it('ToS + Privacy links present (Apple mandate guard)', () => {
    renderPanel();
    expect(screen.getByText('purchase:purchase.iap.terms')).toBeDefined();
    expect(screen.getByText('purchase:purchase.iap.privacy')).toBeDefined();
  });

  it('auto-renewal disclosure present (Apple mandate guard)', () => {
    renderPanel();
    expect(screen.getByTestId('iap-auto-renewal-disclosure')).toBeDefined();
  });

  it('Manage Subscription present', () => {
    renderPanel();
    expect(screen.getByText('purchase:purchase.iap.manageSubscription')).toBeDefined();
  });

  it('does not use window.confirm', () => {
    const confirmSpy = vi.spyOn(window, 'confirm');
    renderPanel();
    fireEvent.click(screen.getByText('purchase:purchase.iap.restorePurchases'));
    expect(confirmSpy).not.toHaveBeenCalled();
    confirmSpy.mockRestore();
  });

  it('renders a single product row driven by the StoreKit product', () => {
    hookState = { ...defaultHookState(), products: [makeProduct()] };
    renderPanel();
    const rows = screen.getAllByTestId(/^iap-product-/);
    expect(rows).toHaveLength(1);
    expect(screen.getByTestId(`iap-product-${BASIC_1Y}`)).toBeDefined();
    // Apple-authoritative displayName + displayPrice are shown (not hardcoded).
    expect(screen.getByText('Basic Yearly')).toBeDefined();
    expect(screen.getByText('US$49.00')).toBeDefined();
  });

  it('falls back to a single placeholder row when products are empty', () => {
    hookState = { ...defaultHookState(), products: [] };
    renderPanel();
    const rows = screen.getAllByTestId(/^iap-product-/);
    expect(rows).toHaveLength(1);
    expect(screen.getByTestId(`iap-product-${BASIC_1Y}`)).toBeDefined();
  });

  it('Subscribe purchases the single basic.1y product with the account token', () => {
    hookState = { ...defaultHookState(), products: [makeProduct()] };
    renderPanel();
    fireEvent.click(screen.getByTestId('iap-subscribe-btn'));
    expect(mockPurchase).toHaveBeenCalledWith(BASIC_1Y, 'tok');
  });

  it('does NOT show the multi-plan / WordGate list (single product only)', () => {
    hookState = { ...defaultHookState(), products: [makeProduct()] };
    renderPanel();
    // Center plan rows are testid-less Cards with promo/total fields; the IAP
    // panel exposes exactly one product row and no "select plan" affordance.
    expect(screen.getAllByTestId(/^iap-product-/)).toHaveLength(1);
    expect(screen.queryByText('purchase:purchase.selectPlan')).toBeNull();
  });

  describe('unauthenticated prospect (subscribe mode, no account)', () => {
    it('shows the inline login form and gates the subscribe button', () => {
      hookState = { ...defaultHookState(), products: [makeProduct()] };
      renderPanel({ isAuthenticated: false, accountToken: '' });
      // Login form is shown inline (no dialog, no redirect).
      expect(screen.getByTestId('email-login-form')).toBeDefined();
      // Button shows the login-first label and does not trigger a purchase.
      const btn = screen.getByTestId('iap-subscribe-btn');
      expect(screen.getByText('purchase:purchase.iap.loginToSubscribe')).toBeDefined();
      fireEvent.click(btn);
      expect(mockPurchase).not.toHaveBeenCalled();
    });
  });
});
