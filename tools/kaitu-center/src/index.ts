/**
 * kaitu-center — MCP server entry point.
 *
 * Wires together config loading, the Center API client, and all MCP tools,
 * then connects to stdio transport for MCP protocol communication.
 */

import { McpServer } from '@modelcontextprotocol/sdk/server/mcp.js'
import { StdioServerTransport } from '@modelcontextprotocol/sdk/server/stdio.js'
import { loadConfig } from './config.js'
import type { Config } from './config.js'
import { CenterApiClient } from './center-api.js'
import { getToolsForRole } from './roles.js'
import { registerListNodes } from './tools/list-nodes.js'
import { registerExecOnNode } from './tools/exec-on-node.js'
import { registerPingNode } from './tools/ping-node.js'
import { registerDeleteNode } from './tools/delete-node.js'
import { registerQueryDeviceLogs } from './tools/query-device-logs.js'
import { registerDownloadDeviceLog } from './tools/download-device-log.js'
import { registerQueryFeedbackTickets } from './tools/query-feedback-tickets.js'
import { registerResolveFeedbackTicket } from './tools/resolve-feedback-ticket.js'
import { registerLookupUser } from './tools/lookup-user.js'
import { registerListUserDevices } from './tools/list-user-devices.js'
import { registerCloseFeedbackTicket } from './tools/close-feedback-ticket.js'
import { registerListRetailers } from './tools/list-retailers.js'
import { registerGetRetailerDetail } from './tools/get-retailer-detail.js'
import { registerUpdateRetailerLevel } from './tools/update-retailer-level.js'
import { registerCreateRetailerNote } from './tools/create-retailer-note.js'
import { registerListRetailerTodos } from './tools/list-retailer-todos.js'
import { registerListEdmTemplates } from './tools/list-edm-templates.js'
import { registerCreateEdmTask } from './tools/create-edm-task.js'
import { registerPreviewEdmTargets } from './tools/preview-edm-targets.js'
import { registerGetEdmSendStats } from './tools/get-edm-send-stats.js'

/**
 * Creates and configures the MCP server with all tools registered.
 *
 * Extracted from main() so it can be called directly in tests without
 * starting the stdio transport (which would block the process).
 *
 * @param config - Fully resolved configuration object
 * @returns A configured McpServer with all tools registered
 */
export async function createServer(config: Config): Promise<McpServer> {
  const apiClient = new CenterApiClient(config)
  const role = process.env['KAITU_ROLE'] || 'devops'
  const allowed = new Set(getToolsForRole(role))

  const server = new McpServer({
    name: 'kaitu-center',
    version: '0.3.0',
  })

  // DevOps tools
  if (allowed.has('list_nodes'))              registerListNodes(server, apiClient)
  if (allowed.has('exec_on_node'))            registerExecOnNode(server, config.ssh)
  if (allowed.has('ping_node'))               registerPingNode(server, config.ssh)
  if (allowed.has('delete_node'))             registerDeleteNode(server, apiClient)

  // Shared tools (DevOps + Support)
  if (allowed.has('query_device_logs'))       registerQueryDeviceLogs(server, apiClient)
  if (allowed.has('download_device_log'))     registerDownloadDeviceLog(server)
  if (allowed.has('query_feedback_tickets'))  registerQueryFeedbackTickets(server, apiClient)
  if (allowed.has('resolve_feedback_ticket')) registerResolveFeedbackTicket(server, apiClient)

  // Support tools
  if (allowed.has('lookup_user'))              registerLookupUser(server, apiClient)
  if (allowed.has('list_user_devices'))        registerListUserDevices(server, apiClient)
  if (allowed.has('close_feedback_ticket'))    registerCloseFeedbackTicket(server, apiClient)

  // Marketing tools — retailers
  if (allowed.has('list_retailers'))         registerListRetailers(server, apiClient)
  if (allowed.has('get_retailer_detail'))    registerGetRetailerDetail(server, apiClient)
  if (allowed.has('update_retailer_level'))  registerUpdateRetailerLevel(server, apiClient)
  if (allowed.has('create_retailer_note'))   registerCreateRetailerNote(server, apiClient)
  if (allowed.has('list_retailer_todos'))    registerListRetailerTodos(server, apiClient)

  // Marketing tools — EDM
  if (allowed.has('list_edm_templates'))     registerListEdmTemplates(server, apiClient)
  if (allowed.has('create_edm_task'))        registerCreateEdmTask(server, apiClient)
  if (allowed.has('preview_edm_targets'))    registerPreviewEdmTargets(server, apiClient)
  if (allowed.has('get_edm_send_stats'))     registerGetEdmSendStats(server, apiClient)

  return server
}

/**
 * Main entry point: loads config, creates the server, and connects stdio transport.
 */
async function main(): Promise<void> {
  const config = await loadConfig()
  const server = await createServer(config)

  const transport = new StdioServerTransport()
  await server.connect(transport)
}

// Only run main() when this file is the entry point, not when imported as a module.
// Comparing import.meta.url to the process argv path prevents main() from running
// during tests or when the module is imported by other code.
const isEntryPoint =
  process.argv[1] !== undefined &&
  import.meta.url === new URL(`file://${process.argv[1]}`).href

if (isEntryPoint) {
  main().catch((err: unknown) => {
    console.error('Failed to start kaitu-center MCP server:', err)
    process.exit(1)
  })
}
