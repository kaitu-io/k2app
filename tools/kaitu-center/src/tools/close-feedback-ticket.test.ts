/**
 * Tests for the close_feedback_ticket MCP tool.
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
import { registerCloseFeedbackTicket } from './close-feedback-ticket.js'

async function invokeCloseFeedbackTicket(
  params: Record<string, unknown>
): Promise<{ content: Array<{ type: string; text: string }> }> {
  const server = new McpServer({ name: 'test', version: '0.0.1' })
  const apiClient = new CenterApiClient({} as never)
  registerCloseFeedbackTicket(server, apiClient)

  const registeredTools = (server as unknown as {
    _registeredTools: Record<string, { handler: (args: Record<string, unknown>) => Promise<unknown> }>
  })._registeredTools

  const tool = registeredTools['close_feedback_ticket']
  if (!tool) throw new Error('close_feedback_ticket tool not registered')

  return tool.handler(params) as Promise<{ content: Array<{ type: string; text: string }> }>
}

function parseResult(result: { content: Array<{ type: string; text: string }> }): Record<string, unknown> {
  return JSON.parse(result.content[0]!.text) as Record<string, unknown>
}

beforeEach(() => {
  mockRequest.mockReset()
})

describe('close_feedback_ticket', () => {
  it('closes a ticket successfully', async () => {
    mockRequest.mockResolvedValue({ code: 0 })

    const result = await invokeCloseFeedbackTicket({ id: 42, reason: 'Resolved by diagnosis' })
    const parsed = parseResult(result)

    expect(mockRequest).toHaveBeenCalledWith('/app/feedback-tickets/42/close', {
      method: 'PUT',
      body: JSON.stringify({ reason: 'Resolved by diagnosis' }),
    })
    expect(parsed).toEqual({ closed: true, id: 42 })
  })

  it('handles API error', async () => {
    mockRequest.mockResolvedValue({ code: 404, message: 'Ticket not found' })

    const result = await invokeCloseFeedbackTicket({ id: 999, reason: 'test' })
    const parsed = parseResult(result)

    expect(parsed).toHaveProperty('closed', false)
    expect(parsed).toHaveProperty('error', 'Ticket not found')
  })

  it('handles network error', async () => {
    mockRequest.mockRejectedValue(new Error('Network down'))

    const result = await invokeCloseFeedbackTicket({ id: 42, reason: 'test' })
    const parsed = parseResult(result)

    expect(parsed).toHaveProperty('closed', false)
    expect(parsed).toHaveProperty('error', 'Network down')
  })
})
