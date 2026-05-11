import { describe, it, expect, afterEach } from 'vitest';
import { shouldShowMacOS11Notice } from '../device-detection';

// ---------------------------------------------------------------------------
// shouldShowMacOS11Notice
// ---------------------------------------------------------------------------

// UAs frozen to the shapes each browser actually ships on macOS, so the
// detection ladder (Client Hints → Firefox regex → fall-through) is exercised
// against real signals, not synthetic ones.
const MACOS_UAS = {
  chromium: 'Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/122.0.0.0 Safari/537.36',
  safari: 'Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/17.0 Safari/605.1.15',
  firefoxBigSur: 'Mozilla/5.0 (Macintosh; Intel Mac OS X 11.0; rv:120.0) Gecko/20100101 Firefox/120.0',
  firefoxMonterey: 'Mozilla/5.0 (Macintosh; Intel Mac OS X 12.0; rv:120.0) Gecko/20100101 Firefox/120.0',
  firefoxAppleSilicon: 'Mozilla/5.0 (Macintosh; Intel Mac OS X 10.15; rv:120.0) Gecko/20100101 Firefox/120.0',
  windowsChrome: 'Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/122.0.0.0 Safari/537.36',
};

function withUA(ua: string, fn: () => Promise<void>): Promise<void> {
  const original = window.navigator.userAgent;
  Object.defineProperty(window.navigator, 'userAgent', { value: ua, configurable: true });
  return fn().finally(() => {
    Object.defineProperty(window.navigator, 'userAgent', { value: original, configurable: true });
  });
}

function stubUserAgentData(
  impl:
    | { getHighEntropyValues: (hints: string[]) => Promise<{ platformVersion?: string }> }
    | undefined,
) {
  const nav = window.navigator as Navigator & { userAgentData?: unknown };
  Object.defineProperty(nav, 'userAgentData', { value: impl, configurable: true });
}

describe('shouldShowMacOS11Notice — policy', () => {
  afterEach(() => {
    const nav = window.navigator as Navigator & { userAgentData?: unknown };
    // jsdom has no userAgentData by default; restore that.
    Object.defineProperty(nav, 'userAgentData', { value: undefined, configurable: true });
  });

  it('hides when Chromium Client Hints report macOS 12', async () => {
    stubUserAgentData({
      getHighEntropyValues: async () => ({ platformVersion: '12.0.0' }),
    });
    await withUA(MACOS_UAS.chromium, async () => {
      expect(await shouldShowMacOS11Notice()).toBe(false);
    });
  });

  it('hides when Chromium Client Hints report macOS 15', async () => {
    stubUserAgentData({
      getHighEntropyValues: async () => ({ platformVersion: '15.1.0' }),
    });
    await withUA(MACOS_UAS.chromium, async () => {
      expect(await shouldShowMacOS11Notice()).toBe(false);
    });
  });

  it('shows when Chromium Client Hints report macOS 11', async () => {
    stubUserAgentData({
      getHighEntropyValues: async () => ({ platformVersion: '11.7.10' }),
    });
    await withUA(MACOS_UAS.chromium, async () => {
      expect(await shouldShowMacOS11Notice()).toBe(true);
    });
  });

  it('shows when Client Hints call rejects (unsupported or blocked)', async () => {
    stubUserAgentData({
      getHighEntropyValues: async () => {
        throw new Error('NotAllowedError');
      },
    });
    await withUA(MACOS_UAS.chromium, async () => {
      expect(await shouldShowMacOS11Notice()).toBe(true);
    });
  });

  it('hides when Firefox UA exposes Mac OS X 12', async () => {
    stubUserAgentData(undefined);
    await withUA(MACOS_UAS.firefoxMonterey, async () => {
      expect(await shouldShowMacOS11Notice()).toBe(false);
    });
  });

  it('shows when Firefox UA exposes Mac OS X 11 (Big Sur)', async () => {
    stubUserAgentData(undefined);
    await withUA(MACOS_UAS.firefoxBigSur, async () => {
      expect(await shouldShowMacOS11Notice()).toBe(true);
    });
  });

  it('shows when Firefox UA is capped at 10.15 (Apple Silicon)', async () => {
    stubUserAgentData(undefined);
    await withUA(MACOS_UAS.firefoxAppleSilicon, async () => {
      expect(await shouldShowMacOS11Notice()).toBe(true);
    });
  });

  it('shows on Safari (UA hard-capped at 10_15_7 post-Big Sur)', async () => {
    stubUserAgentData(undefined);
    await withUA(MACOS_UAS.safari, async () => {
      expect(await shouldShowMacOS11Notice()).toBe(true);
    });
  });

  it('shows when no macOS signal at all (non-macOS UA)', async () => {
    stubUserAgentData(undefined);
    await withUA(MACOS_UAS.windowsChrome, async () => {
      // The disclaimer is only rendered from the macOS panel, but the function
      // itself is conservative: unknown → show, callers gate by platform.
      expect(await shouldShowMacOS11Notice()).toBe(true);
    });
  });
});
