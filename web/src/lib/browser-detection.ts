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
  | 'brave'
  | 'samsung'
  | 'arc'
  | 'vivaldi'
  | 'unknown';

export interface BrowserInfo {
  family: BrowserFamily;
  isMainstream: boolean;
  isInAppWebView: boolean;
  userAgent: string;
}

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
  // Real mobile Safari has BOTH "Safari/" and "Version/". WKWebViews
  // embedded in in-app browsers typically lack one or both.
  return !(/Safari\//.test(ua) && /Version\//.test(ua));
}

function identifyMainstream(ua: string): BrowserFamily {
  // Order matters: specific tokens before generic Chrome/Safari, because
  // Chromium-derived browsers all also include "Chrome/" in their UA.
  if (/Edg(?:A|iOS)?\//.test(ua)) return 'edge';
  if (/OPR\/|Opera/.test(ua)) return 'opera';
  if (/SamsungBrowser/.test(ua)) return 'samsung';
  if (/Vivaldi/.test(ua)) return 'vivaldi';
  if (/Arc\//.test(ua)) return 'arc';
  if (/Firefox\//.test(ua)) return 'firefox';
  if (/Chrome\//.test(ua)) return 'chrome';
  if (/Safari\//.test(ua) && /Version\//.test(ua)) return 'safari';
  return 'unknown';
}

export function detectBrowser(userAgent?: string): BrowserInfo {
  const ua = userAgent ?? (typeof window !== 'undefined' ? window.navigator.userAgent : '');

  if (!ua) {
    return { family: 'unknown', isMainstream: false, isInAppWebView: false, userAgent: '' };
  }

  if (hasInAppWebViewToken(ua) || hasAndroidWebViewMarker(ua) || isIOSWKWebView(ua)) {
    return { family: 'unknown', isMainstream: false, isInAppWebView: true, userAgent: ua };
  }

  const family = identifyMainstream(ua);
  return {
    family,
    isMainstream: family !== 'unknown',
    isInAppWebView: false,
    userAgent: ua,
  };
}
