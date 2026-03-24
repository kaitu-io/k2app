/**
 * Tests for the list_user_devices MCP tool.
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
import { registerListUserDevices } from './list-user-devices.js'

async function invokeListUserDevices(
  params: Record<string, unknown>
): Promise<{ content: Array<{ type: string; text: string }> }> {
  const server = new McpServer({ name: 'test', version: '0.0.1' })
  const apiClient = new CenterApiClient({} as never)
  registerListUserDevices(server, apiClient)

  const registeredTools = (server as unknown as {
    _registeredTools: Record<string, { handler: (args: Record<string, unknown>) => Promise<unknown> }>
  })._registeredTools

  const tool = registeredTools['list_user_devices']
  if (!tool) throw new Error('list_user_devices tool not registered')

  return tool.handler(params) as Promise<{ content: Array<{ type: string; text: string }> }>
}

function parseResult(result: { content: Array<{ type: string; text: string }> }): Record<string, unknown> {
  return JSON.parse(result.content[0]!.text) as Record<string, unknown>
}

beforeEach(() => {
  mockRequest.mockReset()
})

describe('list_user_devices', () => {
  it('lists devices for a user uuid', async () => {
    const devices = [
      { udid: 'device-1', platform: 'macOS', version: '0.4.0' },
      { udid: 'device-2', platform: 'iOS', version: '0.3.22' },
    ]
    mockRequest.mockResolvedValue({ code: 0, data: devices })

    const result = await invokeListUserDevices({ uuid: 'user-abc' })
    const parsed = parseResult(result)

    expect(mockRequest).toHaveBeenCalledWith('/app/users/user-abc/devices')
    expect(parsed).toEqual(devices)
  })

  it('handles API error', async () => {
    mockRequest.mockResolvedValue({ code: 404, message: 'User not found' })

    const result = await invokeListUserDevices({ uuid: 'nonexistent' })
    const parsed = parseResult(result)

    expect(parsed).toHaveProperty('error')
  })

  it('handles network error', async () => {
    mockRequest.mockRejectedValue(new Error('Timeout'))

    const result = await invokeListUserDevices({ uuid: 'user-abc' })
    const parsed = parseResult(result)

    expect(parsed).toHaveProperty('error', 'Timeout')
  })
})
