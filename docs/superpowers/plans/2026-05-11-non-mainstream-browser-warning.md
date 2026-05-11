# Non-Mainstream Browser Warning Bar — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace the WeChat-Android-only purchase-page overlay with a site-wide yellow warning bar that triggers on any non-allowlisted browser (in-app webviews, Android WebView, unknown UAs); leave a single source of truth and clean up the legacy WeChat-specific code path.

**Architecture:** New `browser-detection.ts` module produces a typed `BrowserInfo` ({ family, isMainstream, isInAppWebView, userAgent }) via UA-regex matching (blocklist first, then mainstream allowlist). A new `BrowserWarningBar` client component mounts in `[locale]/layout.tsx`, renders `null` on SSR + first paint to avoid hydration mismatch, and after `useEffect` shows a yellow non-dismissible bar above `<Header>` if `!isMainstream && !isEmbedded`. The brand-aware domain (`kaitu.io` / `overleap.io`) is computed server-side from `brand.baseUrl` and passed as a prop into the i18n message via `{domain}` placeholder.

**Tech Stack:** Next.js 15 App Router, React 19, TypeScript, Tailwind CSS 4, next-intl, Vitest + Testing Library.

**Spec:** `docs/superpowers/specs/2026-05-11-non-mainstream-browser-warning-design.md`

---

## File Map

**Create**
- `web/src/lib/browser-detection.ts` — detection module
- `web/src/lib/__tests__/browser-detection.test.ts` — detection tests
- `web/src/components/BrowserWarningBar.tsx` — UI component
- `web/src/components/__tests__/BrowserWarningBar.test.tsx` — component test

**Modify**
- `web/src/app/[locale]/layout.tsx` — mount `<BrowserWarningBar />`
- `web/src/app/[locale]/purchase/PurchaseClient.tsx` — drop WeChat imports + early return
- `web/src/lib/device-detection.ts` — drop `isWeChatAndroid`
- `web/src/lib/__tests__/device-detection.test.ts` — drop WeChat test blocks
- `web/messages/{zh-CN,zh-TW,zh-HK,en-US,en-GB,en-AU,ja}/common.json` — add `browserWarning.message`
- `web/messages/{zh-CN,zh-TW,zh-HK,en-US,en-GB,en-AU,ja}/purchase.json` — remove `wechatGuide` block

**Delete**
- `web/src/components/WeChatBrowserGuide.tsx`
- `web/src/components/__tests__/WeChatBrowserGuide.test.tsx`
- `web/src/app/[locale]/purchase/__tests__/PurchaseClient.wechat-gate.test.tsx`

---

## Conventions

- All commands run from `web/` unless absolutely specified otherwise.
- Vitest test files: `*.test.ts` / `*.test.tsx`. Test setup mocks `useTranslations` → returns the key string; tests assert on i18n keys, not localized text.
- `useEmbedMode()` returns `{ isEmbedded, ... }` (boolean field name is `isEmbedded`, not `isEmbedMode`).
- Path alias `@` → `web/src/`.

---

## Task 1: Create browser-detection module (TDD)

**Files:**
- Create: `web/src/lib/browser-detection.ts`
- Create: `web/src/lib/__tests__/browser-detection.test.ts`

- [ ] **Step 1.1: Create stub module with types and signature only**

Path: `web/src/lib/browser-detection.ts`

```ts
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

export function detectBrowser(_userAgent?: string): BrowserInfo {
  throw new Error('not implemented');
}
```

- [ ] **Step 1.2: Write the failing test matrix**

Path: `web/src/lib/__tests__/browser-detection.test.ts`

