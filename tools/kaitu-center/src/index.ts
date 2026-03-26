/**
 * kaitu-center — MCP server entry point.
 *
 * Loads config, fetches permissions from backend, registers tools
 * based on allowed permission groups, then connects stdio transport.
 */

import { McpServer } from '@modelcontextprotocol/sdk/server/mcp.js'
import { StdioServerTransport } from '@modelcontextprotocol/sdk/server/stdio.js'
import { loadConfig } from './config.js'
import type { Config } from './config.js'
import { CenterApiClient } from './center-api.js'
import { fetchPermissions, type ToolRegistration } from './tool-factory.js'

// Standalone tools (SSH, S3, custom logic)
import { registerListNodes } from './tools/list-nodes.js'
import { registerExecOnNode } from './tools/exec-on-node.js'
import { registerPingNode } from './tools/ping-node.js'
import { registerDeleteNode } from './tools/delete-node.js'
import { registerDownloadDeviceLog } from './tools/download-device-log.js'

// Factory-declared domain tools
import { deviceLogTools } from './tools/admin-device-logs.js'
import { feedbackTicketTools } from './tools/admin-feedback-tickets.js'
import { userTools } from './tools/admin-users.js'
import { retailerTools } from './tools/admin-retailers.js'
import { edmTools } from './tools/admin-edm.js'
import { orderTools } from './tools/admin-orders.js'
import { campaignTools } from './tools/admin-campaigns.js'
import { licenseKeyTools } from './tools/admin-license-keys.js'
import { planTools } from './tools/admin-plans.js'
import { cloudTools } from './tools/admin-cloud.js'
import { tunnelTools } from './tools/admin-tunnels.js'
import { statsTools } from './tools/admin-stats.js'
import { approvalTools } from './tools/admin-approvals.js'
import { walletTools } from './tools/admin-wallet.js'
import { strategyTools } from './tools/admin-strategy.js'
import { announcementTools } from './tools/admin-announcements.js'

/** All factory-declared tools, aggregated for bulk registration. */
const allFactoryTools: ToolRegistration[] = [
  ...deviceLogTools,
  ...feedbackTicketTools,
  ...userTools,
  ...retailerTools,
  ...edmTools,
  ...orderTools,
  ...campaignTools,
  ...licenseKeyTools,
  ...planTools,
  ...cloudTools,
  ...tunnelTools,
  ...statsTools,
  ...approvalTools,
  ...walletTools,
  ...strategyTools,
  ...announcementTools,
]

/**
 * Standalone tool group mapping.
 * These tools have custom logic and can't use the factory.
 */
const STANDALONE_TOOLS: Array<{
  group: string
  register: (server: McpServer, apiClient: CenterApiClient, config: Config) => void
}> = [
  { group: 'nodes', register: (s, a) => registerListNodes(s, a) },
  { group: 'nodes.write', register: (s, _, c) => registerExecOnNode(s, c.ssh) },
  { group: 'nodes', register: (s, _, c) => registerPingNode(s, c.ssh) },
  { group: 'nodes.write', register: (s, a) => registerDeleteNode(s, a) },
  { group: 'device_logs', register: (s) => registerDownloadDeviceLog(s) },
]

export async function createServer(config: Config): Promise<McpServer> {
  const apiClient = new CenterApiClient(config)
  const server = new McpServer({ name: 'kaitu-center', version: '0.4.0' })

  // Fetch permissions from backend
  const permissions = await fetchPermissions(apiClient)
  const allowedGroups = new Set(permissions.groups)

  console.error(`[kaitu-center] Permissions: admin=${permissions.isAdmin}, groups=${permissions.groups.length}`)

  // Register standalone tools
  for (const tool of STANDALONE_TOOLS) {
    if (allowedGroups.has(tool.group)) {
      tool.register(server, apiClient, config)
    }
  }

  // Register factory tools
  for (const tool of allFactoryTools) {
    if (allowedGroups.has(tool.group)) {
      tool.register(server, apiClient)
    }
  }

  return server
}

async function main(): Promise<void> {
  const config = await loadConfig()
  const server = await createServer(config)
  const transport = new StdioServerTransport()
  await server.connect(transport)
}

const isEntryPoint =
  process.argv[1] !== undefined &&
  import.meta.url === new URL(`file://${process.argv[1]}`).href

if (isEntryPoint) {
  main().catch((err: unknown) => {
    console.error('Failed to start kaitu-center MCP server:', err)
    process.exit(1)
  })
}
