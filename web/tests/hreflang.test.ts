/**
 * hreflang: one map per page, shared by the page's <link rel="alternate"> tags
 * and its sitemap entry. These pin the codes and x-default choice; sitemap-brand
 * and metadata tests pin that both consumers actually use this function.
 */
import { describe, expect, it, vi } from 'vitest';

vi.mock('server-only', () => ({}));

import { KAITU } from '../src/lib/brands';
import { hreflangAlternates, hreflangCode, staticRouteLocales, xDefaultLocale } from '../src/lib/hreflang';
import { siteConfig } from '../src/lib/site';

const B = 'https://www.kaitu.io';

describe('hreflangAlternates', () => {
  it('a page in every language: language codes, plain zh → zh-CN, x-default → English', () => {
    const m = hreflangAlternates(KAITU.allowedLocales, '/pricing', KAITU);
    expect(Object.keys(m).sort()).toEqual(['en', 'ja', 'ko', 'ru', 'x-default', 'zh', 'zh-cn', 'zh-hk', 'zh-tw']);
    expect(m['en']).toBe(`${B}/en-US/pricing`);
    expect(m['zh']).toBe(`${B}/zh-CN/pricing`);
    expect(m['x-default']).toBe(`${B}/en-US/pricing`);
  });

  it('a Chinese-only page falls back to zh-CN for x-default', () => {
    const m = hreflangAlternates(['zh-CN', 'zh-TW'], '/guides/x', KAITU);
    expect(Object.keys(m).sort()).toEqual(['x-default', 'zh', 'zh-cn', 'zh-tw']);
    expect(m['x-default']).toBe(`${B}/zh-CN/guides/x`);
  });

  it('a foreign-only page (e.g. an English guide) has no zh entry and points x-default at itself', () => {
    const m = hreflangAlternates(['en-US'], '/guides/vpn-in-china', KAITU);
    expect(m).toEqual({ en: `${B}/en-US/guides/vpn-in-china`, 'x-default': `${B}/en-US/guides/vpn-in-china` });
  });

  it('a Korean-only page uses itself as x-default', () => {
    expect(xDefaultLocale(['ko'], KAITU)).toBe('ko');
  });

  it('codes', () => {
    expect(hreflangCode('en-US')).toBe('en');
    expect(hreflangCode('zh-HK')).toBe('zh-hk');
    expect(hreflangCode('ru')).toBe('ru');
  });
});

describe('staticRouteLocales', () => {
  it('/releases (Chinese-only notes) is served in the zh locales only', () => {
    expect(siteConfig().chineseOnlyRoutes).toContain('/releases');
    expect(staticRouteLocales('/releases', KAITU, siteConfig().chineseOnlyRoutes)).toEqual(['zh-CN', 'zh-TW', 'zh-HK']);
  });

  it('every other static route is served in all seven languages', () => {
    expect(staticRouteLocales('/pricing', KAITU, siteConfig().chineseOnlyRoutes)).toEqual([...KAITU.allowedLocales]);
  });

  it('chineseOnlyRoutes only names real static routes (a typo would silently do nothing)', () => {
    for (const r of siteConfig().chineseOnlyRoutes) expect(siteConfig().staticRoutes).toContain(r);
  });
});
