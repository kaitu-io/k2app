import { describe, it, expect } from 'vitest'
import { redactStdout } from './redact.js'

describe('redactStdout', () => {
  it('test_redact_node_secret — K2_NODE_SECRET=<value> is redacted', () => {
    const input = 'K2_NODE_SECRET=abc123def456'
    const result = redactStdout(input)
    expect(result).toBe('K2_NODE_SECRET=[REDACTED]')
  })

  it('test_redact_secret_equals — SECRET=<value> is redacted', () => {
    const input = 'SECRET=xyz789'
    const result = redactStdout(input)
    expect(result).toBe('SECRET=[REDACTED]')
  })

  it('test_redact_hex_string_64 — standalone 64-char hex string is redacted', () => {
    const hex64 = 'a'.repeat(32) + 'f'.repeat(32)
    const result = redactStdout(hex64)
    expect(result).toBe('[REDACTED]')
  })

  it('test_redact_preserves_normal — normal stdout output is unchanged', () => {
    const input = 'Server started on port 8080\nAll systems operational'
    const result = redactStdout(input)
    expect(result).toBe(input)
  })

  it('test_redact_multiline — redacts secrets across multiple lines', () => {
    const input = [
      'Starting service...',
      'K2_NODE_SECRET=supersecretvalue',
      'API_TOKEN=mytoken123',
      'Service ready',
    ].join('\n')
    const result = redactStdout(input)
    expect(result).toContain('K2_NODE_SECRET=[REDACTED]')
    expect(result).toContain('API_TOKEN=[REDACTED]')
    expect(result).toContain('Starting service...')
    expect(result).toContain('Service ready')
    expect(result).not.toContain('supersecretvalue')
    expect(result).not.toContain('mytoken123')
  })

  it('test_redact_mixed_content — redacts secrets but preserves surrounding text', () => {
    const input = 'Loaded config: DB_PASSWORD=hunter2 and DB_HOST=localhost'
    const result = redactStdout(input)
    expect(result).toContain('DB_PASSWORD=[REDACTED]')
    expect(result).toContain('DB_HOST=localhost')
    expect(result).not.toContain('hunter2')
  })

  it('KEY_NAME containing KEY is redacted', () => {
    const input = 'API_KEY=somekeyvalue'
    const result = redactStdout(input)
    expect(result).toBe('API_KEY=[REDACTED]')
  })

  it('KEY_NAME containing PASSWORD is redacted', () => {
    const input = 'DB_PASSWORD=s3cr3t'
    const result = redactStdout(input)
    expect(result).toBe('DB_PASSWORD=[REDACTED]')
  })

  it('KEY_NAME containing TOKEN is redacted', () => {
    const input = 'AUTH_TOKEN=bearer_abc'
    const result = redactStdout(input)
    expect(result).toBe('AUTH_TOKEN=[REDACTED]')
  })

  it('64-char hex string embedded in text is redacted', () => {
    const hex64 = '0'.repeat(64)
    const input = `hash=${hex64} done`
    const result = redactStdout(input)
    expect(result).not.toContain(hex64)
    expect(result).toContain('[REDACTED]')
    expect(result).toContain('hash=')
    expect(result).toContain('done')
  })

  it('shorter hex string (not 64 chars) is NOT redacted', () => {
    const hex32 = 'deadbeef'.repeat(4) // 32 chars — not 64
    const input = `id=${hex32}`
    const result = redactStdout(input)
    expect(result).toBe(input)
  })
})
