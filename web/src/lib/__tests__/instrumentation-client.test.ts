import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';

// 记录监控 SDK 在 init 那一刻看到的地址，以及 init 之前 history 有没有被它接管。
const seen = vi.hoisted(() => ({ hrefAtInit: [] as string[], init: [] as Record<string, unknown>[] }));
vi.mock('@sentry/nextjs', () => ({
  init: (opts: Record<string, unknown>) => {
    seen.hrefAtInit.push(window.location.href);
    seen.init.push(opts);
  },
  replayIntegration: () => ({}),
  captureRouterTransitionStart: () => {},
}));

const stash = () => (window as unknown as Record<string, unknown>).__chatResume;

describe('instrumentation-client: chat-resume token never reaches the monitoring SDK', () => {
  beforeEach(() => {
    vi.resetModules();
    seen.hrefAtInit = [];
    seen.init = [];
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
});
