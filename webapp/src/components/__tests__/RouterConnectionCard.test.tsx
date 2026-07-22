import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen } from '../../test/utils/render';
import { RouterConnectionCard } from '../RouterConnectionCard';

// k2r's /api/core status returns raw engine.Status — the error key is
// `error`, not the local-daemon `lastError` convention (see
// services/status-transform.ts). This test guards against regressing back
// to reading `status.lastError`, which silently swallows the 402
// purchase-guidance path (spec §7).
let mockStatus: any = null;
vi.mock('../../stores/router.store', () => ({
  useRouterStore: (sel: any) =>
    sel({
      router: { name: 'r1', version: '0.4.7', configured: true },
      status: mockStatus,
      connectRouter: vi.fn(),
      disconnectRouter: vi.fn(),
    }),
}));

describe('RouterConnectionCard', () => {
  beforeEach(() => {
    vi.clearAllMocks();
    mockStatus = null;
  });

  it('renders no error alert when status has no error', () => {
    mockStatus = { state: 'disconnected' };
    render(<RouterConnectionCard />);
    expect(screen.queryByTestId('router-conn-error')).not.toBeInTheDocument();
  });

  it('renders the 402 purchase-guidance message from status.error (not status.lastError)', () => {
    mockStatus = { state: 'disconnected', error: { code: 402, message: 'membership expired' } };
    render(<RouterConnectionCard />);
    const alert = screen.getByTestId('router-conn-error');
    // Test i18n defaults to zh-CN (fallbackLng) — see errorCode.ts
    // ERROR_CODES.PAYMENT_REQUIRED → common:errors.client.paymentRequired.
    expect(alert).toHaveTextContent('会员已过期，请续费');
  });

  it('does not render an error when only the stale lastError key is present', () => {
    // Guards the specific regression: a response shaped with the old,
    // wrong key must not accidentally render (would mask the real bug).
    mockStatus = { state: 'disconnected', lastError: { code: 402, message: 'membership expired' } };
    render(<RouterConnectionCard />);
    expect(screen.queryByTestId('router-conn-error')).not.toBeInTheDocument();
  });
});
