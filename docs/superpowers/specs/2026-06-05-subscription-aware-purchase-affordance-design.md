# Subscription-Aware Purchase Affordance (Provider-Neutral)

- **Date**: 2026-06-05
- **Status**: Design — approved approach, pending spec review
- **Branch**: builds on `feat/ios-storekit-iap-center`
- **Author**: architecture pass (10/10 satisfaction gate)

## 1. Problem

The app now has **two structurally different ways to pay**, writing to **one** entitlement field (`user.ExpiredAt`):

| Source kind | Behavior | Today | Future |
|---|---|---|---|
| **Additive (one-time)** | `ExpiredAt += days` (买断, stacks, no recurring charge) | WordGate one-time, license codes, delegate-pay, admin grant, rewards | — |
| **Recurring (subscription)** | `ExpiredAt = max(current, period_end)` (auto-renews, charged every period) | **Apple IAP** | **Stripe sub, Google Play sub, …** |

Grounded in code:
- Additive grant: `logic_member.go addProExpiredDays` — `ExpiredAt += days` (stacking).
- Recurring grant: `logic_apple_iap.go computeAppleEntitlement` — max-raise only, never lowers.

This is fine *until a user pays for time they already own*. Two failure modes:

1. **Recurring over additive — overlap waste.** User buys 3y one-time (expiry 2029), then subscribes on iOS. Apple charges $49.99 immediately and yearly; `computeAppleEntitlement(2029, 2027)` → no advance → **the user gets zero added time and keeps being charged** until Apple's cumulative renewals pass 2029. Up to ~$150 dead-weight loss + refund tickets + bad reviews + App Review optics.

2. **Recurring over recurring — perpetual double-charge.** (Future, once a 2nd recurring provider exists.) User has an active Apple sub, then starts a Stripe sub → **both auto-charge forever**. Strictly worse than overlap waste.

The current iOS gating (`os === 'ios'`) is the wrong axis. The real axis is **additive vs recurring** × **provider**. The fix must be expressed in those terms so a future Stripe subscription slots in as another *recurring provider*, not as a special case.

## 2. Goals / Non-Goals

**Goals**
- Never let a user start a purchase that pays for overlapping time.
- Make the membership/entitlement model provider-neutral so adding a recurring provider (Stripe/Google) is an *adapter*, not a redesign.
- Drive every purchase-related UI from one provider-neutral signal.
- Ship v1 = approved 方案 A behavior (active member → no new-subscription CTA), with 方案 B (near-expiry conversion) reachable by one config flip.

**Non-Goals (YAGNI — explicitly NOT built now)**
- No Stripe / Google verify, webhook, billing-portal, or storage adapter. Only the seam.
- No change to the one-time/additive WordGate rail on web/desktop/Android.
- No tier-reconciliation rework (see §9 accepted edge).

## 3. Core Concepts

- **Entitlement** = `user.ExpiredAt` (single source of truth) + `user.Tier`. Unchanged.
- **Additive sources** contribute to `ExpiredAt` and are *not* tracked as ongoing state — once granted, they're just time. Reflected to the client purely via `expiredAt`/`tier`.
- **Recurring subscriptions** are ongoing state (they keep charging). They get a first-class row and are surfaced to the client as a list. A user has **0..N** active recurring subscriptions.
- **Purchase rails** differ by platform: iOS = subscription rail (StoreKit). Web/desktop/Android = additive rail (WordGate one-time) today; web gains a subscription rail when Stripe lands. The *subscription rail* is what this design governs.

## 4. Architecture

### 4.1 Center — storage (neutral `subscriptions` table)

Rename the **not-yet-deployed** `AppleSubscription` (model.go:648) → provider-neutral `Subscription`. Subscription semantics are uniform across providers, so a single discriminated table is clean (not nullable-soup):

| Column | Was (AppleSubscription) | Notes |
|---|---|---|
| `provider` | — (new) | `'apple'` default; `'stripe'`, `'google'` later |
| `user_id` | `user_id` | first-write-wins ownership preserved |
| `provider_subscription_id` | `original_transaction_id` | stable sub id; `uniqueIndex(provider, provider_subscription_id)` |
| `provider_latest_ref` | `last_transaction_id` | latest txn/event ref |
| `current_period_end` | `expires_date` (ms) | **normalized to seconds** — Apple adapter converts ms→s on write; removes scattered `/1000` in apply/revoke |
| `auto_renew` | `auto_renew_status` (int32) | bool |
| `environment` | `environment` | Apple sandbox/prod; Stripe live/test |
| `status` | `status` | `active|grace|billing_retry|expired|revoked` |
| `last_event_id` | `last_notification_uuid` | webhook idempotency, provider-neutral |
| `product_id` | `product_id` | maps to `Plan.AppleProductID` (→ later a generic `Plan.<provider>ProductID` or a join table) |