```ts
import { describe, it, expect, afterEach } from 'vitest';
import { detectBrowser, type BrowserFamily } from '../browser-detection';

const MAINSTREAM: Array<{ ua: string; family: BrowserFamily; label: string }> = [
  // Chrome
  { label: 'Chrome desktop (Win)', family: 'chrome',
    ua: 'Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36' },
  { label: 'Chrome Android', family: 'chrome',
    ua: 'Mozilla/5.0 (Linux; Android 13; Pixel 7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Mobile Safari/537.36' },
  // Edge
  { label: 'Edge desktop (Win)', family: 'edge',
    ua: 'Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36 Edg/120.0.0.0' },
  { label: 'Edge Android', family: 'edge',
    ua: 'Mozilla/5.0 (Linux; Android 13; Pixel 7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Mobile Safari/537.36 EdgA/120.0.0.0' },
  // Safari
  { label: 'Safari macOS', family: 'safari',
    ua: 'Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/17.0 Safari/605.1.15' },
  { label: 'Safari iOS', family: 'safari',
    ua: 'Mozilla/5.0 (iPhone; CPU iPhone OS 17_0 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/17.0 Mobile/15E148 Safari/604.1' },
  // Firefox
  { label: 'Firefox desktop', family: 'firefox',
    ua: 'Mozilla/5.0 (Windows NT 10.0; Win64; x64; rv:121.0) Gecko/20100101 Firefox/121.0' },
  { label: 'Firefox Android', family: 'firefox',
    ua: 'Mozilla/5.0 (Android 13; Mobile; rv:121.0) Gecko/121.0 Firefox/121.0' },
  // Opera
  { label: 'Opera desktop', family: 'opera',
    ua: 'Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36 OPR/106.0.0.0' },
  // Samsung Internet
  { label: 'Samsung Internet', family: 'samsung',
    ua: 'Mozilla/5.0 (Linux; Android 13; SAMSUNG SM-S918B) AppleWebKit/537.36 (KHTML, like Gecko) SamsungBrowser/23.0 Chrome/115.0.0.0 Mobile Safari/537.36' },
  // Arc (Chromium fork with Arc/ token)
  { label: 'Arc desktop', family: 'arc',
    ua: 'Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36 Arc/1.50.0' },
  // Vivaldi
  { label: 'Vivaldi desktop', family: 'vivaldi',
    ua: 'Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36 Vivaldi/6.5.3206.55' },
];

const IN_APP_WEBVIEW: Array<{ ua: string; label: string }> = [
  { label: 'WeChat Android (X5 kernel)',
    ua: 'Mozilla/5.0 (Linux; Android 13; Pixel 7) AppleWebKit/537.36 (KHTML, like Gecko) Version/4.0 Chrome/107.0.5304.141 Mobile Safari/537.36 MMWEBID/1234 MicroMessenger/8.0.32.2300(0x28002036) WeChat/arm64 Weixin NetType/WIFI Language/zh_CN' },
  { label: 'WeChat iOS', ua:
    'Mozilla/5.0 (iPhone; CPU iPhone OS 17_0 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Mobile/15E148 MicroMessenger/8.0.40(0x18002830) NetType/WIFI Language/zh_CN' },
  { label: 'WeChat desktop (Windows)', ua:
    'Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/107.0.0.0 Safari/537.36 MicroMessenger/3.8.0' },
  { label: 'WeChat desktop (macOS)', ua:
    'Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/605.1.15 (KHTML, like Gecko) MicroMessenger/3.8.5' },
  { label: 'WeChat mini-program web-view', ua:
    'Mozilla/5.0 (Linux; Android 11; RMX3461) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/86.0.4240.99 XWEB/5181 MMWEBSDK/20240301 MicroMessenger/8.0.49 miniProgram' },
  { label: 'QQ Browser', ua:
    'Mozilla/5.0 (Linux; U; Android 12; zh-cn; SM-G9730) AppleWebKit/537.36 (KHTML, like Gecko) Version/4.0 Chrome/77.0.3865.120 MQQBrowser/6.2 Mobile Safari/537.36' },
  { label: 'UC Browser', ua:
    'Mozilla/5.0 (Linux; U; Android 10; zh-CN; RMX1971) AppleWebKit/537.36 (KHTML, like Gecko) Version/4.0 Chrome/78.0.3904.108 UCBrowser/13.4.2.1307 Mobile Safari/537.36' },
  { label: 'Weibo in-app', ua:
    'Mozilla/5.0 (Linux; Android 12) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/100.0.0.0 Mobile Safari/537.36 Weibo (samsung-SM-G998U__weibo__13.0.0__android__android12)' },
  { label: 'DingTalk in-app', ua:
    'Mozilla/5.0 (Linux; Android 13) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/108.0.0.0 Mobile Safari/537.36 DingTalk(7.0.0)' },
  { label: 'Lark / Feishu', ua:
    'Mozilla/5.0 (Linux; Android 13) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/108.0.0.0 Mobile Safari/537.36 Lark/7.10.0' },
  { label: 'Instagram in-app', ua:
    'Mozilla/5.0 (iPhone; CPU iPhone OS 17_0 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Mobile/15E148 Instagram 312.0.0.27.107' },
  { label: 'Facebook in-app (FBAN)', ua:
    'Mozilla/5.0 (iPhone; CPU iPhone OS 17_0 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Mobile/15E148 [FBAN/FBIOS;FBAV/440.0;FBBV/536635697]' },
  { label: 'TikTok in-app', ua:
    'Mozilla/5.0 (Linux; Android 13) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/108.0.0.0 Mobile Safari/537.36 trill_240500 BytedanceWebview/d8a21c6 TikTok' },
  { label: 'Line in-app', ua:
    'Mozilla/5.0 (iPhone; CPU iPhone OS 17_0 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Mobile/15E148 Line/14.0.0' },
  { label: 'Android WebView (wv marker)', ua:
    'Mozilla/5.0 (Linux; Android 13; Pixel 7; wv) AppleWebKit/537.36 (KHTML, like Gecko) Version/4.0 Chrome/120.0.0.0 Mobile Safari/537.36' },
  { label: 'iOS WKWebView (no Safari/Version token)', ua:
    'Mozilla/5.0 (iPhone; CPU iPhone OS 17_0 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Mobile/15E148' },
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
```

