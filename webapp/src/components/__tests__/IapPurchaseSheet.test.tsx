import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { render, screen, fireEvent } from '@testing-library/react';

// ---- Mocks ----------------------------------------------------------------

vi.mock('react-i18next', () => ({
  useTranslation: () => ({
    t: (key: string, fallback?: string) => fallback ?? key,
  }),
}));

// Mock MUI Dialog to avoid ModalManager jsdom incompatibility.
vi.mock('@mui/material', async () => {
  const actual = await vi.importActual<typeof import('@mui/material')>('@mui/material');
  return {
    ...actual,
    Dialog: ({ open, children }: any) => (open ? <div role="dialog">{children}</div> : null),
    DialogTitle: ({ children }: any) => <div>{children}</div>,
    DialogContent: ({ children }: any) => <div>{children}</div>,
    DialogActions: ({ children }: any) => <div>{children}</div>,
  };
});

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

import IapPurchaseSheet from '../IapPurchaseSheet';

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

describe('IapPurchaseSheet', () => {
  let originalPlatform: any;

  beforeEach(() => {
    originalPlatform = window._platform;
    (window as any)._platform = { iap: {}, openExternal: vi.fn() };
    mockRestore.mockReset();
    mockPurchase.mockReset();
    mockLoadProducts.mockReset();
    mockShowAlert.mockReset();
    hookState = defaultHookState();
  });

  afterEach(() => {
    (window as any)._platform = originalPlatform;
    vi.clearAllMocks();
  });

  it('renders when open', () => {
    render(<IapPurchaseSheet open onClose={vi.fn()} accountToken="tok" />);
    expect(screen.getByRole('dialog')).toBeDefined();
  });

  it('not rendered when open=false', () => {
    render(<IapPurchaseSheet open={false} onClose={vi.fn()} accountToken="tok" />);
    expect(screen.queryByRole('dialog')).toBeNull();
  });

  it('Restore Purchases button present and calls restore', () => {
    render(<IapPurchaseSheet open onClose={vi.fn()} accountToken="tok" />);
    const btn = screen.getByText('purchase:purchase.iap.restorePurchases');
    fireEvent.click(btn);
    expect(mockRestore).toHaveBeenCalled();
  });

  it('ToS + Privacy links present (mandate guard)', () => {
    render(<IapPurchaseSheet open onClose={vi.fn()} accountToken="tok" />);
    expect(screen.getByText('purchase:purchase.iap.terms')).toBeDefined();
    expect(screen.getByText('purchase:purchase.iap.privacy')).toBeDefined();
  });

  it('auto-renewal disclosure present (mandate guard)', () => {
    render(<IapPurchaseSheet open onClose={vi.fn()} accountToken="tok" />);
    expect(screen.getByTestId('iap-auto-renewal-disclosure')).toBeDefined();
  });

  it('Manage Subscription present', () => {
    render(<IapPurchaseSheet open onClose={vi.fn()} accountToken="tok" />);
    expect(screen.getByText('purchase:purchase.iap.manageSubscription')).toBeDefined();
  });

  it('does not use window.confirm', () => {
    const confirmSpy = vi.spyOn(window, 'confirm');
    render(<IapPurchaseSheet open onClose={vi.fn()} accountToken="tok" />);
    fireEvent.click(screen.getByText('purchase:purchase.iap.restorePurchases'));
    expect(confirmSpy).not.toHaveBeenCalled();
    confirmSpy.mockRestore();
  });

  it('renders a single product row driven by the StoreKit product', () => {
    hookState = { ...defaultHookState(), products: [makeProduct()] };
    render(<IapPurchaseSheet open onClose={vi.fn()} accountToken="tok" />);
    // Exactly one product row, identified by the single product id.
    const rows = screen.getAllByTestId(/^iap-product-/);
    expect(rows).toHaveLength(1);
    expect(screen.getByTestId(`iap-product-${BASIC_1Y}`)).toBeDefined();
    // Apple-authoritative displayName + displayPrice are shown (not hardcoded).
    expect(screen.getByText('Basic Yearly')).toBeDefined();
    expect(screen.getByText('US$49.00')).toBeDefined();
  });

  it('falls back to a single placeholder row when products are empty', () => {
    hookState = { ...defaultHookState(), products: [] };
    render(<IapPurchaseSheet open onClose={vi.fn()} accountToken="tok" />);
    const rows = screen.getAllByTestId(/^iap-product-/);
    expect(rows).toHaveLength(1);
    expect(screen.getByTestId(`iap-product-${BASIC_1Y}`)).toBeDefined();
  });

  it('Subscribe purchases the single basic.1y product', () => {
    hookState = { ...defaultHookState(), products: [makeProduct()] };
    render(<IapPurchaseSheet open onClose={vi.fn()} accountToken="tok" />);
    fireEvent.click(screen.getByText('purchase:purchase.iap.subscribeNow'));
    expect(mockPurchase).toHaveBeenCalledWith(BASIC_1Y, 'tok');
  });
});
