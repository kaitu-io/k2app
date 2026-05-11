import { describe, it, expect, vi, beforeEach } from 'vitest'
import { lazyTranslate, makeLockKey, SOURCE_LOCALE } from '@/payload/lazyTranslate'

const mockTranslate = vi.hoisted(() => vi.fn())
vi.mock('@payload-enchants/translator', () => ({
  translateOperation: mockTranslate,
}))

type MockClient = {
  query: ReturnType<typeof vi.fn>
  release: ReturnType<typeof vi.fn>
}

function buildPayload(opts?: {
  lockResult?: boolean
  probeResult?: Record<string, unknown>
  poolPresent?: boolean
}) {
  const client: MockClient = {
    query: vi.fn().mockImplementation((sql: string) => {
      if (sql.includes('pg_try_advisory_lock')) {
        return Promise.resolve({ rows: [{ locked: opts?.lockResult ?? true }] })
      }
      if (sql.includes('pg_advisory_unlock')) {
        return Promise.resolve({ rows: [{ pg_advisory_unlock: true }] })
      }
      return Promise.resolve({ rows: [] })
    }),
    release: vi.fn(),
  }
  return {
    logger: { error: vi.fn(), warn: vi.fn(), info: vi.fn() },
    findByID: vi.fn().mockResolvedValue(opts?.probeResult ?? { id: 'p1', title: null, content: null }),
    db: opts?.poolPresent === false ? {} : {
      pool: { connect: vi.fn().mockResolvedValue(client) },
    },
    _client: client,
  } as any
}

beforeEach(() => {
  mockTranslate.mockReset()
})

describe('lazyTranslate', () => {
  it('returns already-translated when locale is the source locale', async () => {
    const payload = buildPayload()
    const result = await lazyTranslate({
      payload, collectionSlug: 'posts', docId: 'p1', locale: SOURCE_LOCALE,
    })
    expect(result.status).toBe('already-translated')
    expect(mockTranslate).not.toHaveBeenCalled()
  })

  it('translates the locale when advisory lock is acquired', async () => {
    const payload = buildPayload({ lockResult: true })
    mockTranslate.mockResolvedValue({ success: true })

    const result = await lazyTranslate({
      payload, collectionSlug: 'posts', docId: 'p1', locale: 'en-US',
    })

    expect(result).toEqual({ status: 'translated' })
    expect(mockTranslate).toHaveBeenCalledOnce()
    const args = mockTranslate.mock.calls[0][0]
    expect(args.locale).toBe('en-US')
    expect(args.localeFrom).toBe('zh-CN')
    expect(args.payload).toBe(payload)
    expect(args.update).toBe(true)
    // lock acquired + released; client returned to pool
    expect(payload._client.query).toHaveBeenCalledTimes(2)
    expect(payload._client.release).toHaveBeenCalledOnce()
  })

  it('returns locked-by-other and does not translate when another worker holds the lock', async () => {
    const payload = buildPayload({ lockResult: false })

    const result = await lazyTranslate({
      payload, collectionSlug: 'posts', docId: 'p1', locale: 'en-US',
    })

    expect(result.status).toBe('locked-by-other')
    expect(mockTranslate).not.toHaveBeenCalled()
    expect(payload._client.release).toHaveBeenCalledOnce()
  })

  it('skips translation when re-check probe reports already translated', async () => {
    const payload = buildPayload({
      lockResult: true,
      probeResult: { id: 'p1', title: 'Hello', content: { root: {} } },
    })

    const result = await lazyTranslate({
      payload,
      collectionSlug: 'posts',
      docId: 'p1',
      locale: 'en-US',
      isTranslated: (doc) => Boolean((doc as any).title && (doc as any).content),
    })

    expect(result.status).toBe('already-translated')
    expect(mockTranslate).not.toHaveBeenCalled()
    expect(payload._client.release).toHaveBeenCalledOnce()
  })

  it('returns timeout when translation exceeds the configured timeout', async () => {
    const payload = buildPayload({ lockResult: true })
    let translateResolve: ((v: unknown) => void) | null = null
    mockTranslate.mockImplementation(() => new Promise((resolve) => {
      translateResolve = resolve
    }))

    const result = await lazyTranslate({
      payload, collectionSlug: 'posts', docId: 'p1', locale: 'en-US',
      timeoutMs: 5,
    })

    expect(result.status).toBe('timeout')
    expect(payload.logger.error).toHaveBeenCalled()
    expect(payload._client.release).toHaveBeenCalledOnce()
    translateResolve?.({ success: true })
  })

  it('returns error when translateOperation throws', async () => {
    const payload = buildPayload({ lockResult: true })
    const boom = new Error('OpenRouter 503')
    mockTranslate.mockRejectedValue(boom)

    const result = await lazyTranslate({
      payload, collectionSlug: 'posts', docId: 'p1', locale: 'en-US',
    })

    expect(result.status).toBe('error')
    expect((result as any).error).toBe(boom)
    expect(payload.logger.error).toHaveBeenCalled()
    expect(payload._client.release).toHaveBeenCalledOnce()
  })

  it('still translates without lock when the pool is unavailable', async () => {
    const payload = buildPayload({ poolPresent: false })
    mockTranslate.mockResolvedValue({ success: true })

    const result = await lazyTranslate({
      payload, collectionSlug: 'posts', docId: 'p1', locale: 'en-US',
    })

    expect(result).toEqual({ status: 'translated' })
    expect(payload.logger.warn).toHaveBeenCalled()
  })

  it('releases the lock even when translateOperation throws', async () => {
    const payload = buildPayload({ lockResult: true })
    mockTranslate.mockRejectedValue(new Error('boom'))

    await lazyTranslate({
      payload, collectionSlug: 'posts', docId: 'p1', locale: 'en-US',
    })

    const unlockCall = payload._client.query.mock.calls.find(
      (c: any[]) => typeof c[0] === 'string' && c[0].includes('pg_advisory_unlock'),
    )
    expect(unlockCall).toBeDefined()
    expect(payload._client.release).toHaveBeenCalledOnce()
  })
})

describe('makeLockKey', () => {
  it('is deterministic for the same (collection, id, locale)', () => {
    const a = makeLockKey('posts', 1, 'en-US')
    const b = makeLockKey('posts', 1, 'en-US')
    expect(a).toBe(b)
  })

  it('differs per locale', () => {
    expect(makeLockKey('posts', 1, 'en-US')).not.toBe(makeLockKey('posts', 1, 'ja'))
  })

  it('differs per doc', () => {
    expect(makeLockKey('posts', 1, 'en-US')).not.toBe(makeLockKey('posts', 2, 'en-US'))
  })

  it('fits in signed bigint range', () => {
    const k = makeLockKey('posts', 'some-string-id', 'en-US')
    const min = -(BigInt(2) ** BigInt(63))
    const max = BigInt(2) ** BigInt(63)
    expect(k >= min).toBe(true)
    expect(k < max).toBe(true)
  })
})
