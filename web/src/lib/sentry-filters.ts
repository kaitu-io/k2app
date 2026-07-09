import type { ErrorEvent } from '@sentry/nextjs';
import { detectBrowser } from './browser-detection';

const LOOKBEHIND_SYNTAX_ERROR = /invalid group specifier name/i;

const CHATWOOT_SDK_FRAME = /packs\/js\/sdk\.js/i;

const NEXTJS_ON_REQUEST_ERROR_MECHANISM = 'auto.function.nextjs.on_request_error';

const FORMDATA_PARSE_ERROR = /^Failed to parse body as FormData/;

const SERVER_ACTION_LOOKUP_ERROR = /^Failed to find Server Action/;

const UNHANDLED_REJECTION_MECHANISM = 'auto.browser.global_handlers.onunhandledrejection';

const NATIVE_CODE_FILENAME = '[native code]';

const ANONYMOUS_FRAME_FILENAME = '<anonymous>';

const CIRCULAR_JSON_ERROR = /^Converting circular structure to JSON/;

const GENERIC_FETCH_LOAD_FAILURE = /^(Load failed|Failed to fetch)$/;

const RSC_FETCH_FAILURE_CONSOLE_MESSAGE =
  /^Failed to fetch RSC payload for .+\. Falling back to browser navigation\.$/;

/**
 * Drop SyntaxErrors caused by outdated iOS WebKit parsing regex features
 * (lookbehind / Unicode property escapes) it doesn't support. These users
 * already see the "upgrade iOS" warning bar; the SyntaxError is expected
 * background noise from prefetched chunks and would otherwise dominate the
 * Sentry budget.
 *
 * Same error on a modern browser is a real regression (a new lookbehind crept
 * into a client chunk) — we KEEP those so a future maintainer notices.
 *
 * Returning `null` drops the event; returning the event unchanged forwards it.
 */
export function dropOutdatedBrowserSyntaxErrors(event: ErrorEvent): ErrorEvent | null {
  const exc = event.exception?.values?.[0];
  if (exc?.type !== 'SyntaxError') return event;
  if (!exc.value || !LOOKBEHIND_SYNTAX_ERROR.test(exc.value)) return event;

  if (typeof navigator === 'undefined') return event;
  const { isOutdatedIOS } = detectBrowser(navigator.userAgent);
  if (isOutdatedIOS) return null;

  return event;
}

/**
 * Drop errors originating in the self-hosted Chatwoot widget SDK
 * (`<host>/packs/js/sdk.js`). The SDK's `sendMessage` calls
 * `iframe.contentWindow.postMessage` without null-checking, which crashes
 * when the chat iframe hasn't finished loading. We don't own that code and
 * the user-visible effect (chat bubble no-ops on first click) is harmless;
 * the noise is dominated by headless crawlers hitting the bubble before
 * the iframe is ready.
 *
 * The same error from our own code would have a different `filename` and
 * still be reported.
 */
export function dropChatwootSdkErrors(event: ErrorEvent): ErrorEvent | null {
  const frames = event.exception?.values?.[0]?.stacktrace?.frames;
  if (!frames?.length) return event;
  const fromChatwoot = frames.some(
    (f) => typeof f.filename === 'string' && CHATWOOT_SDK_FRAME.test(f.filename)
  );
  return fromChatwoot ? null : event;
}

/**
 * Drop `TypeError: Failed to parse body as FormData.` surfaced through
 * Next.js's `onRequestError` hook. Bots POST junk bodies to URLs that match
 * our catch-all `[locale]/[...slug]/page` route; Next.js tries to dispatch
 * them as Server Actions and undici / @edge-runtime/primitives throws while
 * reading the body. The mechanism check (`auto.function.nextjs.on_request_error`)
 * ensures we only suppress the framework-originated noise — if app code ever
 * throws the same string, it still reaches Sentry.
 */
export function dropFailedFormDataParseFromBotProbes(event: ErrorEvent): ErrorEvent | null {
  const exc = event.exception?.values?.[0];
  if (exc?.type !== 'TypeError') return event;
  if (!exc.value || !FORMDATA_PARSE_ERROR.test(exc.value)) return event;
  if (exc.mechanism?.type !== NEXTJS_ON_REQUEST_ERROR_MECHANISM) return event;
  return null;
}

/**
 * Drop `Error: Failed to find Server Action.` surfaced through Next.js's
 * `onRequestError` hook. Sibling of `dropFailedFormDataParseFromBotProbes`:
 * the same bots POST to `[locale]/[...slug]/page` with a forged `Next-Action`
 * header; the action-handler at
 * `node_modules/next/dist/server/app-render/action-handler.js` can't find a
 * matching built action and throws (with or without a quoted action id).
 * Narrowed by mechanism so app-code errors with this string still surface.
 */
export function dropFailedServerActionLookupFromBotProbes(event: ErrorEvent): ErrorEvent | null {
  const exc = event.exception?.values?.[0];
  if (exc?.type !== 'Error') return event;
  if (!exc.value || !SERVER_ACTION_LOOKUP_ERROR.test(exc.value)) return event;
  if (exc.mechanism?.type !== NEXTJS_ON_REQUEST_ERROR_MECHANISM) return event;
  return null;
}

