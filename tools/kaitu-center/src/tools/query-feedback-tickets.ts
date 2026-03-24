/**
 * query_feedback_tickets MCP tool.
 *
 * Queries feedback ticket records from Center API.
 * Each ticket may have associated device logs (linked by feedbackId).
 */

import { z } from 'zod'
import type { McpServer } from '@modelcontextprotocol/sdk/server/mcp.js'
import type { CenterApiClient } from '../center-api.js'
import { audit } from '../audit.js'

interface CenterListResponse {
  code: number
  data: {
    items: unknown[]
    pagination?: { page: number; pageSize: number; total: number }
  }
}

export function registerQueryFeedbackTickets(server: McpServer, apiClient: CenterApiClient): void {
  server.tool(
    'query_feedback_tickets',
    'Query user feedback tickets. Filter by UDID, email, user ID, status, or time range. Each ticket may have associated device logs (use feedback_id to cross-reference).',
    {
      udid: z.string().optional().describe('Filter by device UDID'),
      email: z.string().optional().describe('Filter by user email (partial match)'),
      user_id: z.string().optional().describe('Filter by user ID'),
      status: z.enum(['open', 'resolved', 'closed']).optional().describe('Filter by ticket status'),
      from: z.string().optional().describe('Start time (RFC3339)'),
      to: z.string().optional().describe('End time (RFC3339)'),
      page: z.number().optional().describe('Page number (default 1)'),
      page_size: z.number().optional().describe('Page size (default 20, max 100)'),
    },
    async (params) => {
      const query = new URLSearchParams()
      if (params.udid) query.set('udid', params.udid)
      if (params.email) query.set('email', params.email)
      if (params.user_id) query.set('user_id', params.user_id)
      if (params.status) query.set('status', params.status)
      if (params.from) query.set('from', params.from)
      if (params.to) query.set('to', params.to)
      if (params.page) query.set('page', String(params.page))
      if (params.page_size) query.set('pageSize', String(params.page_size))

      const rawResponse = await apiClient.request(`/app/feedback-tickets?${query.toString()}`)
      const response = rawResponse as CenterListResponse

      if (response.code !== 0) {
        return {
          content: [{ type: 'text' as const, text: JSON.stringify({ error: 'Query failed', code: response.code }) }],
        }
      }

      await audit('query_feedback_tickets', {
        filter: Object.entries(params).filter(([, v]) => v !== undefined).map(([k, v]) => `${k}=${v}`).join(',') || 'none',
        count: response.data.items.length,
        total: response.data.pagination?.total ?? 0,
      })

      return {
        content: [{ type: 'text' as const, text: JSON.stringify(response.data, null, 2) }],
      }
    }
  )
}
