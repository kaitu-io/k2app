import type { ErrorEvent } from '@sentry/nextjs';
import { detectBrowser } from './browser-detection';

const LOOKBEHIND_SYNTAX_ERROR = /invalid group specifier name/i;

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
