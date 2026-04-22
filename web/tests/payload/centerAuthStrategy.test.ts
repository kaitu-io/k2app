import { describe, it, expect, vi, beforeEach } from 'vitest'
import { centerAuthStrategy } from '@/payload/auth/centerAuthStrategy'

const makePayload = () => ({
  logger: { error: vi.fn(), info: vi.fn() },
  find: vi.fn().mockResolvedValue({ docs: [{ id: 'admin-1', email: 'a@b.c', centerId: 'uuid-123' }] }),
  create: vi.fn().mockResolvedValue({ id: 'admin-1' }),
}) as any

const makeCenterResponse = (overrides: Record<string, unknown> = {}) => ({
  ok: true,
  json: async () => ({
    code: 0,
    data: {
      uuid: 'uuid-123',
      loginIdentifies: [{ type: 'email', value: 'x@y.z' }],
      roles: 0,
      isAdmin: true,
      ...overrides,
    },
  }),
})

describe('centerAuthStrategy', () => {
  beforeEach(() => {
    vi.stubGlobal('fetch', vi.fn())
    process.env.CENTER_API_URL = 'https://center.test'
  })

  it('returns null user when no access_token cookie', async () => {
    const result = await centerAuthStrategy.authenticate({
      headers: new Headers(),
      payload: makePayload(),
    } as any)
    expect(result.user).toBeNull()
  })

  it('returns null user when center returns non-ok', async () => {
    ;(globalThis.fetch as any).mockResolvedValueOnce({ ok: false })
    const result = await centerAuthStrategy.authenticate({
      headers: new Headers({ cookie: 'access_token=abc' }),
      payload: makePayload(),
    } as any)
    expect(result.user).toBeNull()
  })

  it('returns null user when center returns non-admin', async () => {
    ;(globalThis.fetch as any).mockResolvedValueOnce(makeCenterResponse({
      isAdmin: false, roles: 1,
    }))
    const result = await centerAuthStrategy.authenticate({
      headers: new Headers({ cookie: 'access_token=abc' }),
      payload: makePayload(),
    } as any)
    expect(result.user).toBeNull()
  })

  it('returns admin user with collection when center returns admin', async () => {
    ;(globalThis.fetch as any).mockResolvedValueOnce(makeCenterResponse())
    const result = await centerAuthStrategy.authenticate({
      headers: new Headers({ cookie: 'access_token=abc' }),
      payload: makePayload(),
    } as any)
    expect(result.user).toMatchObject({ collection: 'admins', email: 'a@b.c' })
  })

  it('calls center /api/user/info with access_token cookie', async () => {
    ;(globalThis.fetch as any).mockResolvedValueOnce(makeCenterResponse())
    await centerAuthStrategy.authenticate({
      headers: new Headers({ cookie: 'access_token=tok-xyz' }),
      payload: makePayload(),
    } as any)
    expect(globalThis.fetch).toHaveBeenCalledWith(
      'https://center.test/api/user/info',
      { headers: { Cookie: 'access_token=tok-xyz' } },
    )
  })

  it('returns null when fetch throws (network error)', async () => {
    ;(globalThis.fetch as any).mockRejectedValueOnce(new Error('ECONNREFUSED'))
    const result = await centerAuthStrategy.authenticate({
      headers: new Headers({ cookie: 'access_token=abc' }),
      payload: makePayload(),
    } as any)
    expect(result.user).toBeNull()
  })

  it('returns null when CENTER_API_URL is unset', async () => {
    delete process.env.CENTER_API_URL
    const result = await centerAuthStrategy.authenticate({
      headers: new Headers({ cookie: 'access_token=abc' }),
      payload: makePayload(),
    } as any)
    expect(result.user).toBeNull()
  })

  it('forwards X-Access-Key header when cookie absent', async () => {
    ;(globalThis.fetch as any).mockResolvedValueOnce(makeCenterResponse())
    await centerAuthStrategy.authenticate({
      headers: new Headers({ 'x-access-key': 'ktu_abc' }),
      payload: makePayload(),
    } as any)
    expect(globalThis.fetch).toHaveBeenCalledWith(
      'https://center.test/api/user/info',
      { headers: { 'X-Access-Key': 'ktu_abc' } },
    )
  })

  it('prefers cookie when both cookie and X-Access-Key present', async () => {
    ;(globalThis.fetch as any).mockResolvedValueOnce(makeCenterResponse())
    await centerAuthStrategy.authenticate({
      headers: new Headers({
        cookie: 'access_token=tok-xyz',
        'x-access-key': 'ktu_abc',
      }),
      payload: makePayload(),
    } as any)
    expect(globalThis.fetch).toHaveBeenCalledWith(
      'https://center.test/api/user/info',
      { headers: { Cookie: 'access_token=tok-xyz' } },
    )
  })

  it('returns null user when neither cookie nor X-Access-Key present', async () => {
    const result = await centerAuthStrategy.authenticate({
      headers: new Headers(),
      payload: makePayload(),
    } as any)
    expect(result.user).toBeNull()
    expect(globalThis.fetch).not.toHaveBeenCalled()
  })

  it('reads X-Access-Key case-insensitively', async () => {
    ;(globalThis.fetch as any).mockResolvedValueOnce(makeCenterResponse())
    await centerAuthStrategy.authenticate({
      headers: new Headers({ 'X-Access-Key': 'ktu_upper' }),
      payload: makePayload(),
    } as any)
    expect(globalThis.fetch).toHaveBeenCalledWith(
      'https://center.test/api/user/info',
      { headers: { 'X-Access-Key': 'ktu_upper' } },
    )
  })
})
