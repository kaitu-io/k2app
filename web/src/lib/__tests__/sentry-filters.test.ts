import { describe, it, expect, beforeEach, afterEach } from 'vitest';
import type { ErrorEvent } from '@sentry/nextjs';
import type { SpanJSON, TransactionEvent } from '../sentry-filters';
import {
  dropFailedFormDataParseFromBotProbes,
  dropFailedServerActionLookupFromBotProbes,
  dropInjectedMatchMediaCircularJsonErrors,
  dropOutdatedBrowserSyntaxErrors,
  dropRscNavigationFallbackRejections,
  scrubChatApiBreadcrumb,
  scrubChatApiEvent,
  scrubChatApiSpan,
  scrubChatResumeBreadcrumb,
  scrubChatResumeEvent,
  scrubChatResumeRecordingEvent,
  scrubChatResumeSpan,
  scrubChatResumeToken,
  scrubChatResumeTransaction,
} from '../sentry-filters';

const originalUA = window.navigator.userAgent;

function spoofUA(ua: string) {
  Object.defineProperty(window.navigator, 'userAgent', { value: ua, configurable: true });
}

const IOS_12 =
  'Mozilla/5.0 (iPhone; CPU iPhone OS 12_5_7 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/12.1.2 Mobile/15E148 Safari/604.1';
const IOS_17 =
  'Mozilla/5.0 (iPhone; CPU iPhone OS 17_0 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/17.0 Mobile/15E148 Safari/604.1';
const CHROME_DESKTOP =
  'Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36';

function syntaxErrorEvent(value: string): ErrorEvent {
  return {
    exception: {
      values: [{ type: 'SyntaxError', value }],
    },
  } as ErrorEvent;
}

afterEach(() => {
  spoofUA(originalUA);
});

describe('dropOutdatedBrowserSyntaxErrors', () => {
  describe('the targeted lookbehind SyntaxError', () => {
    const value = 'Invalid regular expression: invalid group specifier name';

    it('drops the event on outdated iOS (12.5)', () => {
      spoofUA(IOS_12);
      expect(dropOutdatedBrowserSyntaxErrors(syntaxErrorEvent(value))).toBeNull();
    });

    it('KEEPS the event on modern iOS Safari 17 (would be a real regression)', () => {
      spoofUA(IOS_17);
      const event = syntaxErrorEvent(value);
      expect(dropOutdatedBrowserSyntaxErrors(event)).toBe(event);
    });

    it('KEEPS the event on Chrome desktop (would be a real regression)', () => {
      spoofUA(CHROME_DESKTOP);
      const event = syntaxErrorEvent(value);
      expect(dropOutdatedBrowserSyntaxErrors(event)).toBe(event);
    });
  });

  describe('unrelated errors pass through unchanged', () => {
    beforeEach(() => spoofUA(IOS_12));

    it('keeps TypeErrors on outdated iOS', () => {
      const event = {
        exception: { values: [{ type: 'TypeError', value: "Cannot read property 'x' of undefined" }] },
      } as ErrorEvent;
      expect(dropOutdatedBrowserSyntaxErrors(event)).toBe(event);
    });

    it('keeps other SyntaxErrors not about lookbehind', () => {
      const event = syntaxErrorEvent('Unexpected token <');
      expect(dropOutdatedBrowserSyntaxErrors(event)).toBe(event);
    });

    it('keeps events with no exception payload', () => {
      const event = { message: 'just a log' } as ErrorEvent;
      expect(dropOutdatedBrowserSyntaxErrors(event)).toBe(event);
    });

    it('keeps events with empty exception values', () => {
      const event = { exception: { values: [] } } as unknown as ErrorEvent;
      expect(dropOutdatedBrowserSyntaxErrors(event)).toBe(event);
    });
  });
});

function nextOnRequestErrorEvent(
  value: string,
  mechanismType: string = 'auto.function.nextjs.on_request_error',
  type: string = 'TypeError'
): ErrorEvent {
  return {
    exception: {
      values: [{ type, value, mechanism: { handled: false, type: mechanismType } }],
    },
  } as ErrorEvent;
}

