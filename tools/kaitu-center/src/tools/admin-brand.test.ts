import { describe, it, expect, vi } from 'vitest'
import type { CenterApiClient } from '../center-api.ts'
import type { ToolRegistration } from '../tool-factory.ts'
import { planTools } from './admin-plans.ts'
import { campaignTools } from './admin-campaigns.ts'
import { announcementTools } from './admin-announcements.ts'
import { licenseKeyTools } from './admin-license-keys.ts'
import { edmTools } from './admin-edm.ts'
import { userTools } from './admin-users.ts'
import { orderTools } from './admin-orders.ts'
import { feedbackTicketTools } from './admin-feedback-tickets.ts'
import { statsTools } from './admin-stats.ts'

vi.mock('../audit.ts', () => ({ audit: vi.fn().mockResolvedValue(undefined) }))

/**
 * Center /app/* is cross-brand (kaitu + overleap). Creates of brand-owned
 * entities must carry an explicit brand (the API rejects an empty one), and
 * list/stat tools must be able to narrow by ?brand=.
 */

const all: ToolRegistration[] = [
  ...planTools, ...campaignTools, ...announcementTools, ...licenseKeyTools, ...edmTools,
  ...userTools, ...orderTools, ...feedbackTicketTools, ...statsTools,
]

function invoke(name: string, params: Record<string, unknown>) {
  const tools: Record<string, { schema: Record<string, { safeParse: (v: unknown) => { success: boolean } }>; handler: Function }> = {}
  const server = { tool: (n: string, _d: string, schema: any, handler: Function) => { tools[n] = { schema, handler } } }
  const request = vi.fn().mockResolvedValue({ code: 0, data: {} })
  const reg = all.find((t) => t.name === name)
  if (!reg) throw new Error(`no tool ${name}`)
  reg.register(server as any, { center: { request } as unknown as CenterApiClient })
  return { tool: tools[name], request, run: () => tools[name].handler(params) }
}

describe('brand-owned creates require brand and send it', () => {
  const creates: Array<[string, Record<string, unknown>]> = [
    ['create_plan', { pid: 'p', label: 'l', price: 1, month: 1 }],
    ['create_campaign', { code: 'C', name: 'n' }],
    ['create_announcement', { message: 'm' }],
    ['create_license_key_batch', { name: 'n', recipient_matcher: 'all', plan_days: 1, quantity: 1, expires_in_days: 1 }],
    ['create_edm_template', { name: 'n', language: 'en-US', subject: 's', content: 'c' }],
  ]
  for (const [name, params] of creates) {
    it(`${name}: brand param is required`, () => {
      const { tool } = invoke(name, params)
      expect(tool.schema.brand).toBeDefined()
      expect(tool.schema.brand.safeParse(undefined).success).toBe(false)
      expect(tool.schema.brand.safeParse('nonsense').success).toBe(false)
    })
    it(`${name}: brand is sent in the body`, async () => {
      const { request, run } = invoke(name, { ...params, brand: 'overleap' })
      await run()
      const opts = request.mock.calls[0][1]
      expect(JSON.parse(opts.body).brand).toBe('overleap')
    })
  }
})

describe('list/stat tools accept ?brand=', () => {
  const lists = [
    'list_admin_plans', 'list_campaigns', 'list_announcements', 'list_license_key_batches',
    'list_license_keys', 'list_edm_templates', 'lookup_user', 'list_orders',
    'query_feedback_tickets', 'device_statistics', 'user_statistics', 'order_statistics',
    'funnel', 'retention',
  ]
  for (const name of lists) {
    it(`${name} forwards brand`, async () => {
      const extra: Record<string, unknown> = name === 'funnel' ? { key: 'web_purchase' } : name === 'retention' ? { metric: 'paid' } : {}
      const { tool, request, run } = invoke(name, { brand: 'overleap', ...extra })
      expect(tool.schema.brand?.safeParse(undefined).success).toBe(true)
      await run()
      expect(request.mock.calls[0][0]).toContain('brand=overleap')
    })
  }
})

describe('send_templated_email', () => {
  it('forwards batch brand', async () => {
    const { request, run } = invoke('send_templated_email', {
      batch_id: 'b', brand: 'overleap', items: [{ email: 'a@x', slug: 's' }],
    })
    await run()
    expect(JSON.parse(request.mock.calls[0][1].body).brand).toBe('overleap')
  })
})

describe('list_orders email filter', () => {
  it('maps email to the loginProvider/loginIdentity pair the API reads', async () => {
    const { request, run } = invoke('list_orders', { email: 'a@x.com' })
    await run()
    const path = request.mock.calls[0][0] as string
    expect(path).toContain('loginProvider=email')
    expect(path).toContain('loginIdentity=a%40x.com')
  })
})

describe('funnel / retention tools', () => {
  it('funnel_paths hits the list endpoint', async () => {
    const { request, run } = invoke('funnel_paths', {})
    await run()
    expect(request.mock.calls[0][0]).toBe('/app/stats/funnels')
  })
  it('funnel puts key in the path and maps group_by to groupBy', async () => {
    const { request, run } = invoke('funnel', { key: 'web_purchase', brand: 'overleap', group_by: 'source', from: '2026-09-01', to: '2026-09-30' })
    await run()
    const path = request.mock.calls[0][0] as string
    expect(path.startsWith('/app/stats/funnels/web_purchase?')).toBe(true)
    expect(path).toContain('brand=overleap')
    expect(path).toContain('groupBy=source')
    expect(path).toContain('from=2026-09-01')
    expect(path).toContain('to=2026-09-30')
    expect(path).not.toContain('key=')
    expect(path).not.toContain('group_by')
  })
  it('retention sends metric and months', async () => {
    const { tool, request, run } = invoke('retention', { metric: 'paid', months: 6 })
    await run()
    const path = request.mock.calls[0][0] as string
    expect(path).toContain('/app/stats/retention')
    expect(path).toContain('metric=paid')
    expect(path).toContain('months=6')
    expect(tool.schema.metric.safeParse('nonsense').success).toBe(false)
    expect(tool.schema.months.safeParse(25).success).toBe(false)
  })
})
