# Disconnect rating — ambient bottom strip (redesign)

**Status:** spec, awaiting plan
**Supersedes:** `2026-05-02-disconnect-rating-stars-design.md` (the blocking `DisconnectFeedbackDialog`)
**Date:** 2026-05-14

## Goal

Weaken the post-disconnect rating interaction so it never blocks the user. Replace the modal Dialog with a non-blocking strip pinned just above the bottom navigation. The strip drains a 5-second progress bar; if the user does not act, we default to a 5-star (good) rating and dismiss. The API payload remains plain `good`/`bad` — see [Rating semantics](#rating-semantics) for the framing.

## Why

The current `DisconnectFeedbackDialog` is a `Dialog` with `disableEscapeKeyDown` and a noop backdrop click, so the user has to interact with it to dismiss. That is the wrong tradeoff for what is, in most sessions, a passive signal-gathering touchpoint — it blocks the natural next action (reconnect, switch tab, exit). Users who don't rate explicitly are overwhelmingly satisfied (matches our observed positive-skew); making silence default to 5★ matches reality and removes friction.

## Non-goals

- Changing what `submitRating` / `submitNegativeFeedback` post to the backend (`POST /api/user/connection-rating`, `POST /api/user/ticket`, `uploadLogs`, `POST /api/user/device-log`, `POST /api/user/feedback-notify`). The payload schemas, endpoints, and log-upload flow stay byte-identical.
- Changing the gating in `connection.store` (`MIN_FEEDBACK_DURATION_SEC = 20`, user-initiated disconnect only, `pendingFeedback` promotion on VPN→idle).
- Changing the i18n strings under `feedback:feedback.disconnectFeedback.*`. Title, tag labels, "感谢反馈", and detail prompt all reuse existing keys.

## UI

### Idle state (just stars, countdown running)

```
┌─────────────────────────────────────────────────────────┐
│                                                         │
│                Dashboard content unchanged              │
│                                                         │
├─────────────────────────────────────────────────────────┤
│   本次连接体验如何？      ☆  ☆  ☆  ☆  ☆                │  ← strip (~56px)
│   ▓▓▓▓▓▓▓▓▓░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░               │  ← progress (2px)
├─────────────────────────────────────────────────────────┤
│   Dashboard   │  Invite  │  Discover  │  Account        │  ← BottomNav
└─────────────────────────────────────────────────────────┘
```

- Strip height ~56 px (label + stars + progress) on mobile, same on desktop
- Linear progress bar drains 5 000 ms left → right
- No close (×) button — escape paths are timeout, star tap, or external dismiss event (see below)
- Mounts via `max-height` expand (0 → 56 px, 250 ms ease-out); dismisses via collapse (56 px → 0). Layout-aware: Main shrinks, Dashboard content + Advanced Settings shift up uniformly

### Expanded state (after 1-2★ tap — negative path)

```
┌─────────────────────────────────────────────────────────┐
│   本次连接体验如何？      ★  ★  ☆  ☆  ☆                │
│   遇到了什么问题？(可选)                                │  ← detailTitle
│   [速度慢] [连不上] [经常断开] [视频或网页打不开] [其他]│  ← tag chips
│                                  [   提交   ]           │  ← submit btn
├─────────────────────────────────────────────────────────┤
│   Dashboard   │  Invite  │  Discover  │  Account        │
└─────────────────────────────────────────────────────────┘
```

- Strip grows to ~140 px to fit chips + submit
- Countdown progress bar disappears once expanded (no auto-fire in negative state)
- User can still tap any underlying page area; strip overlays, does not capture pointer outside its own bounds

### Positioning — push, don't overlay

**The strip is a flex sibling of `<BottomNavigation />` in `Layout.tsx`, NOT an absolutely-positioned overlay.** It contributes height to the layout when visible.

Reason: Dashboard already has its own sticky-bottom block — the **Advanced Settings** collapsible bar (`Dashboard.tsx:593-652`, `mt: 'auto'` inside Dashboard's flex column). An absolute overlay at `bottom: 56px` would land exactly on top of the Advanced Settings header button (~44 px), blocking the user from expanding routing / DNS / always-on settings. A flex sibling pushes Main upward so the Advanced Settings header (and any expanded panel) shifts up unobstructed.

```
Layout flex column (height: 100%)
├─ AnnouncementBanner          flexShrink: 0
├─ ServiceAlert                flexShrink: 0
├─ <Main>  flex: 1             ← shrinks when strip mounts
│   └─ TabPage Box  flex: 1, overflow: auto
│       └─ Dashboard  flex column
│           ├─ ... scrollable content ...
│           └─ Advanced Settings  mt: 'auto'  ← stays at bottom of Dashboard
├─ DisconnectFeedbackStrip     flexShrink: 0  ← NEW. hidden = height 0
└─ BottomNavigation (mobile)   flexShrink: 0
```

Animation: `max-height: 0 → ~56 px` (countdown) or `→ ~140 px` (chips expanded), `transition: max-height 0.25s cubic-bezier(.4, 0, .2, 1)`. The hidden state is `max-height: 0; overflow: hidden` — zero layout footprint, no `z-index` battles, no overlap with sticky-bottom UI inside Dashboard.

Sidebar offset on desktop: the strip lives outside `<Main>`, so it does not inherit Main's `marginLeft`. Apply `marginLeft: isDesktop ? SIDEBAR_WIDTH : 0` directly on the strip wrapper.

`env(safe-area-inset-bottom)` is already handled by `<BottomNavigation>`'s own `paddingBottom`; the strip needs none.

## Interaction state machine

```
                 pendingFeedback=true
                          │
                          ▼
                   ┌─────────────┐
       timeout (5s)│  COUNTDOWN  │ tap ≥3★ ─────► submit good/bad → DISMISS
        ───────────│             │ tap 1-2★ ─────► CHIPS
       submit good │             │
                   └──────┬──────┘
                          │ external dismiss event
                          ▼ (reconnect / new qualifying disconnect)
                       DISMISS
                          │
                          │
                   ┌─────────────┐
            submit │    CHIPS    │ tap ≥3★ ──────► submit good/bad (+ current tags if bad)
       ───────────│             │                   → DISMISS
          fire bad │             │ tap 1-2★ ──────► update star highlight, stay in CHIPS
       + tags     │             │ toggle chip ───► update tags
                   └──────┬──────┘
                          │ external dismiss event
                          ▼
                    submit bad + current tags → DISMISS
```

Rules:
- `≥3★` while in CHIPS does *not* re-enter the auto-submit-on-tap shortcut for "good" tags. 3★ = bad-without-tags; 4-5★ = good (tags discarded — `submitRating('good', ...)` has no tag field). 1-2★ stays in CHIPS.
- DISMISS = slide down + `clearPendingFeedback()`.

## Edge cases

### 1. User taps Connect (reconnects) during countdown

The strip is showing 5 s of feedback for the just-ended session. User clicks the Dashboard Connect button.

Behavior: **treat reconnect as acceptance.** Fire `submitRating('good', info, randomUUID())` and dismiss the strip immediately. The connect call proceeds without delay.

Rationale: re-engaging the product within seconds is itself a positive behavioral signal. Holding the strip open while a new session starts would also collide with any future strip from this new session if it also disconnects.

Implementation: subscribe to `connectionStore.connectedTunnel` becoming non-null while strip is open. Or simpler: hook into the connect button via the existing `pendingFeedback` clear path — `connection.connect()` calls `clearPendingFeedback()` early in its flow as a side-effect of the strip's own onDismiss before strip closes.

Concrete approach: the strip component subscribes to `useVPNMachineStore` state. Any transition out of `idle` (i.e. `connecting`, `connected`, or `reconnecting`) while the strip is mounted triggers an automatic submit + dismiss. This naturally covers both manual connect taps and any other re-engagement path.

### 2. User taps Connect during CHIPS state

User went into negative-feedback chips, then changed their mind and reconnects. Their `1-2★` was an explicit signal — don't overwrite it with 5★.

Behavior: fire `submitNegativeFeedback(info, stars, currentTags)` and dismiss. The negative submission pipeline (log upload + ticket + Slack) runs in the background; connect proceeds in parallel.

### 3. New qualifying disconnect arrives while strip is open

Example: user reconnected during countdown (case 1 dismissed the strip with a synthesized 5★ good) → new 25-second session → disconnect again → `pendingFeedback` flips true again. Strip should appear with a fresh 5 s countdown for this new session.

Since the strip already dismissed in case 1, this is just a normal mount. No conflict.

Edge sub-case: what if the strip is somehow still mounted (race condition between subscriber timing)? The store enforces this by setting `pendingFeedback: false, lastConnectionInfo: null` in `clearPendingFeedback`, and the new disconnect overwrites `lastConnectionInfo`. The strip component should treat `pendingFeedback` going false → true (with new `lastConnectionInfo` identity) as a remount — useEffect on `[pendingFeedback, lastConnectionInfo]` already does this.

### 4. User navigates between tabs while strip is showing

Strip lives in `Layout.tsx`, so navigation between `/`, `/invite`, `/discover`, `/account` doesn't unmount it. Countdown keeps draining. This is a behavior change from today (current Dialog is mounted in Dashboard and is keep-alive-tied).

### 5. User navigates to a non-tab route (`/purchase`, `/account/wallet`, etc.)

Same — Layout is the parent of all routes. Strip stays mounted.

### 6. Visibility-hidden window / app backgrounded

If the page goes hidden (`document.visibilityState === 'hidden'`), pause the countdown. Resume on visibilitychange → visible. Rationale: a backgrounded tab shouldn't burn through the 5 s window the user never saw.

### 7. CHIPS state — what if user just walks away?

There's no timeout in CHIPS. Strip persists until Submit / star upgrade / external dismiss event (reconnect / new disconnect). This is by design — explicit negative input should not silently default; users who tap 1★ then ignore the chips can still reconnect to dismiss, and most users in this state will actively engage with the chips.

If we observe in telemetry that lots of CHIPS sessions go indefinitely undismissed, revisit by adding a 30 s tail timeout that fires `submitNegativeFeedback` with current tags. Out of scope for v1.

### 8. Advanced Settings stays tappable

Dashboard's Advanced Settings bar (`Dashboard.tsx:593-652`) sits at the bottom of the Dashboard scroll area via `mt: 'auto'`. When the strip is visible, the strip's height push shifts Main upward, which shifts Dashboard's flex column upward, which means the Advanced Settings header (collapsed ~44 px) is now at `(bottom of Main) - 56 px = (bottom of viewport) - 112 px` — fully visible, fully tappable. User can expand routing / DNS / always-on settings while the 5 s countdown runs.

If user expands Advanced Settings while strip is open, the expanded panel (up to 40 vh) grows downward inside Dashboard; Dashboard's internal scroll absorbs the growth. Strip is unaffected.

This is the reason the strip is a flex sibling rather than an absolute overlay — see "Positioning" above.

### 9. App killed mid-strip

State is in-memory (`useState`). On next launch the strip does not reappear. `pendingFeedback` is also in-memory, so cold start = clean slate. No persistence needed.

## Architecture

### File layout

| Path | Action | Why |
|---|---|---|
| `webapp/src/components/DisconnectFeedbackStrip.tsx` | new | Replaces the Dialog with a non-blocking bottom-anchored strip |
| `webapp/src/components/DisconnectFeedbackDialog.tsx` | delete | No longer used |
| `webapp/src/components/__tests__/DisconnectFeedbackDialog.test.tsx` | delete + rewrite as `DisconnectFeedbackStrip.test.tsx` | Test surface changes — countdown timer, dismiss events, expanded state |
| `webapp/src/pages/Dashboard.tsx` | edit | Remove `<DisconnectFeedbackDialog />` mount at line 654 |
| `webapp/src/components/Layout.tsx` | edit | Add `<DisconnectFeedbackStrip />` as sibling of `<BottomNavigation />` |
| `webapp/src/i18n/locales/zh-CN/feedback.json` | no change | Keys reused |
| Other locales (`en-US`, `ja`, `zh-TW`, `zh-HK`, `en-AU`, `en-GB`) | no change | Same keys |

### Submission helpers — reuse vs refactor

Keep `submitRating`, `submitNegativeFeedback`, `fireSubmit`, `formatConnectionInfo`, and the `TAG_KEYS`/`TAG_LABEL_ZH` constants. They're well-tested and unchanged. Move them into `webapp/src/services/disconnect-feedback.ts` so the new component file stays focused on UI/state, and so the silent-good path (no chips, no detail) can call `submitRating` directly without depending on `fireSubmit`'s star-routing branch.

```
webapp/src/services/disconnect-feedback.ts        // submit functions (moved from DisconnectFeedbackDialog.tsx)
webapp/src/components/DisconnectFeedbackStrip.tsx // UI + state machine only
```

### State

```tsx
type StripState = 'countdown' | 'chips';

const [state, setState] = useState<StripState>('countdown');
const [stars, setStars] = useState(0);
const [tags, setTags] = useState<TagKey[]>([]);
const [progress, setProgress] = useState(0); // 0..100, drains over 5s

const connectionInfoRef = useRef<LastConnectionInfo | null>(null);
const submittedRef = useRef(false); // guard against double-submit on dismiss races
```

### Countdown driver

Use `requestAnimationFrame` for smooth progress animation. Pause on `document.visibilityState === 'hidden'`. Track start time + accumulated paused duration.

```tsx
useEffect(() => {
  if (state !== 'countdown' || !pendingFeedback) return;
  let start = performance.now();
  let pausedAt: number | null = null;
  let rafId = 0;

  const tick = (now: number) => {
    const elapsed = now - start;
    if (elapsed >= DURATION_MS) {
      handleTimeout(); // fires good (silent default)
      return;
    }
    setProgress((elapsed / DURATION_MS) * 100);
    rafId = requestAnimationFrame(tick);
  };
  rafId = requestAnimationFrame(tick);

  const onVis = () => {
    if (document.visibilityState === 'hidden') {
      pausedAt = performance.now();
      cancelAnimationFrame(rafId);
    } else if (pausedAt !== null) {
      start += performance.now() - pausedAt;
      pausedAt = null;
      rafId = requestAnimationFrame(tick);
    }
  };
  document.addEventListener('visibilitychange', onVis);
  return () => {
    cancelAnimationFrame(rafId);
    document.removeEventListener('visibilitychange', onVis);
  };
}, [state, pendingFeedback]);
```

`DURATION_MS = 5000`.

### Reconnect detection

```tsx
const vpnState = useVPNMachineStore((s) => s.state);

useEffect(() => {
  if (!pendingFeedback) return;
  if (vpnState === 'idle') return;
  // Any transition out of idle while strip is open = user re-engaged.
  // Silent-good for COUNTDOWN, submit-with-current-tags for CHIPS.
  dismissOnReconnect();
}, [vpnState, pendingFeedback]);
```

`dismissOnReconnect` checks `state`: COUNTDOWN → `submitRating('good', ...)`; CHIPS → `submitNegativeFeedback(...)`.

### Submit guard

`submittedRef` prevents double-fire. Any path that submits sets `submittedRef.current = true` before firing. The dismiss handler checks this before calling `clearPendingFeedback`.

## Rating semantics

Rating is a **complaint filter**, not a satisfaction survey. The backend stores `good` / `bad` only; the API payload schema is unchanged from the legacy Dialog. Both explicit 4-5★ taps and silent-default 5★ produce identical `{ rating: "good" }` rows.

The product framing: silence = OK. Users who care will tap 1-3★ and surface the signal; users who don't tap aren't unhappy enough to bother. Splitting "explicit good" from "implicit good" adds storage + analytics complexity for no downstream consumer.

If a future stakeholder asks for the breakdown, the strip's behavior is already documented here — add a column then, not preemptively.

## Test plan

Vitest cases (rewrite of `DisconnectFeedbackDialog.test.tsx`):

1. Strip is hidden when `pendingFeedback=false`.
2. Strip appears when `pendingFeedback=true`.
3. Tapping 5★ fires `submitRating('good', ...)` and dismisses.
4. Tapping 3★ fires `submitRating('bad', ...)` and dismisses.
5. Tapping 1★ enters CHIPS state, hides progress, shows chips + submit button.
6. In CHIPS, tapping 4★ fires `submitRating('good')` with no tags and dismisses.
7. In CHIPS, tapping submit fires `submitNegativeFeedback` with selected tags.
8. Countdown elapsing fires `submitRating('good', ...)` and dismisses (silent default).
9. Visibility hidden pauses countdown; visibility visible resumes (use `vi.useFakeTimers`).
10. VPN state transition `idle → connecting` while in COUNTDOWN fires good (silent default).
11. VPN state transition `idle → connecting` while in CHIPS fires `submitNegativeFeedback` with current tags.
12. New `pendingFeedback` cycle (false → true) with new `lastConnectionInfo` resets stars/tags/state.
13. Submit guard: double-tap on star fires submit exactly once.

E2E coverage out of scope; existing E2E suite doesn't cover the dialog.

## Rollout

Local-only single-commit change (no PR, no remote push). No feature flag — the strip subsumes the dialog and the behavior change is user-positive. Locale strings reused, no translator round-trip.

No backend schema change required. The `/api/user/connection-rating` payload stays at the legacy `good`/`bad` shape so the API is wire-compatible with both the old Dialog clients and the new strip clients during a staged rollout.

## Open questions

None. The two decisions surfaced during brainstorming:
- 1-2★ negative path: **expand inline chips, no countdown in CHIPS state**
- × close button: **omit**

are baked into the spec above.
