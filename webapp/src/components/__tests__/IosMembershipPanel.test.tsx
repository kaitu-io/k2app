import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { render, screen, fireEvent } from '@testing-library/react';
import IosMembershipPanel from '../IosMembershipPanel';

vi.mock('react-i18next', () => ({
  useTranslation: () => ({ t: (k: string, opts?: { date?: string }) => (opts?.date ? `${k}:${opts.date}` : k) }),
  initReactI18next: { type: '3rdParty', init: () => {} },
}));

describe('IosMembershipPanel', () => {
  let original: unknown;
  beforeEach(() => { original = (window as { _platform?: unknown })._platform; });
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
});
