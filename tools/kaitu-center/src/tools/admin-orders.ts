/**
 * Admin order management tools.
 */

import { z } from 'zod'
import { defineApiTool, type ToolRegistration } from '../tool-factory.js'
import { brandFilter, withBrand } from './brand-params.js'

/**
 * notice_at: the moment the user told us they were withdrawing (their email's time).
 * Accepts ISO 8601 (with offset) or unix seconds; the Center API takes unix seconds.
 */
export function toUnixSeconds(v: string): number {
  if (/^\d+$/.test(v)) return Number(v)
  const ms = Date.parse(v)
  if (Number.isNaN(ms)) throw new Error(`notice_at must be ISO 8601 or unix seconds, got: ${v}`)
  return Math.floor(ms / 1000)
}

const noticeAt = z
  .string()
  .describe("When the user told us they're withdrawing — the time of their email/message (ISO 8601 with offset, or unix seconds). NOT the time you run this.")

export const orderTools: ToolRegistration[] = [
  defineApiTool({
    name: 'list_orders',
    description: 'List orders with pagination. Filter by email and/or brand (an order\'s brand is its buyer\'s brand).',
    group: 'orders',
    params: {
      page: z.number().optional().describe('Page number'),
      page_size: z.number().optional().describe('Page size'),
      email: z.string().optional().describe('Filter by user email'),
      brand: brandFilter,
    },
    path: '/app/orders',
    mapQuery: (p) => {
      const q: Record<string, string> = {}
      if (p.page !== undefined) q.page = String(p.page)
      if (p.page_size !== undefined) q.pageSize = String(p.page_size)
      // The API filters by (loginProvider, loginIdentity); a bare `email` param is ignored.
      if (p.email !== undefined) {
        q.loginProvider = 'email'
        q.loginIdentity = String(p.email)
      }
      return withBrand(q, p.brand)
    },
  }),

  defineApiTool({
    name: 'get_order_detail',
    description: 'Get full details for a single order by UUID.',
    group: 'orders',
    params: {
      uuid: z.string().describe('Order UUID'),
    },
    path: (p) => `/app/orders/${p.uuid}`,
  }),

  defineApiTool({
    name: 'refund_order',
    description: 'Refund a paid order. Credits the user wallet, revokes granted Pro days, and reverses retailer cashback. Requires dual-admin approval (superadmin auto-executes). Full refunds only. Not available for brands without a wallet (overleap): use quote_stripe_withdrawal / withdraw_stripe_subscription for Stripe, Apple refunds are handled by Apple.',
    group: 'orders.write',
    method: 'POST',
    params: {
      uuid: z.string().describe('Order UUID'),
      reason: z.string().min(2).max(500).describe('Refund reason (2-500 chars, required)'),
    },
    path: (p) => `/app/orders/${p.uuid}/refund`,
    mapBody: (p) => ({ reason: p.reason }),
  }),

  defineApiTool({
    name: 'quote_stripe_withdrawal',
    description: "Preview an overleap 14-day withdrawal (Stripe): which payments are refunded and how much (prorated by days used, counting the payment day; payments charged after the notice are refunded in full). Read-only. Run this first and tell the user the amount before withdraw_stripe_subscription.",
    group: 'orders',
    params: {
      uuid: z.string().describe('User UUID'),
      notice_at: noticeAt,
      mode: z.enum(['withdrawal', 'termination']).optional().describe("withdrawal (default) = user's 14-day right; termination = we end the service (Terms 8.3), skips eligibility, notice_at must be within 48h"),
      subscription_id: z.string().optional().describe('Stripe subscription id (sub_...). Default: the user\'s latest non-revoked Stripe subscription'),
    },
    path: (p) => `/app/users/${p.uuid}/stripe-withdrawal`,
    mapQuery: (p) => {
      const q: Record<string, string> = { notice_at: String(toUnixSeconds(String(p.notice_at))) }
      if (p.mode !== undefined) q.mode = String(p.mode)
      if (p.subscription_id !== undefined) q.subscription_id = String(p.subscription_id)
      return q
    },
  }),

  defineApiTool({
    name: 'withdraw_stripe_subscription',
    description: "Execute an overleap 14-day withdrawal (Stripe): refund to the original card, end the membership, cancel the subscription. Never refund partially by hand in the Stripe Dashboard instead. Safe to re-run: pass request_id to resume a request that failed midway (it never refunds twice). Requires dual-admin approval (superadmin auto-executes).",
    group: 'orders.write',
    method: 'POST',
    params: {
      uuid: z.string().describe('User UUID'),
      notice_at: noticeAt,
      mode: z.enum(['withdrawal', 'termination']).describe('withdrawal = user request; termination = we end the service (Terms 8.3)'),
      reason: z.string().max(255).optional().describe('Internal note'),
      subscription_id: z.string().optional().describe('Stripe subscription id; default the latest non-revoked one'),
      request_id: z.string().optional().describe('Resume an unfinished request (wdr_...)'),
    },
    path: (p) => `/app/users/${p.uuid}/stripe-withdrawal`,
    mapBody: (p) => ({
      noticeAt: toUnixSeconds(String(p.notice_at)),
      mode: p.mode,
      reason: p.reason,
      subscriptionId: p.subscription_id,
      requestId: p.request_id,
    }),
  }),

  defineApiTool({
    name: 'abandon_stripe_withdrawal',
    description: 'Abandon a stuck withdrawal request so it can be redone or handled manually. Finds any refund already issued, and if money went out it still ends the membership and cancels the subscription. Returns what was completed.',
    group: 'orders.write',
    method: 'POST',
    params: {
      request_id: z.string().describe('Withdrawal request id (wdr_...)'),
      reason: z.string().min(2).max(255).describe('Why it is being abandoned'),
    },
    path: (p) => `/app/stripe-withdrawals/${p.request_id}/abandon`,
    mapBody: (p) => ({ reason: p.reason }),
  }),
]
