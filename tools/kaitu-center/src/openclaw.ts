/**
 * kaitu-center — OpenClaw plugin entry point.
 *
 * Registers Center API tools scoped by role (devops/support/marketing).
 * Reads configuration from OpenClaw's pluginConfig (set in agent config).
 *
 * This file is referenced by package.json openclaw.extensions.
 * OpenClaw loads it in-process via jiti (TypeScript JIT).
 *
 * Phase 1: Config validation + role initialization only.
 * Phase 2/3: Tool registration using pure-function refactor of each tool.
 */

import { getToolsForRole } from './roles.js'

/**
 * OpenClaw plugin API interface (subset we use).
 * Kept minimal to avoid adding openclaw as a dependency.
 */
interface OpenClawPluginApi {
  pluginConfig: Record<string, unknown>
  registerTool(def: OpenClawToolDef, opts?: { optional?: boolean }): void
  log?: { warn?: (...args: unknown[]) => void }
}

interface OpenClawToolDef {
  name: string
  label?: string
  description: string
  parameters: Record<string, unknown>
  execute: (id: string, params: Record<string, unknown>) => Promise<{
    content: Array<{ type: string; text: string }>
    isError?: boolean
  }>
}

export default {
  id: 'kaitu-center',
  name: 'Kaitu Center',

  register(api: OpenClawPluginApi) {
    const cfg = api.pluginConfig
    if (!cfg?.centerUrl || !cfg?.accessKey) {
      api.log?.warn?.('[kaitu-center] Missing centerUrl or accessKey in config')
      return
    }

    const role = (cfg.role as string) || 'devops'
    const allowed = new Set(getToolsForRole(role))

    api.log?.warn?.(`[kaitu-center] Initialized with role=${role}, ${allowed.size} tools available`)

    // TODO(phase2): Register support tools with CenterApiClient
    // TODO(phase3): Register marketing tools with CenterApiClient
  },
}
