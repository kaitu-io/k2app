/**
 * SSH helper module — stub for RED phase.
 * Full implementation in GREEN phase.
 */

import type { SshConfig } from './config.ts'

export interface ExecResult {
  stdout: string
  stderr: string
  exitCode: number
}

export async function sshExec(
  _host: string,
  _sshConfig: SshConfig,
  _command: string,
  _timeoutMs?: number
): Promise<ExecResult> {
  throw new Error('Not implemented')
}

export async function sshExecWithStdin(
  _host: string,
  _sshConfig: SshConfig,
  _command: string,
  _stdinData: string,
  _timeoutMs?: number
): Promise<ExecResult> {
  throw new Error('Not implemented')
}
