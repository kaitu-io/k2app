/**
 * Locale-fallback SEO: a zh-TW/zh-HK URL that renders the zh-CN text must not
 * compete with it — canonical → a locale that has the content, noindex,follow,
 * and hreflang lists only real versions.
 */
import { describe, it, expect, vi } from 'vitest';

vi.mock('@/i18n/routing', () => ({
  routing: { locales: ['zh-CN', 'zh-TW', 'zh-HK'] },
}));
import { generateMetadata, localeSeo } from '../src/app/[locale]/metadata';
import { siteBrand } from '../src/lib/brands';

const brand = siteBrand();
const base = brand.baseUrl;

describe('localeSeo', () => {
  it('no availableLocales = every brand locale, no fallback', () => {
    expect(localeSeo('zh-TW', brand)).toEqual({
      served: [...brand.allowedLocales],
      canonicalLocale: 'zh-TW',
      isFallback: false,
    });
  });

  it('a locale without its own version canonicalises to the default locale', () => {
    expect(localeSeo('zh-HK', brand, ['zh-CN'])).toEqual({
      served: ['zh-CN'],
      canonicalLocale: 'zh-CN',
      isFallback: true,
    });
  });

  it('default locale missing → first served locale', () => {
    expect(localeSeo('zh-HK', brand, ['zh-TW']).canonicalLocale).toBe('zh-TW');
  });

  it('empty availability keeps the plain behaviour (page would 404 anyway)', () => {
    expect(localeSeo('zh-TW', brand, []).isFallback).toBe(false);
  });
});

describe('generateMetadata with availableLocales', () => {
  const path = '/guides/x';

  it('fallback page: canonical → zh-CN, noindex,follow, hreflang only zh-CN', () => {
    const meta = generateMetadata('zh-TW', path, { availableLocales: ['zh-CN'] }, brand);
    expect(meta.alternates?.canonical).toBe(`${base}/zh-CN${path}`);
    expect(meta.robots).toEqual({ index: false, follow: true });
    expect(meta.alternates?.languages).toEqual({
      'zh-cn': `${base}/zh-CN${path}`,
      'x-default': `${base}/zh-CN${path}`,
    });
  });

  it('own version: self canonical, indexable, hreflang lists real versions', () => {
    const meta = generateMetadata('zh-TW', path, { availableLocales: ['zh-CN', 'zh-TW'] }, brand);
    expect(meta.alternates?.canonical).toBe(`${base}/zh-TW${path}`);
    expect(meta.robots).toBeUndefined();
    expect(Object.keys(meta.alternates?.languages ?? {}).sort()).toEqual(['x-default', 'zh-cn', 'zh-tw']);
  });

  it('UI pages (no availableLocales) are unchanged', () => {
    const meta = generateMetadata('zh-HK', '/pricing', {}, brand);
    expect(meta.alternates?.canonical).toBe(`${base}/zh-HK/pricing`);
    expect(meta.robots).toBeUndefined();
    expect(Object.keys(meta.alternates?.languages ?? {})).toHaveLength(brand.allowedLocales.length + 1);
  });
});
