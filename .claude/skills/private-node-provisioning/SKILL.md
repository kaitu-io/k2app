---
name: private-node-provisioning
description: End-to-end runbook for an AI agent to provision a dedicated private node (专属节点) VPS — claim a provisioning intent from Center, stand up the VPS, deploy the k2s tunnel stack (single compose file), and let the node self-register so Center activates the owner's subscription. The sidecar self-meters host-NIC usage to Center.
triggers:
  - private node
  - 专属节点
  - provision private node
  - provisioning intent
  - claim provisioning
  - provision job
  - private node deploy
  - dedicated node
  - K2_PRIVATE_CLAIM
---

# Private Node Provisioning (专属节点 开通)

Use this skill when an external Claude Code agent (holding the `kaitu-center` MCP) provisions a **dedicated private node** end-to-end: claim a provisioning intent from Center, create a VPS, OS-prep it, deploy the k2s tunnel stack, and verify the node self-registers so Center flips the owner's subscription to active. The node's **sidecar** self-meters host-NIC usage to Center.

This is a **sibling** of `kaitu-node-ops`. Where that skill operates *existing* shared-pool nodes, this one *creates* a private node from a Center work item. All the safety guardrails, architecture identification, `exec_on_node` script-pipe rules, and post-deployment verification from `kaitu-node-ops` apply verbatim — read it for any operation not covered here. **Never** read/display/modify another node's `K2_NODE_SECRET`; use `pull + up -d`, never `down`.

## Architecture context (read before the loop)

**Capability matrix** — a private node changes *who routes through it*, not the tunnel stack:

| Surface | Routes through | Metering |
|---------|----------------|----------|
| App (iOS / Android / desktop) | **shared pool** (k2subs picks) | per-node, no cutoff |
| Customer router / dedicated VPS | **private node** (single-tenant) | self-metered, owner quota cutoff |

A private node is the **exact same** `k2s + k2-sidecar` stack as a shared node (see `kaitu-node-ops` Step 2), deployed from the **same single** `docker-compose.yml`. The only difference is one `.env` variable — `K2_PRIVATE_CLAIM` — which the sidecar reads and which switches on two behaviors, both inside the sidecar:

1. **Claim carriage (activation)** — the sidecar carries `K2_PRIVATE_CLAIM` on registration. Center flips that node's `Class=private`, binds the owner, and activates the owner's subscription. Activation is **not** in the agent's hands.
2. **Host-NIC self-metering + node-side cutoff (cost gate)** — the **sidecar** `TrafficMonitor` reads the **host** NIC byte counters (`/host/proc/net/dev`, already mounted) against a monthly cycle (`K2_NODE_BILLING_START_DATE`) and a limit (`K2_NODE_TRAFFIC_LIMIT_GB`). **The node is the single authority**: it hard-cuts k2s locally (pauses the container) when `used ≥ limit − 500MB`, and fail-closes (self-pauses) if metering breaks for 3 cycles. It also `POST`s usage to `{centerURL}/slave/usage` (Basic auth `base64(ipv4:secret)`); Center mirrors it into `NodeUsage` and, derived from that, hides over-quota/offline nodes from `/api/tunnels` + `/api/subs` so no client lands on a dead/overage node. **Center does NOT decide the cutoff.** Metering runs on **every** node that has a billing date set (shared + private); `K2_PRIVATE_CLAIM` controls only identity/activation, not metering.

> **Why host-NIC, not k2s app-bytes:** the NIC counter is the number the provider actually bills, is provider-agnostic (works where the provider has no traffic API), and keeps the tunnel data-plane decoupled from billing. (This supersedes the retired "Option D" in-k2s reporter — there is no longer a `docker-compose.private.yml` override.)

## Step 0: Preconditions / identity

Before the loop:

- The agent runs the `kaitu-center` MCP with the `cloud` **and** `cloud.write` permission groups (claim/report and instance creation are `cloud.write`; list/get are `cloud`).
- SSH access to the new VPS uses the **cloud account's default keypair**. The agent must be able to reach the new instance over SSH (port 22 initially, 1022 after hardening — see Step 4). **⚠ Open question (agent-provisioning spec §8):** multi-provider SSH-key acquisition is only solved for providers with a default-keypair API (e.g. Lightsail). For residential-IP / other providers the SSH access path is **TBD** — if the operation's provider has no known key path, `update_node_operation(status=failed)` and escalate.
- `claimToken` and `K2_NODE_SECRET` are **machine secrets**: never echo them to logs, the conversation, or any `update_node_operation` call. Write them to `.env` via heredoc only (Step 5).

