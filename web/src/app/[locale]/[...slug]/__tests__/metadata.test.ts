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
// hoisted) so we use vi.hoisted() to declare mockFind before hoisting occurs.
const { mockFind } = vi.hoisted(() => ({ mockFind: vi.fn() }))

vi.mock('payload', () => ({
  getPayload: vi.fn().mockResolvedValue({ find: mockFind }),
}))

vi.mock('@payload-config', () => ({ default: {} }))

vi.mock('@/lib/brands', () => ({
  brandById: (id: string) => ({
    id,
    displayName: id === 'kaitu' ? 'Kaitu' : 'Overleap',
    baseUrl: id === 'kaitu' ? 'https://kaitu.io' : 'https://overleap.io',
  }),
}))

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
      showOnKaitu: true,
      showOnOverleap: false,
    }

    mockFind
      .mockResolvedValueOnce({ docs: [category] })
      .mockResolvedValueOnce({ docs: [post] })

    const meta = await generateMetadata({
      params: Promise.resolve({ locale: 'zh-CN', slug: ['cat-detail-1', 'hello'] }),
    })

    expect(meta.title).toBe('Hello | Kaitu')
    expect(meta.description).toBe('Excerpted')
    expect(meta.alternates?.canonical).toBe('https://kaitu.io/zh-CN/cat-detail-1/hello')
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