- [ ] **Step 1.3: Run tests — must FAIL with `not implemented`**

```bash
cd web && yarn vitest run src/lib/__tests__/browser-detection.test.ts
```

Expected: all tests fail with `Error: not implemented`.

- [ ] **Step 1.4: Implement detectBrowser**

Path: `web/src/lib/browser-detection.ts` (replace the stub):

```ts
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

// In-app webview tokens. Match is case-insensitive against the raw UA.
const IN_APP_WEBVIEW_TOKENS = [
  'micromessenger', // WeChat (all platforms)
  'mmwebsdk',       // WeChat web-view SDK
  'xweb/',          // WeChat X-Web kernel
  'miniprogram',    // WeChat mini-program web-view
  'qq/',            // QQ Mail / QQ in-app
  'mqqbrowser',     // QQ Browser
  'ucbrowser',
  'weibo',
  'dingtalk',
  'lark/',
  'feishu',
  'fban',           // Facebook iOS in-app
  'fbav',           // Facebook iOS in-app version
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
  // Real mobile Safari has BOTH "Safari/" and "Version/". WKWebViews embedded
  // in in-app browsers typically lack one or both.
  return !(/Safari\//.test(ua) && /Version\//.test(ua));
}

function identifyMainstream(ua: string): BrowserFamily {
  // Order matters: specific tokens (Edge, Opera, Samsung, Vivaldi, Arc) must
  // run before generic Chrome/Safari detection, because those browsers all
  // also include "Chrome/" or "Safari/" in their UA.
  if (/Edg(?:A|iOS)?\//.test(ua)) return 'edge';
  if (/OPR\/|Opera/.test(ua)) return 'opera';
  if (/SamsungBrowser/.test(ua)) return 'samsung';
  if (/Vivaldi/.test(ua)) return 'vivaldi';
  if (/Arc\//.test(ua)) return 'arc';
  if (/Firefox\//.test(ua)) return 'firefox';
  if (/Chrome\//.test(ua)) return 'chrome';
  // Safari requires BOTH tokens (already checked in isIOSWKWebView path; this
  // catches desktop Safari which does not match any above).
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
```

