/**
 * list_retailer_todos MCP tool.
 *
 * Lists pending retailer follow-up items.
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

export function registerListRetailerTodos(server: McpServer, apiClient: CenterApiClient): void {
  server.tool(
    'list_retailer_todos',
    'List pending retailer follow-up items that need action.',
    {
      page: z.number().optional().describe('Page number (default 1)'),
      page_size: z.number().optional().describe('Page size (default 20)'),
    },
    async (params) => {
      try {
        const query = new URLSearchParams()
        if (params.page) query.set('page', String(params.page))
        if (params.page_size) query.set('pageSize', String(params.page_size))

        const raw = await apiClient.request(`/app/retailers/todos?${query.toString()}`)
        const response = raw as CenterListResponse

        if (response.code !== 0) {
          return { content: [{ type: 'text' as const, text: JSON.stringify({ error: 'Query failed', code: response.code }) }] }
        }

        await audit('list_retailer_todos', { count: response.data.items.length, total: response.data.pagination?.total ?? 0 })
        return { content: [{ type: 'text' as const, text: JSON.stringify(response.data, null, 2) }] }
      } catch (err) {
        const msg = err instanceof Error ? err.message : String(err)
        return { content: [{ type: 'text' as const, text: JSON.stringify({ error: msg }) }] }
      }
    }
  )
}
