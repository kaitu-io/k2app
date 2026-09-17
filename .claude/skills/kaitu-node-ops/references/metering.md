# Traffic metering + quota cutoff (node-authority)

Reference for `kaitu-node-ops`. Use when building, deploying, configuring, observing, or testing the **traffic metering + quota cutoff** system.

**Model:** the **node is the single authority**. The `k2-sidecar` self-meters the host NIC, owns the monthly cycle, and **hard-cuts the k2s data plane locally** (`docker pause`) when over quota. Center (`/slave/usage` → `NodeUsage`) is a **passive recorder** — it does not command the cut; it only mirrors usage and, derived from that, hides over-quota/offline nodes from `/api/tunnels` + `/api/subs`.

## Metering architecture facts (read first)

- **Lives entirely in the sidecar.** k2s is not involved. Metering-only change → rebuild **sidecar** only; k2s stays on its tag.
- **Reads the HOST NIC, not the container veth.** Compose mounts `/proc`→`/host/proc`; the sidecar reads `/host/proc/1/net/dev` (PID 1 = host netns). A bridge container reading its own `/proc/net/dev` sees ≈0 → silent under-metering (`host_nic.go`).
- **How deltas combine = `K2_NODE_TRAFFIC_BILLING_MODE`** (per-direction baselines `cycle_start_rx`/`cycle_start_tx`, combined at read time, `traffic.go computeUsedLocked`):
  - `sum` → `used = rxΔ + txΔ`. **Required on every AWS Lightsail node.**
  - `max` (or **empty = default**) → `used = max(rxΔ, txΔ)`. For outbound-billed providers only. A typo'd non-empty value fails closed to `sum`.
- **AWS Lightsail: the allowance is consumed by inbound + outbound (SUM); only the outbound share of an overage is charged.** AWS FAQ verbatim: *"Both data transfer in and data transfer out of your instance count toward your data transfer allowance"* / *"You will only get charged for data transfer OUT … data transfer IN … [is] free beyond your data transfer allowance."* So the **cut point** is decided by in+out; the **overage bill** ≈ excess × out/(in+out) (~53% on a VPN node).
  - **History — do not revert to `max`:** 2026-06-20 `f1b14843` switched the meter to `max(in,out)` on the belief that Lightsail bills the greater direction. That was wrong: fleet self-reports matched NetworkOut alone, au-2 reached 152% of allowance, and overage bills followed (APS2: Jun $180, Jul $74, Aug $40). Fixed 2026-08-20 `c0b6a339` (sidecar `sum` mode + Center in+out + authoritative ratchet + autostop). Cross-check that holds: APS2 Jul, 3×1024 GB bundles, in+out 3905.9 GB → excess 833.9 × out-share 0.528 ≈ 441 GB vs billed `DataXfer-Out-Overage` 435.3 GB; outbound alone (2064 GB) would never have exceeded 3072 GB.
