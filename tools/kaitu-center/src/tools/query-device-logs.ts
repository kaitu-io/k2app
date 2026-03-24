/**
 * query_device_logs MCP tool.
 *
 * Queries device log metadata from Center API.
 * Returns paginated list of log records with S3 keys for download.
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

export function registerQueryDeviceLogs(server: McpServer, apiClient: CenterApiClient): void {
  server.tool(
    'query_device_logs',
    'Query device log upload records. Filter by UDID, user ID, feedback ID, reason, or time range. Returns metadata + S3 keys for downloading logs.',
    {
      udid: z.string().optional().describe('Filter by device UDID'),
      user_id: z.string().optional().describe('Filter by user ID'),
      feedback_id: z.string().optional().describe('Filter by feedback ID (links logs to a ticket)'),
      reason: z.string().optional().describe('Filter by upload reason (e.g. "user_feedback_report", "beta-auto-upload")'),
      from: z.string().optional().describe('Start time (RFC3339, e.g. "2026-03-01T00:00:00Z")'),
      to: z.string().optional().describe('End time (RFC3339)'),
      page: z.number().optional().describe('Page number (default 1)'),
      page_size: z.number().optional().describe('Page size (default 20, max 100)'),
    },
    async (params) => {
      const query = new URLSearchParams()
      if (params.udid) query.set('udid', params.udid)
      if (params.user_id) query.set('user_id', params.user_id)
      if (params.feedback_id) query.set('feedback_id', params.feedback_id)
      if (params.reason) query.set('reason', params.reason)
      if (params.from) query.set('from', params.from)
      if (params.to) query.set('to', params.to)
      if (params.page) query.set('page', String(params.page))
      if (params.page_size) query.set('pageSize', String(params.page_size))

      const rawResponse = await apiClient.request(`/app/device-logs?${query.toString()}`)
      const response = rawResponse as CenterListResponse

      if (response.code !== 0) {
        return {
          content: [{ type: 'text' as const, text: JSON.stringify({ error: 'Query failed', code: response.code }) }],
        }
      }

      await audit('query_device_logs', {
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
