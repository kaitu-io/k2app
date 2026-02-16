import { describe, it, expect, vi, afterEach } from 'vitest';
import { render, screen, cleanup } from '@testing-library/react';
import { MemoryRouter } from 'react-router-dom';
import { LoginRequiredGuard } from '../LoginRequiredGuard';
import { MembershipGuard } from '../MembershipGuard';
import { useAuthStore } from '../../stores/auth.store';
import { useLoginDialogStore } from '../../stores/login-dialog.store';
import { useUserStore } from '../../stores/user.store';

// Mock i18next
vi.mock('react-i18next', () => ({
  useTranslation: () => ({
    t: (key: string) => key,
    i18n: { language: 'en-US', changeLanguage: vi.fn() },
  }),
}));

// Mock react-router-dom (partial)
const mockNavigate = vi.fn();
vi.mock('react-router-dom', async () => {
  const actual = await vi.importActual('react-router-dom');
  return { ...actual, useNavigate: () => mockNavigate };
});

// Mock stores
vi.mock('../../stores/auth.store', () => ({
  useAuthStore: vi.fn(),
}));

vi.mock('../../stores/login-dialog.store', () => ({
  useLoginDialogStore: vi.fn(),
}));

vi.mock('../../stores/user.store', () => ({
  useUserStore: vi.fn(),
}));

describe('Guards', () => {
  afterEach(() => {
    cleanup();
    vi.restoreAllMocks();
    mockNavigate.mockReset();
  });

  it('test_login_required_guard_opens_dialog — LoginRequiredGuard opens LoginDialog when user is not logged in', () => {
    const mockOpen = vi.fn();
    (useAuthStore as unknown as ReturnType<typeof vi.fn>).mockReturnValue({
      isLoggedIn: false,
    });
    (useLoginDialogStore as unknown as ReturnType<typeof vi.fn>).mockReturnValue({
      isOpen: false,
      open: mockOpen,
      close: vi.fn(),
    });
    (useUserStore as unknown as ReturnType<typeof vi.fn>).mockReturnValue({
      user: null,
      getMembershipStatus: () => null,
    });

    render(
      <MemoryRouter>
        <LoginRequiredGuard>
          <div data-testid="protected-content">Protected</div>
        </LoginRequiredGuard>
      </MemoryRouter>
    );

    // Guard should open the login dialog when user is not logged in
    expect(mockOpen).toHaveBeenCalled();
    // Protected content should NOT be rendered
    expect(screen.queryByTestId('protected-content')).not.toBeInTheDocument();
  });

  it('test_membership_guard_redirect — MembershipGuard redirects to /purchase when user has no active membership', () => {
    (useAuthStore as unknown as ReturnType<typeof vi.fn>).mockReturnValue({
      isLoggedIn: true,
    });
    (useUserStore as unknown as ReturnType<typeof vi.fn>).mockReturnValue({
      user: { id: '1', email: 'test@example.com', membership: null },
      getMembershipStatus: () => null,
    });
    (useLoginDialogStore as unknown as ReturnType<typeof vi.fn>).mockReturnValue({
      isOpen: false,
      open: vi.fn(),
      close: vi.fn(),
    });

    render(
      <MemoryRouter>
        <MembershipGuard>
          <div data-testid="member-content">Member Only</div>
        </MembershipGuard>
      </MemoryRouter>
    );

    // Guard should redirect to /purchase
    expect(mockNavigate).toHaveBeenCalledWith('/purchase', expect.anything());
    // Protected content should NOT be rendered
    expect(screen.queryByTestId('member-content')).not.toBeInTheDocument();
  });
});
