/**
 * LoginRequiredGuard Component Tests
 *
 * Tests authentication guard behavior for protected routes
 */
import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen, waitFor } from '@testing-library/react';
import { MemoryRouter, Routes, Route } from 'react-router-dom';
import { LoginRequiredGuard } from '../LoginRequiredGuard';

// Mock react-i18next
vi.mock('react-i18next', () => ({
  useTranslation: () => ({
    t: (key: string, fallback?: string) => fallback || key,
    i18n: { language: 'en' },
  }),
}));

// Mock stores
const mockAuthState = {
  isAuthenticated: false,
  isAuthChecking: false,
};

const mockOpenLoginDialog = vi.fn();
const mockLoginDialogState = {
  open: mockOpenLoginDialog,
};

vi.mock('../../stores', () => ({
  useAuthStore: (selector: (s: typeof mockAuthState) => unknown) => selector(mockAuthState),
}));

vi.mock('../../stores/login-dialog.store', () => ({
  useLoginDialogStore: (selector: (s: typeof mockLoginDialogState) => unknown) =>
    selector(mockLoginDialogState),
}));

describe('LoginRequiredGuard', () => {
  beforeEach(() => {
    vi.clearAllMocks();
    mockAuthState.isAuthenticated = false;
    mockAuthState.isAuthChecking = false;
  });

  const renderGuard = (pagePath: string, currentPath?: string, messageKey?: string) => {
    return render(
      <MemoryRouter initialEntries={[currentPath || pagePath]}>
        <Routes>
          <Route
            path={pagePath}
            element={
              <LoginRequiredGuard pagePath={pagePath} messageKey={messageKey}>
                <div data-testid="protected-content">Protected Content</div>
              </LoginRequiredGuard>
            }
          />
          <Route path="*" element={<div>Other Page</div>} />
        </Routes>
      </MemoryRouter>
    );
  };

  describe('Auth Checking State', () => {
    it('should render nothing when auth is checking', () => {
      mockAuthState.isAuthChecking = true;

      const { container } = renderGuard('/account');

      // When auth is checking, no protected content should be rendered
      expect(container.textContent).toBe('');
      expect(screen.queryByTestId('protected-content')).not.toBeInTheDocument();
    });
  });

  describe('Authenticated User', () => {
    it('should render children when authenticated', () => {
      mockAuthState.isAuthenticated = true;

      renderGuard('/account');

      expect(screen.getByTestId('protected-content')).toBeInTheDocument();
      expect(mockOpenLoginDialog).not.toHaveBeenCalled();
    });
  });

  describe('Unauthenticated User', () => {
    it('should open login dialog when not authenticated', async () => {
      mockAuthState.isAuthenticated = false;

      renderGuard('/account');

      await waitFor(() => {
        expect(mockOpenLoginDialog).toHaveBeenCalledWith({
          trigger: 'guard:/account',
          redirectPath: '/account',
          message: expect.any(String),
        });
      });
    });

    it('should still render children (open access mode)', () => {
      mockAuthState.isAuthenticated = false;

      renderGuard('/account');

      // Children are rendered even when not authenticated
      expect(screen.getByTestId('protected-content')).toBeInTheDocument();
    });
  });

  describe('Page Path Matching', () => {
    it('should only trigger login dialog for active page', async () => {
      mockAuthState.isAuthenticated = false;

      // Guard is for /account but we're on /dashboard
      render(
        <MemoryRouter initialEntries={['/dashboard']}>
          <Routes>
            <Route
              path="/account"
              element={
                <LoginRequiredGuard pagePath="/account">
                  <div>Account</div>
                </LoginRequiredGuard>
              }
            />
            <Route path="/dashboard" element={<div>Dashboard</div>} />
          </Routes>
        </MemoryRouter>
      );

      // Login dialog should NOT be opened because we're not on /account
      await waitFor(() => {
        expect(mockOpenLoginDialog).not.toHaveBeenCalled();
      });
    });
  });

  describe('Message Handling', () => {
    it('should use custom messageKey when provided', async () => {
      mockAuthState.isAuthenticated = false;

      renderGuard('/devices', '/devices', 'custom.message.key');

      await waitFor(() => {
        expect(mockOpenLoginDialog).toHaveBeenCalledWith({
          trigger: 'guard:/devices',
          redirectPath: '/devices',
          message: 'custom.message.key',
        });
      });
    });

    it('should use page-specific message for known pages', async () => {
      mockAuthState.isAuthenticated = false;

      renderGuard('/account');

      await waitFor(() => {
        expect(mockOpenLoginDialog).toHaveBeenCalledWith({
          trigger: 'guard:/account',
          redirectPath: '/account',
          message: 'guard.accountMessage',
        });
      });
    });

    it('should use default message for unknown pages', async () => {
      mockAuthState.isAuthenticated = false;

      renderGuard('/unknown-page');

      await waitFor(() => {
        expect(mockOpenLoginDialog).toHaveBeenCalledWith({
          trigger: 'guard:/unknown-page',
          redirectPath: '/unknown-page',
          message: '请登录以继续',
        });
      });
    });
  });

  describe('Known Page Paths', () => {
    const knownPages = [
      { path: '/account', expectedMessage: 'guard.accountMessage' },
      { path: '/devices', expectedMessage: 'guard.devicesMessage' },
      { path: '/invite', expectedMessage: 'guard.inviteMessage' },
      { path: '/invite-codes', expectedMessage: 'guard.inviteMessage' },
      { path: '/member-management', expectedMessage: 'guard.memberManagementMessage' },
      { path: '/pro-histories', expectedMessage: 'guard.proHistoriesMessage' },
    ];

    knownPages.forEach(({ path, expectedMessage }) => {
      it(`should use correct message for ${path}`, async () => {
        mockAuthState.isAuthenticated = false;

        renderGuard(path);

        await waitFor(() => {
          expect(mockOpenLoginDialog).toHaveBeenCalledWith(
            expect.objectContaining({
              message: expectedMessage,
            })
          );
        });
      });
    });
  });
});