Deploy note: table not in production yet (branch unpushed). AutoMigrate creates `subscriptions`; the dev-only `apple_subscriptions` is orphaned and harmless. `logic_apple_iap_test.go` references updated.

### 4.2 Center — neutral grant core + provider seam

- `CanonicalSubscription` (in-memory struct): `{Provider, ProviderSubscriptionID, ProviderLatestRef, ProductID, CurrentPeriodEndSec, AutoRenew, Environment, Status, Ownership}`. The contract every provider adapter produces.
- Grant core (provider-agnostic), renamed from the Apple-named functions, math already neutral:
  - `computeRecurringEntitlement(currentExpiredAt, periodEndSec, nowSec)` (= today's `computeAppleEntitlement`, pure).
  - `upsertSubscription(tx, *Subscription)` (= `upsertAppleSubscription`; first-write-wins on `(provider, provider_subscription_id)`).
  - `applyRecurringSubscription(tx, *Subscription)` (= `applyAppleSubscription`; max-raise `ExpiredAt`, tier-on-first-order, activate, `UserProHistory`).
  - `revokeSubscription(ctx, *Subscription)` (= `revokeAppleSubscription`; conservative clawback).
- Provider seam (interface; **Apple is the only implementation now**):
  ```
  type RecurringProvider interface {
      // Verify re-fetches the canonical subscription from the provider's
      // authenticated API (Apple: appstore.GetTransaction). Trust anchor.
      Verify(ctx, ref string) (*CanonicalSubscription, error)
      // ManageSurface tells the client how to let the user manage/cancel.
      ManageSurface() ManageSurface  // {kind:'apple_settings'|'url', url?}
  }
  ```
  Webhook parsing stays a provider-specific handler entry (`api_apple_webhook.go`) that normalizes to `(CanonicalSubscription, eventType)` then calls the shared core. Adding Stripe = `logic_stripe_sub.go` + `api_stripe_webhook.go` implementing the same seam; **core untouched**.

### 4.3 Center — read model (`subscriptions[]` on `/api/user/info`)

Add to `DataUser` (type.go:99, next to `AppleAccountToken`):
```
Subscriptions []DataSubscription `json:"subscriptions,omitempty"`

type DataSubscription struct {
    Provider         string         `json:"provider"`          // 'apple' | 'stripe' | ...
    Tier             string         `json:"tier"`
    CurrentPeriodEnd int64          `json:"currentPeriodEnd"`  // unix seconds
    AutoRenew        bool           `json:"autoRenew"`
    Manage           ManageSurface  `json:"manage"`            // {kind, url?}
}
```
Populated in `buildDataUserWithDevice` (api_user.go:~493) via `GetActiveSubscriptions(userID)` — reads `subscriptions WHERE user_id AND status IN (active,grace,billing_retry)`. v1 returns 0 or 1 element. **A list, not a bool** — this is what lets the purchase flow detect *any* active recurring sub across providers and refuse to start a second one (failure mode 2).

Additive entitlement is **not** in this list; it stays reflected by `expiredAt`/`tier` (unchanged).

### 4.4 Webapp — pure decision function

```
type AffordanceMode = 'subscribe' | 'manage' | 'status';

function subscriptionAffordance(input: {
  expiredAt: number;            // unix seconds
  subscriptions: DataSubscription[];
  nowSec: number;
  renewWindowDays: number;      // v1 = 0
}): { mode: AffordanceMode; activeSub?: DataSubscription }
```
Rule:
1. `subscriptions.length > 0` → `manage` (`activeSub = subscriptions[0]`).
2. else `expiredAt <= now` **OR** `(expiredAt - now) <= renewWindowDays*86400` → `subscribe`.
3. else → `status`.

`renewWindowDays = 0` ⇒ **exactly 方案 A**: any active additive member → `status`; only the truly expired get `subscribe`. 方案 B = bump to 30/60. Pure + unit-tested.

### 4.5 Webapp — integration

- **Single SoT hook** `useSubscriptionAffordance()` wraps the pure fn over `useUser()` + `renewWindowDays` (build/remote config).
- **Capability gate stays** (already landed this session): nav `/purchase` entry (`BottomNavigation`/`SideNavigation`) and the `App.tsx` route are gated on `os === 'ios' && !iap` (registered when IAP is possible). This controls *whether IAP exists*, not *what to show*.
- **Content is affordance-driven** — `IapPurchaseSheet` (or the Purchase page on iOS) renders by `mode`:
  - `subscribe` → the StoreKit purchase UI (current sheet).
  - `manage` → "管理订阅" → open `activeSub.manage` (`apple_settings` ⇒ `itms-apps://apps.apple.com/account/subscriptions`; `url` ⇒ `openExternal(url)`).
  - `status` → "会员有效期至 {date}", no buy button.
- **Account CTAs** (`Account.tsx:380` 续费, `:489` 开通) and **LoginDialog** (`:423` 激活) consult the same hook: render a CTA only when `mode !== 'status'`; route `subscribe`→sheet, `manage`→manage surface.
- **Restore** stays always-available on iOS (Apple requirement); independent of affordance (re-verify never double-charges).

## 5. Case Matrix → Affordance (provider-neutral)

| State | `subscriptions[]` | remaining | mode | iOS UI |
|---|---|---|---|---|
| Non-member / expired | empty | ≤ 0 | `subscribe` | StoreKit buy |
| Active recurring (any provider) | ≥1 | — | `manage` | 管理订阅 → provider surface |
| Active additive only, remaining > window | empty | > window | `status` | 有效期至 X, no buy |
| Active additive only, remaining ≤ window | empty | ≤ window | `subscribe` (B only; v1 window=0 ⇒ `status`) | StoreKit buy (B) |
| Both | ≥1 | — | `manage` | 管理订阅 |

## 6. Data Flow

Purchase (iOS) → StoreKit → `transactionId` → webapp `verify` → Center `RecurringProvider.Verify` (trust anchor) → `upsertSubscription` + `applyRecurringSubscription` (max-raise `ExpiredAt`) → next `/api/user/info` returns updated `expiredAt` + `subscriptions[]` → `useSubscriptionAffordance` recomputes → UI flips to `manage`. Renewals/refunds via webhook → same core → read model updates.

## 7. "Active subscription" predicate

`status IN (active, grace, billing_retry)`. Grace and billing-retry **count as active** so we never offer a second subscription to someone Apple is still retrying — prevents accidental double-subscribe. A sub only leaves "active" on `expired`/`revoked`.

## 8. Extension proof — adding Stripe later

1. `logic_stripe_sub.go`: implement `RecurringProvider` (`Verify` via Stripe API; `ManageSurface` → `{kind:'url', url: <billing portal>}`).
2. `api_stripe_webhook.go`: normalize Stripe events → `(CanonicalSubscription{Provider:'stripe'}, eventType)` → shared core.
3. Write rows to `subscriptions` with `provider='stripe'`.
4. Web subscription rail renders `subscribe` via Stripe checkout.
- **Untouched**: grant core, `subscriptions[]` read model, `subscriptionAffordance`, all iOS UI. The double-charge guard works automatically because `subscriptions[]` is cross-provider.

## 9. Edge cases & accepted-v1 behavior

- **Tier set-once leak** (accepted, flagged): tier is written only on first order (`IsFirstOrderDone`). An *expired* `family` member who subscribes `basic` IAP keeps `family` tier (Center won't downgrade). Benefit leak, minor; active family members never reach `subscribe` (they're `manage`/`status`). Proper tier reconciliation is separate future work — documented, not silently ignored.
- **Multi-provider double-charge**: prevented by the cross-provider `subscriptions[]` check (the reason the read model is a neutral list).
- **Near-expiry conversion** (additive member wanting auto-renew before expiry): deferred to 方案 B (`renewWindowDays > 0`). In v1 they convert after expiry or via the website.
- **Restore**: always available; re-verify is idempotent, never charges.
- **Expired/lapsed Apple subscriber re-subscribing**: `subscriptions[]` empty (status not active) → `subscribe`; upsert preserves original ownership.

## 10. Testing

- **Center**: unit `computeRecurringEntitlement` (max semantics, no-lower); integration (real dev MySQL) for upsert ownership/first-write-wins, apply max-raise, `GetActiveSubscriptions` predicate, read-model population. Reuse existing apple integration tests, renamed.
- **Webapp**: pure `subscriptionAffordance` table tests (all §5 rows, window=0 and >0); hook test; IapPurchaseSheet mode-render tests (subscribe/manage/status); guard `expect(no external link on iOS)`.

## 11. Scope / phasing (for the implementation plan)

1. Center: rename model → `subscriptions` + neutral columns; refactor grant core + Apple adapter; `GetActiveSubscriptions`; read-model `subscriptions[]`. Tests.
2. Webapp: `subscriptionAffordance` + `useSubscriptionAffordance`; wire `renewWindowDays=0`.
3. Webapp UI: IapPurchaseSheet/Purchase-page modes; Account + LoginDialog CTAs; keep capability gate + restore.
4. (Already landed this session) `/purchase` route + nav capability un-gate — the foundation this sits on.

Stripe adapter, 方案 B window, and tier reconciliation are explicitly out of scope.
