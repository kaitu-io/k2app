# Disconnect Rating — Stars Redesign

**Date:** 2026-05-02
**Scope:** Webapp only. Backend zero-change.
**Supersedes UX of:** [2026-03-26-disconnect-feedback-dialog-design.md](./2026-03-26-disconnect-feedback-dialog-design.md), [2026-04-07-connection-rating-design.md](./2026-04-07-connection-rating-design.md) (rating endpoint stays unchanged)

## Problem

Current `DisconnectFeedbackDialog` shows a forced two-button choice ("好" / "不好") on every authenticated disconnect. The "不好" button is rendered as `variant="contained" color="error"` — a large red button visually heavier than the outlined "好". Hypothesis: the visual asymmetry is biasing users toward "不好", inflating the negative-rating share. We are also dialoging on connections that immediately dropped, where the user has no real experience to rate.

## Goals

1. Reduce visual coercion toward negative ratings by replacing the two-button layout with a 5-star rating where positive ratings are friction-free.
2. Suppress the dialog when the user did not meaningfully experience the connection (very short connection, or engine-initiated disconnect).
3. Preserve the existing actionable signal pipeline (auto-ticket + log upload + Slack) but only for genuinely bad ratings (1-2 stars), and enrich tickets with structured "what went wrong" tags chosen by the user.
4. Zero backend changes. The existing `POST /api/user/connection-rating` and `ConnectionRating` table are unchanged.

## Non-goals

- Persisting per-star (1-5) granularity in the rating table. Backend still only stores `good`/`bad`. Per-star analytics are out of scope under the zero-backend-change constraint.
- Changing the existing admin statistics dashboard at `/app/connection-ratings/statistics`.
- Changing the negative-feedback ticket flow (`api/user/ticket` with `auto_generated=true`), the device-log registration flow (`api/user/device-log`), or the Slack notify endpoint (`api/user/feedback-notify`). Those are reused as-is.

## Suppression Rule

In `webapp/src/stores/connection.store.ts` `disconnect()`, set `feedbackRequested` to true only when:

1. User is authenticated (current behavior).
2. `connectedTunnel != null` at disconnect time (current behavior).
3. **`durationSec >= 20`** (new). `Math.round((Date.now() - connectedAt) / 1000) >= 20`. Otherwise log `console.info('[Connection] feedback dialog suppressed: durationSec=Ns')` and skip.
4. **Trigger source is the user's own disconnect call** (already inherent — `feedbackRequested` is only ever set inside `connection.store.disconnect()`; engine/network/server-initiated transitions to `idle` go through `vpn-machine` and never touch `feedbackRequested`, so no extra code needed).

The 20-second threshold is a product judgment, not a technical constraint. It can be revisited based on the suppression-log volume.

## Dialog UX

### State machine

```
              ┌─────────────────────────────┐
              │  STATE_RATING (initial)     │
              │  Title: "本次连接体验如何？" │
              │  5 hollow stars, hoverable  │
              │  No buttons, no close X     │
              └─────────────┬───────────────┘
                            │
                ┌───────────┼────────────────┐
                │           │                │
              5★/4★        3★            2★/1★
                │           │                │
                ▼           ▼                ▼
          submit good   submit bad     STATE_DETAIL
          + close       + close        ┌──────────────────────────────┐
          + toast       + toast        │ stars row at top (still      │
                                       │  changeable)                 │
                                       │ "遇到了什么问题？(可选)"      │
                                       │ chips: 速度慢 / 连不上 /      │
                                       │  经常断开 / 视频或网页       │
                                       │  打不开 / 其他               │
                                       │ [提交]                       │
                                       └─────────────┬────────────────┘
                                                     │ submit
                                                     ▼
                                            submit bad + ticket(chips)
                                            + close + toast
```

### Rating-to-action table

| Stars | Backend `rating` | Negative machinery (ticket + log + slack) | Detail step |
|-------|------------------|-------------------------------------------|-------------|
| 5★    | `good`           | no                                        | no          |
| 4★    | `good`           | no                                        | no          |
| 3★    | `bad`            | no                                        | no          |
| 2★    | `bad`            | yes (chips written into ticket content)   | yes         |
| 1★    | `bad`            | yes (chips written into ticket content)   | yes         |

3 stars is "meh — counts as bad in stats but not worth a ticket." 1-2 stars is "the user has a complaint we want to act on."

### Visual

