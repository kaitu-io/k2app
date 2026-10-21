/**
 * Admin wallet and withdrawal management tools.
 */

import { z } from 'zod'
import { defineApiTool, type ToolRegistration } from '../tool-factory.js'

export const ALIPAY_REDACTED = '[redacted — view payee in kaitu.io/manager/withdraws]'

interface WithdrawAccountView {
  accountType?: string
  accountId?: string
  realName?: string
}

/**
 * The admin withdraw list decrypts Alipay payee info (account + real name) for manual payout in the
 * web manager. Nobody pays out through MCP, so keep that PII out of the model context and transcripts.
 */
export function redactAlipayPayees(data: unknown): unknown {
  const items = (data as { items?: { account?: WithdrawAccountView }[] } | null)?.items
  if (!Array.isArray(items)) return data
  return {
    ...(data as object),
    items: items.map((item) => {
      if (item.account?.accountType !== 'alipay') return item
      const { realName: _realName, ...account } = item.account
      return { ...item, account: { ...account, accountId: ALIPAY_REDACTED } }
    }),
  }
}

export const walletTools: ToolRegistration[] = [
  defineApiTool({
    name: 'list_withdraws',
    description: 'List withdrawal requests with pagination. Filter by status.',
    group: 'wallet',
    params: {
      page: z.number().optional().describe('Page number'),
      page_size: z.number().optional().describe('Page size'),
      status: z.string().optional().describe('Filter by status (pending, approved, completed, rejected)'),
    },
    path: '/app/wallet/withdraws',
    mapQuery: (p) => {
      const q: Record<string, string> = {}
      if (p.page !== undefined) q.page = String(p.page)
      if (p.page_size !== undefined) q.pageSize = String(p.page_size)
      if (p.status !== undefined) q.status = String(p.status)
      return q
    },
    mapResponse: redactAlipayPayees,
  }),

  defineApiTool({
    name: 'approve_withdraw',
    description: 'Approve a pending withdrawal request.',
    group: 'wallet.write',
    method: 'POST',
    params: {
      id: z.number().describe('Withdrawal request ID'),
    },
    path: (p) => `/app/wallet/withdraws/${p.id}/approve`,
  }),

  defineApiTool({
    name: 'complete_withdraw',
    description: 'Mark an approved withdrawal as completed (funds sent).',
    group: 'wallet.write',
    method: 'POST',
    params: {
      id: z.number().describe('Withdrawal request ID'),
    },
    path: (p) => `/app/wallet/withdraws/${p.id}/complete`,
  }),
]
