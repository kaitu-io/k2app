/**
 * Tests for the exec_on_node MCP tool.
 *
 * Uses vi.mock to replace sshExec/sshExecWithStdin so no real SSH is needed.
 * Focuses on: truncation, redaction, scriptPath file-reading, parameter passing,
 * structured error output, stderr handling, and audit logging.
 */

import { describe, it, expect, vi, beforeEach } from 'vitest'
import * as os from 'node:os'
import * as path from 'node:path'
import * as fs from 'node:fs'
import { McpServer } from '@modelcontextprotocol/sdk/server/mcp.js'
import type { SshConfig } from '../config.ts'

// ---------------------------------------------------------------------------
// Mock ssh module and audit module
// ---------------------------------------------------------------------------

vi.mock('../ssh.js', () => ({
  sshExec: vi.fn(),
  sshExecWithStdin: vi.fn(),
}))

vi.mock('../audit.js', () => ({
  audit: vi.fn().mockResolvedValue(undefined),
}))

import * as sshModule from '../ssh.js'

// Typed accessors so tests have proper MockInstance types
const mockSshExec = sshModule.sshExec as ReturnType<typeof vi.fn>
const mockSshExecWithStdin = sshModule.sshExecWithStdin as ReturnType<typeof vi.fn>

import { registerExecOnNode } from './exec-on-node.js'

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

const TEST_SSH_CONFIG: SshConfig = {
  privateKeyPath: '/home/user/.ssh/id_rsa',
  user: 'root',
  port: 22,
}

interface ExecResult {
  status: string
  stdout?: string
  stderr?: string
  exitCode: number
  truncated?: boolean
  stderrTruncated?: boolean
  error?: string
}

/**
 * Invoke the exec_on_node tool on an McpServer by directly calling the
 * registered handler.
 */
async function invokeExecOnNode(
  params: Record<string, unknown>
): Promise<{ content: Array<{ type: string; text: string }> }> {
  const server = new McpServer({ name: 'test', version: '0.0.1' })
  registerExecOnNode(server, TEST_SSH_CONFIG)

  const registeredTools = (server as unknown as {
    _registeredTools: Record<string, { handler: (args: Record<string, unknown>) => Promise<unknown> }>
  })._registeredTools

  const tool = registeredTools['exec_on_node']
  if (!tool) throw new Error('exec_on_node tool not registered')

  return tool.handler(params) as Promise<{ content: Array<{ type: string; text: string }> }>
}

