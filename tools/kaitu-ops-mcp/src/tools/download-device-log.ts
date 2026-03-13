/**
 * download_device_log MCP tool.
 *
 * Downloads a device log from S3 by its s3Key, decompresses/extracts,
 * saves to a local file, and returns the file path + summary.
 * This avoids flooding the LLM context with large log content.
 *
 * Supports three formats:
 * - Legacy: individual .log.gz files (single log file)
 * - Desktop: .tar.gz archives (multiple log files)
 * - Mobile: .zip archives (multiple log files)
 */

import { z } from 'zod'
import { gunzipSync, inflateRawSync } from 'node:zlib'
import { mkdirSync, writeFileSync } from 'node:fs'
import { join, basename } from 'node:path'
import { tmpdir } from 'node:os'
import type { McpServer } from '@modelcontextprotocol/sdk/server/mcp.js'
import { audit } from '../audit.js'

const S3_BUCKET_URL = 'https://kaitu-service-logs.s3.ap-northeast-1.amazonaws.com'

/** Extract files from a POSIX tar buffer. Returns array of {name, data} entries. */
export function extractTar(tarBuffer: Buffer): Array<{ name: string; data: Buffer }> {
  const entries: Array<{ name: string; data: Buffer }> = []
  let offset = 0
  const BLOCK = 512

  while (offset + BLOCK <= tarBuffer.length) {
    const header = tarBuffer.subarray(offset, offset + BLOCK)

    // Check for end-of-archive (two zero blocks)
    if (header.every(b => b === 0)) break

    // Read name (offset 0, 100 bytes, null-terminated)
    const nameEnd = header.indexOf(0, 0)
    const rawName = header.subarray(0, Math.min(nameEnd >= 0 ? nameEnd : 100, 100)).toString('utf-8')
    // Sanitize: strip directory traversal, use only the basename
    const name = basename(rawName)

    // Read size (offset 124, 12 bytes, octal, null-terminated)
    const sizeStr = header.subarray(124, 136).toString('utf-8').replace(/\0/g, '').trim()
    const size = parseInt(sizeStr, 8) || 0

    // Type flag (offset 156): '0' or '\0' = regular file
    const typeFlag = header[156]
    const isRegularFile = typeFlag === 0x30 || typeFlag === 0x00

    offset += BLOCK // move past header

    if (isRegularFile && size > 0 && name) {
      const data = tarBuffer.subarray(offset, offset + size)
      entries.push({ name, data: Buffer.from(data) })
    }

    // Advance past data blocks (padded to 512)
    offset += Math.ceil(size / BLOCK) * BLOCK
  }

  return entries
}

/** Extract files from a ZIP buffer using Node.js built-in zlib for deflate. */
export function extractZip(zipBuffer: Buffer): Array<{ name: string; data: Buffer }> {
  const entries: Array<{ name: string; data: Buffer }> = []
  let offset = 0

  while (offset + 30 <= zipBuffer.length) {
    // Local file header signature = 0x04034b50
    const sig = zipBuffer.readUInt32LE(offset)
    if (sig !== 0x04034b50) break

    const compressionMethod = zipBuffer.readUInt16LE(offset + 8)
    const compressedSize = zipBuffer.readUInt32LE(offset + 18)
    const uncompressedSize = zipBuffer.readUInt32LE(offset + 22)
    const nameLen = zipBuffer.readUInt16LE(offset + 26)
    const extraLen = zipBuffer.readUInt16LE(offset + 28)

    const rawName = zipBuffer.subarray(offset + 30, offset + 30 + nameLen).toString('utf-8')
    // Sanitize: strip directory traversal
    const name = basename(rawName)

    const dataStart = offset + 30 + nameLen + extraLen
    const compressedData = zipBuffer.subarray(dataStart, dataStart + compressedSize)

    if (name && uncompressedSize > 0) {
      let data: Buffer
      if (compressionMethod === 0) {
        // Stored (no compression)
        data = Buffer.from(compressedData)
      } else if (compressionMethod === 8) {
        // Deflate — use raw inflate (no zlib/gzip header)
        data = inflateRawSync(compressedData) as Buffer
      } else {
        // Unknown compression — skip
        offset = dataStart + compressedSize
        continue
      }
      entries.push({ name, data })
    }

    offset = dataStart + compressedSize
  }

  return entries
}

export function registerDownloadDeviceLog(server: McpServer): void {
  server.tool(
    'download_device_log',
    'Download a device log from S3, save locally, return file path + metadata. Use the Read tool to inspect content. Use s3Key from query_device_logs results.',
    {
      s3_key: z.string().describe('S3 object key from device log record (e.g. "feedback-logs/udid/2026/03/08/logs-143022-abc.tar.gz")'),
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
        const outDir = join(tmpdir(), 'kaitu-device-logs')
        mkdirSync(outDir, { recursive: true })

        const isTarGz = params.s3_key.endsWith('.tar.gz')
        const isZip = params.s3_key.endsWith('.zip')

        if (isTarGz || isZip) {
          // Archive format — extract to subdirectory
          let entries: Array<{ name: string; data: Buffer }>
          let decompressedSize: number

          if (isTarGz) {
            const decompressed = buffer[0] === 0x1f && buffer[1] === 0x8b
              ? gunzipSync(buffer)
              : buffer
            decompressedSize = decompressed.length
            entries = extractTar(decompressed)
          } else {
            decompressedSize = buffer.length
            entries = extractZip(buffer)
          }

          const archiveName = basename(params.s3_key)
            .replace(/\.tar\.gz$/, '')
            .replace(/\.zip$/, '')
          const extractDir = join(outDir, archiveName)
          mkdirSync(extractDir, { recursive: true })

          const files: Array<{ name: string; path: string; size: number; lines: number }> = []
          for (const entry of entries) {
            const text = entry.data.toString('utf-8')
            const filePath = join(extractDir, entry.name)
            writeFileSync(filePath, text, 'utf-8')
            files.push({
              name: entry.name,
              path: filePath,
              size: text.length,
              lines: text.split('\n').length,
            })
          }

          await audit('download_device_log', {
            s3_key: params.s3_key,
            compressedBytes: buffer.length,
            decompressedBytes: decompressedSize,
            fileCount: files.length,
            extractDir,
          })

          return {
            content: [{
              type: 'text' as const,
              text: JSON.stringify({
                extractDir,
                s3Key: params.s3_key,
                compressedBytes: buffer.length,
                decompressedBytes: decompressedSize,
                files,
              }),
            }],
          }
        } else {
          // Legacy format: single .log.gz file
          let text: string
          if (buffer[0] === 0x1f && buffer[1] === 0x8b) {
            text = gunzipSync(buffer).toString('utf-8')
          } else {
            text = buffer.toString('utf-8')
          }

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
