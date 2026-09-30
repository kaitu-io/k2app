import { describe, it, expect } from 'vitest';
import { NextRequest } from 'next/server';
import middleware, { negotiateLocale } from '@/middleware';

const req = (path: string, headers: Record<string, string> = {}) =>
  new NextRequest(new URL(path, 'https://overleap.io'), { headers });

describe('negotiateLocale', () => {
  it.each([
    [null, 'en-GB'],
    ['en', 'en-GB'],
    ['en-IE,en;q=0.8', 'en-GB'],
    ['en-US,en;q=0.9', 'en-US'],
    ['en-au', 'en-AU'],
    ['ja-JP,ja;q=0.9', 'ja'],
    ['ja-JP', 'ja'],
    ['fr-FR,de;q=0.8', 'en-GB'],
    ['fr;q=1,ja;q=0.5', 'ja'],
  ])('%s → %s', (header, expected) => {
    expect(negotiateLocale(header)).toBe(expected);
  });
});

describe('middleware', () => {
  it('/ redirects to the negotiated locale and is never shared-cached', () => {
    const res = middleware(req('/', { 'accept-language': 'en-US' }));
    expect(res.status).toBe(307);
    expect(res.headers.get('location')).toBe('https://overleap.io/en-US');
    expect(res.headers.get('cache-control')).toContain('no-store');
  });

  it('/ honours the preferredLocale cookie', () => {
    const res = middleware(req('/', { cookie: 'preferredLocale=ja', 'accept-language': 'en-US' }));
    expect(res.headers.get('location')).toBe('https://overleap.io/ja');
  });

  it('a locale we do not serve 301s to the master, keeping path and query', () => {
    const res = middleware(req('/zh-CN/privacy?x=1'));
    expect(res.status).toBe(301);
    expect(res.headers.get('location')).toBe('https://overleap.io/en-GB/privacy?x=1');
  });

  it('/api/* is passed through with X-K2-Brand', () => {
    const res = middleware(req('/api/plans'));
    expect(res.headers.get('x-middleware-request-x-k2-brand')).toBe('overleap');
  });

  it('/app/* (admin API) is not served', () => {
    expect(middleware(req('/app/users')).status).toBe(404);
  });
});
