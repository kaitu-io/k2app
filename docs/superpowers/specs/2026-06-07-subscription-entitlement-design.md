# Subscription Entitlement — Single-Truth Additive Ledger (Design Spec)

**Date:** 2026-06-07
**Status:** Approved (brainstorming → spec). Next: writing-plans.
**Area:** `api/` (Center), `webapp/` (iOS subscribe UX)
**Supersedes:** `docs/superpowers/plans/2026-06-07-subscription-entitlement-dual-clock.md` (the earlier dual-clock model — replaced by the simpler additive single-ledger model below).

---

## 1. Problem

iOS Apple IAP shipped, but the subscription state is wrong in two ways:

1. **Two views contradict each other.** Purchase page shows "已订阅" while the Account
   membership card shows "已过期", for the same account on the same refresh.
2. **Latent dual-source-of-truth.** `users.expired_at` is written by two *incompatible*
   semantics: Apple uses **absolute/max** (`computeRecurringEntitlement` — set expiry to the
   Apple period end), while the other six grant sources use **additive** (`addProExpiredDays`
   — +N days). A gift given to a user with an active auto-renewing Apple sub gets **absorbed**
   by the next renewal.

### Confirmed root cause (prod, user 7977 sandbox)

- `users.expired_at = 2026-06-05 16:50` (past) → Account card correctly shows expired.
- `subscriptions[id=1].status='active'`, `current_period_end=2026-06-05 16:50` (**past**) →
  purchase page incorrectly shows subscribed.
- `verifyAndGrantTransaction` hardcoded `status:"active"` even though the transaction's
  `ExpiresDate` was already past (sandbox accelerated cycle) → **born-stale** row.
- `GetActiveSubscriptions` filtered by `status` only, **no `current_period_end > now`** check →
  the stale row read as live.
- No Apple S2S `EXPIRED` notification arrives in sandbox (`/webhook/appstore` zero hits) → the
  stale `active` is permanent.

The Apple side already works end-to-end otherwise: the deployed Center (commit `9114c343`,
2026-06-07) has the verify endpoint, the `.p8` App Store Server key is configured, and the prod
row holds real Apple data. The only defect is how state is **stored and read**.

---

## 2. Core principle — ONE source of truth

> **`users.expired_at` (our own ledger) is the single source of truth for entitlement.
> Exactly one accumulator writes it. Every source — Apple, one-time web payment, invite/bug/
> survey rewards, system grants — only *adds days* to this ledger, each credited *once* by a
> unique reference. There is no second clock, no overwrite, no max/absolute path.**

Apple stops being a "truth": it becomes **a payment source that credits the ledger** (each paid
transaction = one period added, deduped by transaction id), plus a **best-effort** "delay the
next charge" perk (Extend API, optional). When a user cancels auto-renew, Apple simply stops
adding days; the days already credited keep counting down — the user retains exactly what was
paid for, identical to how a one-time purchase already behaves.

This is the *minimal, consistent* change: the system is **already additive** for its six other
grant sources. The only thing that broke the model was Apple using absolute/max. We make Apple
additive like everything else, and the whole class of contradictions disappears.

---

## 3. Invariants (the testable backbone)

- **INV1 — Idempotent credit.** Each `(provider, transaction_id)` (and each gift `reference_id`)
  is credited **at most once**, even if a webhook and a client verify both report it, or it
  replays.
- **INV2 — Monotonic entitlement.** `expired_at` only increases from credits; it decreases only
  via an explicit refund/revoke clawback.
- **INV3 — No absorption.** A gift is never eaten by a subsequent recurring renewal (guaranteed
  by additivity — both add).
- **INV4 — Cancel keeps paid time.** After auto-renew is cancelled, `expired_at` retains all
  credited time and counts down normally.
- **INV5 — Entitlement ⟂ status.** "Is the user a member right now" is derived **only** from
  `expired_at > now`, never from `subscriptions.status`.
- **INV6 — No double-pay.** A user with an active recurring plan cannot create a one-time order
  (server-enforced, not just hidden in the UI).
- **INV7 — Self-healing.** If a renewal notification is missed, reconciliation against Apple's
  transaction history credits the un-credited transactions, so a paying user is never locked out.

---

## 4. Data model

- `users.expired_at int64` — the single truth. **No structural change.**
- `subscriptions` table — **demoted** to "recurring-plan state": `status`
  (active/grace/billing_retry/expired/revoked), `current_period_end`, `auto_renew`,
  `provider`, `provider_subscription_id`, `provider_latest_ref`, `environment`. Used ONLY for:
  (a) UI subscribe-vs-manage, (b) double-sell block, (c) Extend target, (d) transaction dedup.
  **It no longer determines entitlement.**
