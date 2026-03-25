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
    name: 'create_edm_task',
    description: 'Create an EDM email send task. ALWAYS preview targets first with preview_edm_targets before sending.',
    group: 'edm',
    method: 'POST',
    path: '/app/edm/tasks',
    params: {
      template_id: z.number().describe('Email template ID'),
      target_filter: z.record(z.string(), z.unknown()).describe('Target user filter criteria'),
    },
    mapBody: (p) => ({ templateId: p.template_id, targetFilter: p.target_filter }),
  }),

  defineApiTool({
    name: 'preview_edm_targets',
    description: 'Preview target user count and sample for an EDM campaign. Use before create_edm_task to verify audience.',
    group: 'edm',
    method: 'POST',
    path: '/app/edm/preview-targets',
    params: {
      target_filter: z.record(z.string(), z.unknown()).describe('Target user filter criteria'),
    },
    mapBody: (p) => ({ targetFilter: p.target_filter }),
  }),

  defineApiTool({
    name: 'get_edm_send_stats',
    description: 'Get send statistics for EDM campaigns.',
    group: 'edm',
    path: '/app/edm/send-logs/stats',
  }),
]
