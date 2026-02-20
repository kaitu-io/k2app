/**
 * Tests for the exec_on_node MCP tool.
 *
 * Uses vi.mock to replace sshExec/sshExecWithStdin so no real SSH is needed.
 * Focuses on: truncation, redaction, scriptPath file-reading, and parameter passing.
 */

import { describe, it, expect, vi, beforeEach } from 'vitest'
import * as os from 'node:os'
import * as path from 'node:path'
import * as fs from 'node:fs'
import { McpServer } from '@modelcontextprotocol/sdk/server/mcp.js'
import type { SshConfig } from '../config.ts'

// ---------------------------------------------------------------------------
// Mock ssh module — replace sshExec and sshExecWithStdin with spies.
// vi.mock is hoisted to the top of the file so we cannot reference top-level
// variables inside the factory. Instead we create the fns inside the factory
// and re-export them so tests can access the spies via the module.
// ---------------------------------------------------------------------------

vi.mock('../ssh.js', () => ({
  sshExec: vi.fn(),
  sshExecWithStdin: vi.fn(),
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

/**
 * Invoke the exec_on_node tool on an McpServer by directly calling the
 * registered handler.  We introspect the server's internal _registeredTools
 * map (same approach used in other sdk-level tests).
 */
async function invokeExecOnNode(
  params: Record<string, unknown>
): Promise<{ content: Array<{ type: string; text: string }> }> {
  const server = new McpServer({ name: 'test', version: '0.0.1' })
  registerExecOnNode(server, TEST_SSH_CONFIG)

  // The McpServer stores tools in a private object (not a Map).
  // The registered tool's callable method is named "handler".
  const registeredTools = (server as unknown as {
    _registeredTools: Record<string, { handler: (args: Record<string, unknown>) => Promise<unknown> }>
  })._registeredTools

  const tool = registeredTools['exec_on_node']
  if (!tool) throw new Error('exec_on_node tool not registered')

  return tool.handler(params) as Promise<{ content: Array<{ type: string; text: string }> }>
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

  it('test_truncation_at_limit — stdout >10000 chars → truncated=true, output cut to 10000', async () => {
    const longOutput = 'x'.repeat(15000)
    mockSshExec.mockResolvedValue({ stdout: longOutput, stderr: '', exitCode: 0 })

    const result = await invokeExecOnNode({ ip: '1.2.3.4', command: 'echo test' })

    expect(result.content).toHaveLength(1)
    const parsed = JSON.parse(result.content[0]!.text) as {
      stdout: string
      truncated: boolean
    }
    expect(parsed.truncated).toBe(true)
    expect(parsed.stdout.length).toBe(10000)
  })

  it('test_no_truncation_under_limit — stdout <10000 chars → truncated=false, full output', async () => {
    const shortOutput = 'hello world'
    mockSshExec.mockResolvedValue({ stdout: shortOutput, stderr: '', exitCode: 0 })

    const result = await invokeExecOnNode({ ip: '1.2.3.4', command: 'echo hello' })

    const parsed = JSON.parse(result.content[0]!.text) as {
      stdout: string
      truncated: boolean
    }
    expect(parsed.truncated).toBe(false)
    expect(parsed.stdout).toBe('hello world')
  })

  it('test_exec_redaction_applied — stdout containing K2_NODE_SECRET=xxx → redacted in output', async () => {
    const rawOutput = 'config: K2_NODE_SECRET=supersecretvalue123 end'
    mockSshExec.mockResolvedValue({ stdout: rawOutput, stderr: '', exitCode: 0 })

    const result = await invokeExecOnNode({ ip: '1.2.3.4', command: 'cat config' })

    const parsed = JSON.parse(result.content[0]!.text) as { stdout: string }
    expect(parsed.stdout).not.toContain('supersecretvalue123')
    expect(parsed.stdout).toContain('K2_NODE_SECRET=[REDACTED]')
  })

  it('test_stdin_pipe_script — scriptPath provided → reads local file, calls sshExecWithStdin', async () => {
    // Write a temp script file
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

    // sshExecWithStdin should have been called
    expect(mockSshExecWithStdin).toHaveBeenCalledOnce()
    const [calledIp, calledConfig, calledCommand, calledStdin] =
      mockSshExecWithStdin.mock.calls[0]! as [string, SshConfig, string, string]

    expect(calledIp).toBe('1.2.3.4')
    expect(calledConfig).toEqual(TEST_SSH_CONFIG)
    expect(calledCommand).toBe('bash -s')
    expect(calledStdin).toBe(scriptContent)

    // sshExec must NOT be called
    expect(mockSshExec).not.toHaveBeenCalled()

    const parsed = JSON.parse(result.content[0]!.text) as { stdout: string }
    expect(parsed.stdout).toBe('from-script')
  })

  it('passes timeout in milliseconds to ssh function', async () => {
    mockSshExec.mockResolvedValue({ stdout: 'ok', stderr: '', exitCode: 0 })

    await invokeExecOnNode({ ip: '1.2.3.4', command: 'cmd', timeout: 60 })

    const [, , , timeoutArg] = mockSshExec.mock.calls[0]! as [string, SshConfig, string, number]
    expect(timeoutArg).toBe(60000)
  })

  it('uses default timeout of 30s when not specified', async () => {
    mockSshExec.mockResolvedValue({ stdout: 'ok', stderr: '', exitCode: 0 })

    await invokeExecOnNode({ ip: '1.2.3.4', command: 'cmd' })

    const [, , , timeoutArg] = mockSshExec.mock.calls[0]! as [string, SshConfig, string, number]
    expect(timeoutArg).toBe(30000)
  })
})