function parseResult(result: { content: Array<{ type: string; text: string }> }): ExecResult {
  return JSON.parse(result.content[0]!.text) as ExecResult
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

describe('exec_on_node tool', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    mockSshExec.mockResolvedValue({ stdout: '', stderr: '', exitCode: 0 })
    mockSshExecWithStdin.mockResolvedValue({ stdout: '', stderr: '', exitCode: 0 })
  })

  it('returns status=success for normal command execution', async () => {
    mockSshExec.mockResolvedValue({ stdout: 'hello', stderr: '', exitCode: 0 })

    const result = await invokeExecOnNode({ ip: '1.2.3.4', command: 'echo hello' })
    const parsed = parseResult(result)

    expect(parsed.status).toBe('success')
    expect(parsed.stdout).toBe('hello')
    expect(parsed.exitCode).toBe(0)
    expect(parsed.truncated).toBe(false)
    expect(parsed.stderrTruncated).toBe(false)
  })

  it('returns status=ssh_error when SSH connection fails', async () => {
    mockSshExec.mockRejectedValue(new Error('Connection refused to 1.2.3.4:1022'))

    const result = await invokeExecOnNode({ ip: '1.2.3.4', command: 'echo test' })
    const parsed = parseResult(result)

    expect(parsed.status).toBe('ssh_error')
    expect(parsed.error).toBe('Connection refused to 1.2.3.4:1022')
    expect(parsed.exitCode).toBe(-1)
    // No stdout/stderr in ssh_error response
    expect(parsed.stdout).toBeUndefined()
    expect(parsed.stderr).toBeUndefined()
  })

  it('returns status=timeout when exitCode is -1', async () => {
    mockSshExec.mockResolvedValue({
      stdout: 'partial',
      stderr: 'Command timed out after 60000ms',
      exitCode: -1,
    })

    const result = await invokeExecOnNode({ ip: '1.2.3.4', command: 'sleep 999' })
    const parsed = parseResult(result)

    expect(parsed.status).toBe('timeout')
    expect(parsed.stdout).toBe('partial')
    expect(parsed.exitCode).toBe(-1)
  })

  it('truncates stdout at 10000 chars', async () => {
    const longOutput = 'x'.repeat(15000)
    mockSshExec.mockResolvedValue({ stdout: longOutput, stderr: '', exitCode: 0 })

    const result = await invokeExecOnNode({ ip: '1.2.3.4', command: 'echo test' })
    const parsed = parseResult(result)

    expect(parsed.truncated).toBe(true)
    expect(parsed.stdout!.length).toBe(10000)
  })

  it('does not truncate stdout under limit', async () => {
    const shortOutput = 'hello world'
    mockSshExec.mockResolvedValue({ stdout: shortOutput, stderr: '', exitCode: 0 })

    const result = await invokeExecOnNode({ ip: '1.2.3.4', command: 'echo hello' })
    const parsed = parseResult(result)

    expect(parsed.truncated).toBe(false)
    expect(parsed.stdout).toBe('hello world')
  })

  it('truncates stderr at 2000 chars', async () => {
    const longStderr = 'e'.repeat(3000)
    mockSshExec.mockResolvedValue({ stdout: '', stderr: longStderr, exitCode: 1 })

    const result = await invokeExecOnNode({ ip: '1.2.3.4', command: 'bad-cmd' })
    const parsed = parseResult(result)

    expect(parsed.stderrTruncated).toBe(true)
    expect(parsed.stderr!.length).toBe(2000)
  })

  it('redacts secrets in stdout', async () => {
    const rawOutput = 'config: K2_NODE_SECRET=supersecretvalue123 end'
    mockSshExec.mockResolvedValue({ stdout: rawOutput, stderr: '', exitCode: 0 })

    const result = await invokeExecOnNode({ ip: '1.2.3.4', command: 'cat config' })
    const parsed = parseResult(result)

    expect(parsed.stdout).not.toContain('supersecretvalue123')
    expect(parsed.stdout).toContain('K2_NODE_SECRET=[REDACTED]')
  })

  it('redacts secrets in stderr', async () => {
    const rawStderr = 'error: DB_PASSWORD=hunter2 failed'
    mockSshExec.mockResolvedValue({ stdout: '', stderr: rawStderr, exitCode: 1 })

    const result = await invokeExecOnNode({ ip: '1.2.3.4', command: 'bad-cmd' })
    const parsed = parseResult(result)

    expect(parsed.stderr).not.toContain('hunter2')
    expect(parsed.stderr).toContain('DB_PASSWORD=[REDACTED]')
  })

  it('pipes script via stdin when scriptPath provided', async () => {
    const tmpDir = fs.mkdtempSync(path.join(os.tmpdir(), 'exec-on-node-test-'))
    const scriptPath = path.join(tmpDir, 'test.sh')
    const scriptContent = '#!/bin/bash\necho from-script'
    fs.writeFileSync(scriptPath, scriptContent)

    mockSshExecWithStdin.mockResolvedValue({ stdout: 'from-script', stderr: '', exitCode: 0 })

    const result = await invokeExecOnNode({
      ip: '1.2.3.4',
      command: 'ignored-when-script-path-given',
      scriptPath,
    })

    expect(mockSshExecWithStdin).toHaveBeenCalledOnce()
    const [calledIp, calledConfig, calledCommand, calledStdin] =
      mockSshExecWithStdin.mock.calls[0]! as [string, SshConfig, string, string]

    expect(calledIp).toBe('1.2.3.4')
    expect(calledConfig).toEqual(TEST_SSH_CONFIG)
    expect(calledCommand).toBe('bash -s')
    expect(calledStdin).toBe(scriptContent)

    expect(mockSshExec).not.toHaveBeenCalled()

    const parsed = parseResult(result)
    expect(parsed.stdout).toBe('from-script')
  })

  it('passes timeout in milliseconds to ssh function', async () => {
    mockSshExec.mockResolvedValue({ stdout: 'ok', stderr: '', exitCode: 0 })

    await invokeExecOnNode({ ip: '1.2.3.4', command: 'cmd', timeout: 120 })

    const [, , , timeoutArg] = mockSshExec.mock.calls[0]! as [string, SshConfig, string, number]
    expect(timeoutArg).toBe(120000)
  })

  it('uses default timeout of 60s when not specified', async () => {
    mockSshExec.mockResolvedValue({ stdout: 'ok', stderr: '', exitCode: 0 })

    await invokeExecOnNode({ ip: '1.2.3.4', command: 'cmd' })

    const [, , , timeoutArg] = mockSshExec.mock.calls[0]! as [string, SshConfig, string, number]
    expect(timeoutArg).toBe(60000)
  })
})