- **Three figures, measured 2026-09-17 (same window, day by day):** CloudWatch `NetworkIn+NetworkOut` (Center's number) = baseline; **Cost Explorer billed GB ≈ CloudWatch bytes/2^30 × 0.984** (−1.3…−2.0%/day, APS2 + USW2; the CE "GB" is GiB — decimal would be ~9% off); host-NIC counters = CloudWatch −0.7…−1.4% (almost all on inbound, −1.5…−2.3%; tx ±0.1%). Net: a node ratcheted to CloudWatch **over-counts the bill by ~1.5% — the safe direction.** At a 1000 GiB limit the node cuts at ≈ 983.5 billed GB on a 1024 GB bundle (~40 GB headroom).
- **Center authoritative ratchet (AWS shared-pool only):** each `/slave/usage` response may carry `authoritative_used_bytes` = the synced CloudWatch in+out figure (cloud sync every 30 min; gated on same calendar-month cycle, sync < 2 h old, non-private instance, figure > self-report). The sidecar adopts it **one-way up** (`AdoptAuthoritativeUsed` → becomes `prior_used_bytes`, NIC baseline re-anchored) — logs `DIAG: usage-reporter-adopted-authoritative`, typically every 30–60 min, a few MB–tens of MB each. So on AWS the reported `cumulative` tracks CloudWatch, not the raw NIC. **Private (dedicated-line) instances get no ratchet and no autostop** — NIC meter only.
- **AWS Lightsail billing cycle = CALENDAR MONTH.** Allowance resets on the **1st of each calendar month, 00:00 UTC** (verified 2026-06-22: node `epoch` == `aws lightsail` reset == `1782864000` = 2026-07-01). So `K2_NODE_BILLING_START_DATE` should pin **day-of-month `01`** on Lightsail nodes. Cross-check used with `aws lightsail get-instance-metric-data` — **sum NetworkIn + NetworkOut**; billed figures via `aws ce get-cost-and-usage` usage types `<REGION>-TotalDataXfer-{In,Out}-Bytes` and `<REGION>-DataXfer-Out-Overage-Bytes` (region-level only, ~1 day lag).
- **Center-side backstop (`api/worker_cloud_overage.go`, runs each cloud sync):** Slack `cloud-alerts` at 80% / 95% of the CloudWatch in+out vs bundle allowance, reconcile alert when provider ≫ self-report or the node goes silent, and at **≥100% `StopInstance`** (`cloud_instance.aws_overage_autostop`, default on; 100% CloudWatch ≈ 98.4% billed). Applies to **every** shared-pool Lightsail instance, including non-VPN boxes with no sidecar (e.g. `zabbix`) — they get stopped, not paused.
- **⚠ First-month proration** — see the calculation method below. Lightsail prorates the allowance (by hours) for an instance created mid-month; neither the node meter nor the Center ratchet models it, so a full-month `LIMIT_GB` on a mid-month node can overshoot the prorated allowance (e.g. created 09-02 ~03:00 UTC on 2048 GB → ≈1972 GB allowance vs ≈1967 billed GB at a 2000 GiB cut — 4 GB margin; any later start overshoots).
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
| `K2_NODE_TRAFFIC_LIMIT_GB` | bundle transfer allowance − headroom | cut trips below provider overage; combination per `K2_NODE_TRAFFIC_BILLING_MODE` |
| `K2_NODE_TRAFFIC_BILLING_MODE` | provider's accounting model | `sum` = in+out (AWS Lightsail); empty/`max` = greater direction |
| `K2_NODE_TRAFFIC_USED_GB` | provider's **current-cycle used** (seed once) | mid-cycle onboarding / existing-node migration; `0` on a fresh instance. Or `set-usage` live (Part C). |

### AWS Lightsail
- **Reset:** calendar month, **1st 00:00 UTC** → `BILLING_START_DATE` day = `01` (verified 2026-06-22: node epoch == `aws lightsail` reset).
- **Accounting:** allowance consumed by **inbound + outbound**; overage charged on the outbound share only → **`K2_NODE_TRAFFIC_BILLING_MODE=sum` is mandatory** (empty defaults to `max` and under-counts by ~half).
- **Bundles:** read `transferPerMonthInGb` per region (`aws lightsail get-bundles`) — e.g. `ap-southeast-2` `micro_3_2`=1024 GB; most other fleet regions `micro_3_0`=2048 GB; `ap-east-1` `micro_3_1`=1024 GB. Fleet convention: `LIMIT_GB` = 1000 on 1024 GB bundles, 2000 on 2048 GB bundles (cut ≈ 96% of the billed allowance, see the three-figures note above).
- **Creation month:** prorate `LIMIT_GB` (Part C) — **only** that month; full limit from the next 1st.
- **Already-used:** `aws lightsail get-instance-metric-data` (NetworkOut usually binding) → seed `TRAFFIC_USED_GB`. Fresh instance ≈ 0.

### Bandwagon (搬瓦工 / KiwiVM)
- **Reset:** monthly on the **plan's** reset day — **NOT the 1st**. Read the exact "Next reset" date from the KiwiVM panel (or `getServiceInfo` / `getRawUsageStats` API) → set `BILLING_START_DATE` day = that date's day-of-month. It is monthly-on-a-day → fits the meter. **`api/cloudprovider/bandwagon.go`'s synced `data_next_reset` (surfaced as `cloud_instances.traffic_reset_at`) is a separate, BWH-side metering-counter timestamp — it can drift up to ~1 day earlier than the plan's actual reset day and must NOT be used to derive `BILLING_START_DATE`.** Confirmed 2026-07-09: two live nodes (93.179.114.62, 93.179.114.208) had `BILLING_START_DATE` day=09 while the account's real reset day is 10 — read the day off the KiwiVM panel directly, not off `traffic_reset_at`.
- **Accounting — VERIFY before sizing, do not assume:** confirm from the KiwiVM plan whether bandwidth counts `max(in,out)`, **in+out sum**, or outbound-only. Leave `K2_NODE_TRAFFIC_BILLING_MODE` empty (`max`) for an outbound-billed plan; if the plan **sums** in+out, set `K2_NODE_TRAFFIC_BILLING_MODE=sum` (do **not** halve `LIMIT_GB` — that was the pre-mode workaround). **A short-window field measurement is NOT reliable evidence** — on 2026-07-09, comparing sidecar `cumulative` growth against `cloud_instances.traffic_used_gb` growth over a ~40min window on two live nodes gave BWH/sidecar rate ratios of 1.54× and 1.22× (noisy, inconsistent, nowhere near a clean 1× or 2×) — traffic bursts and rx/tx asymmetry dominate short windows. Get the answer from KiwiVM's plan docs/support, or observe over many hours, not a 10-40min sample.
- **Allowance:** the plan's "Monthly Data Transfer" → `LIMIT_GB <` that.
- **Already-used:** KiwiVM "used / total" (or API `data_counter`) → seed `TRAFFIC_USED_GB`. **Essential** when migrating an existing 搬瓦工 box mid-cycle — without it the meter starts at 0 and never aligns to the panel.
- **⚠ Provider-side network suspension at 100%:** BWH/KiwiVM may cut the VPS's network entirely (not just throttle) once its own counter hits the plan's cap — observed 2026-07-09 on two nodes at `cloud_instances.traffic_ratio == 1.0`: both became fully TCP-unreachable (SSH connect timeout on the hardened port, confirmed with a raw `ping_node` TCP probe, not just an auth failure). This is a **different, harsher cutoff than our own `docker pause` enforcer** — our node-side cutoff never blocks SSH or the host network, only the `k2s` container. If `LIMIT_GB` (minus the 500 MiB reserve) is set looser than what BWH itself will tolerate, the account-level suspension fires first and the node goes dark until BWH's own reset or a manual data-transfer top-up. Size `LIMIT_GB` with this in mind, not just to avoid overage billing.

### Any other provider (fallback)
- Read the provider's stated **reset date + allowance + accounting model** from its console/API.
- **Monthly on a fixed day** → set `BILLING_START_DATE` to that day; done.
- **30-day-rolling / weekly / non-fixed** → the meter can't model it. Either (a) leave `K2_NODE_BILLING_START_DATE` **unset** → node runs **uncapped** (bundle-only, provider overage possible — see facts §17), or (b) accept a calendar-month approximation and size `LIMIT_GB` conservatively. **Record the choice** on the node.
- Always confirm accounting (`max` vs sum vs outbound), set `K2_NODE_TRAFFIC_BILLING_MODE` to match, and seed `TRAFFIC_USED_GB` from the provider's current-cycle figure.

---

## Part C — Configure quota & seed mid-cycle usage

Limit + billing date = `.env` + `up -d`. The interesting case is a node onboarded mid-cycle that already used N GB (meter would start at 0).

**Editable, persistent usage** (`/etc/kaitu/traffic.state`, survives restart, never auto-reset):

```bash
$SSH 'sudo docker exec k2-sidecar k2-sidecar -c /tmp/sidecar-config.yaml set-usage 920'   # declare 920 GB used
$SSH 'cd /apps/k2s && sudo docker compose restart k2-sidecar'                      # running proc loads it
```

- `set-usage <GB>` records `<GB>` as the cycle's **prior-used floor** (`prior_used_bytes`) and anchors the per-direction baseline at the **current** NIC (live delta starts at 0). Billable `used = prior_used_bytes + (rxΔ + txΔ)` in `sum` mode, `prior_used_bytes + max(rxΔ, txΔ)` in `max` mode. **Persists** → survives restart; **zeroed on cycle rollover** (the seed never carries into next month).
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

**Worked example (au-1, 2026-06-22):** bundle `micro_3_2` = 1024 GB; created 2026-06-07; June = 30d; ~24d remain → prorated ≈ `1024 × 24/30 ≈ 819 GB`. au-1 ran at the full `K2_NODE_TRAFFIC_LIMIT_GB=1000` → cut at ~999.5 GiB, **above** the ~819 GB prorated free tier. Correct first-month value would have been `≈800` (leave headroom for the 500 MiB reserve), then `1000` from 2026-07-01. **Worse than it looked at the time:** the meter was then in `max` mode, so the 999.5 GiB was outbound alone (NetworkOut 999.7 / NetworkIn 916.5 GiB) — real in+out ≈ 1916 GiB. Cost Explorer for APS2 June: `DataXfer-Out-Overage-Bytes` 1058 GB = **$179.92**. Lightsail prorates by **hours** (`BundleUsage` is billed in Hrs), so the precise formula is `bundle × hours_remaining / hours_in_month`.

> Only the **creation month** needs this. Every subsequent full month: node cycle (day `01`) + a full-month limit `< bundle` align with AWS automatically.

---

## Part D — Observe the usage recorder

```bash
$SSH 'sudo docker logs --tail 80 k2-sidecar 2>&1 | grep -iE "Registration completed|Traffic monitor initialized|usage-reporter|cutoff-enforcer-start|cutoff-(un)?paused"'
```

Healthy markers:
- `Traffic monitor initialized … billingDate=… limitGB=… rx=… tx=…` (rx/tx read separately)
- `DIAG: usage-reporter-start`, then periodic `DIAG: usage-reporter-cycle-ok epoch=<cycleEnd> cumulative=<usedBytes> quotaTotal=<limitBytes>` — `cumulative` climbs monotonically; interval ≈ Center's `next_report_interval` (~60s), not the local default. `cumulative = prior_used_bytes + mode-combined deltas` (bytes — divide by 2^30 for GiB; `1,019,…` bytes is ~950 GiB, not "1019 GB"); `quotaTotal = limit × 2^30`; the cut fires at `cumulative ≥ quotaTotal − 524288000`. On AWS nodes also expect periodic `usage-reporter-adopted-authoritative` (the Center ratchet).

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
