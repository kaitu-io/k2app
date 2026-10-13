import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { renderHook, act } from '@testing-library/react';
import { useUpdater } from '../useUpdater';

function installPlatform(platformType: 'desktop' | 'mobile') {
  const applyUpdateNow = vi.fn().mockResolvedValue(undefined);
  (window as any)._platform = {
    platformType,
    updater: {
      isUpdateReady: true,
      updateInfo: { currentVersion: '0.4.7', newVersion: '0.4.13' },
      isChecking: false,
      error: null,
      applyUpdateNow,
    },
  };
  return applyUpdateNow;
}

describe('useUpdater.applyUpdateNow', () => {
  beforeEach(() => {
    vi.spyOn(console, 'info').mockImplementation(() => {});
  });
  afterEach(() => {
    delete (window as any)._platform;
    vi.restoreAllMocks();
  });

  // Mobile "Update Now" only hands a URL to the browser / App Store and
  // returns; nothing restarts the app. Ticket #3902: the banner sat on
  // "Installing..." with "Later" disabled for days.
  it('mobile: never enters the installing state, so the button stays usable', async () => {
    const apply = installPlatform('mobile');
    const { result } = renderHook(() => useUpdater());

    await act(async () => { await result.current.applyUpdateNow(); });
    expect(apply).toHaveBeenCalledTimes(1);
    expect(result.current.isInstalling).toBe(false);

    await act(async () => { await result.current.applyUpdateNow(); });
    expect(apply).toHaveBeenCalledTimes(2);
    expect(result.current.isInstalling).toBe(false);
  });

  it('desktop: shows installing while the updater installs and restarts', async () => {
    installPlatform('desktop');
    const { result } = renderHook(() => useUpdater());

    await act(async () => { await result.current.applyUpdateNow(); });
    expect(result.current.isInstalling).toBe(true);
  });

  it('desktop: a failed install clears installing and reports the error', async () => {
    const apply = installPlatform('desktop');
    apply.mockRejectedValueOnce(new Error('boom'));
    vi.spyOn(console, 'error').mockImplementation(() => {});
    const { result } = renderHook(() => useUpdater());

    await act(async () => { await result.current.applyUpdateNow(); });
    expect(result.current.isInstalling).toBe(false);
    expect(result.current.error).toBe('boom');
  });
});
