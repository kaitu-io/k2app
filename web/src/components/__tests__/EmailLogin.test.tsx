import React from 'react';
import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen, fireEvent, waitFor } from '@testing-library/react';
import EmailLogin from '../EmailLogin';

// Mock API: passwordLogin (T16) returns the same shape as webLogin.
vi.mock('@/lib/api', () => {
  class ApiError extends Error {
    code: number;
    constructor(code: number, message: string) {
      super(message);
      this.code = code;
    }
  }
  return {
    api: {
      sendCode: vi.fn().mockResolvedValue({ isActivated: true }),
      webLogin: vi
        .fn()
        .mockResolvedValue({ user: { id: 1, email: 'a@b.com' }, accessToken: 't' }),
      passwordLogin: vi
        .fn()
        .mockResolvedValue({ user: { id: 1, email: 'a@b.com' }, accessToken: 't' }),
    },
    ApiError,
    ErrorCode: {
      VerificationCodeExpired: 400013,
      InvalidArgument: 422,
      InvalidOperation: 400,
    },
  };
});

const mockLogin = vi.fn().mockResolvedValue(undefined);
vi.mock('@/contexts/AuthContext', () => ({
  useAuth: () => ({ login: mockLogin }),
  AuthProvider: ({ children }: { children: React.ReactNode }) => <>{children}</>,
}));

vi.mock('@/contexts/AppConfigContext', () => ({
  useAppConfig: () => ({ appConfig: null }),
  AppConfigProvider: ({ children }: { children: React.ReactNode }) => <>{children}</>,
}));

vi.mock('sonner', () => ({
  toast: { error: vi.fn(), success: vi.fn() },
}));

beforeEach(() => {
  vi.clearAllMocks();
});

describe('EmailLogin password tab', () => {
  it('renders both tabs and starts on the code tab', () => {
    render(<EmailLogin />);
    // next-intl mock returns raw keys, so assert on the i18n key strings.
    expect(screen.getByText('auth.login.codeLogin')).toBeInTheDocument();
    expect(screen.getByText('auth.login.passwordLogin')).toBeInTheDocument();
    // Code tab visible by default — its email label is shown.
    expect(screen.getAllByText('auth.login.email').length).toBeGreaterThan(0);
  });

  it('switches to password tab and submits via api.passwordLogin', async () => {
    const { api } = await import('@/lib/api');
    render(<EmailLogin />);

    // Click password tab trigger.
    const passwordTab = screen.getByText('auth.login.passwordLogin');
    fireEvent.click(passwordTab);

    // After switching, the password input is in the DOM (TabsContent renders null
    // for inactive tabs — the active tab now contains login-email-pw + login-password).
    const emailInput = document.getElementById('login-email-pw') as HTMLInputElement;
    const passwordInput = document.getElementById('login-password') as HTMLInputElement;
    expect(emailInput).toBeTruthy();
    expect(passwordInput).toBeTruthy();

    fireEvent.change(emailInput, { target: { value: 'a@b.com' } });
    fireEvent.change(passwordInput, { target: { value: 'k7N#mq2P!xT9' } });

    // Submit button is the one labelled auth.login.loginButton inside the password panel.
    const submitButtons = screen.getAllByRole('button');
    const submitBtn = submitButtons.find((b) =>
      (b.textContent || '').includes('auth.login.loginButton'),
    );
    expect(submitBtn).toBeTruthy();
    fireEvent.click(submitBtn!);

    await waitFor(() => expect(api.passwordLogin).toHaveBeenCalled());
    expect(api.passwordLogin).toHaveBeenCalledWith(
      expect.objectContaining({ email: 'a@b.com', password: 'k7N#mq2P!xT9' }),
      expect.objectContaining({ autoRedirectToAuth: false }),
    );
    // Code-login API path should NOT be called from the password tab.
    expect(api.webLogin).not.toHaveBeenCalled();
  });

  it('renders the forgot-password hint inside the password tab', () => {
    render(<EmailLogin />);
    fireEvent.click(screen.getByText('auth.login.passwordLogin'));
    expect(screen.getByText('auth.login.forgotPasswordHint')).toBeInTheDocument();
  });

  // T21 + Risk #2 regression guard: ensure password tab hydrates the
  // AuthContext with hasPassword=true AND invokes the parent onLoginSuccess
  // callback. This is the contract that PurchaseStep1 / RedeemClient /
  // GiftCodeClient / survey page rely on for their gating to flip.
  it('hydrates AuthContext with hasPassword=true and fires onLoginSuccess', async () => {
    const onLoginSuccess = vi.fn();
    render(<EmailLogin onLoginSuccess={onLoginSuccess} />);

    fireEvent.click(screen.getByText('auth.login.passwordLogin'));

    const emailInput = document.getElementById('login-email-pw') as HTMLInputElement;
    const passwordInput = document.getElementById('login-password') as HTMLInputElement;
    fireEvent.change(emailInput, { target: { value: 'a@b.com' } });
    fireEvent.change(passwordInput, { target: { value: 'k7N#mq2P!xT9' } });

    const submitBtn = screen
      .getAllByRole('button')
      .find((b) => (b.textContent || '').includes('auth.login.loginButton'));
    fireEvent.click(submitBtn!);

    await waitFor(() => expect(mockLogin).toHaveBeenCalled());
    // First arg to login() must be the user shape with hasPassword=true,
    // so the parent re-render shows "Change password" instead of "Set password"
    // immediately, without waiting for the next /api/user/info round-trip.
    expect(mockLogin).toHaveBeenCalledWith(
      expect.objectContaining({ id: 1, email: 'a@b.com', hasPassword: true }),
      't',
    );
    await waitFor(() => expect(onLoginSuccess).toHaveBeenCalledTimes(1));
  });
});
