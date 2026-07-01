# Subscription Entitlement — Single-Truth Additive Ledger (Design Spec)

**Date:** 2026-06-07
**Status:** Approved (brainstorming → spec). Next: writing-plans.
**Area:** `api/` (Center), `webapp/` (iOS subscribe UX), `mobile/` (StoreKit bridge)
**Supersedes:** the earlier dual-clock plan (replaced by the additive single-ledger model below).

---

## 1. Problem

iOS Apple IAP shipped, but subscription state is wrong in two ways:

1. **Two views contradict.** Purchase page shows "已订阅" while the Account card shows "已过期",
   same account, same refresh.
2. **Latent dual-source-of-truth.** `users.expired_at` is written by two *incompatible* semantics:
   Apple uses **absolute/max** (`computeRecurringEntitlement`), the other six grant sources use
   **additive** (`addProExpiredDays`). A gift to an active auto-renew Apple subscriber gets
   **absorbed** by the next renewal.

### Confirmed root cause (prod, user 7977 sandbox)

`expired_at = 2026-06-05 16:50` (past, correct). `subscriptions[id=1].status='active'`,
`current_period_end=2026-06-05 16:50` (past) → purchase page wrongly shows subscribed.
`verifyAndGrantTransaction` hardcoded `status:"active"` (born-stale). `GetActiveSubscriptions`
filtered by `status` only (no `current_period_end > now`). No S2S `EXPIRED` arrives in sandbox.

Apple otherwise works end-to-end (deployed Center `9114c343`; `.p8` configured; prod row holds
real Apple data). The only defect is how state is **stored and read**.

---

## 2. Core principle — ONE source of truth, ONE writer

> **`users.expired_at` (our own ledger) is the single source of truth for entitlement. A single
> entitlement module is the *only* code that mutates it. Every source — Apple, one-time web
> payment, invite/bug/survey rewards, system grants — only *adds time* to this ledger, each
> credited *once* by a unique reference. No second clock, no overwrite, no max/absolute path.**

Apple stops being a "truth": it becomes **a payment source that credits the ledger** (each paid
transaction = one period added, deduped by transaction id) plus a **best-effort** "delay the next
charge" perk (Extend API, optional). After cancel, Apple stops adding; credited days keep counting
down — identical to a one-time purchase.

**Architectural boundary:** all three mutation operations — `credit` (purchase/renewal/gift/
grace), `clawback` (refund/revoke), `coverThrough` (grace window) — live in ONE entitlement
module. No handler, webhook, or worker touches `expired_at` directly. This is the isolation that
makes the system auditable and testable.

This is the *minimal, consistent* change: the system is **already additive** for its six other
sources. Only Apple used absolute/max. Make Apple additive like the rest, and the whole class of
contradictions disappears.

---

## 3. Invariants (testable backbone)

- **INV1 — Idempotent credit.** Each `(provider, transaction_id)` and each gift `reference_id` is
  credited **at most once**, even if webhook + verify both report it, or it replays.
- **INV2 — Monotonic + floored clawback.** `expired_at` only rises from credits; it falls only via
  explicit refund/revoke clawback, and clawback never pushes it **below `now`**.
- **INV3 — No absorption.** A gift is never eaten by a subsequent renewal (both add).
- **INV4 — Cancel keeps paid time.** After auto-renew cancel, `expired_at` retains all credited
  time and counts down normally.
- **INV5 — Entitlement ⟂ status.** "Member right now" is derived ONLY from `expired_at > now`,
  never from `subscriptions.status`.
- **INV6 — No double-charge.** A user with a **live, auto-renewing** Apple plan cannot create a
  one-time order (server-enforced). A *cancelled-but-still-covered* user MAY (legit switch).
- **INV7 — Capture guarantee.** A purchased transaction is **never `finish()`-ed until Center
  confirms the credit**; StoreKit re-delivers unfinished transactions, so a paid transaction is
  eventually captured even if a network call drops. Reconciliation is the backstop, not the
  front line.