## Step 1: Claim an intent

```
list_node_operations(action=provision, status=queued)
```

Returns `data.items[]` (paginated via `page` / `pageSize`). Pick one operation, then atomically lease it:

```
claim_node_operation(id=<operationId>, holder=<agent-id>, leaseSeconds=600?)
```

`leaseSeconds` defaults to **600** if omitted. The response is `{ data.operation, data.identity }` (`data.identity` is present only for `action=provision`). The provision spec fields below live on `data.operation.params.*`. Capture:

| From claim response | Goes to | Notes |
|---------------------|---------|-------|
| `data.identity.claimToken` | `.env` `K2_PRIVATE_CLAIM` | **ONE-TIME** — only ever returned by this call, never shown again. Bake into `.env` immediately; never log it. |
| `data.identity.centerUrl` | `.env` `K2_CENTER_URL` | Center base URL (the sidecar uses it for both registration and usage reporting). |
| `data.identity.domain` | `.env` `K2_DOMAIN` | Empty → leave empty, sidecar auto-derives `{ipv4-with-dashes}.sslip.io`. |
| `data.operation.params.region` | `create_cloud_instance region` + `.env` `K2_NODE_REGION` | Map to the provider's region identifier (Step 2). |
| `data.operation.params.trafficTotalBytes` | `.env` `K2_NODE_TRAFFIC_LIMIT_GB` | Derive GB = `trafficTotalBytes / (1024^3)`. The **sold** quota (e.g. 950G on a 1T bundle). |
| `data.operation.params.ipType` | provider / bundle selection (Step 2) **and** `.env` `K2_IP_TYPE` | residential vs non-residential. Pass it through verbatim — the sidecar reports it to Center so the node is flagged as a 住宅IP / residential exit. Center normalizes any unexpected value to `unknown`, so use exactly `residential` / `non_residential` / `unknown`. |
| `data.operation.subId` | instance `name = pn-<subId>` + `.env` `K2_NODE_NAME` | Deterministic naming → idempotency root. |

> **Note (post-decoupling):** the deploy task carries only business inputs (`region`, `trafficTotalBytes`, `ipType`). Whoever provisions chooses the concrete `provider` / `bundle` / `image` / `k2Version` that satisfies them (Step 2) — pick a bundle whose included transfer comfortably exceeds the sold `trafficTotalBytes` so provider overage never triggers.

**If claim returns an error envelope (409-ish: already claimed / not found):** do **not** retry blindly. Re-run `list_node_operations(action=provision, status=queued)` — someone else took it — and pick another, or exit if the queue is empty.

**Idempotent re-entry:** before creating, probe `list_cloud_instances` for an instance already named `pn-<subId>`. If one exists and is running, **reuse it** (a prior run was interrupted) — skip Step 3's create and resume at Step 4. This is the guard against orphan VPSes.

## Step 2: Discover account / region / plan / image

Map the job's abstract spec fields to concrete provider arguments. **You** choose the bundle/image (the task does not dictate them) — pick a bundle whose included transfer ≥ the sold `trafficTotalBytes` with headroom, and pin a known-good `k2Version` (not `:latest`):

```
list_cloud_accounts   → pick account_name for the chosen provider
list_cloud_regions    → map job.region → create_cloud_instance region
list_cloud_plans      → choose a plan/bundle whose transfer ≥ sold quota → create_cloud_instance plan
list_cloud_images     → choose an Ubuntu 20/22/24 image → create_cloud_instance image_id (provision-node.sh is Ubuntu-only)
```

## Step 3: Create the VPS

```
create_cloud_instance(
  account_name=<from list_cloud_accounts>,
  region=<mapped>,
  plan=<chosen bundle>,
  image_id=<chosen image>,
  name=pn-<subId>
)
```

As soon as the instance ID and public IPv4 are known, report progress so Center sees the work advancing (pass them inside `result`):

```
update_node_operation(id=<operationId>, status=in_progress, result={ instanceId: <...>, ipv4: <publicIPv4> })
```

> For `action=provision`, `update_node_operation` accepts `in_progress` or `failed`. **`done` is REJECTED here** — Center rejects it. The terminal completion (`done`) is set by the node itself at self-registration (Step 7).

## Step 4: OS provision

