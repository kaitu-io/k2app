# Non-Mainstream Browser Warning Bar — Design

**Date**: 2026-05-11
**Scope**: `web/` (Next.js website)
**Replaces**: existing WeChat-Android-only purchase-page overlay

## Problem

Users opening kaitu.io inside in-app webviews (WeChat, QQ Browser, Weibo, DingTalk, Feishu, Lark, Instagram, Facebook, TikTok, Line, etc.) hit login and payment failures:

- HttpOnly cookies don't reliably survive redirect chains in webviews (especially WeChat Android X5/TBS kernel).
- Some webviews block file downloads (`/install` `.apk` / `.exe`).
- WeChat desktop on macOS sometimes exposes no recognizable user-agent string, defeating UA-only detection.

The current implementation only blocks WeChat on Android, and only on the purchase page. Users on iOS WeChat, macOS WeChat, QQ Browser, or any other in-app webview hit the failures silently with no guidance to escape.

## Strategy

Invert the detection: instead of an open-ended blocklist of "bad" webviews, **allowlist known mainstream browsers** and show a non-dismissible top warning bar on everything else.

The bar tells users that login/checkout may fail and to open kaitu.io in Chrome, Edge, or Safari. It does **not** block content — users can still read marketing pages, share links in WeChat, and browse docs. The friction shows up only when they try to log in or pay, at which point the warning has already primed them with the workaround.

## Requirements

### Allowed browsers (no warning)

Chrome, Edge, Safari, Firefox, Opera, Brave, Samsung Internet, Arc, Vivaldi — desktop and mobile.

### Trigger warning

Every other browser environment, including but not limited to:

- WeChat embedded webview (iOS, Android, macOS, Windows)
- QQ Browser / MQQBrowser
- UC Browser
- Weibo, DingTalk, Feishu, Lark in-app webviews
- Instagram, Facebook (FBAN/FBAV), TikTok, Line in-app webviews
- Android WebView (UA contains `; wv)` marker)
- iOS WKWebView used as in-app webview (iOS UA without `Safari/` or `Version/` token)
- Any UA we don't recognize (including the macOS WeChat case where the UA string is stripped)

### Behavior

- Warning bar renders **above** `<Header>` on every `[locale]/*` page.
- **Non-dismissible** — always shown for the entire session when triggered.
- **Skip in `?embed=true`** — our own Tauri desktop app embeds `/releases` and `/changelog`; never show the warning there.
- **No layout shift** — the bar pushes content down; not `position: sticky`.
- **SSR-safe** — render `null` on the server and on first client paint, decide on `useEffect`. Hydration mismatch must not occur.

### Out of scope (YAGNI)

- Dismiss button / persistence across sessions
- Per-platform branching text ("tap …", "copy link", etc.) — single concise sentence
- Manager (`/manager/*`) or Payload (`/payload/*`) integration
- CTA buttons inside the bar (copy-link, etc.)

## Design

### 1. Detection — `src/lib/browser-detection.ts` (new)

```ts
export type BrowserFamily =
  | 'chrome' | 'edge' | 'safari' | 'firefox'
  | 'opera' | 'brave' | 'samsung' | 'arc' | 'vivaldi'
  | 'unknown';

export interface BrowserInfo {
  family: BrowserFamily;
  isMainstream: boolean;   // family !== 'unknown'
  isInAppWebView: boolean; // diagnostic only; not used by gating logic
  userAgent: string;
}

export function detectBrowser(userAgent?: string): BrowserInfo;
```

**Matching order** (first hit wins):

1. **In-app webview blocklist** (regex, case-insensitive): `MicroMessenger`, `QQ/`, `MQQBrowser`, `UCBrowser`, `Weibo`, `DingTalk`, `Lark`, `Feishu`, `FBAN`, `FBAV`, `Instagram`, `TikTok`, `Line/`, `Bytedance`, `XWEB`, `miniProgram` → `unknown`, `isInAppWebView: true`.
2. **Android WebView marker**: UA contains `; wv)` → `unknown`, `isInAppWebView: true`.
3. **iOS WKWebView heuristic**: UA matches `iPhone|iPad|iPod` but contains **neither** `Safari/` nor `Version/` → `unknown`, `isInAppWebView: true`.
4. **Mainstream identification** (order matters — specific before generic):
   - `Edg/` → `edge`
   - `OPR/` or `Opera` → `opera`
   - `SamsungBrowser` → `samsung`
   - `Vivaldi` → `vivaldi`
   - `Arc/` → `arc`
   - `Firefox/` → `firefox`
   - `Chrome/` → `chrome`
   - `Safari/` AND `Version/` → `safari`
