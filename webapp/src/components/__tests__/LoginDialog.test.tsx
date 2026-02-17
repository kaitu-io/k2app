import { describe, it, expect, vi, afterEach } from 'vitest';
import { render, screen, cleanup, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { LoginDialog } from '../LoginDialog';
import { useLoginDialogStore } from '../../stores/login-dialog.store';
import { useAuthStore } from '../../stores/auth.store';

// Mock i18next
vi.mock('react-i18next', () => ({
  useTranslation: () => ({
    t: (key: string) => {
      const map: Record<string, string> = {
        'title': 'Login',
        'email': 'Email',
        'emailPlaceholder': 'Enter your email',
        'getCode': 'Send Code',
        'code': 'Verification Code',
        'codePlaceholder': 'Enter code',
        'login': 'Login',
      };
      return map[key] || key;
    },
    i18n: { language: 'en-US', changeLanguage: vi.fn() },
  }),
}));

// Mock stores
vi.mock('../../stores/login-dialog.store', () => ({
  useLoginDialogStore: vi.fn(),
}));

vi.mock('../../stores/auth.store', () => ({
  useAuthStore: vi.fn(),
}));

describe('LoginDialog', () => {
  afterEach(() => {
    cleanup();
    vi.restoreAllMocks();
  });

  it('test_login_dialog_opens_on_no_session — When login-dialog.store isOpen=true, the LoginDialog renders with email form', () => {
    const mockDialogStore = {
      isOpen: true,
      trigger: 'auto',
      message: null,
      open: vi.fn(),
      close: vi.fn(),
    };
    (useLoginDialogStore as unknown as ReturnType<typeof vi.fn>).mockReturnValue(mockDialogStore);

    const mockAuthStore = {
      isLoggedIn: false,
      isLoading: false,
      getAuthCode: vi.fn(),
      login: vi.fn(),
    };
    (useAuthStore as unknown as ReturnType<typeof vi.fn>).mockReturnValue(mockAuthStore);

    render(<LoginDialog />);

    // Dialog should be visible with an email input
    expect(screen.getByText('Login')).toBeInTheDocument();
    expect(screen.getByPlaceholderText('Enter your email')).toBeInTheDocument();
  });

  it('test_login_dialog_email_then_code — After submitting email, code input appears', async () => {
    const mockGetAuthCode = vi.fn().mockResolvedValue(undefined);
    const mockDialogStore = {
      isOpen: true,
      trigger: 'auto',
      message: null,
      open: vi.fn(),
      close: vi.fn(),
    };
    (useLoginDialogStore as unknown as ReturnType<typeof vi.fn>).mockReturnValue(mockDialogStore);

    const mockAuthStore = {
      isLoggedIn: false,
      isLoading: false,
      getAuthCode: mockGetAuthCode,
      login: vi.fn(),
    };
    (useAuthStore as unknown as ReturnType<typeof vi.fn>).mockReturnValue(mockAuthStore);

    render(<LoginDialog />);

    // Fill in email and submit
    const emailInput = screen.getByPlaceholderText('Enter your email');
    await userEvent.type(emailInput, 'test@example.com');
    await userEvent.click(screen.getByText('Send Code'));

    // After email submission, code input should appear
    await waitFor(() => {
      expect(screen.getByPlaceholderText('Enter code')).toBeInTheDocument();
    });
  });

  it('test_login_success_closes_dialog — After successful login, dialog closes (login-dialog.store close() called)', async () => {
    const mockClose = vi.fn();
    const mockLogin = vi.fn().mockResolvedValue(undefined);
    const mockDialogStore = {
      isOpen: true,
      trigger: 'auto',
      message: null,
      open: vi.fn(),
      close: mockClose,
    };
    (useLoginDialogStore as unknown as ReturnType<typeof vi.fn>).mockReturnValue(mockDialogStore);

    const mockAuthStore = {
      isLoggedIn: false,
      isLoading: false,
      getAuthCode: vi.fn().mockResolvedValue(undefined),
      login: mockLogin,
    };
    (useAuthStore as unknown as ReturnType<typeof vi.fn>).mockReturnValue(mockAuthStore);

    render(<LoginDialog />);

    // Simulate completing the login flow
    const emailInput = screen.getByPlaceholderText('Enter your email');
    await userEvent.type(emailInput, 'test@example.com');
    await userEvent.click(screen.getByText('Send Code'));

    await waitFor(() => {
      expect(screen.getByPlaceholderText('Enter code')).toBeInTheDocument();
    });

    const codeInput = screen.getByPlaceholderText('Enter code');
    await userEvent.type(codeInput, '123456');
    await userEvent.click(screen.getByText('Login'));

    await waitFor(() => {
      expect(mockClose).toHaveBeenCalled();
    });
  });
});
