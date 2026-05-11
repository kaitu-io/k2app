import { describe, it, expect, vi } from 'vitest'
import { autoTranslate } from '@/payload/hooks/autoTranslate'

const mockTranslate = vi.hoisted(() => vi.fn())
vi.mock('@payload-enchants/translator', () => ({
  translateOperation: mockTranslate,
}))

const baseReq = (locale: string) => ({
  locale,
  payload: {
    logger: { error: vi.fn(), info: vi.fn() },
    config: {
      localization: {
        locales: [
          { code: 'zh-CN' }, { code: 'en-US' }, { code: 'ja' },
        ],
      },
    },
  },
}) as any

describe('autoTranslate', () => {
  it('short-circuits when req.locale is not zh-CN (re-entry case)', async () => {
    mockTranslate.mockClear()
    await autoTranslate({
      collection: { slug: 'posts' } as any,
      doc: { id: 'p1' } as any,
      req: baseReq('en-US'),
      operation: 'update',
      previousDoc: {} as any,
      context: {} as any,
      data: {} as any,
    })
    expect(mockTranslate).not.toHaveBeenCalled()
  })

  it('fans out to all non-source locales when source is zh-CN', async () => {
    mockTranslate.mockClear().mockResolvedValue({ success: true })
    await autoTranslate({
      collection: { slug: 'posts' } as any,
      doc: { id: 'p1' } as any,
      req: baseReq('zh-CN'),
      operation: 'update',
      previousDoc: {} as any,
      context: {} as any,
      data: {} as any,
    })
    expect(mockTranslate).toHaveBeenCalledTimes(2)
    const locales = mockTranslate.mock.calls.map(c => c[0].locale).sort()
    expect(locales).toEqual(['en-US', 'ja'])
  })

  it('does not throw when a single locale translation fails', async () => {
    mockTranslate.mockClear()
      .mockResolvedValueOnce({ success: true })
      .mockRejectedValueOnce(new Error('upstream down'))
    await expect(autoTranslate({
      collection: { slug: 'posts' } as any,
      doc: { id: 'p1' } as any,
      req: baseReq('zh-CN'),
      operation: 'update',
      previousDoc: {} as any,
      context: {} as any,
      data: {} as any,
    })).resolves.toBeDefined()
  })

  it('fans out SERIALLY — one translate in flight at a time', async () => {
    // Parallel fan-out (Promise.allSettled) shares the outer req's Postgres
    // transaction across concurrent payload.update calls and aborts the txn,
    // rolling back the entire create. Serial execution avoids the conflict.
    const inflight = new Set<string>()
    const collisions: string[] = []
    mockTranslate.mockClear().mockImplementation(async ({ locale }: { locale: string }) => {
      if (inflight.size > 0) collisions.push(locale)
      inflight.add(locale)
      await new Promise(r => setTimeout(r, 3))
      inflight.delete(locale)
      return { success: true }
    })
    await autoTranslate({
      collection: { slug: 'posts' } as any,
      doc: { id: 'p1' } as any,
      req: baseReq('zh-CN'),
      operation: 'update',
      previousDoc: {} as any,
      context: {} as any,
      data: {} as any,
    })
    expect(collisions).toEqual([])
  })

  it('continues subsequent locales even when an earlier one throws', async () => {
    const seen: string[] = []
    mockTranslate.mockClear().mockImplementation(async ({ locale }: { locale: string }) => {
      seen.push(locale)
      if (locale === 'en-US') throw new Error('boom')
      return { success: true }
    })
    await autoTranslate({
      collection: { slug: 'posts' } as any,
      doc: { id: 'p1' } as any,
      req: baseReq('zh-CN'),
      operation: 'update',
      previousDoc: {} as any,
      context: {} as any,
      data: {} as any,
    })
    expect(seen.sort()).toEqual(['en-US', 'ja'])
  })
})
