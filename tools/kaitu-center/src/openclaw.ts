/**
 * kaitu-center — OpenClaw plugin entry point.
 *
 * TODO: Migrate to use tool-factory.ts and fetchPermissions() once
 * OpenClaw supports async registration.
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

    api.log?.warn?.('[kaitu-center] OpenClaw integration pending migration to tool-factory')
  },
}
