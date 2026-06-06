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
      code: z.string().describe('Campaign code'),
      name: z.string().describe('Campaign name'),
      type: z.string().optional().describe('Campaign type (e.g. "discount", "gift")'),
      value: z.number().optional().describe('Campaign value (discount amount or percentage)'),
      start_at: z.number().optional().describe('Start time (unix timestamp)'),
      end_at: z.number().optional().describe('End time (unix timestamp)'),
      description: z.string().optional().describe('Campaign description'),
      is_active: z.boolean().optional().describe('Whether campaign is active'),
      matcher_type: z.string().optional().describe('Matcher type (e.g. "first_order", "vip", "all")'),
      matcher_params: z.string().optional().describe('Matcher params JSON'),
    },
    path: '/app/campaigns',
    mapBody: (p) => ({
      code: p.code, name: p.name, type: p.type, value: p.value,
      startAt: p.start_at, endAt: p.end_at,
      description: p.description, isActive: p.is_active,
      matcherType: p.matcher_type, matcherParams: p.matcher_params,
    }),
  }),

  defineApiTool({
    name: 'update_campaign',
    description: 'Update an existing campaign. The backend replaces the whole record, so code/name/type/value/start_at/end_at are all required (fetch current values via get_campaign first).',
    group: 'campaigns.write',
    method: 'PUT',
    params: {
      id: z.number().describe('Campaign ID'),
      code: z.string().describe('Campaign code (required by backend; pass the existing code to keep it)'),
      name: z.string().describe('Campaign name'),
      type: z.string().describe('Campaign type (e.g. "discount", "coupon")'),
      value: z.number().describe('Campaign value (discount percentage or coupon amount)'),
      start_at: z.number().describe('Start time (unix timestamp)'),
      end_at: z.number().describe('End time (unix timestamp)'),
      description: z.string().optional().describe('Campaign description'),
      is_active: z.boolean().optional().describe('Whether campaign is active'),
      matcher_type: z.string().describe('Audience matcher (required by backend): "first_order" = 新客 (not yet paid), "vip" = 老客 (already paid), "all" = anyone, "paid_before"/"paid_before_active" = time-windowed'),
      matcher_params: z.string().optional().describe('Matcher params JSON (e.g. {"beforeDate": 1735689600} for paid_before*)'),
    },
    path: (p) => `/app/campaigns/${p.id}`,
    mapBody: (p) => ({
      code: p.code, name: p.name, type: p.type, value: p.value,
      startAt: p.start_at, endAt: p.end_at,
      description: p.description, isActive: p.is_active,
      matcherType: p.matcher_type, matcherParams: p.matcher_params,
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
]