5. **Fallback**: `unknown`, `isInAppWebView: false`.

**Brave override** (client-side only): if `navigator.brave?.isBrave?.()` resolves truthy, set `family: 'brave'`. Brave masquerades as Chrome in UA for privacy; the navigator API is the only way to identify it. UA-only callers (tests, server) cannot distinguish — they return `chrome`, which is also allowlisted, so the user-visible result is unchanged.

**SSR**: with no `userAgent` arg, return `{ family: 'unknown', isMainstream: false, isInAppWebView: false, userAgent: '' }`. The bar component is responsible for not rendering until client effect runs (see below).

### 2. Warning Bar — `src/components/BrowserWarningBar.tsx` (new)

Client component:

```tsx
'use client';

export default function BrowserWarningBar() {
  const t = useTranslations();
  const { isEmbedMode } = useEmbedMode();
  const [show, setShow] = useState(false);

  useEffect(() => {
    if (isEmbedMode) return;
    const info = detectBrowser(window.navigator.userAgent);
    setShow(!info.isMainstream);
  }, [isEmbedMode]);

  if (!show) return null;

  return (
    <div
      role="alert"
      className="bg-yellow-50 border-b border-yellow-300 text-yellow-900
                 dark:bg-yellow-900/20 dark:border-yellow-800 dark:text-yellow-200
                 px-4 py-2.5 text-sm leading-relaxed"
    >
      <div className="mx-auto max-w-7xl flex items-start gap-2">
        <AlertTriangle className="w-4 h-4 mt-0.5 shrink-0" aria-hidden />
        <span>{t('common.browserWarning.message')}</span>
      </div>
    </div>
  );
}
```

Key behaviors:

- Returns `null` on SSR and on the first client paint, so hydration matches the server render. Avoid hydration mismatch warnings.
- `?embed=true` skip uses the existing `useEmbedMode()` hook.
- No layout shift: the bar renders in normal flow above `<Header>`, not `position: fixed/sticky`.
- Width container matches site `max-w-7xl` convention.

### 3. Mount point — `src/app/[locale]/layout.tsx`

Render `<BrowserWarningBar />` as the **first child** of the layout body, before `<Header>`:

```tsx
<body>
  <BrowserWarningBar />
  <Header />
  {children}
  <Footer />
</body>
```

Manager (`/manager/*`), Payload (`/payload/*`), API (`/api/*`, `/app/*`), and the install scripts at `/i/*` do not share this layout and are unaffected.

### 4. i18n

Add a single key `common.browserWarning.message` to all 7 locale files:

| Locale | Message (with `{domain}` placeholder) |
|---|---|
| zh-CN | `⚠️ 您当前的浏览器可能影响登录和支付。如遇问题，请在 Chrome、Edge 或 Safari 中打开 {domain}。` |
| zh-TW | `⚠️ 您目前的瀏覽器可能影響登入和付款。如遇問題，請使用 Chrome、Edge 或 Safari 開啟 {domain}。` |
| zh-HK | `⚠️ 您目前的瀏覽器可能影響登入和付款。如遇問題，請使用 Chrome、Edge 或 Safari 開啟 {domain}。` |
| en-US | `⚠️ The browser you're using may prevent login or checkout from working. If you hit a problem, open {domain} in Chrome, Edge, or Safari.` |
| en-GB | (same as en-US) |
| en-AU | (same as en-US) |
| ja | `⚠️ お使いのブラウザでは、ログインや決済が正常に動作しない場合があります。問題が発生した場合は、Chrome、Edge、または Safari で {domain} を開いてください。` |

Brand-aware domain: the message references a domain (`kaitu.io` / `overleap.app`). Use a single i18n key with an interpolated placeholder:

```json
"browserWarning": {
  "message": "⚠️ ... 请在 Chrome、Edge 或 Safari 中打开 {domain}。"
}
```

The layout (Server Component) computes the brand domain via the existing `brandFromHost(headers().get('host'))` helper and passes it as a prop to `<BrowserWarningBar brandDomain={brand.domain} />`. The component forwards it to `t('common.browserWarning.message', { domain: brandDomain })`. No client-side hostname inspection — the server already knows the brand.