describe('dropFailedFormDataParseFromBotProbes', () => {
  it('drops the exact Next.js Server-Action FormData parse TypeError', () => {
    // Reproduces Sentry issue 7494712884: bot POSTs junk body to
    // /[locale]/[...slug]/page, Next.js tries to dispatch as Server Action,
    // undici throws inside captureRequestError.
    const event = nextOnRequestErrorEvent('Failed to parse body as FormData.');
    expect(dropFailedFormDataParseFromBotProbes(event)).toBeNull();
  });

  it('drops the variant without trailing period (defensive against framework wording changes)', () => {
    const event = nextOnRequestErrorEvent('Failed to parse body as FormData');
    expect(dropFailedFormDataParseFromBotProbes(event)).toBeNull();
  });

  it('KEEPS the same string if it ever appears OUTSIDE the onRequestError mechanism', () => {
    // Hypothetical: app code or another library throws the same string.
    // We do NOT mask it — only the framework's request-error path is noise.
    const event = nextOnRequestErrorEvent('Failed to parse body as FormData.', 'generic');
    expect(dropFailedFormDataParseFromBotProbes(event)).toBe(event);
  });

  it('KEEPS other TypeErrors coming through onRequestError (real app bugs)', () => {
    const event = nextOnRequestErrorEvent("Cannot read properties of undefined (reading 'x')");
    expect(dropFailedFormDataParseFromBotProbes(event)).toBe(event);
  });

  it('KEEPS non-TypeError exceptions with the same value (unexpected, surface it)', () => {
    const event = nextOnRequestErrorEvent('Failed to parse body as FormData.', undefined, 'Error');
    expect(dropFailedFormDataParseFromBotProbes(event)).toBe(event);
  });

  it('keeps events with no exception payload', () => {
    const event = { message: 'just a log' } as ErrorEvent;
    expect(dropFailedFormDataParseFromBotProbes(event)).toBe(event);
  });

  it('keeps events with empty exception values', () => {
    const event = { exception: { values: [] } } as unknown as ErrorEvent;
    expect(dropFailedFormDataParseFromBotProbes(event)).toBe(event);
  });

  it('keeps events with no mechanism field', () => {
    const event = {
      exception: {
        values: [{ type: 'TypeError', value: 'Failed to parse body as FormData.' }],
      },
    } as unknown as ErrorEvent;
    expect(dropFailedFormDataParseFromBotProbes(event)).toBe(event);
  });
});

describe('dropFailedServerActionLookupFromBotProbes', () => {
  it('drops the exact Next.js "Failed to find Server Action" Error (no actionId form)', () => {
    // Reproduces Sentry issue 7476230548: bot POSTs to /[locale]/[...slug] with
    // a forged Next-Action header; Next.js's action-handler can't match the id
    // and throws from node_modules/next/dist/server/app-render/action-handler.js.
    const event = nextOnRequestErrorEvent(
      'Failed to find Server Action. This request might be from an older or newer deployment.\nRead more: https://nextjs.org/docs/messages/failed-to-find-server-action',
      'auto.function.nextjs.on_request_error',
      'Error'
    );
    expect(dropFailedServerActionLookupFromBotProbes(event)).toBeNull();
  });

  it('drops the variant with a quoted action id (action-handler.js:819)', () => {
    const event = nextOnRequestErrorEvent(
      'Failed to find Server Action "7f4a9b2c". This request might be from an older or newer deployment.',
      'auto.function.nextjs.on_request_error',
      'Error'
    );
    expect(dropFailedServerActionLookupFromBotProbes(event)).toBeNull();
  });

  it('KEEPS the same string if it ever appears OUTSIDE the onRequestError mechanism', () => {
    const event = nextOnRequestErrorEvent(
      'Failed to find Server Action. This request might be from an older or newer deployment.',
      'generic',
      'Error'
    );
    expect(dropFailedServerActionLookupFromBotProbes(event)).toBe(event);
  });

  it('KEEPS other generic Errors coming through onRequestError (real app bugs)', () => {
    const event = nextOnRequestErrorEvent('Database connection refused', undefined, 'Error');
    expect(dropFailedServerActionLookupFromBotProbes(event)).toBe(event);
  });

  it('KEEPS non-Error exceptions with the same value (unexpected, surface it)', () => {
    const event = nextOnRequestErrorEvent(
      'Failed to find Server Action. This request might be from an older or newer deployment.',
      undefined,
      'TypeError'
    );
    expect(dropFailedServerActionLookupFromBotProbes(event)).toBe(event);
  });

  it('keeps events with no exception payload', () => {
    const event = { message: 'just a log' } as ErrorEvent;
    expect(dropFailedServerActionLookupFromBotProbes(event)).toBe(event);
  });

  it('keeps events with empty exception values', () => {
    const event = { exception: { values: [] } } as unknown as ErrorEvent;
    expect(dropFailedServerActionLookupFromBotProbes(event)).toBe(event);
  });

  it('keeps events with no mechanism field', () => {
    const event = {
      exception: {
        values: [
          {
            type: 'Error',
            value: 'Failed to find Server Action. This request might be from an older or newer deployment.',
          },
        ],
      },
    } as unknown as ErrorEvent;
    expect(dropFailedServerActionLookupFromBotProbes(event)).toBe(event);
  });
});

