/**
 * Admin statistics and analytics tools.
 */

import { z } from 'zod'
import { defineApiTool, type ToolRegistration } from '../tool-factory.js'

export const statsTools: ToolRegistration[] = [
  defineApiTool({
    name: 'device_statistics',
    description: 'Get aggregate device statistics (total, active, by platform).',
    group: 'stats',
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
    path: '/app/users/statistics',
  }),

  defineApiTool({
    name: 'order_statistics',
    description: 'Get aggregate order statistics (revenue, count by period).',
    group: 'stats',
    path: '/app/orders/statistics',
  }),

  defineApiTool({
    name: 'usage_overview',
    description: 'Get platform usage overview (bandwidth, connections, peak hours).',
    group: 'stats',
    path: '/app/stats/overview',
  }),

  defineApiTool({
    name: 'survey_stats',
    description: 'Get survey response statistics and aggregates.',
    group: 'surveys',
    path: '/app/surveys/stats',
  }),
]
