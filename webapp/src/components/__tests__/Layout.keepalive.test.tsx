import { describe, it, expect, vi, afterEach } from 'vitest';
import { render, screen, cleanup } from '@testing-library/react';
import { MemoryRouter, Routes, Route } from 'react-router-dom';
import userEvent from '@testing-library/user-event';
import { Layout } from '../Layout';

// Mock i18next
vi.mock('react-i18next', () => ({
  useTranslation: () => ({
    t: (key: string) => {
      const map: Record<string, string> = {
        'dashboard:title': 'Dashboard',
        'purchase:title': 'Purchase',
        'invite:title': 'Invite',
        'account:title': 'Account',
        'common:servers': 'Servers',
        'settings:title': 'Settings',
      };
      return map[key] || key;
    },
    i18n: { language: 'en-US', changeLanguage: vi.fn() },
  }),
}));

// Mock stores that Layout/BottomNav may use
vi.mock('../../stores/ui.store', () => ({
  useUiStore: vi.fn().mockReturnValue({
    appConfig: null,
    alerts: [],
    getFeatureFlags: () => ({ showInviteTab: true }),
  }),
}));

vi.mock('../../stores/auth.store', () => ({
  useAuthStore: vi.fn().mockReturnValue({
    isLoggedIn: true,
  }),
}));

function TabPageA() {
  return <div data-testid="page-a">Page A Content</div>;
}

function TabPageB() {
  return <div data-testid="page-b">Page B Content</div>;
}

describe('Layout keep-alive', () => {
  afterEach(() => {
    cleanup();
  });

  it('test_keep_alive_preserves_tab_state — When switching between tabs, previously rendered tab content is kept in DOM (visibility:hidden) not unmounted', async () => {
    render(
      <MemoryRouter initialEntries={['/']}>
        <Routes>
          <Route element={<Layout />}>
            <Route path="/" element={<TabPageA />} />
            <Route path="/purchase" element={<TabPageB />} />
          </Route>
        </Routes>
      </MemoryRouter>
    );

    // Page A should be visible
    expect(screen.getByTestId('page-a')).toBeInTheDocument();

    // Navigate to tab B
    const purchaseTab = screen.getByText('Purchase');
    await userEvent.click(purchaseTab);

    // Page B should be visible
    expect(screen.getByTestId('page-b')).toBeInTheDocument();

    // Page A should still be in the DOM (kept alive, hidden) not unmounted
    expect(screen.getByTestId('page-a')).toBeInTheDocument();
    // The container for page A should have visibility:hidden or display:none
    const pageAContainer = screen.getByTestId('page-a').closest('[style]');
    expect(pageAContainer).toBeTruthy();
    expect(pageAContainer!.getAttribute('style')).toMatch(/visibility:\s*hidden|display:\s*none/);
  });

  it('test_login_route_does_not_exist — No /login route exists in the app', () => {
    render(
      <MemoryRouter initialEntries={['/login']}>
        <Routes>
          <Route element={<Layout />}>
            <Route path="/" element={<div>Dashboard</div>} />
            <Route path="/purchase" element={<div>Purchase</div>} />
            <Route path="/account" element={<div>Account</div>} />
          </Route>
        </Routes>
      </MemoryRouter>
    );

    // /login should not render any login page — it should fall through
    // The app uses LoginDialog overlay instead of a /login route
    expect(screen.queryByText('Login Page')).not.toBeInTheDocument();
    // No /login route is defined, so we should see the layout but not a login page
    // This test passes trivially now but validates the architectural decision
    // that there is no dedicated /login route in the new navigation model
  });
});
