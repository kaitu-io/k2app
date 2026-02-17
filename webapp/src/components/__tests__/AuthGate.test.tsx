/**
 * AuthGate Component Tests
 *
 * Tests authentication state routing behavior
 */
import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen } from '@testing-library/react';
import { AuthGate } from '../AuthGate';

// Mock stores
const mockAuthState = {
  isAuthChecking: false,
};

vi.mock('../../stores', () => ({
  useAuthStore: (selector: (s: typeof mockAuthState) => unknown) => selector(mockAuthState),
}));

// Mock LoadingPage
vi.mock('../LoadingPage', () => ({
  default: () => <div data-testid="loading-page">Loading...</div>,
}));

describe('AuthGate', () => {
  beforeEach(() => {
    vi.clearAllMocks();
    mockAuthState.isAuthChecking = false;
  });

  describe('Loading State', () => {
    it('should show LoadingPage when auth is checking', () => {
      mockAuthState.isAuthChecking = true;

      render(
        <AuthGate>
          <div data-testid="child-content">Child Content</div>
        </AuthGate>
      );

      expect(screen.getByTestId('loading-page')).toBeInTheDocument();
      expect(screen.queryByTestId('child-content')).not.toBeInTheDocument();
    });
  });

  describe('Content Rendering', () => {
    it('should render children when auth check is complete', () => {
      mockAuthState.isAuthChecking = false;

      render(
        <AuthGate>
          <div data-testid="child-content">Child Content</div>
        </AuthGate>
      );

      expect(screen.getByTestId('child-content')).toBeInTheDocument();
      expect(screen.queryByTestId('loading-page')).not.toBeInTheDocument();
    });

    it('should render multiple children', () => {
      mockAuthState.isAuthChecking = false;

      render(
        <AuthGate>
          <div data-testid="child-1">First</div>
          <div data-testid="child-2">Second</div>
        </AuthGate>
      );

      expect(screen.getByTestId('child-1')).toBeInTheDocument();
      expect(screen.getByTestId('child-2')).toBeInTheDocument();
    });

    it('should render nested components', () => {
      mockAuthState.isAuthChecking = false;

      render(
        <AuthGate>
          <div data-testid="parent">
            <div data-testid="nested">Nested Content</div>
          </div>
        </AuthGate>
      );

      expect(screen.getByTestId('parent')).toBeInTheDocument();
      expect(screen.getByTestId('nested')).toBeInTheDocument();
    });
  });

  describe('State Transitions', () => {
    it('should transition from loading to content', () => {
      mockAuthState.isAuthChecking = true;

      const { rerender } = render(
        <AuthGate>
          <div data-testid="child-content">Child Content</div>
        </AuthGate>
      );

      // Initially loading
      expect(screen.getByTestId('loading-page')).toBeInTheDocument();

      // Simulate auth check complete
      mockAuthState.isAuthChecking = false;
      rerender(
        <AuthGate>
          <div data-testid="child-content">Child Content</div>
        </AuthGate>
      );

      // Now shows content
      expect(screen.getByTestId('child-content')).toBeInTheDocument();
      expect(screen.queryByTestId('loading-page')).not.toBeInTheDocument();
    });
  });

  describe('Open Access Mode', () => {
    it('should not require authentication to show content', () => {
      // AuthGate is now open access - it doesn't check isAuthenticated
      mockAuthState.isAuthChecking = false;

      render(
        <AuthGate>
          <div data-testid="protected-content">Protected Content</div>
        </AuthGate>
      );

      // Content should be visible regardless of auth state
      expect(screen.getByTestId('protected-content')).toBeInTheDocument();
    });
  });
});
