/**
 * Payload CMS Tags tools — list/create/update/delete. Flat label collection.
 */

import { z } from 'zod'
import { defineRestApiTool, type ToolRegistration } from '../tool-factory.js'

export const cmsTagsTools: ToolRegistration[] = [
  defineRestApiTool({
    name: 'list_tags',
    description: 'List all tags. Pass locale for translated names.',
    group: 'cms',
    path: '/payload/api/tags',
    params: {
      locale: z.string().optional(),
      page: z.number().optional(),
      limit: z.number().optional(),
    },
  }),

  defineRestApiTool({
    name: 'create_tag',
    description: 'Create a new tag (zh-CN source). autoTranslate fans name out to 6 locales.',
    group: 'cms',
    method: 'POST',
    path: '/payload/api/tags?locale=zh-CN',
    params: {
      name: z.string(),
      slug: z.string(),
    },
  }),

  defineRestApiTool({
    name: 'update_tag',
    description: 'Partial update of a tag. Pass locale to update a translation.',
    group: 'cms',
    method: 'PATCH',
    path: (p) => `/payload/api/tags/${encodeURIComponent(String(p.id))}`,
    params: {
      id: z.union([z.string(), z.number()]),
      locale: z.string().optional(),
      name: z.string().optional(),
      slug: z.string().optional(),
    },
    mapQuery: (p) => {
      const q: Record<string, string> = {}
      if (p.locale) q.locale = String(p.locale)
      return q
    },
    mapBody: (p) => {
      const body: Record<string, unknown> = {}
      for (const k of ['name', 'slug']) {
        if ((p as Record<string, unknown>)[k] !== undefined) body[k] = (p as Record<string, unknown>)[k]
      }
      return body
    },
  }),

  defineRestApiTool({
    name: 'delete_tag',
    description: 'Delete a tag. Check for posts referencing it first via list_posts filters.',
    group: 'cms',
    method: 'DELETE',
    path: (p) => `/payload/api/tags/${encodeURIComponent(String(p.id))}`,
    params: { id: z.union([z.string(), z.number()]) },
  }),
]
