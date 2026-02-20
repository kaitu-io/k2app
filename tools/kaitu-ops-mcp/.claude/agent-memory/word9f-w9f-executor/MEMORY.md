# kaitu-ops-mcp Agent Memory

## Project Structure
- `/tools/kaitu-ops-mcp/src/` — TypeScript source files
- `/tools/kaitu-ops-mcp/src/tools/` — MCP tool implementations (list-nodes.ts, etc.)
- `src/config.ts` — loadConfig(), Config, CenterConfig, SshConfig
- `src/center-api.ts` — CenterApiClient with request(path, options?) → Promise<unknown>
- `src/index.ts` — re-exports config + CenterApiClient

## Key Patterns

### MCP Tool Registration
```ts
import { z } from 'zod'
import type { McpServer } from '@modelcontextprotocol/sdk/server/mcp.js'

export function registerListNodes(server: McpServer, apiClient: CenterApiClient): void {
  server.tool('tool_name', 'description', { param: z.string().optional() }, async (params) => {
    return { content: [{ type: 'text', text: JSON.stringify(result) }] }
  })
}
```

### Pure function + wrapper pattern
Extract business logic into `filterNodes(rawData, filters)` pure function for testability.
The MCP `registerXxx` wrapper calls the pure function — test the pure function directly.

### Type guard for unknown API responses
Use `isBatchMatrixResponse(value: unknown): value is T` guard before accessing nested fields.

### NodeNext ESM imports
Tests import `.js` extension: `import { foo } from './list-nodes.js'`
vitest.config.ts aliases `.js` → `.ts` at runtime.

## Pre-existing Issues (not to fix)
- `src/index.ts` has `.ts` extension errors in `tsc --noEmit` (pre-T1)
- `src/ssh.test.ts` errors because `ssh.ts` not yet implemented (another task)
- `npm run typecheck` fails due to pre-existing issues — not T3's fault

## Test Patterns
- Use `Object.keys(node).toEqual([...])` to assert exact field set (no extra fields)
- Use `expect(node).not.toHaveProperty('sensitive_field')` for stripping verification
- vitest `describe`/`it` with snake_case test names matching task spec

## McpServer Internal Structure (SDK v1.0.0)
- `server._registeredTools` is a plain object (NOT a Map)
- Registered tool's callable field is `.handler(params)` (NOT `.callback`)
- Invoke in tests: `server._registeredTools['tool_name'].handler(params)`

## vi.mock Hoisting Pitfall
- `vi.mock('module')` is hoisted before variable declarations
- Cannot reference top-level `const mockFn = vi.fn()` inside factory
- Pattern: use `vi.fn()` directly in factory, then import mocked module:
  ```ts
  vi.mock('../ssh.js', () => ({ sshExec: vi.fn(), sshExecWithStdin: vi.fn() }))
  import * as sshModule from '../ssh.js'
  const mockSshExec = sshModule.sshExec as ReturnType<typeof vi.fn>
  ```

## SSH Mock Channel Pattern
- Create mock client in closure with `listeners: Record<string, cb[]>`
- `on()` and `once()` push to listeners array; expose `_triggerReady()` and `_triggerError()`
- Mock channels need: `on`, `write`, `end`, `close`, `destroy`, `stderr.on`
- Timeout: call `channel.destroy()` if available, else `channel.close()`; return exitCode -1
