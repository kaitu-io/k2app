/**
 * Admin campaign management tools.
 */

import { z } from 'zod'
import { defineApiTool, type ToolRegistration } from '../tool-factory.js'

export const campaignTools: ToolRegistration[] = [
  defineApiTool({
    name: 'list_campaigns',
    description: 'List all campaigns.',
    group: 'campaigns',
    path: '/app/campaigns',
  }),

  defineApiTool({
    name: 'get_campaign',
    description: 'Get a single campaign by ID.',
    group: 'campaigns',
    params: {
      id: z.number().describe('Campaign ID'),
    },
    path: (p) => `/app/campaigns/${p.id}`,
  }),

  defineApiTool({
    name: 'create_campaign',
    description: 'Create a new campaign with optional discount and date range.',
    group: 'campaigns.write',
    method: 'POST',
    params: {
      code: z.string().describe('Campaign code (unique slug)'),
      name: z.string().describe('Campaign display name'),
      discount_percent: z.number().optional().describe('Discount percentage (0-100)'),
      start_date: z.string().optional().describe('Start date (ISO 8601)'),
      end_date: z.string().optional().describe('End date (ISO 8601)'),
    },
    path: '/app/campaigns',
    mapBody: (p) => ({
      code: p.code,
      name: p.name,
      discountPercent: p.discount_percent,
      startDate: p.start_date,
      endDate: p.end_date,
    }),
  }),

  defineApiTool({
    name: 'update_campaign',
    description: 'Update an existing campaign. Only provided fields are changed.',
    group: 'campaigns.write',
    method: 'PUT',
    params: {
      id: z.number().describe('Campaign ID'),
      name: z.string().optional().describe('Campaign display name'),
      discount_percent: z.number().optional().describe('Discount percentage (0-100)'),
      start_date: z.string().optional().describe('Start date (ISO 8601)'),
      end_date: z.string().optional().describe('End date (ISO 8601)'),
    },
    path: (p) => `/app/campaigns/${p.id}`,
    mapBody: (p) => ({
      name: p.name,
      discountPercent: p.discount_percent,
      startDate: p.start_date,
      endDate: p.end_date,
    }),
  }),

  defineApiTool({
    name: 'delete_campaign',
    description: 'Delete a campaign by ID.',
    group: 'campaigns.write',
    method: 'DELETE',
    params: {
      id: z.number().describe('Campaign ID'),
    },
    path: (p) => `/app/campaigns/${p.id}`,
  }),

  defineApiTool({
    name: 'campaign_stats',
    description: 'Get aggregate statistics for a campaign by code.',
    group: 'campaigns',
    params: {
      code: z.string().describe('Campaign code'),
    },
    path: (p) => `/app/campaigns/code/${p.code}/stats`,
  }),

  defineApiTool({
    name: 'campaign_funnel',
    description: 'Get campaign conversion funnel by campaign code.',
    group: 'campaigns',
    params: {
      code: z.string().describe('Campaign code'),
    },
    path: (p) => `/app/campaigns/code/${p.code}/funnel`,
  }),

  defineApiTool({
    name: 'campaign_orders',
    description: 'List orders attributed to a campaign by code.',
    group: 'campaigns',
    params: {
      code: z.string().describe('Campaign code'),
    },
    path: (p) => `/app/campaigns/code/${p.code}/orders`,
  }),

  defineApiTool({
    name: 'issue_campaign_keys',
    description: 'Issue license keys for a campaign. Specify how many keys to generate.',
    group: 'campaigns.write',
    method: 'POST',
    params: {
      id: z.number().describe('Campaign ID'),
      count: z.number().describe('Number of keys to issue'),
    },
    path: (p) => `/app/campaigns/${p.id}/issue-keys`,
  }),
]
