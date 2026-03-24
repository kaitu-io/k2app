# kaitu-center Phase 1 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Rename `kaitu-ops-mcp` → `kaitu-center`, add role-based tool filtering, add OpenClaw plugin entry, and bring `kaitu-mail` into the repo.

**Architecture:** Single package with two entry points — `index.ts` (MCP server for Claude Code, stdio) and `openclaw.ts` (native OpenClaw plugin, in-process). Tool functions are pure logic shared by both entries. A `roles.ts` module maps role names to allowed tool lists. `kaitu-mail` is a separate OpenClaw-only plugin copied from the remote machine.

**Tech Stack:** TypeScript (NodeNext ESM), `@modelcontextprotocol/sdk`, `zod`, `ssh2`, `smol-toml`, vitest

**Spec:** `docs/superpowers/specs/2026-03-24-role-based-skills-design.md`

---

## File Structure

| Action | Path | Responsibility |
|--------|------|---------------|
| Rename dir | `tools/kaitu-ops-mcp/` → `tools/kaitu-center/` | Directory rename |
| Modify | `tools/kaitu-center/package.json` | Update name, add `openclaw.extensions` |
| Modify | `tools/kaitu-center/src/index.ts` | Add role-based filtering to tool registration |
| Create | `tools/kaitu-center/src/roles.ts` | Role → tool-name mapping |
| Create | `tools/kaitu-center/src/roles.test.ts` | Tests for role mapping |
| Create | `tools/kaitu-center/src/openclaw.ts` | OpenClaw plugin entry |
| Create | `tools/kaitu-center/openclaw.plugin.json` | OpenClaw manifest |
| Modify | `tools/kaitu-center/src/config.ts` | Rename comments/paths from kaitu-ops → kaitu-center |
| Modify | `tools/kaitu-center/src/audit.ts` | Update default path from `.kaitu-ops` → `.kaitu-center` |
| Modify | `tools/kaitu-center/src/audit.test.ts` | Update path assertion |
| Modify | `tools/kaitu-center/src/index.test.ts` | Update server name assertion |
| Create | `tools/kaitu-mail/` | Copy from remote, entire directory |
| Modify | `CLAUDE.md` | Update all `kaitu-ops-mcp` references |
| Modify | `.claude/skills/kaitu-node-ops/SKILL.md` | Update description reference |
| Modify | `.claude/settings.local.json` | Update `enabledMcpjsonServers` |

---

### Task 1: Rename directory and update package.json

**Files:**
- Rename: `tools/kaitu-ops-mcp/` → `tools/kaitu-center/`
- Modify: `tools/kaitu-center/package.json`

- [ ] **Step 1: Rename the directory**

```bash
cd /Users/david/projects/kaitu-io/k2app
mv tools/kaitu-ops-mcp tools/kaitu-center
```

- [ ] **Step 2: Update package.json**

In `tools/kaitu-center/package.json`, change:
- `"name": "kaitu-ops-mcp"` → `"name": "@kaitu/center-plugin"`
- Add `"openclaw"` field with extensions entry

```json
{
  "name": "@kaitu/center-plugin",
  "version": "0.2.0",
  "description": "Kaitu Center API tools — MCP server (Claude Code) + OpenClaw plugin",
  "type": "module",
  "main": "dist/index.js",
  "bin": "dist/index.js",
  "openclaw": {
    "extensions": ["./src/openclaw.ts"]
  },
  "scripts": {
    "build": "tsc",
    "dev": "tsc --watch",
    "test": "vitest run",
    "test:watch": "vitest",
    "typecheck": "tsc --noEmit"
  },
  "dependencies": {
    "@modelcontextprotocol/sdk": "^1.0.0",
    "smol-toml": "^1.3.1",
    "ssh2": "^1.16.0"
  },
  "devDependencies": {
    "@types/node": "^22.0.0",
    "@types/ssh2": "^1.15.1",
    "typescript": "^5.6.0",
    "vitest": "^2.1.8"
  },
  "engines": {
    "node": ">=22.0.0"
  }
}
```

- [ ] **Step 3: Regenerate package-lock.json**