function circularJsonEvent(frames: Array<{ function?: string; filename?: string }>): ErrorEvent {
  return {
    exception: {
      values: [
        {
          type: 'TypeError',
          value:
            "Converting circular structure to JSON\n    --> starting at object with constructor 'HTMLHtmlElement'\n    |     property '__reactFiber$xgd5lru6jpq' -> object with constructor 'rn'\n    --- property 'stateNode' closes the circle",
          stacktrace: { frames },
        },
      ],
    },
  } as ErrorEvent;
}

describe('dropInjectedMatchMediaCircularJsonErrors', () => {
  it('drops when window.matchMedia is an anonymous (injected) override', () => {
    const event = circularJsonEvent([
      { function: '<anonymous>', filename: './node_modules/next-themes/dist/index.mjs' },
      { function: 'window.matchMedia', filename: '<anonymous>' },
      { function: 'JSON.stringify', filename: '<anonymous>' },
    ]);
    expect(dropInjectedMatchMediaCircularJsonErrors(event)).toBeNull();
  });

  it('KEEPS the same TypeError when matchMedia resolves to our own bundle (real regression)', () => {
    const event = circularJsonEvent([
      { function: 'window.matchMedia', filename: 'https://www.kaitu.io/_next/static/chunks/main-abc123.js' },
      { function: 'JSON.stringify', filename: '<anonymous>' },
    ]);
    expect(dropInjectedMatchMediaCircularJsonErrors(event)).toBe(event);
  });

  it('KEEPS circular JSON TypeErrors unrelated to matchMedia', () => {
    const event = circularJsonEvent([
      { function: 'someAppFunction', filename: 'https://www.kaitu.io/_next/static/chunks/main-abc123.js' },
      { function: 'JSON.stringify', filename: '<anonymous>' },
    ]);
    expect(dropInjectedMatchMediaCircularJsonErrors(event)).toBe(event);
  });

  it('KEEPS other TypeErrors with an unrelated message', () => {
    const event = circularJsonEvent([{ function: 'window.matchMedia', filename: '<anonymous>' }]);
    event.exception!.values![0].value = "Cannot read properties of undefined (reading 'foo')";
    expect(dropInjectedMatchMediaCircularJsonErrors(event)).toBe(event);
  });

  it('keeps events with no stacktrace', () => {
    const event = {
      exception: { values: [{ type: 'TypeError', value: 'Converting circular structure to JSON' }] },
    } as ErrorEvent;
    expect(dropInjectedMatchMediaCircularJsonErrors(event)).toBe(event);
  });

  it('keeps events with no exception payload', () => {
    const event = { message: 'just a log' } as ErrorEvent;
    expect(dropInjectedMatchMediaCircularJsonErrors(event)).toBe(event);
  });
});

function rscFallbackConsoleBreadcrumb(url: string) {
  return {
    category: 'console',
    level: 'error' as const,
    message: `Failed to fetch RSC payload for ${url}. Falling back to browser navigation.`,
  };
}

