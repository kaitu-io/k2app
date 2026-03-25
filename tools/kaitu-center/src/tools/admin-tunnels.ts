/**
 * Admin tunnel management tools.
 */

import { z } from 'zod'
import { defineApiTool, type ToolRegistration } from '../tool-factory.js'

export const tunnelTools: ToolRegistration[] = [
  defineApiTool({
    name: 'list_tunnels',
    description: 'List tunnels with pagination.',
    group: 'tunnels',
    params: {
      page: z.number().optional().describe('Page number'),
      page_size: z.number().optional().describe('Page size'),
    },
    path: '/app/tunnels',
    mapQuery: (p) => {
      const q: Record<string, string> = {}
      if (p.page !== undefined) q.page = String(p.page)
      if (p.page_size !== undefined) q.pageSize = String(p.page_size)
      return q
    },
  }),

  defineApiTool({
    name: 'update_tunnel',
    description: 'Update a tunnel (rename or enable/disable).',
    group: 'tunnels.write',
    method: 'PUT',
    params: {
      id: z.number().describe('Tunnel ID'),
      name: z.string().optional().describe('New tunnel name'),
      enabled: z.boolean().optional().describe('Enable or disable the tunnel'),
    },
    path: (p) => `/app/tunnels/${p.id}`,
  }),

  defineApiTool({
    name: 'delete_tunnel',
    description: 'Delete a tunnel by ID.',
    group: 'tunnels.write',
    method: 'DELETE',
    params: {
      id: z.number().describe('Tunnel ID'),
    },
    path: (p) => `/app/tunnels/${p.id}`,
  }),
]
