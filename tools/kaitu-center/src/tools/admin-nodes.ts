/**
 * Admin node management tools.
 */

import { z } from 'zod'
import { defineApiTool, type ToolRegistration } from '../tool-factory.js'
import { brandFilter, withBrand } from './brand-params.js'

export const nodeTools: ToolRegistration[] = [
  defineApiTool({
    name: 'get_node_distribution',
    description:
      'Live node placement: per-node active devices (traffic in the last 5 minutes), load and status, plus balance summaries for the fleet and each country. status: normal|idle|overloaded count towards balance; exhausted|offline|hidden|private do not. verdict: ok|skewed (idle nodes while others carry devices)|imbalanced (idle and overloaded at once). Null load fields mean unknown (node sidecar not upgraded or silent), not zero. Use to accept a client node-selection strategy.',
    group: 'nodes',
    params: {
      brand: brandFilter,
    },
    path: '/app/nodes/distribution',
    mapQuery: (p) => withBrand({}, p.brand),
  }),

  defineApiTool({
    name: 'get_node_distribution_trend',
    description:
      'Placement over time in completed 5-minute buckets, for comparing a client strategy before and after release. Covers shared nodes only. topShare is the busiest node share; activeNodes vs nodesInScope shows how many nodes carried traffic. Data is kept for 7 days.',
    group: 'nodes',
    params: {
      hours: z.number().int().min(1).max(168).optional().describe('Lookback in hours, default 24'),
      brand: brandFilter,
      country: z.string().optional().describe('ISO 3166-1 alpha-2 country code'),
    },
    path: '/app/nodes/distribution/trend',
    mapQuery: (p) => {
      const q: Record<string, string> = {}
      if (p.hours !== undefined) q.hours = String(p.hours)
      if (p.country) q.country = String(p.country)
      return withBrand(q, p.brand)
    },
  }),

  defineApiTool({
    name: 'update_node',
    description:
      'Update a physical node (name/country/ipv6/ipType). ipType: residential|non_residential|unknown',
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
