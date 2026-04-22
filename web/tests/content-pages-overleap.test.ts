/**
 * Content Pages Tests — Overleap host variant
 *
 * Mirrors tests/content-pages.test.ts, but mocks `getBrand()` to return
 * OVERLEAP so we can assert the sitemap served from overleap.io contains
 * en-* and ja URLs and excludes zh-* ones.
 *
 * Kept in a dedicated file because vitest's top-level `vi.mock` + its module
 * cache make swapping brand mocks within a single file unreliable.
 */
import { describe, it, expect, vi } from 'vitest';

// Mock #velite with the same fixtures as the Kaitu variant — we only change
// which brand the sitemap thinks it's serving.
vi.mock('#velite', () => ({
  posts: [
    {
      title: '欢迎来到 Kaitu 博客',
      date: '2026-02-20T00:00:00.000Z',
      summary: '这是 Kaitu 博客的第一篇文章。',
      tags: ['公告', '入门'],
      draft: false,
      content: '<h1>欢迎来到 Kaitu 博客</h1><p>Content here.</p>',
      metadata: { readingTime: 1, wordCount: 78 },
      filePath: 'zh-CN/blog/hello-world',
      locale: 'zh-CN',
      slug: 'blog/hello-world',
    },
    {
      title: 'Welcome to the Kaitu Blog',
      date: '2026-02-20T00:00:00.000Z',
      summary: 'The first post on the Kaitu blog.',
      tags: ['announcement'],
      draft: false,
      content: '<h1>Welcome to the Kaitu Blog</h1><p>Content here.</p>',
      metadata: { readingTime: 1, wordCount: 78 },
      filePath: 'en-US/blog/hello-world',
      locale: 'en-US',
      slug: 'blog/hello-world',
    },
    {
      title: '入门指南',
      date: '2026-02-18T00:00:00.000Z',
      summary: '如何开始使用 Kaitu VPN。',
      tags: ['指南'],
      draft: false,
      content: '<h1>入门指南</h1><p>Content here.</p>',
      metadata: { readingTime: 2, wordCount: 150 },
      filePath: 'zh-CN/guides/getting-started',
      locale: 'zh-CN',
      slug: 'guides/getting-started',
    },
  ],
}));

vi.mock('@/i18n/routing', () => ({
  routing: {
    locales: ['zh-CN', 'en-US', 'en-GB', 'en-AU', 'zh-TW', 'zh-HK', 'ja'],
  },
}));

vi.mock('@payload-config', () => ({ default: {} }));
vi.mock('payload', () => ({
  getPayload: async () => ({
    find: async () => ({ docs: [] }),
  }),
}));

// Route sitemap through the OVERLEAP brand (overleap.io baseUrl, en-* and ja locales).
vi.mock('@/lib/brand-server', async () => {
  const actual = await vi.importActual<typeof import('../src/lib/brands')>('../src/lib/brands');
  return { getBrand: async () => actual.OVERLEAP };
});

describe('test_sitemap_overleap_host_serves_english', () => {
  it('overleap host serves en-* and ja blog URLs and excludes zh-*', async () => {
    const { default: sitemap } = await import('../src/app/sitemap');
    const entries = await sitemap();
    const urls = entries.map((e: { url: string }) => e.url);

    // Overleap host serves English and Japanese
    expect(urls).toContain('https://overleap.io/en-US/blog/hello-world');
    expect(urls).toContain('https://overleap.io/en-GB/blog/hello-world');
    expect(urls).toContain('https://overleap.io/en-AU/blog/hello-world');
    expect(urls).toContain('https://overleap.io/ja/blog/hello-world');

    // Overleap host must NOT serve zh-* URLs — those live on kaitu.io
    expect(urls).not.toContain('https://overleap.io/zh-CN/blog/hello-world');
    expect(urls).not.toContain('https://overleap.io/zh-TW/blog/hello-world');
    expect(urls).not.toContain('https://overleap.io/zh-HK/blog/hello-world');

    // No kaitu.io URLs leak through
    expect(urls.some((url: string) => url.includes('kaitu.io'))).toBe(false);
  });

  it('overleap host serves static pages under en-US', async () => {
    const { default: sitemap } = await import('../src/app/sitemap');
    const entries = await sitemap();
    const urls = entries.map((e: { url: string }) => e.url);

    expect(urls).toContain('https://overleap.io/en-US/install');
    expect(urls).toContain('https://overleap.io/en-US/purchase');
    expect(urls).toContain('https://overleap.io/ja/install');
  });
});
