/**
 * MembershipGuard Component Tests
 *
 * Tests membership/subscription guard behavior
 */
import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen } from '@testing-library/react';
import { MemoryRouter, Routes, Route } from 'react-router-dom';
import MembershipGuard from '../MembershipGuard';

// Mock useUser hook
const mockUserState = {
  isExpired: false,
  loading: false,
  user: { uuid: 'test-user' } as { uuid: string } | null,
};

vi.mock('../../hooks/useUser', () => ({
  useUser: () => mockUserState,
}));

describe('MembershipGuard', () => {
  beforeEach(() => {
    vi.clearAllMocks();
    mockUserState.isExpired = false;
    mockUserState.loading = false;
    mockUserState.user = { uuid: 'test-user' };
  });

  const renderGuard = (initialPath: string = '/dashboard') => {
    return render(
      <MemoryRouter initialEntries={[initialPath]}>
        <Routes>
          <Route
            path="/dashboard"
            element={
              <MembershipGuard>
                <div data-testid="protected-content">Dashboard Content</div>
              </MembershipGuard>
            }
          />
          <Route
            path="/account"
            element={
              <MembershipGuard>
                <div data-testid="account-content">Account Content</div>
              </MembershipGuard>
            }
          />
          <Route
            path="/purchase"
            element={
              <MembershipGuard>
                <div data-testid="purchase-content">Purchase Content</div>
              </MembershipGuard>
            }
          />
          <Route path="*" element={<div>404</div>} />
        </Routes>
      </MemoryRouter>
    );
  };

  describe('Loading State', () => {
    it('should render children when loading', () => {
      mockUserState.loading = true;

      renderGuard('/dashboard');

      expect(screen.getByTestId('protected-content')).toBeInTheDocument();
    });

    it('should render children when user is null', () => {
      mockUserState.user = null;

      renderGuard('/dashboard');

      expect(screen.getByTestId('protected-content')).toBeInTheDocument();
    });
  });

  describe('Active Membership', () => {
    it('should render children when membership is active', () => {
      mockUserState.isExpired = false;

      renderGuard('/dashboard');

      expect(screen.getByTestId('protected-content')).toBeInTheDocument();
    });
  });

  describe('Expired Membership', () => {
    it('should redirect to /purchase when expired on protected page', () => {
      mockUserState.isExpired = true;

      renderGuard('/dashboard');

      // Should redirect to purchase page
      expect(screen.queryByTestId('protected-content')).not.toBeInTheDocument();
      expect(screen.getByTestId('purchase-content')).toBeInTheDocument();
    });
  });

  describe('Allowed Paths', () => {
    it('should allow /purchase even when expired', () => {
      mockUserState.isExpired = true;

      renderGuard('/purchase');

      expect(screen.getByTestId('purchase-content')).toBeInTheDocument();
    });

    it('should allow /account even when expired', () => {
      mockUserState.isExpired = true;

      renderGuard('/account');

      expect(screen.getByTestId('account-content')).toBeInTheDocument();
    });
  });

  describe('Edge Cases', () => {
    it('should handle loading then active transition', () => {
      mockUserState.loading = true;

      const { rerender } = render(
        <MemoryRouter initialEntries={['/dashboard']}>
          <Routes>
            <Route
              path="/dashboard"
              element={
                <MembershipGuard>
                  <div data-testid="protected-content">Dashboard Content</div>
                </MembershipGuard>
              }
            />
          </Routes>
        </MemoryRouter>
      );

      // Initially loading - shows content
      expect(screen.getByTestId('protected-content')).toBeInTheDocument();

      // Finish loading with active membership
      mockUserState.loading = false;
      mockUserState.isExpired = false;

      rerender(
        <MemoryRouter initialEntries={['/dashboard']}>
          <Routes>
            <Route
              path="/dashboard"
              element={
                <MembershipGuard>
                  <div data-testid="protected-content">Dashboard Content</div>
                </MembershipGuard>
              }
            />
          </Routes>
        </MemoryRouter>
      );

      // Still shows content
      expect(screen.getByTestId('protected-content')).toBeInTheDocument();
    });
  });
});
