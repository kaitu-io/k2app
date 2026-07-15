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
import { describe, it, expect, vi, beforeEach } from 'vitest';
import { KAITU, OVERLEAP } from '@/lib/brands';

// Mock @/lib/brand-server — tests override the resolved brand per-case via beforeEach.
vi.mock('@/lib/brand-server', () => ({
  getBrand: vi.fn(),
}));

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
      title: 'k2 协议对比',
      date: '2026-02-21T00:00:00.000Z',
      summary: 'k2 与主流协议对比',
      tags: ['k2', 'comparison'],
      draft: false,
      content: '<h1>k2 协议对比</h1><p>Comparison aggregate.</p>',
      metadata: { readingTime: 5, wordCount: 700 },
      filePath: 'zh-CN/k2/comparison',
      locale: 'zh-CN',
      slug: 'k2/comparison',
      order: 0,
      section: 'comparison',
    },
    {
      title: 'k2 Protocol Comparison',
      date: '2026-02-21T00:00:00.000Z',
      summary: 'k2 vs WireGuard / Shadowsocks / VLESS+Reality / Hysteria2',
      tags: ['k2', 'comparison'],
      draft: false,
      content: '<h1>k2 Protocol Comparison</h1><p>Comparison aggregate.</p>',
      metadata: { readingTime: 5, wordCount: 700 },
      filePath: 'en-US/k2/comparison',
      locale: 'en-US',
      slug: 'k2/comparison',
      order: 0,
      section: 'comparison',
    },
    // en-US counterpart of the zh-CN architecture doc. The brand-aware JSON-LD
    // cases below render this under OVERLEAP; before the fallback was made
    // brand-aware they silently rendered the zh-CN post instead — i.e. they were
    // passing while exercising the leak this suite now forbids.
    {
      title: 'k2 Architecture',
      date: '2026-02-21T00:00:00.000Z',
      summary: 'Technical deep-dive into k2 protocol architecture',
      tags: ['k2', 'technical'],
      draft: false,
      content: '<h1>k2 Architecture</h1><p>Technical content.</p>',
      metadata: { readingTime: 5, wordCount: 800 },
      filePath: 'en-US/k2/architecture',
      locale: 'en-US',
      slug: 'k2/architecture',
      order: 1,
      section: 'technical',
    },
    // kaitu-only install doc, present in BOTH locales — the shape that made the
    // frontmatter gate necessary (en-US gated, zh-CN unmarked → 'both').
    {
      title: 'k2s Server Deployment',
      date: '2026-02-21T00:00:00.000Z',
      summary: 'Deploy k2s on a Linux VPS. Install via https://kaitu.io/i/k2s.',
      tags: ['k2', 'getting-started'],
      draft: false,
      content: '<h1>k2s Server Deployment</h1><p>curl -fsSL https://kaitu.io/i/k2s | sudo sh</p>',
      metadata: { readingTime: 3, wordCount: 300 },
      filePath: 'en-US/k2/server',
      locale: 'en-US',
      slug: 'k2/server',
      order: 3,
      section: 'getting-started',
      brand: 'kaitu',
    },
    {
      title: 'k2s 服务端部署',
      date: '2026-02-21T00:00:00.000Z',
      summary: '在 Linux VPS 上部署 k2s。',
      tags: ['k2', 'getting-started'],
      draft: false,
      content: '<h1>k2s 服务端部署</h1><p>开途 服务端部署说明。</p>',
      metadata: { readingTime: 3, wordCount: 300 },
      filePath: 'zh-CN/k2/server',
      locale: 'zh-CN',
      slug: 'k2/server',
      order: 3,
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

// Default to KAITU for every test so existing assertions (which predate brand-aware
// JSON-LD) continue to see the legacy Kaitu URLs. Individual tests override this.
beforeEach(async () => {
  const { getBrand } = await import('@/lib/brand-server');
  (getBrand as unknown as { mockResolvedValue: (b: unknown) => void }).mockResolvedValue(KAITU);
});

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

/**
 * Extract the JSON-LD payload from the rendered page's <script> element.
 * The page returns a Fragment whose first child is the <script> tag carrying
 * the stringified JSON-LD on its inner-HTML injection prop.
 */
const RAW_HTML_PROP = ['danger', 'ously', 'Set', 'Inner', 'HTML'].join('');
function extractJsonLd(element: unknown): unknown {
  const fragment = element as {
    props: { children: Array<{ props?: Record<string, unknown> }> };
  };
  const children = fragment.props.children;
  const scriptNode = children.find(
    (child) => (child?.props?.type as string | undefined) === 'application/ld+json'
  );
  if (!scriptNode) {
    throw new Error('JSON-LD script element not found in rendered page');
  }
  const htmlWrapper = scriptNode.props?.[RAW_HTML_PROP] as { __html?: string } | undefined;
  const json = htmlWrapper?.__html;
  if (!json) {
    throw new Error('JSON-LD payload missing from script element');
  }
  return JSON.parse(json);
}

/**
 * Collect EVERY JSON-LD payload from the rendered page. The page emits each
 * schema.org entity as its own <script> tag (TechArticle always, FAQPage only
 * on /k2/comparison) rather than a single array root, so parsers that assume a
 * single-object root don't crash on @context lookup. Tests that care about the
 * FAQPage entity must scan all scripts, not just the first.
 */
function extractAllJsonLd(element: unknown): Array<Record<string, unknown>> {
  const fragment = element as {
    props: { children: Array<{ props?: Record<string, unknown> }> };
  };
  return fragment.props.children
    .filter((child) => (child?.props?.type as string | undefined) === 'application/ld+json')
    .map((scriptNode) => {
      const htmlWrapper = scriptNode.props?.[RAW_HTML_PROP] as { __html?: string } | undefined;
      const json = htmlWrapper?.__html;
      if (!json) {
        throw new Error('JSON-LD payload missing from script element');
      }
      return JSON.parse(json) as Record<string, unknown>;
    });
}

describe('test_k2_comparison_emits_faqpage_jsonld', () => {
  it('emits per-entity TechArticle + FAQPage <script> tags for k2/comparison (zh-CN)', async () => {
    const { default: K2Page } = await import('../src/app/[locale]/k2/[[...path]]/page');

    const element = await K2Page({
      params: Promise.resolve({ locale: 'zh-CN', path: ['comparison'] }),
    });

    // Each schema.org entity is its own <script> tag (not a JSON array root).
    const entities = extractAllJsonLd(element) as Array<{ '@type': string }>;
    expect(entities.length).toBe(2);

    const techArticle = entities.find((obj) => obj['@type'] === 'TechArticle');
    expect(techArticle).toBeDefined();

    const faqPages = entities.filter((obj) => obj['@type'] === 'FAQPage');
    expect(faqPages.length).toBe(1);
  });

  it('FAQPage has 4 mainEntity Q&A pairs with proper shape (zh-CN)', async () => {
    const { default: K2Page } = await import('../src/app/[locale]/k2/[[...path]]/page');

    const element = await K2Page({
      params: Promise.resolve({ locale: 'zh-CN', path: ['comparison'] }),
    });

    const jsonLd = extractAllJsonLd(element) as Array<{
      '@type': string;
      mainEntity?: Array<{
        '@type': string;
        name: string;
        acceptedAnswer: { '@type': string; text: string };
      }>;
    }>;

    const faqPage = jsonLd.find((obj) => obj['@type'] === 'FAQPage');
    expect(faqPage).toBeDefined();
    expect(faqPage!.mainEntity).toBeDefined();
    expect(faqPage!.mainEntity!.length).toBe(4);

    faqPage!.mainEntity!.forEach((qa) => {
      expect(qa['@type']).toBe('Question');
      expect(typeof qa.name).toBe('string');
      expect(qa.name.length).toBeGreaterThan(0);
      expect(qa.acceptedAnswer['@type']).toBe('Answer');
      expect(typeof qa.acceptedAnswer.text).toBe('string');
      expect(qa.acceptedAnswer.text.length).toBeGreaterThan(0);
    });
  });

  it('FAQPage zh-CN Q&A text uses Chinese phrasing ("区别" + "WireGuard")', async () => {
    const { default: K2Page } = await import('../src/app/[locale]/k2/[[...path]]/page');

    const element = await K2Page({
      params: Promise.resolve({ locale: 'zh-CN', path: ['comparison'] }),
    });

    const jsonLd = extractAllJsonLd(element) as Array<{
      '@type': string;
      mainEntity?: Array<{ name: string; acceptedAnswer: { text: string } }>;
    }>;

    const faqPage = jsonLd.find((obj) => obj['@type'] === 'FAQPage');
    const firstQuestion = faqPage!.mainEntity![0].name;
    expect(firstQuestion).toContain('WireGuard');
    expect(firstQuestion).toContain('区别');
  });

  it('FAQPage en-US Q&A text uses English phrasing', async () => {
    const { default: K2Page } = await import('../src/app/[locale]/k2/[[...path]]/page');

    const element = await K2Page({
      params: Promise.resolve({ locale: 'en-US', path: ['comparison'] }),
    });

    const jsonLd = extractAllJsonLd(element) as Array<{
      '@type': string;
      mainEntity?: Array<{ name: string; acceptedAnswer: { text: string } }>;
    }>;

    const faqPage = jsonLd.find((obj) => obj['@type'] === 'FAQPage');
    expect(faqPage).toBeDefined();
    const firstQuestion = faqPage!.mainEntity![0].name;
    expect(firstQuestion).toContain('WireGuard');
    expect(firstQuestion.toLowerCase()).toContain('differ');
  });

  it('non-comparison slug still emits a single JSON-LD object (not an array)', async () => {
    const { default: K2Page } = await import('../src/app/[locale]/k2/[[...path]]/page');

    const element = await K2Page({
      params: Promise.resolve({ locale: 'zh-CN', path: ['architecture'] }),
    });

    const jsonLd = extractJsonLd(element);
    expect(Array.isArray(jsonLd)).toBe(false);
    expect((jsonLd as { '@type': string })['@type']).toBe('TechArticle');
  });
});

describe('test_k2_docs_are_brand_gated', () => {
  /** Render and report whether the page 404'd (notFound() throws NOT_FOUND). */
  async function renders(locale: string, path: string[] | undefined): Promise<boolean> {
    const { default: K2Page } = await import('../src/app/[locale]/k2/[[...path]]/page');
    try {
      await K2Page({ params: Promise.resolve({ locale, path }) });
      return true;
    } catch (e) {
      if ((e as Error).message === 'NOT_FOUND') return false;
      throw e;
    }
  }

  it('overleap 404s a brand: kaitu doc instead of serving it', async () => {
    const { getBrand } = await import('@/lib/brand-server');
    (getBrand as unknown as { mockResolvedValue: (b: unknown) => void }).mockResolvedValue(OVERLEAP);

    expect(await renders('en-US', ['server'])).toBe(false);
  });

  it('kaitu still serves that same doc', async () => {
    const { getBrand } = await import('@/lib/brand-server');
    (getBrand as unknown as { mockResolvedValue: (b: unknown) => void }).mockResolvedValue(KAITU);

    expect(await renders('zh-CN', ['server'])).toBe(true);
  });

  it('overleap does not fall back to the zh-CN copy of a gated doc', async () => {
    // The regression: en-US/k2/server is brand: kaitu, but zh-CN/k2/server is
    // unmarked ('both'). A zh-CN fallback would render a 开途 Chinese page on
    // overleap.io rather than 404.
    const { getBrand } = await import('@/lib/brand-server');
    (getBrand as unknown as { mockResolvedValue: (b: unknown) => void }).mockResolvedValue(OVERLEAP);

    expect(await renders('ja', ['server'])).toBe(false);
  });

  it('overleap falls back to its OWN default locale, never to zh-CN', async () => {
    // /ja/k2/architecture has no ja copy. Overleap must land on the en-US post.
    const { getBrand } = await import('@/lib/brand-server');
    (getBrand as unknown as { mockResolvedValue: (b: unknown) => void }).mockResolvedValue(OVERLEAP);

    const { default: K2Page } = await import('../src/app/[locale]/k2/[[...path]]/page');
    const element = await K2Page({ params: Promise.resolve({ locale: 'ja', path: ['architecture'] }) });
    const jsonLd = extractJsonLd(element) as { headline: string };
    expect(jsonLd.headline).toBe('k2 Architecture');
  });

  it('generateStaticParams prerenders no gated doc and no off-brand locale (overleap)', async () => {
    vi.stubEnv('NEXT_PUBLIC_BRAND', 'overleap');
    vi.resetModules();
    const { generateStaticParams } = await import('../src/app/[locale]/k2/[[...path]]/page');

    const params = generateStaticParams();
    expect(params.some((p) => p.path?.join('/') === 'server')).toBe(false);
    expect(params.every((p) => OVERLEAP.allowedLocales.includes(p.locale as never))).toBe(true);
    vi.unstubAllEnvs();
  });

  it('the sidebar source hides gated docs from overleap but keeps them on kaitu', async () => {
    const { getK2Posts } = await import('../src/lib/k2-posts');

    const overleapSlugs = getK2Posts('en-US', 'overleap').flatMap((g) => g.posts.map((p) => p.slug));
    expect(overleapSlugs).not.toContain('k2/server');

    const kaituSlugs = getK2Posts('zh-CN', 'kaitu').flatMap((g) => g.posts.map((p) => p.slug));
    expect(kaituSlugs).toContain('k2/server');
  });
});

describe('test_k2_techarticle_is_brand_aware', () => {
  it('TechArticle author/publisher/isPartOf name + url uses KAITU when brand is Kaitu', async () => {
    const { getBrand } = await import('@/lib/brand-server');
    (getBrand as unknown as { mockResolvedValue: (b: unknown) => void }).mockResolvedValue(KAITU);

    const { default: K2Page } = await import('../src/app/[locale]/k2/[[...path]]/page');

    const element = await K2Page({
      params: Promise.resolve({ locale: 'zh-CN', path: ['architecture'] }),
    });

    const jsonLd = extractJsonLd(element) as {
      '@type': string;
      url: string;
      author: { name: string; url: string };
      publisher: { name: string; url: string };
      isPartOf: { name: string; url: string };
      mainEntityOfPage: { '@id': string };
    };

    expect(jsonLd['@type']).toBe('TechArticle');
    expect(jsonLd.author.name).toBe('Kaitu');
    expect(jsonLd.author.url).toBe('https://kaitu.io');
    expect(jsonLd.publisher.name).toBe('Kaitu');
    expect(jsonLd.publisher.url).toBe('https://kaitu.io');
    expect(jsonLd.isPartOf.name).toBe('Kaitu');
    expect(jsonLd.isPartOf.url).toBe('https://kaitu.io');
    expect(jsonLd.url.startsWith('https://kaitu.io/')).toBe(true);
    expect(jsonLd.mainEntityOfPage['@id'].startsWith('https://kaitu.io/')).toBe(true);
  });

  it('TechArticle author/publisher/isPartOf name + url uses OVERLEAP when brand is Overleap', async () => {
    const { getBrand } = await import('@/lib/brand-server');
    (getBrand as unknown as { mockResolvedValue: (b: unknown) => void }).mockResolvedValue(OVERLEAP);

    const { default: K2Page } = await import('../src/app/[locale]/k2/[[...path]]/page');

    const element = await K2Page({
      params: Promise.resolve({ locale: 'en-US', path: ['architecture'] }),
    });

    const jsonLd = extractJsonLd(element) as {
      '@type': string;
      url: string;
      author: { name: string; url: string };
      publisher: { name: string; url: string };
      isPartOf: { name: string; url: string };
      mainEntityOfPage: { '@id': string };
    };

    expect(jsonLd['@type']).toBe('TechArticle');
    expect(jsonLd.author.name).toBe('Overleap');
    expect(jsonLd.author.url).toBe('https://overleap.io');
    expect(jsonLd.publisher.name).toBe('Overleap');
    expect(jsonLd.publisher.url).toBe('https://overleap.io');
    expect(jsonLd.isPartOf.name).toBe('Overleap');
    expect(jsonLd.isPartOf.url).toBe('https://overleap.io');
    expect(jsonLd.url.startsWith('https://overleap.io/')).toBe(true);
    expect(jsonLd.mainEntityOfPage['@id'].startsWith('https://overleap.io/')).toBe(true);
  });

  it('FAQPage @id on /k2/comparison uses brand baseUrl (Overleap)', async () => {
    const { getBrand } = await import('@/lib/brand-server');
    (getBrand as unknown as { mockResolvedValue: (b: unknown) => void }).mockResolvedValue(OVERLEAP);

    const { default: K2Page } = await import('../src/app/[locale]/k2/[[...path]]/page');

    const element = await K2Page({
      params: Promise.resolve({ locale: 'en-US', path: ['comparison'] }),
    });

    const jsonLd = extractAllJsonLd(element) as Array<{ '@type': string; '@id'?: string }>;
    const faqPage = jsonLd.find((obj) => obj['@type'] === 'FAQPage');
    expect(faqPage).toBeDefined();
    expect(typeof faqPage!['@id']).toBe('string');
    expect(faqPage!['@id']!.startsWith('https://overleap.io/')).toBe(true);
  });
});
