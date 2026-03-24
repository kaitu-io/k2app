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

/**
 * Extract files from a ZIP buffer using Node.js built-in zlib for deflate.
 *
 * Reads from the Central Directory (always has correct sizes) rather than
 * local file headers, because:
 * - Android ZipOutputStream sets Data Descriptor flag (bit 3) → sizes=0 in local headers
 * - iOS SSZipArchive uses ZIP64 extensions → sizes=0xFFFFFFFF in standard fields,
 *   real sizes in ZIP64 extra field (header ID 0x0001)
 */
export function extractZip(zipBuffer: Buffer): Array<{ name: string; data: Buffer }> {
  const entries: Array<{ name: string; data: Buffer }> = []

  // 1. Find End of Central Directory record (scan backwards)
  //    EOCD signature = 0x06054b50, minimum EOCD size = 22 bytes
  let eocdOffset = -1
  for (let i = zipBuffer.length - 22; i >= 0; i--) {
    if (zipBuffer.readUInt32LE(i) === 0x06054b50) {
      eocdOffset = i
      break
    }
  }
  if (eocdOffset === -1) return entries

  // 2. Read Central Directory location from EOCD
  const cdOffset = zipBuffer.readUInt32LE(eocdOffset + 16)
  const cdSize = zipBuffer.readUInt32LE(eocdOffset + 12)

  // 3. Walk Central Directory entries (signature = 0x02014b50)
  let pos = cdOffset
  const cdEnd = cdOffset + cdSize
  while (pos + 46 <= cdEnd && pos + 46 <= zipBuffer.length) {
    if (zipBuffer.readUInt32LE(pos) !== 0x02014b50) break

    const compressionMethod = zipBuffer.readUInt16LE(pos + 10)
    let compressedSize = zipBuffer.readUInt32LE(pos + 20)
    let uncompressedSize = zipBuffer.readUInt32LE(pos + 24)
    const nameLen = zipBuffer.readUInt16LE(pos + 28)
    const extraLen = zipBuffer.readUInt16LE(pos + 30)
    const commentLen = zipBuffer.readUInt16LE(pos + 32)
    const localHeaderOffset = zipBuffer.readUInt32LE(pos + 42)

    const rawName = zipBuffer.subarray(pos + 46, pos + 46 + nameLen).toString('utf-8')
    const name = basename(rawName)

    // Parse ZIP64 extra field from Central Directory if sizes are 0xFFFFFFFF
    if (compressedSize === 0xFFFFFFFF || uncompressedSize === 0xFFFFFFFF) {
      const extraStart = pos + 46 + nameLen
      const extraEnd = extraStart + extraLen
      let ePos = extraStart
      while (ePos + 4 <= extraEnd) {
        const headerId = zipBuffer.readUInt16LE(ePos)
        const dataSize = zipBuffer.readUInt16LE(ePos + 2)
        if (headerId === 0x0001 && dataSize >= 16 && ePos + 4 + 16 <= zipBuffer.length) {
          // ZIP64 extended information: uncompressedSize (8 bytes) + compressedSize (8 bytes)
          // Safe to use Number() — log files won't exceed Number.MAX_SAFE_INTEGER (9 PB)
          uncompressedSize = Number(zipBuffer.readBigUInt64LE(ePos + 4))
          compressedSize = Number(zipBuffer.readBigUInt64LE(ePos + 12))
          break
        }
        ePos += 4 + dataSize
      }
    }

    // Advance past this central directory entry
    pos += 46 + nameLen + extraLen + commentLen

    if (!name || uncompressedSize === 0) continue

    // 4. Locate compressed data via the local file header
    //    Local header has its own nameLen/extraLen (may differ from CD)
    const localNameLen = zipBuffer.readUInt16LE(localHeaderOffset + 26)
    const localExtraLen = zipBuffer.readUInt16LE(localHeaderOffset + 28)
    const dataStart = localHeaderOffset + 30 + localNameLen + localExtraLen
    const compressedData = zipBuffer.subarray(dataStart, dataStart + compressedSize)

    if (compressionMethod === 0) {
      entries.push({ name, data: Buffer.from(compressedData) })
    } else if (compressionMethod === 8) {
      entries.push({ name, data: inflateRawSync(compressedData) as Buffer })
    }
    // Unknown compression methods silently skipped
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
