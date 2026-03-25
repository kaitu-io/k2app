/**
 * Admin order management tools.
 */

import { z } from 'zod'
import { defineApiTool, type ToolRegistration } from '../tool-factory.js'

export const orderTools: ToolRegistration[] = [
  defineApiTool({
    name: 'list_orders',
    description: 'List orders with pagination. Filter by email.',
    group: 'orders',
    params: {
      page: z.number().optional().describe('Page number'),
      page_size: z.number().optional().describe('Page size'),
      email: z.string().optional().describe('Filter by user email'),
    },
    path: '/app/orders',
    mapQuery: (p) => {
      const q: Record<string, string> = {}
      if (p.page !== undefined) q.page = String(p.page)
      if (p.page_size !== undefined) q.pageSize = String(p.page_size)
      if (p.email !== undefined) q.email = String(p.email)
      return q
    },
  }),

  defineApiTool({
    name: 'get_order_detail',
    description: 'Get full details for a single order by UUID.',
    group: 'orders',
    params: {
      uuid: z.string().describe('Order UUID'),
    },
    path: (p) => `/app/orders/${p.uuid}`,
  }),
]
