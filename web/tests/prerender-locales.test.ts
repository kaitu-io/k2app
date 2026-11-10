/**
 * Build-output budget: content pages are prerendered only in locales that show
 * their own text, or read the zh-CN fallback's language. Prerendering every
 * Chinese post in en-US/ko/ja/ru blew the Amplify 220 MiB cap (job 145).
 */
import { describe, expect, it, vi } from 'vitest';

vi.mock('server-only', () => ({}));
vi.mock('@/lib/brand-server', () => ({ getBrand: vi.fn() }));
vi.mock('@/i18n/routing', () => ({ routing: { locales: [] }, Link: () => null }));
vi.mock('next-intl/server', () => ({ getTranslations: vi.fn(), setRequestLocale: vi.fn() }));
vi.mock('next-intl', () => ({ useTranslations: () => (k: string) => k, useLocale: () => 'zh-CN' }));
vi.mock('next/navigation', () => ({ notFound: vi.fn(), usePathname: vi.fn() }));
vi.mock('@/components/Header', () => ({ default: () => null }));
vi.mock('@/components/Footer', () => ({ default: () => null }));
vi.mock('@/components/Breadcrumb', () => ({ default: () => null }));

import { posts } from '#velite';
import { KAITU } from '../src/lib/brands';
import { prerenderLocales } from '../src/lib/prerender-locales';
import { generateStaticParams as contentParams } from '../src/app/[locale]/[...slug]/page';
import { generateStaticParams as k2Params } from '../src/app/[locale]/k2/[[...path]]/page';

const FOREIGN = KAITU.allowedLocales.filter((l) => !l.startsWith('zh'));

describe('prerenderLocales', () => {
  it('a zh-CN-only post: the three Chinese locales, no foreign fallback', () => {
    expect(prerenderLocales(['zh-CN'], KAITU)).toEqual(['zh-CN', 'zh-TW', 'zh-HK']);
  });

  it('a post with its own English version adds en-US only', () => {
    expect(prerenderLocales(['zh-CN', 'en-US'], KAITU)).toEqual(['zh-CN', 'zh-TW', 'zh-HK', 'en-US']);
  });

  it('a foreign-only post: just its own locale (no zh fallback exists)', () => {
    expect(prerenderLocales(['ko'], KAITU)).toEqual(['ko']);
  });
});

describe('content routes use it', () => {
  type P = { slug: string; locale: string; draft?: boolean };
  const own = new Set((posts as P[]).filter((p) => !p.draft).map((p) => `${p.locale}::${p.slug}`));

  it('[...slug]: a foreign locale is prerendered only for posts it has its own version of', () => {
    const params = contentParams();
    for (const p of params) {
      if (p.slug.length < 2 || !FOREIGN.includes(p.locale as never)) continue;
      expect(own.has(`${p.locale}::${p.slug.join('/')}`), `${p.locale}/${p.slug.join('/')}`).toBe(true);
    }
    // Chinese fallbacks are still prerendered, and category lists in every locale.
    expect(params.some((p) => p.locale === 'zh-TW' && p.slug.length === 2)).toBe(true);
    for (const l of FOREIGN) expect(params.some((p) => p.locale === l && p.slug.length === 1)).toBe(true);
  });

  it('k2: a foreign locale is prerendered only for docs it has its own version of', () => {
    for (const p of k2Params()) {
      if (!p.path || !FOREIGN.includes(p.locale as never)) continue;
      expect(own.has(`${p.locale}::k2/${p.path.join('/')}`), `${p.locale}/k2/${p.path.join('/')}`).toBe(true);
    }
  });
});
