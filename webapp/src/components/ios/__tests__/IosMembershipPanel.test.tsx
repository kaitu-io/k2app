import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { render, screen, fireEvent } from '@testing-library/react';
import IosMembershipPanel from '../IosMembershipPanel';
import type { DataSubscription } from '../../../services/api-types';

// t echoes the key, appending interpolated date/days so assertions can see them.
vi.mock('react-i18next', () => ({
  useTranslation: () => ({
    t: (k: string, opts?: { date?: string; days?: number }) =>
      opts?.date != null ? `${k}:${opts.date}` : opts?.days != null ? `${k}:${opts.days}` : k,
  }),
  initReactI18next: { type: '3rdParty', init: () => {} },
}));

const restore = vi.fn();
let restoring = false;
let purchaseError: string | null = null;
const fetchUser = vi.fn();
const showAlert = vi.fn();
const navigate = vi.fn();
let appConfig: unknown = { inviteReward: { purchaseRewardDays: 7 } };
const user = { expiredAt: 1_700_000_000 + 200 * 86400, maxDevice: 5, maxRouterDevice: 0, maxLanClient: 0 };

vi.mock('../../../hooks/useIapPurchase', () => ({
  useIapPurchase: () => ({ restore, restoring, purchaseError, lastGrantedUser: null }),
}));
vi.mock('../../../hooks/useUser', () => ({
  useUser: () => ({ user, fetchUser }),
}));
vi.mock('../../../hooks/useAppConfig', () => ({
  useAppConfig: () => ({ appConfig, loading: false, error: null }),
}));
vi.mock('../../../stores/alert.store', () => ({
  useAlert: () => ({ showAlert }),
}));
vi.mock('react-router-dom', () => ({
  useNavigate: () => navigate,
}));

const sub = (over: Partial<DataSubscription> = {}): DataSubscription => ({
  provider: 'apple',
  tier: 'basic',
  currentPeriodEnd: 1_700_000_000 + 200 * 86400,
  autoRenew: true,
  manage: { kind: 'apple_settings' },
  ...over,
});

describe('IosMembershipPanel', () => {
  let original: unknown;
  beforeEach(() => {
    original = (window as { _platform?: unknown })._platform;
    (window as { _platform?: unknown })._platform = { openExternal: vi.fn() };
    restore.mockClear();
    navigate.mockClear();
    restoring = false;
    purchaseError = null;
    appConfig = { inviteReward: { purchaseRewardDays: 7 } };
  });
  afterEach(() => {
    (window as { _platform?: unknown })._platform = original;
  });

  it('manage mode opens the Apple settings deep link', () => {
    const openExternal = vi.fn();
    (window as { _platform?: unknown })._platform = { openExternal };
    render(<IosMembershipPanel mode="manage" activeSub={sub()} />);
    fireEvent.click(screen.getByTestId('ios-membership-manage-btn'));
    expect(openExternal).toHaveBeenCalledWith('itms-apps://apps.apple.com/account/subscriptions');
  });

  it('manage mode with url surface opens that url', () => {
    const openExternal = vi.fn();
    (window as { _platform?: unknown })._platform = { openExternal };
    render(
      <IosMembershipPanel
        mode="manage"
        activeSub={sub({ manage: { kind: 'url', url: 'https://billing.example/portal' } })}
      />,
    );
    fireEvent.click(screen.getByTestId('ios-membership-manage-btn'));
    expect(openExternal).toHaveBeenCalledWith('https://billing.example/portal');
  });

  it('manage mode with auto-renew OFF surfaces the cancellation warning + re-enable CTA', () => {
    render(<IosMembershipPanel mode="manage" activeSub={sub({ autoRenew: false })} />);
    expect(screen.getByTestId('renewal-status-card')).toBeTruthy();
    // The re-enable CTA reuses the manage button (jumps to App Store to turn renewal back on).
    expect(screen.getByText('purchase:purchase.iap.renewOffCta')).toBeTruthy();
    expect(screen.getByTestId('ios-membership-manage-btn')).toBeTruthy();
  });

  it('status mode shows the panel and no manage button', () => {
    render(<IosMembershipPanel mode="status" />);
    expect(screen.getByTestId('ios-membership-status')).toBeTruthy();
    expect(screen.queryByTestId('ios-membership-manage-btn')).toBeNull();
  });

  it('exposes Restore Purchases in BOTH modes (Apple requirement) and invokes restore', () => {
    const { rerender } = render(<IosMembershipPanel mode="status" />);
    const statusRestore = screen.getByTestId('ios-membership-restore-btn');
    fireEvent.click(statusRestore);
    expect(restore).toHaveBeenCalledTimes(1);

    rerender(<IosMembershipPanel mode="manage" activeSub={sub()} />);
    expect(screen.getByTestId('ios-membership-restore-btn')).toBeTruthy();
  });

  it('surfaces a restore error when present', () => {
    purchaseError = 'purchase:purchase.iap.nothingToRestore';
    render(<IosMembershipPanel mode="manage" activeSub={sub()} />);
    expect(screen.getByTestId('ios-membership-restore-error').textContent).toBe(
      'purchase:purchase.iap.nothingToRestore',
    );
  });

  it('history button navigates to the purchase history page', () => {
    render(<IosMembershipPanel mode="status" />);
    fireEvent.click(screen.getByTestId('ios-membership-history-btn'));
    expect(navigate).toHaveBeenCalledWith('/pro-histories?from=/purchase');
  });

  it('shows the invite-reward card and navigates to /invite', () => {
    render(<IosMembershipPanel mode="status" />);
    fireEvent.click(screen.getByTestId('invite-reward-btn'));
    expect(navigate).toHaveBeenCalledWith('/invite');
  });

  it('hides the invite-reward card when no reward configured', () => {
    appConfig = { inviteReward: { purchaseRewardDays: 0 } };
    render(<IosMembershipPanel mode="status" />);
    expect(screen.queryByTestId('invite-reward-card')).toBeNull();
  });
});
