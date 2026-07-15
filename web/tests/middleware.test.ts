/**
 * Middleware single-brand routing tests — Brand Split Phase 2.
 *
 * The brand is baked via NEXT_PUBLIC_BRAND (siteBrand() reads it per call, so
 * vi.stubEnv is enough — no module reset needed). There is NO cross-domain
 * behavior anymore: hosts are irrelevant, redirects stay on the same origin.
 */
import { describe, it, expect, vi, afterEach } from 'vitest';
import { NextRequest } from 'next/server';

vi.mock('next-intl/middleware', () => ({
  default: () => () => new Response(null, { status: 200 }),
}));
vi.mock('../src/i18n/routing', () => ({
  routing: {
    locales: ['en-US', 'en-GB', 'en-AU', 'zh-CN', 'zh-TW', 'zh-HK', 'ja'],
    defaultLocale: 'zh-CN',
  },
}));

import middleware from '../src/middleware';

function makeRequest(
  path: string,
  extra: { acceptLanguage?: string; cookie?: string; search?: string } = {},
): NextRequest {
  const url = `https://example.test${path}${extra.search ?? ''}`;
  const headers: Record<string, string> = { host: 'example.test' };
  if (extra.acceptLanguage) headers['accept-language'] = extra.acceptLanguage;
  if (extra.cookie) headers['cookie'] = extra.cookie;
  return new NextRequest(url, { headers });
}

async function run(req: NextRequest): Promise<Response> {
  const res = await middleware(req);
  return res ?? new Response(null, { status: 200 });
}

afterEach(() => vi.unstubAllEnvs());

describe('off-brand locale → same-host 301 to brand default locale', () => {
  it('kaitu build: /en-US/install → 301 /zh-CN/install (same host)', async () => {
    vi.stubEnv('NEXT_PUBLIC_BRAND', 'kaitu');
    const res = await run(makeRequest('/en-US/install'));
    expect(res.status).toBe(301);
    expect(res.headers.get('location')).toBe('https://example.test/zh-CN/install');
  });
  it('kaitu build: /ja → 301 /zh-CN', async () => {
    vi.stubEnv('NEXT_PUBLIC_BRAND', 'kaitu');
    const res = await run(makeRequest('/ja'));
    expect(res.status).toBe(301);
    expect(res.headers.get('location')).toBe('https://example.test/zh-CN');
  });
  it('overleap build: /zh-CN/purchase?ref=x → 301 /en-US/purchase?ref=x', async () => {
    vi.stubEnv('NEXT_PUBLIC_BRAND', 'overleap');
    const res = await run(makeRequest('/zh-CN/purchase', { search: '?ref=x' }));
    expect(res.status).toBe(301);
    expect(res.headers.get('location')).toBe('https://example.test/en-US/purchase?ref=x');
  });
  it('own-brand locales pass through (kaitu zh-TW, overleap ja)', async () => {
    vi.stubEnv('NEXT_PUBLIC_BRAND', 'kaitu');
    expect((await run(makeRequest('/zh-TW/install'))).status).not.toBe(301);
    vi.stubEnv('NEXT_PUBLIC_BRAND', 'overleap');
    expect((await run(makeRequest('/ja/k2'))).status).not.toBe(301);
  });
});

describe('X-K2-Brand injection on /api and /app', () => {
  it('kaitu: /api/plans passes through with X-K2-Brand=kaitu on the downstream request', async () => {
    vi.stubEnv('NEXT_PUBLIC_BRAND', 'kaitu');
    const res = await run(makeRequest('/api/plans'));
    expect(res.status).toBe(200);
    // NextResponse.next({request:{headers}}) surfaces overrides via x-middleware-request-* headers.
    expect(res.headers.get('x-middleware-request-x-k2-brand')).toBe('kaitu');
  });
  it('overleap: /api/plans carries X-K2-Brand=overleap', async () => {
    vi.stubEnv('NEXT_PUBLIC_BRAND', 'overleap');
    const res = await run(makeRequest('/api/plans'));
    expect(res.headers.get('x-middleware-request-x-k2-brand')).toBe('overleap');
  });
  it('overleap: /app/* admin API proxy is 404', async () => {
    vi.stubEnv('NEXT_PUBLIC_BRAND', 'overleap');
    expect((await run(makeRequest('/app/users'))).status).toBe(404);
  });
  it('kaitu: /app/* passes through with the brand header', async () => {
    vi.stubEnv('NEXT_PUBLIC_BRAND', 'kaitu');
    const res = await run(makeRequest('/app/users'));
    expect(res.status).toBe(200);
    expect(res.headers.get('x-middleware-request-x-k2-brand')).toBe('kaitu');
  });
});

