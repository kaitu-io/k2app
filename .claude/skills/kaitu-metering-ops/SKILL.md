---
name: kaitu-metering-ops
description: Build, deploy, and maintain the node-authority traffic metering + cutoff sidecar. Covers building/pushing the k2-sidecar image, deploying/upgrading it on a node, configuring quota (billing date / limit / mid-cycle used baseline), observing the usage recorder, and operating/testing the node-local cutoff.
triggers:
  - metering ops
  - traffic metering
  - sidecar deploy
  - sidecar image
  - build sidecar
  - traffic limit
  - traffic quota
  - set-usage
  - traffic cutoff
  - quota cutoff
  - K2_NODE_TRAFFIC_LIMIT_GB
  - K2_NODE_BILLING_START_DATE
  - K2_NODE_TRAFFIC_USED_GB
  - node usage
  - NodeUsage
  - usage reporter
---

# Kaitu Metering Ops (node-authority traffic metering + cutoff)

Use this when building, deploying, or maintaining the **traffic metering + quota cutoff** system. In the node-authority model the **node is the single authority**: the `k2-sidecar` self-meters the host NIC, owns the monthly cycle, and **hard-cuts the k2s data plane locally** when over quota. Center (`/slave/usage` → `NodeUsage`) is a **passive recorder** (it does not command the cut).

