/**
 * Content Pages Tests — T2
 *
 * Vitest tests for sitemap content page integration.
 * RED phase: tests fail before implementation.
 */
import { describe, it, expect, vi } from 'vitest';

// Mock #velite module with sample posts
vi.mock('#velite', () => ({
  posts: [
    {
      title: '欢迎来到 Kaitu 博客',
      date: '2026-02-20T00:00:00.000Z',
      summary: '这是 Kaitu 博客的第一篇文章，介绍我们的 VPN 服务和内容发布系统。',
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
      title: 'Draft Post',
      date: '2026-02-20T00:00:00.000Z',
      summary: 'This is a draft.',
      tags: [],
      draft: true,
      content: '<p>Draft content.</p>',
      metadata: { readingTime: 1, wordCount: 10 },
      filePath: 'zh-CN/blog/draft-post',
      locale: 'zh-CN',
      slug: 'blog/draft-post',
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

// Mock @/i18n/routing
vi.mock('@/i18n/routing', () => ({
  routing: {
    locales: ['zh-CN', 'en-US', 'en-GB', 'en-AU', 'zh-TW', 'zh-HK', 'ja'],
  },
}));

describe('test_sitemap_includes_content', () => {
  it('sitemap includes content page URLs for published posts', async () => {
    // Dynamically import after mocks are set up
    const { default: sitemap } = await import('../src/app/sitemap');
    const entries = sitemap();

    const urls = entries.map((entry: { url: string }) => entry.url);

    // Must include zh-CN blog post
    expect(urls).toContain('https://kaitu.io/zh-CN/blog/hello-world');

    // Must include en-US blog post
    expect(urls).toContain('https://kaitu.io/en-US/blog/hello-world');

    // Must include zh-CN guides post
    expect(urls).toContain('https://kaitu.io/zh-CN/guides/getting-started');

    // Must NOT include draft post
    expect(urls).not.toContain('https://kaitu.io/zh-CN/blog/draft-post');
  });

  it('sitemap content entries have correct metadata', async () => {
    const { default: sitemap } = await import('../src/app/sitemap');
    const entries = sitemap();

    const contentEntry = entries.find(
      (entry: { url: string }) => entry.url === 'https://kaitu.io/zh-CN/blog/hello-world'
    );

    expect(contentEntry).toBeDefined();
    expect(contentEntry?.changeFrequency).toBe('weekly');
    expect(contentEntry?.priority).toBe(0.6);
    expect(contentEntry?.lastModified).toBeInstanceOf(Date);
  });

  it('sitemap still includes static pages alongside content pages', async () => {
    const { default: sitemap } = await import('../src/app/sitemap');
    const entries = sitemap();

    const urls = entries.map((entry: { url: string }) => entry.url);

    // Static pages must still be present
    expect(urls.some((url: string) => url.includes('/zh-CN/install'))).toBe(true);
    expect(urls.some((url: string) => url.includes('/zh-CN/purchase'))).toBe(true);
    expect(urls.some((url: string) => url.includes('kaitu.io'))).toBe(true);
  });
});
