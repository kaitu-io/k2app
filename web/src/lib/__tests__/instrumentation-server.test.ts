import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';

// 服务端 / edge 的 Sentry.init 必须带上 chat 路由的擦除钩子：`/api/chat/*` 经 rewrites
// 由 Next 服务端代理，SDK 会把请求体（继续对话令牌、访客消息）与 cookie 采进事件。
const seen = vi.hoisted(() => ({ init: [] as Record<string, unknown>[] }));
vi.mock('@sentry/nextjs', () => ({
  init: (opts: Record<string, unknown>) => { seen.init.push(opts); },
  captureRequestError: () => {},
}));

type Hooks = {
  sendDefaultPii: boolean;
  tracesSampleRate: number;
  beforeSend: (e: unknown) => unknown;
  beforeSendTransaction: (e: unknown) => unknown;
  beforeSendSpan: (s: unknown) => unknown;
  beforeBreadcrumb: (b: unknown) => unknown;
};

const TOKEN = 'resume.TOKEN-123';
const chatEvent = () => ({
  transaction: 'POST http://127.0.0.1:5811/api/chat/session',
  request: {
    url: 'http://localhost/api/chat/session',
    data: { path: '/zh-CN/support', resume: TOKEN },
    cookies: { sid: 'sid-secret', cid: 'cid-secret' },
    headers: { cookie: 'sid=sid-secret; cid=cid-secret', 'user-agent': 'UA' },
  },
});

describe.each(['nodejs', 'edge'])('instrumentation (%s runtime): chat routes are scrubbed before leaving the server', (runtime) => {
  beforeEach(() => {
    vi.resetModules();
    seen.init = [];
    vi.stubEnv('NEXT_PUBLIC_SENTRY_DSN', 'https://k@example.ingest/1');
    vi.stubEnv('NEXT_RUNTIME', runtime);
  });
  afterEach(() => {
    vi.unstubAllEnvs();
  });

  const hooks = async () => {
    const { register } = await import('../../instrumentation');
    await register();
    expect(seen.init).toHaveLength(1);
    return seen.init[0] as unknown as Hooks;
  };

  it('beforeSendTransaction and beforeSend strip body, cookies and the cookie header', async () => {
    const h = await hooks();
    for (const hook of [h.beforeSendTransaction, h.beforeSend]) {
      expect(typeof hook).toBe('function');
      const out = hook({ type: hook === h.beforeSendTransaction ? 'transaction' : undefined, ...chatEvent() });
      expect(out).not.toBeNull();
      const json = JSON.stringify(out);
      for (const secret of [TOKEN, 'sid-secret', 'cid-secret']) expect(json).not.toContain(secret);
      expect(json).toContain('/api/chat/session');
      expect(json).toContain('UA');
    }
  });

  it('beforeSendSpan and beforeBreadcrumb filter the ws token', async () => {
    const h = await hooks();
    const span = h.beforeSendSpan({ span_id: 'a', trace_id: 't', start_timestamp: 1, description: 'GET /api/chat/ws?token=ws-secret' });
    expect(JSON.stringify(span)).not.toContain('ws-secret');
    const crumb = h.beforeBreadcrumb({ category: 'http', data: { url: 'http://h/api/chat/ws?token=ws-secret' } });
    expect(JSON.stringify(crumb)).not.toContain('ws-secret');
  });

  it('the existing bot-probe drop filters still run, and global options are untouched', async () => {
    const h = await hooks();
    const probe = {
      exception: { values: [{ type: 'TypeError', value: 'Failed to parse body as FormData.', mechanism: { type: 'auto.function.nextjs.on_request_error' } }] },
    };
    expect(h.beforeSend(probe)).toBeNull();
    const real = { exception: { values: [{ type: 'Error', value: 'boom' }] }, request: { url: 'http://localhost/api/user/x', data: 'keep' } };
    expect(h.beforeSend(real)).toBe(real);
    expect(h.sendDefaultPii).toBe(true);
    expect(h.tracesSampleRate).toBe(1.0);
  });
});
