// @vitest-environment jsdom
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { render, screen, cleanup, act, fireEvent } from '@testing-library/react';
import userEvent from '@testing-library/user-event';

// Mock i18n — return key as value
vi.mock('react-i18next', () => ({
  useTranslation: () => ({
    t: (key: string) => {
      const map: Record<string, string> = {
        title: 'Settings',
        language: 'Language',
        version: 'Version',
        about: 'About',
        aboutText: 'Kaitu VPN Client',
      };
      return map[key] || key;
    },
    i18n: {
      language: 'en-US',
      changeLanguage: vi.fn().mockResolvedValue(undefined),
    },
  }),
}));

import { Settings } from '../Settings';

describe('Settings', () => {
  let locationObj: { href: string };

  beforeEach(() => {
    vi.clearAllMocks();
    // Use a writable object so we can detect href assignment
    locationObj = { href: 'http://localhost/' };
    vi.spyOn(window, 'location', 'get').mockReturnValue(
      locationObj as unknown as Location,
    );
  });

  afterEach(() => {
    cleanup();
    vi.restoreAllMocks();
    vi.useRealTimers();
  });

  // 1. test_settings_debug_tap_counter
  it('test_settings_debug_tap_counter — 5 rapid clicks on version navigates to /debug.html', async () => {
    const user = userEvent.setup();
    render(<Settings />);

    const versionElement = screen.getByText('0.4.0');
    expect(versionElement).toBeInTheDocument();

    // Click 4 times — should NOT navigate
    for (let i = 0; i < 4; i++) {
      await user.click(versionElement);
    }
    expect(locationObj.href).toBe('http://localhost/');

    // Click 5th time — should navigate to /debug.html
    await user.click(versionElement);
    expect(locationObj.href).toBe('/debug.html');
  });

  // 2. test_settings_tap_counter_resets
  it('test_settings_tap_counter_resets — counter resets after 2s timeout', () => {
    vi.useFakeTimers();
    render(<Settings />);

    const versionElement = screen.getByText('0.4.0');

    // Click 3 times (use fireEvent to avoid userEvent delay issues with fake timers)
    for (let i = 0; i < 3; i++) {
      fireEvent.click(versionElement);
    }

    // Advance timers by 3000ms — counter should reset
    act(() => {
      vi.advanceTimersByTime(3000);
    });

    // Click 3 more times (total would be 6 if counter didn't reset, but only 3 after reset)
    for (let i = 0; i < 3; i++) {
      fireEvent.click(versionElement);
    }

    // Should NOT have navigated since counter was reset (only 3 clicks after reset, not 5)
    expect(locationObj.href).toBe('http://localhost/');
  });
});
