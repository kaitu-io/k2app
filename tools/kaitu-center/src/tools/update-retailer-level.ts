/**
 * update_retailer_level MCP tool.
 *
 * Updates a retailer's commission level.
 * Roles: marketing
 */

import { z } from 'zod'
import type { McpServer } from '@modelcontextprotocol/sdk/server/mcp.js'
import type { CenterApiClient } from '../center-api.js'
import { audit } from '../audit.js'

interface CenterResponse { code: number; message?: string }

export function registerUpdateRetailerLevel(server: McpServer, apiClient: CenterApiClient): void {
  server.tool(
    'update_retailer_level',
    'Update a retailer commission level. Always document the reason via create_retailer_note first.',
    {
      uuid: z.string().describe('Retailer UUID'),
      level: z.string().describe('New level (e.g. L1, L2, L3, L4)'),
    },
    async (params) => {
      try {
        const raw = await apiClient.request(`/app/retailers/${params.uuid}/level`, {
          method: 'PUT',
          body: JSON.stringify({ level: params.level }),
        })
        const response = raw as CenterResponse

        if (response.code !== 0) {
          await audit('update_retailer_level', { uuid: params.uuid, level: params.level, updated: false, error: response.message })
          return { content: [{ type: 'text' as const, text: JSON.stringify({ updated: false, uuid: params.uuid, error: response.message }) }] }
        }

        await audit('update_retailer_level', { uuid: params.uuid, newLevel: params.level, updated: true })
        return { content: [{ type: 'text' as const, text: JSON.stringify({ updated: true, uuid: params.uuid, newLevel: params.level }) }] }
      } catch (err) {
        const msg = err instanceof Error ? err.message : String(err)
        await audit('update_retailer_level', { uuid: params.uuid, updated: false, error: msg })
        return { content: [{ type: 'text' as const, text: JSON.stringify({ updated: false, uuid: params.uuid, error: msg }) }] }
      }
    }
  )
}
