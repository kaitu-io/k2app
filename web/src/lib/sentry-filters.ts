import type { Breadcrumb, ErrorEvent, init } from '@sentry/nextjs';

// `@sentry/nextjs` does not re-export these two; take them from the option
// signatures so they always match the installed SDK's hooks.
type SentryInitOptions = NonNullable<Parameters<typeof init>[0]>;
export type SpanJSON = Parameters<NonNullable<SentryInitOptions['beforeSendSpan']>>[0];
export type TransactionEvent = Parameters<NonNullable<SentryInitOptions['beforeSendTransaction']>>[0];
import { detectBrowser } from './browser-detection';

const LOOKBEHIND_SYNTAX_ERROR = /invalid group specifier name/i;

const NEXTJS_ON_REQUEST_ERROR_MECHANISM = 'auto.function.nextjs.on_request_error';

const FORMDATA_PARSE_ERROR = /^Failed to parse body as FormData/;

const SERVER_ACTION_LOOKUP_ERROR = /^Failed to find Server Action/;

const UNHANDLED_REJECTION_MECHANISM = 'auto.browser.global_handlers.onunhandledrejection';

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

const CHAT_RESUME_FRAGMENT = /#chat=[^\s"'<>]*/g;

/** Remove a chat-resume token fragment (`#chat=…`) from a URL-bearing string. */
export function scrubChatResumeToken(value: string): string {
  return value.includes('#chat=') ? value.replace(CHAT_RESUME_FRAGMENT, '') : value;
}

function scrubStrings<T extends Record<string, unknown>>(record: T): T {
  let out: Record<string, unknown> | null = null;
  for (const [key, value] of Object.entries(record)) {
    if (typeof value !== 'string') continue;
    const clean = scrubChatResumeToken(value);
    if (clean !== value) (out ??= { ...record })[key] = clean;
  }
  return (out ?? record) as T;
}

/**
 * Defense in depth for the emailed chat-resume link (`/…#chat=<token>`).
 * `instrumentation-client.ts` strips the fragment before `Sentry.init`, so the
 * SDK should never see it; if that ever regresses (or a soft navigation lands
 * on such a URL), this keeps the token out of breadcrumbs — navigation
 * `from`/`to`, fetch/xhr `url`, console messages.
 *
 * Never drops a breadcrumb, only rewrites strings. Transactions, standalone
 * spans and Session Replay have their own hooks below.
 */
export function scrubChatResumeBreadcrumb(breadcrumb: Breadcrumb): Breadcrumb {
  const message =
    typeof breadcrumb.message === 'string' ? scrubChatResumeToken(breadcrumb.message) : breadcrumb.message;
  const data = breadcrumb.data ? scrubStrings(breadcrumb.data) : breadcrumb.data;
  if (message === breadcrumb.message && data === breadcrumb.data) return breadcrumb;
  return { ...breadcrumb, message, data };
}

/**
 * Event-level twin of `scrubChatResumeBreadcrumb`: `request.url`, request
 * headers (Referer) and any breadcrumbs already attached to the event.
 */
export function scrubChatResumeEvent(event: ErrorEvent): ErrorEvent {
  const request = event.request
    ? {
        ...event.request,
        url: typeof event.request.url === 'string' ? scrubChatResumeToken(event.request.url) : event.request.url,
        headers: event.request.headers ? scrubStrings(event.request.headers) : event.request.headers,
      }
    : event.request;
  const breadcrumbs = event.breadcrumbs?.map(scrubChatResumeBreadcrumb);
  return { ...event, request, breadcrumbs };
}

const SCRUB_MAX_DEPTH = 8;

/**
 * Walk plain objects / arrays and rewrite every string with `fn`. Returns the
 * SAME reference when nothing changed, so untouched payloads are not copied.
 * Depth-limited; non-plain objects (Date, Error, DOM nodes) are left alone.
 */
function mapStringsDeep<T>(value: T, fn: (s: string) => string, depth = 0): T {
  if (typeof value === 'string') return fn(value) as T;
  if (value === null || typeof value !== 'object' || depth >= SCRUB_MAX_DEPTH) return value;
  if (Array.isArray(value)) {
    let out: unknown[] | null = null;
    value.forEach((item, i) => {
      const clean = mapStringsDeep(item, fn, depth + 1);
      if (clean !== item) (out ??= value.slice())[i] = clean;
    });
    return (out ?? value) as T;
  }
  const proto = Object.getPrototypeOf(value);
  if (proto !== Object.prototype && proto !== null) return value;
  let out: Record<string, unknown> | null = null;
  for (const [key, item] of Object.entries(value as Record<string, unknown>)) {
    const clean = mapStringsDeep(item, fn, depth + 1);
    if (clean !== item) (out ??= { ...(value as Record<string, unknown>) })[key] = clean;
  }
  return (out ?? value) as T;
}

function scrubChatResumeDeep<T>(value: T): T {
  return mapStringsDeep(value, scrubChatResumeToken);
}

/**
 * `history.replaceState` cannot rewrite `PerformanceNavigationTiming.name`:
 * the navigation entry keeps the URL the document was loaded with, fragment
 * included, for the life of the page. The SDK reads that entry in places the
 * error/breadcrumb hooks never see, so the pre-init strip is not enough:
 *
 *  - pageload transaction: browser-utils builds the `browser.*` request /
 *    response / DNS / cache spans from `entry.name`;
 *  - standalone spans (web vitals, INP) sent outside a transaction;
 *  - Session Replay: the navigation entry becomes a `performanceSpan` whose
 *    `description` is that URL.
 *
 * `beforeSendSpan` hook — every string in the span (description, data.*).
 */
export function scrubChatResumeSpan(span: SpanJSON): SpanJSON {
  return scrubChatResumeDeep(span);
}

