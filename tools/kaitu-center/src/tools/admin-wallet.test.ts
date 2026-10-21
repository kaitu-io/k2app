import { describe, it, expect } from 'vitest'
import { walletTools, redactAlipayPayees, ALIPAY_REDACTED } from './admin-wallet.ts'

type Handler = (params: Record<string, unknown>) => Promise<{ content: { text: string }[] }>

const listResponse = {
  items: [
    { id: 1, account: { accountType: 'alipay', accountId: '13812345678', realName: '张三', currency: 'cny' } },
    { id: 2, account: { accountType: 'paypal', accountId: 'a@b.com', currency: 'usd' } },
  ],
  pagination: { page: 1, pageSize: 10, total: 2 },
}

describe('list_withdraws', () => {
  it('never hands decrypted Alipay payee info to the model', async () => {
    const handlers = new Map<string, Handler>()
    const fakeServer = { tool(name: string, _d: string, _s: unknown, h: Handler) { handlers.set(name, h) } }
    const fakeClients = { center: { request: async () => ({ code: 0, data: listResponse }) } } as never
    for (const reg of walletTools) reg.register(fakeServer as never, fakeClients)

    const out = (await handlers.get('list_withdraws')!({})).content[0].text
    expect(out).not.toContain('13812345678')
    expect(out).not.toContain('张三')
    expect(out).toContain(ALIPAY_REDACTED)
    expect(out).toContain('a@b.com') // other channels untouched
  })

  it('leaves non-list payloads alone', () => {
    expect(redactAlipayPayees(null)).toBeNull()
    expect(redactAlipayPayees({ foo: 1 })).toEqual({ foo: 1 })
  })
})
