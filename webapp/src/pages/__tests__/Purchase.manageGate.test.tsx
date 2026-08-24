// 互斥拦截(spec 2026-08-22):WordGate 轨有活跃订阅(manage)→ 渲染管理面板+提醒,
// 不渲染套餐购买流;status(一次性会员)不拦。
import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen } from '@testing-library/react';
import { I18nextProvider } from 'react-i18next';
import { MemoryRouter } from 'react-router-dom';
import i18n from '../../i18n/i18n';
import type { Plan } from '../../services/api-types';

// ---- Mocks ------------------------------------------------------------
// Skeleton mirrors Purchase.privateNode.test.tsx — see that file for why each
// mock exists (MUI Modal/Select crash under jsdom, cache-store subscribe must
// return an unsubscribe fn, etc.).

vi.mock('@mui/material', async () => {
  const actual = await vi.importActual<typeof import('@mui/material')>('@mui/material');
  return {
    ...actual,
    Dialog: ({ open, children }: any) => (open ? <div role="dialog">{children}</div> : null),
    DialogTitle: ({ children }: any) => <div>{children}</div>,
    DialogContent: ({ children }: any) => <div>{children}</div>,
    DialogActions: ({ children }: any) => <div>{children}</div>,
    Select: ({ value, onChange, children, label }: any) => (
      <select
        aria-label={typeof label === 'string' ? label : 'region'}
        value={value}
        onChange={(e) => onChange?.({ target: { value: e.target.value } })}
      >
        {children}
      </select>
    ),
    MenuItem: ({ value, children }: any) => <option value={value}>{children}</option>,
    FormControl: ({ children }: any) => <div>{children}</div>,
    InputLabel: ({ children }: any) => <label>{children}</label>,
  };
});

const showAlert = vi.fn();
const openPortal = vi.fn();
const fetchUser = vi.fn();

vi.mock('../../services/cloud-api', () => ({
  cloudApi: {
    get: vi.fn(),
    post: vi.fn(),
  },
}));

vi.mock('../../services/cache-store', () => ({
  cacheStore: {
    get: vi.fn().mockReturnValue(null),
    set: vi.fn(),
    subscribe: vi.fn(() => vi.fn()),
  },
}));

vi.mock('../../hooks/useUser', () => ({
  useUser: () => ({
    user: { tier: 'basic', isFirstOrderDone: false, maxDevice: 5, maxRouterDevice: 1, maxLanClient: -1 },
    isExpired: false,
    isMembership: true,
    fetchUser,
  }),
}));

// Affordance is mutable per-test via mockAffordance() below.
let affordanceMock: any = { mode: 'subscribe' };
vi.mock('../../hooks/useSubscriptionAffordance', () => ({
  useSubscriptionAffordance: () => affordanceMock,
}));

vi.mock('../../hooks/useStripeCheckout', () => ({
  useStripeCheckout: () => ({
    checkout: vi.fn(),
    openPortal,
    loading: false,
    error: null,
    clearError: vi.fn(),
  }),
}));

vi.mock('../../stores', () => ({
  useAlert: () => ({ showAlert }),
  useAuthStore: (selector: (s: any) => any) =>
    selector({ isAuthenticated: true }),
}));

vi.mock('../../stores/login-dialog.store', () => ({
  useLoginDialogStore: (selector: (s: any) => any) =>
    selector({ open: vi.fn() }),
}));

import Purchase from '../Purchase';
import { cloudApi } from '../../services/cloud-api';
import { brandConfig } from '../../brands';

const WORDGATE = brandConfig.features.wordgatePurchase;

const SHARED_PLAN: Plan = {
  pid: 'shared-1m',
  tier: 'basic',
  label: '共享订阅 1 个月',
  price: 1900,
  originPrice: 1900,
  month: 1,
  highlight: true,
  maxDevice: 5,
  maxRouterDevice: 0,
  maxLanClient: 0,
  product: 'app',
};

function mockEndpoints({ app = [] }: { app?: Plan[] }) {
  (cloudApi.get as any).mockImplementation((path: string) => {
    if (path === '/api/plans') {
      return Promise.resolve({ code: 0, data: { items: app } });
    }
    if (path === '/api/products/private_node/plans') {
      return Promise.resolve({ code: 0, data: { items: [] } });
    }
    if (path === '/api/app/config') {
      return Promise.resolve({ code: 0, data: { features: {} } });
    }
    return Promise.resolve({ code: 0, data: {} });
  });
}

function mockAffordance(value: any) {
  affordanceMock = value;
}

function renderPurchase(initialEntries: string[] = ['/']) {
  return render(
    <MemoryRouter initialEntries={initialEntries}>
      <I18nextProvider i18n={i18n}>
        <Purchase />
      </I18nextProvider>
    </MemoryRouter>,
  );
}

describe.runIf(WORDGATE)('Purchase WordGate manage gate', () => {
  beforeEach(() => {
    vi.clearAllMocks();
    showAlert.mockReset();
    fetchUser.mockReset();
    openPortal.mockReset();
    affordanceMock = { mode: 'subscribe' };
    mockEndpoints({ app: [SHARED_PLAN] });
    (cloudApi.post as any).mockResolvedValue({
      code: 0,
      data: { order: { uuid: 'order-1', payAmount: 1900 }, payUrl: 'https://pay.example/x' },
    });
  });

  it('renders SubscriptionManagePanel with blocked hint when affordance is manage', async () => {
    mockAffordance({
      mode: 'manage',
      activeSub: {
        provider: 'apple',
        currentPeriodEnd: 1790000000,
        autoRenew: true,
        tier: 'basic',
        manage: { kind: 'apple_settings' },
      },
    });
    renderPurchase();

    expect(screen.getByTestId('stripe-manage-panel')).toBeInTheDocument();
    expect(screen.getByText(/不可重复购买|cannot purchase again/i)).toBeInTheDocument();
  });

  it('keeps the WordGate plan list when affordance is status (one-off member)', async () => {
    mockAffordance({ mode: 'status' });
    renderPurchase();

    // WordGate plan flow renders normally — shared plan card shows up.
    await screen.findAllByText('$19.00');
    expect(screen.queryByTestId('stripe-manage-panel')).not.toBeInTheDocument();
  });
});
