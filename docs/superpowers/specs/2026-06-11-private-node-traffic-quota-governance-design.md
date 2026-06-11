# 专属节点流量额度治理 (Private-Node Traffic Quota Governance) — Design

> Spec date: 2026-06-11
> Decision owner: David (anc.info7@gmail.com)
> Status: **Approved cost model (A); design pending user spec review**

## 1. Context & The Decision

专属节点 (private node / 定制线路) lets a customer buy a dedicated VPS that a k2r router routes all household traffic through. The marketing premise was "买独立 VPS 就不用担心流量". The product owner raised the concern: **a router is always-on; a household streaming Netflix 4K daily can push 1–2 TB/month, which could blow out traffic cost.**

**The concern is valid, and amplified by adverse selection:** the people who buy a *dedicated line* are precisely the ones who want stable whole-household 4K streaming — i.e. the heaviest traffic users. Average usage on this product must be modeled as *near-cap is normal*, not as a tail.

**Cost-model decision (made 2026-06-11): Model A — fixed quota + hard cutoff.** We sell a fixed monthly quota (e.g. 1 TB). Center meters usage and cuts the line at 95%. Cost is deterministic. Marketing positions it as **"你的专属线路 + 月度大额流量,用超可加购/升级"**, NOT "无限流量". Rejected alternatives: (B) unmetered/flat-bandwidth provider — has fair-use/封机 risk + bandwidth shaping + adverse selection makes a household router the exact abuse profile; (危险错法) metered provider marketed as "unlimited" — we eat provider overage (a single Netflix household on a $5/1TB box → ~$180/month overage).

## 2. The Invariant That Makes Model A Airtight

> **soldQuota (`TrafficTotalBytes`) < providerIncludedQuota (`BundleTransferBytes`)**

We buy a *metered* VPS whose tier includes a fixed traffic allowance and charges overage only beyond it. As long as the sold quota sits strictly below the provider's included allowance, **the user is cut on our side before the machine ever crosses the provider's overage line.** Our cost = machine monthly fee (fixed) + margin, independent of how much the user streams. Operating rule: **always provision a bundle one tier above the sold quota** so the safety margin is large (e.g. sell 1 TB, provision a 2 TB bundle).

**Why the hard ceiling is 100% of sold quota, not 95%.** The runtime cut at 95% is the *online soft-stop* (heartbeat `verdict=stop`). But `EpochHardCeilingBytes = TrafficTotalBytes` (`slave_api_usage.go:98`) — when a node can't reach Center it enforces *locally up to 100%* of sold quota. So the worst-case consumption before any hard stop is `soldQuota`, and the invariant must guard against that: `soldQuota < includedQuota` (strict). The 95% margin is a bonus on the online path, not the floor.

