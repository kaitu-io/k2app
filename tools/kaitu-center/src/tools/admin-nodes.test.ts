import { describe, it, expect, vi } from 'vitest'
import type { CenterApiClient } from '../center-api.ts'
import { nodeTools } from './admin-nodes.ts'

vi.mock('../audit.ts', () => ({ audit: vi.fn().mockResolvedValue(undefined) }))

function invoke(params: Record<string, unknown>) {
  const tools: Record<string, { schema: Record<string, { safeParse: (v: unknown) => { success: boolean } }>; handler: Function }> = {}
  const server = { tool: (n: string, _d: string, schema: any, handler: Function) => { tools[n] = { schema, handler } } }
  const request = vi.fn().mockResolvedValue({ code: 0, data: {} })
  const reg = nodeTools.find((t) => t.name === 'update_node')
  if (!reg) throw new Error('no update_node')
  reg.register(server as any, { center: { request } as unknown as CenterApiClient })
  return { tool: tools['update_node'], request, run: () => tools['update_node'].handler(params) }
}

describe('update_node pool (trial line isolation)', () => {
  it('accepts only "" and "trial"', () => {
    const { tool } = invoke({ ipv4: '1.2.3.4' })
    expect(tool.schema.pool.safeParse('trial').success).toBe(true)
    expect(tool.schema.pool.safeParse('').success).toBe(true)
    expect(tool.schema.pool.safeParse(undefined).success).toBe(true)
    expect(tool.schema.pool.safeParse('vip').success).toBe(false)
  })

  it('sends pool in the PUT body, ipv4 in the path', async () => {
    const { request, run } = invoke({ ipv4: '1.2.3.4', pool: 'trial' })
    await run()
    const [path, opts] = request.mock.calls[0]
    expect(path).toBe('/app/nodes/1.2.3.4')
    expect(opts.method).toBe('PUT')
    expect(JSON.parse(opts.body)).toEqual({ pool: 'trial' })
  })

  it('can move a node back to the member pool with ""', async () => {
    const { request, run } = invoke({ ipv4: '1.2.3.4', pool: '' })
    await run()
    expect(JSON.parse(request.mock.calls[0][1].body)).toEqual({ pool: '' })
  })
})
