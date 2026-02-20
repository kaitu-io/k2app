/**
 * Integration tests for the MCP server entry point.
 *
 * Tests verify that createServer() correctly configures an McpServer with
 * the expected name/version and registers both tools without starting
 * the stdio transport (which would block).
 */
import { describe, it, expect } from 'vitest'
import type { Config } from './config.ts'
import { createServer } from './index.ts'

/**
 * Minimal valid config fixture for tests.
 * SSH and Center values are fake — tools are not exercised here.
 */
const testConfig: Config = {
  center: {
    url: 'https://api.example.com',
    accessKey: 'test-access-key',
  },
  ssh: {
    privateKeyPath: '/home/user/.ssh/id_rsa',
    user: 'root',
    port: 22,
  },
}

describe('createServer', () => {
  it('test_server_stdio_init — createServer returns an McpServer with correct name and version', async () => {
    const server = await createServer(testConfig)

    // McpServer wraps an internal Server instance at .server._serverInfo
    const innerServer = (server as unknown as Record<string, unknown>)['server'] as Record<
      string,
      unknown
    >
    const info = innerServer['_serverInfo'] as Record<string, unknown>
    expect(info).toBeDefined()
    expect(info['name']).toBe('kaitu-ops')
    expect(info['version']).toBe('0.1.0')
  })

  it('test_server_registers_two_tools — after createServer, both list_nodes and exec_on_node are registered', async () => {
    const server = await createServer(testConfig)

    // McpServer stores registered tools in _registeredTools (plain object, SDK v1.0.0)
    // eslint-disable-next-line @typescript-eslint/no-explicit-any
    const registeredTools = (server as unknown as Record<string, unknown>)[
      '_registeredTools'
    ] as Record<string, unknown>

    expect(registeredTools).toBeDefined()
    expect(Object.keys(registeredTools)).toContain('list_nodes')
    expect(Object.keys(registeredTools)).toContain('exec_on_node')
  })
})
