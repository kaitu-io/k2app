import { describe, it, expect, vi, afterEach } from 'vitest';
import { render, screen, cleanup } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { MemoryRouter } from 'react-router-dom';
import { ForceUpgradeDialog } from '../ForceUpgradeDialog';
import { AnnouncementBanner } from '../AnnouncementBanner';
import { ServiceAlert } from '../ServiceAlert';
import { ErrorBoundary } from '../ErrorBoundary';
import { AlertContainer } from '../AlertContainer';
import { FeedbackButton } from '../FeedbackButton';
import { useUiStore } from '../../stores/ui.store';
import { useVpnStore } from '../../stores/vpn.store';

// Mock i18next
vi.mock('react-i18next', () => ({
  useTranslation: () => ({
    t: (key: string) => {
      const map: Record<string, string> = {
        'common:force_upgrade_title': 'Update Required',
        'common:force_upgrade_message': 'Please update to continue',
        'common:announcement': 'Announcement',
        'common:service_unavailable': 'Service Unavailable',
        'common:retry': 'Retry',
        'common:error_occurred': 'An error occurred',
        'common:feedback': 'Feedback',
      };
      return map[key] || key;
    },
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
vi.mock('../../stores/ui.store', () => ({
  useUiStore: vi.fn(),
}));

vi.mock('../../stores/vpn.store', () => ({
  useVpnStore: vi.fn(),
}));

describe('Global Components', () => {
  afterEach(() => {
    cleanup();
    vi.restoreAllMocks();
    mockNavigate.mockReset();
  });

  it('test_force_upgrade_blocks_app — ForceUpgradeDialog shows blocking overlay when app version < minClientVersion from config', () => {
    (useUiStore as unknown as ReturnType<typeof vi.fn>).mockReturnValue({
      appConfig: {
        version: '2.0.0',
        downloadUrl: 'https://example.com/download',
        features: {},
        minClientVersion: '1.5.0',
      },
      alerts: [],
    });

    render(<ForceUpgradeDialog currentVersion="1.0.0" />);

    // Should show a blocking upgrade dialog
    expect(screen.getByText('Update Required')).toBeInTheDocument();
    expect(screen.getByText('Please update to continue')).toBeInTheDocument();
  });

  it('test_announcement_banner_display — AnnouncementBanner renders when app config has announcement text', () => {
    (useUiStore as unknown as ReturnType<typeof vi.fn>).mockReturnValue({
      appConfig: {
        version: '1.0.0',
        downloadUrl: '',
        features: {},
        announcement: 'Scheduled maintenance on Saturday',
      },
      alerts: [],
    });

    render(<AnnouncementBanner />);

    expect(screen.getByText('Scheduled maintenance on Saturday')).toBeInTheDocument();
  });

  it('test_service_alert_on_daemon_failure — ServiceAlert shows when VPN daemon is unreachable', () => {
    (useVpnStore as unknown as ReturnType<typeof vi.fn>).mockReturnValue({
      daemonReachable: false,
      error: 'Connection refused',
    });

    render(<ServiceAlert />);

    expect(screen.getByText('Service Unavailable')).toBeInTheDocument();
  });

  it('test_error_boundary_catches_retry — ErrorBoundary catches child errors and shows retry button', () => {
    // Suppress console.error for expected error boundary logs
    const consoleSpy = vi.spyOn(console, 'error').mockImplementation(() => {});

    function ThrowingChild(): never {
      throw new Error('Test error');
    }

    render(
      <ErrorBoundary>
        <ThrowingChild />
      </ErrorBoundary>
    );

    // ErrorBoundary should catch the error and show a retry button
    expect(screen.getByText('An error occurred')).toBeInTheDocument();
    expect(screen.getByText('Retry')).toBeInTheDocument();

    consoleSpy.mockRestore();
  });

  it('test_alert_container_toast — AlertContainer renders alerts from ui.store', () => {
    (useUiStore as unknown as ReturnType<typeof vi.fn>).mockReturnValue({
      alerts: [
        { id: '1', type: 'info', message: 'Connected successfully' },
        { id: '2', type: 'error', message: 'Connection failed' },
      ],
      removeAlert: vi.fn(),
    });

    render(<AlertContainer />);

    expect(screen.getByText('Connected successfully')).toBeInTheDocument();
    expect(screen.getByText('Connection failed')).toBeInTheDocument();
  });

  it('test_feedback_button_navigate — FeedbackButton click navigates to /issues', async () => {
    render(
      <MemoryRouter>
        <FeedbackButton />
      </MemoryRouter>
    );

    const button = screen.getByText('Feedback');
    await userEvent.click(button);

    expect(mockNavigate).toHaveBeenCalledWith('/issues');
  });
});