function loadFailureRejectionEvent(
  value: string,
  breadcrumbs: Array<{ category?: string; message?: string }> = [],
  mechanismType: string = 'auto.browser.global_handlers.onunhandledrejection',
  type: string = 'TypeError'
): ErrorEvent {
  return {
    exception: {
      values: [{ type, value, mechanism: { handled: false, type: mechanismType } }],
    },
    breadcrumbs,
  } as unknown as ErrorEvent;
}

describe('dropRscNavigationFallbackRejections', () => {
  it('drops "Load failed" when paired with the RSC-fetch-fallback console breadcrumb', () => {
    // Reproduces Sentry issue 7551594594: iOS Safari's fetch/stream read fails
    // mid-navigation; Next.js's fetchServerResponse catches it, logs, and
    // falls back to a full navigation (which the breadcrumb trail confirms
    // succeeded) — the dangling promise still surfaces as an unhandled
    // rejection with just the bare TypeError.
    const event = loadFailureRejectionEvent('Load failed', [
      rscFallbackConsoleBreadcrumb('https://www.kaitu.io/zh-CN/purchase'),
    ]);
    expect(dropRscNavigationFallbackRejections(event)).toBeNull();
  });

  it('drops the Chrome-equivalent "Failed to fetch" message with the same breadcrumb', () => {
    const event = loadFailureRejectionEvent('Failed to fetch', [
      rscFallbackConsoleBreadcrumb('https://www.kaitu.io/en-US/account'),
    ]);
    expect(dropRscNavigationFallbackRejections(event)).toBeNull();
  });

  it('KEEPS "Load failed" with no matching breadcrumb (could be our own unguarded fetch)', () => {
    const event = loadFailureRejectionEvent('Load failed', []);
    expect(dropRscNavigationFallbackRejections(event)).toBe(event);
  });

  it('KEEPS "Load failed" when breadcrumbs are unrelated (real bug elsewhere)', () => {
    const event = loadFailureRejectionEvent('Load failed', [
      { category: 'console', message: 'some unrelated log line' },
      { category: 'fetch', message: 'GET https://www.kaitu.io/api/user/info' },
    ]);
    expect(dropRscNavigationFallbackRejections(event)).toBe(event);
  });

  it('KEEPS the same message when it is a handled/caught error, not an unhandled rejection', () => {
    const event = loadFailureRejectionEvent(
      'Load failed',
      [rscFallbackConsoleBreadcrumb('https://www.kaitu.io/zh-CN/purchase')],
      'generic'
    );
    expect(dropRscNavigationFallbackRejections(event)).toBe(event);
  });

  it('KEEPS other TypeErrors even with the matching breadcrumb present', () => {
    const event = loadFailureRejectionEvent(
      "Cannot read properties of undefined (reading 'x')",
      [rscFallbackConsoleBreadcrumb('https://www.kaitu.io/zh-CN/purchase')]
    );
    expect(dropRscNavigationFallbackRejections(event)).toBe(event);
  });

  it('KEEPS non-TypeError exceptions with the same message text', () => {
    const event = loadFailureRejectionEvent(
      'Load failed',
      [rscFallbackConsoleBreadcrumb('https://www.kaitu.io/zh-CN/purchase')],
      'auto.browser.global_handlers.onunhandledrejection',
      'Error'
    );
    expect(dropRscNavigationFallbackRejections(event)).toBe(event);
  });

  it('keeps events with no exception payload', () => {
    const event = { message: 'just a log' } as ErrorEvent;
    expect(dropRscNavigationFallbackRejections(event)).toBe(event);
  });

  it('keeps events with empty exception values', () => {
    const event = { exception: { values: [] } } as unknown as ErrorEvent;
    expect(dropRscNavigationFallbackRejections(event)).toBe(event);
  });

  it('keeps events with no breadcrumbs field at all', () => {
    const event = {
      exception: {
        values: [
          {
            type: 'TypeError',
            value: 'Load failed',
            mechanism: { handled: false, type: 'auto.browser.global_handlers.onunhandledrejection' },
          },
        ],
      },
    } as unknown as ErrorEvent;
    expect(dropRscNavigationFallbackRejections(event)).toBe(event);
  });
});

