import { describe, it, expect } from 'vitest'
import { extractTar, extractZip } from './download-device-log.js'
import { deflateRawSync } from 'node:zlib'

// ============================================================================
// Tar extraction tests
// ============================================================================

/** Build a minimal POSIX tar archive from file entries (matches Rust `tar` crate output). */
function buildTar(files: Array<{ name: string; content: string }>): Buffer {
  const BLOCK = 512
  const blocks: Buffer[] = []

  for (const file of files) {
    const contentBuf = Buffer.from(file.content, 'utf-8')
    const header = Buffer.alloc(BLOCK)

    // name (offset 0, 100 bytes)
    header.write(file.name, 0, Math.min(file.name.length, 99), 'utf-8')

    // mode (offset 100, 8 bytes)
    header.write('0000644\0', 100, 8, 'ascii')

    // uid (offset 108, 8 bytes)
    header.write('0000000\0', 108, 8, 'ascii')

    // gid (offset 116, 8 bytes)
    header.write('0000000\0', 116, 8, 'ascii')

    // size (offset 124, 12 bytes) — octal null-terminated
    const sizeOctal = contentBuf.length.toString(8).padStart(11, '0') + '\0'
    header.write(sizeOctal, 124, 12, 'ascii')

    // mtime (offset 136, 12 bytes)
    const mtime = Math.floor(Date.now() / 1000).toString(8).padStart(11, '0') + '\0'
    header.write(mtime, 136, 12, 'ascii')

    // typeflag (offset 156) — '0' for regular file
    header[156] = 0x30

    // magic (offset 257, 6 bytes) — "ustar\0"
    header.write('ustar\0', 257, 6, 'ascii')

    // version (offset 263, 2 bytes)
    header.write('00', 263, 2, 'ascii')

    // checksum (offset 148, 8 bytes) — spaces first, then compute
    header.fill(0x20, 148, 156)
    const checksum = header.reduce((sum, b) => sum + b, 0)
    const chkStr = checksum.toString(8).padStart(6, '0') + '\0 '
    header.write(chkStr.slice(0, 8), 148, 8, 'ascii')

    blocks.push(header)
    blocks.push(contentBuf)

    // Pad content to BLOCK boundary
    const remainder = contentBuf.length % BLOCK
    if (remainder > 0) {
      blocks.push(Buffer.alloc(BLOCK - remainder))
    }
  }

  // Two zero blocks at end
  blocks.push(Buffer.alloc(BLOCK * 2))

  return Buffer.concat(blocks)
}

describe('extractTar', () => {
  it('extracts single file from tar', () => {
    const tar = buildTar([{ name: 'k2.log', content: 'hello world' }])
    const entries = extractTar(tar)

    expect(entries).toHaveLength(1)
    expect(entries[0].name).toBe('k2.log')
    expect(entries[0].data.toString('utf-8')).toBe('hello world')
  })

  it('extracts multiple files from tar', () => {
    const tar = buildTar([
      { name: 'system--k2.log', content: 'service log content' },
      { name: 'user--panic-20260309.log', content: 'panic trace here' },
      { name: 'desktop.log', content: 'desktop log content' },
    ])
    const entries = extractTar(tar)

    expect(entries).toHaveLength(3)
    expect(entries.map(e => e.name).sort()).toEqual([
      'desktop.log',
      'system--k2.log',
      'user--panic-20260309.log',
    ])
    expect(entries.find(e => e.name === 'system--k2.log')!.data.toString()).toBe('service log content')
  })

  it('handles empty tar (no files)', () => {
    const tar = Buffer.alloc(1024) // two zero blocks
    const entries = extractTar(tar)
    expect(entries).toHaveLength(0)
  })

  it('strips directory traversal from filenames', () => {
    const tar = buildTar([{ name: '../../etc/passwd', content: 'malicious' }])
    const entries = extractTar(tar)

    expect(entries).toHaveLength(1)
    expect(entries[0].name).toBe('passwd') // basename only
  })

  it('handles large file content crossing block boundaries', () => {
    const largeContent = 'A'.repeat(1500) // crosses 512-byte blocks
    const tar = buildTar([{ name: 'large.log', content: largeContent }])
    const entries = extractTar(tar)

    expect(entries).toHaveLength(1)
    expect(entries[0].data.toString()).toBe(largeContent)
    expect(entries[0].data.length).toBe(1500)
  })
})

// ============================================================================
// Zip extraction tests
// ============================================================================

