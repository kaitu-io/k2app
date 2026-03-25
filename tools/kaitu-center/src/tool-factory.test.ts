import { describe, it, expect, vi, beforeEach } from 'vitest'
import { z } from 'zod'
import { defineApiTool, fetchPermissions } from './tool-factory.ts'
import type { CenterApiClient } from './center-api.ts'

vi.mock('./audit.ts', () => ({
  audit: vi.fn().mockResolvedValue(undefined),
}))

function createMockServer() {
  const tools: Record<string, { description: string; schema: unknown; handler: Function }> = {}
  return {
    tool: vi.fn((name: string, description: string, schema: unknown, handler: Function) => {
      tools[name] = { description, schema, handler }
    }),
    _tools: tools,
  }
}

function createMockApiClient(response: unknown = { code: 0, data: {} }) {
  return {
    request: vi.fn().mockResolvedValue(response),
  } as unknown as CenterApiClient
}

describe('defineApiTool', () => {
  it('returns correct name, group, and register function', () => {
    const reg = defineApiTool({
      name: 'test_tool',
      description: 'A test tool',
      group: 'test',
      path: '/api/test',
    })

    expect(reg.name).toBe('test_tool')
    expect(reg.group).toBe('test')
    expect(typeof reg.register).toBe('function')
  })

  it('registers tool on the server with correct name and description', () => {
    const server = createMockServer()
    const apiClient = createMockApiClient()
    const reg = defineApiTool({
      name: 'my_tool',
      description: 'My description',
      group: 'ops',
      path: '/api/items',
    })

    reg.register(server as any, apiClient)

    expect(server.tool).toHaveBeenCalledWith(
      'my_tool',
      'My description',
      {},
      expect.any(Function),
    )
  })

  describe('GET requests', () => {
    it('auto-builds query string from params, skips undefined', async () => {
      const server = createMockServer()
      const apiClient = createMockApiClient({ code: 0, data: { ok: true } })
      const reg = defineApiTool({
        name: 'list_items',
        description: 'List items',
        group: 'items',
        params: { page: z.string(), status: z.string().optional() },
        path: '/api/items',
      })

      reg.register(server as any, apiClient)
      await server._tools['list_items'].handler({ page: '2', status: undefined })

      const calledPath = (apiClient.request as any).mock.calls[0][0] as string
      expect(calledPath).toBe('/api/items?page=2')
      expect((apiClient.request as any).mock.calls[0][1]).toBeUndefined()
    })

    it('includes all defined params in query string', async () => {
      const server = createMockServer()
      const apiClient = createMockApiClient({ code: 0, data: [] })
      const reg = defineApiTool({
        name: 'search',
        description: 'Search',
        group: 'search',
        params: { q: z.string(), limit: z.string() },
        path: '/api/search',
      })

      reg.register(server as any, apiClient)
      await server._tools['search'].handler({ q: 'hello', limit: '10' })

      const calledPath = (apiClient.request as any).mock.calls[0][0] as string
      expect(calledPath).toContain('q=hello')
      expect(calledPath).toContain('limit=10')
    })

    it('excludes path params from query string with dynamic path', async () => {
      const server = createMockServer()
      const apiClient = createMockApiClient({ code: 0, data: {} })
      const reg = defineApiTool({
        name: 'get_item',
        description: 'Get item',
        group: 'items',
        params: { id: z.string(), fields: z.string() },
        path: (p) => `/api/items/${p.id}`,
      })

      reg.register(server as any, apiClient)
      await server._tools['get_item'].handler({ id: '42', fields: 'name,price' })

      const calledPath = (apiClient.request as any).mock.calls[0][0] as string
      expect(calledPath).toBe('/api/items/42?fields=name%2Cprice')
      // id should NOT appear in query string
      expect(calledPath).not.toContain('id=')
    })

    it('mapQuery overrides default query building', async () => {
      const server = createMockServer()
      const apiClient = createMockApiClient({ code: 0, data: {} })
      const reg = defineApiTool({
        name: 'custom_query',
        description: 'Custom query',
        group: 'test',
        params: { keyword: z.string() },
        path: '/api/search',
        mapQuery: (params) => ({ q: String(params.keyword), source: 'mcp' }),
      })

      reg.register(server as any, apiClient)
      await server._tools['custom_query'].handler({ keyword: 'test' })

      const calledPath = (apiClient.request as any).mock.calls[0][0] as string
      expect(calledPath).toContain('q=test')
      expect(calledPath).toContain('source=mcp')
      expect(calledPath).not.toContain('keyword=')
    })
  })

  describe('POST requests', () => {
    it('sends non-path params as JSON body', async () => {
      const server = createMockServer()
      const apiClient = createMockApiClient({ code: 0, data: { created: true } })
      const reg = defineApiTool({
        name: 'create_item',
        description: 'Create item',
        group: 'items',
        params: { name: z.string(), price: z.number() },
        method: 'POST',
        path: '/api/items',
      })

      reg.register(server as any, apiClient)
      await server._tools['create_item'].handler({ name: 'Widget', price: 9.99 })

      expect(apiClient.request).toHaveBeenCalledWith('/api/items', {
        method: 'POST',
        body: JSON.stringify({ name: 'Widget', price: 9.99 }),
      })
    })

    it('mapBody overrides default body', async () => {
      const server = createMockServer()
      const apiClient = createMockApiClient({ code: 0, data: {} })
      const reg = defineApiTool({
        name: 'custom_body',
        description: 'Custom body',
        group: 'test',
        params: { user: z.string(), role: z.string() },
        method: 'POST',
        path: '/api/assign',
        mapBody: (params) => ({ user_id: params.user, role_name: params.role }),
      })

      reg.register(server as any, apiClient)
      await server._tools['custom_body'].handler({ user: 'u1', role: 'admin' })

      expect(apiClient.request).toHaveBeenCalledWith('/api/assign', {
        method: 'POST',
        body: JSON.stringify({ user_id: 'u1', role_name: 'admin' }),
      })
    })
  })

  describe('PUT requests', () => {
    it('sends remaining params as body with dynamic path', async () => {
      const server = createMockServer()
      const apiClient = createMockApiClient({ code: 0, data: {} })
      const reg = defineApiTool({
        name: 'update_item',
        description: 'Update item',
        group: 'items',
        params: { id: z.string(), name: z.string(), price: z.number() },
        method: 'PUT',
        path: (p) => `/api/items/${p.id}`,
      })

      reg.register(server as any, apiClient)
      await server._tools['update_item'].handler({ id: '7', name: 'Gadget', price: 19.99 })

      expect(apiClient.request).toHaveBeenCalledWith('/api/items/7', {
        method: 'PUT',
        body: JSON.stringify({ name: 'Gadget', price: 19.99 }),
      })
    })
  })

  describe('DELETE requests', () => {
    it('sends no body when no remaining params after path interpolation', async () => {
      const server = createMockServer()
      const apiClient = createMockApiClient({ code: 0, data: {} })
      const reg = defineApiTool({
        name: 'delete_item',
        description: 'Delete item',
        group: 'items',
        params: { id: z.string() },
        method: 'DELETE',
        path: (p) => `/api/items/${p.id}`,
      })

      reg.register(server as any, apiClient)
      await server._tools['delete_item'].handler({ id: '99' })

      expect(apiClient.request).toHaveBeenCalledWith('/api/items/99', {
        method: 'DELETE',
      })
    })

    it('sends body when there are remaining non-path params', async () => {
      const server = createMockServer()
      const apiClient = createMockApiClient({ code: 0, data: {} })
      const reg = defineApiTool({
        name: 'delete_with_reason',
        description: 'Delete with reason',
        group: 'items',
        params: { id: z.string(), reason: z.string() },
        method: 'DELETE',
        path: (p) => `/api/items/${p.id}`,
      })

      reg.register(server as any, apiClient)
      await server._tools['delete_with_reason'].handler({ id: '99', reason: 'obsolete' })

      expect(apiClient.request).toHaveBeenCalledWith('/api/items/99', {
        method: 'DELETE',
        body: JSON.stringify({ reason: 'obsolete' }),
      })
    })
  })

  describe('error handling', () => {
    it('returns error and code when API response code !== 0', async () => {
      const server = createMockServer()
      const apiClient = createMockApiClient({ code: 1001, message: 'Not found', data: null })
      const reg = defineApiTool({
        name: 'fail_tool',
        description: 'Will fail',
        group: 'test',
        path: '/api/missing',
      })

      reg.register(server as any, apiClient)
      const result = await server._tools['fail_tool'].handler({})

      expect(result.content[0].text).toBe(JSON.stringify({ error: 'Not found', code: 1001 }))
    })

    it('returns error message on exception', async () => {
      const server = createMockServer()
      const apiClient = {
        request: vi.fn().mockRejectedValue(new Error('Network timeout')),
      } as unknown as CenterApiClient
      const reg = defineApiTool({
        name: 'crash_tool',
        description: 'Will crash',
        group: 'test',
        path: '/api/boom',
      })

      reg.register(server as any, apiClient)
      const result = await server._tools['crash_tool'].handler({})

      expect(result.content[0].text).toBe(JSON.stringify({ error: 'Network timeout' }))
    })

    it('handles non-Error exception', async () => {
      const server = createMockServer()
      const apiClient = {
        request: vi.fn().mockRejectedValue('string error'),
      } as unknown as CenterApiClient
      const reg = defineApiTool({
        name: 'string_err',
        description: 'String error',
        group: 'test',
        path: '/api/oops',
      })

      reg.register(server as any, apiClient)
      const result = await server._tools['string_err'].handler({})

      expect(result.content[0].text).toBe(JSON.stringify({ error: 'string error' }))
    })
  })

  describe('successful response', () => {
    it('returns formatted JSON data on success', async () => {
      const server = createMockServer()
      const data = { users: [{ id: 1, name: 'Alice' }] }
      const apiClient = createMockApiClient({ code: 0, data })
      const reg = defineApiTool({
        name: 'list_users',
        description: 'List users',
        group: 'users',
        path: '/api/users',
      })

      reg.register(server as any, apiClient)
      const result = await server._tools['list_users'].handler({})

      expect(result.content[0].text).toBe(JSON.stringify(data, null, 2))
    })
  })
})

