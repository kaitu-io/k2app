/**
 * Admin node management tools.
 */

import { z } from 'zod'
import { defineApiTool, type ToolRegistration } from '../tool-factory.js'

export const nodeTools: ToolRegistration[] = [
  defineApiTool({
    name: 'update_node',
    description:
      'Update a physical node (name/country/ipv6/ipType/pool). ipType: residential|non_residential|unknown. ' +
      'pool: "" = member node, "trial" = trial line (free-trial users only see and can only connect to trial-pool nodes).',
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
      pool: z
        .enum(['', 'trial'])
        .optional()
        .describe('节点池:""=会员节点,"trial"=体验线路(免费试用用户只能拿到/连这个池)'),
    },
    path: (p) => `/app/nodes/${p.ipv4}`,
  }),
]
