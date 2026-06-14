import { describe, it, expect } from 'vitest'
import { nodeOperationTools } from './admin-node-operations.ts'

/**
 * Structural assertions for the node-operation tool registrations.
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
  for (const reg of nodeOperationTools) {
    reg.register(fakeServer as never, fakeClients)
  }
  return captured
}

describe('nodeOperationTools', () => {
  it('registers exactly the four node-operation tools with correct groups', () => {
    const byName = new Map(nodeOperationTools.map((t) => [t.name, t]))

    expect(nodeOperationTools).toHaveLength(4)
    expect(byName.get('list_node_operations')?.group).toBe('cloud')
    expect(byName.get('create_node_operation')?.group).toBe('cloud.write')
    expect(byName.get('claim_node_operation')?.group).toBe('cloud.write')
    expect(byName.get('update_node_operation')?.group).toBe('cloud.write')
  })

  it('registers each tool name on the server with a description', () => {
    const captured = captureRegistrations()
    const names = captured.map((c) => c.name).sort()
    expect(names).toEqual([
      'claim_node_operation',
      'create_node_operation',
      'list_node_operations',
      'update_node_operation',
    ])
    for (const c of captured) {
      expect(c.description.length).toBeGreaterThan(0)
    }
  })

  it('warns about the one-time claim token in the claim tool description', () => {
    const captured = captureRegistrations()
    const claim = captured.find((c) => c.name === 'claim_node_operation')
    expect(claim?.description).toContain('K2_PRIVATE_CLAIM')
  })
})
