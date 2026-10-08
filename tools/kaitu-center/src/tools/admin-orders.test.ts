import { describe, it, expect } from 'vitest'
import { orderTools, toUnixSeconds } from './admin-orders.ts'

type Handler = (params: Record<string, unknown>) => Promise<unknown>

function capture() {
  const handlers = new Map<string, Handler>()
  const calls: { path: string; init?: RequestInit }[] = []
  const fakeServer = {
    tool(name: string, _d: string, _s: unknown, h: Handler) {
      handlers.set(name, h)
    },
  }
  const fakeClients = {
    center: {
      request: async (path: string, init?: RequestInit) => {
        calls.push({ path, init })
        return { code: 0, data: {} }
      },
    },
  } as never
  for (const reg of orderTools) reg.register(fakeServer as never, fakeClients)
  return { handlers, calls }
}

describe('stripe withdrawal tools', () => {
  it('converts notice_at (ISO or unix) to unix seconds', () => {
    expect(toUnixSeconds('1790000000')).toBe(1790000000)
    expect(toUnixSeconds('2026-10-07T10:00:00+07:00')).toBe(Date.UTC(2026, 9, 7, 3) / 1000)
    expect(() => toUnixSeconds('yesterday')).toThrow()
  })

  it('registers quote (read) and withdraw/abandon (write) with the right groups', () => {
    const byName = new Map(orderTools.map((t) => [t.name, t]))
    expect(byName.get('quote_stripe_withdrawal')?.group).toBe('orders')
    expect(byName.get('withdraw_stripe_subscription')?.group).toBe('orders.write')
    expect(byName.get('abandon_stripe_withdrawal')?.group).toBe('orders.write')
  })

  it('quote sends notice_at as unix seconds on the query', async () => {
    const { handlers, calls } = capture()
    await handlers.get('quote_stripe_withdrawal')!({ uuid: 'user-1', notice_at: '2026-10-07T03:00:00Z', mode: 'withdrawal' })
    expect(calls[0].path).toContain('/app/users/user-1/stripe-withdrawal?')
    expect(calls[0].path).toContain(`notice_at=${Date.UTC(2026, 9, 7, 3) / 1000}`)
  })

  it('withdraw posts camelCase body with request_id for resume', async () => {
    const { handlers, calls } = capture()
    await handlers.get('withdraw_stripe_subscription')!({
      uuid: 'user-1', notice_at: '1790000000', mode: 'withdrawal', request_id: 'wdr_x',
    })
    expect(calls[0].path).toBe('/app/users/user-1/stripe-withdrawal')
    expect(calls[0].init?.method).toBe('POST')
    expect(JSON.parse(String(calls[0].init?.body))).toMatchObject({ noticeAt: 1790000000, mode: 'withdrawal', requestId: 'wdr_x' })
  })

  it('abandon posts to the request path', async () => {
    const { handlers, calls } = capture()
    await handlers.get('abandon_stripe_withdrawal')!({ request_id: 'wdr_x', reason: 'stuck' })
    expect(calls[0].path).toBe('/app/stripe-withdrawals/wdr_x/abandon')
  })
})

describe('refund_order', () => {
  it('sends keepInviteRewards (default false) in the body', async () => {
    const { handlers, calls } = capture()
    await handlers.get('refund_order')!({ uuid: 'ord-1', reason: 'goodwill' })
    expect(JSON.parse(String(calls[0].init?.body))).toEqual({ reason: 'goodwill', keepInviteRewards: false })
    await handlers.get('refund_order')!({ uuid: 'ord-1', reason: 'outage', keep_invite_rewards: true })
    expect(JSON.parse(String(calls[1].init?.body))).toEqual({ reason: 'outage', keepInviteRewards: true })
  })
})