- [ ] **Step 1.5: Run tests — must PASS**

```bash
cd web && yarn vitest run src/lib/__tests__/browser-detection.test.ts
```

Expected: all tests pass.

- [ ] **Step 1.6: Commit**

```bash
cd web && git add src/lib/browser-detection.ts src/lib/__tests__/browser-detection.test.ts
git commit -m "feat(web): add browser-family detection with allowlist + webview blocklist"
```

---

## Task 2: Build BrowserWarningBar component (TDD)

**Files:**
- Create: `web/src/components/BrowserWarningBar.tsx`
- Create: `web/src/components/__tests__/BrowserWarningBar.test.tsx`

- [ ] **Step 2.1: Create component stub**

Path: `web/src/components/BrowserWarningBar.tsx`

```tsx
'use client';

interface BrowserWarningBarProps {
  brandDomain: string;
}

export default function BrowserWarningBar(_props: BrowserWarningBarProps) {
  return null;
}
```

- [ ] **Step 2.2: Write the failing test**

Path: `web/src/components/__tests__/BrowserWarningBar.test.tsx`

```tsx
import React from 'react';
import { describe, it, expect, vi, afterEach } from 'vitest';
import { render, screen, act } from '@testing-library/react';

// Per-test useEmbedMode mock. Default: not embedded.
const mockUseEmbedMode = vi.fn(() => ({
  isEmbedded: false,
  showNavigation: true,
  showFooter: true,
  compactLayout: false,
  authToken: null,
  embedTheme: null as 'auto' | 'light' | 'dark' | null,
}));

vi.mock('@/hooks/useEmbedMode', () => ({
  useEmbedMode: () => mockUseEmbedMode(),
}));

import BrowserWarningBar from '../BrowserWarningBar';

const CHROME_DESKTOP =
  'Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36';
const WECHAT_ANDROID =
  'Mozilla/5.0 (Linux; Android 13; Pixel 7) AppleWebKit/537.36 (KHTML, like Gecko) Version/4.0 Chrome/107.0.5304.141 Mobile Safari/537.36 MicroMessenger/8.0.32.2300';

function spoofUA(ua: string) {
  Object.defineProperty(window.navigator, 'userAgent', { value: ua, configurable: true });
}

const originalUA = window.navigator.userAgent;
afterEach(() => {
  spoofUA(originalUA);
  mockUseEmbedMode.mockReturnValue({
    isEmbedded: false,
    showNavigation: true,
    showFooter: true,
    compactLayout: false,
    authToken: null,
    embedTheme: null,
  });
});

describe('BrowserWarningBar', () => {
  it('does not render for mainstream browsers (Chrome desktop)', async () => {
    spoofUA(CHROME_DESKTOP);
    await act(async () => {
      render(<BrowserWarningBar brandDomain="kaitu.io" />);
    });
    expect(screen.queryByRole('alert')).toBeNull();
  });

  it('renders the warning bar for WeChat Android', async () => {
    spoofUA(WECHAT_ANDROID);
    await act(async () => {
      render(<BrowserWarningBar brandDomain="kaitu.io" />);
    });
    const alert = screen.getByRole('alert');
    expect(alert).toBeInTheDocument();
    // i18n mock returns the key string; assertion is on the key.
    expect(alert.textContent).toContain('common.browserWarning.message');
  });

  it('does not render in embed mode even with a webview UA', async () => {
    spoofUA(WECHAT_ANDROID);
    mockUseEmbedMode.mockReturnValue({
      isEmbedded: true,
      showNavigation: false,
      showFooter: false,
      compactLayout: true,
      authToken: 'token-stub',
      embedTheme: 'auto',
    });
    await act(async () => {
      render(<BrowserWarningBar brandDomain="kaitu.io" />);
    });
    expect(screen.queryByRole('alert')).toBeNull();
  });
});

// Note on SSR safety: the component's `useState(false)` initial value combined
// with the post-mount `useEffect` is the standard pattern for hydration-safe
// client-only logic in Next.js. Testing-library's render() always flushes
// effects via act(), so we cannot directly assert the pre-effect synchronous
// DOM here. Rely on the React pattern + manual smoke (Task 6.4) instead.
```

