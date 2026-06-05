import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { render, screen, fireEvent } from '@testing-library/react';
import IosMembershipPanel from '../IosMembershipPanel';

vi.mock('react-i18next', () => ({
  useTranslation: () => ({ t: (k: string, opts?: { date?: string }) => (opts?.date ? `${k}:${opts.date}` : k) }),
  initReactI18next: { type: '3rdParty', init: () => {} },
}));

const restore = vi.fn();
let restoring = false;
let purchaseError: string | null = null;
const fetchUser = vi.fn();
const showAlert = vi.fn();

vi.mock('../../hooks/useIapPurchase', () => ({
  useIapPurchase: () => ({ restore, restoring, purchaseError, lastGrantedUser: null }),
}));
vi.mock('../../hooks/useUser', () => ({
  useUser: () => ({ fetchUser }),
}));
vi.mock('../../stores/alert.store', () => ({
  useAlert: () => ({ showAlert }),
}));

describe('IosMembershipPanel', () => {
  let original: unknown;
  beforeEach(() => {
    original = (window as { _platform?: unknown })._platform;
    restore.mockClear();
    restoring = false;
    purchaseError = null;
  });
  afterEach(() => { (window as { _platform?: unknown })._platform = original; });

  it('manage mode opens the Apple settings deep link', () => {
    const openExternal = vi.fn();
    (window as { _platform?: unknown })._platform = { openExternal };
    render(<IosMembershipPanel mode="manage" expiredAt={0} manageSurface={{ kind: 'apple_settings' }} />);
    fireEvent.click(screen.getByTestId('ios-membership-manage-btn'));
    expect(openExternal).toHaveBeenCalledWith('itms-apps://apps.apple.com/account/subscriptions');
  });

  it('manage mode with url surface opens that url', () => {
    const openExternal = vi.fn();
    (window as { _platform?: unknown })._platform = { openExternal };
    render(<IosMembershipPanel mode="manage" expiredAt={0} manageSurface={{ kind: 'url', url: 'https://billing.example/portal' }} />);
    fireEvent.click(screen.getByTestId('ios-membership-manage-btn'));
    expect(openExternal).toHaveBeenCalledWith('https://billing.example/portal');
  });

  it('status mode shows the panel and no manage button', () => {
    render(<IosMembershipPanel mode="status" expiredAt={1_700_000_000} />);
    expect(screen.getByTestId('ios-membership-status')).toBeTruthy();
    expect(screen.queryByTestId('ios-membership-manage-btn')).toBeNull();
  });

  it('exposes Restore Purchases in BOTH modes (Apple requirement) and invokes restore on click', () => {
    const { rerender } = render(<IosMembershipPanel mode="status" expiredAt={1_700_000_000} />);
    const statusRestore = screen.getByTestId('ios-membership-restore-btn');
    expect(statusRestore).toBeTruthy();
    fireEvent.click(statusRestore);
    expect(restore).toHaveBeenCalledTimes(1);

    rerender(<IosMembershipPanel mode="manage" expiredAt={0} manageSurface={{ kind: 'apple_settings' }} />);
    expect(screen.getByTestId('ios-membership-restore-btn')).toBeTruthy();
  });

  it('surfaces a restore error (e.g. nothing to restore) when present', () => {
    purchaseError = 'purchase:purchase.iap.nothingToRestore';
    render(<IosMembershipPanel mode="manage" expiredAt={0} manageSurface={{ kind: 'apple_settings' }} />);
    expect(screen.getByTestId('ios-membership-restore-error').textContent).toBe(
      'purchase:purchase.iap.nothingToRestore',
    );
  });
});