/** Build a minimal ZIP archive with stored (uncompressed) entries. */
function buildZipStored(files: Array<{ name: string; content: string }>): Buffer {
  const parts: Buffer[] = []
  const centralDir: Buffer[] = []
  let offset = 0

  for (const file of files) {
    const contentBuf = Buffer.from(file.content, 'utf-8')
    const nameBuf = Buffer.from(file.name, 'utf-8')

    // Local file header (30 + nameLen + 0 extra + content)
    const header = Buffer.alloc(30)
    header.writeUInt32LE(0x04034b50, 0)  // signature
    header.writeUInt16LE(20, 4)           // version needed
    header.writeUInt16LE(0, 6)            // flags
    header.writeUInt16LE(0, 8)            // compression: stored
    header.writeUInt16LE(0, 10)           // mod time
    header.writeUInt16LE(0, 12)           // mod date
    header.writeUInt32LE(0, 14)           // crc32 (not checked by our extractor)
    header.writeUInt32LE(contentBuf.length, 18)  // compressed size
    header.writeUInt32LE(contentBuf.length, 22)  // uncompressed size
    header.writeUInt16LE(nameBuf.length, 26)     // name length
    header.writeUInt16LE(0, 28)           // extra length

    parts.push(header, nameBuf, contentBuf)

    // Central directory entry
    const cdEntry = Buffer.alloc(46)
    cdEntry.writeUInt32LE(0x02014b50, 0) // central dir signature
    cdEntry.writeUInt16LE(20, 4)          // version made by
    cdEntry.writeUInt16LE(20, 6)          // version needed
    cdEntry.writeUInt16LE(0, 8)           // flags
    cdEntry.writeUInt16LE(0, 10)          // compression: stored
    cdEntry.writeUInt32LE(contentBuf.length, 20) // compressed size
    cdEntry.writeUInt32LE(contentBuf.length, 24) // uncompressed size
    cdEntry.writeUInt16LE(nameBuf.length, 28)    // name length
    cdEntry.writeUInt32LE(offset, 42)    // local header offset
    centralDir.push(cdEntry, nameBuf)

    offset += 30 + nameBuf.length + contentBuf.length
  }

  // End of central directory
  const eocd = Buffer.alloc(22)
  eocd.writeUInt32LE(0x06054b50, 0)
  eocd.writeUInt16LE(files.length, 8)   // total entries
  eocd.writeUInt16LE(files.length, 10)  // total entries
  const cdSize = centralDir.reduce((s, b) => s + b.length, 0)
  eocd.writeUInt32LE(cdSize, 12)
  eocd.writeUInt32LE(offset, 16)

  return Buffer.concat([...parts, ...centralDir, eocd])
}

/** Build a ZIP with deflate-compressed entries. */
function buildZipDeflated(files: Array<{ name: string; content: string }>): Buffer {
  const parts: Buffer[] = []
  let offset = 0

  for (const file of files) {
    const contentBuf = Buffer.from(file.content, 'utf-8')
    const nameBuf = Buffer.from(file.name, 'utf-8')
    const compressed = deflateRawSync(contentBuf)

    const header = Buffer.alloc(30)
    header.writeUInt32LE(0x04034b50, 0)
    header.writeUInt16LE(20, 4)
    header.writeUInt16LE(0, 6)
    header.writeUInt16LE(8, 8)            // compression: deflate
    header.writeUInt32LE(compressed.length, 18)   // compressed size
    header.writeUInt32LE(contentBuf.length, 22)   // uncompressed size
    header.writeUInt16LE(nameBuf.length, 26)
    header.writeUInt16LE(0, 28)

    parts.push(header, nameBuf, compressed)
    offset += 30 + nameBuf.length + compressed.length
  }

  // Minimal EOCD to terminate
  const eocd = Buffer.alloc(22)
  eocd.writeUInt32LE(0x06054b50, 0)
  parts.push(eocd)

  return Buffer.concat(parts)
}

describe('extractZip', () => {
  it('extracts stored (uncompressed) files from zip', () => {
    const zip = buildZipStored([
      { name: 'k2.log', content: 'go engine log' },
      { name: 'native.log', content: 'native layer log' },
    ])
    const entries = extractZip(zip)

    expect(entries).toHaveLength(2)
    expect(entries[0].name).toBe('k2.log')
    expect(entries[0].data.toString()).toBe('go engine log')
    expect(entries[1].name).toBe('native.log')
    expect(entries[1].data.toString()).toBe('native layer log')
  })

  it('extracts deflate-compressed files from zip', () => {
    const zip = buildZipDeflated([
      { name: 'webapp.log', content: 'console log entries here' },
    ])
    const entries = extractZip(zip)

    expect(entries).toHaveLength(1)
    expect(entries[0].name).toBe('webapp.log')
    expect(entries[0].data.toString()).toBe('console log entries here')
  })

  it('strips directory traversal from filenames', () => {
    const zip = buildZipStored([{ name: '../../../tmp/evil.txt', content: 'bad' }])
    const entries = extractZip(zip)

    expect(entries).toHaveLength(1)
    expect(entries[0].name).toBe('evil.txt') // basename only
  })

  it('handles empty zip', () => {
    // Just an EOCD record
    const eocd = Buffer.alloc(22)
    eocd.writeUInt32LE(0x06054b50, 0)
    const entries = extractZip(eocd)
    expect(entries).toHaveLength(0)
  })
})
