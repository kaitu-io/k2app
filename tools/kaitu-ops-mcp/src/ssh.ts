/**
 * SSH connection helper module.
 *
 * Provides sshExec and sshExecWithStdin for executing commands on remote hosts
 * over SSH using the ssh2 npm package with private key authentication.
 */

import * as fs from 'node:fs'
import { Client } from 'ssh2'
import type { SshConfig } from './config.js'

/**
 * Result of an SSH command execution.
 */
export interface ExecResult {
  /** Standard output captured from the remote command. */
  stdout: string
  /** Standard error captured from the remote command. */
  stderr: string
  /** Exit code from the remote process. -1 if the command timed out. */
  exitCode: number
}

/**
 * Executes a command on a remote host via SSH using private key authentication.
 *
 * Reads the private key from sshConfig.privateKeyPath, connects to host,
 * and runs command. Stdout and stderr are captured separately.
 *
 * If timeoutMs is provided, the channel is destroyed and exitCode -1 is
 * returned in the result with an error message in stderr.
 *
 * @param host - Remote host IP or hostname to connect to.
 * @param sshConfig - SSH connection configuration (key path, username, port).
 * @param command - Shell command to execute on the remote host.
 * @param timeoutMs - Optional timeout in milliseconds. Defaults to no timeout.
 * @returns Promise resolving to {stdout, stderr, exitCode}.
 * @throws {Error} If the connection is refused or authentication fails.
 */
export async function sshExec(
  host: string,
  sshConfig: SshConfig,
  command: string,
  timeoutMs?: number
): Promise<ExecResult> {
  return _sshExecCore(host, sshConfig, command, undefined, timeoutMs)
}

/**
 * Executes a command on a remote host with stdin data piped to the process.
 *
 * Useful for transferring and executing scripts without copying them to disk.
 * After the SSH connection is ready, it runs command (typically "bash -s"),
 * writes stdinData to stdin, then closes stdin before capturing output.
 *
 * @param host - Remote host IP or hostname to connect to.
 * @param sshConfig - SSH connection configuration (key path, username, port).
 * @param command - Shell command to execute (typically "bash -s").
 * @param stdinData - Content to pipe via stdin (e.g. script content).
 * @param timeoutMs - Optional timeout in milliseconds. Defaults to no timeout.
 * @returns Promise resolving to {stdout, stderr, exitCode}.
 * @throws {Error} If the connection is refused or authentication fails.
 */
export async function sshExecWithStdin(
  host: string,
  sshConfig: SshConfig,
  command: string,
  stdinData: string,
  timeoutMs?: number
): Promise<ExecResult> {
  return _sshExecCore(host, sshConfig, command, stdinData, timeoutMs)
}

/**
 * Internal implementation shared by sshExec and sshExecWithStdin.
 *
 * @param host - Remote host to connect to.
 * @param sshConfig - SSH connection parameters.
 * @param command - Command to run on the remote host.
 * @param stdinData - If defined, this data is written to stdin then stdin is closed.
 * @param timeoutMs - If defined, kills the channel after this many milliseconds.
 */
function _sshExecCore(
  host: string,
  sshConfig: SshConfig,
  command: string,
  stdinData: string | undefined,
  timeoutMs: number | undefined
): Promise<ExecResult> {
  return new Promise<ExecResult>((resolve, reject) => {
    const client = new Client()
    const privateKey = fs.readFileSync(sshConfig.privateKeyPath, 'utf-8')

    let settled = false

    function settle(result: ExecResult | Error): void {
      if (settled) return
      settled = true
      client.end()
      if (result instanceof Error) {
        reject(result)
      } else {
        resolve(result)
      }
    }

    client.on('error', (err: Error & { level?: string; code?: string }) => {
      // Classify connection vs auth errors into user-friendly messages
      if (err.level === 'client-authentication') {
        settle(new Error(`Authentication failed for ${host}`))
      } else if (
        err.code === 'ECONNREFUSED' ||
        (err.message != null && err.message.toLowerCase().includes('econnrefused'))
      ) {
        settle(new Error(`Connection refused to ${host}:${sshConfig.port}`))
      } else {
        settle(new Error(`SSH connection error: ${err.message}`))
      }
    })

    client.on('ready', () => {
      client.exec(command, (execErr, channel) => {
        if (execErr) {
          settle(new Error(`Failed to execute command: ${execErr.message}`))
          return
        }

        let stdoutBuf = ''
        let stderrBuf = ''
        let exitCode = 0
        let timeoutHandle: ReturnType<typeof setTimeout> | null = null

        function cleanup(): void {
          if (timeoutHandle !== null) {
            clearTimeout(timeoutHandle)
            timeoutHandle = null
          }
        }

        // Set up timeout if requested
        if (timeoutMs !== undefined && timeoutMs > 0) {
          timeoutHandle = setTimeout(() => {
            timeoutHandle = null
            // Attempt to close the channel; destroy() may not exist on all implementations
            if (typeof (channel as { destroy?: () => void }).destroy === 'function') {
              ;(channel as { destroy: () => void }).destroy()
            } else {
              channel.close()
            }
            settle({
              stdout: stdoutBuf,
              stderr: `Command timed out after ${timeoutMs}ms`,
              exitCode: -1,
            })
          }, timeoutMs)
        }

        // Collect stdout
        channel.on('data', (chunk: Buffer) => {
          stdoutBuf += chunk.toString('utf-8')
        })

        // Collect stderr via the stderr substream
        channel.stderr.on('data', (chunk: Buffer) => {
          stderrBuf += chunk.toString('utf-8')
        })

        // Capture exit code
        channel.on('exit', (code: number | null) => {
          exitCode = code ?? 0
        })

        // Resolve when the channel is fully closed
        channel.on('close', () => {
          cleanup()
          settle({
            stdout: stdoutBuf,
            stderr: stderrBuf,
            exitCode,
          })
        })

        // If stdin data was provided, write it and then close stdin
        if (stdinData !== undefined) {
          channel.write(stdinData, 'utf-8', () => {
            channel.end()
          })
        }
      })
    })

    client.connect({
      host,
      port: sshConfig.port,
      username: sshConfig.user,
      privateKey,
    })
  })
}
