/**
 * Retailer management tools (factory declarations).
 *
 * Migrated from list-retailers.ts, get-retailer-detail.ts,
 * update-retailer-level.ts, create-retailer-note.ts, list-retailer-todos.ts.
 */

import { z } from 'zod'
import { defineApiTool, type ToolRegistration } from '../tool-factory.js'

export const retailerTools: ToolRegistration[] = [
  defineApiTool({
    name: 'list_retailers',
    description: 'List retailers with optional pagination.',
    group: 'retailers',
    path: '/app/retailers',
    params: {
      page: z.number().optional().describe('Page number (default 1)'),
      page_size: z.number().optional().describe('Page size (default 20)'),
    },
    mapQuery: (p) => {
      const q: Record<string, string> = {}
      if (p.page) q.page = String(p.page)
      if (p.page_size) q.pageSize = String(p.page_size)
      return q
    },
  }),

  defineApiTool({
    name: 'get_retailer_detail',
    description: 'Get detailed retailer profile including commission rates and performance metrics.',
    group: 'retailers',
    path: (p) => `/app/retailers/${p.uuid}`,
    params: {
      uuid: z.string().describe('Retailer UUID'),
    },
  }),

  defineApiTool({
    name: 'update_retailer_level',
    description: 'Update a retailer commission level. Always document the reason via create_retailer_note first.',
    group: 'retailers.write',
    method: 'PUT',
    path: (p) => `/app/retailers/${p.uuid}/level`,
    params: {
      uuid: z.string().describe('Retailer UUID'),
      level: z.string().describe('New level (e.g. L1, L2, L3, L4)'),
    },
  }),

  defineApiTool({
    name: 'create_retailer_note',
    description: 'Add a follow-up note to a retailer profile. Use before level changes to document reasoning.',
    group: 'retailers.write',
    method: 'POST',
    path: (p) => `/app/retailers/${p.uuid}/notes`,
    params: {
      uuid: z.string().describe('Retailer UUID'),
      content: z.string().describe('Note content'),
    },
  }),

  defineApiTool({
    name: 'list_retailer_todos',
    description: 'List pending retailer follow-up items that need action.',
    group: 'retailers',
    path: '/app/retailers/todos',
  }),
]
