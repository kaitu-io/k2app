/**
 * Node-provisioning job tools for the private-node provisioning agent.
 *
 * Center emits "provision jobs" — work items describing a VPS that an external
 * AI agent must stand up (region, bundle, image, k2 version, traffic, IP type,
 * domain). The agent uses these three tools to drive the lifecycle:
 *
 *   1. list_provisioning_intents  — discover queued/claimable jobs
 *   2. claim_provisioning_intent  — atomically lease one + get the one-time claim token
 *   3. report_provisioning        — report progress (provisioning) or failure
 *
 * The terminal `succeeded` status is NOT settable here — it is set by the node
 * itself when it self-registers with Center using the baked-in claim token.
 */

import { z } from 'zod'
import { defineApiTool, type ToolRegistration } from '../tool-factory.js'

export const provisionJobTools: ToolRegistration[] = [
  defineApiTool({
    name: 'list_provisioning_intents',
    description:
      'List pending node-provisioning work items (provision jobs). Each job describes a VPS the ' +
      'provisioning agent must stand up: region, bundleId, imageId, composeVariant, k2Version, ' +
      'trafficTotalBytes, ipType, domain. Filter with status=queued to find jobs that are ' +
      'claimable right now (other values: claimed/provisioning/succeeded/failed). Paginated ' +
      'via page/pageSize; returns data.items[] plus pagination metadata.',
    group: 'cloud',
    params: {
      status: z
        .enum(['queued', 'claimed', 'provisioning', 'succeeded', 'failed'])
        .optional()
        .describe('Filter by job status. Use "queued" to list claimable jobs.'),
      page: z.number().optional().describe('Page number'),
      pageSize: z.number().optional().describe('Page size'),
    },
    path: '/app/provision-jobs',
    mapQuery: (p) => {
      const q: Record<string, string> = {}
      if (p.status !== undefined) q.status = String(p.status)
      if (p.page !== undefined) q.page = String(p.page)
      if (p.pageSize !== undefined) q.pageSize = String(p.pageSize)
      return q
    },
  }),

  defineApiTool({
    name: 'claim_provisioning_intent',
    description:
      'Atomically claim (lease) a queued provision job so this agent owns it. Returns ' +
      'data.job (the full job) plus data.identity = { claimToken, centerUrl, domain }. ' +
      'WARNING: claimToken is one-time and is ONLY ever returned by this call — it is never ' +
      'shown again. You MUST bake it into the new VPS .env as K2_PRIVATE_CLAIM so the node can ' +
      'self-register with Center. If the job is already claimed or not found, Center returns an ' +
      'error envelope (409-ish) — do not retry blindly; list queued jobs again. holder identifies ' +
      'this agent for the lease; leaseSeconds defaults to 600 if omitted.',
    group: 'cloud.write',
    method: 'POST',
    params: {
      id: z.number().describe('Provision job ID to claim'),
      holder: z.string().describe('Agent identity claiming the lease (recorded as holder)'),
      leaseSeconds: z
        .number()
        .optional()
        .describe('Lease duration in seconds. Defaults to 600 if omitted.'),
    },
    path: (p) => `/app/provision-jobs/${p.id}/claim`,
    mapBody: (p) => ({
      holder: p.holder,
      leaseSeconds: p.leaseSeconds,
    }),
  }),

  defineApiTool({
    name: 'report_provisioning',
    description:
      'Report progress or failure for a claimed provision job. status must be "provisioning" ' +
      '(work in progress — optionally pass instanceId/ipv4 as they become known) or "failed" ' +
      '(pass error with the reason). NOTE: "succeeded" is NOT accepted here — Center rejects it; ' +
      'the terminal succeeded status is set by the node itself when it self-registers using the ' +
      'baked-in claim token.',
    group: 'cloud.write',
    method: 'POST',
    params: {
      id: z.number().describe('Provision job ID to report on'),
      status: z
        .enum(['provisioning', 'failed'])
        .describe('Report status. "succeeded" is set by node self-registration, not here.'),
      instanceId: z.string().optional().describe('Cloud instance ID, once known'),
      ipv4: z.string().optional().describe('Provisioned IPv4 address, once known'),
      error: z.string().optional().describe('Failure reason (when status="failed")'),
    },
    path: (p) => `/app/provision-jobs/${p.id}/report`,
    mapBody: (p) => ({
      status: p.status,
      instanceId: p.instanceId,
      ipv4: p.ipv4,
      error: p.error,
    }),
  }),
]
