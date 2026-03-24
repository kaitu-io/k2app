import * as fs from 'node:fs'
import * as os from 'node:os'
import * as path from 'node:path'
import { parse as parseToml } from 'smol-toml'

/**
 * Configuration for the Center API connection.
 */
export interface CenterConfig {
  /** Base URL of the Center API service (e.g. https://api.kaitu.io) */
  url: string
  /** Access key used for X-Access-Key authentication header */
  accessKey: string
}

/**
 * Configuration for SSH connections to nodes.
 */
export interface SshConfig {
  /** Absolute path to the SSH private key file */
  privateKeyPath: string
  /** SSH username (typically "root") */
  user: string
  /** SSH port number (default 22) */
  port: number
}

/**
 * Complete configuration for kaitu-ops-mcp.
 */
export interface Config {
  center: CenterConfig
  ssh: SshConfig
}

/**
 * Raw TOML structure as parsed from disk.
 */
interface RawToml {
  center?: {
    url?: string
    access_key?: string
  }
  ssh?: {
    private_key_path?: string
    user?: string
    port?: number | string
  }
}

/**
 * Default paths to search for SSH private keys, in resolution order.
 * Higher-index paths are lower priority.
 */
const DEFAULT_SSH_KEY_PATHS = [
  path.join(os.homedir(), '.ssh', 'id_rsa'),
  path.join(os.homedir(), '.ssh', 'id_ed25519'),
]

/**
 * Resolves the SSH private key path using the following priority order:
 * 1. KAITU_SSH_KEY environment variable
 * 2. Config file private_key_path value
 * 3. First existing default path (~/.ssh/id_rsa → ~/.ssh/id_ed25519)
 */
function resolveSshKeyPath(
  envKeyPath: string | undefined,
  tomlKeyPath: string | undefined
): string | undefined {
  // Env var has highest priority
  if (envKeyPath) {
    return envKeyPath
  }
  // Config file value next
  if (tomlKeyPath) {
    return tomlKeyPath
  }
  // Fall back to default paths — return first that exists
  for (const defaultPath of DEFAULT_SSH_KEY_PATHS) {
    if (fs.existsSync(defaultPath)) {
      return defaultPath
    }
  }
  // Return first default path even if it doesn't exist (will cause clearer error downstream)
  return DEFAULT_SSH_KEY_PATHS[0]
}

/**
 * Loads configuration from a TOML file + environment variable overrides.
 *
 * Resolution order (highest priority first):
 * - KAITU_CENTER_URL, KAITU_ACCESS_KEY, KAITU_SSH_KEY, KAITU_SSH_USER, KAITU_SSH_PORT env vars
 * - Values from the TOML config file at configPath
 *
 * SSH key resolution order:
 * - KAITU_SSH_KEY env var
 * - config file private_key_path
 * - ~/.ssh/id_rsa (if exists)
 * - ~/.ssh/id_ed25519 (if exists)
 *
 * @param configPath - Path to the TOML config file. Defaults to ~/.kaitu-ops/config.toml.
 *   If the file does not exist, env vars must supply all required values.
 * @throws {Error} If required fields are missing from both TOML and env vars,
 *   with a clear message listing each missing field.
 */
export async function loadConfig(
  configPath: string = path.join(os.homedir(), '.kaitu-ops', 'config.toml')
): Promise<Config> {
  // Attempt to parse TOML file — silently skip if file doesn't exist
  let toml: RawToml = {}
  try {
    const content = fs.readFileSync(configPath, 'utf-8')
    toml = parseToml(content) as RawToml
  } catch (err) {
    // File not found is acceptable — env vars can supply all values
    if ((err as NodeJS.ErrnoException).code !== 'ENOENT') {
      throw new Error(
        `Failed to parse config file at ${configPath}: ${(err as Error).message}`
      )
    }
  }

  // Collect resolved values with env var priority over TOML
  const centerUrl = process.env['KAITU_CENTER_URL'] ?? toml.center?.url
  const accessKey = process.env['KAITU_ACCESS_KEY'] ?? toml.center?.access_key
  const sshUser = process.env['KAITU_SSH_USER'] ?? toml.ssh?.user ?? 'ubuntu'
  const sshPortRaw = process.env['KAITU_SSH_PORT'] ?? toml.ssh?.port ?? 1022
  const sshKeyPath = resolveSshKeyPath(
    process.env['KAITU_SSH_KEY'],
    toml.ssh?.private_key_path
  )

  // Parse SSH port — must be a valid integer
  const sshPort = sshPortRaw !== undefined ? parseInt(String(sshPortRaw), 10) : undefined

  // Collect missing required fields for a helpful error message
  const missing: string[] = []
  if (!centerUrl) {
    missing.push('center.url (or KAITU_CENTER_URL env var)')
  }
  if (!accessKey) {
    missing.push('center.access_key (or KAITU_ACCESS_KEY env var)')
  }
  if (!sshUser) {
    missing.push('ssh.user (or KAITU_SSH_USER env var)')
  }
  if (sshPort === undefined || isNaN(sshPort)) {
    missing.push('ssh.port (or KAITU_SSH_PORT env var)')
  }
  if (!sshKeyPath) {
    missing.push('ssh.private_key_path (or KAITU_SSH_KEY env var, or ~/.ssh/id_rsa / ~/.ssh/id_ed25519)')
  }

  if (missing.length > 0) {
    throw new Error(
      `Configuration missing required fields:\n${missing.map(f => `  - ${f}`).join('\n')}`
    )
  }

  return {
    center: {
      url: centerUrl as string,
      accessKey: accessKey as string,
    },
    ssh: {
      privateKeyPath: sshKeyPath as string,
      user: sshUser as string,
      port: sshPort as number,
    },
  }
}