describe('chat-resume token scrubbing', () => {
  it('scrubChatResumeToken removes only the #chat= fragment', () => {
    expect(scrubChatResumeToken('https://x.test/zh-CN/support?utm=1#chat=abc.DEF-123_x')).toBe('https://x.test/zh-CN/support?utm=1');
    expect(scrubChatResumeToken('/support#chat=tok')).toBe('/support');
    expect(scrubChatResumeToken('GET "/support#chat=tok" failed, then /a#chat=t2 too')).toBe('GET "/support" failed, then /a too');
    for (const untouched of ['/support#contact', '/support?chat=preview', 'https://x.test/', '']) {
      expect(scrubChatResumeToken(untouched)).toBe(untouched);
    }
  });

  it('scrubChatResumeBreadcrumb cleans navigation from/to, fetch url and message; keeps everything else', () => {
    const nav = scrubChatResumeBreadcrumb({
      category: 'navigation',
      data: { from: '/zh-CN/support#chat=tok', to: '/zh-CN/support', status: 200 },
    });
    expect(nav).toEqual({ category: 'navigation', message: undefined, data: { from: '/zh-CN/support', to: '/zh-CN/support', status: 200 } });
    expect(scrubChatResumeBreadcrumb({ category: 'fetch', data: { url: 'https://x.test/p#chat=tok', method: 'GET' } }).data).toEqual({
      url: 'https://x.test/p',
      method: 'GET',
    });
    expect(scrubChatResumeBreadcrumb({ category: 'console', message: 'at /p#chat=tok' }).message).toBe('at /p');
  });

  it('returns the very same breadcrumb when there is nothing to scrub', () => {
    const b = { category: 'navigation', message: 'hi', data: { from: '/a#contact', to: '/b' } };
    expect(scrubChatResumeBreadcrumb(b)).toBe(b);
    const bare = { category: 'ui.click' };
    expect(scrubChatResumeBreadcrumb(bare)).toBe(bare);
  });

  it('scrubChatResumeEvent cleans request.url, the Referer header and attached breadcrumbs', () => {
    const event = scrubChatResumeEvent({
      request: { url: 'https://x.test/support#chat=tok', headers: { Referer: 'https://x.test/a#chat=tok', 'User-Agent': 'ua' } },
      breadcrumbs: [{ category: 'navigation', data: { from: '/a#chat=tok', to: '/a' } }, { category: 'ui.click' }],
      exception: { values: [{ type: 'Error', value: 'boom' }] },
    } as unknown as ErrorEvent);
    expect(JSON.stringify(event)).not.toContain('chat=');
    expect(event.request).toEqual({ url: 'https://x.test/support', headers: { Referer: 'https://x.test/a', 'User-Agent': 'ua' } });
    expect(event.breadcrumbs).toHaveLength(2);
    expect(event.exception?.values?.[0].value).toBe('boom');
  });

  it('scrubChatResumeEvent tolerates events with no request and no breadcrumbs', () => {
    const event = scrubChatResumeEvent({ exception: { values: [{ type: 'Error', value: 'boom' }] } } as ErrorEvent);
    expect(event.exception?.values?.[0].value).toBe('boom');
    expect(event.request).toBeUndefined();
    expect(event.breadcrumbs).toBeUndefined();
  });
});