describe('fetchPermissions', () => {
  it('returns permissions from API response', async () => {
    const apiClient = createMockApiClient({
      code: 0,
      data: { is_admin: true, roles: 3, groups: ['admin', 'nodes', 'orders'] },
    })

    const perms = await fetchPermissions(apiClient)

    expect(perms).toEqual({
      isAdmin: true,
      roles: 3,
      groups: ['admin', 'nodes', 'orders'],
    })
    expect(apiClient.request).toHaveBeenCalledWith('/app/my-permissions')
  })

  it('returns empty fallback on non-zero code', async () => {
    const apiClient = createMockApiClient({ code: 401, message: 'Unauthorized' })
    const consoleSpy = vi.spyOn(console, 'error').mockImplementation(() => {})

    const perms = await fetchPermissions(apiClient)

    expect(perms).toEqual({ isAdmin: false, roles: 0, groups: [] })
    consoleSpy.mockRestore()
  })

  it('returns empty fallback on network error', async () => {
    const apiClient = {
      request: vi.fn().mockRejectedValue(new Error('Connection refused')),
    } as unknown as CenterApiClient
    const consoleSpy = vi.spyOn(console, 'error').mockImplementation(() => {})

    const perms = await fetchPermissions(apiClient)

    expect(perms).toEqual({ isAdmin: false, roles: 0, groups: [] })
    consoleSpy.mockRestore()
  })

  it('handles missing fields in data with defaults', async () => {
    const apiClient = createMockApiClient({ code: 0, data: {} })

    const perms = await fetchPermissions(apiClient)

    expect(perms).toEqual({ isAdmin: false, roles: 0, groups: [] })
  })
})
