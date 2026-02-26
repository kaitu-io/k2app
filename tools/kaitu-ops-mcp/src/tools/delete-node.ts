/**
 * delete_node MCP tool.
 *
 * Registers the delete_node tool on an McpServer instance.
 * Deletes a node from Center API. This removes the database record only —
 * it does NOT stop containers on the remote node.
 */

import { McpServer } from '@modelcontextprotocol/sdk/server/mcp.js'
import { z } from 'zod'
import type { CenterApiClient } from '../center-api.js'
import { audit } from '../audit.js'

/**
 * Center API response shape for node deletion.
 */
interface CenterResponse {
  code: number
  message: string
}

/**
 * Registers the delete_node tool on the given McpServer.
 *
 * Tool behaviour:
 * - Calls DELETE /app/nodes/:ipv4 on Center API.
 * - Center API checks for active batch tasks before allowing deletion.
 * - Returns { deleted: true, ip } on success.
 * - Returns { deleted: false, error } on failure.
 * - This is an API-only operation — no SSH connection to the node.
 *
 * @param server - The McpServer to register the tool on.
 * @param apiClient - The CenterApiClient for authenticated API requests.
 */
export function registerDeleteNode(server: McpServer, apiClient: CenterApiClient): void {
  server.tool(
    'delete_node',
    'Delete a node from Center. Use for decommissioned/inactive nodes. This does NOT stop containers on the node — only removes the Center record.',
    {
      ip: z.string().describe('Node IP address to delete'),
    },
    async (params) => {
      const { ip } = params

      try {
        const rawResponse = await apiClient.request(`/app/nodes/${ip}`, {
          method: 'DELETE',
        })

        // Check Center API response code
        const response = rawResponse as CenterResponse
        if (response.code !== 0) {
          await audit('delete_node', { ip, deleted: false, error: response.message })

          return {
            content: [
              {
                type: 'text' as const,
                text: JSON.stringify({
                  deleted: false,
                  ip,
                  error: response.message,
                }),
              },
            ],
          }
        }

        await audit('delete_node', { ip, deleted: true })

        return {
          content: [
            {
              type: 'text' as const,
              text: JSON.stringify({ deleted: true, ip }),
            },
          ],
        }
      } catch (err) {
        const errorMessage = err instanceof Error ? err.message : String(err)

        await audit('delete_node', { ip, deleted: false, error: errorMessage })

        return {
          content: [
            {
              type: 'text' as const,
              text: JSON.stringify({
                deleted: false,
                ip,
                error: errorMessage,
              }),
            },
          ],
        }
      }
    }
  )
}
