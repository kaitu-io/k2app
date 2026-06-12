---
name: private-node-provisioning
description: End-to-end runbook for an AI agent to provision a dedicated private node (专属节点) VPS — claim a provisioning intent from Center, stand up the VPS, deploy the k2s tunnel stack with Option D self-metering, and let the node self-register so Center activates the owner's subscription.
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
  - docker-compose.private
---

# Private Node Provisioning (专属节点 开通)

Use this skill when an external Claude Code agent (holding the `kaitu-center` MCP) provisions a **dedicated private node** end-to-end: claim a provisioning intent from Center, create a VPS, OS-prep it, deploy the k2s tunnel stack with the Option D self-metering override, and verify the node self-registers so Center flips the owner's subscription to active.

This is a **sibling** of `kaitu-node-ops`. Where that skill operates *existing* shared-pool nodes, this one *creates* a private node from a Center work item. All the safety guardrails, architecture identification, `exec_on_node` script-pipe rules, and post-deployment verification from `kaitu-node-ops` apply verbatim — read it for any operation not covered here. **Never** read/display/modify another node's `K2_NODE_SECRET`; use `pull + up -d`, never `down`.

## Architecture context (read before the loop)

**Capability matrix** — a private node changes *who routes through it*, not the tunnel stack:

| Surface | Routes through | Metering |
|---------|----------------|----------|
| App (iOS / Android / desktop) | **shared pool** (k2subs picks) | per-node, no cutoff |
| Customer router / dedicated VPS | **private node** (single-tenant) | self-metered, owner quota cutoff |

A private node is the **same** `k2v5 + k2-sidecar + k2v4-slave` stack as a shared node (see `kaitu-node-ops` Step 2), with **two** additions, both env-driven:

1. **Claim carriage (activation)** — the sidecar carries `K2_PRIVATE_CLAIM` on registration. Center flips that node's `Class=private`, binds the owner, and activates the owner's subscription. Activation is **not** in the agent's hands.
2. **Option D self-metering (cost gate)** — under spec `2026-06-12-private-node-usage-reporting-in-k2s-design.md`, the **k2s process itself** runs the metering loop: it reads its own counters, `POST`s `{centerURL}/slave/usage`, and applies the verdict to its own `accepting` gate (cuts new connections at 95% / 100%). No separate metering sidecar wiring, no cross-container IPC. The loop is **gated on env presence** — k2s only starts the reporter when `K2_USAGE_REPORT_URL != "" && K2_NODE_SECRET != ""`. Shared-pool k2s never gets those env vars → reporter never starts → byte-for-byte identical to today.

That env-gating is *why* the private node deploys with **two** compose files: the base `docker-compose.yml` (shared by all nodes) plus the `docker-compose.private.yml` override that injects the three metering env vars onto the `k2v5` container. Shared nodes simply omit the override.

## Step 0: Preconditions / identity

Before the loop:

- The agent runs the `kaitu-center` MCP with the `cloud` **and** `cloud.write` permission groups (claim/report and instance creation are `cloud.write`; list/get are `cloud`).
- SSH access to the new VPS uses the **cloud account's default keypair**. The agent must be able to reach the new instance over SSH (port 22 initially, 1022 after hardening — see Step 4). **⚠ Open question (agent-provisioning spec §8):** multi-provider SSH-key acquisition is only solved for providers with a default-keypair API (e.g. Lightsail). For residential-IP / other providers the SSH access path is **TBD** — if the job's provider has no known key path, `report_provisioning(failed)` and escalate.
- `claimToken` and `K2_NODE_SECRET` are **machine secrets**: never echo them to logs, the conversation, or any `report_provisioning` call. Write them to `.env` via heredoc only (Step 5).

## Step 1: Claim an intent

```
list_provisioning_intents(status=queued)
```

Returns `data.items[]` (paginated via `page` / `pageSize`). Pick one job, then atomically lease it:

```
claim_provisioning_intent(id=<jobId>, holder=<agent-id>, leaseSeconds=600?)
```

`leaseSeconds` defaults to **600** if omitted. The response is `{ data.job, data.identity }`. Capture:

| From claim response | Goes to | Notes |
|---------------------|---------|-------|
| `data.identity.claimToken` | `.env` `K2_PRIVATE_CLAIM` | **ONE-TIME** — only ever returned by this call, never shown again. Bake into `.env` immediately; never log it. |
| `data.identity.centerUrl` | `.env` `K2_CENTER_URL` + `K2_USAGE_REPORT_URL` | Center base URL. |
| `data.identity.domain` | `.env` `K2_DOMAIN` | Empty → leave empty, sidecar auto-derives `{ipv4-with-dashes}.sslip.io`. |
| `data.job.region` | `create_cloud_instance region` + `.env` `K2_NODE_REGION` | Map to the provider's region identifier (Step 2). |
| `data.job.bundleId` | `create_cloud_instance plan` | Map bundle → plan (Step 2). |
| `data.job.imageId` | `create_cloud_instance image_id` | OS image. |
| `data.job.k2Version` | `.env` `K2_VERSION` | Pin the version — do **not** use `:latest`. |
| `data.job.trafficTotalBytes` | `.env` `K2_NODE_TRAFFIC_LIMIT_GB` | Derive GB = `trafficTotalBytes / (1024^3)`. |
| `data.job.ipType` | provider selection / notes | residential vs non-residential. |
| `data.job.subId` | instance `name = pn-<subId>` + `.env` `K2_NODE_NAME` | Deterministic naming → idempotency root. |

**If claim returns an error envelope (409-ish: already claimed / not found):** do **not** retry blindly. Re-run `list_provisioning_intents(status=queued)` — someone else took it — and pick another, or exit if the queue is empty.

**Idempotent re-entry:** before creating, probe `list_cloud_instances` for an instance already named `pn-<subId>`. If one exists and is running, **reuse it** (a prior run was interrupted) — skip Step 3's create and resume at Step 4. This is the guard against orphan VPSes.

## Step 2: Discover account / region / plan / image

Map the job's abstract spec fields to concrete provider arguments:

```
list_cloud_accounts   → pick account_name for the job's provider
list_cloud_regions    → map job.region → create_cloud_instance region
list_cloud_plans      → map job.bundleId → create_cloud_instance plan
list_cloud_images     → map job.imageId → create_cloud_instance image_id (Ubuntu 20/22/24 — provision-node.sh is Ubuntu-only)
```

## Step 3: Create the VPS

```
create_cloud_instance(
  account_name=<from list_cloud_accounts>,
  region=<mapped>,
  plan=<mapped from bundleId>,
  image_id=<mapped from imageId>,
  name=pn-<subId>
)
```

As soon as the instance ID and public IPv4 are known, report progress so Center sees the work advancing:

```
report_provisioning(id=<jobId>, status=provisioning, instanceId=<...>, ipv4=<publicIPv4>)
```

> `report_provisioning` accepts only `provisioning` or `failed`. **`succeeded` is NOT accepted here** — Center rejects it. The terminal `succeeded` status is set by the node itself at self-registration (Step 7).

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
| `K2_NODE_SECRET` | **agent-generated** `openssl rand -hex 32` | sidecar (registration) **and** k2s (Center Basic-auth for usage reporting under Option D) | **YES — never log** |
| `K2_PRIVATE_CLAIM` | `identity.claimToken` from Step 1 | **sidecar** — carried on registration → Center flips `Class=private` + owner + activates sub | **YES — one-time, never log** |
| `K2_USAGE_REPORT_URL` | `identity.centerUrl` | **k2s** (private override) — enables the self-report/cutoff loop | no |
| `K2_NODE_IPV4` | the instance's public IPv4 | **k2s** (private override) — Basic-auth **username**; MUST equal the registered node IPv4 (empty → k2s `ProbePublicIP` fallback) | no |
| `K2_CENTER_URL` | `identity.centerUrl` | sidecar + k2v4-slave | no |
| `K2_DOMAIN` | `identity.domain`, or **empty** | sidecar (empty → auto `{ipv4-with-dashes}.sslip.io`) | no |
| `K2_VERSION` | `job.k2Version` (pin, not `:latest`) | image tags | no |
| `K2_NODE_TRAFFIC_LIMIT_GB` | `job.trafficTotalBytes / 1024^3` | traffic gate | no |
| `K2_NODE_NAME` | `pn-<subId>` | registration meta | no |
| `K2_NODE_REGION` | `job.region` | registration meta | no |

**The env split (Option D):**
- `K2_PRIVATE_CLAIM` → the **sidecar** (base compose passes `K2_PRIVATE_CLAIM=${K2_PRIVATE_CLAIM:-}`). The sidecar registers and carries the claim; Center does the activation.
- `K2_USAGE_REPORT_URL` + `K2_NODE_SECRET` + `K2_NODE_IPV4` → **k2s**, injected by `docker-compose.private.yml` onto the `k2v5` container. k2s self-meters: counts bytes → `POST {centerURL}/slave/usage` (Basic auth `base64(ipv4:secret)`) → applies verdict to its own `accepting` gate. The reporter is **double-gated** (`UsageReportURL != "" && NodeSecret != ""`); shared nodes that never get this override stay dumb and hold no secret.

