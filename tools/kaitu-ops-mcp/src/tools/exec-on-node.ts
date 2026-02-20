/**
 * exec_on_node MCP tool — stub for RED phase.
 * Full implementation in GREEN phase.
 */

import type { McpServer } from '@modelcontextprotocol/sdk/server/mcp.js'
import type { SshConfig } from '../config.ts'

export function registerExecOnNode(_server: McpServer, _sshConfig: SshConfig): void {
  throw new Error('Not implemented')
}
