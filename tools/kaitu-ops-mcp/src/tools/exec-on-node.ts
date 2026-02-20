/**
 * exec_on_node MCP tool.
 *
 * Registers the exec_on_node tool on an McpServer instance.
 * The tool executes a command on a Kaitu node via SSH, with optional
 * script-piping via stdin, stdout redaction, and automatic truncation.
 */

import * as fs from 'node:fs'
import { McpServer } from '@modelcontextprotocol/sdk/server/mcp.js'
import { z } from 'zod'
import type { SshConfig } from '../config.js'
import { sshExec, sshExecWithStdin } from '../ssh.js'
import { redactStdout } from '../redact.js'

/** Maximum characters of stdout to return before truncation. */
const STDOUT_TRUNCATE_LIMIT = 10000

/**
 * Registers the exec_on_node tool on the given McpServer.
 *
 * Tool behaviour:
 * - If scriptPath is provided: reads the local file and pipes its content via
 *   stdin to "bash -s" on the remote host, ignoring the command parameter.
 * - Otherwise: executes the command parameter directly via SSH.
 * - stdout is always redacted via redactStdout() before returning.
 * - If stdout exceeds 10000 chars it is truncated and truncated=true is set.
 *
 * @param server - The McpServer to register the tool on.
 * @param sshConfig - SSH configuration used for all connections.
 */
export function registerExecOnNode(server: McpServer, sshConfig: SshConfig): void {
  server.tool(
    'exec_on_node',
    'Execute a command on a Kaitu node via SSH.',
    {
      ip: z.string().describe('Node IP address'),
      command: z.string().describe('Command to execute on the remote node'),
      timeout: z
        .number()
        .optional()
        .default(30)
        .describe('Timeout in seconds (default: 30)'),
      scriptPath: z
        .string()
        .optional()
        .describe('Local script file path to pipe via stdin (uses bash -s)'),
    },
    async (params) => {
      const { ip, command, timeout, scriptPath } = params
      const timeoutMs = (timeout ?? 30) * 1000

      let result: { stdout: string; stderr: string; exitCode: number }

      if (scriptPath !== undefined) {
        // Read local script file and pipe via stdin
        const fileContent = fs.readFileSync(scriptPath, 'utf-8')
        result = await sshExecWithStdin(ip, sshConfig, 'bash -s', fileContent, timeoutMs)
      } else {
        result = await sshExec(ip, sshConfig, command, timeoutMs)
      }

      // Redact sensitive patterns from stdout
      let stdout = redactStdout(result.stdout)
      let truncated = false

      // Truncate if stdout exceeds the limit
      if (stdout.length > STDOUT_TRUNCATE_LIMIT) {
        stdout = stdout.slice(0, STDOUT_TRUNCATE_LIMIT)
        truncated = true
      }

      return {
        content: [
          {
            type: 'text' as const,
            text: JSON.stringify({
              stdout,
              stderr: result.stderr,
              exitCode: result.exitCode,
              truncated,
            }),
          },
        ],
      }
    }
  )
}
