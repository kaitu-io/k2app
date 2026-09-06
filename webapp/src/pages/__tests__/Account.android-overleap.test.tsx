/**
 * Play-compliance gate on Android (spec 2026-09-06-overleap-app-launch §1.4).
 *
 * A Google-Play-only brand must not show ANY purchase / subscription-management
 * surface on Android: no /purchase CTA, no prices, no checkout or portal link
 * (Play Payments policy). purchaseSurfaceAvailable() is the single gate; this
 * file proves the Account page honours it end to end for the active brand:
 *   - overleap (androidPurchase=false): status-only block, zero CTAs / links.
 *   - kaitu    (androidPurchase=true):  the sideloaded APK keeps its CTA.
 * Each brand runs the case that applies to it (vitest bakes __K2_BRAND__ the
 * same way builds do), so `K2_BRAND=overleap npx vitest run` exercises the
 * closed gate and the default run exercises the open one.
 *
 * Mock set mirrors Account.test.tsx. Test locale is zh-CN (pinned in
 * src/test/setup.ts) — expectations are resolved through i18n, never literal.
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

const mockNavigate = vi.fn();
vi.mock('react-router-dom', async () => {
  const actual = await vi.importActual<typeof import('react-router-dom')>('react-router-dom');
  return { ...actual, useNavigate: () => mockNavigate };
});

import { useAuth } from '../../stores';
import { useUser } from '../../hooks/useUser';
import Account from '../Account';
import { brandConfig } from '../../brands';
import i18n from '../../i18n/i18n';

const tx = (key: string) => i18n.t(key);
/** Every string that leads to a purchase from the Account page. */
const PURCHASE_CTAS = [
  'account:account.renewNow',
  'account:account.proPlan',
  'account:account.upgradeToPro',
];

function mountExpiredUserOnAndroid() {
  (window as any)._k2 = { run: vi.fn().mockResolvedValue({ code: 0 }) };
  (window as any)._platform = { os: 'android', version: '0.4.10', openExternal: vi.fn() };
  vi.mocked(useAuth).mockReturnValue({ isAuthenticated: true, setIsAuthenticated: vi.fn() } as any);
  vi.mocked(useUser).mockReturnValue({
    user: {
      id: 1,
      expiredAt: '2020-01-01T00:00:00Z',
      loginIdentifies: [{ type: 'email', value: 'test@example.com' }],
    },
    loading: false,
    isMembership: false,
    isExpired: true,
    fetchUser: vi.fn(),
  } as any);
  return render(<Account />);
}

describe('Account on Android — purchase surface follows the brand gate', () => {
  beforeEach(() => {
    vi.clearAllMocks();
    // setup.ts mocks getComputedStyle; clearAllMocks wipes the implementation
    // and MUI Modal reads paddingRight from it (same restore as Account.test).
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
    for (const k of PURCHASE_CTAS) expect(tx(k)).not.toBe(k.split(':')[1]);
    expect(tx('account:account.managedElsewhere')).not.toBe('account.managedElsewhere');
  });

  it.skipIf(brandConfig.id !== 'overleap')(
    'overleap: no purchase CTA, no price, no external link; status-only block instead',
    () => {
      mountExpiredUserOnAndroid();
      expect((window as any)._platform.openExternal).not.toHaveBeenCalled();
      expect(mockNavigate).not.toHaveBeenCalled();
      for (const k of PURCHASE_CTAS) expect(screen.queryByText(tx(k))).toBeNull();
      expect(document.body.textContent ?? '').not.toMatch(/[$£€]|stripe|subscribe/i);
      expect(screen.getByText(tx('account:account.managedElsewhere'))).toBeTruthy();
    },
  );

  it.skipIf(brandConfig.id !== 'kaitu')(
    'kaitu: the /purchase CTA is still rendered (sideloaded APK may link to WordGate)',
    () => {
      mountExpiredUserOnAndroid();
      expect(screen.getByText(tx('account:account.proPlan'))).toBeTruthy();
      expect(screen.getByText(tx('account:account.renewNow'))).toBeTruthy();
      expect(screen.queryByText(tx('account:account.managedElsewhere'))).toBeNull();
    },
  );
});
