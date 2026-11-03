import { describe, it, expect } from 'vitest'
import { pushTools } from './admin-push.ts'

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
  for (const reg of pushTools) reg.register(fakeServer as never, fakeClients)
  return { handlers, calls }
}

describe('push tools', () => {
  it('are in the edm (marketing) permission group', () => {
    for (const t of pushTools) expect(t.group).toBe('edm')
  })

  it('send_push posts camelCase body and never sends a category', async () => {
    const { handlers, calls } = capture()
    await handlers.get('send_push')!({
      batch_id: 'b1', brand: 'kaitu', user_ids: [1, 2], route: '/purchase', dry_run: true,
      copy: { 'zh-CN': { title: 't', body: 'b' } },
    })
    expect(calls[0].path).toBe('/app/push/send')
    expect(calls[0].init?.method).toBe('POST')
    const body = JSON.parse(String(calls[0].init?.body))
    expect(body).toEqual({
      batchId: 'b1', brand: 'kaitu', userIds: [1, 2], route: '/purchase', dryRun: true,
      copy: { 'zh-CN': { title: 't', body: 'b' } },
    })
    expect(body).not.toHaveProperty('category')
  })

  it('get_push_batch_stats encodes the batch id in the path', async () => {
    const { handlers, calls } = capture()
    await handlers.get('get_push_batch_stats')!({ batch_id: 'push:2026-11-05.x' })
    expect(calls[0].path).toBe('/app/push/batches/push%3A2026-11-05.x')
  })
})