1. Wait for SSH reachability. New instances answer on **port 22** first; `provision-node.sh` step 13 hardens SSH to **port 1022 only**, so **after** provisioning all subsequent `exec_on_node` calls use 1022. Use `ping_node` / a trivial `exec_on_node` to confirm reachability before proceeding.
2. Run the full OS prep via the **mandatory** script-pipe form (reads the local file, pipes over SSH stdin — never inline a large script):

```
exec_on_node(ip=<ipv4>, "sudo bash -s", { scriptPath: "docker/scripts/provision-node.sh", timeout: 300 })
```

`provision-node.sh` is a 16-step, idempotent, root-required script: timezone (Asia/Singapore) → snapd removal → swap → Docker CE → IPv6 → BBR → Docker daemon.json → UFW-Docker → **SSH 22→1022** → journald persistence + crash monitor → auto-update cron. It runs **before** any compose deploy and prepares `/apps/kaitu-slave/` as the deploy dir. It is destructive (stops containers) — fine on a fresh node.

## Step 5: Write `/apps/kaitu-slave/.env` (heredoc — secrets never on the command line)

Write via `exec_on_node` with a **heredoc** so secrets never appear in the process list / shell history:

```
exec_on_node(ip=<ipv4>, "sudo tee /apps/kaitu-slave/.env > /dev/null <<'ENVEOF'\n<contents>\nENVEOF")
```

Exact variables and their sources:

| `.env` variable | Value / source | Consumer | Secret? |
|-----------------|----------------|----------|---------|
| `K2_NODE_SECRET` | **agent-generated** `openssl rand -hex 32` | **sidecar** — registration **and** the Center Basic-auth (`ipv4:secret`) for usage reporting | **YES — never log** |
| `K2_PRIVATE_CLAIM` | `identity.claimToken` from Step 1 | **sidecar** — carried on registration → Center flips `Class=private` + owner + activates sub; also switches on host-NIC self-metering | **YES — one-time, never log** |
| `K2_CENTER_URL` | `identity.centerUrl` | sidecar (registration + `/slave/usage`) + k2v4-slave | no |
| `K2_DOMAIN` | `identity.domain`, or **empty** | sidecar (empty → auto `{ipv4-with-dashes}.sslip.io`) | no |
| `K2_VERSION` | chosen pin (not `:latest`) | image tags | no |
| `K2_NODE_BILLING_START_DATE` | provisioning date `yyyy-MM-dd` (today) | **sidecar `TrafficMonitor`** — monthly-cycle anchor. **REQUIRED for metering + cutoff**: omit it and the node runs UNCAPPED (no quota cutoff, no usage reports). | no |
| `K2_NODE_TRAFFIC_LIMIT_GB` | `job.trafficTotalBytes / 1024^3` | **sidecar — the hard cutoff limit**: node pauses k2s when used ≥ limit − 500MB; also reported to Center for display/score. `0` = unlimited | no |
| `K2_NODE_NAME` | `pn-<subId>` | registration meta | no |
| `K2_NODE_REGION` | `job.region` | registration meta | no |
| `K2_IP_TYPE` | `job.ipType` (`residential` / `non_residential`; omit → `unknown`) | **sidecar** — reported on registration → Center records `SlaveNode.ip_type` (drives 住宅IP visibility in `/api/v20260717/tunnels` + admin/MCP). Last-writer-wins with ops `update_node`. | no |

**How the private node differs (all in the sidecar, base compose only):**
- `K2_PRIVATE_CLAIM` → the **sidecar** (base compose passes `K2_PRIVATE_CLAIM=${K2_PRIVATE_CLAIM:-}`). The sidecar registers and carries the claim → Center flips `Class=private`, binds the owner, and activates the sub. The claim is about **identity/activation only**.
- **Metering + cutoff are independent of the claim and run on every node.** The sidecar's `TrafficMonitor` self-meters the host NIC, hard-cuts k2s locally when `used ≥ limit − 500MB`, and reports usage to Center (mirrored into `NodeUsage`). This requires **`K2_NODE_BILLING_START_DATE`** (cycle anchor) + **`K2_NODE_TRAFFIC_LIMIT_GB`** (the limit) — omit the billing date and the node runs uncapped. The sidecar already holds `K2_NODE_SECRET`/`K2_CENTER_URL` and auto-detects its IPv4, so it needs **no extra env** to report. No k2s-side metering, no compose override.

## Step 6: Deploy the stack (single compose file)

SCP the canonical base file to `/apps/kaitu-slave/`, then bring up:

1. SCP `docker/docker-compose.yml` to `/apps/kaitu-slave/` (use the `scriptPath` upload form per `kaitu-node-ops`, e.g. `exec_on_node(ip, "sudo tee /apps/kaitu-slave/docker-compose.yml > /dev/null", { scriptPath: "docker/docker-compose.yml" })`).
2. Also deploy `users` (empty → pure remote auth), `auto-update.sh`, and `k2s-crash-monitor.sh` per `kaitu-node-ops` Step 5 (the crash-monitor + cron steps of `provision-node.sh` look for these in `/apps/kaitu-slave/`).
3. Bring up:

```
exec_on_node(ip=<ipv4>, "cd /apps/kaitu-slave && docker compose -f docker-compose.yml up -d")
```

**Private vs shared is the `.env`, not the compose:** the same `docker-compose.yml` deploys both. A node is private iff its `.env` carries `K2_PRIVATE_CLAIM` — the sidecar registers the claim and Center activates the owner's sub. **Metering/cutoff is NOT tied to the claim** — every node with `K2_NODE_BILLING_START_DATE` set self-meters and reports to `/slave/usage` (shared nodes included). There is no separate override file.

> Updates use `pull + up -d`, **never** `down`.

## Step 7: Verify

Run the `kaitu-node-ops` post-deployment checklist (containers Up, sidecar healthy, tunnel domain derived, container outbound network, port mapping) **plus** these private-node specifics:

| Check | Command | Expected |
|-------|---------|----------|
| k2s healthy + tunnel ready | `docker logs --tail 20 k2s \| grep "server ready"` | `k2s server ready listen=:443` |
| Sidecar registered (carried claim) | `docker logs --tail 30 k2-sidecar \| grep "Registration completed"` | `tunnels=1` |
| Usage reporter started | `docker logs --tail 80 k2-sidecar \| grep "usage-reporter"` | `DIAG: usage-reporter-start` (and periodic `usage-reporter-cycle-ok`) |
| Node visible in Center | `list_nodes(name=pn-<subId>)` | one `tunnels` entry with the sslip.io domain |
| Metering active (self-meter + report) | `docker logs --tail 80 k2-sidecar \| grep "usage-reporter-cycle-ok"` | periodic lines with advancing `cumulative` host-NIC bytes (usage is mirrored into Center `NodeUsage`, not `CloudInstance`) |
| Operation flipped to done | `list_node_operations(action=provision, status=done)` (or check the operation) | operation is `done` — **set by node self-registration, NOT by `update_node_operation`** |

If the usage-reporter line is absent, re-check that **`K2_NODE_BILLING_START_DATE`** (+ `K2_NODE_TRAFFIC_LIMIT_GB`) and `K2_NODE_SECRET` are present in `.env`. The reporter + cutoff enforcer start only when the `TrafficMonitor` initialized (billing date set); otherwise the sidecar logs `Metering disabled: no billing date` and runs **uncapped**.

## Step 8: On failure

Any step failing → mark the operation failed so Center frees / alerts on it:

```
update_node_operation(id=<operationId>, status=failed, error=<concise reason — NEVER include claimToken or K2_NODE_SECRET>)
```

- Deploy steps are idempotent — within the lease you may self-retry (re-run `provision-node.sh` / re-`up -d`) before giving up.
- **Never report `done` for provision** — Center rejects it; only the node's self-registration sets the terminal success. If the node never self-registers, Center's timeout-sweep cron marks the sub failed (the authoritative gate, independent of the agent). An agent crash at any point never wedges the sub permanently.

## Step 9: Guardrails (mirror kaitu-node-ops)

1. **`claimToken` + `K2_NODE_SECRET` are untouchable** — never echo to logs, the conversation, or any `update_node_operation` call. Write only via heredoc (Step 5); never pass on the command line.
2. **Deterministic naming `pn-<subId>` = idempotency root** — always probe `list_cloud_instances` before creating; reuse a running match instead of spawning an orphan.
3. **Re-runs are idempotent** — `provision-node.sh`, `.env` write, and `up -d` are all safe to repeat.
4. **`pull + up -d`, never `down`.**
5. **Don't touch other nodes** — this runbook only ever operates on the instance it just created (`pn-<subId>`). Never read/modify the `K2_NODE_SECRET` or config of any existing node.
6. **`K2_DOMAIN` stays empty** unless the job supplies a domain — the sidecar auto-derives a globally-unique `{ipv4-with-dashes}.sslip.io`.
7. **Pick a big-enough bundle** — the cost guardrail is bundle sizing: choose a VPS bundle whose included transfer exceeds the sold `trafficTotalBytes` so the sidecar's quota cutoff trips before provider overage billing.