/**
 * Drop `InvalidAccessError` unhandled promise rejections originating in
 * native `postMessage` calls with no app frame in the stack.
 *
 * Companion to `dropChatwootSdkErrors`: when the same Chatwoot SDK
 * postMessage bug fires on iOS Safari / Baidu Explorer's WebView, the engine
 * strips the JS stack for errors thrown from native code, leaving only
 * `postMessage in [native code]`. The frame-based Chatwoot filter therefore
 * can't see the SDK origin. We narrow on three signals instead:
 *   - exception type === 'InvalidAccessError' (DOMException code 15)
 *   - mechanism is the global unhandled-rejection handler
 *   - stack contains no app frame (only native frames, or none at all)
 *
 * Our only direct `postMessage` call lives in `useEmbedMode` and is wrapped
 * in a synchronous try/catch, so it cannot produce an unhandled rejection.
 * Any app frame in the stack therefore signals a real bug — keep it.
 */
export function dropNativePostMessageRejections(event: ErrorEvent): ErrorEvent | null {
  const exc = event.exception?.values?.[0];
  if (exc?.type !== 'InvalidAccessError') return event;
  if (exc.mechanism?.type !== UNHANDLED_REJECTION_MECHANISM) return event;
  const frames = exc.stacktrace?.frames ?? [];
  const hasAppFrame = frames.some(
    (f) => typeof f.filename === 'string' && f.filename !== NATIVE_CODE_FILENAME
  );
  return hasAppFrame ? event : null;
}

/**
 * Drop `TypeError: Converting circular structure to JSON` thrown from an
 * anonymous, unsourcemapped override of `window.matchMedia`.
 *
 * Observed exclusively on UC Browser / HarmonyOS, always paired with a
 * breadcrumb where the browser's built-in ad-filter runs a giant hardcoded
 * `querySelector` selector list against the page. That same content script
 * appears to override `window.matchMedia` (likely for its own dark-mode
 * detection) and tries to serialize page/DOM state, which throws on the
 * circular `documentElement -> __reactFiber* -> stateNode` graph React
 * attaches to every mounted DOM node.
 *
 * Our own `EmbedThemeProvider` calls `useTheme()` from `next-themes`, whose
 * only `matchMedia` call site (`useEffect(() => { window.matchMedia(...) },
 * ...)` in the bundled `next-themes/dist/index.mjs`) never calls
 * `JSON.stringify`. A first-party regression producing this message would
 * resolve through sourcemaps to a real chunk path — narrowing on the
 * `matchMedia` frame being `<anonymous>` (i.e. injected/eval'd, not ours)
 * means this filter can't mask a real bug in our code.
 */
export function dropInjectedMatchMediaCircularJsonErrors(event: ErrorEvent): ErrorEvent | null {
  const exc = event.exception?.values?.[0];
  if (exc?.type !== 'TypeError') return event;
  if (!exc.value || !CIRCULAR_JSON_ERROR.test(exc.value)) return event;

  const frames = exc.stacktrace?.frames ?? [];
  const fromAnonymousMatchMedia = frames.some(
    (f) => f.function === 'window.matchMedia' && f.filename === ANONYMOUS_FRAME_FILENAME
  );
  return fromAnonymousMatchMedia ? null : event;
}

/**
 * Drop generic `TypeError: Load failed` / `TypeError: Failed to fetch` unhandled
 * rejections that Next.js's own App Router client already caught and recovered
 * from.
 *
 * `fetchServerResponse` (router-reducer) wraps its RSC payload fetch in a
 * try/catch: on failure it logs `console.error("Failed to fetch RSC payload
 * for <url>. Falling back to browser navigation.", err)` and then returns a
 * normal (non-throwing) fallback result that triggers a full MPA navigation —
 * this is the framework's designed degrade path for flaky mobile networks
 * (observed on iOS Safari mid-navigation), not a bug we can fix, and the user
 * is not blocked (navigation still completes, just without the SPA
 * transition). Some dangling promise from that same failure still reaches
 * `window`'s unhandledrejection handler with just the bare `err`, which is
 * what Sentry captures here.
 *
 * We only drop when Sentry's own `event.breadcrumbs` contain the exact
 * console.error message from that catch block moments earlier — this ties
 * the captured rejection directly to Next's internal RSC-fetch fallback
 * rather than to any unrelated unguarded `fetch()` in our own code (which
 * would never produce that breadcrumb and must stay visible).
 */
export function dropRscNavigationFallbackRejections(event: ErrorEvent): ErrorEvent | null {
  const exc = event.exception?.values?.[0];
  if (exc?.type !== 'TypeError') return event;
  if (!exc.value || !GENERIC_FETCH_LOAD_FAILURE.test(exc.value)) return event;
  if (exc.mechanism?.type !== UNHANDLED_REJECTION_MECHANISM) return event;

  const breadcrumbs = event.breadcrumbs ?? [];
  const fromRscFetchFallback = breadcrumbs.some(
    (b) =>
      b.category === 'console' &&
      typeof b.message === 'string' &&
      RSC_FETCH_FAILURE_CONSOLE_MESSAGE.test(b.message)
  );
  return fromRscFetchFallback ? null : event;
}
