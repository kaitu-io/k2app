---
name: kaitu-marketing
description: Marketing workflow — EDM email campaigns, retailer management, and daily reporting via kaitu-center tools.
triggers:
  - marketing
  - edm
  - email campaign
  - retailer
  - 分销商
  - 邮件营销
  - send email
  - retailer todo
---

# Kaitu Marketing Operations

Use this skill for EDM email campaigns and retailer management. Uses kaitu-center MCP tools (marketing role) and kaitu-mail OpenClaw tools.

## Available Tools (Marketing Role)

| Tool | Purpose |
|------|---------|
| `lookup_user` | Find user by email or UUID (read-only) |
| `list_retailers` | List retailers with filtering |
| `get_retailer_detail` | Get retailer profile + metrics |
| `update_retailer_level` | Change retailer commission level |
| `create_retailer_note` | Add follow-up note |
| `list_retailer_todos` | List pending follow-ups |
| `list_edm_templates` | List email templates |
| `create_edm_task` | Queue email send task |
| `preview_edm_targets` | Preview target audience |
| `get_edm_send_stats` | Get send statistics |

## EDM Email Campaign Workflow

### Step 1: Determine Campaign Goal

Types: new feature announcement, renewal reminder, promotion, re-engagement.

### Step 2: Check Existing Templates

```
list_edm_templates()
```

### Step 3: Create Template (if needed)

Templates are created via Center API POST /app/edm/templates. Drafting content is part of the marketing agent's responsibility.

### Step 4: Preview Target Audience

```
preview_edm_targets(target_filter={...})
```

**MANDATORY** before sending. Verify audience size and composition.

### Step 5: Send

```
create_edm_task(template_id=N, target_filter={...})
```

### Step 6: Track Results

```
get_edm_send_stats(template_id=N)
```

### Step 7: Report

Send summary to david@kaitu.io via `mail_send` (kaitu-mail plugin).

## Retailer Management Workflow

### Step 1: Check Pending Follow-ups

```
list_retailer_todos()
```

### Step 2: Review Each Todo

```
get_retailer_detail(uuid=...)
```

Assess performance metrics, order volume, customer feedback.

### Step 3: Take Action

For each retailer:
- **Upgrade eligible**: `create_retailer_note(reason)` then `update_retailer_level(uuid, newLevel)`
- **Needs follow-up**: `create_retailer_note(action items)`
- **No action needed**: `create_retailer_note(status update)`

### Step 4: Report

Send daily summary to david@kaitu.io via `mail_send`.

## Safety Rules

- **ALWAYS** run `preview_edm_targets` before `create_edm_task`
- **NEVER** send EDM to more than 10,000 users without human approval
- **ALWAYS** document reason via `create_retailer_note` before `update_retailer_level`
- All user/order data access is **read-only**
- Reports go to `david@kaitu.io` only — never to end users
- Audit trail: all write operations are logged automatically
