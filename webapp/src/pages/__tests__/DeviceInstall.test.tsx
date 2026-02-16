// @vitest-environment jsdom
import { describe, it, expect, vi, afterEach } from 'vitest';
import { render, screen, cleanup } from '@testing-library/react';
import { DeviceInstall } from '../DeviceInstall';

// Mock i18n
vi.mock('react-i18next', () => ({
  useTranslation: () => ({
    t: (key: string) => {
      const map: Record<string, string> = {
        'deviceInstall.title': 'Install on Your Devices',
        'deviceInstall.windows': 'Windows',
        'deviceInstall.macos': 'macOS',
        'deviceInstall.android': 'Android',
        'deviceInstall.ios': 'iOS',
        'deviceInstall.download': 'Download',
        'deviceInstall.scanQr': 'Scan QR code to install mobile app',
      };
      return map[key] || key;
    },
    i18n: { language: 'en-US', changeLanguage: vi.fn() },
  }),
}));

describe('DeviceInstall', () => {
  afterEach(() => {
    cleanup();
  });

  it('test_device_install_qr_buttons — renders platform cards with download buttons and QR code section', () => {
    render(<DeviceInstall />);

    // Title
    expect(screen.getByText('Install on Your Devices')).toBeInTheDocument();

    // Platform cards
    expect(screen.getByText('Windows')).toBeInTheDocument();
    expect(screen.getByText('macOS')).toBeInTheDocument();
    expect(screen.getByText('Android')).toBeInTheDocument();
    expect(screen.getByText('iOS')).toBeInTheDocument();

    // Download buttons
    const downloadButtons = screen.getAllByText('Download');
    expect(downloadButtons.length).toBeGreaterThanOrEqual(4);

    // QR code section
    expect(screen.getByText('Scan QR code to install mobile app')).toBeInTheDocument();

    // QR container should exist with white background (data-testid)
    const qrContainer = screen.getByTestId('qr-container');
    expect(qrContainer).toBeInTheDocument();
  });
});
