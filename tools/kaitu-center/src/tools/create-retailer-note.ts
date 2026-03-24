/**
 * create_retailer_note MCP tool.
 *
 * Adds a follow-up note to a retailer profile.
 * Roles: marketing
 */

import { z } from 'zod'
import type { McpServer } from '@modelcontextprotocol/sdk/server/mcp.js'
import type { CenterApiClient } from '../center-api.js'
import { audit } from '../audit.js'

interface CenterResponse { code: number; message?: string; data?: { id?: number } }

export function registerCreateRetailerNote(server: McpServer, apiClient: CenterApiClient): void {
  server.tool(
    'create_retailer_note',
    'Add a follow-up note to a retailer profile. Use before level changes to document reasoning.',
    {
      uuid: z.string().describe('Retailer UUID'),
      content: z.string().describe('Note content'),
    },
    async (params) => {
      try {
        const raw = await apiClient.request(`/app/retailers/${params.uuid}/notes`, {
          method: 'POST',
          body: JSON.stringify({ content: params.content }),
        })
        const response = raw as CenterResponse

        if (response.code !== 0) {
          await audit('create_retailer_note', { uuid: params.uuid, created: false, error: response.message })
          return { content: [{ type: 'text' as const, text: JSON.stringify({ created: false, uuid: params.uuid, error: response.message }) }] }
        }

        const noteId = response.data?.id
        await audit('create_retailer_note', { uuid: params.uuid, created: true, noteId })
        return { content: [{ type: 'text' as const, text: JSON.stringify({ created: true, uuid: params.uuid, noteId }) }] }
      } catch (err) {
        const msg = err instanceof Error ? err.message : String(err)
        await audit('create_retailer_note', { uuid: params.uuid, created: false, error: msg })
        return { content: [{ type: 'text' as const, text: JSON.stringify({ created: false, uuid: params.uuid, error: msg }) }] }
      }
    }
  )
}
