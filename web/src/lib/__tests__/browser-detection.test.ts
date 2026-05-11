import { describe, it, expect, afterEach } from 'vitest';
import { detectBrowser, type BrowserFamily } from '../browser-detection';

const MAINSTREAM: Array<{ ua: string; family: BrowserFamily; label: string }> = [
  { label: 'Chrome desktop (Win)', family: 'chrome',
    ua: 'Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36' },
  { label: 'Chrome Android', family: 'chrome',
    ua: 'Mozilla/5.0 (Linux; Android 13; Pixel 7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Mobile Safari/537.36' },
  { label: 'Edge desktop (Win)', family: 'edge',
    ua: 'Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36 Edg/120.0.0.0' },
  { label: 'Edge Android', family: 'edge',
    ua: 'Mozilla/5.0 (Linux; Android 13; Pixel 7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Mobile Safari/537.36 EdgA/120.0.0.0' },
  { label: 'Safari macOS', family: 'safari',
    ua: 'Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/17.0 Safari/605.1.15' },
  { label: 'Safari iOS', family: 'safari',
    ua: 'Mozilla/5.0 (iPhone; CPU iPhone OS 17_0 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/17.0 Mobile/15E148 Safari/604.1' },
  { label: 'Firefox desktop', family: 'firefox',
    ua: 'Mozilla/5.0 (Windows NT 10.0; Win64; x64; rv:121.0) Gecko/20100101 Firefox/121.0' },
  { label: 'Firefox Android', family: 'firefox',
    ua: 'Mozilla/5.0 (Android 13; Mobile; rv:121.0) Gecko/121.0 Firefox/121.0' },
  { label: 'Opera desktop', family: 'opera',
    ua: 'Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36 OPR/106.0.0.0' },
  { label: 'Samsung Internet', family: 'samsung',
    ua: 'Mozilla/5.0 (Linux; Android 13; SAMSUNG SM-S918B) AppleWebKit/537.36 (KHTML, like Gecko) SamsungBrowser/23.0 Chrome/115.0.0.0 Mobile Safari/537.36' },
  { label: 'Arc desktop', family: 'arc',
    ua: 'Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36 Arc/1.50.0' },
  { label: 'Vivaldi desktop', family: 'vivaldi',
    ua: 'Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36 Vivaldi/6.5.3206.55' },
];

const IN_APP_WEBVIEW: Array<{ ua: string; label: string }> = [
  { label: 'WeChat Android (X5 kernel)',
    ua: 'Mozilla/5.0 (Linux; Android 13; Pixel 7) AppleWebKit/537.36 (KHTML, like Gecko) Version/4.0 Chrome/107.0.5304.141 Mobile Safari/537.36 MMWEBID/1234 MicroMessenger/8.0.32.2300(0x28002036) WeChat/arm64 Weixin NetType/WIFI Language/zh_CN' },
  { label: 'WeChat iOS',
    ua: 'Mozilla/5.0 (iPhone; CPU iPhone OS 17_0 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Mobile/15E148 MicroMessenger/8.0.40(0x18002830) NetType/WIFI Language/zh_CN' },
  { label: 'WeChat desktop (Windows)',
    ua: 'Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/107.0.0.0 Safari/537.36 MicroMessenger/3.8.0' },
  { label: 'WeChat desktop (macOS)',
    ua: 'Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/605.1.15 (KHTML, like Gecko) MicroMessenger/3.8.5' },
  { label: 'WeChat mini-program web-view',
    ua: 'Mozilla/5.0 (Linux; Android 11; RMX3461) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/86.0.4240.99 XWEB/5181 MMWEBSDK/20240301 MicroMessenger/8.0.49 miniProgram' },
  { label: 'QQ Browser',
    ua: 'Mozilla/5.0 (Linux; U; Android 12; zh-cn; SM-G9730) AppleWebKit/537.36 (KHTML, like Gecko) Version/4.0 Chrome/77.0.3865.120 MQQBrowser/6.2 Mobile Safari/537.36' },
  { label: 'UC Browser',
    ua: 'Mozilla/5.0 (Linux; U; Android 10; zh-CN; RMX1971) AppleWebKit/537.36 (KHTML, like Gecko) Version/4.0 Chrome/78.0.3904.108 UCBrowser/13.4.2.1307 Mobile Safari/537.36' },
  { label: 'Weibo in-app',
    ua: 'Mozilla/5.0 (Linux; Android 12) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/100.0.0.0 Mobile Safari/537.36 Weibo (samsung-SM-G998U__weibo__13.0.0__android__android12)' },
  { label: 'DingTalk in-app',
    ua: 'Mozilla/5.0 (Linux; Android 13) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/108.0.0.0 Mobile Safari/537.36 DingTalk(7.0.0)' },
  { label: 'Lark / Feishu',
    ua: 'Mozilla/5.0 (Linux; Android 13) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/108.0.0.0 Mobile Safari/537.36 Lark/7.10.0' },
  { label: 'Instagram in-app',
    ua: 'Mozilla/5.0 (iPhone; CPU iPhone OS 17_0 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Mobile/15E148 Instagram 312.0.0.27.107' },
  { label: 'Facebook in-app (FBAN)',
    ua: 'Mozilla/5.0 (iPhone; CPU iPhone OS 17_0 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Mobile/15E148 [FBAN/FBIOS;FBAV/440.0;FBBV/536635697]' },
  { label: 'TikTok in-app',
    ua: 'Mozilla/5.0 (Linux; Android 13) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/108.0.0.0 Mobile Safari/537.36 trill_240500 BytedanceWebview/d8a21c6 TikTok' },
  { label: 'Line in-app',
    ua: 'Mozilla/5.0 (iPhone; CPU iPhone OS 17_0 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Mobile/15E148 Line/14.0.0' },
  { label: 'Android WebView (wv marker)',
    ua: 'Mozilla/5.0 (Linux; Android 13; Pixel 7; wv) AppleWebKit/537.36 (KHTML, like Gecko) Version/4.0 Chrome/120.0.0.0 Mobile Safari/537.36' },
  { label: 'iOS WKWebView (no Safari/Version token)',
    ua: 'Mozilla/5.0 (iPhone; CPU iPhone OS 17_0 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Mobile/15E148' },
];