### 5. Cleanup of existing WeChat code

Delete:

- `src/components/WeChatBrowserGuide.tsx`
- `src/components/__tests__/WeChatBrowserGuide.test.tsx`
- `src/app/[locale]/purchase/__tests__/PurchaseClient.wechat-gate.test.tsx`
- `isWeChatAndroid` function + its test block in `src/lib/__tests__/device-detection.test.ts` (keep `shouldShowMacOS11Notice` and the rest of device-detection)

Edit `src/app/[locale]/purchase/PurchaseClient.tsx`:

- Remove `isWeChatAndroid` import (line 9)
- Remove `WeChatBrowserGuide` import (line 10)
- Remove `showWeChatGuide` state and effect (lines 99-103)
- Remove the early-return at lines 525-526

Remove i18n keys `purchase.wechatGuide.*` from all 7 locale JSON files.

`src/lib/device-detection.ts` keeps everything except `isWeChatAndroid`. The new `browser-detection.ts` is a separate module — it does not absorb `detectDevice()` / `shouldShowMacOS11Notice()`, which serve different purposes (download routing, version disclaimer).

### 6. Tests

**`src/lib/__tests__/browser-detection.test.ts`** (new):

- **Mainstream positive matrix**: at least 2 real UAs per family (desktop + mobile where applicable), assert `isMainstream === true` and `family` is correct.
- **In-app webview blocklist**: WeChat (iOS / Android / macOS / Windows variants), QQ, UC, Weibo, DingTalk, Feishu, Lark, Instagram, Facebook, TikTok, Line, miniProgram, XWEB — assert `isMainstream === false` and `isInAppWebView === true`.
- **Android WebView (`; wv)`)**: separate group; assert `isInAppWebView`.
- **iOS WKWebView heuristic**: iOS UAs missing `Safari/` and `Version/` → `isInAppWebView`. Ensure normal mobile Safari is **not** caught (must have both tokens).
- **Order sensitivity**: Edge UA contains both `Chrome/` and `Edg/`; must return `edge`, not `chrome`. Same check for Opera (`OPR/` before `Chrome/`).
- **Empty / SSR**: `detectBrowser()` with no args or `''` returns `unknown`, `isMainstream: false`.

**`src/components/__tests__/BrowserWarningBar.test.tsx`** (new):

- Chrome desktop UA → bar does not render (`queryByRole('alert')` is null).
- WeChat Android UA → bar renders, contains the i18n message.
- `useEmbedMode()` mocked to `isEmbedMode: true` + WeChat UA → bar does not render.
- First render (before `useEffect`) returns null — assert no `alert` role on initial synchronous render.

### 7. Acceptance criteria

- Open kaitu.io in WeChat (Android or iOS): yellow warning bar appears at the top of every page.
- Open kaitu.io in Chrome / Edge / Safari / Firefox: no bar, no DOM change.
- Open `/releases?embed=true` in the Tauri desktop app: no bar.
- All 7 locale JSON files contain `common.browserWarning.message`; `yarn build` succeeds.
- `yarn test` passes, including the new `browser-detection.test.ts` and `BrowserWarningBar.test.tsx`.
- `grep -r WeChatBrowserGuide src/` returns no matches.
- `grep -r isWeChatAndroid src/` returns no matches.
- Lighthouse on `/zh-CN` shows no new CLS regression.

## Risks and mitigations

| Risk | Mitigation |
|---|---|
| New mainstream browser misclassified as unknown (false positive) | Allowlist is explicit; bar text says "may", and is non-blocking — worst case is harmless friction. Adding a new browser is one regex addition. |
| In-app webview not in our blocklist (false negative) | Items 2 (`wv` marker) and 3 (iOS WKWebView heuristic) act as catch-all for most cases. macOS desktop WeChat where UA is stripped → caught by the unknown fallback. |
| Brave detection requires async `navigator.brave.isBrave()` | We treat Brave as Chrome on first paint (UA-based) and apply the override after the promise resolves. Both outcomes are allowlisted — user sees the same (no bar) either way. |
| Hydration mismatch | Component renders `null` on SSR and first client paint; state changes happen inside `useEffect` after hydration completes. |
| SEO penalty from adding a bar | Bar is a single `<div role="alert">` with one `<span>` of localized text; no impact on heading hierarchy, no impact on structured data. Mainstream browsers see zero DOM change. |
