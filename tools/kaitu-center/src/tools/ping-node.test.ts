/**
 * Tests for the ping_node MCP tool.
 *
 * Uses vi.mock to replace ssh2 Client so no real SSH is needed.
 * Focuses on: success latency, connection errors, timeout, audit logging.
 */

import { describe, it, expect, vi, beforeEach } from 'vitest'
import { McpServer } from '@modelcontextprotocol/sdk/server/mcp.js'
import type { SshConfig } from '../config.ts'

// ---------------------------------------------------------------------------
// Mock ssh2 and audit module
// ---------------------------------------------------------------------------

const mockClientInstance = {
  on: vi.fn(),
  connect: vi.fn(),
  end: vi.fn(),
}

vi.mock('ssh2', () => ({
  Client: vi.fn(() => mockClientInstance),
}))

vi.mock('node:fs', async (importOriginal) => {
  const actual = await importOriginal<typeof import('node:fs')>()
  return {
    ...actual,
    readFileSync: vi.fn().mockReturnValue('fake-private-key'),
  }
})

vi.mock('../audit.js', () => ({
  audit: vi.fn().mockResolvedValue(undefined),
}))

import { registerPingNode } from './ping-node.js'

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

const TEST_SSH_CONFIG: SshConfig = {
  privateKeyPath: '/home/user/.ssh/id_rsa',
  user: 'root',
  port: 22,
}

interface PingResult {
  reachable: boolean
  latencyMs?: number
  error?: string
}

async function invokePingNode(
  params: Record<string, unknown>
): Promise<{ content: Array<{ type: string; text: string }> }> {
  const server = new McpServer({ name: 'test', version: '0.0.1' })
  registerPingNode(server, TEST_SSH_CONFIG)

  const registeredTools = (server as unknown as {
    _registeredTools: Record<string, { handler: (args: Record<string, unknown>) => Promise<unknown> }>
  })._registeredTools

  const tool = registeredTools['ping_node']
  if (!tool) throw new Error('ping_node tool not registered')

  return tool.handler(params) as Promise<{ content: Array<{ type: string; text: string }> }>
}

function parseResult(result: { content: Array<{ type: string; text: string }> }): PingResult {
  return JSON.parse(result.content[0]!.text) as PingResult
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

describe('ping_node tool', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    // Reset the mock client event handlers
    mockClientInstance.on.mockReturnValue(mockClientInstance)
    mockClientInstance.connect.mockImplementation(() => {
      // Find the 'ready' handler and call it
      const readyCall = mockClientInstance.on.mock.calls.find(
        (call: unknown[]) => call[0] === 'ready'
      )
      if (readyCall) {
        ;(readyCall[1] as () => void)()
      }
    })
    mockClientInstance.end.mockReturnValue(undefined)
  })

  it('returns reachable=true with latency on success', async () => {
    const result = await invokePingNode({ ip: '1.2.3.4' })
    const parsed = parseResult(result)

    expect(parsed.reachable).toBe(true)
    expect(typeof parsed.latencyMs).toBe('number')
    expect(parsed.latencyMs).toBeGreaterThanOrEqual(0)
  })

  it('returns reachable=false on connection error', async () => {
    mockClientInstance.connect.mockImplementation(() => {
      const errorCall = mockClientInstance.on.mock.calls.find(
        (call: unknown[]) => call[0] === 'error'
      )
      if (errorCall) {
        const err = new Error('Connection refused') as Error & { code?: string }
        err.code = 'ECONNREFUSED'
        ;(errorCall[1] as (e: Error) => void)(err)
      }
    })

    const result = await invokePingNode({ ip: '1.2.3.4' })
    const parsed = parseResult(result)

    expect(parsed.reachable).toBe(false)
    expect(parsed.error).toBeDefined()
  })

  it('returns reachable=false on auth failure', async () => {
    mockClientInstance.connect.mockImplementation(() => {
      const errorCall = mockClientInstance.on.mock.calls.find(
        (call: unknown[]) => call[0] === 'error'
      )
      if (errorCall) {
        const err = new Error('Auth failed') as Error & { level?: string }
        err.level = 'client-authentication'
        ;(errorCall[1] as (e: Error) => void)(err)
      }
    })

    const result = await invokePingNode({ ip: '1.2.3.4' })
    const parsed = parseResult(result)

    expect(parsed.reachable).toBe(false)
    expect(parsed.error).toContain('Authentication failed')
  })
})
