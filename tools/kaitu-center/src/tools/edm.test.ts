/**
 * Tests for EDM MCP tools: list_edm_templates, create_edm_task,
 * preview_edm_targets, get_edm_send_stats.
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
import { registerListEdmTemplates } from './list-edm-templates.js'
import { registerCreateEdmTask } from './create-edm-task.js'
import { registerPreviewEdmTargets } from './preview-edm-targets.js'
import { registerGetEdmSendStats } from './get-edm-send-stats.js'

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

describe('list_edm_templates', () => {
  it('returns template list', async () => {
    mockRequest.mockResolvedValue({ code: 0, data: { items: [{ id: 1, name: 'Welcome' }], pagination: { total: 1 } } })
    const result = await invoke(registerListEdmTemplates, 'list_edm_templates', {})
    expect(result).toHaveProperty('items')
  })

  it('handles error', async () => {
    mockRequest.mockRejectedValue(new Error('fail'))
    const result = await invoke(registerListEdmTemplates, 'list_edm_templates', {})
    expect(result).toHaveProperty('error', 'fail')
  })
})

describe('create_edm_task', () => {
  it('queues task successfully', async () => {
    mockRequest.mockResolvedValue({ code: 0, data: { taskId: 'task-xyz' } })
    const result = await invoke(registerCreateEdmTask, 'create_edm_task', { template_id: 1, target_filter: { plan: 'pro' } })
    expect(result).toEqual({ queued: true, taskId: 'task-xyz' })
    expect(mockRequest).toHaveBeenCalledWith('/app/edm/tasks', {
      method: 'POST',
      body: JSON.stringify({ templateId: 1, targetFilter: { plan: 'pro' } }),
    })
  })

  it('handles API error', async () => {
    mockRequest.mockResolvedValue({ code: 400, message: 'Bad template' })
    const result = await invoke(registerCreateEdmTask, 'create_edm_task', { template_id: 999, target_filter: {} })
    expect(result).toHaveProperty('queued', false)
  })
})

describe('preview_edm_targets', () => {
  it('returns target count and sample', async () => {
    mockRequest.mockResolvedValue({ code: 0, data: { count: 500, sample: [{ email: 'a@b.com' }] } })
    const result = await invoke(registerPreviewEdmTargets, 'preview_edm_targets', { target_filter: { plan: 'pro' } })
    expect(result).toHaveProperty('count', 500)
    expect(result).toHaveProperty('sample')
  })

  it('handles error', async () => {
    mockRequest.mockRejectedValue(new Error('timeout'))
    const result = await invoke(registerPreviewEdmTargets, 'preview_edm_targets', { target_filter: {} })
    expect(result).toHaveProperty('error', 'timeout')
  })
})

describe('get_edm_send_stats', () => {
  it('returns stats by template', async () => {
    mockRequest.mockResolvedValue({ code: 0, data: { total: 100, sent: 95, failed: 5 } })
    const result = await invoke(registerGetEdmSendStats, 'get_edm_send_stats', { template_id: 1 })
    expect(result).toHaveProperty('total', 100)
    expect(mockRequest).toHaveBeenCalledWith(expect.stringContaining('templateId=1'))
  })

  it('handles error', async () => {
    mockRequest.mockRejectedValue(new Error('fail'))
    const result = await invoke(registerGetEdmSendStats, 'get_edm_send_stats', {})
    expect(result).toHaveProperty('error', 'fail')
  })
})