- No `color="error"`, no `variant="contained"` red button anywhere in the dialog. Eliminate the destructive visual weight that biased the original two-button layout.
- 5 stars rendered with MUI `Rating`, `size="large"`, filled in `warning.main` (the same gold token used in `webapp/src/components/StarRating.tsx`).
- Layout centered, `minWidth: 320`. Title sourced from existing i18n key `feedback.disconnectFeedback.title` (no copy change needed for the title).
- No close X. No backdrop dismiss. Dialog stays mandatory — the goal is to reduce skew, not reduce volume.
- Toast on close: `useAlertStore.showAlert(t('feedback.disconnectFeedback.thankYou'), 'info')`. Matches the current "感谢反馈" message.
- Chips: MUI `Chip`, `variant="outlined"` when unselected, `variant="filled"` when selected, `gap: 1`, wraps automatically.
- Detail step has only a `提交` button (primary contained, no destructive color). No "稍后" / skip button. Submit is always enabled — empty chip selection is allowed.

### Interaction details

| Detail | Behavior |
|---|---|
| 4★ / 5★ tap | `onChange` immediately fires fire-and-forget submit and closes dialog. Network failure is swallowed (matches current). |
| 3★ tap | Same as 4★/5★ but with `rating='bad'`. No detail step. |
| 1★ / 2★ tap | Transitions to `STATE_DETAIL`. Submit is **not** fired yet. |
| 1★/2★ in detail then user changes to 4★/5★ | Exits `STATE_DETAIL`, fires immediate good submit, closes. Chip selections discarded. |
| 1★/2★ in detail then user changes to 3★ | Exits `STATE_DETAIL`, fires immediate bad submit, closes. Chip selections discarded. |
| 1★/2★ in detail then user changes between 1★ and 2★ | Stays in `STATE_DETAIL`. Selected chips preserved. |
| Submit button in detail with no chips selected | Allowed. `tags=[]`. Ticket is still created (1-2 stars always trigger negative machinery). |
| Rapid tap on 4★ then 5★ before close | First tap already submitted + closed. Second tap is on an unmounted component. Acceptable: 4-5 stars are committed instantly with no undo affordance. |
| Re-`disconnect` while dialog still open | Existing `useEffect` semantics: new `pendingFeedback=true` is ignored because the dialog is already open consuming the previous one. Maintain current behavior. |

### Copy (zh-CN)

Reuse where possible. New keys go into `webapp/src/i18n/locales/zh-CN/feedback.json` under the existing `feedback.disconnectFeedback` namespace.

```json
{
  "disconnectFeedback": {
    "title": "本次连接体验如何？",        // unchanged
    "thankYou": "感谢反馈",                // unchanged
    "detailTitle": "遇到了什么问题？(可选)", // new
    "submit": "提交",                       // new
    "tags": {                               // new
      "slow": "速度慢",
      "cantConnect": "连不上",
      "frequentDrops": "经常断开",
      "contentBlocked": "视频或网页打不开",
      "other": "其他"
    }
  }
}
```

The `good` and `bad` keys become unused — removed from zh-CN and from all other locale files.

## Backend Mapping (Zero-Change)

`POST /api/user/connection-rating` is called with the existing schema. Stars are mapped client-side:

```ts
const rating: 'good' | 'bad' = stars >= 4 ? 'good' : 'bad';
```

The body sent to the endpoint is byte-for-byte identical to today's payload. The backend does not need to know stars exist.

## Negative Machinery Routing

The five-step `submitNegativeFeedback()` flow in `webapp/src/components/DisconnectFeedbackDialog.tsx` is preserved. Trigger condition changes from `rating === 'bad'` to **`stars <= 2`**. 3-star ratings only call `submitRating()` and do not invoke log upload, ticket creation, or Slack notification.

Selected chips are joined into the ticket content. The label text used in the ticket is the **zh-CN label regardless of the user's current locale** (e.g. always `速度慢, 经常断开`, never `Slow, Frequent drops`). The Kaitu admin team reads tickets in Chinese, so locking ticket content to zh-CN keeps tickets uniform across user populations and avoids per-locale label drift. Implement by reading the chip label through a helper that resolves the zh-CN string directly, not via `t()`.

```
[Auto] 用户报告体验问题 (2★)
Tags: 速度慢, 经常断开

Server: <name> (<domain>)
Region: <country>
Type: <source>
Duration: <durationSec>s
Rule: <ruleMode>
OS: <os>
Version: <appVersion>
Commit: <commit>
```

If no chips selected, the `Tags:` line reads `Tags: 无` (literal zh-CN, same locale rule as above).

## Edge Cases