```bash
cd tools/kaitu-center && npm install
```

- [ ] **Step 4: Verify build**

```bash
cd tools/kaitu-center && npm run build
```
Expected: compiles with no errors.

- [ ] **Step 5: Commit**

```bash
git add tools/
git commit -m "refactor: rename kaitu-ops-mcp → kaitu-center"
```

Note: `git add tools/` stages both the new directory and the deletion of the old one. Git detects the rename automatically.

---

### Task 2: Update internal references (config, audit, index, tests)

**Files:**
- Modify: `tools/kaitu-center/src/index.ts`
- Modify: `tools/kaitu-center/src/config.ts`
- Modify: `tools/kaitu-center/src/audit.ts`
- Modify: `tools/kaitu-center/src/audit.test.ts`
- Modify: `tools/kaitu-center/src/index.test.ts`

- [ ] **Step 1: Update index.ts server name and error message**

In `tools/kaitu-center/src/index.ts`:
- Line 35: `name: 'kaitu-ops'` → `name: 'kaitu-center'`
- Line 71: `'Failed to start kaitu-ops MCP server:'` → `'Failed to start kaitu-center MCP server:'`
- Line 2 comment: `kaitu-ops-mcp` → `kaitu-center`

- [ ] **Step 2: Update config.ts comments and default path**

In `tools/kaitu-center/src/config.ts`:
- Line 29 comment: `kaitu-ops-mcp` → `kaitu-center`
- Line 101 comment: `~/.kaitu-ops/config.toml` → `~/.kaitu-center/config.toml`
- Line 107: `path.join(os.homedir(), '.kaitu-ops', 'config.toml')` → `path.join(os.homedir(), '.kaitu-center', 'config.toml')`

- [ ] **Step 3: Update audit.ts default path**

In `tools/kaitu-center/src/audit.ts`:
- Comment and const: `.kaitu-ops` → `.kaitu-center`

```typescript
/** Default audit log path: ~/.kaitu-center/audit.log */
const LOG_DIR = path.join(os.homedir(), '.kaitu-center')
```

- [ ] **Step 4: Update audit.test.ts assertion**

In `tools/kaitu-center/src/audit.test.ts`:
- Change: `expect(dirPath).toContain('.kaitu-ops')` → `expect(dirPath).toContain('.kaitu-center')`

- [ ] **Step 5: Update index.test.ts assertions**

In `tools/kaitu-center/src/index.test.ts`:
- Change: `expect(info['name']).toBe('kaitu-ops')` → `expect(info['name']).toBe('kaitu-center')`
- Change: `expect(info['version']).toBe('0.2.0')` → `expect(info['version']).toBe('0.3.0')` (if version assertion exists)

- [ ] **Step 6: Rename local config directory (if exists)**

```bash
[ -d ~/.kaitu-ops ] && mv ~/.kaitu-ops ~/.kaitu-center
```

This ensures the existing `config.toml` and `audit.log` are found at the new default path.

- [ ] **Step 7: Run tests**

```bash
cd tools/kaitu-center && npm test
```
Expected: all tests pass.

- [ ] **Step 8: Commit**

```bash
git add tools/kaitu-center/src
git commit -m "refactor: update internal references from kaitu-ops to kaitu-center"
```

---

### Task 3: Create roles.ts with role-based tool filtering

**Files:**
- Create: `tools/kaitu-center/src/roles.ts`
- Create: `tools/kaitu-center/src/roles.test.ts`

- [ ] **Step 1: Write the failing test**

Create `tools/kaitu-center/src/roles.test.ts`:

