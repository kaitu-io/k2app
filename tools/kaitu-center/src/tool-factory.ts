/**
 * Factory for generating MCP tool registrations from declarative configs.
 *
 * Handles GET query strings, POST/PUT/DELETE bodies, dynamic path interpolation,
 * custom mapQuery/mapBody overrides, unified error formatting, and audit logging.
 */

import type { ZodRawShapeCompat } from '@modelcontextprotocol/sdk/server/zod-compat.js'
import type { McpServer } from '@modelcontextprotocol/sdk/server/mcp.js'
import type { CenterApiClient } from './center-api.js'
import { audit } from './audit.js'

type Params = Record<string, unknown>

export interface ApiToolDef {
  name: string
  description: string
  group: string
  params?: ZodRawShapeCompat
  method?: 'GET' | 'POST' | 'PUT' | 'DELETE'
  path: string | ((params: Params) => string)
  mapQuery?: (params: Params) => Record<string, string>
  mapBody?: (params: Params) => unknown
}

export interface ToolRegistration {
  name: string
  group: string
  register: (server: McpServer, apiClient: CenterApiClient) => void
}

interface CenterResponse {
  code: number
  message?: string
  data?: unknown
}

/**
 * Detects which parameter keys a dynamic path function accesses.
 *
 * Uses a Proxy to intercept property access during a dry-run call,
 * collecting the set of accessed keys so they can be excluded from
 * query strings or request bodies.
 */
function getPathParamKeys(pathFn: (params: Params) => string): Set<string> {
  const keys = new Set<string>()
  const proxy = new Proxy({} as Params, {
    get(_target, prop: string) {
      keys.add(prop)
      return `__${prop}__`
    },
  })
  pathFn(proxy)
  return keys
}

/**
 * Creates an MCP tool registration from a declarative API tool definition.
 *
 * Behaviors by HTTP method:
 * - GET: auto-builds query string from non-path params (skips undefined). `mapQuery` overrides.
 * - POST/PUT: sends non-path params as JSON body. `mapBody` overrides.
 * - DELETE: sends non-path params as JSON body if any remain. `mapBody` overrides.
 *
 * Error handling:
 * - API response with `code !== 0` returns `{error, code}` text content.
 * - Exceptions return `{error}` text content.
 *
 * All invocations are audit-logged via `audit()`.
 */
export function defineApiTool(def: ApiToolDef): ToolRegistration {
  const method = def.method ?? 'GET'
  const pathParamKeys = typeof def.path === 'function'
    ? getPathParamKeys(def.path)
    : new Set<string>()

  return {
    name: def.name,
    group: def.group,
    register(server: McpServer, apiClient: CenterApiClient) {
      server.tool(
        def.name,
        def.description,
        def.params ?? {},
        async (params: Params) => {
          try {
            const resolvedPath = typeof def.path === 'function'
              ? def.path(params)
              : def.path

            let requestPath = resolvedPath
            let requestOptions: RequestInit | undefined

            if (method === 'GET') {
              let queryParams: Record<string, string>
              if (def.mapQuery) {
                queryParams = def.mapQuery(params)
              } else {
                queryParams = {}
                for (const [key, value] of Object.entries(params)) {
                  if (value !== undefined && !pathParamKeys.has(key)) {
                    queryParams[key] = String(value)
                  }
                }
              }
              const qs = new URLSearchParams(queryParams).toString()
              if (qs) requestPath = `${resolvedPath}?${qs}`
            } else {
              const nonPathParams: Params = {}
              for (const [key, value] of Object.entries(params)) {
                if (!pathParamKeys.has(key) && value !== undefined) {
                  nonPathParams[key] = value
                }
              }

              const body = def.mapBody ? def.mapBody(params) : nonPathParams
              const hasBody = body != null && typeof body === 'object' && Object.keys(body as Params).length > 0

              requestOptions = {
                method,
                ...(hasBody ? { body: JSON.stringify(body) } : {}),
              }
            }

            const rawResponse = await apiClient.request(requestPath, requestOptions)
            const response = rawResponse as CenterResponse

            if (response.code !== 0) {
              await audit(def.name, { ...params, error: response.message })
              return {
                content: [{
                  type: 'text' as const,
                  text: JSON.stringify({ error: response.message, code: response.code }),
                }],
              }
            }

            await audit(def.name, params)
            return {
              content: [{
                type: 'text' as const,
                text: JSON.stringify(response.data, null, 2),
              }],
            }
          } catch (err) {
            const errorMessage = err instanceof Error ? err.message : String(err)
            await audit(def.name, { ...params, error: errorMessage })
            return {
              content: [{
                type: 'text' as const,
                text: JSON.stringify({ error: errorMessage }),
              }],
            }
          }
        },
      )
    },
  }
}

export interface Permissions {
  isAdmin: boolean
  roles: number
  groups: string[]
}

/**
 * Fetches the current user's permissions from the Center API.
 *
 * Calls `GET /app/my-permissions` and returns `{isAdmin, roles, groups}`.
 * On any error (network, non-zero code), returns a safe fallback with
 * empty groups and no admin access.
 */
export async function fetchPermissions(apiClient: CenterApiClient): Promise<Permissions> {
  try {
    const raw = await apiClient.request('/app/my-permissions')
    const response = raw as CenterResponse
    if (response.code !== 0) {
      console.error(`[kaitu-center] Permission fetch failed: code=${response.code} message=${response.message}`)
      return { isAdmin: false, roles: 0, groups: [] }
    }
    const data = response.data as { is_admin?: boolean; roles?: number; groups?: string[] }
    return {
      isAdmin: data.is_admin ?? false,
      roles: data.roles ?? 0,
      groups: data.groups ?? [],
    }
  } catch (err) {
    console.error(`[kaitu-center] Permission fetch error: ${err instanceof Error ? err.message : err}`)
    return { isAdmin: false, roles: 0, groups: [] }
  }
}