- [ ] **Step 2.3: Run test — must FAIL on the "renders for WeChat Android" assertion**

```bash
cd web && yarn vitest run src/components/__tests__/BrowserWarningBar.test.tsx
```

Expected: the "renders the warning bar for WeChat Android" test fails (no alert role found).

- [ ] **Step 2.4: Implement the component**

Path: `web/src/components/BrowserWarningBar.tsx` (replace stub):

```tsx
'use client';

import { useEffect, useState } from 'react';
import { useTranslations } from 'next-intl';
import { AlertTriangle } from 'lucide-react';
import { useEmbedMode } from '@/hooks/useEmbedMode';
import { detectBrowser } from '@/lib/browser-detection';

interface BrowserWarningBarProps {
  brandDomain: string;
}

export default function BrowserWarningBar({ brandDomain }: BrowserWarningBarProps) {
  const t = useTranslations();
  const { isEmbedded } = useEmbedMode();
  const [show, setShow] = useState(false);

  useEffect(() => {
    if (isEmbedded) {
      setShow(false);
      return;
    }
    const info = detectBrowser(window.navigator.userAgent);
    setShow(!info.isMainstream);
  }, [isEmbedded]);

  if (!show) return null;

  return (
    <div
      role="alert"
      className="bg-yellow-50 border-b border-yellow-300 text-yellow-900 dark:bg-yellow-900/20 dark:border-yellow-800 dark:text-yellow-200 px-4 py-2.5 text-sm leading-relaxed"
    >
      <div className="mx-auto max-w-7xl flex items-start gap-2">
        <AlertTriangle className="w-4 h-4 mt-0.5 shrink-0" aria-hidden="true" />
        <span>{t('common.browserWarning.message', { domain: brandDomain })}</span>
      </div>
    </div>
  );
}
```

- [ ] **Step 2.5: Run test — must PASS**

```bash
cd web && yarn vitest run src/components/__tests__/BrowserWarningBar.test.tsx
```

Expected: all 4 tests pass.

- [ ] **Step 2.6: Commit**

```bash
cd web && git add src/components/BrowserWarningBar.tsx src/components/__tests__/BrowserWarningBar.test.tsx
git commit -m "feat(web): add BrowserWarningBar component with embed bypass and SSR safety"
```

---

## Task 3: Add i18n keys to common namespace (7 locales)

**Files:**
- Modify: `web/messages/zh-CN/common.json`
- Modify: `web/messages/zh-TW/common.json`
- Modify: `web/messages/zh-HK/common.json`
- Modify: `web/messages/en-US/common.json`
- Modify: `web/messages/en-GB/common.json`
- Modify: `web/messages/en-AU/common.json`
- Modify: `web/messages/ja/common.json`

Insert a new top-level key `browserWarning` immediately after the existing `common` object in each file. The key uses `{domain}` placeholder which next-intl will substitute at render time.

- [ ] **Step 3.1: zh-CN — add browserWarning block**

In `web/messages/zh-CN/common.json`, append a new sibling key after the existing top-level `common` block (find the closing `}` that ends the `"common": { ... }` block; add a comma after it; then insert):

```json
  "browserWarning": {
    "message": "⚠️ 您当前的浏览器可能影响登录和支付。如遇问题，请在 Chrome、Edge 或 Safari 中打开 {domain}。"
  }
```

- [ ] **Step 3.2: zh-TW**

```json
  "browserWarning": {
    "message": "⚠️ 您目前的瀏覽器可能影響登入和付款。如遇問題，請使用 Chrome、Edge 或 Safari 開啟 {domain}。"
  }
```

- [ ] **Step 3.3: zh-HK** (same text as zh-TW)

