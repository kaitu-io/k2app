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

## Part B — Per-provider quota index

The three quota knobs are provider-specific **facts**, not guesses. Don't hand-pick a `LIMIT_GB` and a `01` reset day for every node — **derive them per provider** from the provider's own panel/API at provision time, then write into `.env` (vars in hub `SKILL.md` §2). This is the "deduct from actual usage" model: seed the meter to the provider's real current-cycle usage instead of anchoring fresh to 0.

**Reset-shape constraint (the correctness hinge):** the cutoff code models exactly **one** reset shape — *monthly, on the day-of-month of `K2_NODE_BILLING_START_DATE`, 00:00 UTC* (`traffic.go calculateNextCycleEnd`). Any provider whose reset is monthly-on-a-fixed-day fits (just set the right day). A provider with **30-day-rolling / weekly / non-fixed** reset **cannot** be metered correctly → see fallback.

Three knobs per node:

| `.env` var | Source (per provider, below) | Notes |
|------------|------------------------------|-------|
| `K2_NODE_BILLING_START_DATE` | provider's cycle reset date | **only the day-of-month matters**; pins the monthly reset day |
| `K2_NODE_TRAFFIC_LIMIT_GB` | bundle transfer allowance − headroom | cut trips below provider overage; meter bills `max(rx,tx)` |
| `K2_NODE_TRAFFIC_USED_GB` | provider's **current-cycle used** (seed once) | mid-cycle onboarding / existing-node migration; `0` on a fresh instance. Or `set-usage` live (Part C). |

### AWS Lightsail
- **Reset:** calendar month, **1st 00:00 UTC** → `BILLING_START_DATE` day = `01` (verified 2026-06-22: node epoch == `aws lightsail` reset).
- **Accounting:** `max(inbound, outbound)` per month — **matches the meter exactly**.
- **Bundles (`ap-southeast-2`):** `micro_3_2`=1024 GB/$7 · `small_3_2`=1536 GB/$12 · `medium_3_2`=2048 GB/$24. Set `LIMIT_GB < bundle` (≥1 GB over the 500 MiB reserve).
- **Creation month:** prorate `LIMIT_GB` (Part C) — **only** that month; full limit from the next 1st.
- **Already-used:** `aws lightsail get-instance-metric-data` (NetworkOut usually binding) → seed `TRAFFIC_USED_GB`. Fresh instance ≈ 0.

### Bandwagon (搬瓦工 / KiwiVM)
- **Reset:** monthly on the **plan's** reset day — **NOT the 1st**. Read the exact "Next reset" date from the KiwiVM panel (or `getServiceInfo` / `getRawUsageStats` API) → set `BILLING_START_DATE` day = that date's day-of-month. It is monthly-on-a-day → fits the meter. **`api/cloudprovider/bandwagon.go`'s synced `data_next_reset` (surfaced as `cloud_instances.traffic_reset_at`) is a separate, BWH-side metering-counter timestamp — it can drift up to ~1 day earlier than the plan's actual reset day and must NOT be used to derive `BILLING_START_DATE`.** Confirmed 2026-07-09: two live nodes (93.179.114.62, 93.179.114.208) had `BILLING_START_DATE` day=09 while the account's real reset day is 10 — read the day off the KiwiVM panel directly, not off `traffic_reset_at`.
- **Accounting — VERIFY before sizing, do not assume:** confirm from the KiwiVM plan whether bandwidth counts `max(in,out)`, **in+out sum**, or outbound-only. The meter uses `max(rx,tx)`; if the plan **sums** in+out, the meter under-counts ~2× → set `LIMIT_GB ≈ allowance/2` headroom (or accept the divergence and document it on the node). **A short-window field measurement is NOT reliable evidence** — on 2026-07-09, comparing sidecar `cumulative` growth against `cloud_instances.traffic_used_gb` growth over a ~40min window on two live nodes gave BWH/sidecar rate ratios of 1.54× and 1.22× (noisy, inconsistent, nowhere near a clean 1× or 2×) — traffic bursts and rx/tx asymmetry dominate short windows. Get the answer from KiwiVM's plan docs/support, or observe over many hours, not a 10-40min sample.
- **Allowance:** the plan's "Monthly Data Transfer" → `LIMIT_GB <` that.
- **Already-used:** KiwiVM "used / total" (or API `data_counter`) → seed `TRAFFIC_USED_GB`. **Essential** when migrating an existing 搬瓦工 box mid-cycle — without it the meter starts at 0 and never aligns to the panel.
- **⚠ Provider-side network suspension at 100%:** BWH/KiwiVM may cut the VPS's network entirely (not just throttle) once its own counter hits the plan's cap — observed 2026-07-09 on two nodes at `cloud_instances.traffic_ratio == 1.0`: both became fully TCP-unreachable (SSH connect timeout on the hardened port, confirmed with a raw `ping_node` TCP probe, not just an auth failure). This is a **different, harsher cutoff than our own `docker pause` enforcer** — our node-side cutoff never blocks SSH or the host network, only the `k2s` container. If `LIMIT_GB` (minus the 500 MiB reserve) is set looser than what BWH itself will tolerate, the account-level suspension fires first and the node goes dark until BWH's own reset or a manual data-transfer top-up. Size `LIMIT_GB` with this in mind, not just to avoid overage billing.

