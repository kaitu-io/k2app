/**
 * Admin license key batch management tools.
 */

import { z } from 'zod'
import { defineApiTool, type ToolRegistration } from '../tool-factory.js'

export const licenseKeyTools: ToolRegistration[] = [
  defineApiTool({
    name: 'list_license_key_batches',
    description: 'List license key batches with pagination.',
    group: 'license_keys',
    params: {
      page: z.number().optional().describe('Page number'),
      page_size: z.number().optional().describe('Page size'),
      source_tag: z.string().optional().describe('Filter by source tag'),
    },
    path: '/app/license-key-batches',
    mapQuery: (p) => {
      const q: Record<string, string> = {}
      if (p.page !== undefined) q.page = String(p.page)
      if (p.page_size !== undefined) q.pageSize = String(p.page_size)
      if (p.source_tag !== undefined) q.sourceTag = String(p.source_tag)
      return q
    },
  }),

  defineApiTool({
    name: 'get_license_key_batch',
    description: 'Get license key batch detail with conversion stats.',
    group: 'license_keys',
    params: {
      batch_id: z.number().describe('Batch ID'),
    },
    path: (p) => `/app/license-key-batches/${p.batch_id}`,
  }),

  defineApiTool({
    name: 'create_license_key_batch',
    description: 'Create a license key batch (requires approval). Generates keys upon approval.',
    group: 'license_keys.write',
    method: 'POST',
    params: {
      name: z.string().describe('Batch name'),
      source_tag: z.string().optional().describe('Channel tag (twitter, kol-xxx, winback)'),
      recipient_matcher: z.enum(['all', 'never_paid']).describe('Who can redeem'),
      plan_days: z.number().describe('Membership days per key'),
      quantity: z.number().describe('Number of keys (1-10000)'),
      expires_in_days: z.number().describe('Key expiration in days'),
      note: z.string().optional().describe('Note'),
    },
    path: '/app/license-key-batches',
    mapBody: (p) => ({
      name: p.name,
      sourceTag: p.source_tag || '',
      recipientMatcher: p.recipient_matcher,
      planDays: p.plan_days,
      quantity: p.quantity,
      expiresInDays: p.expires_in_days,
      note: p.note || '',
    }),
  }),

  defineApiTool({
    name: 'list_license_key_batch_keys',
    description: 'List keys in a batch with status filter and pagination.',
    group: 'license_keys',
    params: {
      batch_id: z.number().describe('Batch ID'),
      status: z.enum(['all', 'used', 'unused', 'expired']).optional().describe('Filter by status'),
      page: z.number().optional().describe('Page number'),
      page_size: z.number().optional().describe('Page size'),
    },
    path: (p) => `/app/license-key-batches/${p.batch_id}/keys`,
    mapQuery: (p) => {
      const q: Record<string, string> = {}
      if (p.status !== undefined) q.status = String(p.status)
      if (p.page !== undefined) q.page = String(p.page)
      if (p.page_size !== undefined) q.pageSize = String(p.page_size)
      return q
    },
  }),

  defineApiTool({
    name: 'license_key_batch_stats',
    description: 'Get license key batch stats with conversion rate. Omit batch_id for all batches.',
    group: 'license_keys',
    params: {
      batch_id: z.number().optional().describe('Batch ID (omit for all)'),
    },
    path: (p) => p.batch_id ? `/app/license-key-batches/${p.batch_id}` : '/app/license-key-batches/stats',
  }),

  defineApiTool({
    name: 'license_key_batch_stats_by_source',
    description: 'Get license key stats aggregated by source tag (channel).',
    group: 'license_keys',
    path: '/app/license-key-batches/stats/by-source',
  }),

  defineApiTool({
    name: 'invalidate_license_key_batch',
    description: 'Invalidate a batch — deletes all unused keys, keeps batch and redeemed keys for stats. Requires approval.',
    group: 'license_keys.write',
    method: 'DELETE',
    params: {
      batch_id: z.number().describe('Batch ID'),
    },
    path: (p) => `/app/license-key-batches/${p.batch_id}`,
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
    name: 'list_license_keys',
    description: 'List all license keys (filter by batchId).',
    group: 'license_keys',
    params: {
      page: z.number().optional().describe('Page number'),
      page_size: z.number().optional().describe('Page size'),
      batch_id: z.number().optional().describe('Filter by batch ID'),
      is_used: z.boolean().optional().describe('Filter by used status'),
    },
    path: '/app/license-keys',
    mapQuery: (p) => {
      const q: Record<string, string> = {}
      if (p.page !== undefined) q.page = String(p.page)
      if (p.page_size !== undefined) q.pageSize = String(p.page_size)
      if (p.batch_id !== undefined) q.batchId = String(p.batch_id)
      if (p.is_used !== undefined) q.isUsed = String(p.is_used)
      return q
    },
  }),
]
