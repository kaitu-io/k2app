import { describe, it, expect, vi, afterEach } from 'vitest';

// sitemap.ts → brand-server.ts → `server-only`, a side-effect module that
// throws outside RSC. Same stub as request-pathname.test.ts.
vi.mock('server-only', () => ({}));

// Velite posts: one kaitu-only, one shared across both locales, and one shared
// but authored in zh-CN only — mirrors the mock pattern used by
// tests/content-pages.test.ts. 'guides/cn-guide' also exercises the category
// listing-page emission (guides is a registered category in content-posts.ts).
vi.mock('#velite', () => ({
  posts: [
    { slug: 'guides/cn-guide', locale: 'zh-CN', date: '2026-01-01', draft: false, brand: 'kaitu' },
    { slug: 'k2/protocol', locale: 'zh-CN', date: '2026-01-01', draft: false, brand: 'both' },
    { slug: 'k2/protocol', locale: 'en-US', date: '2026-01-01', draft: false, brand: 'both' },
    // brand: both, but no en-US/ja copy exists. Overleap 404s it (the k2 route
    // falls back to the BRAND's default locale, never to zh-CN), so overleap
    // must not advertise it either.
    { slug: 'k2/zh-only', locale: 'zh-CN', date: '2026-01-01', draft: false, brand: 'both' },
  ],
}));

import sitemap from '../src/app/sitemap';

afterEach(() => {
  vi.unstubAllEnvs();
});

describe('sitemap — baked brand isolation', () => {
  it('overleap build: only overleap.io URLs, only overleap locales, no kaitu-only content', async () => {
    vi.stubEnv('NEXT_PUBLIC_BRAND', 'overleap');
    const entries = await sitemap();
    expect(entries.length).toBeGreaterThan(0);
    for (const e of entries) {
      expect(e.url).toMatch(/^https:\/\/overleap\.io/);
      expect(e.url).not.toMatch(/zh-CN|zh-TW|zh-HK/);
    }
    expect(entries.some((e) => e.url.includes('/cn-guide'))).toBe(false);
    expect(entries.some((e) => e.url.includes('/k2/protocol'))).toBe(true);
  });

  it('overleap build: a brand: both doc with no overleap-locale copy is not advertised', async () => {
    // Slugs are harvested across all locales, so without a locale filter this
    // zh-CN-only doc would be emitted as https://overleap.io/en-US/k2/zh-only —
    // a sitemap entry pointing at a 404.
    vi.stubEnv('NEXT_PUBLIC_BRAND', 'overleap');
    const entries = await sitemap();
    expect(entries.some((e) => e.url.includes('/k2/zh-only'))).toBe(false);
  });

  it('kaitu build: still advertises its zh-CN content', async () => {
    vi.stubEnv('NEXT_PUBLIC_BRAND', 'kaitu');
    const entries = await sitemap();
    expect(entries.some((e) => e.url.includes('/k2/zh-only'))).toBe(true);
    expect(entries.some((e) => e.url.includes('/cn-guide'))).toBe(true);
  });

  it('overleap build: /guides listing page is absent (its only posts are kaitu-only)', async () => {
    vi.stubEnv('NEXT_PUBLIC_BRAND', 'overleap');
    const entries = await sitemap();
    expect(entries.some((e) => e.url.endsWith('/guides'))).toBe(false);
  });

  it('kaitu build: /guides listing page is present', async () => {
    vi.stubEnv('NEXT_PUBLIC_BRAND', 'kaitu');
    const entries = await sitemap();
    expect(entries.some((e) => e.url.endsWith('/guides'))).toBe(true);
  });

  // Feature-gated surfaces must not be advertised by a brand that 404s them.
  it('overleap build: /routers is absent (routers is a kaitu-only feature)', async () => {
    vi.stubEnv('NEXT_PUBLIC_BRAND', 'overleap');
    const entries = await sitemap();
    expect(entries.some((e) => e.url.endsWith('/routers'))).toBe(false);
  });

  it('kaitu build: /routers is present', async () => {
    vi.stubEnv('NEXT_PUBLIC_BRAND', 'kaitu');
    const entries = await sitemap();
    expect(entries.some((e) => e.url.endsWith('/routers'))).toBe(true);
  });

  it('overleap build: /releases is absent (release notes are a kaitu-only feature)', async () => {
    vi.stubEnv('NEXT_PUBLIC_BRAND', 'overleap');
    const entries = await sitemap();
    expect(entries.some((e) => e.url.endsWith('/releases'))).toBe(false);
  });

  it('kaitu build: /releases is present', async () => {
    vi.stubEnv('NEXT_PUBLIC_BRAND', 'kaitu');
    const entries = await sitemap();
    expect(entries.some((e) => e.url.endsWith('/releases'))).toBe(true);
  });

  it('kaitu build: only kaitu.io URLs', async () => {
    vi.stubEnv('NEXT_PUBLIC_BRAND', 'kaitu');
    const entries = await sitemap();
    for (const e of entries) {
      expect(e.url).toMatch(/^https:\/\/kaitu\.io/);
    }
  });
});
