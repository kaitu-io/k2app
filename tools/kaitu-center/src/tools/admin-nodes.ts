/**
 * Admin node management tools.
 */

import { z } from 'zod'
import { defineApiTool, type ToolRegistration } from '../tool-factory.js'

export const nodeTools: ToolRegistration[] = [
  defineApiTool({
    name: 'update_node',
    description:
      'Update a physical node (name/country/ipv6/ipType). ipType: residential|non_residential|unknown.',
    group: 'nodes.write',
    method: 'PUT',
    params: {
      ipv4: z.string().describe('Node IPv4 (path key)'),
      name: z.string().optional().describe('New node name'),
      country: z.string().optional().describe('ISO 3166-1 alpha-2'),
      ipv6: z.string().optional().describe('IPv6 address'),
      ipType: z
        .enum(['residential', 'non_residential', 'unknown'])
        .optional()
        .describe('节点出口 IP 性质;住宅IP=residential'),
    },
    path: (p) => `/app/nodes/${p.ipv4}`,
  }),
]
