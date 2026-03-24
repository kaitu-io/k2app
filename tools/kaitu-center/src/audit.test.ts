/**
 * Tests for the audit logging module.
 *
 * Verifies log format, file creation, rotation, and silent failure handling.
 * Uses vi.mock for node:fs since ESM module properties are non-configurable.
 */

import { describe, it, expect, vi, beforeEach } from 'vitest'

// ---------------------------------------------------------------------------
// Mock node:fs at module level (ESM properties cannot be spied on directly)
// ---------------------------------------------------------------------------

const mockMkdirSync = vi.fn()
const mockStatSync = vi.fn()
const mockTruncateSync = vi.fn()
const mockAppendFileSync = vi.fn()

vi.mock('node:fs', async (importOriginal) => {
  const actual = await importOriginal<typeof import('node:fs')>()
  return {
    ...actual,
    mkdirSync: (...args: unknown[]) => mockMkdirSync(...args),
    statSync: (...args: unknown[]) => mockStatSync(...args),
    truncateSync: (...args: unknown[]) => mockTruncateSync(...args),
    appendFileSync: (...args: unknown[]) => mockAppendFileSync(...args),
  }
})

import { audit } from './audit.js'

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

describe('audit', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    // Default: file doesn't exist yet
    mockStatSync.mockImplementation(() => {
      throw new Error('ENOENT')
    })
  })

  it('does not throw on any input', async () => {
    await expect(audit('test_tool', { ip: '1.2.3.4', status: 'success' })).resolves.toBeUndefined()
  })

  it('does not throw with empty fields', async () => {
    await expect(audit('test_tool', {})).resolves.toBeUndefined()
  })

  it('does not throw with various value types', async () => {
    await expect(
      audit('test_tool', {
        ip: '1.2.3.4',
        exitCode: 0,
        truncated: false,
        count: 42,
      })
    ).resolves.toBeUndefined()
  })
})

describe('audit log format', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    // Default: file doesn't exist yet
    mockStatSync.mockImplementation(() => {
      throw new Error('ENOENT')
    })
  })

  it('writes log line with ISO timestamp and tool name', async () => {
    await audit('exec_on_node', { ip: '1.2.3.4', status: 'success' })

    expect(mockAppendFileSync).toHaveBeenCalledOnce()
    const line = mockAppendFileSync.mock.calls[0]![1] as string
    // Format: [ISO timestamp] [tool_name] key=value ...
    expect(line).toMatch(/^\[.*\] \[exec_on_node\] /)
    expect(line).toContain('ip="1.2.3.4"')
    expect(line).toContain('status="success"')
    expect(line.endsWith('\n')).toBe(true)
  })

  it('quotes string values and leaves numbers unquoted', async () => {
    await audit('ping_node', { ip: '1.2.3.4', latencyMs: 45, reachable: true })

    const line = mockAppendFileSync.mock.calls[0]![1] as string
    expect(line).toContain('ip="1.2.3.4"')
    expect(line).toContain('latencyMs=45')
    expect(line).toContain('reachable=true')
  })

  it('creates log directory on write', async () => {
    await audit('test_tool', { key: 'val' })

    expect(mockMkdirSync).toHaveBeenCalledOnce()
    const dirPath = mockMkdirSync.mock.calls[0]![0] as string
    expect(dirPath).toContain('.kaitu-ops')
  })

  it('triggers rotation when file exceeds 500KB', async () => {
    mockStatSync.mockReturnValue({ size: 600 * 1024 })

    await audit('test_tool', { key: 'val' })

    expect(mockTruncateSync).toHaveBeenCalledOnce()
  })

  it('does not truncate when file is under limit', async () => {
    mockStatSync.mockReturnValue({ size: 100 * 1024 })

    await audit('test_tool', { key: 'val' })

    expect(mockTruncateSync).not.toHaveBeenCalled()
  })
})
