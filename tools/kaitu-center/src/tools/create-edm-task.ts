/**
 * create_edm_task MCP tool.
 *
 * Creates an EDM send task (queued to Asynq).
 * Roles: marketing
 */

import { z } from 'zod'
import type { McpServer } from '@modelcontextprotocol/sdk/server/mcp.js'
import type { CenterApiClient } from '../center-api.js'
import { audit } from '../audit.js'

interface CenterResponse { code: number; message?: string; data?: { taskId?: string } }

export function registerCreateEdmTask(server: McpServer, apiClient: CenterApiClient): void {
  server.tool(
    'create_edm_task',
    'Create an EDM email send task. ALWAYS preview targets first with preview_edm_targets before sending.',
    {
      template_id: z.number().describe('Email template ID'),
      target_filter: z.record(z.string(), z.unknown()).describe('Target user filter criteria'),
    },
    async (params) => {
      try {
        const raw = await apiClient.request('/app/edm/tasks', {
          method: 'POST',
          body: JSON.stringify({ templateId: params.template_id, targetFilter: params.target_filter }),
        })
        const response = raw as CenterResponse

        if (response.code !== 0) {
          await audit('create_edm_task', { templateId: params.template_id, queued: false, error: response.message })
          return { content: [{ type: 'text' as const, text: JSON.stringify({ queued: false, error: response.message }) }] }
        }

        const taskId = response.data?.taskId
        await audit('create_edm_task', { templateId: params.template_id, taskId, queued: true })
        return { content: [{ type: 'text' as const, text: JSON.stringify({ queued: true, taskId }) }] }
      } catch (err) {
        const msg = err instanceof Error ? err.message : String(err)
        await audit('create_edm_task', { templateId: params.template_id, queued: false, error: msg })
        return { content: [{ type: 'text' as const, text: JSON.stringify({ queued: false, error: msg }) }] }
      }
    }
  )
}
