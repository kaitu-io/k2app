import { describe, it, expect } from 'vitest';
import { isWeChatAndroid } from '../device-detection';

/**
 * Real-world WeChat Android User-Agent samples.
 * Sources: WeChat dev tools docs, production web logs, Stack Overflow archives.
 * When WeChat ships new versions, new UAs land here — the detection must keep matching.
 */
const WECHAT_ANDROID_UAS = [
  // WeChat 8.0.x on Android — common in 2024-2026
  'Mozilla/5.0 (Linux; Android 13; Pixel 7) AppleWebKit/537.36 (KHTML, like Gecko) Version/4.0 Chrome/107.0.5304.141 Mobile Safari/537.36 MMWEBID/1234 MicroMessenger/8.0.32.2300(0x28002036) WeChat/arm64 Weixin NetType/WIFI Language/zh_CN ABI/arm64',
  // WeChat with X5 (TBS) kernel — QQ Browser's Tencent-built X5
  'Mozilla/5.0 (Linux; U; Android 12; zh-cn; SM-G9730 Build/RKQ1.211001.001) AppleWebKit/537.36 (KHTML, like Gecko) Version/4.0 Chrome/77.0.3865.120 MQQBrowser/6.2 TBS/046011 Mobile Safari/537.36 MMWEBID/8341 MicroMessenger/8.0.44.2560(0x2800002C) WeChat/arm64 Weixin NetType/WIFI Language/zh_CN ABI/arm64',
  // Moments (朋友圈) external link opener
  'Mozilla/5.0 (Linux; Android 11; MI 10 Build/RKQ1.200826.002) AppleWebKit/537.36 (KHTML, like Gecko) Version/4.0 Chrome/86.0.4240.99 XWEB/4375 MMWEBSDK/20231202 Mobile Safari/537.36 MMWEBID/5896 MicroMessenger/8.0.47.2560(0x28002F35) WeChat/arm32 Weixin NetType/4G Language/zh_CN',
  // WeChat older 7.x
  'Mozilla/5.0 (Linux; Android 9; SM-N960F Build/PPR1.180610.011) AppleWebKit/537.36 (KHTML, like Gecko) Version/4.0 Chrome/78.0.3904.62 Mobile Safari/537.36 MMWEBID/3344 MicroMessenger/7.0.21.1880(0x27001535) Process/tools NetType/WIFI Language/zh_CN',
  // WeChat on Android tablet
  'Mozilla/5.0 (Linux; Android 10; SM-T720 Build/QP1A.190711.020) AppleWebKit/537.36 (KHTML, like Gecko) Version/4.0 Chrome/77.0.3865.120 Mobile Safari/537.36 MMWEBID/7722 MicroMessenger/8.0.30.2240(0x28001E3F) WeChat/arm64 Weixin NetType/WIFI Language/zh_CN',
  // WeChat with NetType/3G+ (network transition)
  'Mozilla/5.0 (Linux; Android 12; Pixel 6) AppleWebKit/537.36 (KHTML, like Gecko) Version/4.0 Chrome/99.0.4844.88 Mobile Safari/537.36 MMWEBID/1001 MicroMessenger/8.0.38.2400(0x28002631) WeChat/arm64 Weixin NetType/3G+ Language/zh_CN ABI/arm64',
  // Mini-program web-view host on Android
  'Mozilla/5.0 (Linux; Android 11; RMX3461 Build/RKQ1.201217.002) AppleWebKit/537.36 (KHTML, like Gecko) Version/4.0 Chrome/86.0.4240.99 XWEB/5181 MMWEBSDK/20240301 Mobile Safari/537.36 MMWEBID/4400 MicroMessenger/8.0.49.2600(0x28003139) WeChat/arm64 Weixin NetType/WIFI Language/zh_CN ABI/arm64 miniProgram',
  // Lowercased MicroMessenger (some UA parsers lowercase the string; our detector already lowercases)
  'Mozilla/5.0 (linux; android 14; pixel 8) applewebkit/537.36 micromessenger/8.0.50.2610(0x28003234)',
  // Android + MicroMessenger without version suffix
  'Mozilla/5.0 (Linux; Android) MicroMessenger',
];

