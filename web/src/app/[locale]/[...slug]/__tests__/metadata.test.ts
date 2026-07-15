import { describe, it, expect, vi, beforeEach } from 'vitest'

// Make React's `cache()` a no-op identity so each test gets fresh mock state.
vi.mock('react', async (importOriginal) => {
  const actual = await importOriginal<typeof import('react')>()
  return {
    ...actual,
    cache: <T extends (...args: unknown[]) => unknown>(fn: T) => fn,
  }
})

// Mock @/i18n/routing to avoid pulling next-intl/navigation into vitest's
// module graph (same pattern as sibling metadata tests).
vi.mock('@/i18n/routing', () => ({
  routing: {
    locales: ['en-US', 'en-GB', 'en-AU', 'zh-CN', 'zh-TW', 'zh-HK', 'ja'],
    defaultLocale: 'zh-CN',
  },
  Link: 'a',
}))

// Mock next-intl/server setRequestLocale (imported transitively).
vi.mock('next-intl/server', () => ({
  setRequestLocale: vi.fn(),
}))

// Mock brand server with configurable default.
vi.mock('@/lib/brand-server', () => ({
  getBrand: vi.fn(),
}))

// Mock Payload. The factory must not reference top-level variables (vi.mock is
// hoisted) so we use vi.hoisted() to declare mockFind/mockFindByID before
// hoisting occurs.
const { mockFind, mockFindByID } = vi.hoisted(() => ({
  mockFind: vi.fn(),
  mockFindByID: vi.fn(),
}))

vi.mock('payload', () => ({
  getPayload: vi.fn().mockResolvedValue({ find: mockFind, findByID: mockFindByID }),
}))

vi.mock('@payload-config', () => ({ default: {} }))

// @payload-enchants/translator has an ESM dir-import bug; stub before
// queries.ts (imported transitively) pulls it in.
vi.mock('@payload-enchants/translator', () => ({ translateOperation: vi.fn() }))

// Stub lazyTranslate: metadata tests don't exercise translation, just the
// query orchestration. Default to a no-op that always reports translated.
vi.mock('@/payload/lazyTranslate', async () => {
  const actual = await vi.importActual<typeof import('@/payload/lazyTranslate')>('@/payload/lazyTranslate')
  return {
    ...actual,
    lazyTranslate: vi.fn().mockResolvedValue({ status: 'translated' }),
  }
})

vi.mock('@/lib/brands', () => {
  const mk = (id: string) => ({
    id,
    displayName: id === 'kaitu' ? 'Kaitu' : 'Overleap',
    baseUrl: id === 'kaitu' ? 'https://kaitu.io' : 'https://overleap.io',
    defaultLocale: id === 'kaitu' ? 'zh-CN' : 'en-US',
    // Phase 2: metadata.ts builds hreflang from the brand's OWN locales.
    allowedLocales:
      id === 'kaitu' ? ['zh-CN', 'zh-TW', 'zh-HK'] : ['en-US', 'en-GB', 'en-AU', 'ja'],
    faviconPrefix: id === 'kaitu' ? '' : '/brand/overleap',
    wordmark: id === 'kaitu' ? '开途' : 'Overleap',
    ogImagePath: '/og-image.png',
  })
  return {
    brandById: (id: string) => mk(id),
    // metadata.ts pulls KAITU through the shared helper.
    KAITU: mk('kaitu'),
    OVERLEAP: mk('overleap'),
  }
})

// Mock Header/Footer to avoid pulling their transitive client-component deps.
vi.mock('@/components/Header', () => ({ default: () => null }))
vi.mock('@/components/Footer', () => ({ default: () => null }))

// Mock RichText to avoid lexical dependency in tests.
vi.mock('@payloadcms/richtext-lexical/react', () => ({ RichText: () => null }))

import { generateMetadata } from '../page'
import { getBrand } from '@/lib/brand-server'

const mockedGetBrand = vi.mocked(getBrand)

beforeEach(() => {
  vi.clearAllMocks()
  mockedGetBrand.mockResolvedValue({
    id: 'kaitu',
    displayName: 'Kaitu',
    baseUrl: 'https://kaitu.io',
    defaultLocale: 'zh-CN',
    allowedLocales: ['zh-CN', 'zh-TW', 'zh-HK'],
    faviconPrefix: '',
    wordmark: '开途',
    ogImagePath: '/og-image.png',
  } as never)
})

describe('generateMetadata (category list, 1 segment)', () => {
  it('returns title + description from category', async () => {
    mockFind.mockResolvedValueOnce({
      docs: [{ id: 1, slug: 'blog', name: 'Blog', description: 'Latest posts' }],
    })

    const meta = await generateMetadata({
      params: Promise.resolve({ locale: 'zh-CN', slug: ['blog'] }),
    })

    expect(meta.title).toBe('Blog | Kaitu')
    expect(meta.description).toBe('Latest posts')
    // Regression: category pages must carry their own OG title (not inherit
    // the homepage k2cc default via Next.js shallow metadata merge).
    expect(meta.openGraph?.title).toBe('Blog | Kaitu')
    expect((meta.openGraph as { url?: string })?.url).toBe('https://kaitu.io/zh-CN/blog')
    expect((meta.openGraph as { type?: string })?.type).toBe('website')
    expect(meta.twitter?.title).toBe('Blog | Kaitu')
  })

  it('returns empty object when category not found', async () => {
    mockFind.mockResolvedValueOnce({ docs: [] })
    const meta = await generateMetadata({
      params: Promise.resolve({ locale: 'zh-CN', slug: ['nope-1seg'] }),
    })
    expect(meta).toEqual({})
  })
})

