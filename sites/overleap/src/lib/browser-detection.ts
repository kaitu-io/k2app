/**
 * Browser-family detection for the warning bar.
 *
 * Strategy: allowlist mainstream browsers; everything else (in-app webviews,
 * Android WebView, stripped/empty UAs) falls into 'unknown' and triggers the
 * warning. This is the single source of truth for "is this a usable browser
 * for login/checkout" — do not add WeChat-specific code paths elsewhere.
 */

export type BrowserFamily =
  | 'chrome'
  | 'edge'
  | 'safari'
  | 'firefox'
  | 'opera'
  | 'samsung'
  | 'arc'
  | 'vivaldi'
  | 'unknown';

export interface BrowserInfo {
  family: BrowserFamily;
  isMainstream: boolean;
  isInAppWebView: boolean;
  /** iOS version × 100 (e.g. 1304 for iOS 13.4), null when not iOS. All iOS
   *  browsers share the system WebKit, so this gates JS-feature support for
   *  every iOS browser regardless of brand. */
  iosVersion: number | null;
  /** true when iOS version is below our minimum target (13.4). Client code
   *  shipped to the browser uses `?.` / `??` (ES2020) which iOS Safari < 13.4
   *  rejects at parse time — those users get a blank page. */
  isOutdatedIOS: boolean;
  userAgent: string;
}

/** Minimum iOS version we can serve. Below this, our chunks fail to parse. */
export const MIN_IOS_VERSION = 1304;

const IN_APP_WEBVIEW_TOKENS = [
  'micromessenger',
  'mmwebsdk',
  'xweb/',
  'miniprogram',
  'qq/',
  'mqqbrowser',
  'ucbrowser',
  'weibo',
  'dingtalk',
  'lark/',
  'feishu',
  'fban',
  'fbav',
  'instagram',
  'tiktok',
  'bytedancewebview',
  'line/',
];

function hasInAppWebViewToken(ua: string): boolean {
  const lower = ua.toLowerCase();
  return IN_APP_WEBVIEW_TOKENS.some((tok) => lower.includes(tok));
}

function hasAndroidWebViewMarker(ua: string): boolean {
  // Android WebView appends "; wv)" right before the WebKit token.
  return /;\s*wv\s*\)/i.test(ua);
}

function isIOSWKWebView(ua: string): boolean {
  if (!/iPhone|iPad|iPod/.test(ua)) return false;
  // Mainstream iOS browsers legitimately omit Version/ even though they
  // are not in-app webviews. Recognize them explicitly so they don't trip
  // the warning bar.
  if (/CriOS\/|FxiOS\/|EdgiOS\/|OPiOS\//.test(ua)) return false;
  // Real mobile Safari has BOTH "Safari/" and "Version/". WKWebViews
  // embedded in in-app browsers typically lack one or both.
  return !(/Safari\//.test(ua) && /Version\//.test(ua));
}

/** Extract iOS major.minor version × 100 from a UA. Returns null if not iOS.
 *  iOS UA examples:
 *    "CPU iPhone OS 17_0 like Mac OS X"  -> 1700
 *    "CPU iPhone OS 13_4 like Mac OS X"  -> 1304
 *    "CPU iPad OS 16_3_1 like Mac OS X"  -> 1603
 *    "CPU iPhone OS 12 like Mac OS X"    -> 1200 */
export function getIOSVersion(ua: string): number | null {
  if (!/iPhone|iPad|iPod/.test(ua)) return null;
  const m = ua.match(/(?:iPhone OS|iPad OS|CPU OS)\s+(\d+)(?:[._](\d+))?/);
  if (!m) return null;
  const major = parseInt(m[1], 10);
  const minor = m[2] ? parseInt(m[2], 10) : 0;
  if (!Number.isFinite(major)) return null;
  return major * 100 + minor;
}

function identifyMainstream(ua: string): BrowserFamily {
  // Order matters: specific tokens before generic Chrome/Safari, because
  // Chromium-derived browsers all also include "Chrome/" in their UA.
  if (/Edg(?:A|iOS)?\//.test(ua)) return 'edge';
  if (/OPR\/|Opera|OPiOS\//.test(ua)) return 'opera';
  if (/SamsungBrowser/.test(ua)) return 'samsung';
  if (/Vivaldi/.test(ua)) return 'vivaldi';
  if (/Arc\//.test(ua)) return 'arc';
  if (/Firefox\/|FxiOS\//.test(ua)) return 'firefox';
  if (/Chrome\/|CriOS\//.test(ua)) return 'chrome';
  if (/Safari\//.test(ua) && /Version\//.test(ua)) return 'safari';
  return 'unknown';
}

export function detectBrowser(userAgent?: string): BrowserInfo {
  const ua = userAgent ?? (typeof window !== 'undefined' ? window.navigator.userAgent : '');

  if (!ua) {
    return {
      family: 'unknown',
      isMainstream: false,
      isInAppWebView: false,
      iosVersion: null,
      isOutdatedIOS: false,
      userAgent: '',
    };
  }

  const iosVersion = getIOSVersion(ua);
  const isOutdatedIOS = iosVersion !== null && iosVersion < MIN_IOS_VERSION;

  if (hasInAppWebViewToken(ua) || hasAndroidWebViewMarker(ua) || isIOSWKWebView(ua)) {
    return {
      family: 'unknown',
      isMainstream: false,
      isInAppWebView: true,
      iosVersion,
      isOutdatedIOS,
      userAgent: ua,
    };
  }

  const family = identifyMainstream(ua);
  return {
    family,
    isMainstream: family !== 'unknown',
    isInAppWebView: false,
    iosVersion,
    isOutdatedIOS,
    userAgent: ua,
  };
}
