/**
 * kaitu-ops-mcp — MCP server entry point.
 *
 * Wires together config loading, the Center API client, and both MCP tools,
 * then connects to stdio transport for MCP protocol communication.
 */

import { McpServer } from '@modelcontextprotocol/sdk/server/mcp.js'
import { StdioServerTransport } from '@modelcontextprotocol/sdk/server/stdio.js'
import { loadConfig } from './config.js'
import type { Config } from './config.js'
import { CenterApiClient } from './center-api.js'
import { registerListNodes } from './tools/list-nodes.js'
import { registerExecOnNode } from './tools/exec-on-node.js'

/**
 * Creates and configures the MCP server with all tools registered.
 *
 * Extracted from main() so it can be called directly in tests without
 * starting the stdio transport (which would block the process).
 *
 * @param config - Fully resolved configuration object
 * @returns A configured McpServer with list_nodes and exec_on_node registered
 */
export async function createServer(config: Config): Promise<McpServer> {
  const apiClient = new CenterApiClient(config)

  const server = new McpServer({
    name: 'kaitu-ops',
    version: '0.1.0',
  })

  registerListNodes(server, apiClient)
  registerExecOnNode(server, config.ssh)

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
    console.error('Failed to start kaitu-ops MCP server:', err)
    process.exit(1)
  })
}