- **INV8 — Every mutation audited.** Every `expired_at` change writes a `UserProHistory` row
  (source, reference, signed delta, reason). The ledger is fully reconstructable.
- **INV9 — Binding is permanent.** An Apple subscription binds to exactly one our-user at first
  verify (via `appAccountToken`); all later transactions for that `original_transaction_id` credit
  that bound user only.

---

## 4. Data model

- `users.expired_at int64` — the single truth. No structural change.
- `subscriptions` — **demoted** to recurring-plan state: `status`
  (active/grace/billing_retry/expired/revoked), `current_period_end`, `auto_renew`, `provider`,
  `provider_subscription_id` (= Apple `original_transaction_id`, the binding key),
  `provider_latest_ref`, `environment`, `user_id`. Used ONLY for: subscribe-vs-manage UI,
  double-sell block, Extend target, binding. **Not** entitlement.
- **Credit dedup (resolves former open-question #1).** Apple `transaction_id` is a **string** and
  does not fit `UserProHistory.ReferenceID uint64`. Decision: **add a dedicated
  `subscription_credits` table** — `(provider, transaction_id VARCHAR)` **UNIQUE**,
  `original_transaction_id VARCHAR`, `user_id`, `credited_seconds int64`,
  `kind ENUM(purchase|renewal|grace)`, `created_at`. This is the machine idempotency key (INV1).
  `UserProHistory` remains the human-readable audit (INV8). The entitlement module writes both in
  one transaction.

---

## 5. Write paths (all additive, one module)

- **Apple first purchase / renewal** → entitlement module `credit`: dedup-check
  `subscription_credits(provider, transaction_id)`; if absent, insert + add time to `expired_at` +
  write `UserProHistory`. If present, **no-op** (INV1).
- **Credit amount = exact `expiresDate` delta** (`thisExpiresDate − priorExpiresDate` for the
  binding, in seconds), NOT a calendar-day approximation — keeps the ledger's Apple portion exactly
  aligned with Apple cumulatively while gifts stack on top. First purchase from an expired state
  bases from `max(expired_at, now)` (the existing `addProExpiredDays` "from now if expired" rule).
- **Delete `computeRecurringEntitlement`** (the absolute/max path — absorption + born-stale root).
- **Non-Apple sources** (one-time/invite/survey/system grant) → unchanged additive credits, now
  routed through the same entitlement-module `credit` (day-based).
- **Refund / revoke** → entitlement module `clawback`: reduce `expired_at` by the refunded
  transaction's `credited_seconds`, floored at `now` (INV2); mark `subscriptions` row `revoked`;
  audit row with negative delta.

### 5.1 Grace / billing-retry coverage (product-case fix)

When Apple sends `DID_FAIL_TO_RENEW` with a grace period (or `billing_retry`), the user is still
entitled per Apple even though no new payment posted. The entitlement module `coverThrough`:
`expired_at = max(expired_at, gracePeriodExpiresDate)` (a provisional cover, audited as
`kind=grace`). If the retry later succeeds → the renewal credit extends further (idempotent). If it
finally fails → grace already lapsed naturally; no clawback needed. This keeps `expired_at` the
single truth **without** ever reading `status` for entitlement (INV5 preserved).

---

## 6. Capture + reconciliation (self-healing, INV7)

- **Front line — finish-gating:** the StoreKit bridge calls `finish()` on a transaction **only
  after** Center returns a successful verify+credit. Until then StoreKit keeps re-delivering it
  (`Transaction.updates` / on next launch). This guarantees first-purchase capture even if a
  network call drops — without any server polling.
- **Backstop — reconciliation:** a scheduled job + on-app-open reads Apple `Get All Subscription
  Statuses` / transaction history for each **known** `original_transaction_id` and credits any
  transaction missing from `subscription_credits` (INV1 makes this safe). Also flips lapsed
  `subscriptions` rows to `expired`. Cadence: hourly job + on app foreground.
- A subscription we have **never** seen (no row, no original_transaction_id) is captured by the
  front line (client always verifies post-purchase); reconciliation only heals **known** subs.
- Requires App Store Server API history/status calls — **verify `qtoolkit/appstore` exposes them;
  add if missing** (documented, JWT-signed endpoints).

---

## 7. Read model (= Phase 0, implemented)

Two different questions, each from the right place — can never contradict:

- **"Active recurring plan?"** (subscribe vs manage; double-sell) → `isSubscriptionLive(sub, now)`:
  `active` requires `current_period_end > now`; `grace`/`billing_retry` count regardless; terminal
  never counts.
- **"Member right now?"** → `expired_at > now` (INV5).

`GetActiveSubscriptions` filters via `isSubscriptionLive`; verify/`upsertSubscription` derive
`status` via `deriveVerifiedStatus` (never hardcode `active`).

---

## 8. Attack surface / product constraints (zero attackable points on existing payment)

### 8.0 Account binding — entitlement follows OUR account, never the device/Apple ID

Entitlement is granted to our **email-based user account**, never to the device or Apple ID. The
sub binds to the purchasing our-user via `appAccountToken = uuidv5(NS, user.uuid)`:

- **At purchase:** client passes the logged-in user's `appAccountToken`; the native bridge refuses
  to purchase without a valid UUID → **every** transaction carries it.
- **At first verify (binding):** grant only if `tx.appAccountToken == derive(currentUser.uuid)`;
  else reject. **Hard-reject empty `appAccountToken` in production** (zero legacy purchases; every
  purchase carries it) — closes the Restore free-ride path. Binding (`original_transaction_id →
  user_id`) is recorded once (INV9).
- **At renewal / reconciliation:** later transactions for an already-bound `original_transaction_id`
  credit the bound user directly (binding is permanent; no re-check needed even if a renewal's
  token were absent). Restore on a *different* our-account hits the token mismatch → reject.

Correct-by-design: the sub belongs to the our-account that bought it and does not follow the user
to a different account; an unauthorized user on a device whose Apple ID already subscribed can
neither claim it (token mismatch) nor buy a second (Apple dedupes per Apple ID).

| # | Threat | Constraint / defense |
|---|---|---|
| T1 | Steal a `transactionId`, or pick up a device's sub via Restore on a different/unpaid our-account | `appAccountToken` bind at verify + restore (§8.0); **hard-reject empty token in prod**; binding permanent (INV9) |
| T2 | Double-credit one transaction (webhook + verify, replay) | `subscription_credits(provider, transaction_id)` UNIQUE; credit only if absent; row lock (INV1) |
| T3 | **Active auto-renew sub + one-time payment → user double-charged** | server rejects one-time order when user has a **live auto-renewing** plan + client hides entry (INV6). Cancelled-but-covered users allowed (legit switch). |
| T4 | Keep entitlement after refund | REFUND/REVOKE → clawback credited seconds, floored at now (INV2) |
| T5 | Reward farming | each `reference_id` credited once (INV1) + per-source business caps |
| T6 | Under-credit (missed notification) → paying user locked out | finish-gating front line + reconciliation backstop (INV7) |
| T7 | Concurrency race (parallel notifications) | user row `FOR UPDATE` + dedup UNIQUE + txn (`withDeadlockRetry`) |
| T8 | Client lies about sub-state / forges ownership | sub-state derived server-side; ownership re-verified with Apple |
| T9 | Tier escalation via a cheap product | tier derived from the plan mapped to the real `product_id`; never client-set |
| **T10** | **Forged S2S webhook injects a fake transaction/grant** | webhook payload is a signed JWS — **verify Apple's signature (x5c chain to Apple root)**; and the webhook **never grants on its own data** — it only triggers a server-to-server `GetTransaction` (the trust anchor) which carries the real, Apple-authenticated data. Confirm `qtoolkit/appstore` does x5c verification; if it "skips verification C", add it. |

### Cross-cutting decisions
- **Block is server-enforced + platform-agnostic** (order API rejects), client UI hides as UX.
- **Reverse direction** (active one-time membership, then Apple subscribe) is allowed — sequential
  additive credits, not double-pay; can't block StoreKit anyway.
- **Tier** orthogonal to expiry: set from the product's plan, keep the highest active; never
  downgraded by a lower-tier Apple `basic` while a higher one-time tier is active. v1 ships a
  **single product** (`io.kaitu.sub.basic.1y`); multi-tier + Apple upgrade/downgrade proration is
  deferred (future spec).

---

## 9. Extend API — optional best-effort (后置)

When gifting to a live Apple subscriber, **additionally** best-effort call App Store Server "Extend
a Subscription Renewal Date" to push the next charge out by the gift days. **Not required for
correctness** — the additive ledger already grants the days. Constraints: ≤90 days/call (chunk
larger), idempotent `requestIdentifier`, Apple's own cumulative caps → on rejection just skip
(ledger already correct). Apple positions it for goodwill/compensation; keep volume sane.

---

## 10. Migration

**No production Apple subscribers exist** (IAP not yet released to production). The only existing
`subscriptions` row is sandbox test data (user 7977). Therefore **no entitlement migration is
needed** — the six existing additive sources already use `expired_at` correctly, and switching
Apple from absolute→additive affects no real production entitlement. New tables/columns
(`subscription_credits`) start empty.

---

## 11. Phasing

- **Phase 0 — DONE (uncommitted on `main`).** Read-consistency: `isSubscriptionLive` +
  `deriveVerifiedStatus`; `GetActiveSubscriptions` gate; drop hardcoded `active`. 15 Go subtests +
  webapp button regression green. **Independently unblocks App Store review — ship first.**
- **Phase 1.** Entitlement module + `subscription_credits` table; Apple → additive credit by
  `expiresDate` delta with dedup (INV1); delete `computeRecurringEntitlement`; hard-reject empty
  `appAccountToken` (§8.0); permanent binding (INV9). Tests pin INV1–INV5, INV8, INV9.
- **Phase 2.** Grace/billing-retry `coverThrough` (§5.1); reconciliation + finish-gating
  verification (INV7); webhook JWS x5c signature verification (T10); lapsed-row cleanup.
- **Phase 3.** Server-side one-time-order block for live auto-renewing plans (INV6, T3); confirm
  client UI hiding.
- **Phase 4 (optional).** Extend best-effort "delay the charge".

---

## 12. Test strategy

- **Pure unit (no DB):** `isSubscriptionLive`, `deriveVerifiedStatus` (done); additive-credit +
  dedup decision; clawback math (floor-at-now); `expiresDate`-delta computation; grace
  `coverThrough`.
- **Mock-DB:** order/verify handlers reject when live auto-renewing plan (INV6); dedup blocks
  double credit (INV1); hard-reject empty `appAccountToken`; binding mismatch rejects (INV9).
- **Integration (real dev MySQL, `skipIfNoConfig`):** purchase + renewal + gift → no absorption
  (INV3); idempotent re-verify (INV1); cancel keeps time (INV4); refund clawback floored (INV2);
  reconciliation credits a missed transaction (INV7); grace covers the window (§5.1).
- **Webhook:** forged/unsigned payload rejected (T10); valid notification triggers GetTransaction.
- **webapp vitest:** purchase vs Account agree; subscribe button in-flight guard (done).
- **Release confidence:** functional/money change capped 6–7/10 until real-device sandbox smoke;
  desk-verified pure logic 9–9.5/10.

---

## 13. Open questions

None blocking. One implementation nuance pinned with tests in writing-plans: the exact
`expiresDate`-delta vs first-purchase-from-now arithmetic on late reconciliation of an old missed
transaction (must not over-credit beyond Apple's actual coverage).
