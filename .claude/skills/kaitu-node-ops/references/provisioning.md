# Provisioning a NEW private node (专属节点)

Reference for `kaitu-node-ops`. Use when an agent (holding the `kaitu-center` MCP with `cloud` + `cloud.write`) stands up a **dedicated private node** from a Center work item: claim → create VPS → OS prep → deploy → let the node self-register so Center activates the owner's subscription.

**A private node is the standard k2s deploy + two things:** (1) a Center work-item wrapper (claim/report), (2) `K2_PRIVATE_CLAIM` in `.env`. Everything else — compose, images, OS prep, verification, guardrails — is the hub (`SKILL.md` §1–§7). Don't duplicate; read the hub for any step not detailed here.

## Capability matrix (what "private" changes)

| Surface | Routes through | Metering |
|---------|----------------|----------|
| App (iOS/Android/desktop) | shared pool (k2subs picks) | per-node, no cutoff |
| Customer router / dedicated VPS | **private node** (single-tenant) | self-metered, **owner quota cutoff** |

`K2_PRIVATE_CLAIM` is the **only** structural difference and it switches on exactly two sidecar behaviors:
1. **Claim carriage (activation)** — sidecar carries the claim on registration → Center sets `Class=private`, binds owner, activates the owner's sub. **Not** in the agent's hands.
2. *(Metering is NOT one of them.)* Host-NIC metering + node-side cutoff run on **every** node with `K2_NODE_BILLING_START_DATE` set (shared included). The claim controls identity/activation only. (Host-NIC, not k2s app-bytes: it's the number the provider bills, provider-agnostic, decoupled from the data plane. There is no `docker-compose.private.yml`.)

## Step 0 — Preconditions

- MCP with `cloud` **and** `cloud.write` (claim/report/create are `cloud.write`).
- SSH uses the cloud account's default keypair (port 22 first, 1022 after `provision-node.sh`). **⚠ Open question:** multi-provider SSH-key acquisition is only solved for default-keypair-API providers (Lightsail). No key path → `update_node_operation(status=failed)` + escalate.
- `claimToken` + `K2_NODE_SECRET` are machine secrets — `.env` heredoc only, never logged (hub §0).

## Step 1 — Claim an intent

```
list_node_operations(action=provision, status=queued)
claim_node_operation(id=<operationId>, holder=<agent-id>, leaseSeconds=600?)
```

Response `{ data.operation, data.identity }` (`identity` only for `provision`). Capture:

| From claim | → | Notes |
|---|---|---|
| `identity.claimToken` | `.env K2_PRIVATE_CLAIM` | **ONE-TIME**, never shown again, never log. |
| `identity.centerUrl` | `.env K2_CENTER_URL` | |
| `identity.domain` | `.env K2_DOMAIN` | empty → auto sslip.io |
| `operation.params.region` | `create_cloud_instance region` + `.env K2_NODE_REGION` | |
| `operation.params.trafficTotalBytes` | `.env K2_NODE_TRAFFIC_LIMIT_GB` = `bytes / 1024^3` | the **sold** quota |
| `operation.params.ipType` | bundle choice (Step 2) + `.env K2_IP_TYPE` | `residential`/`non_residential`/`unknown` |
| `operation.subId` | instance `name = pn-<subId>` + `.env K2_NODE_NAME` | deterministic = idempotency root |

- **Claim error (409: taken/not found)** → don't retry blindly; re-list and pick another, or exit if empty.
- **Idempotent re-entry:** before creating, `list_cloud_instances` for `pn-<subId>`. Running match → reuse it (resume at Step 4), don't spawn an orphan.

## Step 2 — Choose account/region/plan/image

The task carries only business inputs (`region`, `trafficTotalBytes`, `ipType`). **You** pick concrete provider args — bundle whose included transfer comfortably **exceeds** the sold quota (so provider overage never triggers), and a pinned `k2Version` (not `:latest`):

```
list_cloud_accounts → account_name      list_cloud_regions → region
list_cloud_plans   → plan (transfer ≥ sold quota + headroom)
list_cloud_images  → Ubuntu 20/22/24 image_id  (provision-node.sh is Ubuntu-only)
```

## Step 3 — Create the VPS

```
create_cloud_instance(account_name=…, region=…, plan=…, image_id=…, name=pn-<subId>)
update_node_operation(id=<operationId>, status=in_progress, result={ instanceId, ipv4 })
```

> `update_node_operation` for `provision` accepts only `in_progress` / `failed`. **`done` is REJECTED** — the terminal success is set by the node's self-registration (Step 7).

## Step 4 — OS provision

Wait for SSH (port 22 first). Then the mandatory script-pipe form (hub §4):

```
exec_on_node(ip, "sudo bash -s", { scriptPath: "docker/scripts/provision-node.sh", timeout: 300 })
```

`provision-node.sh` hardens SSH to **1022** — all later `exec_on_node` use 1022. It prepares `/apps/k2s/`.

## Step 5 — Write `.env` (heredoc)

```
exec_on_node(ip, "sudo tee /apps/k2s/.env > /dev/null <<'ENVEOF'\n<contents>\nENVEOF")
```

Variables + sources are the hub §2 master table. Private-node essentials:
- `K2_NODE_SECRET` = `openssl rand -hex 32` (secret)
- `K2_PRIVATE_CLAIM` = `identity.claimToken` (secret, one-time)
- `K2_CENTER_URL`, `K2_DOMAIN` (empty), `K2_VERSION` (pinned), `K2_NODE_NAME`=`pn-<subId>`, `K2_NODE_REGION`, `K2_IP_TYPE`
- **`K2_NODE_BILLING_START_DATE`** — **REQUIRED** or the node runs uncapped. **Only the day-of-month matters; it must be the provider's real reset day, NOT the provisioning date.** Lightsail = `01` (calendar month); Bandwagon/KiwiVM = the plan's own "Next reset" day read off the panel/API — see `metering.md` Part B for both, plus first-month proration. Using today's date as a shortcut silently bakes in the wrong reset day (confirmed on two BWH nodes 2026-07-09: provisioned with day = provisioning date instead of the plan's real day, drifted the reset by a day for months undetected).
- **`K2_NODE_TRAFFIC_LIMIT_GB`** = `trafficTotalBytes / 1024^3` — the hard cutoff limit. **For Bandwagon, verify the plan's accounting model (`max(in,out)` vs in+out sum) before trusting this 1:1 mapping** — see `metering.md` Part B; the meter itself always bills `max(rx,tx)`, so a sum-billed plan needs `LIMIT_GB` roughly halved, not the raw sold quota.