/**
 * `beforeSendTransaction` hook — the whole event: transaction name, request,
 * `contexts.trace` (root span data), every child span, tags, breadcrumbs.
 * Never drops the transaction.
 */
export function scrubChatResumeTransaction(event: TransactionEvent): TransactionEvent {
  return scrubChatResumeDeep(event);
}

/**
 * Replay `beforeAddRecordingEvent` hook. The SDK calls it for its custom
 * recording events only (`performanceSpan`, `breadcrumb`, `options`): this
 * scrubs every string in the payload — a span's `description` and `data`, a
 * breadcrumb's `message` / `data.url` / `from` / `to`. rrweb DOM snapshots are
 * not passed through this hook; their `href` is read after the strip.
 * Generic over the event so the replay package's types are not imported here.
 */
export function scrubChatResumeRecordingEvent<T>(event: T): T {
  return scrubChatResumeDeep(event);
}

// ---------------------------------------------------------------------------
// Server / edge runtime: the Next proxy of `/api/chat/*`.
//
// `/api/*` is a Next rewrite, so the request passes through the Next server and
// the server SDK (sendDefaultPii) records it: `request.data` (the resume token
// on `POST /session`, the visitor's message text on `POST /messages`, the
// address on `POST /email`), `request.cookies` and the Cookie header (the
// HttpOnly visitor credential). None of that may leave for chat routes.
// Scope is deliberately `/api/chat/` only — other routes keep the SDK's
// existing behaviour.
// ---------------------------------------------------------------------------

const CHAT_API_PATH = /\/api\/chat\//;
const TOKEN_QUERY_PARAM = /([?&]token=)[^&#\s"'<>]*/g;
const BARE_TOKEN_QUERY = /^(token=)[^&#]*/;
const SENSITIVE_HEADERS = new Set(['cookie', 'authorization']);
const FILTERED = '[Filtered]';

const isChatApiString = (v: unknown): boolean => typeof v === 'string' && CHAT_API_PATH.test(v);

/** `?token=` on a chat URL is the WebSocket credential (`/api/chat/ws?token=…`). */
function filterChatTokenParam(value: string): string {
  if (!value.includes('token=')) return value;
  return value.replace(TOKEN_QUERY_PARAM, `$1${FILTERED}`).replace(BARE_TOKEN_QUERY, `$1${FILTERED}`);
}

const scrubChatString = (value: string) => scrubChatResumeToken(filterChatTokenParam(value));

/** Only a URL that is itself a chat route gets its `token=` filtered; the `#chat=` fragment always goes. */
const scrubChatUrlString = (value: string) => (CHAT_API_PATH.test(value) ? scrubChatString(value) : scrubChatResumeToken(value));

type ChatScrubbable = {
  transaction?: string;
  request?: {
    url?: string;
    data?: unknown;
    cookies?: unknown;
    query_string?: unknown;
    headers?: Record<string, string>;
  };
  contexts?: { trace?: { description?: unknown; data?: Record<string, unknown> } };
};

function isChatApiEvent(event: ChatScrubbable): boolean {
  if (isChatApiString(event.transaction) || isChatApiString(event.request?.url)) return true;
  const trace = event.contexts?.trace;
  if (!trace) return false;
  if (isChatApiString(trace.description)) return true;
  return Object.values(trace.data ?? {}).some(isChatApiString);
}

function dropTokenFromQuery(query: unknown): unknown {
  if (typeof query === 'string') return filterChatTokenParam(query);
  if (Array.isArray(query)) return query.filter((pair) => !(Array.isArray(pair) && pair[0] === 'token'));
  if (query && typeof query === 'object') {
    const rest = { ...(query as Record<string, unknown>) };
    delete rest.token;
    return rest;
  }
  return query;
}

/**
 * `beforeSend` / `beforeSendTransaction` hook for the server and edge runtimes.
 *
 * An event that belongs to a chat route (transaction name, `request.url` or the
 * root span's data mentions `/api/chat/`) loses `request.data`,
 * `request.cookies`, the Cookie / Authorization headers and any `token=` query
 * value, wherever it appears in the event. Every event, chat route or not, also
 * gets the `#chat=` fragment scrub.
 *
 * Never drops the event. Returns the same reference when nothing changed.
 */
export function scrubChatApiEvent<T extends ErrorEvent | TransactionEvent>(event: T): T {
  const loose = event as unknown as ChatScrubbable;
  if (!isChatApiEvent(loose)) return scrubChatResumeDeep(event);
  let next: ChatScrubbable = loose;
  if (loose.request) {
    // eslint-disable-next-line @typescript-eslint/no-unused-vars
    const { data, cookies, ...request } = loose.request;
    if (request.headers) {
      request.headers = Object.fromEntries(
        Object.entries(request.headers).filter(([name]) => !SENSITIVE_HEADERS.has(name.toLowerCase())),
      );
    }
    if ('query_string' in request) request.query_string = dropTokenFromQuery(request.query_string);
    next = { ...loose, request };
  }
  return mapStringsDeep(next, scrubChatString) as unknown as T;
}

/** `beforeSendSpan` twin: a span whose description / data names a chat route. */
export function scrubChatApiSpan(span: SpanJSON): SpanJSON {
  const isChat =
    isChatApiString(span.description) || Object.values((span.data ?? {}) as Record<string, unknown>).some(isChatApiString);
  return mapStringsDeep(span, isChat ? scrubChatString : scrubChatResumeToken);
}

/** `beforeBreadcrumb` twin: outgoing-request breadcrumbs carry the proxied URL. */
export function scrubChatApiBreadcrumb(breadcrumb: Breadcrumb): Breadcrumb {
  return mapStringsDeep(breadcrumb, scrubChatUrlString);
}