```json
  "browserWarning": {
    "message": "⚠️ 您目前的瀏覽器可能影響登入和付款。如遇問題，請使用 Chrome、Edge 或 Safari 開啟 {domain}。"
  }
```

- [ ] **Step 3.4: en-US**

```json
  "browserWarning": {
    "message": "⚠️ The browser you're using may prevent login or checkout from working. If you hit a problem, open {domain} in Chrome, Edge, or Safari."
  }
```

- [ ] **Step 3.5: en-GB** (same English as en-US)

```json
  "browserWarning": {
    "message": "⚠️ The browser you're using may prevent login or checkout from working. If you hit a problem, open {domain} in Chrome, Edge, or Safari."
  }
```

- [ ] **Step 3.6: en-AU** (same English as en-US)

```json
  "browserWarning": {
    "message": "⚠️ The browser you're using may prevent login or checkout from working. If you hit a problem, open {domain} in Chrome, Edge, or Safari."
  }
```

- [ ] **Step 3.7: ja**

```json
  "browserWarning": {
    "message": "⚠️ お使いのブラウザでは、ログインや決済が正常に動作しない場合があります。問題が発生した場合は、Chrome、Edge、または Safari で {domain} を開いてください。"
  }
```

- [ ] **Step 3.8: Validate all 7 files are valid JSON and contain the key**

```bash
cd web && for loc in zh-CN zh-TW zh-HK en-US en-GB en-AU ja; do
  python3 -c "import json; d=json.load(open('messages/$loc/common.json')); assert 'browserWarning' in d, '$loc missing'; assert '{domain}' in d['browserWarning']['message'], '$loc placeholder missing'; print('$loc ok')"
done
```

Expected: 7 lines, each ending in `ok`.

- [ ] **Step 3.9: Commit**

```bash
cd web && git add messages/*/common.json
git commit -m "feat(web): add common.browserWarning.message i18n key for 7 locales"
```

---

## Task 4: Mount BrowserWarningBar in [locale]/layout.tsx

**Files:**
- Modify: `web/src/app/[locale]/layout.tsx`

- [ ] **Step 4.1: Edit layout.tsx**

Find the existing import block (lines 1-22 of `web/src/app/[locale]/layout.tsx`). Add the import alongside the other component imports:

```tsx
import BrowserWarningBar from '@/components/BrowserWarningBar';
```

In the layout body, the current structure is:

```tsx
<AuthProvider>
  <LanguageDetectionBanner />
  {children}
  <Toaster />
  <CookieConsent />
  <ChatwootWidget />
</AuthProvider>
```

Replace it with:

```tsx
<AuthProvider>
  <BrowserWarningBar brandDomain={new URL(brand.baseUrl).hostname} />
  <LanguageDetectionBanner />
  {children}
  <Toaster />
  <CookieConsent />
  <ChatwootWidget />
</AuthProvider>
```

`brand` is already in scope (resolved on line `const brand = await getBrand(locale);`). `new URL('https://kaitu.io').hostname` → `'kaitu.io'`; `new URL('https://overleap.io').hostname` → `'overleap.io'`.

- [ ] **Step 4.2: Type-check**

```bash
cd web && yarn tsc --noEmit
```

Expected: no errors.

- [ ] **Step 4.3: Commit**

```bash
cd web && git add src/app/[locale]/layout.tsx
git commit -m "feat(web): mount BrowserWarningBar in [locale] layout above page content"
```

---

## Task 5: Delete WeChat-specific code (single source of truth)

The new bar replaces all WeChat-specific code paths. After this task, the following grep must return zero matches:

```bash
grep -ri "WeChatBrowserGuide\|isWeChatAndroid\|wechatGuide" web/src web/messages
```

- [ ] **Step 5.1: Delete WeChatBrowserGuide component and its test**

```bash
cd web && git rm src/components/WeChatBrowserGuide.tsx src/components/__tests__/WeChatBrowserGuide.test.tsx
```

- [ ] **Step 5.2: Delete PurchaseClient WeChat integration test**

