// @vitest-environment jsdom
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { render, screen, cleanup } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { MemoryRouter } from 'react-router-dom';
import { Login } from '../Login';
import { useAuthStore } from '../../stores/auth.store';

// Mock i18n to return keys as values
vi.mock('react-i18next', () => ({
  useTranslation: () => ({
    t: (key: string) => key,
    i18n: { language: 'en-US', changeLanguage: vi.fn() },
  }),
}));

// Mock react-router-dom's useNavigate
const mockNavigate = vi.fn();
vi.mock('react-router-dom', async () => {
  const actual = await vi.importActual('react-router-dom');
  return {
    ...actual,
    useNavigate: () => mockNavigate,
  };
});

// Stub localStorage for Node 25 compatibility
function createLocalStorageStub() {
  const store = new Map<string, string>();
  return {
    getItem: vi.fn((key: string) => store.get(key) ?? null),
    setItem: vi.fn((key: string, value: string) => { store.set(key, value); }),
    removeItem: vi.fn((key: string) => { store.delete(key); }),
    clear: vi.fn(() => { store.clear(); }),
    get length() { return store.size; },
    key: vi.fn((index: number) => [...store.keys()][index] ?? null),
  };
}

function renderLogin() {
  return render(
    <MemoryRouter>
      <Login />
    </MemoryRouter>
  );
}

describe('Login', () => {
  beforeEach(() => {
    vi.stubGlobal('localStorage', createLocalStorageStub());

    // Reset auth store
    useAuthStore.setState({
      token: null,
      refreshToken: null,
      user: null,
      isLoggedIn: false,
      isLoading: false,
    });

    vi.clearAllMocks();
  });

  afterEach(() => {
    cleanup();
  });

  it('renders title and email form initially', () => {
    renderLogin();

    expect(screen.getByText('title')).toBeDefined();
    expect(screen.getByPlaceholderText('emailPlaceholder')).toBeDefined();
    expect(screen.getByText('getCode')).toBeDefined();
  });

  it('shows code form after email submission', async () => {
    // Mock getAuthCode to succeed
    const mockGetAuthCode = vi.fn().mockResolvedValue(undefined);
    useAuthStore.setState({ getAuthCode: mockGetAuthCode } as any);

    const user = userEvent.setup();
    renderLogin();

    // Fill in email
    const emailInput = screen.getByPlaceholderText('emailPlaceholder');
    await user.type(emailInput, 'test@example.com');

    // Submit email form
    const getCodeButton = screen.getByText('getCode');
    await user.click(getCodeButton);

    // Should now show the code input
    expect(screen.getByPlaceholderText('codePlaceholder')).toBeDefined();
    expect(screen.getByText('login')).toBeDefined();

    // The email field should be read-only in the code form
    const emailInputInCodeForm = screen.getByDisplayValue('test@example.com');
    expect(emailInputInCodeForm).toBeDefined();
    expect(emailInputInCodeForm.getAttribute('readOnly')).not.toBeNull();
  });

  it('shows error when getAuthCode fails', async () => {
    const mockGetAuthCode = vi.fn().mockRejectedValue(new Error('Rate limited'));
    useAuthStore.setState({ getAuthCode: mockGetAuthCode } as any);

    const user = userEvent.setup();
    renderLogin();

    const emailInput = screen.getByPlaceholderText('emailPlaceholder');
    await user.type(emailInput, 'test@example.com');
    await user.click(screen.getByText('getCode'));

    // Should show error message
    expect(screen.getByText('Rate limited')).toBeDefined();
  });

  it('navigates to / after successful login', async () => {
    const mockGetAuthCode = vi.fn().mockResolvedValue(undefined);
    const mockLogin = vi.fn().mockResolvedValue(undefined);
    useAuthStore.setState({
      getAuthCode: mockGetAuthCode,
      login: mockLogin,
      isLoading: false,
    } as any);

    const user = userEvent.setup();
    renderLogin();

    // Step 1: Get code
    await user.type(screen.getByPlaceholderText('emailPlaceholder'), 'test@example.com');
    await user.click(screen.getByText('getCode'));

    // Step 2: Enter code and login
    await user.type(screen.getByPlaceholderText('codePlaceholder'), '123456');
    await user.click(screen.getByText('login'));

    expect(mockLogin).toHaveBeenCalledWith('test@example.com', '123456');
    expect(mockNavigate).toHaveBeenCalledWith('/');
  });
});
