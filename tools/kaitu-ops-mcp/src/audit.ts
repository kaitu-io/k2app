/**
 * Audit logging module.
 *
 * Logs MCP tool invocations to a local file for post-hoc traceability.
 * Non-blocking (fire-and-forget) — audit failures never affect tool results.
 */

import * as fs from 'node:fs'
import * as path from 'node:path'
import * as os from 'node:os'

/** Maximum audit log file size before rotation (500 KB). */
const MAX_LOG_SIZE = 500 * 1024

/** Default audit log path: ~/.kaitu-ops/audit.log */
const LOG_DIR = path.join(os.homedir(), '.kaitu-ops')
const LOG_PATH = path.join(LOG_DIR, 'audit.log')

/**
 * Appends an audit entry for an MCP tool invocation.
 *
 * Format: `[ISO timestamp] [tool_name] key=value key=value ...`
 *
 * This function is fire-and-forget. Errors are silently ignored to avoid
 * disrupting tool execution. The log directory is created on first write.
 *
 * @param tool - MCP tool name (e.g. "exec_on_node", "ping_node")
 * @param fields - Key-value pairs to log (ip, command, status, exitCode, etc.)
 */
export async function audit(tool: string, fields: Record<string, unknown>): Promise<void> {
  try {
    // Ensure log directory exists
    fs.mkdirSync(LOG_DIR, { recursive: true })

    // Rotate if file exceeds max size
    try {
      const stat = fs.statSync(LOG_PATH)
      if (stat.size > MAX_LOG_SIZE) {
        fs.truncateSync(LOG_PATH, 0)
      }
    } catch {
      // File doesn't exist yet — that's fine
    }

    // Build log line
    const timestamp = new Date().toISOString()
    const pairs = Object.entries(fields)
      .map(([k, v]) => {
        const val = typeof v === 'string' ? `"${v}"` : String(v)
        return `${k}=${val}`
      })
      .join(' ')

    const line = `[${timestamp}] [${tool}] ${pairs}\n`

    // Append asynchronously
    fs.appendFileSync(LOG_PATH, line, 'utf-8')
  } catch {
    // Silently ignore audit failures — never disrupt tool execution
  }
}
