# Traffic metering + quota cutoff (node-authority)

Reference for `kaitu-node-ops`. Use when building, deploying, configuring, observing, or testing the **traffic metering + quota cutoff** system.

**Model:** the **node is the single authority**. The `k2-sidecar` self-meters the host NIC, owns the monthly cycle, and **hard-cuts the k2s data plane locally** (`docker pause`) when over quota. Center (`/slave/usage` → `NodeUsage`) is a **passive recorder** — it does not command the cut; it only mirrors usage and, derived from that, hides over-quota/offline nodes from `/api/tunnels` + `/api/subs`.

## Metering architecture facts (read first)

- **Lives entirely in the sidecar.** k2s is not involved. Metering-only change → rebuild **sidecar** only; k2s stays on its tag.
- **Reads the HOST NIC, not the container veth.** Compose mounts `/proc`→`/host/proc`; the sidecar reads `/host/proc/1/net/dev` (PID 1 = host netns). A bridge container reading its own `/proc/net/dev` sees ≈0 → silent under-metering (`host_nic.go`).
- **Billable usage = `max(inbound, outbound)` per cycle — NOT rx+tx sum, NOT outbound-only.** AWS Lightsail's rule is the **greater of** total inbound vs total outbound for the month — it does not add them, and it does not bill outbound alone. The meter mirrors this: per-direction baselines (`cycle_start_rx`/`cycle_start_tx`), reports `used = max(rxΔ, txΔ)`. (Summing rx+tx ~2× overstates and cuts at half the real transfer — fixed 2026-06-20 `f1b14843`.)
  - **Why not just outbound:** the binding direction can flip. A normal VPN-egress node is outbound-dominant (tx > rx, billed on tx), but a receive/download-heavy node can be inbound-dominant (rx > tx) — then AWS bills on *inbound*. An outbound-only meter would silently under-count it and never cut. `max(rx,tx)` is safe for both. (Observed 2026-06-22 on au-1: NetworkOut 999.7 GiB > NetworkIn 916.5 GiB → tx binding, matched AWS CLI to ~0.02%.)
