import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { render, screen, fireEvent, waitFor } from '@testing-library/react';
import BetaChannelToggle from '../BetaChannelToggle';

vi.mock('react-i18next', () => ({
  useTranslation: () => ({
    t: (key: string, fallback?: string) => fallback || key,
  }),
}));

// Mock MUI Dialog to avoid ModalManager jsdom incompatibility
// (ownerWindow().getComputedStyle returns undefined in jsdom)
vi.mock('@mui/material', async () => {
  const actual = await vi.importActual<typeof import('@mui/material')>('@mui/material');
  return {
    ...actual,
    Dialog: ({ open, children }: any) =>
      open ? <div role="dialog">{children}</div> : null,
    DialogTitle: ({ children }: any) => <div>{children}</div>,
    DialogContent: ({ children }: any) => <div>{children}</div>,
    DialogContentText: ({ children }: any) => <div>{children}</div>,
    DialogActions: ({ children }: any) => <div>{children}</div>,
  };
});

const mockCloudApiRequest = vi.fn().mockResolvedValue({ code: 0 });
vi.mock('../../services/cloud-api', () => ({
  cloudApi: {
    request: (...args: any[]) => mockCloudApiRequest(...args),
  },
}));

const mockUseUser = vi.fn().mockReturnValue({ user: null, loading: false });
vi.mock('../../hooks/useUser', () => ({
  useUser: () => mockUseUser(),
}));