- **Credit dedup** — a unique key guaranteeing INV1. Either:
  - a unique index on `user_pro_histories (type, reference_id)` where `reference_id` carries the
    Apple `transaction_id` (preferred — reuses the existing audit ledger), **or**
  - a dedicated `subscription_credits(provider, transaction_id UNIQUE, …)` table.
  Decision deferred to writing-plans; both satisfy INV1. The audit row already records
  `Type=VipAppleSub`, `ReferenceID`, `Days`, `Reason`.

---

## 5. Write paths (all additive)

- **Apple first purchase / each renewal** → credit the period via the additive accumulator
  (`addProExpiredDays`-style), keyed by `transaction_id`, **after a dedup check**; already
  credited → skip (INV1). Credit amount = the period this transaction paid for (the
  `expiresDate` delta; exact day-arithmetic — delta vs fixed plan period, and the "expired ⇒
  from now" branch on late reconciliation — pinned with tests in the plan).
- **Delete `computeRecurringEntitlement`** (the absolute/max path — the absorption + born-stale
  root). `applyRecurringSubscription` is rewritten to credit additively + update the
  `subscriptions` row's plan-state (`current_period_end`, `auto_renew`, `status` via
  `deriveVerifiedStatus`).
- **One-time / invite / survey / system grant / invited** → unchanged (already additive via
  `addProExpiredDays`).
- **Refund / revoke** (`revokeSubscription`) → subtract the refunded transaction's credited days
  from the ledger (clawback, INV2), and mark the `subscriptions` row `revoked`.

---

## 6. Reconciliation (self-healing, INV7)

- A scheduled job + on-demand (App open / verify) reads Apple `Get All Subscription Statuses` /
  transaction history for the user's `original_transaction_id`, and **credits any transaction
  not yet in the dedup ledger**.
- Closes the "missed webhook → paying user locked out" hole. Also flips lapsed `subscriptions`
  rows to `expired` (cleanup), independent of S2S delivery.
- Requires the App Store Server API client. **Verify whether `qtoolkit/appstore` exposes the
  needed history/status calls; if not, add them** (documented endpoints, JWT-signed).

---

## 7. Read model (= Phase 0, already implemented)

Two *different* questions, each answered from the right place — they can never contradict:

- **"Is there an active recurring plan?"** (subscribe vs manage; double-sell) →
  `isSubscriptionLive(sub, now)`: `active` requires `current_period_end > now`;
  `grace`/`billing_retry` count regardless; terminal never counts.
- **"Is the user a member right now?"** → `expired_at > now` (INV5).

A user with `expired_at` in the future but no active plan (paid once / cancelled but still
covered) correctly shows "member" on Account and "subscribe/start a plan" on Purchase — not a
contradiction.

`GetActiveSubscriptions` filters via `isSubscriptionLive`; `verifyAndGrantTransaction` /
`upsertSubscription` derive `status` via `deriveVerifiedStatus` (never hardcode `active`).

---

## 8. Attack surface / product constraints (zero attackable points on existing payment)

### 8.0 Account binding — entitlement follows OUR account, never the device/Apple ID

Entitlement is granted to **our email-based user account**, never to whoever holds the device or
the Apple ID. The Apple subscription is cryptographically bound to the purchasing our-user via
`appAccountToken = uuidv5(NS, user.uuid)`:

- **At purchase:** the client passes the logged-in user's `appAccountToken`; the native bridge
  refuses to purchase without a valid UUID, so **every** transaction carries it.
