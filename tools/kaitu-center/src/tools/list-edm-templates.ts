/**
 * list_edm_templates MCP tool.
 *
 * Lists email marketing templates.
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

export function registerListEdmTemplates(server: McpServer, apiClient: CenterApiClient): void {
  server.tool(
    'list_edm_templates',
    'List email marketing templates. Returns id, name, subject, language, created_at.',
    {
      page: z.number().optional().describe('Page number (default 1)'),
      page_size: z.number().optional().describe('Page size (default 20)'),
    },
    async (params) => {
      try {
        const query = new URLSearchParams()
        if (params.page) query.set('page', String(params.page))
        if (params.page_size) query.set('pageSize', String(params.page_size))

        const raw = await apiClient.request(`/app/edm/templates?${query.toString()}`)
        const response = raw as CenterListResponse

        if (response.code !== 0) {
          return { content: [{ type: 'text' as const, text: JSON.stringify({ error: 'Query failed', code: response.code }) }] }
        }

        await audit('list_edm_templates', { count: response.data.items.length })
        return { content: [{ type: 'text' as const, text: JSON.stringify(response.data, null, 2) }] }
      } catch (err) {
        const msg = err instanceof Error ? err.message : String(err)
        return { content: [{ type: 'text' as const, text: JSON.stringify({ error: msg }) }] }
      }
    }
  )
}
