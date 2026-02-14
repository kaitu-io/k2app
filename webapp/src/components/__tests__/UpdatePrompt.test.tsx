import { describe, it, expect, vi, afterEach, beforeEach } from 'vitest';
import { render, screen, cleanup, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { UpdatePrompt } from '../UpdatePrompt';

// Mock i18next
vi.mock('react-i18next', () => ({
  useTranslation: () => ({
    t: (key: string, opts?: Record<string, string>) => {
      const map: Record<string, string> = {
        updateAvailable: 'Update Available',
        updateVersion: `Version ${opts?.version ?? ''}`,
        updateSize: `Size ${opts?.size ?? ''}`,
        updateNow: 'Update Now',
        updateGoToAppStore: 'Go to App Store',
        updateDownloading: 'Downloading...',
        updateLater: 'Later',
        updateRestartToApply: 'Update downloaded. Restart the app to apply.',
        updateFailed: `Update failed: ${opts?.message ?? ''}`,
      };
      return map[key] || key;
    },
  }),
}));

// Mock vpn-client
const mockCheckForUpdates = vi.fn();
const mockApplyWebUpdate = vi.fn().mockResolvedValue(undefined);
const mockDownloadNativeUpdate = vi.fn().mockResolvedValue({ path: '/tmp/update.apk' });
const mockInstallNativeUpdate = vi.fn().mockResolvedValue(undefined);
const mockOnDownloadProgress = vi.fn().mockReturnValue(() => {});

vi.mock('../../vpn-client', () => ({
  getVpnClient: () => ({
    checkForUpdates: mockCheckForUpdates,
    applyWebUpdate: mockApplyWebUpdate,
    downloadNativeUpdate: mockDownloadNativeUpdate,
    installNativeUpdate: mockInstallNativeUpdate,
    onDownloadProgress: mockOnDownloadProgress,
  }),
}));

// Mock Capacitor platform
let mockPlatform = 'android';
Object.defineProperty(window, 'Capacitor', {
  value: { getPlatform: () => mockPlatform },
  writable: true,
  configurable: true,
});

describe('UpdatePrompt', () => {
  beforeEach(() => {
    mockPlatform = 'android';
    mockCheckForUpdates.mockReset();
    mockApplyWebUpdate.mockReset().mockResolvedValue(undefined);
    mockDownloadNativeUpdate.mockReset().mockResolvedValue({ path: '/tmp/update.apk' });
    mockInstallNativeUpdate.mockReset().mockResolvedValue(undefined);
    mockOnDownloadProgress.mockReset().mockReturnValue(() => {});
  });

  afterEach(() => {
    cleanup();
  });

  it('shows nothing when no update available', async () => {
    mockCheckForUpdates.mockResolvedValue({ type: 'none' });
    const { container } = render(<UpdatePrompt />);
    await waitFor(() => {
      expect(mockCheckForUpdates).toHaveBeenCalled();
    });
    expect(container.firstChild).toBeNull();
  });

  it('shows dialog with version + size when native update ready', async () => {
    mockCheckForUpdates.mockResolvedValue({
      type: 'native',
      version: '0.5.0',
      size: 45000000,
    });
    render(<UpdatePrompt />);
    await waitFor(() => {
      expect(screen.getByText('Update Available')).toBeInTheDocument();
    });
    expect(screen.getByText('Version 0.5.0')).toBeInTheDocument();
    expect(screen.getByText('Size 42.9 MB')).toBeInTheDocument();
  });

  it('shows dialog for web update', async () => {
    mockCheckForUpdates.mockResolvedValue({
      type: 'web',
      version: '0.4.1',
      size: 1500000,
    });
    render(<UpdatePrompt />);
    await waitFor(() => {
      expect(screen.getByText('Update Available')).toBeInTheDocument();
    });
    expect(screen.getByText('Version 0.4.1')).toBeInTheDocument();
    expect(screen.getByText('Update Now')).toBeInTheDocument();
  });

  it('calls downloadNativeUpdate + installNativeUpdate on Android', async () => {
    mockPlatform = 'android';
    mockCheckForUpdates.mockResolvedValue({
      type: 'native',
      version: '0.5.0',
      size: 45000000,
    });
    render(<UpdatePrompt />);
    await waitFor(() => {
      expect(screen.getByText('Update Now')).toBeInTheDocument();
    });
    await userEvent.click(screen.getByText('Update Now'));
    await waitFor(() => {
      expect(mockDownloadNativeUpdate).toHaveBeenCalled();
    });
    expect(mockInstallNativeUpdate).toHaveBeenCalledWith({ path: '/tmp/update.apk' });
  });

  it('calls installNativeUpdate (App Store) on iOS', async () => {
    mockPlatform = 'ios';
    mockCheckForUpdates.mockResolvedValue({
      type: 'native',
      version: '0.5.0',
      url: 'https://apps.apple.com/app/id6759199298',
    });
    render(<UpdatePrompt />);
    await waitFor(() => {
      expect(screen.getByText('Go to App Store')).toBeInTheDocument();
    });
    await userEvent.click(screen.getByText('Go to App Store'));
    await waitFor(() => {
      expect(mockInstallNativeUpdate).toHaveBeenCalledWith({ path: '' });
    });
    expect(mockDownloadNativeUpdate).not.toHaveBeenCalled();
  });

  it('dismiss hides the dialog', async () => {
    mockCheckForUpdates.mockResolvedValue({
      type: 'native',
      version: '0.5.0',
      size: 45000000,
    });
    render(<UpdatePrompt />);
    await waitFor(() => {
      expect(screen.getByText('Later')).toBeInTheDocument();
    });
    await userEvent.click(screen.getByText('Later'));
    expect(screen.queryByText('Update Available')).not.toBeInTheDocument();
  });

  it('shows restart message after web update applied', async () => {
    mockCheckForUpdates.mockResolvedValue({
      type: 'web',
      version: '0.4.1',
      size: 1500000,
    });
    render(<UpdatePrompt />);
    await waitFor(() => {
      expect(screen.getByText('Update Now')).toBeInTheDocument();
    });
    await userEvent.click(screen.getByText('Update Now'));
    await waitFor(() => {
      expect(screen.getByText('Update downloaded. Restart the app to apply.')).toBeInTheDocument();
    });
    // "Update Now" button should be hidden after successful apply
    expect(screen.queryByText('Update Now')).not.toBeInTheDocument();
  });

  it('shows error message when download fails', async () => {
    mockDownloadNativeUpdate.mockRejectedValue(new Error('Network timeout'));
    mockCheckForUpdates.mockResolvedValue({
      type: 'native',
      version: '0.5.0',
      size: 45000000,
    });
    render(<UpdatePrompt />);
    await waitFor(() => {
      expect(screen.getByText('Update Now')).toBeInTheDocument();
    });
    await userEvent.click(screen.getByText('Update Now'));
    await waitFor(() => {
      expect(screen.getByText('Update failed: Network timeout')).toBeInTheDocument();
    });
  });

  it('subscribes to download progress during native update', async () => {
    let progressHandler: ((percent: number) => void) | null = null;
    const unsubFn = vi.fn();
    mockOnDownloadProgress.mockImplementation((handler: (percent: number) => void) => {
      progressHandler = handler;
      return unsubFn;
    });
    // Make download hang so we can inspect progress
    mockDownloadNativeUpdate.mockImplementation(() => new Promise(() => {}));
    mockCheckForUpdates.mockResolvedValue({
      type: 'native',
      version: '0.5.0',
      size: 45000000,
    });
    render(<UpdatePrompt />);
    await waitFor(() => {
      expect(screen.getByText('Update Now')).toBeInTheDocument();
    });
    await userEvent.click(screen.getByText('Update Now'));
    await waitFor(() => {
      expect(mockOnDownloadProgress).toHaveBeenCalled();
    });
    // Simulate progress update
    progressHandler!(42);
    await waitFor(() => {
      expect(screen.getByRole('progressbar')).toHaveAttribute('aria-valuenow', '42');
    });
  });
});
