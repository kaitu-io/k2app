/**
 * resolve_feedback_ticket MCP tool.
 *
 * Marks a feedback ticket as resolved in Center API.
 */

import { z } from 'zod'
import type { McpServer } from '@modelcontextprotocol/sdk/server/mcp.js'
import type { CenterApiClient } from '../center-api.js'
import { audit } from '../audit.js'

interface CenterResponse {
  code: number
  message?: string
}

export function registerResolveFeedbackTicket(server: McpServer, apiClient: CenterApiClient): void {
  server.tool(
    'resolve_feedback_ticket',
    'Mark a feedback ticket as resolved. Use after troubleshooting is complete.',
    {
      id: z.number().describe('Ticket ID (from query_feedback_tickets results)'),
      resolved_by: z.string().describe('Who resolved the ticket (e.g. "claude", "david")'),
    },
    async (params) => {
      try {
        const rawResponse = await apiClient.request(`/app/feedback-tickets/${params.id}/resolve`, {
          method: 'PUT',
          body: JSON.stringify({ resolvedBy: params.resolved_by }),
        })

        const response = rawResponse as CenterResponse
        if (response.code !== 0) {
          await audit('resolve_feedback_ticket', { id: params.id, resolved: false, error: response.message })
          return {
            content: [{ type: 'text' as const, text: JSON.stringify({ resolved: false, id: params.id, error: response.message }) }],
          }
        }

        await audit('resolve_feedback_ticket', { id: params.id, resolved: true, resolved_by: params.resolved_by })

        return {
          content: [{ type: 'text' as const, text: JSON.stringify({ resolved: true, id: params.id, resolvedBy: params.resolved_by }) }],
        }
      } catch (err) {
        const errorMessage = err instanceof Error ? err.message : String(err)
        await audit('resolve_feedback_ticket', { id: params.id, resolved: false, error: errorMessage })
        return {
          content: [{ type: 'text' as const, text: JSON.stringify({ resolved: false, id: params.id, error: errorMessage }) }],
        }
      }
    }
  )
}
