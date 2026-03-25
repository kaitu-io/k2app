import { describe, it, expect, vi } from 'vitest'
import type { Config } from './config.ts'

// Mock fetchPermissions to return all groups
vi.mock('./tool-factory.ts', async (importOriginal) => {
  const original = await importOriginal() as Record<string, unknown>
  return {
    ...original,
    fetchPermissions: vi.fn().mockResolvedValue({
      isAdmin: true,
      roles: 0,
      groups: [
        'nodes', 'nodes.write', 'tunnels', 'tunnels.write',
        'cloud', 'cloud.write', 'users', 'users.write',
        'orders', 'campaigns', 'campaigns.write',
        'license_keys', 'license_keys.write',
        'plans', 'plans.write', 'stats',
        'device_logs', 'feedback_tickets', 'feedback_tickets.write',
        'retailers', 'retailers.write', 'edm',
        'approvals', 'approvals.write',
        'wallet', 'wallet.write',
        'strategy', 'strategy.write', 'surveys', 'admins',
      ],
    }),
  }
})

const testConfig: Config = {
  center: { url: 'https://api.example.com', accessKey: 'test-key' },
  ssh: { privateKeyPath: '/home/user/.ssh/id_rsa', user: 'root', port: 22 },
}

describe('createServer', () => {
  it('returns McpServer with correct version', async () => {
    const { createServer } = await import('./index.ts')
    const server = await createServer(testConfig)
    const innerServer = (server as any)['server'] as Record<string, unknown>
    const info = innerServer['_serverInfo'] as Record<string, unknown>
    expect(info['name']).toBe('kaitu-center')
    expect(info['version']).toBe('0.4.0')
  })

  it('registers standalone and factory tools', async () => {
    const { createServer } = await import('./index.ts')
    const server = await createServer(testConfig)
    const registeredTools = (server as any)['_registeredTools'] as Record<string, unknown>

    // Standalone
    expect(Object.keys(registeredTools)).toContain('list_nodes')
    expect(Object.keys(registeredTools)).toContain('exec_on_node')

    // Factory (spot check)
    expect(Object.keys(registeredTools)).toContain('list_orders')
    expect(Object.keys(registeredTools)).toContain('list_campaigns')
    expect(Object.keys(registeredTools)).toContain('lookup_user')

    // Should have 50+ tools
    expect(Object.keys(registeredTools).length).toBeGreaterThan(50)
  })
})
