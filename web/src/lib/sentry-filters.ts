import type { ErrorEvent } from '@sentry/nextjs';
import { detectBrowser } from './browser-detection';

const LOOKBEHIND_SYNTAX_ERROR = /invalid group specifier name/i;

const CHATWOOT_SDK_FRAME = /packs\/js\/sdk\.js/i;

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