## Step 6 — Deploy (single compose)

```
# SCP canonical compose + helper files (hub §4)
exec_on_node(ip, "sudo tee /apps/k2s/docker-compose.yml > /dev/null", { scriptPath: "docker/docker-compose.yml" })
# also: users (empty → pure remote auth), auto-update.sh, k2s-crash-monitor.sh
exec_on_node(ip, "cd /apps/k2s && sudo docker compose up -d")
```

**Private vs shared is the `.env`, not the compose** — same file deploys both. Private iff `.env` carries `K2_PRIVATE_CLAIM`. Updates = `pull + up -d`, never `down`.

## Step 7 — Verify

Run the hub §6 checklist (containers / sidecar registered / domain / k2s ready / net / port map) **plus**:

| Check | Command | Expected |
|-------|---------|----------|
| Reporter started | `docker logs --tail 80 k2-sidecar \| grep usage-reporter` | `usage-reporter-start` + periodic `usage-reporter-cycle-ok` (cumulative climbs) |
| Node in Center | `list_nodes(name=pn-<subId>)` | one tunnel with the sslip.io domain |
| Operation done | `list_node_operations(action=provision, status=done)` | `done` — set by **node self-registration**, not by `update_node_operation` |

## Step 8 — On failure

```
update_node_operation(id=<operationId>, status=failed, error=<concise — NEVER include claimToken/secret>)
```

Deploy steps are idempotent (re-run within the lease). **Never report `done`** — only self-registration sets it. If the node never registers, Center's timeout-sweep cron marks the sub failed (authoritative, agent-independent) — an agent crash never wedges the sub.

## Provisioning-specific guardrails (on top of hub §0)

1. Deterministic `pn-<subId>` = idempotency root — always probe `list_cloud_instances` before creating.
2. Re-runs idempotent (`provision-node.sh`, `.env` write, `up -d`).
3. Pick a big-enough bundle — included transfer > sold `trafficTotalBytes` so the node-side cutoff trips before provider overage.
