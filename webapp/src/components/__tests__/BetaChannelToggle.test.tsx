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

  it('always renders toggle regardless of auth', () => {
    (window as any)._platform = {
      updater: { channel: 'stable', isUpdateReady: false, updateInfo: null, isChecking: false, error: null, applyUpdateNow: vi.fn(), setChannel: vi.fn() },
    };

    render(<BetaChannelToggle />);
    expect(screen.getByRole('checkbox')).toBeDefined();
  });

  it('renders toggle on desktop with setChannel', () => {
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

  it('defaults to false on iOS (no local channel)', () => {
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
    expect(checkbox.checked).toBe(false);
  });

  it('does NOT call API on disable (one-way opt-in)', async () => {
    const mockSetChannel = vi.fn().mockResolvedValue('stable');
    (window as any)._platform = {
      os: 'macos',
      updater: {
        channel: 'beta',
        isUpdateReady: false,
        updateInfo: null,
        isChecking: false,
        error: null,
        applyUpdateNow: vi.fn(),
        setChannel: mockSetChannel,
      },
    };

    render(<BetaChannelToggle />);
    // Toggle starts checked (channel=beta), click to disable
    const checkbox = screen.getByRole('checkbox') as HTMLInputElement;
    expect(checkbox.checked).toBe(true);

    fireEvent.click(checkbox);

    const buttons = screen.getAllByRole('button');
    const confirmButton = buttons.find(b => b.textContent === 'betaProgram.disableConfirm');
    fireEvent.click(confirmButton!);

    await waitFor(() => {
      expect(mockSetChannel).toHaveBeenCalledWith('stable');
    });

    // API should NOT be called when disabling
    expect(mockCloudApiRequest).not.toHaveBeenCalled();
  });
});
