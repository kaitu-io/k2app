// @vitest-environment jsdom
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { render, screen, cleanup, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { MemoryRouter } from 'react-router-dom';
import { UpdateLoginEmail } from '../UpdateLoginEmail';

// Mock i18n
vi.mock('react-i18next', () => ({
  useTranslation: () => ({
    t: (key: string) => {
      const map: Record<string, string> = {
        'updateEmail.title': 'Update Email',
        'updateEmail.step1': 'Step 1',
        'updateEmail.step2': 'Step 2',
        'updateEmail.newEmail': 'New Email',
        'updateEmail.emailPlaceholder': 'Enter new email',
        'updateEmail.sendCode': 'Send Code',
        'updateEmail.code': 'Verification Code',
        'updateEmail.codePlaceholder': 'Enter code',
        'updateEmail.submit': 'Update',
        'updateEmail.success': 'Email updated successfully',
      };
      return map[key] || key;
    },
    i18n: { language: 'en-US', changeLanguage: vi.fn() },
  }),
}));

// Mock react-router-dom
const mockNavigate = vi.fn();
vi.mock('react-router-dom', async () => {
  const actual = await vi.importActual('react-router-dom');
  return { ...actual, useNavigate: () => mockNavigate };
});

// Mock cloudApi
const mockSendEmailCode = vi.fn();
const mockUpdateEmail = vi.fn();
vi.mock('../../api/cloud', () => ({
  cloudApi: {
    sendEmailCode: (...args: unknown[]) => mockSendEmailCode(...args),
    updateEmail: (...args: unknown[]) => mockUpdateEmail(...args),
  },
}));

// Mock MembershipGuard to just render children
vi.mock('../../components/MembershipGuard', () => ({
  MembershipGuard: ({ children }: { children: React.ReactNode }) => <>{children}</>,
}));

function renderPage() {
  return render(
    <MemoryRouter>
      <UpdateLoginEmail />
    </MemoryRouter>
  );
}

describe('UpdateLoginEmail', () => {
  beforeEach(() => {
    vi.clearAllMocks();
  });

  afterEach(() => {
    cleanup();
  });

  it('test_update_email_flow — shows step 1 with email input, then step 2 with code input after sending code, then submits', async () => {
    mockSendEmailCode.mockResolvedValue({ code: 0, message: 'ok' });
    mockUpdateEmail.mockResolvedValue({ code: 0, message: 'ok' });

    const user = userEvent.setup();
    renderPage();

    // Step 1: should show title and email input
    expect(screen.getByText('Update Email')).toBeInTheDocument();
    expect(screen.getByText('Step 1')).toBeInTheDocument();

    const emailInput = screen.getByPlaceholderText('Enter new email');
    expect(emailInput).toBeInTheDocument();

    // Fill in email and send code
    await user.type(emailInput, 'new@example.com');
    await user.click(screen.getByText('Send Code'));

    expect(mockSendEmailCode).toHaveBeenCalledWith('new@example.com');

    // Step 2: should now show code input
    await waitFor(() => {
      expect(screen.getByText('Step 2')).toBeInTheDocument();
    });

    const codeInput = screen.getByPlaceholderText('Enter code');
    expect(codeInput).toBeInTheDocument();

    // Fill in code and submit
    await user.type(codeInput, '123456');
    await user.click(screen.getByText('Update'));

    expect(mockUpdateEmail).toHaveBeenCalledWith('new@example.com', '123456');
  });
});
