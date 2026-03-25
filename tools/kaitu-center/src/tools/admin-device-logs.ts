/**
 * Device log query tools (factory declarations).
 *
 * Migrated from query-device-logs.ts to declarative defineApiTool format.
 */

import { z } from 'zod'
import { defineApiTool, type ToolRegistration } from '../tool-factory.js'

export const deviceLogTools: ToolRegistration[] = [
  defineApiTool({
    name: 'query_device_logs',
    description:
      'Query device log upload records. Filter by UDID, user ID, feedback ID, reason, or time range. Returns metadata + S3 keys for downloading logs.',
    group: 'device_logs',
    path: '/app/device-logs',
    params: {
      udid: z.string().optional().describe('Filter by device UDID'),
      user_id: z.string().optional().describe('Filter by user ID'),
      feedback_id: z.string().optional().describe('Filter by feedback ID (links logs to a ticket)'),
      reason: z.string().optional().describe('Filter by upload reason (e.g. "user_feedback_report", "beta-auto-upload")'),
      from: z.string().optional().describe('Start time (RFC3339, e.g. "2026-03-01T00:00:00Z")'),
      to: z.string().optional().describe('End time (RFC3339)'),
      page: z.number().optional().describe('Page number (default 1)'),
      page_size: z.number().optional().describe('Page size (default 20, max 100)'),
    },
    mapQuery: (p) => {
      const q: Record<string, string> = {}
      if (p.udid) q.udid = String(p.udid)
      if (p.user_id) q.user_id = String(p.user_id)
      if (p.feedback_id) q.feedback_id = String(p.feedback_id)
      if (p.reason) q.reason = String(p.reason)
      if (p.from) q.from = String(p.from)
      if (p.to) q.to = String(p.to)
      if (p.page) q.page = String(p.page)
      if (p.page_size) q.pageSize = String(p.page_size)
      return q
    },
  }),
]
