/**
 * exec_on_node MCP tool.
 *
 * Registers the exec_on_node tool on an McpServer instance.
 * The tool executes a command on a Kaitu node via SSH, with optional
 * script-piping via stdin, stdout/stderr redaction, and automatic truncation.
 *
 * Output includes a `status` field to distinguish SSH errors from command results:
 * - "success": command executed (check exitCode for pass/fail)
 * - "ssh_error": SSH connection or authentication failed (no command ran)
 * - "timeout": command timed out (partial output may be available)
 */

import * as fs from 'node:fs'
import { McpServer } from '@modelcontextprotocol/sdk/server/mcp.js'
import { z } from 'zod'
import type { SshConfig } from '../config.js'
import { sshExec, sshExecWithStdin } from '../ssh.js'
import { redactStdout } from '../redact.js'
import { audit } from '../audit.js'

/** Maximum characters of stdout to return before truncation. */
const STDOUT_TRUNCATE_LIMIT = 10000

/** Maximum characters of stderr to return before truncation (shorter — supplementary info). */
const STDERR_TRUNCATE_LIMIT = 2000

/**
 * Registers the exec_on_node tool on the given McpServer.
 *
 * Tool behaviour:
 * - If scriptPath is provided: reads the local file and pipes its content via
 *   stdin to the command on the remote host (defaults to "bash -s" if command
 *   is empty; use "sudo bash -s" for root execution).
 * - Otherwise: executes the command parameter directly via SSH.
 * - Both stdout and stderr are redacted via redactStdout() before returning.
 * - stdout is truncated at 10000 chars, stderr at 2000 chars.
 * - SSH connection errors return status="ssh_error" with no stdout/stderr.
 * - Timeout returns status="timeout" with partial output.
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
        .default(60)
        .describe('Timeout in seconds (default: 60)'),
      scriptPath: z
        .string()
        .optional()
        .describe(
          'Local script file path to pipe via stdin. The command parameter ' +
            'specifies what to run (e.g. "sudo bash -s"). Defaults to "bash -s" if command is not provided.'
        ),
    },
    async (params) => {
      const { ip, command, timeout, scriptPath } = params
      const timeoutMs = (timeout ?? 60) * 1000

      let result: { stdout: string; stderr: string; exitCode: number }

      try {
        if (scriptPath !== undefined) {
          // Read local script file and pipe via stdin.
          // Use command param as the remote shell (e.g. "sudo bash -s"), default "bash -s".
          const fileContent = fs.readFileSync(scriptPath, 'utf-8')
          const remoteCmd = command || 'bash -s'
          result = await sshExecWithStdin(ip, sshConfig, remoteCmd, fileContent, timeoutMs)
        } else {
          result = await sshExec(ip, sshConfig, command, timeoutMs)
        }
      } catch (err) {
        // SSH connection/auth errors are thrown as Error by ssh.ts
        const errorMessage = err instanceof Error ? err.message : String(err)

        await audit('exec_on_node', {
          ip,
          command: (scriptPath ?? command).slice(0, 200),
          status: 'ssh_error',
          error: errorMessage,
        })

        return {
          content: [
            {
              type: 'text' as const,
              text: JSON.stringify({
                status: 'ssh_error',
                error: errorMessage,
                exitCode: -1,
              }),
            },
          ],
        }
      }

      // Redact sensitive patterns from both stdout and stderr
      let stdout = redactStdout(result.stdout)
      let stderr = redactStdout(result.stderr)
      let truncated = false
      let stderrTruncated = false

      // Truncate stdout if it exceeds the limit
      if (stdout.length > STDOUT_TRUNCATE_LIMIT) {
        stdout = stdout.slice(0, STDOUT_TRUNCATE_LIMIT)
        truncated = true
      }

      // Truncate stderr if it exceeds the limit
      if (stderr.length > STDERR_TRUNCATE_LIMIT) {
        stderr = stderr.slice(0, STDERR_TRUNCATE_LIMIT)
        stderrTruncated = true
      }

      // Determine status
      const status = result.exitCode === -1 ? 'timeout' : 'success'

      await audit('exec_on_node', {
        ip,
        command: (scriptPath ?? command).slice(0, 200),
        status,
        exitCode: result.exitCode,
      })

      return {
        content: [
          {
            type: 'text' as const,
            text: JSON.stringify({
              status,
              stdout,
              stderr,
              exitCode: result.exitCode,
              truncated,
              stderrTruncated,
            }),
          },
        ],
      }
    }
  )
}
