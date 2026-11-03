/**
 * iOS push broadcast tools (spec docs/superpowers/specs/2026-11-03-ios-push-design.md §6.5).
 *
 * Marketing only: the API has no category parameter, so a broadcast can never bypass the
 * user's in-app marketing consent. Run with dry_run first to see how many users it reaches.
 * Shares the `edm` permission group (Marketing role).
 */

import { z } from 'zod'
import { defineApiTool, type ToolRegistration } from '../tool-factory.js'
import { brandRequired } from './brand-params.js'

/** Mirrors api pushRouteWhitelist (api/logic_push.go). */
export const PUSH_ROUTES = ['/purchase', '/account', '/pro-histories'] as const

export const pushTools: ToolRegistration[] = [
  defineApiTool({
    name: 'send_push',
    description:
      'Send a marketing iOS push to an explicit list of users of one brand. Only users who opted in to marketing push in the app AND have an active device token receive it. ' +
      'ALWAYS call with dry_run=true first: it returns {requested, matched (in brand), withToken, consented} and sends nothing. ' +
      'A real send goes through approval (super admins execute immediately). Re-sending the same batch_id never pushes a user twice. ' +
      'Copy rules: no brand name (iOS shows the app name), no prices/emails/order numbers.',
    group: 'edm',
    method: 'POST',
    path: '/app/push/send',
    params: {
      batch_id: z
        .string()
        .max(100)
        .regex(/^[A-Za-z0-9:_.-]+$/)
        .describe('Unique batch ID, also the idempotency key; letters, digits and : _ . - only (e.g. "push:2026-11-05:winback")'),
      brand: brandRequired,
      user_ids: z.array(z.number().int().positive()).min(1).max(5000).describe('Recipient user IDs, at most 5000 per batch — split bigger audiences into several batch_ids (users of another brand are skipped)'),
      copy: z
        .record(z.string(), z.object({ title: z.string().max(100), body: z.string().max(500) }))
        .describe('Copy per language, e.g. {"zh-CN": {...}, "en-US": {...}}. Users get their language, else the normalized one (zh-HK→zh-TW), else the brand default (kaitu zh-CN / overleap en-US), else the first key.'),
      route: z.enum(PUSH_ROUTES).optional().describe('In-app page opened on tap'),
      dry_run: z.boolean().describe('true = only count the audience'),
    },
    mapBody: (p) => ({
      batchId: p.batch_id,
      brand: p.brand,
      userIds: p.user_ids,
      copy: p.copy,
      route: p.route,
      dryRun: p.dry_run,
    }),
  }),

  defineApiTool({
    name: 'get_push_batch_stats',
    description: 'Delivery stats of a push batch from the send log: total / queued / sent / failed / noToken. Users skipped at send time (no consent, no token) are not in the log — use send_push dry_run for those counts.',
    group: 'edm',
    path: (p) => `/app/push/batches/${encodeURIComponent(String(p.batch_id))}`,
    params: {
      batch_id: z.string().describe('Batch ID used in send_push'),
    },
  }),
]
