import { z } from 'zod'
import type { McpServer } from '@modelcontextprotocol/sdk/server/mcp.js'
import type { CenterApiClient } from '../center-api.js'
import { audit } from '../audit.js'

/**
 * Raw tunnel shape as returned by Center API batch-matrix endpoint.
 */
interface RawTunnel {
  id: number
  domain: string
  protocol: string
  port: number
  server_url: string
  [key: string]: unknown
}

/**
 * Raw node shape as returned by Center API batch-matrix endpoint.
 */
interface RawNode {
  id: number
  name: string
  ipv4: string
  ipv6: string
  country: string
  region: string
  status: string
  created_at: string
  updated_at: string
  tunnels: RawTunnel[]
  batch_script_results: unknown
  [key: string]: unknown
}

/**
 * Raw batch-matrix API response shape.
 */
interface BatchMatrixResponse {
  code: number
  data: {
    nodes: RawNode[]
  }
}

/**
 * Filtered tunnel shape exposed to MCP callers.
 */
export interface TunnelInfo {
  domain: string
  protocol: string
  port: number
  serverUrl: string
}

/**
 * Filtered node shape exposed to MCP callers.
 * Only safe, non-sensitive fields are included.
 */
export interface NodeInfo {
  name: string
  ipv4: string
  ipv6: string
  country: string
  region: string
  tunnels: TunnelInfo[]
  meta?: Record<string, unknown>
}

/**
 * Optional filters for the filterNodes function.
 */
export interface NodeFilters {
  country?: string
  name?: string
}

/**
 * Type guard to verify the raw API response matches the expected BatchMatrixResponse shape.
 */
function isBatchMatrixResponse(value: unknown): value is BatchMatrixResponse {
  if (typeof value !== 'object' || value === null) return false
  const obj = value as Record<string, unknown>
  if (typeof obj['code'] !== 'number') return false
  if (typeof obj['data'] !== 'object' || obj['data'] === null) return false
  const data = obj['data'] as Record<string, unknown>
  if (!Array.isArray(data['nodes'])) return false
  return true
}

/**
 * Maps a raw tunnel from the API response to the filtered TunnelInfo shape.
 * Converts snake_case `server_url` to camelCase `serverUrl` and drops `id`.
 *
 * @param raw - The raw tunnel object from the API response
 * @returns A filtered TunnelInfo with only the safe fields
 */
function mapTunnel(raw: RawTunnel): TunnelInfo {
  return {
    domain: raw.domain,
    protocol: raw.protocol,
    port: raw.port,
    serverUrl: raw.server_url,
  }
}

/**
 * Maps a raw node from the API response to the filtered NodeInfo shape.
 * Drops id, status, created_at, updated_at, and batch_script_results.
 *
 * @param raw - The raw node object from the API response
 * @returns A filtered NodeInfo with only the safe fields
 */
function mapNode(raw: RawNode): NodeInfo {
  const node: NodeInfo = {
    name: raw.name,
    ipv4: raw.ipv4,
    ipv6: raw.ipv6,
    country: raw.country,
    region: raw.region,
    tunnels: raw.tunnels.map(mapTunnel),
  }
  // Pass through meta if present (added by sidecar registration)
  if (raw['meta'] != null && typeof raw['meta'] === 'object') {
    node.meta = raw['meta'] as Record<string, unknown>
  }
  return node
}

/**
 * Filters nodes from the raw batch-matrix API response, applying optional country/name
 * filters and stripping all sensitive or heavy fields (id, status, timestamps,
 * batch_script_results). Returns only {name, ipv4, ipv6, country, region, tunnels}.
 *
 * @param rawResponse - The raw unknown response from the Center API batch-matrix endpoint
 * @param filters - Optional filters: `country` and/or `name` for exact string matching
 * @returns Array of filtered NodeInfo objects
 * @throws {Error} If the response does not match the expected BatchMatrixResponse shape
 */
export function filterNodes(rawResponse: unknown, filters: NodeFilters): NodeInfo[] {
  if (!isBatchMatrixResponse(rawResponse)) {
    throw new Error('Invalid batch-matrix response shape from Center API')
  }

  let nodes = rawResponse.data.nodes

  if (filters.country !== undefined) {
    const country = filters.country
    nodes = nodes.filter((node) => node.country === country)
  }

  if (filters.name !== undefined) {
    const name = filters.name
    nodes = nodes.filter((node) => node.name === name)
  }

  return nodes.map(mapNode)
}

/**
 * Registers the `list_nodes` MCP tool on the given server.
 *
 * The tool fetches all nodes from the Center API batch-matrix endpoint, applies optional
 * country/name filters, and returns a cleaned list of node objects. Sensitive fields such
 * as batch_script_results, internal IDs, and status are always stripped from the output.
 *
 * @param server - The McpServer instance to register the tool on
 * @param apiClient - The CenterApiClient used to make the authenticated API request
 */
export function registerListNodes(server: McpServer, apiClient: CenterApiClient): void {
  server.tool(
    'list_nodes',
    'List all Kaitu VPN nodes with their tunnels. Optionally filter by country or name.',
    {
      country: z.string().optional().describe('Filter nodes by country code (e.g. "jp", "sg")'),
      name: z.string().optional().describe('Filter nodes by exact node name (e.g. "jp-01")'),
    },
    async (params) => {
      const rawResponse = await apiClient.request('/app/nodes/batch-matrix')
      const nodes = filterNodes(rawResponse, {
        country: params.country,
        name: params.name,
      })

      await audit('list_nodes', {
        filter: [params.country, params.name].filter(Boolean).join(',') || 'none',
        count: nodes.length,
      })

      return {
        content: [
          {
            type: 'text',
            text: JSON.stringify(nodes, null, 2),
          },
        ],
      }
    }
  )
}
