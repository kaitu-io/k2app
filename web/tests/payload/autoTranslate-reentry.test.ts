import { describe, it, expect, vi } from 'vitest'
import { autoTranslate } from '@/payload/hooks/autoTranslate'

const basePayload = () => ({
  logger: { error: vi.fn(), info: vi.fn(), warn: vi.fn() },
  config: {
    localization: {
      locales: [
        { code: 'zh-CN' }, { code: 'en-US' }, { code: 'ja' },
      ],
    },
  },
  db: {
    updateOne: vi.fn().mockResolvedValue({}),
  },
}) as any

const baseReq = (locale: string, payload = basePayload()) => ({
  locale,
  payload,
}) as any

describe('autoTranslate hook (lazy-load cache invalidation)', () => {
  it('short-circuits when req.locale is not zh-CN (re-entry case)', async () => {
    const payload = basePayload()
    await autoTranslate({
      collection: { slug: 'posts' } as any,
      doc: { id: 'p1' } as any,
      req: baseReq('en-US', payload),
      operation: 'update',
      previousDoc: {} as any,
      context: {} as any,
      data: {} as any,
    })
    expect(payload.db.updateOne).not.toHaveBeenCalled()
  })

  it('clears stale translations for every non-source locale on zh-CN write', async () => {
    const payload = basePayload()
    await autoTranslate({
      collection: { slug: 'posts' } as any,
      doc: { id: 'p1' } as any,
      req: baseReq('zh-CN', payload),
      operation: 'update',
      previousDoc: {} as any,
      context: {} as any,
      data: {} as any,
    })
    expect(payload.db.updateOne).toHaveBeenCalledTimes(2)
    const locales = payload.db.updateOne.mock.calls.map((c: any[]) => c[0].locale).sort()
    expect(locales).toEqual(['en-US', 'ja'])
    for (const call of payload.db.updateOne.mock.calls) {
      expect(call[0].data).toEqual({ title: null, excerpt: null, content: null })
      expect(call[0].id).toBe('p1')
      expect(call[0].collection).toBe('posts')
    }
  })

  it('does not throw when clearing one locale fails — continues with the rest', async () => {
    const payload = basePayload()
    payload.db.updateOne = vi.fn()
      .mockRejectedValueOnce(new Error('connection lost'))
      .mockResolvedValueOnce({})
    await expect(autoTranslate({
      collection: { slug: 'posts' } as any,
      doc: { id: 'p1' } as any,
      req: baseReq('zh-CN', payload),
      operation: 'update',
      previousDoc: {} as any,
      context: {} as any,
      data: {} as any,
    })).resolves.toBeDefined()
    expect(payload.db.updateOne).toHaveBeenCalledTimes(2)
    expect(payload.logger.error).toHaveBeenCalledOnce()
  })

  it('runs sequentially so concurrent updates on the outer transaction do not collide', async () => {
    const payload = basePayload()
    const inflight = new Set<string>()
    const collisions: string[] = []
    payload.db.updateOne = vi.fn().mockImplementation(async ({ locale }: { locale: string }) => {
      if (inflight.size > 0) collisions.push(locale)
      inflight.add(locale)
      await new Promise(r => setTimeout(r, 3))
      inflight.delete(locale)
      return {}
    })
    await autoTranslate({
      collection: { slug: 'posts' } as any,
      doc: { id: 'p1' } as any,
      req: baseReq('zh-CN', payload),
      operation: 'update',
      previousDoc: {} as any,
      context: {} as any,
      data: {} as any,
    })
    expect(collisions).toEqual([])
  })
})
