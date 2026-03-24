---
name: kaitu-support
description: Technical support workflow — ticket triage, user lookup, device log analysis, and diagnostic reporting via kaitu-center tools.
triggers:
  - support ticket
  - feedback ticket
  - user issue
  - device log
  - diagnose
  - troubleshoot
  - customer support
---

# Kaitu Technical Support

Use this skill when triaging user feedback tickets. The workflow uses kaitu-center MCP tools (support role) and kaitu-mail OpenClaw tools.

## Available Tools (Support Role)

| Tool | Purpose |
|------|---------|
| `lookup_user` | Find user by email or UUID |
| `list_user_devices` | List user's registered devices |
| `query_device_logs` | Find device logs in S3 by UDID |
| `download_device_log` | Download + extract log files |
| `query_feedback_tickets` | Search feedback tickets |
| `resolve_feedback_ticket` | Mark ticket as resolved |
| `close_feedback_ticket` | Close ticket with reason |

## Triage Workflow

### Step 1: Identify the Ticket

```
query_feedback_tickets(id=<ticket_id>)
```

Extract: user_uuid, udid, platform, description.

### Step 2: Look Up User Context

```
lookup_user(uuid=<user_uuid>)
list_user_devices(uuid=<user_uuid>)
```

Note: membership status, app version, device count.

### Step 3: Pull Device Logs

```
query_device_logs(udid=<udid>)
download_device_log(key=<s3_key>)
```

Logs are extracted to `/tmp/kaitu-device-logs/`. Use Read tool to analyze.

### Step 4: Diagnose

For automated diagnosis via Claude Code subprocess:

```bash
claude --print \
  --tools "Read,Grep,Glob,Bash" \
  --permission-mode bypassPermissions \
  --max-budget-usd 2.00 \
  --prompt "$(cat tools/kaitu-center/skills/kaitu-support/prompts/diagnose.md)"
```

Template variables must be rendered before passing.

### Step 5: Report & Resolve

Send diagnosis to `david@kaitu.io` via `mail_send` (kaitu-mail plugin).

Then either:
- `resolve_feedback_ticket(id, resolved_by="openclaw-support")` — issue diagnosed
- `close_feedback_ticket(id, reason="...")` — not actionable

### Step 6: Cleanup

```bash
rm -rf /tmp/kaitu-support/<ticket-id>/
```

## Classification Guide

| Classification | Meaning | Action |
|---------------|---------|--------|
| CLIENT_BUG | Bug in app code | Email with code path + fix suggestion |
| CLIENT_CONFIG | User config issue | Email with suggested reply to user |
| SERVER_ISSUE | Server/node problem | Email + flag for DevOps |
| NETWORK | User network issue | Email with suggested reply |
| KNOWN_FIXED | Fixed in later version | Email noting fix version |
| UNKNOWN | Cannot determine | Email all evidence, do NOT auto-resolve |

## Safety Rules

- NEVER send diagnostic details to end users — only to david@kaitu.io
- NEVER modify code during diagnosis — read-only analysis only
- If confidence < 7/10, mark as UNKNOWN and escalate
- Always clean up /tmp files after completion
