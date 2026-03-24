/**
 * Tests for the lookup_user MCP tool.
 */

import { describe, it, expect, vi, beforeEach } from 'vitest'
import { McpServer } from '@modelcontextprotocol/sdk/server/mcp.js'

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
import { registerLookupUser } from './lookup-user.js'

async function invokeLookupUser(
  params: Record<string, unknown>
): Promise<{ content: Array<{ type: string; text: string }> }> {
  const server = new McpServer({ name: 'test', version: '0.0.1' })
  const apiClient = new CenterApiClient({} as never)
  registerLookupUser(server, apiClient)

  const registeredTools = (server as unknown as {
    _registeredTools: Record<string, { handler: (args: Record<string, unknown>) => Promise<unknown> }>
  })._registeredTools

  const tool = registeredTools['lookup_user']
  if (!tool) throw new Error('lookup_user tool not registered')

  return tool.handler(params) as Promise<{ content: Array<{ type: string; text: string }> }>
}

function parseResult(result: { content: Array<{ type: string; text: string }> }): Record<string, unknown> {
  return JSON.parse(result.content[0]!.text) as Record<string, unknown>
}

beforeEach(() => {
  mockRequest.mockReset()
})

describe('lookup_user', () => {
  it('looks up by uuid', async () => {
    mockRequest.mockResolvedValue({ code: 0, data: { uuid: 'abc-123', email: 'test@example.com' } })

    const result = await invokeLookupUser({ uuid: 'abc-123' })
    const parsed = parseResult(result)

    expect(mockRequest).toHaveBeenCalledWith('/app/users/abc-123')
    expect(parsed).toHaveProperty('uuid', 'abc-123')
  })

  it('looks up by email', async () => {
    mockRequest.mockResolvedValue({ code: 0, data: { items: [{ uuid: 'abc-123', email: 'test@example.com' }], pagination: { total: 1 } } })

    const result = await invokeLookupUser({ email: 'test@example.com' })
    const parsed = parseResult(result)

    expect(mockRequest).toHaveBeenCalledWith('/app/users?email=test%40example.com')
    expect(parsed).toHaveProperty('items')
  })

  it('returns error when neither email nor uuid provided', async () => {
    const result = await invokeLookupUser({})
    const parsed = parseResult(result)

    expect(parsed).toHaveProperty('error')
    expect(mockRequest).not.toHaveBeenCalled()
  })

  it('handles API error', async () => {
    mockRequest.mockResolvedValue({ code: 404, message: 'User not found' })

    const result = await invokeLookupUser({ uuid: 'nonexistent' })
    const parsed = parseResult(result)

    expect(parsed).toHaveProperty('error')
  })

  it('handles network error', async () => {
    mockRequest.mockRejectedValue(new Error('Connection refused'))

    const result = await invokeLookupUser({ uuid: 'abc-123' })
    const parsed = parseResult(result)

    expect(parsed).toHaveProperty('error', 'Connection refused')
  })
})