// PerformanceNavigationTiming.name 保留加载时的完整地址（replaceState 改不了），
// SDK 用它生成下面这些形状的数据——钩子直接吃这些形状。
describe('chat-resume token scrubbing: navigation-timing paths (spans, transactions, replay)', () => {
  const LEAK = 'https://kaitu.io/zh-CN/support?utm=1#chat=abc.DEF-123';
  const CLEAN = 'https://kaitu.io/zh-CN/support?utm=1';
  const span = (over: Partial<SpanJSON> = {}): SpanJSON => ({
    span_id: 's1',
    trace_id: 't1',
    start_timestamp: 1,
    timestamp: 2,
    op: 'browser.request',
    description: LEAK,
    data: { 'sentry.op': 'browser.request', 'http.url': LEAK, 'http.response_content_length': 123 },
    ...over,
  });

  it('scrubChatResumeSpan cleans description and every string in data, keeps the rest', () => {
    const out = scrubChatResumeSpan(span());
    expect(JSON.stringify(out)).not.toContain('chat=');
    expect(out.description).toBe(CLEAN);
    expect(out.data).toEqual({ 'sentry.op': 'browser.request', 'http.url': CLEAN, 'http.response_content_length': 123 });
    expect(out.span_id).toBe('s1');
    expect(out.start_timestamp).toBe(1);
  });

  it('scrubChatResumeSpan returns the very same span when there is nothing to scrub', () => {
    const clean = span({ description: CLEAN, data: { 'http.url': '/a#contact' } });
    expect(scrubChatResumeSpan(clean)).toBe(clean);
  });

  it('scrubChatResumeTransaction cleans the transaction name, request, root-span context, child spans, tags and breadcrumbs', () => {
    const event = {
      type: 'transaction',
      transaction: '/zh-CN/support#chat=abc.DEF-123',
      request: { url: LEAK, headers: { Referer: LEAK } },
      contexts: { trace: { op: 'pageload', span_id: 'r', trace_id: 't1', data: { 'sentry.source': 'url', url: LEAK } } },
      tags: { url: LEAK, locale: 'zh-CN' },
      breadcrumbs: [{ category: 'navigation', data: { from: LEAK, to: CLEAN } }],
      spans: [
        span({ op: 'browser.request' }),
        span({ op: 'browser.response', span_id: 's2' }),
        span({ op: 'browser.DNS', span_id: 's3' }),
        span({ op: 'browser.cache', span_id: 's4' }),
        span({ op: 'resource.script', span_id: 's5', description: '/_next/static/chunk.js', data: {} }),
      ],
    } as unknown as TransactionEvent;
    const out = scrubChatResumeTransaction(event);
    expect(JSON.stringify(out)).not.toContain('chat=');
    expect(out.type).toBe('transaction');
    expect(out.transaction).toBe('/zh-CN/support');
    expect(out.request?.url).toBe(CLEAN);
    expect(out.contexts?.trace?.data).toEqual({ 'sentry.source': 'url', url: CLEAN });
    expect(out.tags).toEqual({ url: CLEAN, locale: 'zh-CN' });
    expect(out.spans).toHaveLength(5);
    expect(out.spans!.map((s) => s.description)).toEqual([CLEAN, CLEAN, CLEAN, CLEAN, '/_next/static/chunk.js']);
    // 没动过的 span 不复制
    expect(out.spans![4]).toBe(event.spans![4]);
    const untouched = { type: 'transaction', transaction: '/a', spans: [span({ description: CLEAN, data: {} })] } as unknown as TransactionEvent;
    expect(scrubChatResumeTransaction(untouched)).toBe(untouched);
  });

  it('scrubChatResumeRecordingEvent cleans a replay performanceSpan built from the navigation entry', () => {
    const event = {
      type: 5,
      timestamp: 1700000000,
      data: {
        tag: 'performanceSpan',
        payload: {
          op: 'navigation.navigate',
          description: LEAK,
          startTimestamp: 1,
          endTimestamp: 2,
          data: { size: 1234, duration: 80, domInteractive: 30 },
        },
      },
    };
    const out = scrubChatResumeRecordingEvent(event);
    expect(JSON.stringify(out)).not.toContain('chat=');
    expect(out.data.payload.description).toBe(CLEAN);
    expect(out.data.payload.data).toBe(event.data.payload.data);
    expect(out.type).toBe(5);
    expect(out.data.tag).toBe('performanceSpan');
  });

  it('scrubChatResumeRecordingEvent also cleans replay breadcrumbs and history spans; clean events pass through by reference', () => {
    const crumb = {
      type: 5,
      timestamp: 1,
      data: { tag: 'breadcrumb', payload: { type: 'default', category: 'navigation', timestamp: 1, message: LEAK, data: { from: LEAK, to: CLEAN, url: LEAK } } },
    };
    const outCrumb = scrubChatResumeRecordingEvent(crumb);
    expect(JSON.stringify(outCrumb)).not.toContain('chat=');
    expect(outCrumb.data.payload.data).toEqual({ from: CLEAN, to: CLEAN, url: CLEAN });

    const history = {
      type: 5,
      timestamp: 1,
      data: { tag: 'performanceSpan', payload: { op: 'navigation.push', description: LEAK, startTimestamp: 1, endTimestamp: 1, data: { previous: LEAK } } },
    };
    expect(JSON.stringify(scrubChatResumeRecordingEvent(history))).not.toContain('chat=');

    const options = { type: 5, timestamp: 1, data: { tag: 'options', payload: { maskAllText: false, sessionSampleRate: 1 } } };
    expect(scrubChatResumeRecordingEvent(options)).toBe(options);
  });

  it('leaves non-plain objects alone and survives cycles via the depth limit', () => {
    const when = new Date(0);
    const loop: Record<string, unknown> = { url: LEAK, when };
    loop.self = loop;
    const out = scrubChatResumeRecordingEvent(loop);
    expect(out.url).toBe(CLEAN);
    expect(out.when).toBe(when);
  });
});

