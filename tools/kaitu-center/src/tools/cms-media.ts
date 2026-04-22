/**
 * Payload CMS Media tools — list/update-alt/delete.
 * Upload uses cms-upload-media.ts (multipart/form-data standalone).
 */

import { z } from 'zod'
import { defineRestApiTool, type ToolRegistration } from '../tool-factory.js'

export const cmsMediaTools: ToolRegistration[] = [
  defineRestApiTool({
    name: 'list_media',
    description: 'List uploaded media files. Pass locale to retrieve translated alt text.',
    group: 'cms',
    path: '/payload/api/media',
    params: {
      locale: z.string().optional(),
      page: z.number().optional(),
      limit: z.number().optional(),
    },
  }),

  defineRestApiTool({
    name: 'update_media_alt',
    description: 'Update the alt text of a media file. Pass locale to write a translation.',
    group: 'cms',
    method: 'PATCH',
    path: (p) => `/payload/api/media/${encodeURIComponent(String(p.id))}`,
    params: {
      id: z.union([z.string(), z.number()]),
      locale: z.string().optional(),
      alt: z.string().describe('Accessibility alt text'),
    },
    mapQuery: (p) => {
      const q: Record<string, string> = {}
      if (p.locale) q.locale = String(p.locale)
      return q
    },
    mapBody: (p) => ({ alt: p.alt }),
  }),

  defineRestApiTool({
    name: 'delete_media',
    description: 'Delete a media file. The S3 object is cascaded by the storage-s3 plugin.',
    group: 'cms',
    method: 'DELETE',
    path: (p) => `/payload/api/media/${encodeURIComponent(String(p.id))}`,
    params: { id: z.union([z.string(), z.number()]) },
  }),
]