```bash
cd web && git rm src/app/[locale]/purchase/__tests__/PurchaseClient.wechat-gate.test.tsx
```

- [ ] **Step 5.3: Edit `PurchaseClient.tsx` to remove WeChat imports and gate**

In `web/src/app/[locale]/purchase/PurchaseClient.tsx`:

- Delete line 9: `import { isWeChatAndroid } from "@/lib/device-detection";`
- Delete line 10: `import WeChatBrowserGuide from "@/components/WeChatBrowserGuide";`
- Delete lines 99-104 (the comment + `showWeChatGuide` state + `useEffect`):

```tsx
  // WeChat Android webview has unreliable cookies across the pay redirect.
  // Block the purchase flow and guide the user to open in a real browser.
  const [showWeChatGuide, setShowWeChatGuide] = useState(false);
  useEffect(() => {
    setShowWeChatGuide(isWeChatAndroid());
  }, []);
```

- Delete lines 525-527 (the early return):

```tsx
  if (showWeChatGuide) {
    return <WeChatBrowserGuide />;
  }
```

(Line numbers reference the file at HEAD; if minor shifts have happened, locate by content.)

- [ ] **Step 5.4: Edit `device-detection.ts` to remove `isWeChatAndroid`**

In `web/src/lib/device-detection.ts`, delete lines 190-201 (the entire block):

```ts
/**
 * Detect WeChat in-app browser on Android.
 *
 * WeChat's Android webview (X5/TBS kernel) has unreliable HttpOnly cookie
 * handling across redirects — the purchase flow depends on cookies surviving
 * OTP login → payUrl redirect → return. iOS WeChat uses WKWebView and is more
 * reliable, so this check is Android-only by design.
 */
export function isWeChatAndroid(userAgent?: string): boolean {
  const ua = (userAgent ?? (typeof window !== 'undefined' ? window.navigator.userAgent : '')).toLowerCase();
  return /micromessenger/.test(ua) && /android/.test(ua);
}
```

The blank line above and below the function should be cleaned up so the file does not leave a double-blank gap.

- [ ] **Step 5.5: Edit `device-detection.test.ts` to drop WeChat-related blocks**

In `web/src/lib/__tests__/device-detection.test.ts`:

- Change the import line from
  `import { isWeChatAndroid, shouldShowMacOS11Notice } from '../device-detection';`
  to
  `import { shouldShowMacOS11Notice } from '../device-detection';`
- Delete the `WECHAT_ANDROID_UAS` array and its preceding JSDoc.
- Delete the `NON_WECHAT_ANDROID_UAS` array and its preceding JSDoc.
- Delete all three `describe('isWeChatAndroid ...')` blocks.
- Keep all `shouldShowMacOS11Notice` tests untouched.

- [ ] **Step 5.6: Remove `wechatGuide` block from 7 purchase.json files**

In each of the 7 locale files `web/messages/{zh-CN,zh-TW,zh-HK,en-US,en-GB,en-AU,ja}/purchase.json`, delete the entire `wechatGuide` object (5 keys: `title`, `tapHere`, `step1`, `step2`, `reason`). The block in current zh-CN reads:

```json
  "wechatGuide": {
    "title": "请在浏览器中打开此页面",
    "tapHere": "点击这里",
    "step1": "点击右上角的「···」菜单",
    "step2": "选择「在浏览器打开」",
    "reason": "微信内置浏览器无法稳定完成支付，请使用系统浏览器以保证购买成功。"
  },
```

Be careful to delete the trailing comma if `wechatGuide` is not the last key; preserve it if a sibling follows. (In all 7 files, `wechatGuide` is followed by `error403` or a similar sibling — keep the JSON well-formed.)

- [ ] **Step 5.7: Validate 7 purchase.json files are still valid JSON and `wechatGuide` is gone**

```bash
cd web && for loc in zh-CN zh-TW zh-HK en-US en-GB en-AU ja; do
  python3 -c "import json; d=json.load(open('messages/$loc/purchase.json')); assert 'wechatGuide' not in d, '$loc still has wechatGuide'; print('$loc ok')"
done
```

