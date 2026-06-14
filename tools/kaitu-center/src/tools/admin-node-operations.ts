/**
 * Node-operation queue tools (private-node lifecycle: provision/change_ip/stop/destroy).
 *
 * Center emits NodeOperation work items; human operators (and a future agent) claim
 * them and execute the real cloud action externally, then report progress/result.
 *   list_node_operations   — discover operations (filter action/status)
 *   create_node_operation  — manually dispatch change_ip / ad-hoc stop / destroy
 *   claim_node_operation   — atomically lease one (provision also returns identity)
 *   update_node_operation  — report progress/done/failed (provision done is set by self-register, not here)
 */

import { z } from 'zod'
import { defineApiTool, type ToolRegistration } from '../tool-factory.js'

export const nodeOperationTools: ToolRegistration[] = [
  defineApiTool({
    name: 'list_node_operations',
    description:
      'List private-node operations (provision/change_ip/stop/destroy). Filter by action and/or ' +
      'status (queued/claimed/in_progress/done/failed/canceled). Use status=queued to find ' +
      'claimable work. Paginated via page/pageSize; returns data.items[] + pagination.',
    group: 'cloud',
    params: {
      action: z.enum(['provision', 'change_ip', 'stop', 'destroy']).optional().describe('Filter by action'),
      status: z.enum(['queued', 'claimed', 'in_progress', 'done', 'failed', 'canceled']).optional().describe('Filter by status'),
      page: z.number().optional(),
      pageSize: z.number().optional(),
    },
    path: '/app/node-operations',
    mapQuery: (p) => {
      const q: Record<string, string> = {}
      if (p.action !== undefined) q.action = String(p.action)
      if (p.status !== undefined) q.status = String(p.status)
      if (p.page !== undefined) q.page = String(p.page)
      if (p.pageSize !== undefined) q.pageSize = String(p.pageSize)
      return q
    },
  }),

  defineApiTool({
    name: 'create_node_operation',
    description:
      'Manually dispatch a node operation for a subscription. action MUST be change_ip, stop or ' +
      'destroy — provision is order-triggered only and rejected here. If an open operation of the ' +
      'same action already exists for the subscription, Center returns a conflict. params is an ' +
      'action-specific object (change_ip: {targetRegion?, reason}; stop/destroy: {reason}).',
    group: 'cloud.write',
    method: 'POST',
    params: {
      subId: z.number().describe('PrivateNodeSubscription ID'),
      action: z.enum(['change_ip', 'stop', 'destroy']).describe('Operation action'),
      params: z.record(z.string(), z.any()).optional().describe('Action-specific params object'),
    },
    path: '/app/node-operations',
    mapBody: (p) => ({ subId: p.subId, action: p.action, params: p.params ?? {} }),
  }),

  defineApiTool({
    name: 'claim_node_operation',
    description:
      'Atomically claim (lease) a queued operation. Returns data.operation. For action=provision ' +
      'it ALSO returns data.identity = { claimToken, centerUrl, domain } — claimToken is one-time, ' +
      'bake it into the VPS .env as K2_PRIVATE_CLAIM so the node can self-register. If already ' +
      'claimed/not found, Center returns a conflict — list queued again, do not retry blindly.',
    group: 'cloud.write',
    method: 'POST',
    params: {
      id: z.number().describe('Operation ID to claim'),
      holder: z.string().describe('Operator/agent identity recorded as holder'),
      leaseSeconds: z.number().optional().describe('Lease duration seconds (default 600)'),
    },
    path: (p) => `/app/node-operations/${p.id}/claim`,
    mapBody: (p) => ({ holder: p.holder, leaseSeconds: p.leaseSeconds }),
  }),

  defineApiTool({
    name: 'update_node_operation',
    description:
      'Report progress/result for an operation. status: in_progress | done | failed | canceled. ' +
      'NOTE: for action=provision, done is REJECTED (provision completion is set by node ' +
      'self-registration). result is an action-specific object (e.g. change_ip: {oldIp,newIp}); ' +
      'pass error when status=failed.',
    group: 'cloud.write',
    method: 'POST',
    params: {
      id: z.number().describe('Operation ID'),
      status: z.enum(['in_progress', 'done', 'failed', 'canceled']).describe('New status'),
      result: z.record(z.string(), z.any()).optional().describe('Action-specific result object'),
      error: z.string().optional().describe('Failure reason (when status=failed)'),
    },
    path: (p) => `/app/node-operations/${p.id}/update`,
    mapBody: (p) => ({ status: p.status, result: p.result, error: p.error }),
  }),
]
