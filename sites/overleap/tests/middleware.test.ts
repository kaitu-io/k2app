import { describe, it, expect } from 'vitest';
import { NextRequest } from 'next/server';
import middleware, { negotiateLocale } from '@/middleware';
import { DEFAULT_LOCALE, LOCALES, LOCALE_META, filterLocales } from '@/lib/site';

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
    ['fr-FR,de;q=0.8', 'fr'],
    ['nl;q=1,ja;q=0.5', 'ja'],
    ['nl-NL,sw;q=0.8', 'en-GB'],
    ['zh-CN,zh;q=0.9', 'en-GB'],
    ['zh-CN,zh;q=0.9,ko;q=0.8', 'ko'],
    ['pt-PT,pt;q=0.9', 'pt-BR'],
    ['pt-br', 'pt-BR'],
    ['es-MX,es;q=0.9,en;q=0.8', 'es'],
    ['ar-EG', 'ar'],
    ['fa-IR,en;q=0.5', 'fa'],
    ['my-MM', 'my'],
    ['in-ID', 'id'],
    ['en-NZ,fr;q=0.9', 'en-GB'],
    ['de;q=0.5,it;q=0.9', 'it'],
    ['fr;q=0,de', 'de'],
    ['*', 'en-GB'],
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

describe('language registry', () => {
  it('every served locale has picker metadata, and nothing else does', () => {
    expect(Object.keys(LOCALE_META).sort()).toEqual([...LOCALES].sort());
  });

  it('the default locale is served and listed first', () => {
    expect(LOCALES[0]).toBe(DEFAULT_LOCALE);
  });

  it.each([
    ['korean', ['ko']],
    ['한국', ['ko']],
    ['KO', ['ko']],
    ['english', ['en-GB', 'en-US', 'en-AU']],
    ['portug', ['pt-BR']],
    ['فارسی', ['fa']],
    ['  ', [...LOCALES]],
    ['klingon', []],
  ])('filterLocales(%j)', (query, expected) => {
    expect(filterLocales(query)).toEqual(expected);
  });
});
