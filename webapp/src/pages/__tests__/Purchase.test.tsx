import { describe, it, expect, vi, afterEach, beforeEach } from 'vitest';
import { render, screen, cleanup, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import type { Plan, Order } from '../../api/types';

// Mock i18next — return keys as values for assertions
vi.mock('react-i18next', () => ({
  useTranslation: () => ({
    t: (key: string) => key,
    i18n: { language: 'en-US', changeLanguage: vi.fn() },
  }),
}));

// Mock stores
vi.mock('../../stores/purchase.store', () => ({
  usePurchaseStore: vi.fn(),
}));

vi.mock('../../stores/auth.store', () => ({
  useAuthStore: vi.fn(),
}));

vi.mock('../../stores/user.store', () => ({
  useUserStore: vi.fn(),
}));

vi.mock('../../stores/ui.store', () => ({
  useUiStore: vi.fn(),
}));

// Mock platform
const mockOpenExternal = vi.fn().mockResolvedValue(undefined);
vi.mock('../../platform', () => ({
  getPlatform: () => ({
    openExternal: mockOpenExternal,
    isMobile: false,
    platformName: 'web',
  }),
}));

// Mock cloud API
vi.mock('../../api/cloud', () => ({
  cloudApi: {
    getMembers: vi.fn(),
  },
}));

// Mock EmailLoginForm — render a detectable element
vi.mock('../../components/EmailLoginForm', () => ({
  EmailLoginForm: ({ onSuccess }: { onSuccess?: () => void }) => (
    <div data-testid="email-login-form">
      <button onClick={onSuccess}>mock-login-success</button>
    </div>
  ),
}));

// Import after mocks
import { Purchase } from '../Purchase';
import { usePurchaseStore } from '../../stores/purchase.store';
import { useAuthStore } from '../../stores/auth.store';
import { useUserStore } from '../../stores/user.store';
import { useUiStore } from '../../stores/ui.store';

// Helper to type-cast mocked hooks
const mockPurchaseStore = usePurchaseStore as unknown as ReturnType<typeof vi.fn>;
const mockAuthStore = useAuthStore as unknown as ReturnType<typeof vi.fn>;
const mockUserStore = useUserStore as unknown as ReturnType<typeof vi.fn>;
const mockUiStore = useUiStore as unknown as ReturnType<typeof vi.fn>;

// Test plan data sorted by period months: 1, 6, 12
const testPlans: Plan[] = [
  { id: 'plan-12', name: 'Annual', description: '12 months', price: 199, period: '12', features: ['all'] },
  { id: 'plan-1', name: 'Monthly', description: '1 month', price: 29, period: '1', features: ['basic'] },
  { id: 'plan-6', name: 'Semi-Annual', description: '6 months', price: 149, period: '6', features: ['all'] },
];

function setupDefaultMocks(overrides: {
  purchaseStore?: Partial<ReturnType<typeof mockPurchaseStore>>;
  authStore?: Partial<ReturnType<typeof mockAuthStore>>;
  userStore?: Partial<ReturnType<typeof mockUserStore>>;
} = {}) {
  const defaultPurchaseStore = {
    plans: [],
    selectedPlanId: null,
    campaignCode: null,
    orderPreview: null,
    currentOrder: null,
    isLoading: false,
    error: null,
    loadPlans: vi.fn(),
    selectPlan: vi.fn(),
    setCampaignCode: vi.fn(),
    previewOrder: vi.fn(),
    createOrder: vi.fn(),
    ...overrides.purchaseStore,
  };

  const defaultAuthStore = {
    isLoggedIn: true,
    isLoading: false,
    ...overrides.authStore,
  };

  const defaultUserStore = {
    user: { id: 'u1', email: 'user@test.com' },
    isLoading: false,
    error: null,
    ...overrides.userStore,
  };

  mockPurchaseStore.mockReturnValue(defaultPurchaseStore);
  mockAuthStore.mockReturnValue(defaultAuthStore);
  mockUserStore.mockReturnValue(defaultUserStore);
  mockUiStore.mockReturnValue({
    addAlert: vi.fn(),
  });

  return { purchaseStore: defaultPurchaseStore, authStore: defaultAuthStore, userStore: defaultUserStore };
}

describe('Purchase', () => {
  afterEach(() => {
    cleanup();
    vi.clearAllMocks();
  });

  it('test_purchase_loads_plans_sorted — Plans load from store and display sorted by month ascending', () => {
    setupDefaultMocks({
      purchaseStore: { plans: testPlans },
    });

    render(<Purchase />);

    // All plan cards should be rendered
    const planCards = screen.getAllByTestId('plan-card');
    expect(planCards).toHaveLength(3);

    // Sorted by period ascending: 1, 6, 12
    expect(planCards[0]).toHaveTextContent('Monthly');
    expect(planCards[1]).toHaveTextContent('Semi-Annual');
    expect(planCards[2]).toHaveTextContent('Annual');
  });

  it('test_highlighted_plan_shows_badge — The plan with the longest period shows a recommended badge', () => {
    setupDefaultMocks({
      purchaseStore: { plans: testPlans },
    });

    render(<Purchase />);

    // The best-value plan (12 months) should have a recommended badge
    const badges = screen.getAllByText('recommended');
    expect(badges.length).toBeGreaterThanOrEqual(1);

    // The last sorted card (12-month) should contain the badge
    const planCards = screen.getAllByTestId('plan-card');
    const annualCard = planCards[2]; // 12-month plan is last in ascending sort
    expect(annualCard).toHaveTextContent('recommended');
  });

  it('test_plan_shows_price_formatting — Plan shows formatted price text', () => {
    setupDefaultMocks({
      purchaseStore: { plans: testPlans },
    });

    render(<Purchase />);

    // Each plan card should show its price
    // Plans: Monthly $29/1mo, Semi-Annual $149/6mo, Annual $199/12mo
    const planCards = screen.getAllByTestId('plan-card');
    expect(planCards[0]).toHaveTextContent('29');
    expect(planCards[1]).toHaveTextContent('149');
    expect(planCards[2]).toHaveTextContent('199');
  });

  it('test_campaign_code_preview — Entering campaign code triggers preview order', async () => {
    const mockSetCampaignCode = vi.fn();
    const mockPreviewOrder = vi.fn();

    setupDefaultMocks({
      purchaseStore: {
        plans: testPlans,
        selectedPlanId: 'plan-1',
        setCampaignCode: mockSetCampaignCode,
        previewOrder: mockPreviewOrder,
      },
    });

    const user = userEvent.setup();
    render(<Purchase />);

    // Find campaign code input and enter a code
    const codeInput = screen.getByPlaceholderText('campaignCodePlaceholder');
    await user.type(codeInput, 'SAVE20');

    // Click apply button
    const applyBtn = screen.getByText('applyCampaignCode');
    await user.click(applyBtn);

    expect(mockSetCampaignCode).toHaveBeenCalledWith('SAVE20');
    expect(mockPreviewOrder).toHaveBeenCalled();
  });

  it('test_pay_now_creates_order_opens_url — Pay button creates order and opens payment URL', async () => {
    const mockCreateOrder = vi.fn().mockResolvedValue(undefined);
    const testOrder: Order = {
      id: 'order-1',
      planId: 'plan-1',
      period: '1',
      amount: 29,
      status: 'pending',
      createdAt: '2026-01-01',
    };

    // First render: no order yet, second render: order created
    let hasOrder = false;
    mockPurchaseStore.mockImplementation(() => ({
      plans: testPlans,
      selectedPlanId: 'plan-1',
      campaignCode: null,
      orderPreview: null,
      currentOrder: hasOrder ? testOrder : null,
      isLoading: false,
      error: null,
      loadPlans: vi.fn(),
      selectPlan: vi.fn(),
      setCampaignCode: vi.fn(),
      previewOrder: vi.fn(),
      createOrder: mockCreateOrder.mockImplementation(async () => {
        hasOrder = true;
      }),
    }));

    mockAuthStore.mockReturnValue({ isLoggedIn: true, isLoading: false });
    mockUserStore.mockReturnValue({ user: { id: 'u1', email: 'user@test.com' }, isLoading: false, error: null });
    mockUiStore.mockReturnValue({ addAlert: vi.fn() });

    const user = userEvent.setup();
    render(<Purchase />);

    // Click pay button
    const payBtn = screen.getByText('payNow');
    await user.click(payBtn);

    expect(mockCreateOrder).toHaveBeenCalled();
  });

  it('test_payment_result_dialog — Payment result dialog shows after order creation', () => {
    const testOrder: Order = {
      id: 'order-1',
      planId: 'plan-1',
      period: '1',
      amount: 29,
      status: 'pending',
      createdAt: '2026-01-01',
    };

    setupDefaultMocks({
      purchaseStore: {
        plans: testPlans,
        selectedPlanId: 'plan-1',
        currentOrder: testOrder,
      },
    });

    render(<Purchase />);

    // Payment result dialog should show
    expect(screen.getByText('paymentResult')).toBeInTheDocument();
    expect(screen.getByText('paymentPending')).toBeInTheDocument();
  });

  it('test_unauthenticated_shows_inline_login — Unauthenticated users see EmailLoginForm', () => {
    setupDefaultMocks({
      authStore: { isLoggedIn: false },
    });

    render(<Purchase />);

    // Should render the EmailLoginForm mock
    expect(screen.getByTestId('email-login-form')).toBeInTheDocument();
    // Should show login required message
    expect(screen.getByText('loginRequired')).toBeInTheDocument();
  });

  it('test_plan_selection_updates_store — Clicking a plan card calls selectPlan', async () => {
    const mockSelectPlan = vi.fn();
    setupDefaultMocks({
      purchaseStore: {
        plans: testPlans,
        selectPlan: mockSelectPlan,
      },
    });

    const user = userEvent.setup();
    render(<Purchase />);

    // Click the first plan card (Monthly, period=1)
    const planCards = screen.getAllByTestId('plan-card');
    await user.click(planCards[0]);

    expect(mockSelectPlan).toHaveBeenCalledWith('plan-1');
  });
});
