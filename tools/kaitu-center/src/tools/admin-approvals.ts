/**
 * Admin approval workflow tools.
 */

import { z } from 'zod'
import { defineApiTool, type ToolRegistration } from '../tool-factory.js'

export const approvalTools: ToolRegistration[] = [
  defineApiTool({
    name: 'list_approvals',
    description: 'List approval requests. Filter by status (pending, approved, rejected, cancelled).',
    group: 'approvals',
    params: {
      status: z.string().optional().describe('Filter by status'),
      page: z.number().optional().describe('Page number'),
    },
    path: '/app/approvals',
  }),

  defineApiTool({
    name: 'get_approval',
    description: 'Get full details for a single approval request.',
    group: 'approvals',
    params: {
      id: z.number().describe('Approval ID'),
    },
    path: (p) => `/app/approvals/${p.id}`,
  }),

  defineApiTool({
    name: 'cancel_approval',
    description: 'Cancel a pending approval request.',
    group: 'approvals',
    method: 'POST',
    params: {
      id: z.number().describe('Approval ID'),
    },
    path: (p) => `/app/approvals/${p.id}/cancel`,
  }),

  defineApiTool({
    name: 'approve_approval',
    description: 'Approve a pending approval request.',
    group: 'approvals.write',
    method: 'POST',
    params: {
      id: z.number().describe('Approval ID'),
    },
    path: (p) => `/app/approvals/${p.id}/approve`,
  }),

  defineApiTool({
    name: 'reject_approval',
    description: 'Reject a pending approval request with an optional reason.',
    group: 'approvals.write',
    method: 'POST',
    params: {
      id: z.number().describe('Approval ID'),
      reason: z.string().optional().describe('Rejection reason'),
    },
    path: (p) => `/app/approvals/${p.id}/reject`,
  }),
]
