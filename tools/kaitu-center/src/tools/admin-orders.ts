/**
 * Admin order management tools.
 */

import { z } from 'zod'
import { defineApiTool, type ToolRegistration } from '../tool-factory.js'
import { brandFilter, withBrand } from './brand-params.js'

export const orderTools: ToolRegistration[] = [
  defineApiTool({
    name: 'list_orders',
    description: 'List orders with pagination. Filter by email and/or brand (an order\'s brand is its buyer\'s brand).',
    group: 'orders',
    params: {
      page: z.number().optional().describe('Page number'),
      page_size: z.number().optional().describe('Page size'),
      email: z.string().optional().describe('Filter by user email'),
      brand: brandFilter,
    },
    path: '/app/orders',
    mapQuery: (p) => {
      const q: Record<string, string> = {}
      if (p.page !== undefined) q.page = String(p.page)
      if (p.page_size !== undefined) q.pageSize = String(p.page_size)
      // The API filters by (loginProvider, loginIdentity); a bare `email` param is ignored.
      if (p.email !== undefined) {
        q.loginProvider = 'email'
        q.loginIdentity = String(p.email)
      }
      return withBrand(q, p.brand)
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

  defineApiTool({
    name: 'refund_order',
    description: 'Refund a paid order. Credits the user wallet, revokes granted Pro days, and reverses retailer cashback. Requires dual-admin approval (superadmin auto-executes). Full refunds only.',
    group: 'orders.write',
    method: 'POST',
    params: {
      uuid: z.string().describe('Order UUID'),
      reason: z.string().min(2).max(500).describe('Refund reason (2-500 chars, required)'),
    },
    path: (p) => `/app/orders/${p.uuid}/refund`,
    mapBody: (p) => ({ reason: p.reason }),
  }),
]