Expected: 7 lines, each ending in `ok`.

- [ ] **Step 5.8: Final grep sweep — no orphan WeChat-browser references**

```bash
cd web && grep -ri "WeChatBrowserGuide\|isWeChatAndroid\|wechatGuide" src messages
```

Expected: no matches (exit code 1, empty output). If anything matches, that's a leftover to remove.

- [ ] **Step 5.9: Run full vitest suite — must PASS**

```bash
cd web && yarn vitest run
```

Expected: all tests pass, including the new `browser-detection` and `BrowserWarningBar` tests. If the suite previously had a `device-detection` describe block referencing WeChat constants, ensure your edit in Step 5.5 removed those cleanly.

- [ ] **Step 5.10: Commit**

```bash
cd web && git add -A
git commit -m "refactor(web): remove WeChat-specific overlay path, consolidate to BrowserWarningBar"
```

---

## Task 6: Verification

- [ ] **Step 6.1: TypeScript check**

```bash
cd web && yarn tsc --noEmit
```

Expected: exit 0.

- [ ] **Step 6.2: Lint**

```bash
cd web && yarn lint
```

Expected: exit 0 with no new warnings tied to the new files.

- [ ] **Step 6.3: Production build**

```bash
cd web && yarn build
```

Expected: build succeeds. The bar is included in every `[locale]` page bundle.

- [ ] **Step 6.4: Manual smoke (dev server)**

```bash
cd web && yarn dev
```

Then in three browsers:

1. **Chrome desktop** → http://localhost:3000/zh-CN — no yellow bar.
2. **Browser devtools UA spoof to WeChat Android UA**:
   `Mozilla/5.0 (Linux; Android 13; Pixel 7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/107.0.5304.141 Mobile Safari/537.36 MicroMessenger/8.0.32.2300`
   → reload http://localhost:3000/zh-CN — yellow bar appears at top, contains "请在 Chrome、Edge 或 Safari 中打开 kaitu.io".
3. **Same WeChat UA + `?embed=true`** → http://localhost:3000/zh-CN/releases?embed=true — no yellow bar (embed mode bypass).

- [ ] **Step 6.5: Spec acceptance criteria check**

Open `docs/superpowers/specs/2026-05-11-non-mainstream-browser-warning-design.md` and re-read section 7 (Acceptance criteria). Confirm each bullet against the current branch:

- [x] WeChat (any platform) shows yellow bar
- [x] Chrome / Edge / Safari / Firefox show no bar
- [x] `?embed=true` shows no bar
- [x] 7 locale common.json files all contain `browserWarning.message`
- [x] `yarn build` succeeds
- [x] `yarn test` passes
- [x] `grep -r WeChatBrowserGuide src/` returns no matches
- [x] `grep -r isWeChatAndroid src/` returns no matches

If anything fails, return to the relevant task — do not mark this plan complete.

---

## Out of scope (do not implement)

These are intentionally **not** in the plan. If you find yourself drawn to them, stop and re-read the spec:

- Dismiss button / sessionStorage persistence
- Per-platform branched copy (Android vs iOS vs macOS WeChat instructions)
- Manager (`/manager/*`) or Payload (`/payload/*`) integration
- Brave runtime detection via `navigator.brave.isBrave()` (UA falls through to `chrome` which is allowlisted — same user-visible result, so the extra async detection is YAGNI)
- Telemetry/analytics on banner views

## Risks during execution

- **JSON trailing-comma errors after Step 5.6**: re-validate with Step 5.7's script before committing.
- **Tests timing out on the WeChat UA test**: confirm `useEffect` actually runs in jsdom — `act()` wrappers in Step 2.2 are required.
- **Layout shift complaints in Lighthouse**: the bar is inline-flow, not sticky; if you see CLS regression, you probably mounted it inside `<Suspense fallback={null}>` — keep it outside that boundary (as in Step 4.1).
