/**
 * Payload CMS Categories tools — list/create/update/delete.
 * Hierarchical via self-referencing parent field.
 */

import { z } from 'zod'
import { defineRestApiTool, type ToolRegistration } from '../tool-factory.js'

export const cmsCategoriesTools: ToolRegistration[] = [
  defineRestApiTool({
    name: 'list_categories',
    description: 'List all categories. Pass locale for translated names.',
    group: 'cms',
    path: '/payload/api/categories',
    params: {
      locale: z.string().optional(),
      page: z.number().optional(),
      limit: z.number().optional(),
    },
  }),

  defineRestApiTool({
    name: 'create_category',
    description: 'Create a new category (zh-CN source). autoTranslate fans out name+description to 6 locales.',
    group: 'cms',
    method: 'POST',
    path: '/payload/api/categories?locale=zh-CN',
    params: {
      name: z.string(),
      slug: z.string().describe('URL-safe slug'),
      description: z.string().optional(),
      parent: z.union([z.string(), z.number()]).optional().describe('Parent category ID for hierarchy'),
    },
  }),

  defineRestApiTool({
    name: 'update_category',
    description: 'Partial update of a category. Pass locale to update a translation.',
    group: 'cms',
    method: 'PATCH',
    path: (p) => `/payload/api/categories/${encodeURIComponent(String(p.id))}`,
    params: {
      id: z.union([z.string(), z.number()]),
      locale: z.string().optional(),
      name: z.string().optional(),
      slug: z.string().optional(),
      description: z.string().optional(),
      parent: z.union([z.string(), z.number()]).optional(),
    },
    mapQuery: (p) => {
      const q: Record<string, string> = {}
      if (p.locale) q.locale = String(p.locale)
      return q
    },
    mapBody: (p) => {
      const body: Record<string, unknown> = {}
      for (const k of ['name', 'slug', 'description', 'parent']) {
        if ((p as Record<string, unknown>)[k] !== undefined) body[k] = (p as Record<string, unknown>)[k]
      }
      return body
    },
  }),

  defineRestApiTool({
    name: 'delete_category',
    description: 'Delete a category. Posts referencing it will keep the stale ID unless the field is nullable — verify before delete.',
    group: 'cms',
    method: 'DELETE',
    path: (p) => `/payload/api/categories/${encodeURIComponent(String(p.id))}`,
    params: { id: z.union([z.string(), z.number()]) },
  }),
]
