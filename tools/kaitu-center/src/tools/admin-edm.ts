/**
 * EDM (email marketing) tools (factory declarations).
 *
 * Migrated from list-edm-templates.ts, create-edm-task.ts,
 * preview-edm-targets.ts, get-edm-send-stats.ts.
 */

import { z } from 'zod'
import { defineApiTool, type ToolRegistration } from '../tool-factory.js'

export const edmTools: ToolRegistration[] = [
  defineApiTool({
    name: 'list_edm_templates',
    description: 'List email marketing templates. Returns id, name, subject, language, created_at.',
    group: 'edm',
    path: '/app/edm/templates',
  }),

  defineApiTool({
    name: 'send_templated_email',
    description: 'Send templated emails to one or more recipients. Each item specifies an email address, a template slug (e.g. "renewal-30d", "winback-7d"), and template variables. Use list_edm_templates to find available slugs.',
    group: 'edm',
    method: 'POST',
    path: '/app/edm/send',
    params: {
      batch_id: z.string().describe('Unique batch ID for idempotency (e.g. "mcp:2026-04-03:test")'),
      async: z.boolean().optional().describe('If true, queue for async processing. Default false (sync).'),
      items: z.array(z.object({
        email: z.string().describe('Recipient email address'),
        user_id: z.number().optional().describe('User ID (optional, auto-resolved from email if omitted)'),
        slug: z.string().describe('Template slug (e.g. "renewal-30d")'),
        vars: z.record(z.string(), z.string()).optional().describe('Template variables as key-value pairs'),
      })).describe('Array of email send items'),
    },
    mapBody: (p) => ({
      batchId: p.batch_id,
      async: p.async,
      items: (p.items as Array<{ email: string; user_id?: number; slug: string; vars?: Record<string, string> }>).map((item) => ({
        email: item.email,
        userId: item.user_id,
        slug: item.slug,
        vars: item.vars,
      })),
    }),
  }),

  defineApiTool({
    name: 'get_edm_send_stats',
    description: 'Get send statistics for EDM campaigns.',
    group: 'edm',
    path: '/app/edm/send-logs/stats',
  }),
]
