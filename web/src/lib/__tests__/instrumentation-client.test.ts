import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';

// 记录监控 SDK 在 init 那一刻看到的地址，以及 init 之前 history 有没有被它接管。
const seen = vi.hoisted(() => ({
  hrefAtInit: [] as string[],
  init: [] as Record<string, unknown>[],
  replay: [] as Record<string, unknown>[],
}));
vi.mock('@sentry/nextjs', () => ({
  init: (opts: Record<string, unknown>) => {
    seen.hrefAtInit.push(window.location.href);
    seen.init.push(opts);
  },
  replayIntegration: (opts: Record<string, unknown>) => {
    seen.replay.push(opts);
    return { name: 'Replay' };
  },
  captureRouterTransitionStart: () => {},
}));

const stash = () => (window as unknown as Record<string, unknown>).__chatResume;

describe('instrumentation-client: chat-resume token never reaches the monitoring SDK', () => {
  beforeEach(() => {
    vi.resetModules();
    seen.hrefAtInit = [];
    seen.init = [];
    seen.replay = [];
    delete (window as unknown as Record<string, unknown>).__chatResume;
    vi.stubEnv('NEXT_PUBLIC_SENTRY_DSN', 'https://k@example.ingest/1');
  });
  afterEach(() => {
    vi.unstubAllEnvs();
    vi.restoreAllMocks();
  });

  it('strips #chat=<token> before Sentry.init runs, keeping path and query', async () => {
    window.history.pushState({}, '', '/zh-CN/support?utm_source=mail#chat=abc.DEF-123');
    await import('../../instrumentation-client');
    expect(seen.hrefAtInit).toHaveLength(1);
    expect(seen.hrefAtInit[0]).not.toContain('chat=');
    expect(seen.hrefAtInit[0]).toMatch(/\/zh-CN\/support\?utm_source=mail$/);
    expect(window.location.hash).toBe('');
    expect(stash()).toBe('abc.DEF-123');
  });

  it('strips even when the SDK is disabled (no DSN), and leaves ordinary URLs alone', async () => {
    vi.stubEnv('NEXT_PUBLIC_SENTRY_DSN', '');
    window.history.pushState({}, '', '/zh-CN/support#chat=tok');
    await import('../../instrumentation-client');
    expect(seen.init).toHaveLength(0);
    expect(window.location.hash).toBe('');
    expect(stash()).toBe('tok');

    vi.resetModules();
    delete (window as unknown as Record<string, unknown>).__chatResume;
    window.history.pushState({}, '', '/zh-CN/support?chat=preview#contact');
    const replace = vi.spyOn(window.history, 'replaceState');
    await import('../../instrumentation-client');
    expect(replace).not.toHaveBeenCalled();
    expect(window.location.hash).toBe('#contact');
    expect(stash()).toBeUndefined();
  });

  it('wires the scrubbers as defense in depth: beforeBreadcrumb and beforeSend remove the fragment', async () => {
    window.history.pushState({}, '', '/zh-CN/support');
    await import('../../instrumentation-client');
    const opts = seen.init[0] as {
      beforeBreadcrumb: (b: unknown) => { data: { from: string } };
      beforeSend: (e: unknown) => { request: { url: string } } | null;
    };
    expect(opts.beforeBreadcrumb({ category: 'navigation', data: { from: '/support#chat=tok', to: '/support' } }).data.from).toBe('/support');
    expect(opts.beforeSend({ request: { url: 'https://x.test/support#chat=tok' } })!.request.url).toBe('https://x.test/support');
  });

  // 导航性能条目保留带片段的原始地址，replaceState 改不了：这三条路径只靠钩子。
  it('wires the navigation-timing scrubbers: beforeSendTransaction, beforeSendSpan and Replay beforeAddRecordingEvent', async () => {
    const LEAK = 'https://x.test/support#chat=tok';
    window.history.pushState({}, '', '/zh-CN/support');
    await import('../../instrumentation-client');
    const opts = seen.init[0] as {
      beforeSendTransaction: (e: unknown) => unknown;
      beforeSendSpan: (s: unknown) => unknown;
    };
    const tx = opts.beforeSendTransaction({
      type: 'transaction',
      transaction: '/support#chat=tok',
      spans: [{ span_id: 'a', trace_id: 't', start_timestamp: 1, op: 'browser.request', description: LEAK, data: { 'http.url': LEAK } }],
    });
    expect(tx).not.toBeNull();
    expect(JSON.stringify(tx)).not.toContain('chat=');
    expect(JSON.stringify(tx)).toContain('browser.request');

    const span = opts.beforeSendSpan({ span_id: 'a', trace_id: 't', start_timestamp: 1, description: LEAK, data: { url: LEAK } });
    expect(span).toEqual({ span_id: 'a', trace_id: 't', start_timestamp: 1, description: 'https://x.test/support', data: { url: 'https://x.test/support' } });

    expect(seen.replay).toHaveLength(1);
    const hook = seen.replay[0].beforeAddRecordingEvent as (e: unknown) => unknown;
    const out = hook({ type: 5, timestamp: 1, data: { tag: 'performanceSpan', payload: { op: 'navigation.navigate', description: LEAK, startTimestamp: 1, endTimestamp: 2 } } });
    expect(out).not.toBeNull();
    expect(JSON.stringify(out)).not.toContain('chat=');
    expect(JSON.stringify(out)).toContain('navigation.navigate');
    // 既有的回放选项没被这次改动带偏
    expect(seen.replay[0].maskAllText).toBe(false);
    expect(seen.replay[0].blockAllMedia).toBe(true);
  });
});
