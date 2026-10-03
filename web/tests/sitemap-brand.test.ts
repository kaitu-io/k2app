import { describe, it, expect, vi } from 'vitest';

// sitemap.ts → brand-server.ts → `server-only`, a side-effect module that
// throws outside RSC. Same stub as request-pathname.test.ts.
vi.mock('server-only', () => ({}));

// Velite posts: one kaitu-marked guide, a shared k2 doc, a k2 doc in a locale
// this site does not serve, and a post marked for the other brand — mirrors the
// mock pattern used by tests/content-pages.test.ts. 'guides/cn-guide' also
// exercises the category listing-page emission (guides is a registered category).
vi.mock('#velite', () => ({
  posts: [
    { slug: 'guides/cn-guide', locale: 'zh-CN', date: '2026-01-01', draft: false, brand: 'kaitu' },
    { slug: 'k2/protocol', locale: 'zh-CN', date: '2026-01-01', draft: false, brand: 'both' },
    { slug: 'k2/en-only', locale: 'en-US', date: '2026-01-01', draft: false, brand: 'both' },
    { slug: 'k2/other-brand', locale: 'zh-CN', date: '2026-01-01', draft: false, brand: 'overleap' },
  ],
}));

import sitemap from '../src/app/sitemap';

describe('sitemap', () => {
  it('only kaitu.io URLs under the served zh locales', async () => {
    const entries = await sitemap();
    expect(entries.length).toBeGreaterThan(0);
    for (const e of entries) {
      expect(e.url).toMatch(/^https:\/\/kaitu\.io/);
      expect(e.url).not.toMatch(/\/(en-US|en-GB|en-AU|ja)(\/|$)/);
    }
  });

  it('advertises its zh-CN content', async () => {
    const entries = await sitemap();
    expect(entries.some((e) => e.url.includes('/k2/protocol'))).toBe(true);
    expect(entries.some((e) => e.url.includes('/cn-guide'))).toBe(true);
  });

  it('a doc that exists only in an unserved locale is not advertised', async () => {
    // Slugs are harvested across all locales, so without the locale filter this
    // en-US-only doc would be emitted under a zh URL that 404s.
    const entries = await sitemap();
    expect(entries.some((e) => e.url.includes('/k2/en-only'))).toBe(false);
  });

  it('a post marked for the other brand is not advertised', async () => {
    const entries = await sitemap();
    expect(entries.some((e) => e.url.includes('/k2/other-brand'))).toBe(false);
  });

  it.each(['/guides', '/routers', '/releases', '/pricing'])('%s is present', async (route) => {
    const entries = await sitemap();
    expect(entries.some((e) => e.url.endsWith(route))).toBe(true);
  });
});