describe('detectBrowser — mainstream allowlist', () => {
  it.each(MAINSTREAM)('detects $label as $family (isMainstream=true)', ({ ua, family }) => {
    const info = detectBrowser(ua);
    expect(info.family).toBe(family);
    expect(info.isMainstream).toBe(true);
    expect(info.isInAppWebView).toBe(false);
  });
});

describe('detectBrowser — in-app webview blocklist', () => {
  it.each(IN_APP_WEBVIEW)('flags $label as unknown + isInAppWebView', ({ ua }) => {
    const info = detectBrowser(ua);
    expect(info.family).toBe('unknown');
    expect(info.isMainstream).toBe(false);
    expect(info.isInAppWebView).toBe(true);
  });
});

describe('detectBrowser — order sensitivity', () => {
  it('returns edge (not chrome) for Edge UA that also contains Chrome/', () => {
    const ua = 'Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36 Edg/120.0.0.0';
    expect(detectBrowser(ua).family).toBe('edge');
  });

  it('returns opera (not chrome) for Opera UA that also contains Chrome/', () => {
    const ua = 'Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36 OPR/106.0.0.0';
    expect(detectBrowser(ua).family).toBe('opera');
  });

  it('returns samsung (not chrome) for Samsung Internet UA', () => {
    const ua = 'Mozilla/5.0 (Linux; Android 13; SAMSUNG SM-S918B) AppleWebKit/537.36 (KHTML, like Gecko) SamsungBrowser/23.0 Chrome/115.0.0.0 Mobile Safari/537.36';
    expect(detectBrowser(ua).family).toBe('samsung');
  });
});

describe('detectBrowser — empty / SSR', () => {
  it('returns unknown for empty string', () => {
    expect(detectBrowser('')).toEqual({
      family: 'unknown',
      isMainstream: false,
      isInAppWebView: false,
      userAgent: '',
    });
  });

  it('reads from window.navigator.userAgent when no arg provided', () => {
    const original = window.navigator.userAgent;
    Object.defineProperty(window.navigator, 'userAgent', {
      value: MAINSTREAM[0].ua,
      configurable: true,
    });
    try {
      expect(detectBrowser().family).toBe('chrome');
    } finally {
      Object.defineProperty(window.navigator, 'userAgent', {
        value: original,
        configurable: true,
      });
    }
  });
});

afterEach(() => {
  // Each `it` cleans up via try/finally where it spoofs; nothing global to reset.
});