/**
 * UAs that MUST NOT trigger the overlay. If any of these match, we're spuriously
 * blocking real users from buying.
 */
const NON_WECHAT_ANDROID_UAS = [
  // iOS WeChat — uses WKWebView, has reliable cookies, should NOT be blocked
  'Mozilla/5.0 (iPhone; CPU iPhone OS 17_0 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Mobile/15E148 MicroMessenger/8.0.40(0x18002830) NetType/WIFI Language/zh_CN',
  // iPad WeChat
  'Mozilla/5.0 (iPad; CPU OS 16_6 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Mobile/15E148 MicroMessenger/8.0.42(0x18002a31) NetType/WIFI Language/zh_CN',
  // Desktop WeChat (Windows)
  'Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/107.0.0.0 Safari/537.36 MicroMessenger/3.8.0',
  // Desktop WeChat (macOS)
  'Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/605.1.15 (KHTML, like Gecko) MicroMessenger/3.8.5',
  // Regular Chrome on Android — must pass through
  'Mozilla/5.0 (Linux; Android 13; Pixel 7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Mobile Safari/537.36',
  // Chrome on Android with "Mozilla" but no MicroMessenger
  'Mozilla/5.0 (Linux; Android 14; SM-G998B) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/119.0.0.0 Mobile Safari/537.36',
  // QQ Browser on Android (has TBS/X5 but no MicroMessenger — real browser, not WeChat webview)
  'Mozilla/5.0 (Linux; U; Android 12; zh-cn; SM-G9730 Build/RKQ1.211001.001) AppleWebKit/537.36 (KHTML, like Gecko) Version/4.0 Chrome/77.0.3865.120 MQQBrowser/6.2 TBS/046011 Mobile Safari/537.36',
  // UC Browser on Android
  'Mozilla/5.0 (Linux; U; Android 10; zh-CN; RMX1971 Build/QKQ1.191217.002) AppleWebKit/537.36 (KHTML, like Gecko) Version/4.0 Chrome/78.0.3904.108 UCBrowser/13.4.2.1307 Mobile Safari/537.36',
  // Safari on macOS
  'Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/17.0 Safari/605.1.15',
  // Edge on Windows
  'Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36 Edg/120.0.0.0',
  // Firefox on Android
  'Mozilla/5.0 (Android 13; Mobile; rv:120.0) Gecko/120.0 Firefox/120.0',
  // Empty string
  '',
];

describe('isWeChatAndroid — positive matrix', () => {
  it.each(WECHAT_ANDROID_UAS)('detects WeChat Android: %s', (ua) => {
    expect(isWeChatAndroid(ua)).toBe(true);
  });
});

describe('isWeChatAndroid — negative matrix', () => {
  it.each(NON_WECHAT_ANDROID_UAS)('does NOT detect (must pass through): %s', (ua) => {
    expect(isWeChatAndroid(ua)).toBe(false);
  });
});

describe('isWeChatAndroid — environmental behavior', () => {
  it('reads from window.navigator.userAgent when no arg passed', () => {
    const original = window.navigator.userAgent;
    Object.defineProperty(window.navigator, 'userAgent', {
      value: WECHAT_ANDROID_UAS[0],
      configurable: true,
    });
    try {
      expect(isWeChatAndroid()).toBe(true);
    } finally {
      Object.defineProperty(window.navigator, 'userAgent', {
        value: original,
        configurable: true,
      });
    }
  });

  it('returns false on non-WeChat default jsdom UA', () => {
    // jsdom default UA has neither MicroMessenger nor Android
    expect(isWeChatAndroid()).toBe(false);
  });
});
