// @vitest-environment jsdom
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { render, screen, cleanup } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { MemoryRouter } from 'react-router-dom';

// Mock i18n — return key as value, with interpolation support
vi.mock('react-i18next', () => ({
  useTranslation: () => ({
    t: (key: string, opts?: Record<string, string>) => {
      const map: Record<string, string> = {
        title: 'Account',
        membership: 'Membership',
        'membershipStatus.active': 'Active',
        'membershipStatus.expired': 'Expired',
        'membershipStatus.none': 'None',
        expireAt: `Expires: ${opts?.date ?? '{{date}}'}`,
        plan: `Plan: ${opts?.plan ?? '{{plan}}'}`,
        menuPassword: 'Change Password',
        menuDevices: 'Device Management',
        menuSupport: 'Help & Feedback',
        language: 'Language',
        languageZh: 'Chinese',
        languageEn: 'English',
        version: 'Version',
        logout: 'Logout',
        devModeActivated: 'Dev mode activated',
      };
      return map[key] || key;
    },
    i18n: {
      language: 'en-US',
      changeLanguage: vi.fn().mockResolvedValue(undefined),
    },
  }),
}));

// Mock stores
const mockLogout = vi.fn();
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
const mockSyncLocale = vi.fn().mockResolvedValue(undefined);
const mockOpenExternal = vi.fn().mockResolvedValue(undefined);
vi.mock('../../platform', () => ({
  getPlatform: vi.fn(() => ({
    syncLocale: mockSyncLocale,
    openExternal: mockOpenExternal,
    isMobile: false,
    platformName: 'test',
  })),
}));

// Mock navigate
const mockNavigate = vi.fn();
vi.mock('react-router-dom', async () => {
  const actual = await vi.importActual('react-router-dom');
  return { ...actual, useNavigate: () => mockNavigate };
});

// Mock PasswordDialog
vi.mock('../../components/PasswordDialog', () => ({
  PasswordDialog: ({ open, onClose }: { open: boolean; onClose: () => void }) =>
    open ? <div data-testid="password-dialog">Password Dialog<button onClick={onClose}>Close</button></div> : null,
}));

// Import mocked stores so we can configure return values
import { useAuthStore } from '../../stores/auth.store';
import { useUserStore } from '../../stores/user.store';
import { useUiStore } from '../../stores/ui.store';

// Import component under test
import { Account } from '../Account';

const mockedUseAuthStore = vi.mocked(useAuthStore);
const mockedUseUserStore = vi.mocked(useUserStore);
const mockedUseUiStore = vi.mocked(useUiStore);

function setupMocks(overrides?: {
  user?: any;
  membership?: any;
  appConfig?: any;
}) {
  mockedUseAuthStore.mockReturnValue({
    isLoggedIn: true,
    logout: mockLogout,
  } as any);

  const user = overrides?.user ?? {
    id: 'u1',
    email: 'test@example.com',
    nickname: 'Tester',
    membership: overrides?.membership ?? {
      plan: 'Pro',
      status: 'active',
      expireAt: '2026-12-31',
    },
  };

  mockedUseUserStore.mockReturnValue({
    user,
    isLoading: false,
    error: null,
    getMembershipStatus: () => user?.membership?.status ?? null,
  } as any);

  mockedUseUiStore.mockReturnValue({
    appConfig: overrides?.appConfig ?? { version: '0.4.0', downloadUrl: '', features: {} },
    alerts: [],
    isLoading: false,
  } as any);
}

function renderAccount() {
  return render(
    <MemoryRouter>
      <Account />
    </MemoryRouter>,
  );
}

describe('Account', () => {
  beforeEach(() => {
    vi.clearAllMocks();
    setupMocks();
  });

  afterEach(() => {
    cleanup();
  });

  // 1. test_account_membership_card
  it('test_account_membership_card — Shows membership status card with expiry date', () => {
    renderAccount();
    // Should display the membership status
    expect(screen.getByText('Active')).toBeInTheDocument();
    // Should display the plan name
    expect(screen.getByText(/Plan: Pro/)).toBeInTheDocument();
    // Should display expiry date
    expect(screen.getByText(/Expires: 2026-12-31/)).toBeInTheDocument();
  });

  // 2. test_account_logout_flow
  it('test_account_logout_flow — Logout button calls auth store logout', async () => {
    const user = userEvent.setup();
    renderAccount();

    const logoutButton = screen.getByText('Logout');
    await user.click(logoutButton);
    expect(mockLogout).toHaveBeenCalledOnce();
  });

  // 3. test_language_selector_sync
  it('test_language_selector_sync — Language selector changes language and syncs to platform', async () => {
    const user = userEvent.setup();
    renderAccount();

    // Find the language selector area — it should show current language
    const languageItem = screen.getByText('Language');
    expect(languageItem).toBeInTheDocument();

    // Click on the Chinese option to switch language
    const zhOption = screen.getByText('Chinese');
    await user.click(zhOption);

    // Should have synced locale to platform
    expect(mockSyncLocale).toHaveBeenCalledWith('zh-CN');
  });

  // 4. test_version_dev_mode_activation
  it('test_version_dev_mode_activation — 5 rapid clicks on version activates dev mode', async () => {
    const user = userEvent.setup();
    renderAccount();

    // Find the version display
    const versionElement = screen.getByText('0.4.0');
    expect(versionElement).toBeInTheDocument();

    // Click 5 times rapidly
    for (let i = 0; i < 5; i++) {
      await user.click(versionElement);
    }

    // Dev mode should be activated — shown via text
    expect(screen.getByText('Dev mode activated')).toBeInTheDocument();
  });

  // 5. test_account_sub_page_links
  it('test_account_sub_page_links — Menu items navigate to correct sub-pages', async () => {
    const user = userEvent.setup();
    renderAccount();

    // Click Device Management
    const devicesItem = screen.getByText('Device Management');
    await user.click(devicesItem);
    expect(mockNavigate).toHaveBeenCalledWith('/devices');

    mockNavigate.mockClear();

    // Click Help & Feedback
    const supportItem = screen.getByText('Help & Feedback');
    await user.click(supportItem);
    expect(mockNavigate).toHaveBeenCalledWith('/support');
  });

  // 6. test_account_brand_banner
  it('test_account_brand_banner — Brand banner is displayed at top', () => {
    renderAccount();

    // Brand banner should show app name
    const banner = screen.getByTestId('brand-banner');
    expect(banner).toBeInTheDocument();
  });

  // 7. test_account_password_dialog
  it('test_account_password_dialog — Password menu item triggers password dialog', async () => {
    const user = userEvent.setup();
    renderAccount();

    // Click Change Password menu item
    const passwordItem = screen.getByText('Change Password');
    await user.click(passwordItem);

    // Password dialog should be visible
    expect(screen.getByTestId('password-dialog')).toBeInTheDocument();
  });
});
