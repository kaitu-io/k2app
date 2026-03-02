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

describe('BetaChannelToggle', () => {
  let originalPlatform: any;

  beforeEach(() => {
    originalPlatform = window._platform;
  });

  afterEach(() => {
    (window as any)._platform = originalPlatform;
  });

  it('renders nothing when updater.setChannel is not available', () => {
    (window as any)._platform = {
      updater: { channel: 'stable', isUpdateReady: false, updateInfo: null, isChecking: false, error: null, applyUpdateNow: vi.fn() },
    };

    const { container } = render(<BetaChannelToggle />);
    expect(container.innerHTML).toBe('');
  });

  it('renders toggle when setChannel is available', () => {
    (window as any)._platform = {
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

  it('shows switch unchecked when channel is stable', () => {
    (window as any)._platform = {
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
    const checkbox = screen.getByRole('checkbox') as HTMLInputElement;
    expect(checkbox.checked).toBe(false);
  });

  it('shows switch checked when channel is beta', () => {
    (window as any)._platform = {
      updater: {
        channel: 'beta',
        isUpdateReady: false,
        updateInfo: null,
        isChecking: false,
        error: null,
        applyUpdateNow: vi.fn(),
        setChannel: vi.fn(),
      },
    };

    render(<BetaChannelToggle />);
    const checkbox = screen.getByRole('checkbox') as HTMLInputElement;
    expect(checkbox.checked).toBe(true);
  });

  it('opens confirmation dialog on toggle click', () => {
    (window as any)._platform = {
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
    fireEvent.click(screen.getByRole('checkbox'));

    // Dialog should appear
    expect(screen.getByRole('dialog')).toBeDefined();
  });

  it('calls setChannel(beta) on confirm when currently stable', async () => {
    const mockSetChannel = vi.fn().mockResolvedValue('beta');
    (window as any)._platform = {
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

    // Click the confirm button in dialog
    const buttons = screen.getAllByRole('button');
    const confirmButton = buttons.find(b => b.textContent === 'betaProgram.enableConfirm');
    fireEvent.click(confirmButton!);

    await waitFor(() => {
      expect(mockSetChannel).toHaveBeenCalledWith('beta');
    });
  });

  it('calls setChannel(stable) on confirm when currently beta', async () => {
    const mockSetChannel = vi.fn().mockResolvedValue('stable');
    (window as any)._platform = {
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
    fireEvent.click(screen.getByRole('checkbox'));

    const buttons = screen.getAllByRole('button');
    const confirmButton = buttons.find(b => b.textContent === 'betaProgram.disableConfirm');
    fireEvent.click(confirmButton!);

    await waitFor(() => {
      expect(mockSetChannel).toHaveBeenCalledWith('stable');
    });
  });

  it('closes dialog without switching on cancel', async () => {
    const mockSetChannel = vi.fn();
    (window as any)._platform = {
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

    // Click cancel — t('common:common.cancel', '取消') returns fallback '取消'
    const cancelButton = screen.getAllByRole('button').find(b => b.textContent === '取消');
    fireEvent.click(cancelButton!);

    expect(mockSetChannel).not.toHaveBeenCalled();
  });
});
