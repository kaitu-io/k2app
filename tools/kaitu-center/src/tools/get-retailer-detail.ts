/**
 * get_retailer_detail MCP tool.
 *
 * Gets detailed retailer profile by UUID.
 * Roles: marketing
 */

import { z } from 'zod'
import type { McpServer } from '@modelcontextprotocol/sdk/server/mcp.js'
import type { CenterApiClient } from '../center-api.js'
import { audit } from '../audit.js'

interface CenterResponse { code: number; message?: string; data?: unknown }

export function registerGetRetailerDetail(server: McpServer, apiClient: CenterApiClient): void {
  server.tool(
    'get_retailer_detail',
    'Get detailed retailer profile including commission rates and performance metrics.',
    {
      uuid: z.string().describe('Retailer UUID'),
    },
    async (params) => {
      try {
        const raw = await apiClient.request(`/app/retailers/${params.uuid}`)
        const response = raw as CenterResponse

        if (response.code !== 0) {
          await audit('get_retailer_detail', { uuid: params.uuid, error: response.message })
          return { content: [{ type: 'text' as const, text: JSON.stringify({ error: response.message, code: response.code }) }] }
        }

        await audit('get_retailer_detail', { uuid: params.uuid })
        return { content: [{ type: 'text' as const, text: JSON.stringify(response.data, null, 2) }] }
      } catch (err) {
        const msg = err instanceof Error ? err.message : String(err)
        await audit('get_retailer_detail', { uuid: params.uuid, error: msg })
        return { content: [{ type: 'text' as const, text: JSON.stringify({ error: msg }) }] }
      }
    }
  )
}
