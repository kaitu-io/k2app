import { describe, it, expect } from 'vitest'
import { provisionJobTools } from './admin-provision-jobs.ts'

/**
 * Structural assertions for the provision-job tool registrations.
 *
 * The factory (`defineApiTool`) doesn't expose method/path on the returned
 * ToolRegistration, so we assert the wiring by registering each tool against a
 * fake McpServer and capturing the def, plus the stable name/group fields on
 * the registration itself.
 */

interface CapturedTool {
  name: string
  description: string
}

function captureRegistrations() {
  const captured: CapturedTool[] = []
  const fakeServer = {
    tool(name: string, description: string) {
      captured.push({ name, description })
    },
  }
  const fakeClients = { center: {}, cms: {} } as never
  for (const reg of provisionJobTools) {
    reg.register(fakeServer as never, fakeClients)
  }
  return captured
}

describe('provisionJobTools', () => {
  it('registers exactly the three provisioning tools with correct groups', () => {
    const byName = new Map(provisionJobTools.map((t) => [t.name, t]))

    expect(provisionJobTools).toHaveLength(3)
    expect(byName.get('list_provisioning_intents')?.group).toBe('cloud')
    expect(byName.get('claim_provisioning_intent')?.group).toBe('cloud.write')
    expect(byName.get('report_provisioning')?.group).toBe('cloud.write')
  })

  it('registers each tool name on the server with a description', () => {
    const captured = captureRegistrations()
    const names = captured.map((c) => c.name).sort()
    expect(names).toEqual([
      'claim_provisioning_intent',
      'list_provisioning_intents',
      'report_provisioning',
    ])
    for (const c of captured) {
      expect(c.description.length).toBeGreaterThan(0)
    }
  })

  it('warns about the one-time claim token in the claim tool description', () => {
    const captured = captureRegistrations()
    const claim = captured.find((c) => c.name === 'claim_provisioning_intent')
    expect(claim?.description).toContain('K2_PRIVATE_CLAIM')
  })
})
