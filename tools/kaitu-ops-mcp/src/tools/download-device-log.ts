/**
 * download_device_log MCP tool.
 *
 * Downloads a device log from S3 by its s3Key, decompresses gzip,
 * saves to a local file, and returns the file path + summary.
 * This avoids flooding the LLM context with large log content.
 */

import { z } from 'zod'
import { gunzipSync } from 'node:zlib'
import { mkdirSync, writeFileSync } from 'node:fs'
import { join, basename } from 'node:path'
import { tmpdir } from 'node:os'
import type { McpServer } from '@modelcontextprotocol/sdk/server/mcp.js'
import { audit } from '../audit.js'

const S3_BUCKET_URL = 'https://kaitu-service-logs.s3.ap-northeast-1.amazonaws.com'

export function registerDownloadDeviceLog(server: McpServer): void {
  server.tool(
    'download_device_log',
    'Download a device log from S3, save locally, return file path + metadata. Use the Read tool to inspect content. Use s3Key from query_device_logs results.',
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

        // Save to local file
        const outDir = join(tmpdir(), 'kaitu-device-logs')
        mkdirSync(outDir, { recursive: true })
        const filename = basename(params.s3_key).replace(/\.gz$/, '')
        const filePath = join(outDir, filename)
        writeFileSync(filePath, text, 'utf-8')

        const totalLines = text.split('\n').length

        await audit('download_device_log', {
          s3_key: params.s3_key,
          size: buffer.length,
          decompressed: text.length,
          lines: totalLines,
          filePath,
        })

        return {
          content: [{
            type: 'text' as const,
            text: JSON.stringify({
              filePath,
              s3Key: params.s3_key,
              compressedBytes: buffer.length,
              decompressedBytes: text.length,
              lines: totalLines,
            }),
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
