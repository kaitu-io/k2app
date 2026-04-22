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
    })).resolves.toBeDefined()
  })
})
