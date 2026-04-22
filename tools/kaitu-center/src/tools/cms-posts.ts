/**
 * Payload CMS Posts tools — list/get/create/update/delete + publish/unpublish.
 * Targets Next.js Payload REST at /payload/api/posts via the 'cms' client.
 */

import { z } from 'zod'
import { defineRestApiTool, type ToolRegistration } from '../tool-factory.js'

// Flattens Payload's Where syntax: { status: { equals: 'published' } }
// → { 'where[status][equals]': 'published' }
function flattenWhere(prefix: string, obj: Record<string, unknown>, out: Record<string, string>) {
  for (const [key, value] of Object.entries(obj)) {
    if (value === undefined) continue
    const bracketKey = `${prefix}[${key}]`
    if (value !== null && typeof value === 'object' && !Array.isArray(value)) {
      flattenWhere(bracketKey, value as Record<string, unknown>, out)
    } else {
      out[bracketKey] = String(value)
    }
  }
}

export const cmsPostsTools: ToolRegistration[] = [
  defineRestApiTool({
    name: 'list_posts',
    description: 'List posts. Filter by locale, status, showOnKaitu, showOnOverleap. Supports page/limit/sort.',
    group: 'cms',
    path: '/payload/api/posts',
    params: {
      locale: z.string().optional().describe('Locale code: zh-CN, en-US, etc.'),
      status: z.enum(['draft', 'published']).optional(),
      show_on_kaitu: z.boolean().optional(),
      show_on_overleap: z.boolean().optional(),
      page: z.number().optional(),
      limit: z.number().optional(),
      sort: z.string().optional().describe('Sort field; prefix with - for descending (e.g. -publishedAt)'),
    },
    mapQuery: (p) => {
      const q: Record<string, string> = {}
      if (p.locale) q.locale = String(p.locale)
      if (p.page) q.page = String(p.page)
      if (p.limit) q.limit = String(p.limit)
      if (p.sort) q.sort = String(p.sort)
      const where: Record<string, unknown> = {}
      if (p.status) where.status = { equals: p.status }
      if (p.show_on_kaitu !== undefined) where.showOnKaitu = { equals: p.show_on_kaitu }
      if (p.show_on_overleap !== undefined) where.showOnOverleap = { equals: p.show_on_overleap }
      if (Object.keys(where).length > 0) flattenWhere('where', where, q)
      return q
    },
  }),

  defineRestApiTool({
    name: 'get_post',
    description: 'Get a single post by ID. Pass locale for translated version; draft=true for draft preview.',
    group: 'cms',
    path: (p) => `/payload/api/posts/${encodeURIComponent(String(p.id))}`,
    params: {
      id: z.union([z.string(), z.number()]).describe('Post ID'),
      locale: z.string().optional(),
      draft: z.boolean().optional(),
    },
    mapQuery: (p) => {
      const q: Record<string, string> = {}
      if (p.locale) q.locale = String(p.locale)
      if (p.draft) q.draft = 'true'
      return q
    },
  }),

  defineRestApiTool({
    name: 'create_post',
    description: 'Create a new post (zh-CN source locale). autoTranslate fans out to 6 non-zh locales afterwards.',
    group: 'cms',
    method: 'POST',
    path: '/payload/api/posts?locale=zh-CN',
    params: {
      title: z.string(),
      slug: z.string().describe('URL-safe: lowercase letters, digits, dashes'),
      excerpt: z.string().optional(),
      content: z.any().describe('Lexical rich-text editor state (SerializedEditorState)'),
      category: z.union([z.string(), z.number()]).optional().describe('Category ID'),
      tags: z.array(z.union([z.string(), z.number()])).optional().describe('Array of tag IDs'),
      coverImage: z.union([z.string(), z.number()]).optional().describe('Media ID'),
      status: z.enum(['draft', 'published']).optional().default('draft'),
      showOnKaitu: z.boolean().optional().default(true),
      showOnOverleap: z.boolean().optional().default(true),
    },
  }),

  defineRestApiTool({
    name: 'update_post',
    description: 'Partial update of a post. Pass locale to update translation; omit for zh-CN source.',
    group: 'cms',
    method: 'PATCH',
    path: (p) => `/payload/api/posts/${encodeURIComponent(String(p.id))}`,
    params: {
      id: z.union([z.string(), z.number()]),
      locale: z.string().optional(),
      title: z.string().optional(),
      slug: z.string().optional(),
      excerpt: z.string().optional(),
      content: z.any().optional(),
      category: z.union([z.string(), z.number()]).optional(),
      tags: z.array(z.union([z.string(), z.number()])).optional(),
      coverImage: z.union([z.string(), z.number()]).optional(),
      showOnKaitu: z.boolean().optional(),
      showOnOverleap: z.boolean().optional(),
    },
    mapQuery: (p) => {
      const q: Record<string, string> = {}
      if (p.locale) q.locale = String(p.locale)
      return q
    },
    mapBody: (p) => {
      const body: Record<string, unknown> = {}
      for (const k of ['title','slug','excerpt','content','category','tags','coverImage','showOnKaitu','showOnOverleap']) {
        if ((p as Record<string, unknown>)[k] !== undefined) body[k] = (p as Record<string, unknown>)[k]
      }
      return body
    },
  }),

  defineRestApiTool({
    name: 'delete_post',
    description: 'Delete a post permanently (all locales + versions).',
    group: 'cms',
    method: 'DELETE',
    path: (p) => `/payload/api/posts/${encodeURIComponent(String(p.id))}`,
    params: { id: z.union([z.string(), z.number()]) },
  }),

  defineRestApiTool({
    name: 'publish_post',
    description: 'Set status=published. publishedAt is auto-set by the setPublishedAt hook on first publish.',
    group: 'cms',
    method: 'PATCH',
    path: (p) => `/payload/api/posts/${encodeURIComponent(String(p.id))}`,
    params: { id: z.union([z.string(), z.number()]) },
    mapBody: () => ({ status: 'published' }),
  }),

  defineRestApiTool({
    name: 'unpublish_post',
    description: 'Revert a post to draft status. publishedAt is preserved; republishing will not overwrite it.',
    group: 'cms',
    method: 'PATCH',
    path: (p) => `/payload/api/posts/${encodeURIComponent(String(p.id))}`,
    params: { id: z.union([z.string(), z.number()]) },
    mapBody: () => ({ status: 'draft' }),
  }),
]