describe('admin + install-script surfaces are kaitu-only', () => {
  it.each(['/manager', '/manager/users', '/payload/api/posts', '/admin', '/i/k2', '/i/k2s', '/i/k2r'])(
    'overleap: %s → 404',
    async (path) => {
      vi.stubEnv('NEXT_PUBLIC_BRAND', 'overleap');
      expect((await run(makeRequest(path))).status).toBe(404);
    },
  );
  it.each(['/manager/users', '/payload/api/posts', '/i/k2'])(
    'kaitu: %s passes through',
    async (path) => {
      vi.stubEnv('NEXT_PUBLIC_BRAND', 'kaitu');
      expect((await run(makeRequest(path))).status).toBe(200);
    },
  );
});

describe('favicon rewrite', () => {
  it('overleap: /favicon.ico rewrites to the brand favicon', async () => {
    vi.stubEnv('NEXT_PUBLIC_BRAND', 'overleap');
    const res = await run(makeRequest('/favicon.ico'));
    expect(res.headers.get('x-middleware-rewrite')).toBe(
      'https://example.test/brand/overleap/favicon-32x32.png',
    );
  });
  it('kaitu: /favicon.ico untouched (legacy root file)', async () => {
    vi.stubEnv('NEXT_PUBLIC_BRAND', 'kaitu');
    const res = await run(makeRequest('/favicon.ico'));
    expect(res.headers.get('x-middleware-rewrite')).toBeNull();
  });
});

describe('root path locale pick (brand-constrained)', () => {
  it('overleap: / → redirect to /en-US with private cache-control', async () => {
    vi.stubEnv('NEXT_PUBLIC_BRAND', 'overleap');
    const res = await run(makeRequest('/'));
    expect([302, 307]).toContain(res.status);
    expect(res.headers.get('location')).toBe('https://example.test/en-US');
    expect(res.headers.get('cache-control')).toContain('no-store');
  });
  it('overleap: / with Accept-Language ja → /ja', async () => {
    vi.stubEnv('NEXT_PUBLIC_BRAND', 'overleap');
    const res = await run(makeRequest('/', { acceptLanguage: 'ja,en;q=0.5' }));
    expect(res.headers.get('location')).toBe('https://example.test/ja');
  });
  it('overleap: preferredLocale cookie honored only when brand-allowed', async () => {
    vi.stubEnv('NEXT_PUBLIC_BRAND', 'overleap');
    const stale = await run(makeRequest('/', { cookie: 'preferredLocale=zh-CN' }));
    expect(stale.headers.get('location')).toBe('https://example.test/en-US');
    const ok = await run(makeRequest('/', { cookie: 'preferredLocale=en-GB' }));
    expect(ok.headers.get('location')).toBe('https://example.test/en-GB');
  });
  it('kaitu: / with Accept-Language zh-TW → /zh-TW', async () => {
    vi.stubEnv('NEXT_PUBLIC_BRAND', 'kaitu');
    const res = await run(makeRequest('/', { acceptLanguage: 'zh-TW' }));
    expect(res.headers.get('location')).toBe('https://example.test/zh-TW');
  });
});

describe('x-pathname injection for downstream RSC (unchanged behavior)', () => {
  it('kaitu: /zh-CN/install → x-middleware-request-x-pathname=/install', async () => {
    vi.stubEnv('NEXT_PUBLIC_BRAND', 'kaitu');
    const res = await run(makeRequest('/zh-CN/install'));
    expect(res.headers.get('x-middleware-request-x-pathname')).toBe('/install');
  });
  it('kaitu: /zh-CN → x-pathname is "/"', async () => {
    vi.stubEnv('NEXT_PUBLIC_BRAND', 'kaitu');
    const res = await run(makeRequest('/zh-CN'));
    expect(res.headers.get('x-middleware-request-x-pathname')).toBe('/');
  });
});
