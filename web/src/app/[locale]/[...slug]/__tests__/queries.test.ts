import { describe, it, expect, vi } from 'vitest'
import { findCategoryBySlug, findPostInCategory, listPostsInCategory } from '../queries'

type MockPayload = {
  find: ReturnType<typeof vi.fn>
}

function makePayload(docs: unknown[]): MockPayload {
  return { find: vi.fn().mockResolvedValue({ docs }) }
}

describe('findCategoryBySlug', () => {
  it('returns the category when found', async () => {
    const cat = { id: 1, slug: 'blog', name: 'Blog' }
    const payload = makePayload([cat])

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
    const payload = makePayload([])
    const result = await findCategoryBySlug(payload as never, 'zh-CN', 'nope')
    expect(result).toBeNull()
  })
})

describe('findPostInCategory', () => {
  it('returns the post when found', async () => {
    const post = {
      id: 10,
      slug: 'hello',
      title: 'Hello',
      showOnKaitu: true,
      showOnOverleap: false,
    }
    const payload = makePayload([post])

    const result = await findPostInCategory(payload as never, 'zh-CN', 1, 'hello')

    expect(result).toEqual(post)
    expect(payload.find).toHaveBeenCalledWith({
      collection: 'posts',
      locale: 'zh-CN',
      where: {
        and: [
          { slug: { equals: 'hello' } },
          { status: { equals: 'published' } },
          { category: { equals: 1 } },
        ],
      },
      limit: 1,
      depth: 2,
      overrideAccess: true,
    })
  })

  it('returns null when no post matches', async () => {
    const payload = makePayload([])
    const result = await findPostInCategory(payload as never, 'zh-CN', 1, 'nope')
    expect(result).toBeNull()
  })
})

describe('listPostsInCategory', () => {
  it('filters by category + status + brand visibility', async () => {
    const payload = makePayload([])
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
