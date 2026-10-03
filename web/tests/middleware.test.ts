/**
 * Middleware routing tests. The site serves kaitu.io only (zh-CN / zh-TW /
 * zh-HK); the en-* / ja locales left with overleap.io (sites/overleap/), and
 * their old URLs must keep resolving on this host via a 301 to zh-CN.
 */
import { describe, it, expect, vi } from 'vitest';
import { NextRequest, NextResponse } from 'next/server';

vi.mock('next-intl/middleware', () => ({
  default: () => () => new Response(null, { status: 200 }),
}));
vi.mock('../src/i18n/routing', () => ({
  routing: {
    locales: ['zh-CN', 'zh-TW', 'zh-HK'],
    defaultLocale: 'zh-CN',
  },
}));

import middleware, { config } from '../src/middleware';

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

describe('legacy en-* / ja URLs → same-host 301 to zh-CN', () => {
  it.each([
    ['/en-US/install', 'https://example.test/zh-CN/install'],
    ['/en-GB/k2/vs-hysteria2', 'https://example.test/zh-CN/k2/vs-hysteria2'],
    ['/en-AU/purchase', 'https://example.test/zh-CN/purchase'],
    ['/ja', 'https://example.test/zh-CN'],
    ['/ja/support', 'https://example.test/zh-CN/support'],
  ])('%s → 301 %s', async (path, location) => {
    const res = await run(makeRequest(path));
    expect(res.status).toBe(301);
    expect(res.headers.get('location')).toBe(location);
  });

  it('keeps the query string', async () => {
    const res = await run(makeRequest('/en-US/purchase', { search: '?ref=x' }));
    expect(res.status).toBe(301);
    expect(res.headers.get('location')).toBe('https://example.test/zh-CN/purchase?ref=x');
  });

  it('served locales pass through', async () => {
    expect((await run(makeRequest('/zh-TW/install'))).status).not.toBe(301);
    expect((await run(makeRequest('/zh-HK'))).status).not.toBe(301);
  });

  it('a path that merely starts with a legacy code is not a locale prefix', async () => {
    expect((await run(makeRequest('/japan'))).status).not.toBe(301);
  });

  it('the matcher still routes legacy prefixes (incl. dotted paths) through the middleware', () => {
    expect(config.matcher).toContain('/(en-GB|en-US|en-AU|ja)/:path*');
  });
});

describe('X-K2-Brand injection on /api and /app', () => {
  it('/api/plans passes through with X-K2-Brand=kaitu on the downstream request', async () => {
    const res = await run(makeRequest('/api/plans'));
    expect(res.status).toBe(200);
    // NextResponse.next({request:{headers}}) surfaces overrides via x-middleware-request-* headers.
    expect(res.headers.get('x-middleware-request-x-k2-brand')).toBe('kaitu');
  });
  it('/app/* (admin API) passes through with the brand header', async () => {
    const res = await run(makeRequest('/app/users'));
    expect(res.status).toBe(200);
    expect(res.headers.get('x-middleware-request-x-k2-brand')).toBe('kaitu');
  });
});

describe('admin + install-script surfaces pass through', () => {
  it.each(['/manager', '/manager/users', '/i/k2', '/i/k2s', '/i/k2r'])('%s passes through', async (path) => {
    if (path !== '/i/k2') vi.stubGlobal('fetch', vi.fn(() => Promise.resolve(new Response(null))));
    const res = await run(makeRequest(path));
    expect(res.status).toBe(200);
    expect(res.headers.get('location')).toBeNull();
    vi.unstubAllGlobals();
  });
});

describe('favicon', () => {
  it('/favicon.ico untouched (root file)', async () => {
    const res = await run(makeRequest('/favicon.ico'));
    expect(res.headers.get('x-middleware-rewrite')).toBeNull();
  });
  // Regression test for a real bug the mocked next-intl/middleware above
  // cannot catch: /favicon.ico must short-circuit before falling through to
  // intlMiddleware, or next-intl's default localePrefix:'always' redirects
  // /favicon.ico to /zh-CN/favicon.ico (no matching route → dead page, not the
  // icon). Verified against the real next-intl middleware, not the module-level mock.
  it('/favicon.ico does not fall through to intlMiddleware', async () => {
    // The describe-level mock always returns 200, which can't distinguish
    // "short-circuited before intlMiddleware" from "reached it and it happened
    // to return 200". This override simulates the real redirect so the test
    // can tell the two cases apart.
    vi.doMock('next-intl/middleware', () => ({
      default: () => (req: NextRequest) =>
        NextResponse.redirect(new URL(`/zh-CN${req.nextUrl.pathname}`, req.url), 307),
    }));
    vi.resetModules();
    const { default: mw } = await import('../src/middleware');
    const res = await mw(makeRequest('/favicon.ico'));
    expect(res?.status).not.toBe(307);
    expect(res?.headers.get('location')).toBeNull();
    vi.doUnmock('next-intl/middleware');
  });
});

describe('root path locale pick', () => {
  it('/ → redirect to /zh-CN with private cache-control', async () => {
    const res = await run(makeRequest('/'));
    expect([302, 307]).toContain(res.status);
    expect(res.headers.get('location')).toBe('https://example.test/zh-CN');
    expect(res.headers.get('cache-control')).toContain('no-store');
  });
  it.each([
    ['zh-TW', 'zh-TW'],
    ['zh-HK', 'zh-HK'],
    ['zh-MO', 'zh-HK'],
    ['zh-SG', 'zh-CN'],
    ['zh', 'zh-CN'],
    ['en-US,en;q=0.9', 'zh-CN'],
    ['ja,en;q=0.5', 'zh-CN'],
    ['en-GB,zh-TW;q=0.5', 'zh-TW'],
  ])('Accept-Language %s → /%s', async (acceptLanguage, locale) => {
    const res = await run(makeRequest('/', { acceptLanguage }));
    expect(res.headers.get('location')).toBe(`https://example.test/${locale}`);
  });
  it('preferredLocale cookie honored only when it is a served locale', async () => {
    const stale = await run(makeRequest('/', { cookie: 'preferredLocale=en-GB' }));
    expect(stale.headers.get('location')).toBe('https://example.test/zh-CN');
    const ok = await run(makeRequest('/', { cookie: 'preferredLocale=zh-HK' }));
    expect(ok.headers.get('location')).toBe('https://example.test/zh-HK');
  });
});

describe('x-pathname injection for downstream RSC', () => {
  it('/zh-CN/install → x-middleware-request-x-pathname=/install', async () => {
    const res = await run(makeRequest('/zh-CN/install'));
    expect(res.headers.get('x-middleware-request-x-pathname')).toBe('/install');
  });
  it('/zh-CN → x-pathname is "/"', async () => {
    const res = await run(makeRequest('/zh-CN'));
    expect(res.headers.get('x-middleware-request-x-pathname')).toBe('/');
  });
});
