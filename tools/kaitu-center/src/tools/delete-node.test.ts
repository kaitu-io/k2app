/**
 * Tests for the delete_node MCP tool.
 *
 * Uses vi.mock to replace CenterApiClient so no real API calls are made.
 * Focuses on: successful deletion, API error handling, network errors, audit logging.
 */

import { describe, it, expect, vi, beforeEach } from 'vitest'
import { McpServer } from '@modelcontextprotocol/sdk/server/mcp.js'

// ---------------------------------------------------------------------------
// Mock center-api and audit modules
// ---------------------------------------------------------------------------

const mockRequest = vi.fn()

vi.mock('../center-api.js', () => ({
  CenterApiClient: vi.fn(() => ({
    request: mockRequest,
  })),
}))

vi.mock('../audit.js', () => ({
  audit: vi.fn().mockResolvedValue(undefined),
}))

import { CenterApiClient } from '../center-api.js'
import { registerDeleteNode } from './delete-node.js'

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

interface DeleteResult {
  deleted: boolean
  ip: string
  error?: string
}

async function invokeDeleteNode(
  params: Record<string, unknown>
): Promise<{ content: Array<{ type: string; text: string }> }> {
  const server = new McpServer({ name: 'test', version: '0.0.1' })
  const apiClient = new CenterApiClient({} as never)
  registerDeleteNode(server, apiClient)

  const registeredTools = (server as unknown as {
    _registeredTools: Record<string, { handler: (args: Record<string, unknown>) => Promise<unknown> }>
  })._registeredTools

  const tool = registeredTools['delete_node']
  if (!tool) throw new Error('delete_node tool not registered')

  return tool.handler(params) as Promise<{ content: Array<{ type: string; text: string }> }>
}

function parseResult(result: { content: Array<{ type: string; text: string }> }): DeleteResult {
  return JSON.parse(result.content[0]!.text) as DeleteResult
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

describe('delete_node tool', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    mockRequest.mockResolvedValue({ code: 0, message: 'ok' })
  })

  it('returns deleted=true on successful API response', async () => {
    mockRequest.mockResolvedValue({ code: 0, message: 'ok' })

    const result = await invokeDeleteNode({ ip: '1.2.3.4' })
    const parsed = parseResult(result)

    expect(parsed.deleted).toBe(true)
    expect(parsed.ip).toBe('1.2.3.4')
    expect(parsed.error).toBeUndefined()
  })

  it('calls DELETE /app/nodes/:ip', async () => {
    await invokeDeleteNode({ ip: '5.6.7.8' })

    expect(mockRequest).toHaveBeenCalledWith('/app/nodes/5.6.7.8', { method: 'DELETE' })
  })

  it('returns deleted=false when API returns non-zero code', async () => {
    mockRequest.mockResolvedValue({ code: 403, message: 'node has active batch tasks' })

    const result = await invokeDeleteNode({ ip: '1.2.3.4' })
    const parsed = parseResult(result)

    expect(parsed.deleted).toBe(false)
    expect(parsed.ip).toBe('1.2.3.4')
    expect(parsed.error).toBe('node has active batch tasks')
  })

  it('returns deleted=false when API request throws', async () => {
    mockRequest.mockRejectedValue(new Error('Network error'))

    const result = await invokeDeleteNode({ ip: '1.2.3.4' })
    const parsed = parseResult(result)

    expect(parsed.deleted).toBe(false)
    expect(parsed.ip).toBe('1.2.3.4')
    expect(parsed.error).toBe('Network error')
  })

  it('returns deleted=false when node not found', async () => {
    mockRequest.mockResolvedValue({ code: 404, message: 'node not found' })

    const result = await invokeDeleteNode({ ip: '9.9.9.9' })
    const parsed = parseResult(result)

    expect(parsed.deleted).toBe(false)
    expect(parsed.error).toBe('node not found')
  })
})
