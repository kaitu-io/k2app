/**
 * K2 Route Infrastructure Tests — T3
 *
 * Vitest tests for Velite schema extension and /k2/ route infrastructure.
 * RED phase: tests fail before implementation.
 *
 * Tests verify:
 * 1. Velite schema accepts order and section fields
 * 2. k2/ route page component exists and handles params
 * 3. getK2Posts groups by section, sorts by order
 * 4. Layout component exists and can be imported
 */
import { describe, it, expect, vi } from 'vitest';

// Mock #velite module with k2/ posts that include order and section fields
vi.mock('#velite', () => ({
  posts: [
    {
      title: 'k2 协议',
      date: '2026-02-21T00:00:00.000Z',
      summary: 'k2 隐身网络隧道协议概述',
      tags: ['k2', '协议'],
      draft: false,
      content: '<h1>k2 协议</h1><p>k2 是一种隐身网络隧道协议。</p>',
      metadata: { readingTime: 1, wordCount: 50 },
      filePath: 'zh-CN/k2/index',
      locale: 'zh-CN',
      slug: 'k2',
      order: 1,
      section: 'getting-started',
    },
    {
      title: 'k2 Architecture',
      date: '2026-02-21T00:00:00.000Z',
      summary: 'Technical deep-dive into k2 protocol architecture',
      tags: ['k2', 'technical'],
      draft: false,
      content: '<h1>k2 Architecture</h1><p>Technical content.</p>',
      metadata: { readingTime: 5, wordCount: 800 },
      filePath: 'zh-CN/k2/architecture',
      locale: 'zh-CN',
      slug: 'k2/architecture',
      order: 1,
      section: 'technical',
    },
    {
      title: 'k2 vs WireGuard',
      date: '2026-02-21T00:00:00.000Z',
      summary: 'Comparison between k2 and WireGuard protocols',
      tags: ['k2', 'comparison'],
      draft: false,
      content: '<h1>k2 vs WireGuard</h1><p>Comparison content.</p>',
      metadata: { readingTime: 3, wordCount: 400 },
      filePath: 'zh-CN/k2/vs-wireguard',
      locale: 'zh-CN',
      slug: 'k2/vs-wireguard',
      order: 1,
      section: 'comparison',
    },
    {
      title: 'k2 安装指南',
      date: '2026-02-21T00:00:00.000Z',
      summary: '如何安装 k2 客户端',
      tags: ['k2', '安装'],
      draft: false,
      content: '<h1>k2 安装指南</h1><p>安装内容。</p>',
      metadata: { readingTime: 2, wordCount: 200 },
      filePath: 'zh-CN/k2/install',
      locale: 'zh-CN',
      slug: 'k2/install',
      order: 2,
      section: 'getting-started',
    },
    {
      title: 'Unrelated Blog Post',
      date: '2026-02-20T00:00:00.000Z',
      summary: 'This is not a k2/ post',
      tags: [],
      draft: false,
      content: '<p>Blog content.</p>',
      metadata: { readingTime: 1, wordCount: 50 },
      filePath: 'zh-CN/blog/hello-world',
      locale: 'zh-CN',
      slug: 'blog/hello-world',
    },
  ],
}));

// Mock @/i18n/routing
vi.mock('@/i18n/routing', () => ({
  routing: {
    locales: ['zh-CN', 'en-US', 'en-GB', 'en-AU', 'zh-TW', 'zh-HK', 'ja'],
  },
  Link: ({ children, href }: { children: React.ReactNode; href: string }) => ({ type: 'a', props: { href, children } }),
}));

// Mock next-intl/server
vi.mock('next-intl/server', () => ({
  getTranslations: vi.fn().mockResolvedValue((key: string) => key),
  setRequestLocale: vi.fn(),
}));

// Mock next-intl
vi.mock('next-intl', () => ({
  useTranslations: () => (key: string) => key,
  useLocale: () => 'zh-CN',
}));

// Mock next/navigation
vi.mock('next/navigation', () => ({
  notFound: vi.fn(() => { throw new Error('NOT_FOUND'); }),
  usePathname: vi.fn(() => '/zh-CN/k2/index'),
}));

// Mock @/components/Header
vi.mock('@/components/Header', () => ({
  default: () => null,
}));

// Mock @/components/Footer
vi.mock('@/components/Footer', () => ({
  default: () => null,
}));

// Mock @/components/K2Sidebar
vi.mock('@/components/K2Sidebar', () => ({
  default: () => null,
}));

describe('test_velite_schema_accepts_order_section', () => {
  it('mock posts data includes order field as a number', async () => {
    const { posts } = await import('#velite');

    const k2Posts = (posts as unknown[]).filter(
      (p): p is { slug: string; order?: number; section?: string } =>
        typeof (p as { slug: string }).slug === 'string' &&
        (p as { slug: string }).slug.startsWith('k2/')
    );

    expect(k2Posts.length).toBeGreaterThan(0);

    // Each k2 post with order should have a numeric order
    const postsWithOrder = k2Posts.filter((p) => p.order !== undefined);
    expect(postsWithOrder.length).toBeGreaterThan(0);
    postsWithOrder.forEach((post) => {
      expect(typeof post.order).toBe('number');
    });
  });

  it('mock posts data includes section field as a string', async () => {
    const { posts } = await import('#velite');

    const k2Posts = (posts as unknown[]).filter(
      (p): p is { slug: string; order?: number; section?: string } =>
        typeof (p as { slug: string }).slug === 'string' &&
        (p as { slug: string }).slug.startsWith('k2/')
    );

    const postsWithSection = k2Posts.filter((p) => p.section !== undefined);
    expect(postsWithSection.length).toBeGreaterThan(0);
    postsWithSection.forEach((post) => {
      expect(typeof post.section).toBe('string');
    });
  });

  it('posts without order/section fields remain valid (optional fields)', async () => {
    const { posts } = await import('#velite');

    const blogPost = (posts as unknown[]).find(
      (p): p is { slug: string; order?: number; section?: string } =>
        (p as { slug: string }).slug === 'blog/hello-world'
    );

    expect(blogPost).toBeDefined();
    // Optional fields should be absent or undefined
    expect(blogPost?.order).toBeUndefined();
    expect(blogPost?.section).toBeUndefined();
  });
});