**Enforcement mechanism (revised after recon).** `TransferTB` is only obtainable via a *live cloud `ListPlans` call*, and `PrivateNodePlanSpec` has **no creation handler** — rows are inserted directly via DB/scripts/tests, and there is no provider-name→account resolution. Putting a live cloud call in a DB hook or the deliberately cloud-decoupled provision path is bad engineering (network coupling, breaks offline tests). Instead we make the invariant a **pure, self-contained property of the plan-spec row**: add an explicit `BundleTransferBytes int64` field (the bundle's known included allowance — whoever sets the sold quota already had to know the bundle to set it responsibly), and enforce `soldQuota < BundleTransferBytes` with **integer arithmetic, no external dependency**. This invariant is currently **NOT enforced anywhere**; G1 closes it.

## 3. Verified Current State (2026-06-11)

Authoritative recon (file:line) — what already exists so the design only fills real gaps:

| Area | State | Reference |
|---|---|---|
| Plan quota field | `PrivateNodePlanSpec.TrafficTotalBytes int64`, snapshotted into subscription at purchase | `model_private_node.go:85`, `provision_private_node.go:59` |
| Plan-quota validation | **None** on the value | `api_admin_plan.go:56-117` |
| Provider included quota | Available: `PlanInfo.TransferTB float64` (TB/month) via `Provider.ListPlans(region)` | `cloudprovider/provider.go:73`, `api_admin_cloud.go:503` |
| Provision uses provider quota? | **No** — decoupled; Center uses its own snapshot only | `provision_private_node.go:59` |
| Metering ingestion | **Implemented**: `POST /slave/usage` → `api_slave_node_report_usage`, idempotent max-cumulative update of `CloudInstance.TrafficUsedBytes` per epoch | `slave_api_usage.go:39-101`, `route.go:518` |
| 95% cutoff enforcement | **Implemented twice**: heartbeat `verdict=stop` + device-check-auth returns 402. Integer `used*100 >= total*95`, threshold consts `trafficStopThreshold{Num=95,Den=100}` | `slave_api_usage.go:88-91`, `slave_api_device_auth.go:113-119` |
| `IsServiceable(now)` | **Time-only** — checks `ExpiresAt`/grace, **ignores quota entirely** | `model_private_node.go:64-74` |
| Quota-driven status transition | **None** — subscription stays `active` at 100% used; only expiry drives status | `worker_private_node_lifecycle.go:30-85` |
| User DTO | `DataPrivateNodeSubscription` exposes `TrafficTotalBytes`, `TrafficUsedBytes`, `Status`, `IsServiceable`, expiry/grace/suspend | `type.go:587-601`, `api_user_private_node.go:13-64` |
| Email infra | `SendTemplatedEmails` + `EmailMarketingTemplate` ready; **no cron** wired for quota thresholds | `logic_email_send.go:86+`, `model.go:730-746` |

**Key consequence:** a quota-cut user today sees `status=active, isServiceable=true` in the App while their connection is rejected with 402 → presents as "VPN 莫名其妙坏了". This is the precise工单 driver G2 fixes.

## 4. Gap Analysis & Scope

| # | Gap | Why it matters | In scope |
|---|---|---|---|
| **G1** | Provision-time invariant guard | 商业安全底线 — prevents silent overage exposure | ✅ this spec |
| **G2** | Surface "quota exhausted" as a first-class state to the App | 工单防雪崩 — distinguishes 额度用尽 from 到期 and from "坏了" | ✅ this spec |
| **G3** | 80%/95% warning emails (cron) | 留存/收入优化 | ⏸ Phase 2 (deferred) |
| **G4** | Top-up / 升级额度 purchase path | 收入优化 — lets capped heavy users keep paying | ⏸ Phase 2 (deferred, noted only) |

Non-goals for this spec: changing the cutoff ratio/mechanism (already correct); the k2s Phase-B sidecar polling loop (separate smoke-gated task #12); top-up commerce (G4).

## 5. Design

### G1 — Plan-spec invariant guard (explicit field + DB hook + provision backstop)

**Data:** add `BundleTransferBytes int64` to `PrivateNodePlanSpec` (`model_private_node.go`) — the provider bundle's monthly included transfer, in bytes (e.g. a Lightsail 2 TB tier → `2 * 1e12`). Additive column; AutoMigrate adds it. This is the local, authoritative record of the bundle allowance, set by whoever configures the plan (they had to know the bundle to choose `BundleID` + a responsible quota anyway).

**Primary guard — GORM `BeforeSave` hook on `PrivateNodePlanSpec`:**

```go
func (s *PrivateNodePlanSpec) BeforeSave(tx *gorm.DB) error {
    if s.TrafficTotalBytes <= 0 {
        return fmt.Errorf("private node plan spec: trafficTotalBytes must be > 0")
    }
    if s.BundleTransferBytes <= 0 {
        return fmt.Errorf("private node plan spec: bundleTransferBytes must be > 0 (record the provider bundle's included allowance)")
    }
    if s.TrafficTotalBytes >= s.BundleTransferBytes {
        return fmt.Errorf("private node plan spec: sold quota %d >= bundle allowance %d — would expose us to provider overage; provision a larger bundle",
            s.TrafficTotalBytes, s.BundleTransferBytes)
    }
    return nil
}
```

- Fires on **every** insert/update path — direct DB `Create`, scripts, and tests — because there is no handler to hook. Pure integer arithmetic, **no cloud call, no external dependency**, works offline.
- Strict `>=`: sold quota must be **strictly** below the bundle allowance (the hard ceiling is 100% of sold quota, §2).

**Backstop guard — provision time (`emitNodeProvisionJob`, `provision_private_node.go`):** the spec is already loaded there. Before emitting the job, re-assert the same invariant on the loaded `spec`; on violation, transition the subscription to `PNStatusFailed` (set `LastProvisionError`), log an admin-facing error, and **do not** emit the provision job. Pure arithmetic on the in-memory spec — no cloud call. This catches legacy rows created before the hook existed (whose `BundleTransferBytes` defaults to 0 → fails closed).

- Extract the comparison into one helper (e.g. `validatePrivateNodeQuotaInvariant(trafficTotal, bundleTransfer int64) error`) called by both the hook and the backstop, so they can never drift.
- **Deploy note:** before deploying, backfill `BundleTransferBytes` on any existing `PrivateNodePlanSpec` row (product is pre-launch, so likely zero/test rows) — otherwise their next provision fails closed. Existing test fixtures that create specs must set `BundleTransferBytes` (> their `TrafficTotalBytes`).
- **No live-cloud cross-check here.** Verifying `BundleTransferBytes` actually matches the provider's current `ListPlans().TransferTB` is a *separate, optional admin report* (future, out of scope) — it must never block provision.

### G2 — Surface "quota exhausted" as a first-class state

The App must be able to say, distinctly:
1. **正常** (serviceable, plenty of quota)
2. **额度用尽** (cut by quota; service paused until next cycle reset or top-up) — *new, currently invisible*
3. **到期/宽限/暂停** (time-driven lifecycle) — already surfaced
4. (everything else = genuine error)

**Center side:**
- Add a derived boolean to the DTO: `quotaExhausted bool` on `DataPrivateNodeSubscription` (`type.go`), computed in `api_get_user_private_nodes` (`api_user_private_node.go`) using the **same** `used*Den >= total*Num` threshold (shared constant), guarded for `TrafficTotalBytes > 0`.
- Do **not** overload `IsServiceable()` with quota semantics — keep it time-authoritative. `quotaExhausted` is an orthogonal dimension. (A subscription can be time-serviceable AND quota-exhausted simultaneously; the App needs both bits to render the right message.)
- Optionally also expose the cycle reset instant the user resets at. The metering epoch resets via `CloudInstance.TrafficResetAt` (lazy, on heartbeat). Expose a `quotaResetAt int64` (0 if unknown) so the App can say "X 号重置". Source it from the linked `CloudInstance.TrafficResetAt` when present.

**webapp side:**
- `PrivateNodeSubscriptionView` (api-types.ts) gains `quotaExhausted: boolean` + `quotaResetAt?: number`.
- `PrivateNodePanel.tsx`: when `quotaExhausted` is true AND status is otherwise active/grace, render a distinct "本月额度已用尽" panel state (warning/error chip) with: the reset date ("将于 X 月 X 日重置") and a 续费/加购 affordance (navigate to purchase; G4 top-up wiring is Phase 2, for now route to the plan page). This state takes visual precedence over the normal traffic bar so the user never reads it as "broken".
- Traffic bar already turns red ≥95% (Plan 5 Phase 4) — keep, but the exhausted panel is the explicit, worded state.
- i18n: add keys under the existing `privateNode` namespace (7 locales, nested): `privateNode.quotaExhausted.title`, `.resetHint`, `.topUpCta`. 中文用户面向文案用"开途"/"专属线路",禁用裸词 "Kaitu"。

### G3 — 80%/95% warning emails (Phase 2, deferred)

Sketch only (not built this spec): a periodic Center sweep (alongside `worker_private_node_lifecycle`) queries active private-node subs whose linked instance crossed 80% / 95% this epoch and hasn't been warned for this epoch yet, enqueues a templated EDM, and records a per-epoch "warned" marker (e.g. a column or a sent-log lookup keyed by `TrafficEpoch`) for idempotency so each threshold fires at most once per cycle. Requires real-send smoke → out of the 10/10-desk scope.

## 6. Testing Strategy

- **G1 hook:** unit-level (no DB needed for the pure helper) + integration. `validatePrivateNodeQuotaInvariant`: sold < bundle → nil; sold == bundle → error; sold > bundle → error; bundle ≤ 0 → error; sold ≤ 0 → error. Integration: `db.Get().Create(&PrivateNodePlanSpec{...})` with sold ≥ bundle → `BeforeSave` rejects the insert; with sold < bundle → succeeds.
- **G1 backstop:** Go integration (real dev MySQL, `testInitConfig(); skipIfNoConfig(t)`): a subscription whose spec violates the invariant (simulating a legacy row, e.g. `BundleTransferBytes=0`) → `emitNodeProvisionJob` transitions sub to `PNStatusFailed`, sets `LastProvisionError`, emits **no** `NodeProvisionJob` row. Valid spec → job emitted as before (regression).
- Update the four existing fixtures (`api_plan_private_node_test.go:31`, `api_order_region_test.go:31`, `provision_private_node_test.go:27`, `logic_member_private_test.go:43`) to set `BundleTransferBytes` strictly above their `TrafficTotalBytes` (e.g. `3 * 1e12` when traffic is `2<<40`).
- **G2 Center:** test `api_get_user_private_nodes` returns `quotaExhausted=true` at/above threshold, `false` below, `false` when `TrafficTotalBytes==0`; `quotaResetAt` mirrors the linked instance.
- **G2 webapp:** vitest on `PrivateNodePanel` — exhausted state renders the worded panel (not the generic bar), shows reset hint, exposes the CTA; brand-grep the rendered copy for zero "Kaitu".
- Regression: full Center `go test ./...` (note 7 pre-existing unrelated auth/CSRF failures), webapp `vitest` + `tsc` + `build`.

## 7. Open Decisions (for plan-time, not blocking)

1. **Quota tier/price numbers** are plan *data*, not code — the G1 guard works for any numbers. David sets actual tiers (e.g. 1 TB / 2 TB) later; the guard enforces correctness regardless.
2. **G4 top-up vs upgrade-tier**: deferred. For now the exhausted-state CTA routes to the purchase page.

## 8. Release Confidence Framing

G1 + G2 are desk-verifiable (Go integration tests + vitest + tsc + build) → target 9–9.5/10. They do **not** require real-machine smoke because the enforcement they build on (metering + 95% cutoff) already exists and is exercised; G1/G2 add a guard and a visibility bit. The end-to-end "real router hits cap and user sees the right screen" validation is real-machine and remains a smoke item (caps that path at 6–7/10), but the code units here are fully desk-provable.
