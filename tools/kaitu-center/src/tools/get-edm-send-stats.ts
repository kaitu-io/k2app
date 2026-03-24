/**
 * get_edm_send_stats MCP tool.
 *
 * Gets send statistics for EDM campaigns.
 * Roles: marketing
 */

import { z } from 'zod'
import type { McpServer } from '@modelcontextprotocol/sdk/server/mcp.js'
import type { CenterApiClient } from '../center-api.js'
import { audit } from '../audit.js'

interface CenterResponse { code: number; message?: string; data?: unknown }

export function registerGetEdmSendStats(server: McpServer, apiClient: CenterApiClient): void {
  server.tool(
    'get_edm_send_stats',
    'Get send statistics for EDM campaigns. Filter by batch ID or template ID.',
    {
      batch_id: z.string().optional().describe('Filter by batch/task ID'),
      template_id: z.number().optional().describe('Filter by template ID'),
    },
    async (params) => {
      try {
        const query = new URLSearchParams()
        if (params.batch_id) query.set('batchId', params.batch_id)
        if (params.template_id) query.set('templateId', String(params.template_id))

        const raw = await apiClient.request(`/app/edm/send-logs/stats?${query.toString()}`)
        const response = raw as CenterResponse

        if (response.code !== 0) {
          return { content: [{ type: 'text' as const, text: JSON.stringify({ error: response.message, code: response.code }) }] }
        }

        await audit('get_edm_send_stats', { batchId: params.batch_id, templateId: params.template_id })
        return { content: [{ type: 'text' as const, text: JSON.stringify(response.data, null, 2) }] }
      } catch (err) {
        const msg = err instanceof Error ? err.message : String(err)
        return { content: [{ type: 'text' as const, text: JSON.stringify({ error: msg }) }] }
      }
    }
  )
}