describe('test_k2_route_renders_content', () => {
  it('page module can be imported', async () => {
    const pageModule = await import('../src/app/[locale]/k2/[[...path]]/page');
    expect(pageModule).toBeDefined();
    expect(pageModule.default).toBeTypeOf('function');
  });

  it('page component is an async function (Server Component pattern)', async () => {
    const { default: K2Page } = await import('../src/app/[locale]/k2/[[...path]]/page');
    expect(K2Page).toBeTypeOf('function');

    // Calling with undefined path (index page) should return a Promise
    const result = K2Page({ params: Promise.resolve({ locale: 'zh-CN', path: undefined }) });
    expect(result).toBeInstanceOf(Promise);
  });

  it('page renders without throwing for index path (path: undefined)', async () => {
    const { default: K2Page } = await import('../src/app/[locale]/k2/[[...path]]/page');

    // Index page: no path segments
    const element = await K2Page({ params: Promise.resolve({ locale: 'zh-CN', path: undefined }) });
    expect(element).toBeDefined();
  });

  it('page exports generateStaticParams', async () => {
    const pageModule = await import('../src/app/[locale]/k2/[[...path]]/page');
    expect(pageModule.generateStaticParams).toBeTypeOf('function');
  });

  it('page exports generateMetadata', async () => {
    const pageModule = await import('../src/app/[locale]/k2/[[...path]]/page');
    expect(pageModule.generateMetadata).toBeTypeOf('function');
  });

  it('generateStaticParams returns array with locale and path params', async () => {
    const { generateStaticParams } = await import('../src/app/[locale]/k2/[[...path]]/page');

    const params = generateStaticParams();
    expect(Array.isArray(params)).toBe(true);
    expect(params.length).toBeGreaterThan(0);

    // Each param should have locale and path fields
    params.forEach((param: { locale: string; path?: string[] }) => {
      expect(typeof param.locale).toBe('string');
    });
  });
});

describe('test_k2_sidebar_groups_by_section', () => {
  it('getK2Posts helper can be imported', async () => {
    const module = await import('../src/lib/k2-posts');
    expect(module).toBeDefined();
    expect(module.getK2Posts).toBeTypeOf('function');
  });

  it('getK2Posts filters only k2/ prefix posts for given locale', async () => {
    const { getK2Posts } = await import('../src/lib/k2-posts');

    const result = getK2Posts('zh-CN');

    // blog/hello-world must not appear
    const allSlugs = result.flatMap((group) => group.posts.map((p) => p.slug));
    expect(allSlugs).not.toContain('blog/hello-world');

    // k2/ posts must appear
    expect(allSlugs.some((slug) => slug.startsWith('k2/'))).toBe(true);
  });

  it('getK2Posts groups posts by section', async () => {
    const { getK2Posts } = await import('../src/lib/k2-posts');

    const result = getK2Posts('zh-CN');

    // Result should be an array of groups
    expect(Array.isArray(result)).toBe(true);
    expect(result.length).toBeGreaterThan(0);

    // Each group should have a section key and posts array
    result.forEach((group) => {
      expect(typeof group.section).toBe('string');
      expect(Array.isArray(group.posts)).toBe(true);
      expect(group.posts.length).toBeGreaterThan(0);
    });
  });

  it('getK2Posts sorts posts by order within each section', async () => {
    const { getK2Posts } = await import('../src/lib/k2-posts');

    const result = getK2Posts('zh-CN');

    // getting-started section should have order 1 first, then order 2
    const gettingStarted = result.find((g) => g.section === 'getting-started');
    expect(gettingStarted).toBeDefined();
    expect(gettingStarted!.posts.length).toBeGreaterThanOrEqual(2);

    const orders = gettingStarted!.posts.map((p) => p.order ?? 0);
    for (let i = 1; i < orders.length; i++) {
      expect(orders[i]).toBeGreaterThanOrEqual(orders[i - 1]);
    }
  });

  it('getK2Posts returns posts with required fields', async () => {
    const { getK2Posts } = await import('../src/lib/k2-posts');

    const result = getK2Posts('zh-CN');
    const firstGroup = result[0];
    expect(firstGroup).toBeDefined();

    const firstPost = firstGroup.posts[0];
    expect(firstPost).toBeDefined();
    expect(typeof firstPost.title).toBe('string');
    expect(typeof firstPost.slug).toBe('string');
    expect(firstPost.slug === 'k2' || firstPost.slug.startsWith('k2/')).toBe(true);
  });
});

describe('test_k2_route_renders_sidebar', () => {
  it('layout module can be imported', async () => {
    const layoutModule = await import('../src/app/[locale]/k2/[[...path]]/layout');
    expect(layoutModule).toBeDefined();
    expect(layoutModule.default).toBeTypeOf('function');
  });

  it('layout component is a function (Server Component)', async () => {
    const { default: K2Layout } = await import('../src/app/[locale]/k2/[[...path]]/layout');
    expect(K2Layout).toBeTypeOf('function');
  });

  it('K2Sidebar component can be imported', async () => {
    const sidebarModule = await import('../src/components/K2Sidebar');
    expect(sidebarModule).toBeDefined();
    expect(sidebarModule.default).toBeTypeOf('function');
  });
});
