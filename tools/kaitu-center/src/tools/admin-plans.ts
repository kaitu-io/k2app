/**
 * Admin subscription plan management tools.
 */

import { z } from 'zod'
import { defineApiTool, type ToolRegistration } from '../tool-factory.js'

export const planTools: ToolRegistration[] = [
  defineApiTool({
    name: 'list_admin_plans',
    description: 'List all subscription plans (admin view with hidden plans).',
    group: 'plans',
    path: '/app/plans',
  }),

  defineApiTool({
    name: 'create_plan',
    description: 'Create a new subscription plan.',
    group: 'plans.write',
    method: 'POST',
    params: {
      pid: z.string().describe('Plan ID string (e.g. "monthly", "yearly")'),
      label: z.string().describe('Display label'),
      price: z.number().describe('Price in cents'),
      origin_price: z.number().optional().describe('Original price in cents (for showing discount)'),
      month: z.number().describe('Subscription months'),
      highlight: z.string().optional().describe('Highlight text'),
      is_active: z.boolean().optional().describe('Whether plan is active'),
    },
    path: '/app/plans',
    mapBody: (p) => ({
      pid: p.pid, label: p.label, price: p.price,
      originPrice: p.origin_price, month: p.month,
      highlight: p.highlight, isActive: p.is_active,
    }),
  }),

  defineApiTool({
    name: 'update_plan',
    description: 'Update an existing plan. Only provided fields are changed.',
    group: 'plans.write',
    method: 'PUT',
    params: {
      id: z.number().describe('Plan ID'),
      label: z.string().optional().describe('Display label'),
      price: z.number().optional().describe('Price in cents'),
      origin_price: z.number().optional().describe('Original price in cents'),
      month: z.number().optional().describe('Subscription months'),
      highlight: z.string().optional().describe('Highlight text'),
      is_active: z.boolean().optional().describe('Whether plan is active'),
    },
    path: (p) => `/app/plans/${p.id}`,
    mapBody: (p) => ({
      label: p.label, price: p.price, originPrice: p.origin_price,
      month: p.month, highlight: p.highlight, isActive: p.is_active,
    }),
  }),

  defineApiTool({
    name: 'delete_plan',
    description: 'Soft-delete a plan by ID.',
    group: 'plans.write',
    method: 'DELETE',
    params: {
      id: z.number().describe('Plan ID'),
    },
    path: (p) => `/app/plans/${p.id}`,
  }),

  defineApiTool({
    name: 'restore_plan',
    description: 'Restore a previously deleted plan.',
    group: 'plans.write',
    method: 'POST',
    params: {
      id: z.number().describe('Plan ID'),
    },
    path: (p) => `/app/plans/${p.id}/restore`,
  }),
]
