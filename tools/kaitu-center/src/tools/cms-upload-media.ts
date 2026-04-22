/**
 * upload_media — multipart/form-data upload to /payload/api/media.
 *
 * Standalone because the declarative factories only handle JSON bodies.
 * Reads the file from local disk, builds a FormData with file + optional
 * alt text, and POSTs via the cms client. Returns Payload's raw response
 * body verbatim (typically { doc: {...}, message }).
 */

import { z } from 'zod'
import * as fs from 'node:fs/promises'
import * as path from 'node:path'
import type { McpServer } from '@modelcontextprotocol/sdk/server/mcp.js'
import type { CenterApiClient } from '../center-api.js'
import { audit } from '../audit.js'

const MIME_BY_EXT: Record<string, string> = {
  '.png': 'image/png',
  '.jpg': 'image/jpeg',
  '.jpeg': 'image/jpeg',
  '.webp': 'image/webp',
  '.gif': 'image/gif',
  '.svg': 'image/svg+xml',
}

export function registerUploadMedia(server: McpServer, cms: CenterApiClient): void {
  server.tool(
    'upload_media',
    'Upload an image file to the CMS media library. Returns the media doc (id, url, filename). Supports image/* MIME types; alt text optional (translatable later via update_media_alt).',
    {
      file_path: z.string().describe('Absolute path to an image file on disk'),
      alt: z.string().optional().describe('Alt text for accessibility (zh-CN source)'),
    },
    async (params: { file_path: string; alt?: string }) => {
      try {
        const buf = await fs.readFile(params.file_path)
        const filename = path.basename(params.file_path)
        const ext = path.extname(filename).toLowerCase()
        const mime = MIME_BY_EXT[ext] ?? 'application/octet-stream'

        const form = new FormData()
        form.append('file', new Blob([buf], { type: mime }), filename)
        if (params.alt) {
          // Payload accepts non-file fields as a JSON blob keyed as _payload
          form.append('_payload', JSON.stringify({ alt: params.alt }))
        }

        // Empty Content-Type tells CenterApiClient to drop its default JSON
        // header so fetch auto-generates the multipart boundary.
        const body = await cms.request('/payload/api/media?locale=zh-CN', {
          method: 'POST',
          body: form,
          headers: { 'Content-Type': '' },
        })

        await audit('upload_media', { file_path: params.file_path, alt: params.alt })
        return {
          content: [{ type: 'text' as const, text: JSON.stringify(body, null, 2) }],
        }
      } catch (err) {
        const msg = err instanceof Error ? err.message : String(err)
        await audit('upload_media', { ...params, error: msg })
        return { content: [{ type: 'text' as const, text: JSON.stringify({ error: msg }) }] }
      }
    }
  )
}