```typescript
import { describe, it, expect } from 'vitest'
import { getToolsForRole, TOOL_ROLES } from './roles.js'

describe('TOOL_ROLES', () => {
  it('devops role includes node ops tools', () => {
    expect(TOOL_ROLES.devops).toContain('list_nodes')
    expect(TOOL_ROLES.devops).toContain('exec_on_node')
    expect(TOOL_ROLES.devops).toContain('ping_node')
    expect(TOOL_ROLES.devops).toContain('delete_node')
  })

  it('devops role includes shared log/ticket tools', () => {
    expect(TOOL_ROLES.devops).toContain('query_device_logs')
    expect(TOOL_ROLES.devops).toContain('download_device_log')
    expect(TOOL_ROLES.devops).toContain('query_feedback_tickets')
    expect(TOOL_ROLES.devops).toContain('resolve_feedback_ticket')
  })

  it('support role includes user lookup and ticket tools', () => {
    expect(TOOL_ROLES.support).toContain('lookup_user')
    expect(TOOL_ROLES.support).toContain('list_user_devices')
    expect(TOOL_ROLES.support).toContain('query_device_logs')
    expect(TOOL_ROLES.support).toContain('close_feedback_ticket')
  })

  it('support role does NOT include node ops', () => {
    expect(TOOL_ROLES.support).not.toContain('list_nodes')
    expect(TOOL_ROLES.support).not.toContain('exec_on_node')
  })

  it('marketing role includes retailer and EDM tools', () => {
    expect(TOOL_ROLES.marketing).toContain('list_retailers')
    expect(TOOL_ROLES.marketing).toContain('create_edm_task')
    expect(TOOL_ROLES.marketing).toContain('lookup_user')
  })

  it('marketing role does NOT include node ops or tickets', () => {
    expect(TOOL_ROLES.marketing).not.toContain('exec_on_node')
    expect(TOOL_ROLES.marketing).not.toContain('query_feedback_tickets')
  })
})

describe('getToolsForRole', () => {
  it('returns devops tools for unknown role', () => {
    expect(getToolsForRole('unknown')).toEqual(TOOL_ROLES.devops)
  })

  it('returns devops tools when role is undefined', () => {
    expect(getToolsForRole(undefined as unknown as string)).toEqual(TOOL_ROLES.devops)
  })

  it('returns correct tools for each known role', () => {
    expect(getToolsForRole('devops')).toEqual(TOOL_ROLES.devops)
    expect(getToolsForRole('support')).toEqual(TOOL_ROLES.support)
    expect(getToolsForRole('marketing')).toEqual(TOOL_ROLES.marketing)
  })
})
```

- [ ] **Step 2: Run test to verify it fails**

```bash
cd tools/kaitu-center && npx vitest run src/roles.test.ts
```
Expected: FAIL — module `./roles.js` not found.

- [ ] **Step 3: Write roles.ts**

Create `tools/kaitu-center/src/roles.ts`:

```typescript
/**
 * Role-based tool access control.
 *
 * Maps role names to lists of tool names that the role is allowed to use.
 * Used by both MCP entry (index.ts, via KAITU_ROLE env) and
 * OpenClaw entry (openclaw.ts, via pluginConfig.role).
 */

export const TOOL_ROLES: Record<string, string[]> = {
  devops: [
    'list_nodes', 'exec_on_node', 'ping_node', 'delete_node',
    'query_device_logs', 'download_device_log',
    'query_feedback_tickets', 'resolve_feedback_ticket',
  ],
  support: [
    'lookup_user', 'list_user_devices',
    'query_device_logs', 'download_device_log',
    'query_feedback_tickets', 'resolve_feedback_ticket',
    'close_feedback_ticket',
  ],
  marketing: [
    'lookup_user',
    'list_retailers', 'get_retailer_detail', 'update_retailer_level',
    'create_retailer_note', 'list_retailer_todos',
    'list_edm_templates', 'create_edm_task',
    'preview_edm_targets', 'get_edm_send_stats',
  ],
}

/**
 * Returns the list of allowed tool names for the given role.
 * Defaults to 'devops' for unknown or missing roles.
 */
export function getToolsForRole(role: string): string[] {
  return TOOL_ROLES[role] ?? TOOL_ROLES.devops
}
```

- [ ] **Step 4: Run test to verify it passes**

```bash
cd tools/kaitu-center && npx vitest run src/roles.test.ts
```
Expected: all tests PASS.

- [ ] **Step 5: Commit**

```bash
git add tools/kaitu-center/src/roles.ts tools/kaitu-center/src/roles.test.ts
git commit -m "feat: add role-based tool filtering (roles.ts)"
```