## Step 6: Deploy the stack (two compose files)

SCP the canonical files to `/apps/kaitu-slave/`, then bring up with **both** compose files:

1. SCP **both** `docker/docker-compose.yml` **and** `docker/docker-compose.private.yml` to `/apps/kaitu-slave/` (use the `scriptPath` upload form per `kaitu-node-ops`, e.g. `exec_on_node(ip, "sudo tee /apps/kaitu-slave/docker-compose.private.yml > /dev/null", { scriptPath: "docker/docker-compose.private.yml" })`).
2. Also deploy `users` (empty → pure remote auth), `auto-update.sh`, and `k2v5-crash-monitor.sh` per `kaitu-node-ops` Step 5 (the crash-monitor + cron steps of `provision-node.sh` look for these in `/apps/kaitu-slave/`).
3. Bring up:

```
exec_on_node(ip=<ipv4>, "cd /apps/kaitu-slave && docker compose -f docker-compose.yml -f docker-compose.private.yml up -d")
```

**Why both files:** the private override is a thin layer that only adds the three k2s metering env vars (`K2_NODE_SECRET`, `K2_USAGE_REPORT_URL`, `K2_NODE_IPV4`) onto `k2v5`. It is an override, not a replacement, so any change to the base compose is inherited automatically (no two-file drift). A shared-pool node would bring up with the base file **only** — its k2v5 never receives `K2_NODE_SECRET` → reporter never starts → it holds no Center secret and never contacts Center for usage. Omitting the override is what makes the node shared; including it is what makes it private-metering.

> Updates use `pull + up -d` (with both `-f` files), **never** `down`.

## Step 7: Verify

Run the `kaitu-node-ops` post-deployment checklist (containers Up, sidecar healthy, tunnel domain derived, container outbound network, port mapping) **plus** these private-node specifics:

| Check | Command | Expected |
|-------|---------|----------|
| k2v5 healthy + tunnel ready | `docker logs --tail 20 k2v5 \| grep "server ready"` | `k2s server ready listen=:443` |
| Sidecar registered (carried claim) | `docker logs --tail 30 k2-sidecar \| grep "Registration completed"` | `tunnels=1` |
| Usage reporter started | `docker logs --tail 50 k2v5 \| grep -i "usage report"` | reporter start line present (only logs on verdict change / error — absence of errors is OK) |
| Live counters readable | `curl -s 127.0.0.1:9099/usage` | JSON counters (loopback-only, **read-only** — the sole usage endpoint after the Option D trim; `/reset` + `/verdict` were removed) |
| Node visible in Center | `list_nodes(name=pn-<subId>)` | one `tunnels` entry with the sslip.io domain |
| Job flipped to succeeded | `list_provisioning_intents(status=succeeded)` (or check the job) | job is `succeeded` — **set by node self-registration, NOT by `report_provisioning`** |

If the usage reporter line is absent **and** `127.0.0.1:9099/usage` errors, re-check that `K2_USAGE_REPORT_URL` + `K2_NODE_SECRET` are present in `.env` and that the stack was brought up with **both** compose files (the override is what injects them).

## Step 8: On failure

Any step failing → mark the job failed so Center frees / alerts on it:

```
report_provisioning(id=<jobId>, status=failed, error=<concise reason — NEVER include claimToken or K2_NODE_SECRET>)
```

- Deploy steps are idempotent — within the lease you may self-retry (re-run `provision-node.sh` / re-`up -d`) before giving up.
- **Never report `succeeded`** — Center rejects it; only the node's self-registration sets the terminal success. If the node never self-registers, Center's timeout-sweep cron marks the sub failed (the authoritative gate, independent of the agent). An agent crash at any point never wedges the sub permanently.

## Step 9: Guardrails (mirror kaitu-node-ops)

1. **`claimToken` + `K2_NODE_SECRET` are untouchable** — never echo to logs, the conversation, or any `report_provisioning` call. Write only via heredoc (Step 5); never pass on the command line.
2. **Deterministic naming `pn-<subId>` = idempotency root** — always probe `list_cloud_instances` before creating; reuse a running match instead of spawning an orphan.
3. **Re-runs are idempotent** — `provision-node.sh`, `.env` write, and `up -d` are all safe to repeat.
4. **`pull + up -d`, never `down`** — including both `-f` files on updates.
5. **Don't touch other nodes** — this runbook only ever operates on the instance it just created (`pn-<subId>`). Never read/modify the `K2_NODE_SECRET` or config of any existing node.
6. **`K2_DOMAIN` stays empty** unless the job supplies a domain — the sidecar auto-derives a globally-unique `{ipv4-with-dashes}.sslip.io`.
