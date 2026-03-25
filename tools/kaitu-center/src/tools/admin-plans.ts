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
      name: z.string().describe('Plan name'),
      price: z.number().describe('Price amount'),
      days: z.number().describe('Subscription duration in days'),
      currency: z.string().optional().describe('Currency code (default USD)'),
    },
    path: '/app/plans',
  }),

  defineApiTool({
    name: 'update_plan',
    description: 'Update an existing plan. Only provided fields are changed.',
    group: 'plans.write',
    method: 'PUT',
    params: {
      id: z.number().describe('Plan ID'),
      name: z.string().optional().describe('Plan name'),
      price: z.number().optional().describe('Price amount'),
      days: z.number().optional().describe('Subscription duration in days'),
    },
    path: (p) => `/app/plans/${p.id}`,
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
