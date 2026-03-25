/**
 * Feedback ticket tools (factory declarations).
 *
 * Migrated from query-feedback-tickets.ts, resolve-feedback-ticket.ts,
 * close-feedback-ticket.ts to declarative defineApiTool format.
 */

import { z } from 'zod'
import { defineApiTool, type ToolRegistration } from '../tool-factory.js'

export const feedbackTicketTools: ToolRegistration[] = [
  defineApiTool({
    name: 'query_feedback_tickets',
    description:
      'Query user feedback tickets. Filter by UDID, email, user ID, status, or time range. Each ticket may have associated device logs (use feedback_id to cross-reference).',
    group: 'feedback_tickets',
    path: '/app/feedback-tickets',
    params: {
      udid: z.string().optional().describe('Filter by device UDID'),
      email: z.string().optional().describe('Filter by user email (partial match)'),
      user_id: z.string().optional().describe('Filter by user ID'),
      status: z.enum(['open', 'resolved', 'closed']).optional().describe('Filter by ticket status'),
      from: z.string().optional().describe('Start time (RFC3339)'),
      to: z.string().optional().describe('End time (RFC3339)'),
      page: z.number().optional().describe('Page number (default 1)'),
      page_size: z.number().optional().describe('Page size (default 20, max 100)'),
    },
    mapQuery: (p) => {
      const q: Record<string, string> = {}
      if (p.udid) q.udid = String(p.udid)
      if (p.email) q.email = String(p.email)
      if (p.user_id) q.user_id = String(p.user_id)
      if (p.status) q.status = String(p.status)
      if (p.from) q.from = String(p.from)
      if (p.to) q.to = String(p.to)
      if (p.page) q.page = String(p.page)
      if (p.page_size) q.pageSize = String(p.page_size)
      return q
    },
  }),

  defineApiTool({
    name: 'resolve_feedback_ticket',
    description: 'Mark a feedback ticket as resolved. Use after troubleshooting is complete.',
    group: 'feedback_tickets.write',
    method: 'PUT',
    path: (p) => `/app/feedback-tickets/${p.id}/resolve`,
    params: {
      id: z.number().describe('Ticket ID (from query_feedback_tickets results)'),
      resolved_by: z.string().describe('Who resolved the ticket (e.g. "claude", "david")'),
    },
    mapBody: (p) => ({ resolvedBy: p.resolved_by }),
  }),

  defineApiTool({
    name: 'close_feedback_ticket',
    description: 'Close a feedback ticket. Use when the issue has been fully handled or is not actionable.',
    group: 'feedback_tickets.write',
    method: 'PUT',
    path: (p) => `/app/feedback-tickets/${p.id}/close`,
    params: {
      id: z.number().describe('Ticket ID (from query_feedback_tickets results)'),
    },
    mapBody: () => ({}),
  }),
]
