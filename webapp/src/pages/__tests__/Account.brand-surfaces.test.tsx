/**
 * Kaitu-only Account surfaces must not leak into the other brand's build.
 *
 * Found by a real-browser check of the overleap build against production:
 * "Delegate Payer" (→ /delegate) and "My Wallet" (→ openExternal(walletUrl),
 * a page that does not exist on the overleap site) were gated on the platform
 * only. They are now brand-gated through getCurrentAppConfig().features
 * (`delegate` / `wallet`, both false for overleap). "Authorisation History" is
 * brand-neutral and stays.
 *
 * Each brand runs the case that applies to it (vitest bakes __K2_BRAND__ like
 * builds do). Mock set mirrors Account.test.tsx; expectations go through i18n
 * (test locale is zh-CN).
 */
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { screen } from '@testing-library/react';
import { render } from '../../test/utils/render';

vi.mock('../../stores', async () => {
  const actual = await vi.importActual('../../stores');
  return { ...actual, useAuth: vi.fn() };
});
vi.mock('../../hooks/useUser', () => ({ useUser: vi.fn() }));
vi.mock('../../contexts/ThemeContext', () => ({
  useTheme: vi.fn(() => ({ themeMode: 'dark', setThemeMode: vi.fn() })),
}));
vi.mock('../../hooks/useAppLinks', () => ({
  useAppLinks: vi.fn(() => ({ links: { walletUrl: 'https://example.com/wallet' } })),
}));
vi.mock('../../services/cloud-api', () => ({
  cloudApi: { request: vi.fn(), post: vi.fn() },
}));
vi.mock('../../stores/login-dialog.store', async () => {
  const actual = await vi.importActual('../../stores/login-dialog.store');
  return { ...actual, useLoginDialogStore: { getState: vi.fn(() => ({ open: vi.fn() })) } };
});
vi.mock('../../components/VersionItem', () => ({
  default: vi.fn(() => <div data-testid="version-item" />),
}));
vi.mock('../../components/BetaChannelToggle', () => ({ default: vi.fn(() => null) }));

import { useAuth } from '../../stores';
import { useUser } from '../../hooks/useUser';
import Account from '../Account';
import { brandConfig } from '../../brands';
import i18n from '../../i18n/i18n';

const tx = (key: string) => i18n.t(key);
const DELEGATE = 'account:account.delegate';
const WALLET = 'account:account.wallet';
const HISTORY = 'account:account.paymentHistory';

function mountLoggedInOnDesktop() {
  (window as any)._k2 = { run: vi.fn().mockResolvedValue({ code: 0 }) };
  (window as any)._platform = { os: 'macos', version: '0.4.10', openExternal: vi.fn() };
  vi.mocked(useAuth).mockReturnValue({ isAuthenticated: true, setIsAuthenticated: vi.fn() } as any);
  vi.mocked(useUser).mockReturnValue({
    user: {
      id: 1,
      expiredAt: '2027-01-01T00:00:00Z',
      loginIdentifies: [{ type: 'email', value: 'test@example.com' }],
    },
    loading: false,
    isMembership: true,
    isExpired: false,
    fetchUser: vi.fn(),
  } as any);
  return render(<Account />);
}

describe('Account — kaitu-only surfaces are brand-gated', () => {
  beforeEach(() => {
    vi.clearAllMocks();
    const mockStyles: Record<string, string> = {
      visibility: 'visible', display: 'block', opacity: '1',
      paddingRight: '0px', overflowY: 'auto', overflow: 'visible',
    };
    (window.getComputedStyle as any).mockImplementation(() =>
      new Proxy(mockStyles, {
        get(target, prop) {
          if (prop === 'getPropertyValue') return (name: string) => target[name] || '';
          if (typeof prop === 'string') return target[prop] || '';
          return undefined;
        },
      })
    );
  });

  afterEach(() => {
    delete (window as any)._k2;
    delete (window as any)._platform;
    localStorage.clear();
  });

  it('translations are loaded — the absence assertions below are meaningful', () => {
    for (const k of [DELEGATE, WALLET, HISTORY]) expect(tx(k)).not.toBe(k.split(':')[1]);
  });

  it.skipIf(brandConfig.id !== 'overleap')(
    'overleap: no Delegate Payer, no My Wallet; Authorisation History stays',
    () => {
      mountLoggedInOnDesktop();
      expect(screen.queryByText(tx(DELEGATE))).toBeNull();
      expect(screen.queryByText(tx(WALLET))).toBeNull();
      expect(screen.getByText(tx(HISTORY))).toBeTruthy();
      expect((window as any)._platform.openExternal).not.toHaveBeenCalled();
    },
  );

  it.skipIf(brandConfig.id !== 'kaitu')(
    'kaitu: Delegate Payer and My Wallet are still rendered on desktop',
    () => {
      mountLoggedInOnDesktop();
      expect(screen.getByText(tx(DELEGATE))).toBeTruthy();
      expect(screen.getByText(tx(WALLET))).toBeTruthy();
      expect(screen.getByText(tx(HISTORY))).toBeTruthy();
    },
  );
});
