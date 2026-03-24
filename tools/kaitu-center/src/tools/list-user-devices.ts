/**
 * list_user_devices MCP tool.
 *
 * Lists devices registered to a user by UUID.
 * Roles: support
 */

import { z } from 'zod'
import type { McpServer } from '@modelcontextprotocol/sdk/server/mcp.js'
import type { CenterApiClient } from '../center-api.js'
import { audit } from '../audit.js'

interface CenterResponse {
  code: number
  message?: string
  data?: unknown
}

export function registerListUserDevices(server: McpServer, apiClient: CenterApiClient): void {
  server.tool(
    'list_user_devices',
    'List all devices registered to a user. Returns UDID, platform, app version, last seen time.',
    {
      uuid: z.string().describe('User UUID'),
    },
    async (params) => {
      try {
        const rawResponse = await apiClient.request(`/app/users/${params.uuid}/devices`)
        const response = rawResponse as CenterResponse

        if (response.code !== 0) {
          await audit('list_user_devices', { uuid: params.uuid, error: response.message })
          return {
            content: [{ type: 'text' as const, text: JSON.stringify({ error: response.message, code: response.code }) }],
          }
        }

        await audit('list_user_devices', { uuid: params.uuid })

        return {
          content: [{ type: 'text' as const, text: JSON.stringify(response.data, null, 2) }],
        }
      } catch (err) {
        const errorMessage = err instanceof Error ? err.message : String(err)
        await audit('list_user_devices', { uuid: params.uuid, error: errorMessage })
        return {
          content: [{ type: 'text' as const, text: JSON.stringify({ error: errorMessage }) }],
        }
      }
    }
  )
}
