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
import { createCenterClient, createCmsClient } from './center-api.js'
import { fetchPermissions, type ApiClients, type ToolRegistration } from './tool-factory.js'

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
import { nodeOperationTools } from './tools/admin-node-operations.js'
import { tunnelTools } from './tools/admin-tunnels.js'
import { statsTools } from './tools/admin-stats.js'
import { approvalTools } from './tools/admin-approvals.js'
import { walletTools } from './tools/admin-wallet.js'
import { strategyTools } from './tools/admin-strategy.js'
import { announcementTools } from './tools/admin-announcements.js'

// CMS tools — target Payload REST at /payload/api/*
import { cmsPostsTools } from './tools/cms-posts.js'
import { cmsCategoriesTools } from './tools/cms-categories.js'
import { cmsTagsTools } from './tools/cms-tags.js'
import { cmsMediaTools } from './tools/cms-media.js'
import { registerGetPostAllLocales, registerRetranslatePost } from './tools/cms-post-helpers.js'
import { registerUploadMedia } from './tools/cms-upload-media.js'

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
  ...nodeOperationTools,
  ...tunnelTools,
  ...statsTools,
  ...approvalTools,
  ...walletTools,
  ...strategyTools,
  ...announcementTools,
  ...cmsPostsTools,
  ...cmsCategoriesTools,
  ...cmsTagsTools,
  ...cmsMediaTools,
]

/**
 * Standalone tool group mapping.
 * These tools have custom logic and can't use the factory.
 *
 * Receive both API clients plus the config. Standalone tools that only need
 * the Center client pick `clients.center`; SSH-only tools ignore `clients`
 * entirely and read from `config.ssh`.
 */
const STANDALONE_TOOLS: Array<{
  group: string
  register: (server: McpServer, clients: ApiClients, config: Config) => void
}> = [
  { group: 'nodes',        register: (s, c) => registerListNodes(s, c.center) },
  { group: 'nodes.write',  register: (s, _c, cfg) => registerExecOnNode(s, cfg.ssh) },
  { group: 'nodes',        register: (s, _c, cfg) => registerPingNode(s, cfg.ssh) },
  { group: 'nodes.write',  register: (s, c) => registerDeleteNode(s, c.center) },
  { group: 'device_logs',  register: (s) => registerDownloadDeviceLog(s) },
  { group: 'cms',          register: (s, c) => registerGetPostAllLocales(s, c.cms) },
  { group: 'cms',          register: (s, c) => registerRetranslatePost(s, c.cms) },
  { group: 'cms',          register: (s, c) => registerUploadMedia(s, c.cms) },
]

export async function createServer(config: Config): Promise<McpServer> {
  const centerClient = createCenterClient(config)
  const cmsClient = createCmsClient(config)
  const clients: ApiClients = { center: centerClient, cms: cmsClient }
  const server = new McpServer({ name: 'kaitu-center', version: '0.5.0' })

  // Fetch permissions from backend (always via the Center client).
  const permissions = await fetchPermissions(centerClient)
  const allowedGroups = new Set(permissions.groups)

  console.error(`[kaitu-center] Permissions: admin=${permissions.isAdmin}, groups=${permissions.groups.length}`)

  // Register standalone tools
  for (const tool of STANDALONE_TOOLS) {
    if (allowedGroups.has(tool.group)) {
      tool.register(server, clients, config)
    }
  }

  // Register factory tools
  for (const tool of allFactoryTools) {
    if (allowedGroups.has(tool.group)) {
      tool.register(server, clients)
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