---

### Task 4: Add role filtering to MCP index.ts

**Files:**
- Modify: `tools/kaitu-center/src/index.ts`

- [ ] **Step 1: Update index.ts to use role filtering**

Add import at top:
```typescript
import { getToolsForRole } from './roles.js'
```

Replace the tool registration block in `createServer()` with:

```typescript
export async function createServer(config: Config): Promise<McpServer> {
  const apiClient = new CenterApiClient(config)
  const role = process.env['KAITU_ROLE'] || 'devops'
  const allowed = new Set(getToolsForRole(role))

  const server = new McpServer({
    name: 'kaitu-center',
    version: '0.3.0',
  })

  // DevOps tools
  if (allowed.has('list_nodes'))              registerListNodes(server, apiClient)
  if (allowed.has('exec_on_node'))            registerExecOnNode(server, config.ssh)
  if (allowed.has('ping_node'))               registerPingNode(server, config.ssh)
  if (allowed.has('delete_node'))             registerDeleteNode(server, apiClient)

  // Shared tools (DevOps + Support)
  if (allowed.has('query_device_logs'))       registerQueryDeviceLogs(server, apiClient)
  if (allowed.has('download_device_log'))     registerDownloadDeviceLog(server)
  if (allowed.has('query_feedback_tickets'))  registerQueryFeedbackTickets(server, apiClient)
  if (allowed.has('resolve_feedback_ticket')) registerResolveFeedbackTicket(server, apiClient)

  // Support-only and Marketing-only tools will be added in Phase 2/3

  return server
}
```

- [ ] **Step 2: Run all tests**

```bash
cd tools/kaitu-center && npm test
```
Expected: all tests pass. The existing `index.test.ts` verifies the server is created correctly. Since `KAITU_ROLE` is unset in tests, it defaults to `devops` and registers all 8 existing tools — same as before.

- [ ] **Step 3: Verify build**

```bash
cd tools/kaitu-center && npm run build
```

- [ ] **Step 4: Commit**

```bash
git add tools/kaitu-center/src/index.ts
git commit -m "feat: add KAITU_ROLE env filtering to MCP tool registration"
```

---

### Task 5: Create OpenClaw plugin entry

**Files:**
- Create: `tools/kaitu-center/openclaw.plugin.json`
- Create: `tools/kaitu-center/src/openclaw.ts`

- [ ] **Step 1: Create openclaw.plugin.json**

Create `tools/kaitu-center/openclaw.plugin.json`:

```json
{
  "id": "kaitu-center",
  "name": "Kaitu Center",
  "description": "DevOps, Support, and Marketing tools for Kaitu Center API",
  "version": "1.0.0",
  "configSchema": {
    "type": "object",
    "properties": {
      "centerUrl": {
        "type": "string",
        "description": "Center API base URL (e.g. https://api.kaitu.io)"
      },
      "accessKey": {
        "type": "string",
        "description": "X-Access-Key for Center API authentication"
      },
      "role": {
        "type": "string",
        "enum": ["devops", "support", "marketing"],
        "description": "Which tool set to expose to this agent"
      },
      "sshKeyPath": {
        "type": "string",
        "description": "SSH private key path (devops role only)"
      },
      "sshUser": {
        "type": "string",
        "description": "SSH username (default: ubuntu)"
      },
      "sshPort": {
        "type": "number",
        "description": "SSH port (default: 1022)"
      }
    },
    "required": ["centerUrl", "accessKey", "role"],
    "additionalProperties": false
  }
}
```

- [ ] **Step 2: Create openclaw.ts**

Create `tools/kaitu-center/src/openclaw.ts`:

```typescript
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
```

