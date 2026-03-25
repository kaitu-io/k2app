/**
 * Admin license key management tools.
 */

import { z } from 'zod'
import { defineApiTool, type ToolRegistration } from '../tool-factory.js'

export const licenseKeyTools: ToolRegistration[] = [
  defineApiTool({
    name: 'list_license_keys',
    description: 'List license keys with pagination.',
    group: 'license_keys',
    params: {
      page: z.number().optional().describe('Page number'),
      page_size: z.number().optional().describe('Page size'),
    },
    path: '/app/license-keys',
    mapQuery: (p) => {
      const q: Record<string, string> = {}
      if (p.page !== undefined) q.page = String(p.page)
      if (p.page_size !== undefined) q.pageSize = String(p.page_size)
      return q
    },
  }),

  defineApiTool({
    name: 'create_license_keys',
    description: 'Generate a batch of license keys.',
    group: 'license_keys.write',
    method: 'POST',
    params: {
      count: z.number().describe('Number of keys to create'),
      plan_days: z.number().describe('Membership days per key'),
      expires_in_days: z.number().optional().describe('Key expiration in days'),
      note: z.string().optional().describe('Note for the batch'),
    },
    path: '/app/license-keys',
    mapBody: (p) => ({
      count: p.count, planDays: p.plan_days,
      expiresInDays: p.expires_in_days, note: p.note,
    }),
  }),

  defineApiTool({
    name: 'delete_license_key',
    description: 'Delete a single license key by ID.',
    group: 'license_keys.write',
    method: 'DELETE',
    params: {
      id: z.number().describe('License key ID'),
    },
    path: (p) => `/app/license-keys/${p.id}`,
  }),

  defineApiTool({
    name: 'license_key_stats',
    description: 'Get aggregate license key statistics (total, used, expired).',
    group: 'license_keys',
    path: '/app/license-keys/stats',
  }),
]
