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
    description: 'List email marketing templates. Returns id, name, slug, subject, language, created_at.',
    group: 'edm',
    path: '/app/edm/templates',
  }),

  defineApiTool({
    name: 'create_edm_template',
    description: 'Create a new email marketing template. Content supports Go template variables like {{.VarName}}.',
    group: 'edm',
    method: 'POST',
    path: '/app/edm/templates',
    params: {
      name: z.string().describe('Template display name'),
      slug: z.string().optional().describe('Unique readable identifier (e.g. "renewal-30d")'),
      language: z.string().describe('BCP 47 language tag (e.g. "zh-CN", "en-US")'),
      subject: z.string().describe('Email subject line (supports {{.Var}} placeholders)'),
      content: z.string().describe('Email body content (supports {{.Var}} placeholders)'),
      description: z.string().optional().describe('Template description'),
      is_active: z.boolean().optional().describe('Whether template is active (default true)'),
    },
    mapBody: (p) => ({
      name: p.name,
      slug: p.slug,
      language: p.language,
      subject: p.subject,
      content: p.content,
      description: p.description,
      isActive: p.is_active ?? true,
    }),
  }),

  defineApiTool({
    name: 'update_edm_template',
    description: 'Update an existing email marketing template by ID.',
    group: 'edm',
    method: 'PUT',
    path: (p) => `/app/edm/templates/${p.template_id}`,
    params: {
      template_id: z.number().describe('Template ID to update'),
      name: z.string().describe('Template display name'),
      slug: z.string().optional().describe('Unique readable identifier'),
      language: z.string().describe('BCP 47 language tag'),
      subject: z.string().describe('Email subject line'),
      content: z.string().describe('Email body content'),
      description: z.string().optional().describe('Template description'),
      is_active: z.boolean().optional().describe('Whether template is active'),
    },
    mapBody: (p) => ({
      name: p.name,
      slug: p.slug,
      language: p.language,
      subject: p.subject,
      content: p.content,
      description: p.description,
      isActive: p.is_active ?? true,
    }),
  }),

  defineApiTool({
    name: 'send_templated_email',
    description: 'Send templated emails to one or more recipients. Each item specifies an email address, a template slug (e.g. "renewal-30d", "winback-7d"), and template variables. Use list_edm_templates to find available slugs.',
    group: 'edm',
    method: 'POST',
    path: '/app/edm/send',
    params: {
      batch_id: z.string().describe('Unique batch ID for idempotency (e.g. "mcp:2026-04-03:test")'),
      items: z.array(z.object({
        email: z.string().describe('Recipient email address'),
        user_id: z.number().optional().describe('User ID (optional, auto-resolved from email if omitted)'),
        slug: z.string().describe('Template slug (e.g. "renewal-30d")'),
        vars: z.record(z.string(), z.string()).optional().describe('Template variables as key-value pairs'),
      })).describe('Array of email send items'),
    },
    mapBody: (p) => ({
      batchId: p.batch_id,
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
