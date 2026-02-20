import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest'
import * as fs from 'node:fs'
import * as os from 'node:os'
import * as path from 'node:path'
import { loadConfig } from './config.ts'

// Helper: create a temp TOML file with given content
function writeTempToml(content: string): string {
  const dir = fs.mkdtempSync(path.join(os.tmpdir(), 'kaitu-ops-test-'))
  const filePath = path.join(dir, 'config.toml')
  fs.writeFileSync(filePath, content)
  return filePath
}

const FULL_TOML = `
[center]
url = "https://api.example.com"
access_key = "test-access-key"

[ssh]
private_key_path = "/home/user/.ssh/id_rsa"
user = "admin"
port = 2222
`

describe('loadConfig', () => {
  let savedEnv: NodeJS.ProcessEnv

  beforeEach(() => {
    // snapshot and clear relevant env vars before each test
    savedEnv = { ...process.env }
    delete process.env['KAITU_CENTER_URL']
    delete process.env['KAITU_ACCESS_KEY']
    delete process.env['KAITU_SSH_KEY']
    delete process.env['KAITU_SSH_USER']
    delete process.env['KAITU_SSH_PORT']
  })

  afterEach(() => {
    // restore env
    for (const key of ['KAITU_CENTER_URL', 'KAITU_ACCESS_KEY', 'KAITU_SSH_KEY', 'KAITU_SSH_USER', 'KAITU_SSH_PORT']) {
      if (savedEnv[key] !== undefined) {
        process.env[key] = savedEnv[key]
      } else {
        delete process.env[key]
      }
    }
  })

  it('test_config_from_toml_file — loads all fields from TOML', async () => {
    const tomlPath = writeTempToml(FULL_TOML)
    const config = await loadConfig(tomlPath)

    expect(config.center.url).toBe('https://api.example.com')
    expect(config.center.accessKey).toBe('test-access-key')
    expect(config.ssh.privateKeyPath).toBe('/home/user/.ssh/id_rsa')
    expect(config.ssh.user).toBe('admin')
    expect(config.ssh.port).toBe(2222)
  })

  it('test_config_env_overrides_toml — env vars override TOML values', async () => {
    const tomlPath = writeTempToml(FULL_TOML)

    process.env['KAITU_CENTER_URL'] = 'https://override.example.com'
    process.env['KAITU_ACCESS_KEY'] = 'override-key'
    process.env['KAITU_SSH_KEY'] = '/override/.ssh/id_ed25519'
    process.env['KAITU_SSH_USER'] = 'overrideuser'
    process.env['KAITU_SSH_PORT'] = '9022'

    const config = await loadConfig(tomlPath)

    expect(config.center.url).toBe('https://override.example.com')
    expect(config.center.accessKey).toBe('override-key')
    expect(config.ssh.privateKeyPath).toBe('/override/.ssh/id_ed25519')
    expect(config.ssh.user).toBe('overrideuser')
    expect(config.ssh.port).toBe(9022)
  })

  it('test_config_env_only — works without TOML file if all env vars set', async () => {
    process.env['KAITU_CENTER_URL'] = 'https://env.example.com'
    process.env['KAITU_ACCESS_KEY'] = 'env-access-key'
    process.env['KAITU_SSH_KEY'] = '/env/.ssh/id_rsa'
    process.env['KAITU_SSH_USER'] = 'envuser'
    process.env['KAITU_SSH_PORT'] = '22'

    // pass a path to a non-existent file — should still work via env vars
    const config = await loadConfig('/nonexistent/path/config.toml')

    expect(config.center.url).toBe('https://env.example.com')
    expect(config.center.accessKey).toBe('env-access-key')
    expect(config.ssh.privateKeyPath).toBe('/env/.ssh/id_rsa')
    expect(config.ssh.user).toBe('envuser')
    expect(config.ssh.port).toBe(22)
  })

  it('test_config_missing_error — no TOML + no env → clear error listing missing fields', async () => {
    await expect(loadConfig('/nonexistent/path/config.toml')).rejects.toThrow(/missing/)
    await expect(loadConfig('/nonexistent/path/config.toml')).rejects.toThrow(/center\.url|KAITU_CENTER_URL/)
  })

  it('test_config_partial_missing — TOML has center but no ssh → error lists missing ssh fields', async () => {
    const partialToml = `
[center]
url = "https://api.example.com"
access_key = "test-access-key"
`
    const tomlPath = writeTempToml(partialToml)

    await expect(loadConfig(tomlPath)).rejects.toThrow(/missing/)
    await expect(loadConfig(tomlPath)).rejects.toThrow(/ssh/)
  })

  it('test_ssh_key_resolution_order — resolves ssh key from default paths and env', async () => {
    // This test verifies that when KAITU_SSH_KEY env var is set,
    // it is used as the private key path (higher priority than default paths)
    const tomlPath = writeTempToml(`
[center]
url = "https://api.example.com"
access_key = "test-access-key"

[ssh]
user = "root"
port = 22
`)

    process.env['KAITU_SSH_KEY'] = '/custom/.ssh/id_ed25519'

    const config = await loadConfig(tomlPath)
    expect(config.ssh.privateKeyPath).toBe('/custom/.ssh/id_ed25519')
  })
})
