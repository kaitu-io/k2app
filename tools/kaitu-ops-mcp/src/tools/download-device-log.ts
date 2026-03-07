/**
 * download_device_log MCP tool.
 *
 * Downloads a device log from S3 by its s3Key, decompresses gzip,
 * and returns the text content (truncated to 50k chars for context window).
 */

import { z } from 'zod'
import { gunzipSync } from 'node:zlib'
import type { McpServer } from '@modelcontextprotocol/sdk/server/mcp.js'
import { audit } from '../audit.js'

const S3_BUCKET_URL = 'https://kaitu-service-logs.s3.ap-northeast-1.amazonaws.com'
const MAX_CHARS = 50_000

export function registerDownloadDeviceLog(server: McpServer): void {
  server.tool(
    'download_device_log',
    'Download and decompress a device log file from S3. Returns the log text content (truncated to 50k chars). Use s3Key from query_device_logs results.',
    {
      s3_key: z.string().describe('S3 object key from device log record (e.g. "feedback-logs/udid/2026/03/08/service-143022-abc.log.gz")'),
    },
    async (params) => {
      const url = `${S3_BUCKET_URL}/${params.s3_key}`

      try {
        const response = await fetch(url)
        if (!response.ok) {
          await audit('download_device_log', { s3_key: params.s3_key, status: response.status, error: 'fetch failed' })
          return {
            content: [{ type: 'text' as const, text: JSON.stringify({ error: `S3 fetch failed: HTTP ${response.status}`, s3_key: params.s3_key }) }],
          }
        }

        const buffer = Buffer.from(await response.arrayBuffer())
        let text: string

        // Try gzip decompress; fall back to raw text if not gzipped
        if (buffer[0] === 0x1f && buffer[1] === 0x8b) {
          text = gunzipSync(buffer).toString('utf-8')
        } else {
          text = buffer.toString('utf-8')
        }

        const truncated = text.length > MAX_CHARS
        const content = truncated ? text.slice(0, MAX_CHARS) : text

        await audit('download_device_log', {
          s3_key: params.s3_key,
          size: buffer.length,
          decompressed: text.length,
          truncated,
        })

        return {
          content: [{
            type: 'text' as const,
            text: truncated
              ? `${content}\n\n--- TRUNCATED (${text.length} chars total, showing first ${MAX_CHARS}) ---`
              : content,
          }],
        }
      } catch (err) {
        const errorMessage = err instanceof Error ? err.message : String(err)
        await audit('download_device_log', { s3_key: params.s3_key, error: errorMessage })
        return {
          content: [{ type: 'text' as const, text: JSON.stringify({ error: errorMessage, s3_key: params.s3_key }) }],
        }
      }
    }
  )
}
