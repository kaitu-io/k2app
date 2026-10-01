/**
 * Funnel: the two Account entry points to /purchase pass their identity as
 * router state, which Purchase turns into the paywall_view source.
 * Mock style follows Account.privateNodeEntry.test.tsx.
 */
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { screen, fireEvent } from '@testing-library/react';
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
vi.mock('../../hooks/usePrivateNodes', () => ({
  usePrivateNodes: () => ({ nodes: [], loading: false, error: null, refresh: vi.fn() }),
}));

const navigateMock = vi.fn();
vi.mock('react-router-dom', async () => {
  const actual = await vi.importActual<typeof import('react-router-dom')>('react-router-dom');
  return { ...actual, useNavigate: () => navigateMock };
});

import { useAuth } from '../../stores';
import { useUser } from '../../hooks/useUser';
import Account from '../Account';
import i18n from '../../i18n/i18n';

function mount(userState: { isMembership: boolean; isExpired: boolean }) {
  (window as any)._k2 = { run: vi.fn().mockResolvedValue({ code: 0 }) };
  (window as any)._platform = { os: 'macos', version: '0.4.10', openExternal: vi.fn() };
  vi.mocked(useAuth).mockReturnValue({ isAuthenticated: true, setIsAuthenticated: vi.fn() } as any);
  vi.mocked(useUser).mockReturnValue({
    user: {
      id: 1,
      expiredAt: '2020-01-01T00:00:00Z',
      loginIdentifies: [{ type: 'email', value: 'test@example.com' }],
    },
    loading: false,
    ...userState,
    fetchUser: vi.fn(),
  } as any);
  return render(<Account />);
}

describe('Account → /purchase carries its funnel source', () => {
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

  it('expired member: the renew button navigates with from=account_expired', () => {
    mount({ isMembership: false, isExpired: true });
    fireEvent.click(screen.getByText(i18n.t('account:account.renewNow')));
    expect(navigateMock).toHaveBeenCalledWith('/purchase', { state: { from: 'account_expired' } });
  });

  it('non-member: the plan button navigates with from=account', () => {
    mount({ isMembership: false, isExpired: false });
    fireEvent.click(screen.getByText(i18n.t('account:account.proPlan')));
    expect(navigateMock).toHaveBeenCalledWith('/purchase', { state: { from: 'account' } });
  });
});
