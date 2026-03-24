# Role-Based AI Agent Skills Design

## Goal

Create role-scoped tools and skills so OpenClaw and Claude Code can autonomously handle DevOps, Support, and Marketing workflows against the Kaitu Center API.

## Architecture Overview

Two plugins + one MCP server, three skill files.

```
tools/
  kaitu-center/                         # Center API tools (renamed from kaitu-ops-mcp)
    package.json                        # name: "@kaitu/center-plugin"
                                        # openclaw.extensions: ["./src/openclaw.js"]
    openclaw.plugin.json                # OpenClaw manifest + configSchema
    kaitu-center.toml                   # MCP config (Center API url/key + SSH)
    src/
      index.ts                          # MCP server entry (Claude Code, stdio)
      openclaw.ts                       # OpenClaw plugin entry (plain export default)
      center-api.ts                     # Shared Center API HTTP client
      config.ts                         # Config loader (TOML for MCP, pluginConfig for OpenClaw)
      roles.ts                          # Role → tool-name mapping
      audit.ts                          # Shared audit logger
      redact.ts                         # Shared PII redaction
      ssh.ts                            # SSH client (DevOps only)
      tools/
        # --- DevOps ---
        list-nodes.ts
        exec-on-node.ts
        ping-node.ts
        delete-node.ts
        # --- Shared (DevOps + Support) ---
        query-device-logs.ts            # GET /app/device-logs (S3 metadata)
        download-device-log.ts          # Download + extract from S3
        query-feedback-tickets.ts       # GET /app/feedback-tickets
        resolve-feedback-ticket.ts      # PUT /app/feedback-tickets/:id/resolve
        # --- Support ---
        lookup-user.ts                  # GET /app/users (email or uuid)
        list-user-devices.ts            # GET /app/users/:uuid/devices
        close-feedback-ticket.ts        # PUT /app/feedback-tickets/:id/close
        # --- Marketing ---
        list-retailers.ts               # GET /app/retailers
        get-retailer-detail.ts          # GET /app/retailers/:uuid
        update-retailer-level.ts        # PUT /app/retailers/:uuid/level
        create-retailer-note.ts         # POST /app/retailers/:uuid/notes
        list-retailer-todos.ts          # GET /app/retailers/todos
        list-edm-templates.ts           # GET /app/edm/templates
        create-edm-task.ts              # POST /app/edm/tasks
        preview-edm-targets.ts          # POST /app/edm/preview-targets
        get-edm-send-stats.ts           # GET /app/edm/send-logs/stats

  kaitu-mail/                           # Email tools (OpenClaw plugin only, uses himalaya CLI)
    package.json                        # openclaw.extensions: ["./index.js"]
    openclaw.plugin.json                # configSchema: { account: "support-kaitu" | "marketing-kaitu" }
    index.js                            # mail_list, mail_read, mail_send, mail_search, mail_folders, mail_move

.claude/skills/
  kaitu-node-ops/SKILL.md               # DevOps skill (existing, update references only)
  kaitu-support/SKILL.md                # Support skill (new)
  kaitu-marketing/SKILL.md              # Marketing skill (new)

tools/kaitu-center/skills/              # OpenClaw skill copies (OpenClaw reads from plugin dir)
  kaitu-support/
    SKILL.md
    prompts/
      diagnose.md                       # Claude Code diagnostic prompt template
  kaitu-marketing/
    SKILL.md
```

### Why Two Plugins

| Plugin | Purpose | Claude Code | OpenClaw | Dependencies |
|--------|---------|:-----------:|:--------:|-------------|
| `kaitu-center` | Center API operations | MCP (stdio) | Native plugin | center-api, ssh2 |
| `kaitu-mail` | Email read/send via himalaya | Not needed | Native plugin | himalaya CLI |

