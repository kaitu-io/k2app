/**
 * SEO/GEO Completion Tests — T7
 *
 * Vitest tests for sitemap k2/ page priority and robots.txt /k2/ allowance.
 * RED phase: tests fail before implementation.
 *
 * Tests verify:
 * 1. Sitemap includes k2/ content page URLs
 * 2. k2/ pages have priority 0.9
 * 3. Non-k2 content pages keep priority 0.6
 * 4. robots.txt does NOT disallow /k2/ path
 */
import { describe, it, expect, vi } from 'vitest';

// Mock #velite with posts that include k2/ prefixed slugs
vi.mock('#velite', () => ({
  posts: [
    { slug: 'k2/quickstart', locale: 'zh-CN', date: '2026-02-21', draft: false, title: 'Quickstart' },
    { slug: 'k2/protocol', locale: 'zh-CN', date: '2026-02-21', draft: false, title: 'Protocol' },
    { slug: 'k2/vs-hysteria2', locale: 'zh-CN', date: '2026-02-21', draft: false, title: 'vs Hysteria2' },
    { slug: 'blog/hello', locale: 'zh-CN', date: '2026-02-21', draft: false, title: 'Hello' },
  ],
}));

// Mock @/i18n/routing
vi.mock('@/i18n/routing', () => ({
  routing: { locales: ['zh-CN', 'en-US'] },
}));

describe('test_sitemap_includes_k2_pages', () => {
  it('sitemap result contains URLs with /k2/quickstart', async () => {
    const { default: sitemap } = await import('../src/app/sitemap');
    const entries = sitemap();
    const urls = entries.map((entry: { url: string }) => entry.url);

    expect(urls.some((url: string) => url.includes('/k2/quickstart'))).toBe(true);
  });

  it('sitemap result contains URLs with /k2/protocol', async () => {
    const { default: sitemap } = await import('../src/app/sitemap');
    const entries = sitemap();
    const urls = entries.map((entry: { url: string }) => entry.url);

    expect(urls.some((url: string) => url.includes('/k2/protocol'))).toBe(true);
  });

  it('sitemap result contains URLs with /k2/vs-hysteria2', async () => {
    const { default: sitemap } = await import('../src/app/sitemap');
    const entries = sitemap();
    const urls = entries.map((entry: { url: string }) => entry.url);

    expect(urls.some((url: string) => url.includes('/k2/vs-hysteria2'))).toBe(true);
  });
});

describe('test_sitemap_k2_priority', () => {
  it('k2/ page entries have priority 0.9', async () => {
    const { default: sitemap } = await import('../src/app/sitemap');
    const entries = sitemap();

    const k2Entries = entries.filter((entry: { url: string; priority?: number }) =>
      entry.url.includes('/k2/')
    );

    expect(k2Entries.length).toBeGreaterThan(0);
    k2Entries.forEach((entry: { url: string; priority?: number }) => {
      expect(entry.priority).toBe(0.9);
    });
  });

  it('k2/ entries have changeFrequency weekly', async () => {
    const { default: sitemap } = await import('../src/app/sitemap');
    const entries = sitemap();

    const k2Entries = entries.filter((entry: { url: string; changeFrequency?: string }) =>
      entry.url.includes('/k2/')
    );

    expect(k2Entries.length).toBeGreaterThan(0);
    k2Entries.forEach((entry: { url: string; changeFrequency?: string }) => {
      expect(entry.changeFrequency).toBe('weekly');
    });
  });
});

describe('test_sitemap_non_k2_default_priority', () => {
  it('non-k2 content pages (blog/hello) keep priority 0.6', async () => {
    const { default: sitemap } = await import('../src/app/sitemap');
    const entries = sitemap();

    const blogEntries = entries.filter((entry: { url: string; priority?: number }) =>
      entry.url.includes('/blog/hello')
    );

    expect(blogEntries.length).toBeGreaterThan(0);
    blogEntries.forEach((entry: { url: string; priority?: number }) => {
      expect(entry.priority).toBe(0.6);
    });
  });

  it('non-k2 content pages are still included in sitemap', async () => {
    const { default: sitemap } = await import('../src/app/sitemap');
    const entries = sitemap();
    const urls = entries.map((entry: { url: string }) => entry.url);

    expect(urls.some((url: string) => url.includes('/blog/hello'))).toBe(true);
  });
});

describe('test_robots_allows_k2', () => {
  it('robots disallow array does NOT contain any /k2 pattern', async () => {
    const { default: robots } = await import('../src/app/robots');
    const result = robots();

    const rules = Array.isArray(result.rules) ? result.rules : [result.rules];

    rules.forEach((rule: { disallow?: string | string[] }) => {
      if (!rule.disallow) return;

      const disallowList = Array.isArray(rule.disallow) ? rule.disallow : [rule.disallow];
      const hasK2Disallow = disallowList.some((path: string) =>
        path.includes('/k2') || path.includes('k2/')
      );

      expect(hasK2Disallow).toBe(false);
    });
  });

  it('robots allows root path which covers /k2/', async () => {
    const { default: robots } = await import('../src/app/robots');
    const result = robots();

    const rules = Array.isArray(result.rules) ? result.rules : [result.rules];
    const hasRootAllow = rules.some((rule: { allow?: string | string[] }) => {
      if (!rule.allow) return false;
      const allowList = Array.isArray(rule.allow) ? rule.allow : [rule.allow];
      return allowList.includes('/');
    });

    expect(hasRootAllow).toBe(true);
  });
});
