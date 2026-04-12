/**
 * TravelBanner Component Tests
 *
 * Covers:
 *   1. Shows when modeOverride=auto, detectedCountry is set, and differs
 *      from lastAcknowledgedCountry.
 *   2. Hides when already acknowledged.
 *   3. Hides when modeOverride != auto.
 *   4. Clicking Switch acknowledges and persists the country.
 *   5. Clicking Dismiss acknowledges without switching mode.
 *   6. Unsupported country (e.g. Japan) shows the "global" prompt variant.
 *
 * Uses `useConfigStore.setState()` to skip async plumbing and a stubbed
 * `window._platform.storage.set` to observe persistence.
 */

import { describe, it, expect, beforeEach, afterEach, beforeAll, vi } from 'vitest';
import { render, screen, cleanup, fireEvent, waitFor } from '../../test/utils/render';

import TravelBanner from '../TravelBanner';
import { useConfigStore } from '../../stores/config.store';
import { changeLanguage } from '../../i18n/i18n';

const storageSet = vi.fn().mockResolvedValue(undefined);

beforeAll(async () => {
  // Force English so our text assertions are stable regardless of how the
  // navigator locale is mocked in the shared setup.
  await changeLanguage('en-US');
});

beforeEach(() => {
  storageSet.mockClear();
  (window as any)._platform = {
    os: 'macos',
    isDesktop: true,
    isMobile: false,
    version: '0.0.0',
    storage: {
      get: vi.fn().mockResolvedValue(null),
      set: storageSet,
      remove: vi.fn(),
      has: vi.fn(),
      clear: vi.fn(),
      keys: vi.fn(),
    },
  };

  // Reset the store.
  useConfigStore.setState({
    ruleMode: 'chnroute',
    detectedCountry: null,
    suggestedProfile: null,
    modeOverride: 'auto',
    lastAcknowledgedCountry: null,
    loaded: true,
  });
});

afterEach(() => {
  cleanup();
  delete (window as any)._platform;
});

describe('TravelBanner', () => {
  it('shows when detectedCountry is set and differs from lastAcknowledgedCountry', () => {
    useConfigStore.setState({
      modeOverride: 'auto',
      detectedCountry: 'jp',
      lastAcknowledgedCountry: null,
    });

    render(<TravelBanner />);

    expect(screen.getByTestId('travel-banner')).toBeInTheDocument();
    expect(screen.getByText(/Japan/)).toBeInTheDocument();
  });

  it('hides when detectedCountry matches lastAcknowledgedCountry', () => {
    useConfigStore.setState({
      modeOverride: 'auto',
      detectedCountry: 'jp',
      lastAcknowledgedCountry: 'jp',
    });

    render(<TravelBanner />);

    expect(screen.queryByTestId('travel-banner')).not.toBeInTheDocument();
  });

  it('hides when modeOverride is not auto', () => {
    useConfigStore.setState({
      modeOverride: 'global',
      detectedCountry: 'jp',
      lastAcknowledgedCountry: null,
    });

    render(<TravelBanner />);
    expect(screen.queryByTestId('travel-banner')).not.toBeInTheDocument();

    cleanup();

    useConfigStore.setState({ modeOverride: 'manual' });
    render(<TravelBanner />);
    expect(screen.queryByTestId('travel-banner')).not.toBeInTheDocument();
  });

  it('hides when detectedCountry is null', () => {
    useConfigStore.setState({
      modeOverride: 'auto',
      detectedCountry: null,
      lastAcknowledgedCountry: null,
    });

    render(<TravelBanner />);
    expect(screen.queryByTestId('travel-banner')).not.toBeInTheDocument();
  });

  it('shows supported-country prompt for the 14 supported profiles', () => {
    useConfigStore.setState({
      modeOverride: 'auto',
      detectedCountry: 'ru',
      lastAcknowledgedCountry: null,
    });

    render(<TravelBanner />);
    expect(screen.getByText(/Switch to this country/)).toBeInTheDocument();
  });

  it('shows global-route prompt for countries outside the 14-profile list', () => {
    useConfigStore.setState({
      modeOverride: 'auto',
      detectedCountry: 'jp',
      lastAcknowledgedCountry: null,
    });

    render(<TravelBanner />);
    expect(screen.getByText(/traffic will route globally/)).toBeInTheDocument();
  });

  it('clicking Switch acknowledges the current country and hides banner', async () => {
    useConfigStore.setState({
      modeOverride: 'auto',
      detectedCountry: 'ru',
      lastAcknowledgedCountry: null,
    });

    const { rerender } = render(<TravelBanner />);
    fireEvent.click(screen.getByTestId('travel-banner-switch'));

    await waitFor(() => {
      expect(useConfigStore.getState().lastAcknowledgedCountry).toBe('ru');
    });

    expect(storageSet).toHaveBeenCalled();
    const lastCall = storageSet.mock.calls[storageSet.mock.calls.length - 1];
    expect(lastCall[1]).toMatchObject({
      ruleMode: 'chnroute',
      modeOverride: 'auto',
      lastAcknowledgedCountry: 'ru',
    });

    // Re-render to verify the banner is now hidden.
    rerender(<TravelBanner />);
    expect(screen.queryByTestId('travel-banner')).not.toBeInTheDocument();
  });

  it('clicking Dismiss acknowledges the country without changing mode', async () => {
    useConfigStore.setState({
      modeOverride: 'auto',
      detectedCountry: 'jp',
      lastAcknowledgedCountry: null,
    });

    const { rerender } = render(<TravelBanner />);
    fireEvent.click(screen.getByTestId('travel-banner-dismiss'));

    await waitFor(() => {
      expect(useConfigStore.getState().lastAcknowledgedCountry).toBe('jp');
    });

    // Mode override remains auto.
    expect(useConfigStore.getState().modeOverride).toBe('auto');

    expect(storageSet).toHaveBeenCalled();

    rerender(<TravelBanner />);
    expect(screen.queryByTestId('travel-banner')).not.toBeInTheDocument();
  });
});