Separate because: different dependencies, different purpose (REST API vs IMAP), and `kaitu-mail` has no MCP use case (Claude Code doesn't need email access).

---

## Dual Entry Point — kaitu-center

Each tool is a pure async function. Two thin registration layers wrap the same logic:

```typescript
// tools/lookup-user.ts — pure business logic
export async function lookupUser(
  apiClient: CenterApiClient,
  params: { email?: string; uuid?: string }
) {
  if (params.uuid) return apiClient.request(`/app/users/${params.uuid}`)
  return apiClient.request(`/app/users?email=${params.email}`)
}
```

**MCP registration** (`index.ts` — Claude Code):

```typescript
import { z } from 'zod'
server.tool('lookup_user', 'Look up a user by email or UUID', {
  email: z.string().optional(),
  uuid: z.string().optional(),
}, async (params) => {
  const result = await lookupUser(apiClient, params)
  return { content: [{ type: 'text', text: JSON.stringify(result) }] }
})
```

**OpenClaw plugin registration** (`openclaw.ts` — following actual SDK pattern from kaitu-mail):

```javascript
export default {
  id: 'kaitu-center',
  name: 'Kaitu Center',
  register(api) {
    const cfg = api.pluginConfig
    const apiClient = new CenterApiClient({ url: cfg.centerUrl, accessKey: cfg.accessKey })
    const role = cfg.role || 'devops'
    const allowed = getToolsForRole(role)

    if (allowed.includes('lookup_user')) {
      api.registerTool({
        name: 'lookup_user',
        description: 'Look up a user by email or UUID',
        parameters: {
          type: 'object',
          properties: {
            email: { type: 'string' },
            uuid: { type: 'string' },
          },
        },
        async execute(_id, params) {
          const result = await lookupUser(apiClient, params)
          return { content: [{ type: 'text', text: JSON.stringify(result, null, 2) }] }
        },
      }, { optional: true })
    }
    // ... register other tools based on role
  },
}
```

## Role-Based Tool Filtering

`src/roles.ts`:

```typescript
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
```

**MCP** (`index.ts`): reads `KAITU_ROLE` env var. Default `devops` — backward compatible.
**OpenClaw** (`openclaw.ts`): reads `api.pluginConfig.role`. Each agent instance gets its own role.

## Plugin Manifests

### kaitu-center — `openclaw.plugin.json`

```json
{
  "id": "kaitu-center",
  "name": "Kaitu Center",
  "description": "DevOps, Support, and Marketing tools for Kaitu Center API",
  "version": "1.0.0",
  "configSchema": {
    "type": "object",
    "properties": {
      "centerUrl": { "type": "string", "description": "Center API base URL" },
      "accessKey": { "type": "string", "description": "X-Access-Key for Center API auth" },
      "role": { "type": "string", "enum": ["devops", "support", "marketing"] },
      "sshKeyPath": { "type": "string", "description": "SSH private key path (devops only)" }
    },
    "required": ["centerUrl", "accessKey", "role"],
    "additionalProperties": false
  }
}
```

### kaitu-mail — `openclaw.plugin.json` (existing, moved into repo)

```json
{
  "id": "kaitu-mail",
  "name": "Kaitu Mail",
  "description": "Per-profile email tools via himalaya. Each profile binds its own mail account.",
  "configSchema": {
    "type": "object",
    "properties": {
      "account": { "type": "string", "description": "Himalaya account name (e.g. support-kaitu)" }
    },
    "required": [],
    "additionalProperties": false
  }
}
```

Account naming convention: `support-kaitu` → `support@kaitu.io`, `marketing-kaitu` → `marketing@kaitu.io`.

## Claude Code Configuration

`.claude/settings.json`:

```json
{
  "mcpServers": {
    "kaitu-center": {
      "command": "node",
      "args": ["tools/kaitu-center/dist/index.js"],
      "env": { "KAITU_ROLE": "devops" }
    }
  }
}
```

Claude Code only needs the MCP entry. Email tools are OpenClaw-only.

---

## Support Skill — Automated Ticket Triage

### Trigger

1. User submits feedback ticket in app
2. Center API sends notification email to `support@kaitu.io`
3. OpenClaw support agent runs `mail_list` → finds new email with ticket ID in subject

### Workflow

```
Step 1:  mail_search(query="feedback ticket") → find new ticket emails
         Extract ticket ID from email subject (format: "Feedback Ticket #123")
  ↓
Step 2:  query_feedback_tickets(id) → ticket metadata (user_uuid, udid, platform, description)
  ↓
Step 3:  lookup_user(uuid) → user profile, membership status
  ↓
Step 4:  list_user_devices(uuid) → device list with app versions
  ↓
Step 5:  query_device_logs(udid) → find log files in S3
  ↓
Step 6:  download_device_log(key) → extract to /tmp/kaitu-support/{ticket-id}/logs/
  ↓
Step 7:  Determine app version from log metadata → git tag v{version}
  ↓
Step 8:  git clone --depth=1 --branch v{version} <repo> /tmp/kaitu-support/{ticket-id}/codebase
  ↓
Step 9:  Spawn Claude Code for read-only diagnosis:
         claude --print \
           --tools "Read,Grep,Glob,Bash" \
           --permission-mode bypassPermissions \
           --max-budget-usd 2.00 \
           --prompt "$(render diagnose.md with ticket context variables)"
  ↓
Step 10: Collect structured diagnosis output (classification + root cause + confidence)
  ↓
Step 11: mail_send(to="david@kaitu.io", subject="[Support] Ticket #{id}: {classification}",
                   body=diagnosis_report)
  ↓
Step 12: resolve_feedback_ticket(id, resolved_by="openclaw-support")
  ↓
Step 13: rm -rf /tmp/kaitu-support/{ticket-id}/
```

### Tools Used Per Step

| Step | Plugin | Tool |
|------|--------|------|
| 1 | kaitu-mail | `mail_search` |
| 2 | kaitu-center | `query_feedback_tickets` |
| 3 | kaitu-center | `lookup_user` |
| 4 | kaitu-center | `list_user_devices` |
| 5 | kaitu-center | `query_device_logs` |
| 6 | kaitu-center | `download_device_log` |
| 7-8 | shell | `git clone` |
| 9 | shell | `claude --print` |
| 11 | kaitu-mail | `mail_send` |
| 12 | kaitu-center | `resolve_feedback_ticket` |
| 13 | shell | `rm -rf` |

### Diagnostic Prompt Template

`tools/kaitu-center/skills/kaitu-support/prompts/diagnose.md`:

```markdown
You are a technical diagnostician for the Kaitu VPN application.

## Context

- Feedback ticket ID: {{ticket_id}}
- User UUID: {{user_uuid}}
- Platform: {{platform}} (e.g. macOS, Windows, iOS, Android)
- App version: {{app_version}}
- User description: {{ticket_description}}

## Log files

Device logs are at: {{log_path}}

## Codebase

You are in the codebase checked out at tag v{{app_version}}.
Key directories:
- k2/engine/ — Go tunnel core (connection, reconnection, error handling)
- k2/daemon/ — Desktop daemon HTTP API
- webapp/src/ — React frontend
- desktop/src-tauri/ — Tauri desktop shell
- mobile/ — Capacitor mobile app

## Instructions

1. Read the device logs thoroughly. Identify errors, warnings, and anomalies.
2. Use the superpowers:systematic-debugging approach:
   - Gather evidence from logs (timestamps, error codes, stack traces)
   - Form hypotheses about root cause
   - Cross-reference with source code at the relevant version
   - Narrow down to the most likely cause
3. Aim for 10/10 confidence in your root cause determination.
4. DO NOT modify any code. This is a read-only diagnosis.
5. Classify the issue:
   - CLIENT_BUG — Bug in app code (specify file + line)
   - CLIENT_CONFIG — User configuration issue
   - SERVER_ISSUE — Server/node side problem (specify node if identifiable)
   - NETWORK — User's network environment issue
   - KNOWN_FIXED — Bug exists in this version but fixed in a later release
   - UNKNOWN — Cannot determine with available evidence

## Output Format

### Summary
One-line summary of the issue.

### Classification
CLIENT_BUG | CLIENT_CONFIG | SERVER_ISSUE | NETWORK | KNOWN_FIXED | UNKNOWN

### Root Cause
Detailed explanation with evidence from logs and code references.

### Confidence
N/10 with reasoning for the confidence level.

### Recommended Action
What should be done (fix PR reference if KNOWN_FIXED, config change if CLIENT_CONFIG, etc.)

### Evidence
Key log excerpts and code references that support the diagnosis.
```

### Escalation Rules

| Classification | Email Action |
|---------------|-------------|
| CLIENT_BUG | Full diagnosis + affected code path |
| CLIENT_CONFIG | Diagnosis + suggested user reply template |
| SERVER_ISSUE | Diagnosis + flag for DevOps (include node IP if known) |
| NETWORK | Diagnosis + suggested user reply template |
| KNOWN_FIXED | Note which version/commit fixes it |
| UNKNOWN | All collected evidence, mark ticket as needs-review (do not auto-resolve) |

---

## Marketing Skill — EDM & Retailer Management

### EDM Workflow

```
Step 1:  Determine campaign goal (new feature / renewal reminder / promotion)
  ↓
Step 2:  list_edm_templates → check existing templates
  ↓
Step 3:  If no suitable template → create via Center API POST /app/edm/templates
         (OpenClaw drafts content based on campaign goal)
  ↓
Step 4:  preview_edm_targets(targetFilter) → verify audience size and composition
  ↓
Step 5:  create_edm_task(templateId, targetFilter) → queue send (Asynq background job)
  ↓
Step 6:  Wait, then get_edm_send_stats → report delivery/open rates
  ↓
Step 7:  mail_send(to="david@kaitu.io", subject="[Marketing] EDM Report: {campaign}",
                   body=send_stats_summary)
```

### Retailer Management Workflow

```
Step 1:  list_retailer_todos → check pending follow-ups
  ↓
Step 2:  For each todo:
         get_retailer_detail(uuid) → review performance metrics
  ↓
Step 3:  Decision:
         - Performance qualifies for upgrade → update_retailer_level
         - Needs follow-up → create_retailer_note with action items
         - No action needed → create_retailer_note with status update
  ↓
Step 4:  list_retailers(level=L1) → identify potential upgrades
  ↓
Step 5:  mail_send(to="david@kaitu.io", subject="[Marketing] Retailer Daily Report",
                   body=actions_summary)
```

### Safety Rules

- EDM sends require `preview_edm_targets` confirmation before `create_edm_task`
- Never send EDM to more than 10,000 users without explicit human approval
- Retailer level changes must be preceded by `create_retailer_note` documenting the reason
- All user/order data access is read-only
- Reports always go to `david@kaitu.io`, never to end users

---

## New Tool Specifications (kaitu-center)

### Support Tools

#### `lookup_user`
- **Input**: `{ email?: string, uuid?: string }` (one required)
- **API**: `GET /app/users?email={email}` or `GET /app/users/{uuid}`
- **Output**: User profile (uuid, email, membership status, expired_at, roles, device count)
- **Roles**: support, marketing

#### `list_user_devices`
- **Input**: `{ uuid: string }`
- **API**: `GET /app/users/{uuid}/devices`
- **Output**: Device list (udid, platform, version, last_seen, remark)
- **Roles**: support

#### `close_feedback_ticket`
- **Input**: `{ id: number, reason: string }`
- **API**: `PUT /app/feedback-tickets/{id}/close`
- **Output**: `{ closed: true, id }`
- **Roles**: support

### Marketing Tools

#### `list_retailers`
- **Input**: `{ level?: string, email?: string, page?: number, pageSize?: number }`
- **API**: `GET /app/retailers`
- **Output**: Paginated retailer list
- **Roles**: marketing

#### `get_retailer_detail`
- **Input**: `{ uuid: string }`
- **API**: `GET /app/retailers/{uuid}`
- **Output**: Retailer profile with commission rates, performance metrics
- **Roles**: marketing

#### `update_retailer_level`
- **Input**: `{ uuid: string, level: "L1" | "L2" | "L3" | "L4" }`
- **API**: `PUT /app/retailers/{uuid}/level`
- **Output**: `{ updated: true, uuid, newLevel }`
- **Roles**: marketing

#### `create_retailer_note`
- **Input**: `{ uuid: string, content: string }`
- **API**: `POST /app/retailers/{uuid}/notes`
- **Output**: `{ created: true, noteId }`
- **Roles**: marketing

#### `list_retailer_todos`
- **Input**: `{ page?: number, pageSize?: number }`
- **API**: `GET /app/retailers/todos`
- **Output**: Paginated todo list with retailer info
- **Roles**: marketing

#### `list_edm_templates`
- **Input**: `{ page?: number, pageSize?: number }`
- **API**: `GET /app/edm/templates`
- **Output**: Template list (id, name, subject, language, created_at)
- **Roles**: marketing

#### `create_edm_task`
- **Input**: `{ templateId: number, targetFilter: object }`
- **API**: `POST /app/edm/tasks`
- **Output**: `{ taskId: string, queued: true }`
- **Roles**: marketing

#### `preview_edm_targets`
- **Input**: `{ targetFilter: object }`
- **API**: `POST /app/edm/preview-targets`
- **Output**: `{ count: number, sample: User[] }`
- **Roles**: marketing

#### `get_edm_send_stats`
- **Input**: `{ batchId?: string, templateId?: number }`
- **API**: `GET /app/edm/send-logs/stats`
- **Output**: Send statistics (total, sent, failed, pending)
- **Roles**: marketing

---

## Migration: kaitu-ops-mcp → kaitu-center

### Steps

1. `mv tools/kaitu-ops-mcp tools/kaitu-center`
2. Update `package.json`: name → `@kaitu/center-plugin`, add `openclaw.extensions`
3. Rename config: `kaitu-ops-mcp.toml` → `kaitu-center.toml`
4. Update `.claude/settings.json` MCP server path
5. Update `.claude/skills/kaitu-node-ops/SKILL.md` references
6. Update root `CLAUDE.md` references
7. Copy `kaitu-mail/` from `ldavid@192.168.31.40` into `tools/kaitu-mail/`
8. Add `openclaw.plugin.json` + `src/openclaw.ts` to kaitu-center
9. Add `src/roles.ts`

### Backward Compatibility

- Default `KAITU_ROLE=devops` preserves existing Claude Code behavior
- All 8 existing MCP tools unchanged in signature and output
- Config file format unchanged (just renamed)
- Existing tests continue to pass

---

## Implementation Order

### Phase 1 — Foundation (rename + mail plugin + role filtering)

1. Rename `kaitu-ops-mcp` → `kaitu-center`, update all references
2. Copy `kaitu-mail` into `tools/kaitu-mail/`
3. Add `src/roles.ts` with role-based tool mapping
4. Add role filtering to MCP `index.ts`
5. Add `openclaw.plugin.json` + `src/openclaw.ts` (plugin entry)
6. Verify: existing tests pass, Claude Code MCP still works

### Phase 2 — Support tools + skill

1. Add 3 new tools: `lookup_user`, `list_user_devices`, `close_feedback_ticket`
2. Write `kaitu-support/SKILL.md` + `prompts/diagnose.md`
3. Tests for new tools + role isolation

### Phase 3 — Marketing tools + skill

1. Add 9 new tools for retailers + EDM
2. Write `kaitu-marketing/SKILL.md`
3. Tests for marketing tools

### Phase 4 — Integration testing

1. Support flow: mock email → ticket lookup → log download → diagnosis → email report
2. Marketing flow: EDM template → preview → send → stats
3. Role isolation: support cannot access node ops, marketing cannot access SSH
4. Verified: `claude --print --tools "Read,Grep,Glob,Bash" --permission-mode bypassPermissions` works
