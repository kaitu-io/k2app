---
name: kaitu-support
description: Technical support workflow — ticket triage, user lookup, device log analysis, reply and resolution via kaitu-center MCP tools.
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

Use this skill when triaging user feedback tickets. All operations use kaitu-center MCP tools.

## Available Tools

| Tool | Purpose |
|------|---------|
| `lookup_user` | Find user by email or UUID |
| `list_user_devices` | List user's registered devices |
| `query_device_logs` | Find device logs in S3 by UDID |
| `download_device_log` | Download + extract log files |
| `query_feedback_tickets` | Search feedback tickets |
| `list_ticket_replies` | List all replies on a ticket |
| `reply_feedback_ticket` | Reply to user (triggers aggregated email after 5min) |
| `resolve_feedback_ticket` | Mark ticket as resolved |
| `close_feedback_ticket` | Close ticket (not actionable) |

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

### Step 3: Check Existing Replies

```
list_ticket_replies(id=<ticket_id>)
```

Read conversation history before proceeding.

### Step 4: Pull Device Logs

```
query_device_logs(udid=<udid>)
download_device_log(key=<s3_key>)
```

Logs are extracted to `/tmp/kaitu-device-logs/`. Use Read tool to analyze.

### Step 5: Diagnose

Run the quick-diag script on any k2.log file:

```bash
bash scripts/k2-quick-diag.sh /tmp/kaitu-device-logs/k2.log
```

For deeper analysis, use the DIAG log patterns documented in `kaitu-node-ops` skill Step 12.

### Step 6: Reply to User

```
reply_feedback_ticket(id=<ticket_id>, content="诊断结果和建议...")
```

Reply guidelines:
- Write in the user's language (detect from their ticket description)
- Be concise: state the problem, then the solution/workaround
- Include specific version numbers if an upgrade is needed
- Never expose internal infrastructure details (server IPs, debug logs, error codes)
- If issue requires user action, give clear step-by-step instructions

### Step 7: Resolve or Close

Based on diagnosis:

| Situation | Action |
|-----------|--------|
| Issue diagnosed, reply sent | `resolve_feedback_ticket(id, resolved_by="claude")` |
| Fixed in later version | Reply with version info → `resolve_feedback_ticket` |
| Not actionable / spam | `close_feedback_ticket(id)` |
| Cannot determine cause | Reply asking for more info, do NOT resolve yet |

## Classification Guide

| Classification | Meaning | Reply Template |
|---------------|---------|----------------|
| CLIENT_BUG | Bug in app code | Acknowledge + workaround if any + "will fix in next version" |
| CLIENT_CONFIG | User config issue | Step-by-step fix instructions |
| SERVER_ISSUE | Server/node problem | "We've identified the issue and are working on it" |
| NETWORK | User network issue | Network troubleshooting steps |
| KNOWN_FIXED | Fixed in later version | "Please update to version X.Y.Z" |
| UNKNOWN | Cannot determine | Ask user for more details (specific steps to reproduce) |

## Safety Rules

- NEVER expose internal details to users (server IPs, debug logs, stack traces, error codes)
- NEVER modify code during diagnosis — read-only analysis only
- If confidence < 7/10, classify as UNKNOWN and ask user for more information
- Always check `list_ticket_replies` before replying to avoid duplicate responses
- Clean up `/tmp/kaitu-device-logs/` after completing diagnosis