describe('BetaChannelToggle', () => {
  let originalPlatform: any;

  beforeEach(() => {
    originalPlatform = window._platform;
    mockCloudApiRequest.mockResolvedValue({ code: 0 });
  });

  afterEach(() => {
    (window as any)._platform = originalPlatform;
    vi.clearAllMocks();
  });

  it('renders nothing when user is not logged in', () => {
    mockUseUser.mockReturnValue({ user: null, loading: false });
    (window as any)._platform = {
      updater: { channel: 'stable', isUpdateReady: false, updateInfo: null, isChecking: false, error: null, applyUpdateNow: vi.fn(), setChannel: vi.fn() },
    };

    const { container } = render(<BetaChannelToggle />);
    expect(container.innerHTML).toBe('');
  });

  it('renders toggle on desktop with setChannel', () => {
    mockUseUser.mockReturnValue({ user: { uuid: '1', betaOptedIn: false }, loading: false });
    (window as any)._platform = {
      os: 'macos',
      updater: {
        channel: 'stable',
        isUpdateReady: false,
        updateInfo: null,
        isChecking: false,
        error: null,
        applyUpdateNow: vi.fn(),
        setChannel: vi.fn(),
      },
    };

    render(<BetaChannelToggle />);
    expect(screen.getByRole('checkbox')).toBeDefined();
  });

  it('renders toggle on iOS without setChannel', () => {
    mockUseUser.mockReturnValue({ user: { uuid: '1', betaOptedIn: false }, loading: false });
    (window as any)._platform = {
      os: 'ios',
      updater: {
        channel: 'stable',
        isUpdateReady: false,
        updateInfo: null,
        isChecking: false,
        error: null,
        applyUpdateNow: vi.fn(),
      },
    };

    render(<BetaChannelToggle />);
    expect(screen.getByRole('checkbox')).toBeDefined();
  });

  it('renders toggle on Android with setChannel', () => {
    mockUseUser.mockReturnValue({ user: { uuid: '1', betaOptedIn: false }, loading: false });
    (window as any)._platform = {
      os: 'android',
      updater: {
        channel: 'stable',
        isUpdateReady: false,
        updateInfo: null,
        isChecking: false,
        error: null,
        applyUpdateNow: vi.fn(),
        setChannel: vi.fn(),
      },
    };

    render(<BetaChannelToggle />);
    expect(screen.getByRole('checkbox')).toBeDefined();
  });

  it('shows iOS-specific description on iOS', () => {
    mockUseUser.mockReturnValue({ user: { uuid: '1', betaOptedIn: false }, loading: false });
    (window as any)._platform = {
      os: 'ios',
      updater: {
        channel: 'stable',
        isUpdateReady: false,
        updateInfo: null,
        isChecking: false,
        error: null,
        applyUpdateNow: vi.fn(),
      },
    };

    render(<BetaChannelToggle />);
    expect(screen.getByText('betaProgram.descriptionIos')).toBeDefined();
  });

  it('shows standard description on non-iOS', () => {
    mockUseUser.mockReturnValue({ user: { uuid: '1', betaOptedIn: false }, loading: false });
    (window as any)._platform = {
      os: 'macos',
      updater: {
        channel: 'stable',
        isUpdateReady: false,
        updateInfo: null,
        isChecking: false,
        error: null,
        applyUpdateNow: vi.fn(),
        setChannel: vi.fn(),
      },
    };

    render(<BetaChannelToggle />);
    expect(screen.getByText('betaProgram.description')).toBeDefined();
  });

  it('calls setChannel AND API on desktop enable', async () => {
    const mockSetChannel = vi.fn().mockResolvedValue('beta');
    mockUseUser.mockReturnValue({ user: { uuid: '1', betaOptedIn: false }, loading: false });
    (window as any)._platform = {
      os: 'macos',
      updater: {
        channel: 'stable',
        isUpdateReady: false,
        updateInfo: null,
        isChecking: false,
        error: null,
        applyUpdateNow: vi.fn(),
        setChannel: mockSetChannel,
      },
    };

    render(<BetaChannelToggle />);
    fireEvent.click(screen.getByRole('checkbox'));

    const buttons = screen.getAllByRole('button');
    const confirmButton = buttons.find(b => b.textContent === 'betaProgram.enableConfirm');
    fireEvent.click(confirmButton!);

    await waitFor(() => {
      expect(mockSetChannel).toHaveBeenCalledWith('beta');
    });

    await waitFor(() => {
      expect(mockCloudApiRequest).toHaveBeenCalledWith('PUT', '/api/user/beta-channel', { opted_in: true });
    });
  });

  it('calls only API (no setChannel) on iOS enable', async () => {
    mockUseUser.mockReturnValue({ user: { uuid: '1', betaOptedIn: false }, loading: false });
    (window as any)._platform = {
      os: 'ios',
      updater: {
        channel: 'stable',
        isUpdateReady: false,
        updateInfo: null,
        isChecking: false,
        error: null,
        applyUpdateNow: vi.fn(),
      },
    };

    render(<BetaChannelToggle />);
    fireEvent.click(screen.getByRole('checkbox'));

    const buttons = screen.getAllByRole('button');
    const confirmButton = buttons.find(b => b.textContent === 'betaProgram.enableConfirm');
    fireEvent.click(confirmButton!);

    await waitFor(() => {
      expect(mockCloudApiRequest).toHaveBeenCalledWith('PUT', '/api/user/beta-channel', { opted_in: true });
    });

    // No setChannel on iOS
    expect((window as any)._platform.updater.setChannel).toBeUndefined();
  });

  it('API failure does not block local channel switch', async () => {
    const mockSetChannel = vi.fn().mockResolvedValue('beta');
    mockCloudApiRequest.mockRejectedValue(new Error('network error'));
    mockUseUser.mockReturnValue({ user: { uuid: '1', betaOptedIn: false }, loading: false });
    (window as any)._platform = {
      os: 'macos',
      updater: {
        channel: 'stable',
        isUpdateReady: false,
        updateInfo: null,
        isChecking: false,
        error: null,
        applyUpdateNow: vi.fn(),
        setChannel: mockSetChannel,
      },
    };

    render(<BetaChannelToggle />);
    fireEvent.click(screen.getByRole('checkbox'));

    const buttons = screen.getAllByRole('button');
    const confirmButton = buttons.find(b => b.textContent === 'betaProgram.enableConfirm');
    fireEvent.click(confirmButton!);

    await waitFor(() => {
      expect(mockSetChannel).toHaveBeenCalledWith('beta');
    });

    // Switch still checked despite API failure
    const checkbox = screen.getByRole('checkbox') as HTMLInputElement;
    expect(checkbox.checked).toBe(true);
  });

  it('uses user.betaOptedIn for initial state on iOS', () => {
    mockUseUser.mockReturnValue({ user: { uuid: '1', betaOptedIn: true }, loading: false });
    (window as any)._platform = {
      os: 'ios',
      updater: {
        channel: 'stable',
        isUpdateReady: false,
        updateInfo: null,
        isChecking: false,
        error: null,
        applyUpdateNow: vi.fn(),
      },
    };

    render(<BetaChannelToggle />);
    const checkbox = screen.getByRole('checkbox') as HTMLInputElement;
    expect(checkbox.checked).toBe(true);
  });
});
