/**
 * Admin statistics and analytics tools.
 */

import { z } from 'zod'
import { defineApiTool, type ToolRegistration } from '../tool-factory.js'
import { brandFilter } from './brand-params.js'

export const statsTools: ToolRegistration[] = [
  defineApiTool({
    name: 'device_statistics',
    description: 'Get aggregate device statistics (total, active, by platform).',
    group: 'stats',
    params: { brand: brandFilter },
    path: '/app/devices/statistics',
  }),

  defineApiTool({
    name: 'active_devices',
    description: 'List currently active devices with pagination.',
    group: 'stats',
    params: {
      page: z.number().optional().describe('Page number'),
      page_size: z.number().optional().describe('Page size'),
    },
    path: '/app/devices/active',
    mapQuery: (p) => {
      const q: Record<string, string> = {}
      if (p.page !== undefined) q.page = String(p.page)
      if (p.page_size !== undefined) q.pageSize = String(p.page_size)
      return q
    },
  }),

  defineApiTool({
    name: 'user_statistics',
    description: 'Get aggregate user statistics (total, paid, trial, churned).',
    group: 'stats',
    params: { brand: brandFilter },
    path: '/app/users/statistics',
  }),

  defineApiTool({
    name: 'order_statistics',
    description: 'Get aggregate order statistics (revenue, count by period).',
    group: 'stats',
    params: { brand: brandFilter },
    path: '/app/orders/statistics',
  }),

  defineApiTool({
    name: 'usage_overview',
    description: 'Get platform usage overview (bandwidth, connections, peak hours).',
    group: 'stats',
    path: '/app/stats/overview',
  }),

  defineApiTool({
    name: 'funnel_paths',
    description: 'List the registered conversion funnel keys (path keys, brand-agnostic) that can be passed to funnel.',
    group: 'stats',
    path: '/app/stats/funnels',
  }),

  defineApiTool({
    name: 'funnel',
    description: 'Get conversion funnel step counts for a funnel key, optionally narrowed by brand and date range and grouped by a dimension.',
    group: 'stats',
    params: {
      key: z.string().regex(/^[a-z_]+$/).describe('Funnel key from funnel_paths (lowercase letters and underscores)'),
      brand: brandFilter,
      from: z.string().regex(/^\d{4}-\d{2}-\d{2}$/).optional().describe('Start date YYYY-MM-DD'),
      to: z.string().regex(/^\d{4}-\d{2}-\d{2}$/).optional().describe('End date YYYY-MM-DD'),
      group_by: z.string().optional().describe('Dimension to group by (e.g. source)'),
    },
    path: (p) => `/app/stats/funnels/${encodeURIComponent(String(p.key))}`,
    mapQuery: (p) => {
      // mapQuery replaces the auto-built query, so brand must be forwarded here.
      const q: Record<string, string> = {}
      if (p.brand !== undefined) q.brand = String(p.brand)
      if (p.from !== undefined) q.from = String(p.from)
      if (p.to !== undefined) q.to = String(p.to)
      if (p.group_by !== undefined) q.groupBy = String(p.group_by)
      return q
    },
  }),

  defineApiTool({
    name: 'retention',
    description: 'Get monthly cohort retention for paid users or active users.',
    group: 'stats',
    params: {
      brand: brandFilter,
      metric: z.enum(['paid', 'active']).describe('Retention metric'),
      months: z.number().int().min(1).max(24).optional().describe('Number of monthly cohorts (1-24)'),
    },
    path: '/app/stats/retention',
  }),

  defineApiTool({
    name: 'survey_stats',
    description: 'Get survey response statistics and aggregates.',
    group: 'surveys',
    path: '/app/surveys/stats',
  }),
]