This is a **sibling of `kaitu-node-ops`** — all node-access rules (architecture ID, `exec_on_node` script-pipe, SSH port 1022, `pull + up -d` never `down`, never read/modify another node's `K2_NODE_SECRET`) apply verbatim. Read that skill for anything not covered here.

## Architecture facts (read first)

- **Single compose, single image pair**: `docker/docker-compose.yml` runs `k2-sidecar` + `k2s`. Images: `public.ecr.aws/d6n9t2r2/k2-sidecar:${K2_VERSION}` and `:k2s:${K2_VERSION}`.
- **Metering lives entirely in the sidecar.** k2s is the tunnel data plane and is **not** involved in metering. Metering-only changes → rebuild **sidecar** only; k2s can stay on its last tag.
- **Reads the HOST NIC, not the container veth.** Compose mounts `/proc`→`/host/proc`; the sidecar reads `/host/proc/1/net/dev` (PID 1 = host netns). A bridge container reading its own `/proc/net/dev` sees ≈0 → silent under-metering. (See `host_nic.go`.)
- **Billable usage = max(inbound, outbound) per cycle, NOT rx+tx sum.** AWS Lightsail bills the higher direction. The meter tracks per-direction baselines (`cycle_start_rx` / `cycle_start_tx`) and reports `used = max(rxΔ, txΔ)`. (Summing rx+tx ~2× overstates and cuts at half the real billable transfer — fixed 2026-06-20, commit `f1b14843`.)
- **Cutoff = `docker pause` (freeze), not stop.** The enforcer polls every `K2_CUTOFF_POLL_INTERVAL` (5s) and pauses k2s at `used >= limit − 500 MiB` (the 500 MiB reserve **must** match Center's `quotaCutoffReserveBytes`). `limit == 0` = unlimited, never cut. State persists to `/etc/kaitu/cutoff.state` and re-applies on restart; recovery (`used < limit − reserve`) unpauses.
- **fail-closed**: 3 consecutive meter-read failures with a known limit > 0 → enforcer pauses. The reporter never POSTs on a meter error (never reports a false 0).
- **All nodes meter** (no private-claim gate). A node meters iff it has `K2_NODE_BILLING_START_DATE`; without it, metering is disabled and the node runs uncapped (bounded only by the provider bundle).

## Quota env vars (`/apps/kaitu-slave/.env`, consumed by the sidecar)

| Var | Meaning | Notes |
|-----|---------|-------|
| `K2_NODE_BILLING_START_DATE` | Cycle anchor, **`yyyy-MM-dd`** (day-of-month extracted) | **Required to meter.** Empty → metering off. Bad format → sidecar errors. |
| `K2_NODE_TRAFFIC_LIMIT_GB` | Monthly quota (GiB) | `0` = unlimited (never cut) = safe fallback. Cut trips at `limit − 500 MiB`. |
| `K2_NODE_TRAFFIC_USED_GB` | **Mid-cycle onboarding seed** (GiB already used) | Applied **once on first boot** (no persisted state). Never re-applied on restart or cycle reset. `0` = none. Prefer `set-usage` for later edits. |
| `K2_VERSION` | image tag for **both** images | If sidecar/k2s need different tags, pin the k2s `image:` line directly in the compose and let `K2_VERSION` drive the sidecar. |
| `K2_CUTOFF_POLL_INTERVAL` | enforcer poll period | default `5s`. |

> The reporter cadence is **owned by Center** (it returns `next_report_interval`, 10s floor) — do not tune it from the node.

---

## Part A — Build & publish the sidecar image

The Dockerfile just copies a **pre-built** `linux/amd64` binary into Alpine. CI (`.github/workflows/release-k2s.yml`, tag `v*-k2s` or manual dispatch) is the normal path. To build **out-of-band** (e.g. metering code committed but no CI image yet):

```bash
# 1. From repo root, confirm the code builds & tests pass
cd docker/sidecar && go test -race ./...

# 2. Build the linux/amd64 binary (Dockerfile expects ./k2-sidecar)
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -ldflags "-s -w" -o k2-sidecar .

# 3. Login to ECR Public (always us-east-1) and push a PINNED tag
aws ecr-public get-login-password --region us-east-1 \
  | docker login --username AWS --password-stdin public.ecr.aws
docker buildx build --platform linux/amd64 \
  -t public.ecr.aws/d6n9t2r2/k2-sidecar:v<pkgVersion>-<gitShortSHA> --push .

# 4. Verify
aws ecr-public describe-images --repository-name k2-sidecar --region us-east-1 \
  --query 'reverse(sort_by(imageDetails,&imagePushedAt))[:3].{tags:imageTags,pushed:imagePushedAt}' --output json
```

- **Tag convention**: `v<package.json version>-<git short sha of build commit>` (e.g. `v0.4.6-f1b14843`). Provenance = the SHA.
- **NEVER push `:latest`** unless you are deliberately doing the fleet rollout (task #76). `:latest` is what unpinned nodes auto-pull; moving it touches **every** node.
- The newest existing tags don't always contain your latest commit — check the build date vs your commit date before deploying (`git merge-base --is-ancestor <imgSHA> <yourSHA>`).
- k2s image: only rebuild if k2s code changed; otherwise reuse the last `k2s:v0.4.6-<sha>`.

---

## Part B — Deploy / upgrade the sidecar on a node

Assumes the node is already provisioned (`provision-node.sh` run, SSH on **1022**, `/apps/kaitu-slave/` exists). For a brand-new private node use `private-node-provisioning` instead.

```bash
SSH="ssh -o StrictHostKeyChecking=no -p 1022 ubuntu@<ip>"

# 1. Push the current compose (has restart:unless-stopped + healthcheck + quota env passthrough)
cat docker/docker-compose.yml | $SSH 'sudo tee /apps/kaitu-slave/docker-compose.yml >/dev/null'

# 2. If sidecar & k2s need different tags, pin k2s explicitly (K2_VERSION drives sidecar):
$SSH 'sudo sed -i "s#k2s:\${K2_VERSION:-latest}#k2s:v0.4.6-<k2sSHA>#" /apps/kaitu-slave/docker-compose.yml'

# 3. Set the version + quota in .env (heredoc; NEVER echo K2_NODE_SECRET / K2_PRIVATE_CLAIM)
$SSH 'sudo sed -i "s/^K2_VERSION=.*/K2_VERSION=v0.4.6-<sha>/" /apps/kaitu-slave/.env'
# add if missing (idempotent): K2_NODE_BILLING_START_DATE, K2_NODE_TRAFFIC_LIMIT_GB

# 4. Pull (public ECR, no auth needed) + up. NEVER `down`.
$SSH 'cd /apps/kaitu-slave && sudo docker compose pull && sudo docker compose up -d --remove-orphans'
```

**Upgrading from a pre-per-direction sidecar**: the state format changed `cycle_start_bytes` → `cycle_start_rx`/`cycle_start_tx`. On first boot the new code can't split the legacy field, so it **re-anchors once** (in-cycle usage resets to 0). If the real mid-cycle usage matters, set it back with `set-usage` (Part C).

---

## Part C — Configure quota & seed mid-cycle usage

Setting the limit and billing date is just `.env` + restart. The interesting case is **a node onboarded mid-cycle that already used N GB** — the meter would otherwise start at 0.

**Editable, persistent usage record** (`/etc/kaitu/traffic.state`, survives restart, never auto-reset):

```bash
# Declare "this node has already used 920 GB this cycle"
$SSH 'sudo docker exec k2-sidecar k2-sidecar -c /tmp/sidecar-config.yaml set-usage 920'
# set-usage rewrites the persisted baseline but the RUNNING process holds it in memory:
$SSH 'cd /apps/kaitu-slave && sudo docker compose restart k2-sidecar'   # load it
```

- `set-usage <GB>` reads the current NIC counters and sets the per-direction baselines so the meter immediately reports `<GB>` used (clamps a direction whose counter is below `<GB>` to 0; the dominant direction carries it).
- It **persists** to `traffic.state` → a later restart keeps it.
- For automated provisioning instead, set `K2_NODE_TRAFFIC_USED_GB=<GB>` in `.env` before first boot (seeds once).
- Get the real figure from the provider: AWS Lightsail month-to-date is `max(NetworkOut, NetworkIn)`:
  ```bash
  aws lightsail get-instance-metric-data --region <r> --instance-name <name> \
    --metric-name NetworkOut --period 86400 --start-time <cycleStart> --end-time <now> \
    --unit Bytes --statistics Sum --query 'metricData[].sum' --output text \
    | tr '\t' '\n' | awk '{s+=$1} END{printf "%.0f GB\n", s/1024/1024/1024}'
  ```

`traffic.state` format: `{"billing_cycle_end_at":<unix>,"cycle_start_rx":<bytes>,"cycle_start_tx":<bytes>}`.

---

## Part D — Observe the usage recorder

```bash
# Sidecar: registration + metering + reporter
$SSH 'sudo docker logs --tail 80 k2-sidecar 2>&1 | grep -iE "Registration completed|Traffic monitor initialized|usage-reporter|cutoff-enforcer-start|cutoff-(un)?paused"'
```

Healthy markers:
- `Traffic monitor initialized ... billingDate=... limitGB=... rx=... tx=...` (rx/tx read separately)
- `DIAG: usage-reporter-start` then periodic `DIAG: usage-reporter-cycle-ok epoch=<cycleEnd> cumulative=<usedBytes> quotaTotal=<limitBytes>` — `cumulative` should climb monotonically; interval ≈ Center's `next_report_interval` (~60s), not the local default.
- `cumulative` = `max(rxΔ, txΔ)`; `quotaTotal` = `limit × 2^30`.

Center side (recorder):
- `list_nodes(name=<node>)` shows the node (`protocol` displays as `k2s`).
- The Center DB the node reports to is `K2_CENTER_URL` in its `.env` (dev/test = `https://k2.52j.me`). **The `mysql-dev` MCP is NOT necessarily that Center's DB** — verify by checking the node actually appears there before trusting a query. Prefer the `kaitu-center` MCP (`list_nodes`) for cross-checking.
- A `usage-reporter-cycle-ok` line means Center returned a 2xx ack (the reporter only logs ok on success) → end-to-end recorder confirmed even without DB access.

---

## Part E — Operate & test the cutoff

How it behaves: every 5s the enforcer reads the shared TrafficMonitor; at `used >= limit − 500 MiB` it `docker pause`s k2s, persists `cutoff.state {"cut":true}`, and keeps reporting. When `used < limit − 500 MiB` (e.g. limit raised, or new cycle) it `docker unpause`s and writes `{"cut":false}`.

**Test it on a node with no real users** (e.g. the AU smoke box):

```bash
# Trigger: set limit at/below current used → pause within 5s
$SSH 'sudo sed -i "s/^K2_NODE_TRAFFIC_LIMIT_GB=.*/K2_NODE_TRAFFIC_LIMIT_GB=<≈used>/" /apps/kaitu-slave/.env; cd /apps/kaitu-slave && sudo docker compose up -d k2-sidecar'
$SSH 'sleep 8; sudo docker logs --tail 5 k2-sidecar 2>&1 | grep cutoff-paused; sudo docker inspect -f "paused={{.State.Paused}}" k2s'

# Recover: raise the limit → unpause
$SSH 'sudo sed -i "s/^K2_NODE_TRAFFIC_LIMIT_GB=.*/K2_NODE_TRAFFIC_LIMIT_GB=2048/" /apps/kaitu-slave/.env; cd /apps/kaitu-slave && sudo docker compose up -d k2-sidecar'
$SSH 'sleep 8; sudo docker logs --tail 5 k2-sidecar 2>&1 | grep cutoff-unpaused; sudo docker inspect -f "paused={{.State.Paused}}" k2s'
```

The limit is read at TrafficMonitor construction, so changing `K2_NODE_TRAFFIC_LIMIT_GB` requires `up -d` / `restart` of the sidecar to take effect.

---

## Guardrails

1. **Secrets**: never echo / log `K2_NODE_SECRET` or `K2_PRIVATE_CLAIM`. Write only via heredoc. Redact when printing `.env` (`sed -E "s/(SECRET|CLAIM|TOKEN)=.*/\1=<redacted>/"`).
2. **Pinned tags only**; never move `:latest` outside the deliberate fleet rollout.
3. **`pull + up -d`, never `down`.** Use `--remove-orphans` to clear stale `k2v5`/`k2v4-slave` containers after the k2v5→k2s rename.
4. **Cost**: pick a node bundle whose included transfer exceeds the configured quota, so the node-side cutoff trips before provider overage. AWS Lightsail bundles in `ap-southeast-2`: `micro_3_2`=1024 GB/$7, `small_3_2`=1536 GB/$12, `medium_3_2`=2048 GB/$24.
5. **Reserve constant parity**: the 500 MiB cutoff reserve is duplicated in `docker/sidecar/sidecar/enforcer.go` and Center `api/logic_node_usage.go` — if you change one, change both.
6. **Don't touch other nodes** — only operate the node you were asked to.