- [ ] **Step 3: Exclude openclaw.ts from tsconfig (it's loaded by jiti, not tsc)**

No change needed — `openclaw.ts` can stay in `src/` and compile to `dist/openclaw.js`. It imports the same modules and types. tsc compiles it fine.

- [ ] **Step 4: Build and verify**

```bash
cd tools/kaitu-center && npm run build
```
Expected: compiles with no errors, `dist/openclaw.js` is generated.

- [ ] **Step 5: Commit**

```bash
git add tools/kaitu-center/openclaw.plugin.json tools/kaitu-center/src/openclaw.ts
git commit -m "feat: add OpenClaw plugin entry and manifest"
```

---

### Task 6: Copy kaitu-mail into repo

**Files:**
- Create: `tools/kaitu-mail/` (3 files from remote)

- [ ] **Step 1: Copy from remote machine**

```bash
cd /Users/david/projects/kaitu-io/k2app
scp -r ldavid@192.168.31.40:/Users/ldavid/projects/kaitu-mail-plugin tools/kaitu-mail
```

- [ ] **Step 2: Verify files exist**

```bash
ls tools/kaitu-mail/
```
Expected: `index.js`, `package.json`, `openclaw.plugin.json`

- [ ] **Step 3: Commit**

```bash
git add tools/kaitu-mail
git commit -m "feat: add kaitu-mail OpenClaw plugin (email tools via himalaya)"
```

---

### Task 7: Update all external references

**Files:**
- Modify: `CLAUDE.md`
- Modify: `.claude/skills/kaitu-node-ops/SKILL.md`
- Modify: `.claude/settings.local.json`

- [ ] **Step 1: Update CLAUDE.md**

Replace all occurrences of `kaitu-ops-mcp` with `kaitu-center` in `CLAUDE.md`:
- Line 29: `cd tools/kaitu-ops-mcp && npm run build` → `cd tools/kaitu-center && npm run build`
- Line 30: `cd tools/kaitu-ops-mcp && npm test` → `cd tools/kaitu-center && npm test`
- Line 99: `tools/kaitu-ops-mcp/` description → `tools/kaitu-center/`
- Line 127: NodeNext imports reference → update path
- Line 157: Package section → update path

Also add `tools/kaitu-mail/` to the project structure section.

- [ ] **Step 2: Update kaitu-node-ops SKILL.md**

In `.claude/skills/kaitu-node-ops/SKILL.md` line 3:
- `description: Node infrastructure operations via kaitu-ops-mcp tools.` → `description: Node infrastructure operations via kaitu-center tools.`

- [ ] **Step 3: Update .claude/settings.local.json**

```json
{
  "enabledMcpjsonServers": [
    "kaitu-center"
  ],
  "enableAllProjectMcpServers": true
}
```

Also update the MCP server definition. Run in Claude Code:
```
/mcp
```
Find the `kaitu-ops` server entry, update:
- Name: `kaitu-ops` → `kaitu-center`
- Command args: update path from `tools/kaitu-ops-mcp/dist/index.js` → `tools/kaitu-center/dist/index.js`

If the MCP server is defined in a JSON file outside the repo, update it there. The key change is the directory path in the `args` array.

- [ ] **Step 4: Run full test suite**

```bash
cd tools/kaitu-center && npm test
```
Expected: all tests pass.

- [ ] **Step 5: Build to verify no broken imports**

```bash
cd tools/kaitu-center && npm run build
```

- [ ] **Step 6: Commit**

```bash
git add CLAUDE.md .claude/skills/kaitu-node-ops/SKILL.md .claude/settings.local.json
git commit -m "refactor: update all kaitu-ops-mcp references to kaitu-center"
```

---

### Task 8: Final verification

- [ ] **Step 1: Full build**

```bash
cd tools/kaitu-center && npm run build && npm test
```

- [ ] **Step 2: Verify no stale references**

```bash
grep -rn "kaitu-ops-mcp" --include="*.ts" --include="*.json" --include="*.md" . | grep -v node_modules | grep -v dist | grep -v docs/superpowers
```
Expected: no matches (except possibly in old plan/spec docs which is fine).

- [ ] **Step 3: Verify kaitu-mail is in place**

```bash
ls tools/kaitu-mail/index.js tools/kaitu-mail/package.json tools/kaitu-mail/openclaw.plugin.json
```

- [ ] **Step 4: Commit any remaining changes**

If any files were missed, stage and commit:
```bash
git add -A && git status
```