- **At verify AND restore** (both go through the same `/api/user/apple-iap/verify`): grant only
  if `tx.appAccountToken == derive(currentUser.uuid)`; otherwise reject ("bound to a different
  account"). The check is stateless (token re-derived from the user UUID).
- **Hardening (decision):** **hard-reject transactions with an empty `appAccountToken` in
  production.** We have zero legacy purchases (IAP just launched; every purchase goes through our
  app and always carries the token), so this removes the current `first-write-wins` fallback —
  closing the only path by which an unpaid user, on a device whose Apple ID previously subscribed,
  could pick up entitlement via Restore.

Correct-by-design consequences: the sub belongs to the our-account that bought it and does **not**
follow the user to a different our-account; an unauthorized our-user on a device whose Apple ID
already subscribed can neither claim it (token mismatch) nor buy a second (Apple dedupes per
Apple ID).

| # | Threat | Constraint / defense |
|---|---|---|
| T1 | Steal another user's `transactionId`, OR pick up a device's existing Apple sub via Restore on a different/unpaid our-account | `appAccountToken` binding enforced at verify **and** restore (§8.0); **hard-reject empty token in production**; subscription-row `UserID` first-write-wins as backstop |
| T2 | Double-credit one Apple transaction (webhook + verify, or replay) | `(provider, transaction_id)` unique dedup; credit only if absent; row lock (INV1) |
| T3 | **Active sub + one-time payment → user double-charged** | **Server** rejects a one-time order when the user has an `isSubscriptionLive` plan + client hides the entry. **Two layers.** (INV6) |
| T4 | Keep entitlement after refund | REFUND/REVOKE webhook → subtract the credited days (INV2) |
| T5 | Reward farming | each `reference_id` credited once (INV1) + per-source business caps (not worsened here) |
| T6 | Under-credit (missed notification) → paying user locked out | reconciliation self-heals (INV7) |
| T7 | Concurrency race (parallel notifications) lost-update / double | user row `FOR UPDATE` + dedup unique key + txn (existing `withDeadlockRetry`) |
| T8 | Client lies about sub-state / forges ownership | sub-state always derived server-side from DB; ownership always re-verified with Apple |
| T9 | Tier escalation via a cheap product | tier derived from the plan mapped to the actual `product_id`; never client-set |

T3 is the new core constraint and its point is to **protect the user from double-paying** (Apple
keeps charging *and* they paid one-time) — the "no attackable point on existing payment" bar.

### Cross-cutting decisions
- **Purchase block is platform-agnostic and server-enforced** (the order API rejects), with the
  client UI hiding the entry as UX. Not iOS-only.
- **Reverse direction** (active one-time membership, then Apple subscribe) is **allowed** — it is
  not double-pay, just two sequential additive credits. We cannot (and need not) block StoreKit.
- **Tier** is orthogonal to expiry: set on purchase from the product's plan, keep the highest
  active; never downgraded by a lower-tier Apple `basic` while a higher one-time tier is active.

---

## 9. Extend API — optional best-effort (后置)

When gifting to a user with an active Apple sub, **additionally** best-effort call App Store
Server "Extend a Subscription Renewal Date" to push the next charge out by the gift days, so the
user pays later. **Not required for correctness** — the additive ledger already grants the days
regardless. Constraints if/when built: ≤90 days/call (chunk larger), idempotent
`requestIdentifier`, Apple's own cumulative caps → on rejection just skip (ledger already
correct). Apple positions this API for goodwill/compensation; keep volume sane.

---

## 10. Phasing

- **Phase 0 — DONE (uncommitted on `main`).** Read-consistency fix: `isSubscriptionLive` +
  `deriveVerifiedStatus`; `GetActiveSubscriptions` gate; drop hardcoded `active`. 15 Go subtests
  + webapp button regression test green. **Independently unblocks App Store review — ship first.**
- **Phase 1.** Make Apple additive: rewrite `applyRecurringSubscription` to credit via the
  accumulator + transaction dedup (INV1); delete `computeRecurringEntitlement`; **hard-reject
  empty `appAccountToken`** (§8.0). Tests pin INV1–INV5.
- **Phase 2.** Reconciliation / self-healing against Apple history (INV7) + lapsed-row cleanup.
- **Phase 3.** T3 server-side one-time-order block for active recurring plans (INV6); confirm
  client UI hiding (largely done).
- **Phase 4 (optional).** Extend best-effort "delay the charge".

---

## 11. Test strategy

- **Pure unit (no DB):** `isSubscriptionLive`, `deriveVerifiedStatus` (done); the additive
  credit + dedup decision function; refund clawback math.
- **Mock-DB:** order/verify handlers reject when active plan (INV6); dedup blocks double credit.
- **Integration (real dev MySQL, `skipIfNoConfig`):** Apple purchase + renewal + gift interplay
  asserting no absorption (INV3), idempotent re-verify (INV1), cancel keeps time (INV4),
  refund clawback (INV2), reconciliation credits a missed transaction (INV7).
- **webapp vitest:** purchase page vs Account agree; subscribe button in-flight guard (done).
- **Release confidence:** functional/money change capped 6–7/10 until real-device sandbox smoke;
  desk-verified pure logic 9–9.5/10 (per release-confidence framework).

---

## 12. Open questions

None blocking. Two implementation-level choices deferred to writing-plans:
1. Dedup storage: unique index on `user_pro_histories(type, reference_id)` vs a dedicated
   `subscription_credits` table.
2. Exact Apple credit day-arithmetic (expiresDate delta vs fixed plan period; late-reconciliation
   "from now" handling) — pinned with tests.
