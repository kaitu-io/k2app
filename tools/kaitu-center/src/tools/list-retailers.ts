/**
 * list_retailers MCP tool.
 *
 * Lists retailers from Center API with optional filtering.
 * Roles: marketing
 */

import { z } from 'zod'
import type { McpServer } from '@modelcontextprotocol/sdk/server/mcp.js'
import type { CenterApiClient } from '../center-api.js'
import { audit } from '../audit.js'

interface CenterListResponse {
  code: number
  data: { items: unknown[]; pagination?: { page: number; pageSize: number; total: number } }
}

export function registerListRetailers(server: McpServer, apiClient: CenterApiClient): void {
  server.tool(
    'list_retailers',
    'List retailers with optional filtering by level or email.',
    {
      level: z.string().optional().describe('Filter by retailer level (e.g. L1, L2)'),
      email: z.string().optional().describe('Filter by email (partial match)'),
      page: z.number().optional().describe('Page number (default 1)'),
      page_size: z.number().optional().describe('Page size (default 20)'),
    },
    async (params) => {
      try {
        const query = new URLSearchParams()
        if (params.level) query.set('level', params.level)
        if (params.email) query.set('email', params.email)
        if (params.page) query.set('page', String(params.page))
        if (params.page_size) query.set('pageSize', String(params.page_size))

        const raw = await apiClient.request(`/app/retailers?${query.toString()}`)
        const response = raw as CenterListResponse

        if (response.code !== 0) {
          return { content: [{ type: 'text' as const, text: JSON.stringify({ error: 'Query failed', code: response.code }) }] }
        }

        await audit('list_retailers', { count: response.data.items.length, total: response.data.pagination?.total ?? 0 })
        return { content: [{ type: 'text' as const, text: JSON.stringify(response.data, null, 2) }] }
      } catch (err) {
        const msg = err instanceof Error ? err.message : String(err)
        return { content: [{ type: 'text' as const, text: JSON.stringify({ error: msg }) }] }
      }
    }
  )
}
