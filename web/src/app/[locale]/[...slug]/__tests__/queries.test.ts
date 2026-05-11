import { describe, it, expect, vi } from 'vitest'

// @payload-enchants/translator has an ESM dir-import bug; stub before importing
// queries.ts (which transitively imports translator via lazyTranslate.ts).
vi.mock('@payload-enchants/translator', () => ({ translateOperation: vi.fn() }))

// Stub lazyTranslate so these tests focus on query orchestration, not the
// lock + translation logic (covered in tests/payload/lazyTranslate.test.ts).
vi.mock('@/payload/lazyTranslate', async () => {
  const actual = await vi.importActual<typeof import('@/payload/lazyTranslate')>('@/payload/lazyTranslate')
  return {
    ...actual,
    lazyTranslate: vi.fn().mockResolvedValue({ status: 'translated' }),
  }
})

import { findCategoryBySlug, findPostInCategory, listPostsInCategory } from '../queries'

type MockPayload = {
  find: ReturnType<typeof vi.fn>
  findByID: ReturnType<typeof vi.fn>
}

function makePayload(opts: { probeDocs?: unknown[]; listDocs?: unknown[]; finalDoc?: unknown }): MockPayload {
  return {
    find: vi.fn().mockImplementation((args: { fallbackLocale?: unknown }) => {
      if (args.fallbackLocale === false) {
        return Promise.resolve({ docs: opts.probeDocs ?? [] })
      }
      return Promise.resolve({ docs: opts.listDocs ?? [] })
    }),
    findByID: vi.fn().mockResolvedValue(opts.finalDoc),
  }
}

describe('findCategoryBySlug', () => {
  it('returns the category when found', async () => {
    const cat = { id: 1, slug: 'blog', name: 'Blog' }
    const payload = makePayload({ listDocs: [cat] })

    const result = await findCategoryBySlug(payload as never, 'zh-CN', 'blog')

    expect(result).toEqual(cat)
    expect(payload.find).toHaveBeenCalledWith({
      collection: 'categories',
      locale: 'zh-CN',
      where: { slug: { equals: 'blog' } },
      limit: 1,
      depth: 0,
      overrideAccess: true,
    })
  })

  it('returns null when no category matches', async () => {
    const payload = makePayload({ listDocs: [] })
    const result = await findCategoryBySlug(payload as never, 'zh-CN', 'nope')
    expect(result).toBeNull()
  })
})

describe('findPostInCategory', () => {
  it('returns the post when found — source locale skips lazy translate', async () => {
    const post = {
      id: 10,
      slug: 'hello',
      title: 'Hello',
      content: { root: {} },
      showOnKaitu: true,
      showOnOverleap: false,
    }
    const payload = makePayload({ probeDocs: [post], finalDoc: post })

    const result = await findPostInCategory(payload as never, 'zh-CN', 1, 'hello')

    expect(result).toEqual(post)
    // Probe call with no fallback
    expect(payload.find).toHaveBeenCalledWith({
      collection: 'posts',
      locale: 'zh-CN',
      fallbackLocale: false,
      where: {
        and: [
          { slug: { equals: 'hello' } },
          { status: { equals: 'published' } },
          { category: { equals: 1 } },
        ],
      },
      limit: 1,
      depth: 0,
      overrideAccess: true,
    })
    // Final fetch by id with default fallback
    expect(payload.findByID).toHaveBeenCalledWith({
      collection: 'posts',
      id: 10,
      locale: 'zh-CN',
      depth: 2,
      overrideAccess: true,
    })
  })

  it('returns null when no post matches', async () => {
    const payload = makePayload({ probeDocs: [] })
    const result = await findPostInCategory(payload as never, 'zh-CN', 1, 'nope')
    expect(result).toBeNull()
    expect(payload.findByID).not.toHaveBeenCalled()
  })
})

describe('listPostsInCategory', () => {
  it('filters by category + status + brand visibility', async () => {
    const payload = makePayload({ listDocs: [] })
    await listPostsInCategory(payload as never, 'en-US', 5, 'showOnOverleap')
    expect(payload.find).toHaveBeenCalledWith({
      collection: 'posts',
      locale: 'en-US',
      where: {
        and: [
          { status: { equals: 'published' } },
          { category: { equals: 5 } },
          { showOnOverleap: { equals: true } },
        ],
      },
      sort: '-publishedAt',
      limit: 50,
      depth: 1,
      overrideAccess: true,
    })
  })
})
