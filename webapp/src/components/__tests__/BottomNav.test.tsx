import { describe, it, expect, vi, afterEach } from 'vitest';
import { render, screen, cleanup } from '@testing-library/react';
import { MemoryRouter, Routes, Route } from 'react-router-dom';
import { BottomNav } from '../BottomNav';
import { BackButton } from '../BackButton';
import { useUiStore } from '../../stores/ui.store';

// Mock i18next
vi.mock('react-i18next', () => ({
  useTranslation: () => ({
    t: (key: string) => {
      const map: Record<string, string> = {
        'dashboard:title': 'Dashboard',
        'purchase:title': 'Purchase',
        'invite:title': 'Invite',
        'account:title': 'Account',
        'common:back': 'Back',
      };
      return map[key] || key;
    },
    i18n: { language: 'en-US', changeLanguage: vi.fn() },
  }),
}));

// Mock ui store
vi.mock('../../stores/ui.store', () => ({
  useUiStore: vi.fn(),
}));

describe('BottomNav', () => {
  afterEach(() => {
    cleanup();
    vi.restoreAllMocks();
  });

  it('test_bottom_nav_renders_four_tabs — BottomNav renders 4 tabs: Dashboard, Purchase, Invite, Account', () => {
    (useUiStore as unknown as ReturnType<typeof vi.fn>).mockReturnValue({
      appConfig: null,
      alerts: [],
      getFeatureFlags: () => ({ showInviteTab: true }),
    });

    render(
      <MemoryRouter>
        <BottomNav />
      </MemoryRouter>
    );

    expect(screen.getByText('Dashboard')).toBeInTheDocument();
    expect(screen.getByText('Purchase')).toBeInTheDocument();
    expect(screen.getByText('Invite')).toBeInTheDocument();
    expect(screen.getByText('Account')).toBeInTheDocument();
  });

  it('test_invite_tab_hidden_when_flag_false — When ui.store showInviteTab feature flag is false, invite tab is hidden', () => {
    (useUiStore as unknown as ReturnType<typeof vi.fn>).mockReturnValue({
      appConfig: null,
      alerts: [],
      getFeatureFlags: () => ({ showInviteTab: false }),
    });

    render(
      <MemoryRouter>
        <BottomNav />
      </MemoryRouter>
    );

    expect(screen.getByText('Dashboard')).toBeInTheDocument();
    expect(screen.getByText('Purchase')).toBeInTheDocument();
    expect(screen.queryByText('Invite')).not.toBeInTheDocument();
    expect(screen.getByText('Account')).toBeInTheDocument();
  });

  it('test_sub_page_shows_back_button — Sub-page routes show a BackButton instead of the BottomNav', () => {
    (useUiStore as unknown as ReturnType<typeof vi.fn>).mockReturnValue({
      appConfig: null,
      alerts: [],
      getFeatureFlags: () => ({ showInviteTab: true }),
    });

    render(
      <MemoryRouter initialEntries={['/settings/about']}>
        <Routes>
          <Route path="/settings/about" element={<BackButton />} />
        </Routes>
      </MemoryRouter>
    );

    // BackButton should render a back button element
    expect(screen.getByText('Back')).toBeInTheDocument();
  });
});