// 形状取自真实浏览器验证（task-14 D1）：`/api/*` 经 rewrites 由 Next 服务端代理，
// 服务端 SDK 把请求体、cookie 一并采进事务。
describe('server-side chat API scrubbing (Next proxy of /api/chat/*)', () => {
  const TOKEN = 'eyJhbGciOiJIUzI1NiJ9.eyJjIjoiYWJjIn0.c2lnbmF0dXJl';
  const sessionTx = () =>
    ({
      type: 'transaction',
      platform: 'node',
      transaction: 'POST http://127.0.0.1:5811/api/chat/session',
      sdk: { name: 'sentry.javascript.nextjs' },
      request: {
        method: 'POST',
        url: 'http://localhost:3411/api/chat/session',
        data: { path: '/zh-CN/support', resume: TOKEN },
        cookies: { sid: 'sid-secret', cid: 'cid-secret' },
        headers: {
          cookie: 'sid=sid-secret; cid=cid-secret',
          Authorization: 'Bearer auth-secret',
          'user-agent': 'UA',
          'x-k2-brand': 'kaitu',
        },
      },
      contexts: { trace: { trace_id: 't', span_id: 's', op: 'http.client', data: { 'http.method': 'POST', url: 'http://127.0.0.1:5811/api/chat/session' } } },
      spans: [],
    }) as unknown as TransactionEvent;

  it('session transaction: drops request body, cookies, cookie/authorization headers; keeps the rest', () => {
    const out = scrubChatApiEvent(sessionTx());
    const json = JSON.stringify(out);
    for (const secret of [TOKEN, 'sid-secret', 'cid-secret', 'auth-secret']) expect(json).not.toContain(secret);
    expect(out.request).toEqual({
      method: 'POST',
      url: 'http://localhost:3411/api/chat/session',
      headers: { 'user-agent': 'UA', 'x-k2-brand': 'kaitu' },
    });
    expect(out.transaction).toBe('POST http://127.0.0.1:5811/api/chat/session');
    expect(out.contexts?.trace?.op).toBe('http.client');
  });

  it('messages transaction: visitor message text (string body) never leaves', () => {
    const ev = sessionTx();
    ev.transaction = 'POST http://127.0.0.1:5811/api/chat/messages';
    ev.request = { url: 'http://localhost:3411/api/chat/messages', data: '{"kind":"text","content":"我的订单号是 12345","clientId":"c"}', cookies: { cid: 'cid-secret' } };
    const json = JSON.stringify(scrubChatApiEvent(ev));
    expect(json).not.toContain('订单号');
    expect(json).not.toContain('cid-secret');
  });

  it('matches on any of: transaction name, request.url, root span data — one is enough', () => {
    const byName = { type: 'transaction', transaction: 'POST /api/chat/email', request: { data: 'a@b.c' } } as unknown as TransactionEvent;
    expect(scrubChatApiEvent(byName).request).toEqual({});
    const byUrl = { request: { url: 'https://kaitu.io/api/chat/email', data: 'a@b.c' } } as unknown as ErrorEvent;
    expect(scrubChatApiEvent(byUrl).request).toEqual({ url: 'https://kaitu.io/api/chat/email' });
    const bySpan = {
      type: 'transaction',
      transaction: 'POST',
      request: { data: 'a@b.c' },
      contexts: { trace: { trace_id: 't', span_id: 's', data: { 'http.target': '/api/chat/email' } } },
    } as unknown as TransactionEvent;
    expect(scrubChatApiEvent(bySpan).request).toEqual({});
  });

  it('filters ?token= wherever it appears in a chat event (url, query_string in all three shapes, spans)', () => {
    const ev = {
      type: 'transaction',
      transaction: 'GET /api/chat/ws',
      request: { url: 'https://kaitu.io/api/chat/ws?token=ws-secret&x=1', query_string: 'token=ws-secret&x=1' },
      spans: [{ span_id: 'a', trace_id: 't', start_timestamp: 1, description: 'GET https://ws.kaitu.io/api/chat/ws?x=1&token=ws-secret', data: { 'http.query': '?token=ws-secret' } }],
    } as unknown as TransactionEvent;
    const out = scrubChatApiEvent(ev);
    expect(JSON.stringify(out)).not.toContain('ws-secret');
    expect(out.request?.url).toBe('https://kaitu.io/api/chat/ws?token=[Filtered]&x=1');
    expect(out.request?.query_string).toBe('token=[Filtered]&x=1');
    const obj = scrubChatApiEvent({ ...ev, request: { url: '/api/chat/ws', query_string: { token: 'ws-secret', x: '1' } } } as unknown as TransactionEvent);
    expect(obj.request?.query_string).toEqual({ x: '1' });
    const pairs = scrubChatApiEvent({ ...ev, request: { url: '/api/chat/ws', query_string: [['token', 'ws-secret'], ['x', '1']] } } as unknown as TransactionEvent);
    expect(pairs.request?.query_string).toEqual([['x', '1']]);
  });

  it('other routes are left exactly as they are (scope is /api/chat/ only), same reference', () => {
    const other = {
      type: 'transaction',
      transaction: 'POST /api/user/login',
      request: { url: 'https://kaitu.io/api/user/login?token=keep', data: { email: 'a@b.c' }, cookies: { sid: 's' }, headers: { cookie: 'sid=s' } },
    } as unknown as TransactionEvent;
    expect(scrubChatApiEvent(other)).toBe(other);
    // 路径只是以 chat 开头的别的路由不算
    const lookalike = { transaction: 'GET /api/chatter/x', request: { data: 'keep' } } as unknown as TransactionEvent;
    expect(scrubChatApiEvent(lookalike)).toBe(lookalike);
  });

  it('still removes a #chat= fragment from any event, chat route or not', () => {
    const page = { type: 'transaction', transaction: 'GET /zh-CN/support', request: { url: 'https://kaitu.io/zh-CN/support#chat=tok', data: 'keep' } } as unknown as TransactionEvent;
    expect(scrubChatApiEvent(page).request).toEqual({ url: 'https://kaitu.io/zh-CN/support', data: 'keep' });
  });

  it('scrubChatApiSpan / scrubChatApiBreadcrumb filter the ws token on chat URLs only', () => {
    const span = scrubChatApiSpan({ span_id: 'a', trace_id: 't', start_timestamp: 1, description: 'GET /api/chat/ws?token=ws-secret', data: { url: 'https://h/api/chat/ws?token=ws-secret' } } as SpanJSON);
    expect(JSON.stringify(span)).not.toContain('ws-secret');
    const keep = { span_id: 'a', trace_id: 't', start_timestamp: 1, description: 'GET /api/x?token=keep' } as SpanJSON;
    expect(scrubChatApiSpan(keep)).toBe(keep);
    expect(scrubChatApiBreadcrumb({ category: 'http', data: { url: 'https://h/api/chat/ws?token=ws-secret', status_code: 200 } }).data).toEqual({
      url: 'https://h/api/chat/ws?token=[Filtered]',
      status_code: 200,
    });
    const crumb = { category: 'http', data: { url: 'https://h/api/x?token=keep' } };
    expect(scrubChatApiBreadcrumb(crumb)).toBe(crumb);
  });
});
