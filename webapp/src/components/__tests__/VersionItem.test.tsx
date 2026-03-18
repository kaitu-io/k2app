import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { render, screen } from '@testing-library/react';
import { MemoryRouter } from 'react-router-dom';
import VersionItem from '../VersionItem';

vi.mock('react-i18next', () => ({
  useTranslation: () => ({
    t: (key: string, fallback?: string) => fallback || key,
  }),
}));

vi.mock('../../stores', () => ({
  useAlertStore: {
    getState: () => ({ showAlert: vi.fn() }),
  },
}));

describe('VersionItem', () => {
  let originalPlatform: any;

  beforeEach(() => {
    originalPlatform = window._platform;
  });

  afterEach(() => {
    (window as any)._platform = originalPlatform;
  });

  const renderWithRouter = (appVersion: string) =>
    render(
      <MemoryRouter>
        <VersionItem appVersion={appVersion} />
      </MemoryRouter>
    );

  it('shows version number', () => {
    (window as any)._platform = { updater: undefined };
    renderWithRouter('0.4.0');
    expect(screen.getByText('0.4.0')).toBeDefined();
  });

  it('shows Beta badge when version string contains -beta', () => {
    (window as any)._platform = { updater: undefined };
    renderWithRouter('0.5.0-beta.2');
    expect(screen.getByText('betaProgram.badge')).toBeDefined();
  });

  it('does NOT show Beta badge for stable version even when channel is beta', () => {
    (window as any)._platform = {
      updater: {
        channel: 'beta',
        isUpdateReady: false,
        updateInfo: null,
        isChecking: false,
        error: null,
        applyUpdateNow: vi.fn(),
      },
    };
    renderWithRouter('0.4.0');
    expect(screen.queryByText('betaProgram.badge')).toBeNull();
  });

  it('does NOT show Beta badge for stable version and stable channel', () => {
    (window as any)._platform = {
      updater: {
        channel: 'stable',
        isUpdateReady: false,
        updateInfo: null,
        isChecking: false,
        error: null,
        applyUpdateNow: vi.fn(),
      },
    };
    renderWithRouter('0.4.0');
    expect(screen.queryByText('betaProgram.badge')).toBeNull();
  });
});
