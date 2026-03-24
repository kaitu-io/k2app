/**
 * preview_edm_targets MCP tool.
 *
 * Previews target users for an EDM campaign without sending.
 * Roles: marketing
 */

import { z } from 'zod'
import type { McpServer } from '@modelcontextprotocol/sdk/server/mcp.js'
import type { CenterApiClient } from '../center-api.js'
import { audit } from '../audit.js'

interface CenterResponse { code: number; message?: string; data?: { count: number; sample: unknown[] } }

export function registerPreviewEdmTargets(server: McpServer, apiClient: CenterApiClient): void {
  server.tool(
    'preview_edm_targets',
    'Preview target user count and sample for an EDM campaign. Use before create_edm_task to verify audience.',
    {
      target_filter: z.record(z.string(), z.unknown()).describe('Target user filter criteria'),
    },
    async (params) => {
      try {
        const raw = await apiClient.request('/app/edm/preview-targets', {
          method: 'POST',
          body: JSON.stringify({ targetFilter: params.target_filter }),
        })
        const response = raw as CenterResponse

        if (response.code !== 0) {
          return { content: [{ type: 'text' as const, text: JSON.stringify({ error: response.message, code: response.code }) }] }
        }

        await audit('preview_edm_targets', { count: response.data?.count ?? 0 })
        return { content: [{ type: 'text' as const, text: JSON.stringify(response.data, null, 2) }] }
      } catch (err) {
        const msg = err instanceof Error ? err.message : String(err)
        return { content: [{ type: 'text' as const, text: JSON.stringify({ error: msg }) }] }
      }
    }
  )
}