- **AWS Lightsail billing cycle = CALENDAR MONTH.** Allowance resets on the **1st of each calendar month, 00:00 UTC** (verified 2026-06-22: node `epoch` == `aws lightsail` reset == `1782864000` = 2026-07-01). So `K2_NODE_BILLING_START_DATE` should pin **day-of-month `01`** on Lightsail nodes. Cross-check used with `aws lightsail get-instance-metric-data` (NetworkOut is usually the binding direction).
- **⚠ First-month proration** — see the calculation method below. Lightsail prorates the allowance for an instance created mid-month; the node meter does NOT model it.
- **Cutoff = `docker pause` (freeze), not stop.** Enforcer polls every `K2_CUTOFF_POLL_INTERVAL` (5s); pauses k2s at `used ≥ limit − 500 MiB` (the reserve **must** match Center's `quotaCutoffReserveBytes`). `limit == 0` = unlimited. State persists to `/etc/kaitu/cutoff.state`, re-applies on restart; recovery (`used < limit − reserve`) unpauses.
- **fail-closed:** 3 consecutive meter-read failures with a known limit > 0 → enforcer pauses. The reporter never POSTs on a meter error (never reports a false 0).
- **All nodes meter** (no private-claim gate). A node meters iff it has `K2_NODE_BILLING_START_DATE`; without it, metering is off and it runs uncapped (bounded only by the provider bundle).

Quota env vars are in the hub `SKILL.md` §2 (`K2_NODE_BILLING_START_DATE`, `K2_NODE_TRAFFIC_LIMIT_GB`, `K2_NODE_TRAFFIC_USED_GB`, `K2_CUTOFF_POLL_INTERVAL`, `K2_VERSION`).

---

## Part A — Build & publish the sidecar image

Dockerfile copies a **pre-built** `linux/amd64` binary into Alpine. CI (`.github/workflows/release-k2s.yml`, tag `v*-k2s` or manual dispatch) is the normal path. Out-of-band build (code committed, no CI image yet):

```bash
cd docker/sidecar && go test -race ./...                                            # 1. build & test
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -ldflags "-s -w" -o k2-sidecar .     # 2. binary
aws ecr-public get-login-password --region us-east-1 \                              # 3. login + push PINNED tag
  | docker login --username AWS --password-stdin public.ecr.aws
docker buildx build --platform linux/amd64 \
  -t public.ecr.aws/d6n9t2r2/k2-sidecar:v<pkgVersion>-<gitShortSHA> --push .
aws ecr-public describe-images --repository-name k2-sidecar --region us-east-1 \    # 4. verify
  --query 'reverse(sort_by(imageDetails,&imagePushedAt))[:3].{tags:imageTags,pushed:imagePushedAt}' --output json
```

- **Tag**: `v<package.json version>-<git short sha>` (e.g. `v0.4.6-f1b14843`). Provenance = the SHA.
- **NEVER `:latest`** outside the deliberate fleet rollout (task #76) — it's what unpinned nodes auto-pull.
- Newest tag ≠ your latest commit — check `git merge-base --is-ancestor <imgSHA> <yourSHA>` before deploying.
- k2s image: only rebuild if k2s code changed; else reuse the last `k2s:v0.4.6-<sha>`.

Deploy/upgrade onto a node = hub `SKILL.md` §4 (push compose, edit `.env`, `pull + up -d`). Upgrading from a pre-per-direction sidecar: state format changed `cycle_start_bytes` → `cycle_start_rx`/`cycle_start_tx`; first boot re-anchors once (in-cycle usage → 0). Restore real usage with `set-usage` (Part C).

---

## Part C — Configure quota & seed mid-cycle usage

Limit + billing date = `.env` + `up -d`. The interesting case is a node onboarded mid-cycle that already used N GB (meter would start at 0).

**Editable, persistent usage** (`/etc/kaitu/traffic.state`, survives restart, never auto-reset):

```bash
$SSH 'sudo docker exec k2-sidecar k2-sidecar -c /tmp/sidecar-config.yaml set-usage 920'   # declare 920 GB used
$SSH 'cd /apps/kaitu-slave && sudo docker compose restart k2-sidecar'                      # running proc loads it
```

- `set-usage <GB>` reads current NIC counters, sets per-direction baselines so the meter immediately reports `<GB>` (clamps a direction below `<GB>` to 0; dominant direction carries it). **Persists** → survives restart.
- For provisioning instead, set `K2_NODE_TRAFFIC_USED_GB=<GB>` before first boot (seeds once).
- `traffic.state` = `{"billing_cycle_end_at":<unix>,"cycle_start_rx":<bytes>,"cycle_start_tx":<bytes>}`.

### First-month proration (AWS Lightsail) — calculation method

Lightsail bills per **calendar month** and **prorates the allowance for the creation month**. The node meter enforces a full-month limit and doesn't know about proration, so for the **creation month only** compute the prorated allowance and set `K2_NODE_TRAFFIC_LIMIT_GB` to it; restore the full limit next cycle.

```bash
# 1. Creation date (proration anchor):
aws lightsail get-instances --region <r> --profile default \
  --query "instances[?name=='<name>'].createdAt" --output text     # e.g. 2026-06-07T...
# 2. prorated_GB ≈ bundle_transferGB × (days_from_createdAt_to_month_end / days_in_month)   (round down)
```

**Worked example (au-1, 2026-06-22):** bundle `micro_3_2` = 1024 GB; created 2026-06-07; June = 30d; ~24d remain → prorated ≈ `1024 × 24/30 ≈ 819 GB`. au-1 ran at the full `K2_NODE_TRAFFIC_LIMIT_GB=1000` → cut at ~999.5 GiB, **above** the ~819 GB prorated free tier → ~180 GB AWS overage (≈ $0.09–0.14/GB → ~$17–27; accepted on a dev box). Correct first-month value would have been `≈800` (leave headroom for the 500 MiB reserve), then `1000` from 2026-07-01.

> Only the **creation month** needs this. Every subsequent full month: node cycle (day `01`) + a full-month limit `< bundle` align with AWS automatically.

---

## Part D — Observe the usage recorder

```bash
$SSH 'sudo docker logs --tail 80 k2-sidecar 2>&1 | grep -iE "Registration completed|Traffic monitor initialized|usage-reporter|cutoff-enforcer-start|cutoff-(un)?paused"'
```

Healthy markers:
- `Traffic monitor initialized … billingDate=… limitGB=… rx=… tx=…` (rx/tx read separately)
- `DIAG: usage-reporter-start`, then periodic `DIAG: usage-reporter-cycle-ok epoch=<cycleEnd> cumulative=<usedBytes> quotaTotal=<limitBytes>` — `cumulative` climbs monotonically; interval ≈ Center's `next_report_interval` (~60s), not the local default. `cumulative = max(rxΔ,txΔ)`; `quotaTotal = limit × 2^30`.

Center side:
- `list_nodes(name=<node>)` shows it (`protocol` displays `k2s`). The Center the node reports to is `K2_CENTER_URL` in `.env` (dev/test = `https://k2.52j.me`). **`mysql-dev` MCP is NOT necessarily that Center's DB** — verify the node appears there before trusting a query; prefer `list_nodes`.
- A `usage-reporter-cycle-ok` line = Center returned 2xx → end-to-end recorder confirmed even without DB access.

---

## Part E — Operate & test the cutoff

Every 5s the enforcer reads the shared TrafficMonitor; at `used ≥ limit − 500 MiB` it `docker pause`s k2s, persists `cutoff.state {"cut":true}`, keeps reporting. When `used < limit − 500 MiB` (limit raised / new cycle) it `docker unpause`s, writes `{"cut":false}`.

**Test on a node with no real users** (e.g. the AU smoke box):

```bash
# Trigger: set limit at/below current used → pause within 5s
$SSH 'sudo sed -i "s/^K2_NODE_TRAFFIC_LIMIT_GB=.*/K2_NODE_TRAFFIC_LIMIT_GB=<≈used>/" /apps/kaitu-slave/.env; cd /apps/kaitu-slave && sudo docker compose up -d k2-sidecar'
$SSH 'sleep 8; sudo docker logs --tail 5 k2-sidecar 2>&1 | grep cutoff-paused; sudo docker inspect -f "paused={{.State.Paused}}" k2s'
# Recover: raise the limit → unpause
$SSH 'sudo sed -i "s/^K2_NODE_TRAFFIC_LIMIT_GB=.*/K2_NODE_TRAFFIC_LIMIT_GB=2048/" /apps/kaitu-slave/.env; cd /apps/kaitu-slave && sudo docker compose up -d k2-sidecar'
$SSH 'sleep 8; sudo docker logs --tail 5 k2-sidecar 2>&1 | grep cutoff-unpaused; sudo docker inspect -f "paused={{.State.Paused}}" k2s'
```

The limit is read at TrafficMonitor construction → changing `K2_NODE_TRAFFIC_LIMIT_GB` needs `up -d` / `restart` of the sidecar.

---

## Metering guardrails (on top of hub §0)

- **Reserve constant parity:** the 500 MiB cutoff reserve is duplicated in `docker/sidecar/sidecar/enforcer.go` and Center `api/logic_node_usage.go` — change one, change both.
- **Cost = bundle sizing:** pick a bundle whose included transfer exceeds the configured quota so the node-side cutoff trips before provider overage. Lightsail `ap-southeast-2`: `micro_3_2`=1024 GB/$7, `small_3_2`=1536 GB/$12, `medium_3_2`=2048 GB/$24.
