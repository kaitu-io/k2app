import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen, fireEvent, waitFor } from '@testing-library/react';
import { I18nextProvider } from 'react-i18next';
import { MemoryRouter } from 'react-router-dom';
import i18n from '../../i18n/i18n';
import type { Plan } from '../../services/api-types';

// ---- Mocks ------------------------------------------------------------

// MUI Dialog / Select use Modal + Portal, which crash under jsdom
// (ModalManager reads getComputedStyle(...).paddingRight → undefined).
// Replace them with light DOM stand-ins. The native <select> stand-in
// preserves value/onChange so region-picker interaction is testable.
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
  },
}));

vi.mock('../../hooks/useUser', () => ({
  useUser: () => ({
    user: { tier: 'basic', isFirstOrderDone: false },
    isExpired: false,
    isMembership: false,
    fetchUser: vi.fn(),
  }),
}));

// No iOS IAP — web/desktop payment path.
vi.mock('../../hooks/useSubscriptionAffordance', () => ({
  useSubscriptionAffordance: () => ({ mode: 'subscribe', activeSub: null }),
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

const PRIVATE_NODE_PLAN: Plan = {
  pid: 'pn-1m',
  tier: 'basic',
  label: '专属节点 1 个月',
  price: 9900,
  originPrice: 9900,
  month: 1,
  highlight: false,
  maxDevice: 5,
  maxRouterDevice: 1,
  maxLanClient: -1,
  kind: 'private_node',
  privateNode: {
    provider: 'aws_lightsail',
    ipType: 'non_residential',
    allowedRegions: ['us-east-1', 'ap-northeast-1'],
    trafficTotalBytes: 2 * 1024 * 1024 * 1024 * 1024,
  },
};

function mockPlansResponse(plans: Plan[]) {
  (cloudApi.get as any).mockImplementation((path: string) => {
    if (path === '/api/plans') {
      return Promise.resolve({ code: 0, data: { items: plans } });
    }
    if (path === '/api/app/config') {
      return Promise.resolve({ code: 0, data: { features: {} } });
    }
    return Promise.resolve({ code: 0, data: {} });
  });
}

function renderPurchase() {
  return render(
    <MemoryRouter>
      <I18nextProvider i18n={i18n}>
        <Purchase />
      </I18nextProvider>
    </MemoryRouter>,
  );
}

describe('Purchase — private node region selection', () => {
  beforeEach(() => {
    vi.clearAllMocks();
    showAlert.mockReset();
    mockPlansResponse([PRIVATE_NODE_PLAN]);
    (cloudApi.post as any).mockResolvedValue({
      code: 0,
      data: { order: { uuid: 'order-1', payAmount: 9900 }, payUrl: 'https://pay.example/x' },
    });
  });

  it('renders a region Select defaulting to the first allowed region for a private_node plan', async () => {
    renderPurchase();

    // Region selector should appear (2 allowed regions → a <Select>),
    // defaulting to the first allowed region.
    const select = (await screen.findByRole('combobox')) as HTMLSelectElement;
    expect(select).toBeInTheDocument();
    expect(select.value).toBe('us-east-1');
    // Both regions are offered as options.
    expect(screen.getByRole('option', { name: 'us-east-1' })).toBeInTheDocument();
    expect(screen.getByRole('option', { name: 'ap-northeast-1' })).toBeInTheDocument();
  });

  it('includes the selected region in the order POST body when confirming purchase', async () => {
    renderPurchase();

    // Wait for plans to load and the pay button to become enabled.
    await screen.findByRole('combobox');

    const payButton = (await screen.findByText(i18n.t('purchase:purchase.payNow'))).closest('button')!;
    await waitFor(() => expect(payButton).not.toBeDisabled());

    fireEvent.click(payButton);

    await waitFor(() => {
      const realPost = (cloudApi.post as any).mock.calls.find(
        (c: any[]) => c[0] === '/api/user/orders' && c[1]?.preview === false,
      );
      expect(realPost).toBeTruthy();
      expect(realPost[1].plan).toBe('pn-1m');
      expect(realPost[1].region).toBe('us-east-1');
    });
  });

  it('lets the user pick a different region and sends it in the order body', async () => {
    renderPurchase();
    await screen.findByRole('combobox');

    // Pick the second region via the (stand-in) region select.
    const select = (await screen.findByRole('combobox')) as HTMLSelectElement;
    fireEvent.change(select, { target: { value: 'ap-northeast-1' } });

    const payButton = (await screen.findByText(i18n.t('purchase:purchase.payNow'))).closest('button')!;
    await waitFor(() => expect(payButton).not.toBeDisabled());
    fireEvent.click(payButton);

    await waitFor(() => {
      const realPost = (cloudApi.post as any).mock.calls.find(
        (c: any[]) => c[0] === '/api/user/orders' && c[1]?.preview === false,
      );
      expect(realPost).toBeTruthy();
      expect(realPost[1].region).toBe('ap-northeast-1');
    });
  });
});