| Scenario | Handling |
|---|---|
| Submit fails (offline / cloudApi error) | Swallow. Matches current best-effort contract. No retry. `feedbackId` UUID + backend `(user_id, feedback_id)` unique index would make retry safe, but we deliberately don't retry — losing a rating is acceptable; UX confusion from delayed retry is not. |
| `refreshNetworkEnv()` slow or fails | Submit is fire-and-forget after the dialog closes. Network field may be empty. Already current behavior. |
| Webapp killed while dialog open | `pendingFeedback` is in-memory zustand, not persisted. Lost on restart. Matches current behavior. |
| Webapp goes to background while dialog open (mobile) | Dialog re-appears on foreground. State preserved by zustand. |
| User taps stars very rapidly | MUI `Rating.onChange` is idempotent on identical values; transition logic reads `newValue` once per tick. No debounce needed. |
| Cross-platform consistency | Dialog is shared webapp code → desktop / mobile / standalone all see the same UX. Toast goes through `useAlertStore` which is also platform-agnostic. |

## Test Plan

### `webapp/src/components/__tests__/DisconnectFeedbackDialog.test.tsx`

Extend the existing file. Drop tests that asserted on the two-button labels.

1. `renders nothing when pendingFeedback is false` — current, retained.
2. `5 stars: submits rating='good' immediately and closes, no detail step shown`
3. `4 stars: submits rating='good' immediately and closes, no detail step shown`
4. `3 stars: submits rating='bad' immediately and closes, no detail step shown, no negative machinery (no uploadLogs / ticket / feedback-notify calls)`
5. `2 stars: enters STATE_DETAIL, no submit fired yet`
6. `1 star: enters STATE_DETAIL, no submit fired yet`
7. `2 stars in detail with no chips: submit sends rating='bad', creates ticket with body containing "Tags: 无"`
8. `2 stars in detail with chips: submit sends rating='bad', creates ticket with body containing "Tags: 速度慢, 经常断开" (using zh-CN labels)`
9. `1 star: triggers negative machinery (uploadLogs + ticket + feedback-notify all called)`
10. `2 stars: triggers negative machinery (same)`
11. `1 star then 5 stars: leaves detail, submits rating='good', no negative machinery fired, no ticket created`
12. `1 star then 3 stars: leaves detail, submits rating='bad', no negative machinery fired`
13. `submission failure is swallowed (no thrown error, dialog still closes)`

### `webapp/src/stores/__tests__/connection.store.test.ts` (extend if exists, else create minimal coverage)

14. `disconnect with durationSec < 20: feedbackRequested stays false, console.info logged`
15. `disconnect with durationSec >= 20: feedbackRequested set to true, lastConnectionInfo populated`

### Manual cross-platform smoke

After yarn-running webapp, verify on Tauri dev (`make dev-macos`) and Capacitor dev (`make dev-ios` or `make dev-android`):

- Connect → wait 25s → disconnect → dialog appears → tap 5★ → toast appears → dialog closed.
- Connect → wait 25s → disconnect → dialog appears → tap 1★ → chips visible → select 2 chips → tap 提交 → toast appears → dialog closed.
- Connect → wait 5s → disconnect → no dialog appears, console shows suppression log.
- Connect → kill server / unplug network → dialog does NOT appear (engine-initiated idle).

## Out of Scope (explicit)

- Telemetry beacon for "dialog opened but ignored" — dialog is mandatory, this state cannot occur.
- Per-platform dialog styling differences. The same MUI dialog renders on all three platforms.
- Migration of historical `ConnectionRating` rows — none needed, schema unchanged.
- Backend admin UI changes — none needed.

## Rollout

Single PR. No staged rollout, no feature flag, no deploy ordering. Webapp ships in next desktop/mobile/web release. Existing backend handles new payloads identically to old.

## Risks

| Risk | Mitigation |
|---|---|
| 20s threshold is wrong (too aggressive → bias good ratings up by suppressing legitimate complaints; too lenient → still surfaces the dialog on dropouts) | Suppression log lets us count how many disconnects fall under threshold per day. Adjust if the suppression rate is >50% or <5%. |
| Loss of per-star granularity (only good/bad in DB) | Accepted under zero-backend-change constraint. If product wants 1-5 distribution later, add a `stars TINYINT` column then. |
| 3-star users feel unheard (no chip ask, no ticket) | Acceptable. 3 stars = "fine, not great" is not a complaint that needs follow-up; adding ticket noise hurts more than the small UX cost. |
| Mobile users tap a star by accident | 4-5 stars submit instantly with no undo. Cost: a stray accidental "good" rating. Negligible compared to the bias problem we're fixing. 1-2 stars are recoverable via re-tap before submit. |
