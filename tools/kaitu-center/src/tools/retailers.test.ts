/**
 * Tests for retailer MCP tools: list_retailers, get_retailer_detail,
 * update_retailer_level, create_retailer_note, list_retailer_todos.
 */

import { describe, it, expect, vi, beforeEach } from 'vitest'
import { McpServer } from '@modelcontextprotocol/sdk/server/mcp.js'

const mockRequest = vi.fn()

vi.mock('../center-api.js', () => ({
  CenterApiClient: vi.fn(() => ({ request: mockRequest })),
}))

vi.mock('../audit.js', () => ({
  audit: vi.fn().mockResolvedValue(undefined),
}))

import { CenterApiClient } from '../center-api.js'
import { registerListRetailers } from './list-retailers.js'
import { registerGetRetailerDetail } from './get-retailer-detail.js'
import { registerUpdateRetailerLevel } from './update-retailer-level.js'
import { registerCreateRetailerNote } from './create-retailer-note.js'
import { registerListRetailerTodos } from './list-retailer-todos.js'

async function invoke(
  registerFn: (s: McpServer, c: CenterApiClient) => void,
  toolName: string,
  params: Record<string, unknown>
): Promise<Record<string, unknown>> {
  const server = new McpServer({ name: 'test', version: '0.0.1' })
  const apiClient = new CenterApiClient({} as never)
  registerFn(server, apiClient)

  const tools = (server as unknown as {
    _registeredTools: Record<string, { handler: (args: Record<string, unknown>) => Promise<{ content: Array<{ type: string; text: string }> }> }>
  })._registeredTools

  const tool = tools[toolName]
  if (!tool) throw new Error(`${toolName} not registered`)

  const result = await tool.handler(params)
  return JSON.parse(result.content[0]!.text) as Record<string, unknown>
}

beforeEach(() => { mockRequest.mockReset() })

describe('list_retailers', () => {
  it('returns paginated list', async () => {
    mockRequest.mockResolvedValue({ code: 0, data: { items: [{ uuid: 'r1' }], pagination: { total: 1 } } })
    const result = await invoke(registerListRetailers, 'list_retailers', {})
    expect(result).toHaveProperty('items')
    expect(mockRequest).toHaveBeenCalledWith('/app/retailers?')
  })

  it('passes filter params', async () => {
    mockRequest.mockResolvedValue({ code: 0, data: { items: [], pagination: { total: 0 } } })
    await invoke(registerListRetailers, 'list_retailers', { level: 'L2', email: 'test' })
    expect(mockRequest).toHaveBeenCalledWith(expect.stringContaining('level=L2'))
  })

  it('handles error', async () => {
    mockRequest.mockRejectedValue(new Error('fail'))
    const result = await invoke(registerListRetailers, 'list_retailers', {})
    expect(result).toHaveProperty('error', 'fail')
  })
})

describe('get_retailer_detail', () => {
  it('returns retailer by uuid', async () => {
    mockRequest.mockResolvedValue({ code: 0, data: { uuid: 'r1', level: 'L2' } })
    const result = await invoke(registerGetRetailerDetail, 'get_retailer_detail', { uuid: 'r1' })
    expect(result).toHaveProperty('uuid', 'r1')
    expect(mockRequest).toHaveBeenCalledWith('/app/retailers/r1')
  })

  it('handles not found', async () => {
    mockRequest.mockResolvedValue({ code: 404, message: 'Not found' })
    const result = await invoke(registerGetRetailerDetail, 'get_retailer_detail', { uuid: 'bad' })
    expect(result).toHaveProperty('error')
  })
})

describe('update_retailer_level', () => {
  it('updates level successfully', async () => {
    mockRequest.mockResolvedValue({ code: 0 })
    const result = await invoke(registerUpdateRetailerLevel, 'update_retailer_level', { uuid: 'r1', level: 'L3' })
    expect(result).toEqual({ updated: true, uuid: 'r1', newLevel: 'L3' })
    expect(mockRequest).toHaveBeenCalledWith('/app/retailers/r1/level', {
      method: 'PUT',
      body: JSON.stringify({ level: 'L3' }),
    })
  })

  it('handles API error', async () => {
    mockRequest.mockResolvedValue({ code: 400, message: 'Invalid level' })
    const result = await invoke(registerUpdateRetailerLevel, 'update_retailer_level', { uuid: 'r1', level: 'bad' })
    expect(result).toHaveProperty('updated', false)
  })
})

describe('create_retailer_note', () => {
  it('creates note successfully', async () => {
    mockRequest.mockResolvedValue({ code: 0, data: { id: 42 } })
    const result = await invoke(registerCreateRetailerNote, 'create_retailer_note', { uuid: 'r1', content: 'Follow up needed' })
    expect(result).toEqual({ created: true, uuid: 'r1', noteId: 42 })
  })

  it('handles error', async () => {
    mockRequest.mockRejectedValue(new Error('db error'))
    const result = await invoke(registerCreateRetailerNote, 'create_retailer_note', { uuid: 'r1', content: 'test' })
    expect(result).toHaveProperty('created', false)
  })
})

describe('list_retailer_todos', () => {
  it('returns todo list', async () => {
    mockRequest.mockResolvedValue({ code: 0, data: { items: [{ id: 1 }], pagination: { total: 1 } } })
    const result = await invoke(registerListRetailerTodos, 'list_retailer_todos', {})
    expect(result).toHaveProperty('items')
  })

  it('handles error', async () => {
    mockRequest.mockRejectedValue(new Error('timeout'))
    const result = await invoke(registerListRetailerTodos, 'list_retailer_todos', {})
    expect(result).toHaveProperty('error', 'timeout')
  })
})
