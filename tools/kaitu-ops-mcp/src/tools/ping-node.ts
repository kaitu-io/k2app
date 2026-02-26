/**
 * ping_node MCP tool.
 *
 * Registers the ping_node tool on an McpServer instance.
 * Performs SSH connect-only test to check node reachability and measure latency.
 * No command is executed — only TCP + SSH handshake.
 */

import * as fs from 'node:fs'
import { Client } from 'ssh2'
import { McpServer } from '@modelcontextprotocol/sdk/server/mcp.js'
import { z } from 'zod'
import type { SshConfig } from '../config.js'
import { audit } from '../audit.js'

/** SSH handshake timeout in milliseconds. */
const PING_TIMEOUT_MS = 5000

/**
 * Registers the ping_node tool on the given McpServer.
 *
 * Tool behaviour:
 * - Opens a TCP connection + SSH handshake to the node.
 * - On success: returns { reachable: true, latencyMs }.
 * - On failure: returns { reachable: false, error }.
 * - No command is executed. Much lighter than exec_on_node.
 *
 * @param server - The McpServer to register the tool on.
 * @param sshConfig - SSH configuration used for the connection.
 */
export function registerPingNode(server: McpServer, sshConfig: SshConfig): void {
  server.tool(
    'ping_node',
    'Check if a Kaitu node is reachable via SSH. Returns latency on success, error on failure. No command executed.',
    {
      ip: z.string().describe('Node IP address'),
    },
    async (params) => {
      const { ip } = params
      const startTime = Date.now()

      try {
        const latencyMs = await sshPing(ip, sshConfig)

        await audit('ping_node', { ip, reachable: true, latencyMs })

        return {
          content: [
            {
              type: 'text' as const,
              text: JSON.stringify({ reachable: true, latencyMs }),
            },
          ],
        }
      } catch (err) {
        const errorMessage = err instanceof Error ? err.message : String(err)
        const latencyMs = Date.now() - startTime

        await audit('ping_node', { ip, reachable: false, latencyMs, error: errorMessage })

        return {
          content: [
            {
              type: 'text' as const,
              text: JSON.stringify({ reachable: false, error: errorMessage }),
            },
          ],
        }
      }
    }
  )
}

/**
 * Performs SSH connect-only ping: TCP + handshake, then immediately disconnect.
 *
 * @param host - Remote host IP or hostname.
 * @param sshConfig - SSH connection configuration.
 * @returns Latency in milliseconds.
 * @throws {Error} If connection fails or times out.
 */
function sshPing(host: string, sshConfig: SshConfig): Promise<number> {
  return new Promise<number>((resolve, reject) => {
    const client = new Client()
    const privateKey = fs.readFileSync(sshConfig.privateKeyPath, 'utf-8')
    const startTime = Date.now()

    let settled = false
    let timeoutHandle: ReturnType<typeof setTimeout> | null = null

    function settle(result: number | Error): void {
      if (settled) return
      settled = true
      if (timeoutHandle !== null) {
        clearTimeout(timeoutHandle)
        timeoutHandle = null
      }
      client.end()
      if (result instanceof Error) {
        reject(result)
      } else {
        resolve(result)
      }
    }

    // Timeout
    timeoutHandle = setTimeout(() => {
      timeoutHandle = null
      settle(new Error(`Connection timed out after ${PING_TIMEOUT_MS}ms to ${host}:${sshConfig.port}`))
    }, PING_TIMEOUT_MS)

    client.on('error', (err: Error & { level?: string; code?: string }) => {
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
      const latencyMs = Date.now() - startTime
      settle(latencyMs)
    })

    client.connect({
      host,
      port: sshConfig.port,
      username: sshConfig.user,
      privateKey,
    })
  })
}
