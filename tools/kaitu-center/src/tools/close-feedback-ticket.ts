/**
 * close_feedback_ticket MCP tool.
 *
 * Marks a feedback ticket as closed in Center API.
 * Roles: support
 */

import { z } from 'zod'
import type { McpServer } from '@modelcontextprotocol/sdk/server/mcp.js'
import type { CenterApiClient } from '../center-api.js'
import { audit } from '../audit.js'

interface CenterResponse {
  code: number
  message?: string
}

export function registerCloseFeedbackTicket(server: McpServer, apiClient: CenterApiClient): void {
  server.tool(
    'close_feedback_ticket',
    'Close a feedback ticket. Use when the issue has been fully handled or is not actionable.',
    {
      id: z.number().describe('Ticket ID (from query_feedback_tickets results)'),
      reason: z.string().describe('Reason for closing the ticket'),
    },
    async (params) => {
      try {
        const rawResponse = await apiClient.request(`/app/feedback-tickets/${params.id}/close`, {
          method: 'PUT',
          body: JSON.stringify({ reason: params.reason }),
        })

        const response = rawResponse as CenterResponse
        if (response.code !== 0) {
          await audit('close_feedback_ticket', { id: params.id, closed: false, error: response.message })
          return {
            content: [{ type: 'text' as const, text: JSON.stringify({ closed: false, id: params.id, error: response.message }) }],
          }
        }

        await audit('close_feedback_ticket', { id: params.id, closed: true, reason: params.reason })

        return {
          content: [{ type: 'text' as const, text: JSON.stringify({ closed: true, id: params.id }) }],
        }
      } catch (err) {
        const errorMessage = err instanceof Error ? err.message : String(err)
        await audit('close_feedback_ticket', { id: params.id, closed: false, error: errorMessage })
        return {
          content: [{ type: 'text' as const, text: JSON.stringify({ closed: false, id: params.id, error: errorMessage }) }],
        }
      }
    }
  )
}