### Any other provider (fallback)
- Read the provider's stated **reset date + allowance + accounting model** from its console/API.
- **Monthly on a fixed day** → set `BILLING_START_DATE` to that day; done.
- **30-day-rolling / weekly / non-fixed** → the meter can't model it. Either (a) leave `K2_NODE_BILLING_START_DATE` **unset** → node runs **uncapped** (bundle-only, provider overage possible — see facts §17), or (b) accept a calendar-month approximation and size `LIMIT_GB` conservatively. **Record the choice** on the node.
- Always confirm accounting (`max` vs sum vs outbound) and seed `TRAFFIC_USED_GB` from the provider's current-cycle figure.

---

## Part C — Configure quota & seed mid-cycle usage

Limit + billing date = `.env` + `up -d`. The interesting case is a node onboarded mid-cycle that already used N GB (meter would start at 0).

**Editable, persistent usage** (`/etc/kaitu/traffic.state`, survives restart, never auto-reset):

```bash
$SSH 'sudo docker exec k2-sidecar k2-sidecar -c /tmp/sidecar-config.yaml set-usage 920'   # declare 920 GB used
$SSH 'cd /apps/k2s && sudo docker compose restart k2-sidecar'                      # running proc loads it
```

- `set-usage <GB>` records `<GB>` as the cycle's **prior-used floor** (`prior_used_bytes`) and anchors the per-direction baseline at the **current** NIC (live delta starts at 0). Billable `used = prior_used_bytes + max(rxΔ, txΔ)`. **Persists** → survives restart; **zeroed on cycle rollover** (the seed never carries into next month).
- **Works on a fresh node where `<GB>` exceeds the NIC counter** — this is the key fix (`prior_used_bytes`, 2026-06-23). A node created mid-cycle (NIC ~1 GiB) can still declare e.g. `set-usage 751`. The old "baseline = NIC − used" math clamped to 0 and the seed silently evaporated on such nodes.
- **Mid-cycle join → set the FULL-month LIMIT, not the prorated remainder.** AWS prorates the first month; the proration-clean way to model it is `K2_NODE_TRAFFIC_LIMIT_GB=<full month>` + seed the consumed/phantom portion so the remaining month = `limit − seed`. At the 1st-of-month rollover the seed clears and the node opens to the full month automatically — no manual limit bump. Example: 1000 GB/mo bundle joined on the 23rd → `LIMIT_GB=1000` + `set-usage 751` → ~250 GB this month, full 1000 in the next.
- For provisioning instead, set `K2_NODE_TRAFFIC_USED_GB=<GB>` before first boot (seeds once, same prior-used model).
- `traffic.state` = `{"billing_cycle_end_at":<unix>,"cycle_start_rx":<bytes>,"cycle_start_tx":<bytes>,"prior_used_bytes":<bytes>}` (legacy files without `prior_used_bytes` → 0 → old delta-only behavior).

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
$SSH 'sudo sed -i "s/^K2_NODE_TRAFFIC_LIMIT_GB=.*/K2_NODE_TRAFFIC_LIMIT_GB=<≈used>/" /apps/k2s/.env; cd /apps/k2s && sudo docker compose up -d k2-sidecar'
$SSH 'sleep 8; sudo docker logs --tail 5 k2-sidecar 2>&1 | grep cutoff-paused; sudo docker inspect -f "paused={{.State.Paused}}" k2s'
# Recover: raise the limit → unpause
$SSH 'sudo sed -i "s/^K2_NODE_TRAFFIC_LIMIT_GB=.*/K2_NODE_TRAFFIC_LIMIT_GB=2048/" /apps/k2s/.env; cd /apps/k2s && sudo docker compose up -d k2-sidecar'
$SSH 'sleep 8; sudo docker logs --tail 5 k2-sidecar 2>&1 | grep cutoff-unpaused; sudo docker inspect -f "paused={{.State.Paused}}" k2s'
```

The limit is read at TrafficMonitor construction → changing `K2_NODE_TRAFFIC_LIMIT_GB` needs `up -d` / `restart` of the sidecar.

---

## Metering guardrails (on top of hub §0)

- **Reserve constant parity:** the 500 MiB cutoff reserve is duplicated in `docker/sidecar/sidecar/enforcer.go` and Center `api/logic_node_usage.go` — change one, change both.
- **Cost = bundle sizing:** pick a bundle whose included transfer exceeds the configured quota so the node-side cutoff trips before provider overage. Per-provider bundle/allowance + the correct reset day + already-used seeding are in **Part B** (don't hardcode `01` / a guessed `LIMIT_GB` — derive per provider).
- **Audit for stale pre-2026-06-19 values:** before that date (commit `47c5c5e8`) the convention was `LIMIT_GB ≈ 0.95 × provider_quota` (shared-pool 95% soft-hide model, since retired). Any node's `.env` still carrying a value that's a clean 0.95× multiple of its provider's real quota (e.g. `1900` against a `2000` GB plan) predates the redesign and was never migrated to the current `limit ≈ provider_quota − 500 MiB` convention — confirmed on two BWH nodes 2026-07-09. Audit `K2_NODE_TRAFFIC_LIMIT_GB` on older nodes, not just new provisions.
- **`docker compose restart` does NOT reload `.env`** — Compose only re-reads env files on `up -d` (which recreates the container); `restart` reuses the already-created container with its old env. Always use `up -d <service>` after editing `.env`, never `restart`, or the config change silently no-ops.
