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
    description: 'List the registered conversion funnel keys (path keys, brand-agnostic) that can be passed to funnel, plus groupDims — the only values funnel accepts as group_by.',
    group: 'stats',
    path: '/app/stats/funnels',
  }),

  defineApiTool({
    name: 'funnel',
    description: 'Get conversion funnel step counts for a funnel key, optionally narrowed by brand and date range and grouped by a dimension. The range is from..to with `to` inclusive, at most 90 days. group_by must be one of the groupDims returned by funnel_paths.',
    group: 'stats',
    params: {
      key: z.string().regex(/^[a-z_]+$/).describe('Funnel key from funnel_paths (lowercase letters and underscores)'),
      brand: brandFilter,
      from: z.string().regex(/^\d{4}-\d{2}-\d{2}$/).optional().describe('Start date YYYY-MM-DD'),
      to: z.string().regex(/^\d{4}-\d{2}-\d{2}$/).optional().describe('End date YYYY-MM-DD, inclusive; from..to spans at most 90 days'),
      group_by: z.string().optional().describe('Dimension to group by — must be one of the groupDims returned by funnel_paths (e.g. source)'),
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
    description: 'Get cohort retention. metric=paid: monthly first-payment cohorts with M1/M3/M6/M12 retention (coverage-based). metric=active: daily first-seen cohorts with D1/D7/D30 retention. `months` applies to paid only.',
    group: 'stats',
    params: {
      brand: brandFilter,
      metric: z.enum(['paid', 'active']).describe('Retention metric'),
      months: z.number().int().min(1).max(24).optional().describe('Number of monthly cohorts (1-24); paid only, ignored for active'),
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
