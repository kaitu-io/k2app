/**
 * lookup_user MCP tool.
 *
 * Looks up a user by email or UUID from Center API.
 * Roles: support, marketing
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

export function registerLookupUser(server: McpServer, apiClient: CenterApiClient): void {
  server.tool(
    'lookup_user',
    'Look up a user by email or UUID. Provide one of email or uuid.',
    {
      email: z.string().optional().describe('User email to search for'),
      uuid: z.string().optional().describe('User UUID for direct lookup'),
    },
    async (params) => {
      try {
        if (!params.email && !params.uuid) {
          return {
            content: [{ type: 'text' as const, text: JSON.stringify({ error: 'Provide either email or uuid' }) }],
          }
        }

        let rawResponse: unknown
        if (params.uuid) {
          rawResponse = await apiClient.request(`/app/users/${params.uuid}`)
        } else {
          rawResponse = await apiClient.request(`/app/users?email=${encodeURIComponent(params.email!)}`)
        }

        const response = rawResponse as CenterResponse
        if (response.code !== 0) {
          await audit('lookup_user', { query: params.uuid || params.email, error: response.message })
          return {
            content: [{ type: 'text' as const, text: JSON.stringify({ error: response.message, code: response.code }) }],
          }
        }

        await audit('lookup_user', { query: params.uuid || params.email })

        return {
          content: [{ type: 'text' as const, text: JSON.stringify(response.data, null, 2) }],
        }
      } catch (err) {
        const errorMessage = err instanceof Error ? err.message : String(err)
        await audit('lookup_user', { query: params.uuid || params.email, error: errorMessage })
        return {
          content: [{ type: 'text' as const, text: JSON.stringify({ error: errorMessage }) }],
        }
      }
    }
  )
}