describe('generateMetadata (post detail, 2 segments)', () => {
  it('returns title + canonical from post', async () => {
    const category = { id: 1, slug: 'cat-detail-1', name: 'Cat' }
    const post = {
      id: 10,
      slug: 'hello',
      title: 'Hello',
      excerpt: 'Excerpted',
      publishedAt: '2026-06-04T00:00:00.000Z',
      content: { root: {} },
      showOnKaitu: true,
      showOnOverleap: false,
      coverImage: { url: 'https://media.kaitu.io/media/hello.png', alt: 'Hello cover' },
    }

    // First find call: findCategoryBySlug. Second find call: findPostInCategory probe.
    mockFind
      .mockResolvedValueOnce({ docs: [category] })
      .mockResolvedValueOnce({ docs: [post] })
    // Then findPostInCategory final fetch
    mockFindByID.mockResolvedValueOnce(post)

    const meta = await generateMetadata({
      params: Promise.resolve({ locale: 'zh-CN', slug: ['cat-detail-1', 'hello'] }),
    })

    expect(meta.title).toBe('Hello | Kaitu')
    expect(meta.description).toBe('Excerpted')
    // Phase 2: canonical is always the rendering brand's own host.
    expect(meta.alternates?.canonical).toBe('https://kaitu.io/zh-CN/cat-detail-1/hello')
    // Regression: post pages emit article-type OG with the post's own title +
    // URL, not the inherited homepage default.
    expect(meta.openGraph?.title).toBe('Hello | Kaitu')
    expect((meta.openGraph as { type?: string })?.type).toBe('article')
    expect((meta.openGraph as { url?: string })?.url).toBe('https://kaitu.io/zh-CN/cat-detail-1/hello')
    expect((meta.openGraph as { publishedTime?: string })?.publishedTime).toBe('2026-06-04T00:00:00.000Z')
    expect(meta.twitter?.title).toBe('Hello | Kaitu')
    // hreflang alternates now present (cross-domain locale linking).
    expect(meta.alternates?.languages?.['zh-cn']).toBe('https://kaitu.io/zh-CN/cat-detail-1/hello')
    // coverImage (absolute CDN url) becomes og:image as-is — NOT prefixed with
    // the brand base URL (would otherwise be https://kaitu.iohttps://media...).
    const ogImages = (meta.openGraph as { images?: Array<{ url?: string }> })?.images
    expect(ogImages?.[0]?.url).toBe('https://media.kaitu.io/media/hello.png')
    expect(meta.twitter?.images).toContain('https://media.kaitu.io/media/hello.png')
  })

  // Phase 2 load-bearing case: this post is visible on overleap only, yet it is
  // being rendered by the kaitu deployment. The old resolveCanonicalBrand would
  // have pointed the canonical at overleap.io — a cross-brand leak. The brands
  // are now fully isolated, so the canonical must stay on the rendering host.
  it('post canonical always points at the rendering brand own host', async () => {
    const category = { id: 3, slug: 'cat-detail-3', name: 'Cat 3' }
    const post = {
      id: 30,
      slug: 'overleap-only',
      title: 'Overleap Only',
      excerpt: 'Excerpted',
      publishedAt: '2026-06-04T00:00:00.000Z',
      content: { root: {} },
      showOnKaitu: false,
      showOnOverleap: true,
    }
    mockFind
      .mockResolvedValueOnce({ docs: [category] })
      .mockResolvedValueOnce({ docs: [post] })
    mockFindByID.mockResolvedValueOnce(post)

    const meta = await generateMetadata({
      params: Promise.resolve({ locale: 'zh-CN', slug: ['cat-detail-3', 'overleap-only'] }),
    })

    // Baked brand in this file's harness is kaitu (getBrand mock).
    expect(meta.alternates?.canonical).toBe(
      'https://kaitu.io/zh-CN/cat-detail-3/overleap-only',
    )
    expect(JSON.stringify(meta.alternates)).not.toContain('overleap.io')
  })

  it('returns empty when category not found', async () => {
    mockFind.mockResolvedValueOnce({ docs: [] })
    const meta = await generateMetadata({
      params: Promise.resolve({ locale: 'zh-CN', slug: ['nope-2seg', 'hello'] }),
    })
    expect(meta).toEqual({})
  })

  it('returns empty when post not found in category', async () => {
    mockFind
      .mockResolvedValueOnce({ docs: [{ id: 2, slug: 'cat-detail-2', name: 'Cat 2' }] })
      .mockResolvedValueOnce({ docs: [] })
    const meta = await generateMetadata({
      params: Promise.resolve({ locale: 'zh-CN', slug: ['cat-detail-2', 'nope'] }),
    })
    expect(meta).toEqual({})
  })
})

describe('generateMetadata (3+ segments)', () => {
  it('returns empty object', async () => {
    const meta = await generateMetadata({
      params: Promise.resolve({ locale: 'zh-CN', slug: ['a', 'b', 'c'] }),
    })
    expect(meta).toEqual({})
  })
})
