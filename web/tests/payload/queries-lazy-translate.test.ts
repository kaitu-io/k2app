import { describe, it, expect, vi, beforeEach } from 'vitest'

// @payload-enchants/translator has an ESM dir-import bug; stub before any
// transitive import (queries.ts → lazyTranslate.ts → translator).
vi.mock('@payload-enchants/translator', () => ({ translateOperation: vi.fn() }))

const mockLazyTranslate = vi.hoisted(() => vi.fn())
vi.mock('@/payload/lazyTranslate', async () => {
  const actual = await vi.importActual<typeof import('@/payload/lazyTranslate')>('@/payload/lazyTranslate')
  return {
    ...actual,
    lazyTranslate: mockLazyTranslate,
  }
})

import { findPostInCategory, listPostsInCategory } from '@/app/[locale]/[...slug]/queries'

function buildPayload(opts: {
  probeDoc?: Record<string, unknown> | null
  finalDoc?: Record<string, unknown>
  listDocs?: Record<string, unknown>[]
}) {
  return {
    find: vi.fn().mockImplementation((args: any) => {
      if (args.fallbackLocale === false) {
        return Promise.resolve({ docs: opts.probeDoc === null ? [] : opts.probeDoc ? [opts.probeDoc] : [] })
      }
      return Promise.resolve({ docs: opts.listDocs ?? [] })
    }),
    findByID: vi.fn().mockResolvedValue(opts.finalDoc ?? { id: 'p1' }),
  } as any
}

beforeEach(() => {
  mockLazyTranslate.mockReset()
})

describe('findPostInCategory — lazy translate integration', () => {
  it('returns null when probe finds no doc', async () => {
    const payload = buildPayload({ probeDoc: null })
    const result = await findPostInCategory(payload, 'en-US', 1, 'unknown-slug')
    expect(result).toBeNull()
    expect(mockLazyTranslate).not.toHaveBeenCalled()
    expect(payload.findByID).not.toHaveBeenCalled()
  })

  it('does NOT trigger lazyTranslate when locale is the source (zh-CN)', async () => {
    const payload = buildPayload({
      probeDoc: { id: 'p1', title: '标题', content: { root: {} } },
      finalDoc: { id: 'p1', title: '标题', content: { root: {} }, slug: 's', showOnKaitu: true, showOnOverleap: true },
    })
    const result = await findPostInCategory(payload, 'zh-CN', 1, 's')
    expect(mockLazyTranslate).not.toHaveBeenCalled()
    expect(result).toBeDefined()
    expect(payload.findByID).toHaveBeenCalledOnce()
  })

  it('does NOT trigger lazyTranslate when the locale already has a translation', async () => {
    const payload = buildPayload({
      probeDoc: { id: 'p1', title: 'Title', content: { root: { children: [] } } },
      finalDoc: { id: 'p1', title: 'Title', content: { root: {} }, slug: 's', showOnKaitu: true, showOnOverleap: true },
    })
    const result = await findPostInCategory(payload, 'en-US', 1, 's')
    expect(mockLazyTranslate).not.toHaveBeenCalled()
    expect(result).toBeDefined()
  })

  it('triggers lazyTranslate when the non-source locale title is empty', async () => {
    const payload = buildPayload({
      probeDoc: { id: 'p1', title: null, content: null },
      finalDoc: { id: 'p1', title: 'Translated', content: { root: {} }, slug: 's', showOnKaitu: true, showOnOverleap: true },
    })
    mockLazyTranslate.mockResolvedValue({ status: 'translated' })

    const result = await findPostInCategory(payload, 'en-US', 1, 's')

    expect(mockLazyTranslate).toHaveBeenCalledOnce()
    const args = mockLazyTranslate.mock.calls[0][0]
    expect(args.collectionSlug).toBe('posts')
    expect(args.docId).toBe('p1')
    expect(args.locale).toBe('en-US')
    expect(args.payload).toBe(payload)
    // Final fetch happens with default fallback so caller gets translation OR source fallback
    expect(payload.findByID).toHaveBeenCalledOnce()
    const finalArgs = payload.findByID.mock.calls[0][0]
    expect(finalArgs.locale).toBe('en-US')
    expect(finalArgs.fallbackLocale).toBeUndefined()
    expect(result?.title).toBe('Translated')
  })

  it('still re-fetches and returns source-fallback when lazyTranslate reports locked-by-other', async () => {
    const payload = buildPayload({
      probeDoc: { id: 'p1', title: null, content: null },
      finalDoc: { id: 'p1', title: '中文标题(回退)', content: { root: {} }, slug: 's', showOnKaitu: true, showOnOverleap: true },
    })
    mockLazyTranslate.mockResolvedValue({ status: 'locked-by-other' })

    const result = await findPostInCategory(payload, 'en-US', 1, 's')

    expect(result?.title).toBe('中文标题(回退)')
    expect(payload.findByID).toHaveBeenCalledOnce()
  })

  it('still re-fetches and returns source-fallback when lazyTranslate times out', async () => {
    const payload = buildPayload({
      probeDoc: { id: 'p1', title: null, content: null },
      finalDoc: { id: 'p1', title: '中文标题', content: { root: {} }, slug: 's', showOnKaitu: true, showOnOverleap: true },
    })
    mockLazyTranslate.mockResolvedValue({ status: 'timeout' })

    const result = await findPostInCategory(payload, 'en-US', 1, 's')
    expect(result?.title).toBe('中文标题')
  })
})

describe('listPostsInCategory — does NOT trigger lazy translate', () => {
  it('returns docs from find without invoking lazyTranslate', async () => {
    const payload = buildPayload({
      listDocs: [
        { id: 'p1', slug: 's1', title: 't1', publishedAt: '2026-01-01', showOnKaitu: true, showOnOverleap: false },
        { id: 'p2', slug: 's2', title: 't2', publishedAt: '2026-01-02', showOnKaitu: true, showOnOverleap: false },
      ],
    })
    const result = await listPostsInCategory(payload, 'en-US', 1, 'showOnKaitu')
    expect(result).toHaveLength(2)
    expect(mockLazyTranslate).not.toHaveBeenCalled()
    // Sanity: list call did not pass fallbackLocale:false
    const findCall = payload.find.mock.calls[0][0]
    expect(findCall.fallbackLocale).toBeUndefined()
  })
})
