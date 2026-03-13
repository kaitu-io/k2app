# Onboarding Guide MUI Rewrite

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Replace react-joyride onboarding guide with pure MUI components (SVG overlay + Popper) to fix CSS zoom coordinate mismatch on Windows, and redesign as interactive tutorial where users actually perform each action.

**Architecture:** `position:fixed` SVG overlay with `evenodd` cutout creates spotlight hole. RAF-based `useTargetRect` hook tracks target DOM element every frame via `getBoundingClientRect()`. MUI Popper anchors tooltip to a **virtual element** (object with dynamic `getBoundingClientRect()`) — no real DOM anchor needed, no timing issues. All positioning in viewport coordinate space — immune to CSS body zoom. Users click the actual target element to advance (no "Next" button).

**Tech Stack:** MUI 5 (Popper, Paper), SVG (fixed overlay), Zustand, React, i18next

---

## Context

react-joyride reads `getBoundingClientRect()` (viewport coords) then writes CSS position inside a zoomed body (layout coords), causing the spotlight to appear ~20% offset from the target on Windows (345px window, `body { zoom: 0.8023 }`).

**Key insight:** `position:fixed` elements are NOT affected by CSS body zoom. `getBoundingClientRect()` returns viewport coordinates, which map directly to `position:fixed` coordinate space. By keeping the overlay (SVG) and tooltip anchor (div) both `position:fixed`, zoom cancels out automatically — no compensation math needed.

The guide is also redesigned from passive "read tooltip, click Next" to active "do the action" interactive tutorial. The system is fully decoupled from business logic — it only knows CSS selectors (`data-tour` attributes) and listens for click events on target DOM elements.

## New Guide Flow (6 steps)

| Step | Phase | Target selector | User action | Effect | Route context |
|------|-------|----------------|-------------|--------|---------------|
| 1/6 | 1 | `[data-tour="collapse-toggle"]` | Click | Panel collapses | `/` |
| 2/6 | 2 | `[data-tour="collapse-toggle"]` | Click | Panel expands | `/` |
| 3/6 | 3 | `[data-tour="feedback-button"]` | Click | Navigate to `/submit-ticket` | `/` |
| 4/6 | 4 | `[data-tour="nav-invite"]` | Click | Navigate to `/invite` | any (bottom nav visible everywhere) |
| 5/6 | 5 | `[data-tour="invite-share"]` | Click | Copy/share | `/invite` |
| 6/6 | 6 | `[data-tour="nav-purchase"]` (iOS: `[data-tour="nav-dashboard"]`) | Click | Navigate to `/purchase` (iOS: `/`) | any |

**Conditional logic:**
- Phases 4-5: only when `isFeatureEnabled('invite')`
- Phase 6: all platforms (iOS targets dashboard tab instead of purchase tab)

## Files to Modify

| File | Action | Purpose |
|------|--------|---------|
| `webapp/src/stores/onboarding.store.ts` | **MODIFY** | Add phase 2, move PHASE_CONFIG here, iOS override, remove route gating |
| `webapp/src/components/onboarding/useTargetRect.ts` | **CREATE** | RAF-based hook to track target DOM element rect |
| `webapp/src/components/onboarding/SpotlightOverlay.tsx` | **CREATE** | SVG fixed overlay with evenodd cutout |
| `webapp/src/components/onboarding/OnboardingTooltip.tsx` | **CREATE** | MUI Popper tooltip with virtual fixed anchor |
| `webapp/src/components/OnboardingGuide.tsx` | **REWRITE** | Orchestrator: find target, wire click, compose overlay + tooltip |
| `webapp/src/components/BottomNavigation.tsx` | **MODIFY** | Add `dataTour: "nav-dashboard"` to dashboard nav item |
| `webapp/src/i18n/locales/*/onboarding.json` | **MODIFY** | Add phase2, update text for interactive flow (7 locales) |
| `webapp/package.json` | **MODIFY** | Remove `react-joyride` dependency |

**Files NOT modified** (targets keep existing `data-tour` attributes):
- `CollapsibleConnectionSection.tsx` — `data-tour="collapse-toggle"` (line 180)
- `FeedbackButton.tsx` — `data-tour="feedback-button"` (line 54)
- `BottomNavigation.tsx` — `data-tour="nav-invite"`, `data-tour="nav-purchase"` (line 142)
- `InviteHub.tsx` — `data-tour="invite-share"` (line 622)

---

## Task 1: Update onboarding store

**Files:**
- Modify: `webapp/src/stores/onboarding.store.ts`

### Changes:

1. Add phase 2 to `Phase` type: `type Phase = 1 | 2 | 3 | 4 | 5 | 6;`

2. Replace `PHASE_ROUTE` with `PHASE_CONFIG` (move config from OnboardingGuide into store):

```typescript
interface PhaseConfig {
  target: string;
  placement: 'top' | 'bottom' | 'left' | 'right';
  /** Tooltip i18n key suffix: `onboarding.phase${key}.title/content` */
  i18nKey: string;
}

const PHASE_CONFIG: Record<Phase, PhaseConfig> = {
  1: { target: '[data-tour="collapse-toggle"]', placement: 'bottom', i18nKey: 'phase1' },
  2: { target: '[data-tour="collapse-toggle"]', placement: 'bottom', i18nKey: 'phase2' },
  3: { target: '[data-tour="feedback-button"]', placement: 'left', i18nKey: 'phase3' },
  4: { target: '[data-tour="nav-invite"]', placement: 'top', i18nKey: 'phase4' },
  5: { target: '[data-tour="invite-share"]', placement: 'bottom', i18nKey: 'phase5' },
  6: { target: '[data-tour="nav-purchase"]', placement: 'top', i18nKey: 'phase6' },
};
```

3. Update `buildPhaseList()`:

```typescript
function buildPhaseList(): Phase[] {
  const isIOS = window._platform?.os === 'ios';
  const hasInvite = isFeatureEnabled('invite');

  const phases: Phase[] = [1, 2, 3];

  if (hasInvite) {
    phases.push(4, 5);
  }

  // All platforms get final step (iOS targets dashboard, others target purchase)
  phases.push(6);

  return phases;
}
```

4. iOS phase 6 dynamic config:

```typescript
function getPhaseConfig(phase: Phase, isIOS: boolean): PhaseConfig {
  if (phase === 6 && isIOS) {
    return { target: '[data-tour="nav-dashboard"]', placement: 'top', i18nKey: 'phase6_ios' };
  }
  return PHASE_CONFIG[phase];
}
```

5. Add `isIOS` to store state (set during `start()`). Export `getPhaseConfig()` getter.

6. Remove `PHASE_ROUTE` and `getExpectedRoute()` — no longer gating by route.

---

## Task 2: Create onboarding sub-components

**Files:**
- Create: `webapp/src/components/onboarding/useTargetRect.ts`
- Create: `webapp/src/components/onboarding/SpotlightOverlay.tsx`
- Create: `webapp/src/components/onboarding/OnboardingTooltip.tsx`

### 2a: useTargetRect hook

RAF-based hook that tracks a DOM element's viewport rect every frame.

```typescript
import { useState, useEffect, useRef, useCallback } from 'react';

export interface TargetRect {
  top: number;
  left: number;
  width: number;
  height: number;
}

/**
 * Tracks a DOM element's viewport-coordinate bounding rect every animation frame.
 * Returns null when selector matches nothing.
 */
export function useTargetRect(selector: string | null): {
  rect: TargetRect | null;
  element: HTMLElement | null;
} {
  const [rect, setRect] = useState<TargetRect | null>(null);
  const [element, setElement] = useState<HTMLElement | null>(null);
  const rafRef = useRef<number>(0);

  useEffect(() => {
    if (!selector) {
      setRect(null);
      setElement(null);
      return;
    }

    let prevTop = -1, prevLeft = -1, prevWidth = -1, prevHeight = -1;

    const tick = () => {
      const el = document.querySelector<HTMLElement>(selector);
      if (!el) {
        if (rect !== null) setRect(null);
        if (element !== null) setElement(null);
        rafRef.current = requestAnimationFrame(tick);
        return;
      }

      if (el !== element) setElement(el);

      const r = el.getBoundingClientRect();
      // Only update state when values actually change (avoid re-renders)
      if (r.top !== prevTop || r.left !== prevLeft || r.width !== prevWidth || r.height !== prevHeight) {
        prevTop = r.top;
        prevLeft = r.left;
        prevWidth = r.width;
        prevHeight = r.height;
        setRect({ top: r.top, left: r.left, width: r.width, height: r.height });
      }

      rafRef.current = requestAnimationFrame(tick);
    };

    rafRef.current = requestAnimationFrame(tick);
    return () => cancelAnimationFrame(rafRef.current);
  }, [selector]);

  return { rect, element };
}
```

Key points:
- `getBoundingClientRect()` returns viewport coords — directly usable by `position:fixed` elements
- Previous-value comparison avoids unnecessary React re-renders
- Automatically handles scroll, resize, CSS animations, lazy-loaded elements
- Returns both `rect` (for positioning) and `element` (for click event binding)

### 2b: SpotlightOverlay

SVG fixed overlay with evenodd cutout to create spotlight hole.

```tsx
import React from 'react';
import type { TargetRect } from './useTargetRect';

interface SpotlightOverlayProps {
  rect: TargetRect;
  padding?: number;
  borderRadius?: number;
}

const SpotlightOverlay: React.FC<SpotlightOverlayProps> = ({
  rect,
  padding = 8,
  borderRadius = 12,
}) => {
  // Spotlight hole dimensions (padded around target)
  const x = rect.left - padding;
  const y = rect.top - padding;
  const w = rect.width + padding * 2;
  const h = rect.height + padding * 2;
  const r = Math.min(borderRadius, w / 2, h / 2);

  return (
    <svg
      style={{
        position: 'fixed',
        inset: 0,
        width: '100vw',
        height: '100vh',
        zIndex: 1300, // theme.zIndex.modal + 10
        pointerEvents: 'none',
      }}
    >
      <path
        fillRule="evenodd"
        fill="rgba(0,0,0,0.65)"
        pointerEvents="auto"
        d={`
          M0,0 H${window.innerWidth} V${window.innerHeight} H0 Z
          M${x + r},${y}
          H${x + w - r} Q${x + w},${y} ${x + w},${y + r}
          V${y + h - r} Q${x + w},${y + h} ${x + w - r},${y + h}
          H${x + r} Q${x},${y + h} ${x},${y + h - r}
          V${y + r} Q${x},${y} ${x + r},${y}
          Z
        `}
      />
    </svg>
  );
};

export default SpotlightOverlay;
```

Key points:
- `position:fixed` + `inset:0` — fills entire viewport, NOT affected by body zoom
- SVG `evenodd` fill rule: outer rect (full viewport) drawn clockwise + inner rounded rect creates transparent hole
- `pointerEvents: 'none'` on SVG, `'auto'` on path — overlay blocks clicks on non-target areas, but the cutout hole allows clicks through to the target element
- Viewport coordinates from `useTargetRect` map directly — no zoom compensation needed

### 2c: OnboardingTooltip

MUI Popper tooltip with **virtual element** anchor (no real DOM element needed).

**Why virtual element:** MUI Popper wraps `@popperjs/core` v2. Popper does NOT auto-detect anchor style changes — it relies on React re-renders to trigger its internal `forceUpdate()`. Using a real anchor div updated via `useEffect` creates a timing race (our effect vs MUI's effect execution order is undefined). A virtual element with a dynamic `getBoundingClientRect()` solves this: when Popper calls `forceUpdate()` during re-render, it reads the latest rect values directly from the function — no timing dependency.

```tsx
import React, { useMemo, useRef } from 'react';
import { Popper, Paper, Typography, Box } from '@mui/material';
import { useTranslation } from 'react-i18next';
import type { TargetRect } from './useTargetRect';

interface OnboardingTooltipProps {
  rect: TargetRect;
  placement: 'top' | 'bottom' | 'left' | 'right';
  i18nKey: string;
  currentIndex: number;
  totalSteps: number;
  onSkip: () => void;
}

const OnboardingTooltip: React.FC<OnboardingTooltipProps> = ({
  rect,
  placement,
  i18nKey,
  currentIndex,
  totalSteps,
  onSkip,
}) => {
  const { t } = useTranslation('onboarding');

  // Store latest rect in ref so virtual element always reads current values
  const rectRef = useRef(rect);
  rectRef.current = rect;

  // Virtual element — Popper calls getBoundingClientRect() on each update
  // useMemo ensures stable reference (Popper won't recreate instance on every render)
  const virtualAnchor = useMemo(() => ({
    getBoundingClientRect: () => ({
      top: rectRef.current.top,
      left: rectRef.current.left,
      bottom: rectRef.current.top + rectRef.current.height,
      right: rectRef.current.left + rectRef.current.width,
      width: rectRef.current.width,
      height: rectRef.current.height,
      x: rectRef.current.left,
      y: rectRef.current.top,
      toJSON: () => {},
    }),
  }), []);

  return (
    <Popper
      open
      anchorEl={virtualAnchor}
      placement={placement}
      modifiers={[{ name: 'offset', options: { offset: [0, 12] } }]}
      style={{ zIndex: 1320 }} // theme.zIndex.modal + 30
    >
      <Paper sx={{ p: '14px 18px', maxWidth: 280, borderRadius: 2 }}>
        <Typography variant="subtitle2" fontWeight={600} mb={0.5}>
          {t(`onboarding.${i18nKey}.title`)}
        </Typography>
        <Typography variant="body2" color="text.secondary" lineHeight={1.6} whiteSpace="pre-line">
          {t(`onboarding.${i18nKey}.content`)}
        </Typography>
        <Box mt={1.5} display="flex" alignItems="center" justifyContent="space-between">
          <Typography
            component="button"
            variant="caption"
            onClick={onSkip}
            sx={{ color: 'text.disabled', cursor: 'pointer', background: 'none', border: 'none', p: 0 }}
          >
            {t('onboarding.skip')}
          </Typography>
          <Typography variant="caption" color="text.disabled" fontSize="0.7rem">
            {currentIndex + 1}/{totalSteps}
          </Typography>
        </Box>
      </Paper>
    </Popper>
  );
};

export default OnboardingTooltip;
```

Key points:
- **Virtual element**: object with `getBoundingClientRect()` method — Popper calls this on each `forceUpdate()`, always gets latest values from `rectRef`
- **`useMemo` stable reference**: prevents Popper from destroying/recreating instance on re-render (only need `forceUpdate`, not full re-init)
- **`rectRef` pattern**: rect prop → ref → virtual element reads from ref. When `useTargetRect` updates rect state → parent re-renders → Popper re-renders → MUI's unguarded `useEffect` calls `forceUpdate()` → Popper reads fresh values from `rectRef.current`
- **No real DOM anchor**: eliminates the timing race between useEffect hooks
- **No "Next" button**: user advances by clicking the actual target element
- **Update chain**: RAF tick → `setRect()` in useTargetRect → React re-render → MUI `forceUpdate()` → virtual `getBoundingClientRect()` reads `rectRef.current` → Popper repositions. 100% deterministic, no race conditions.

---

## Task 3: Rewrite OnboardingGuide orchestrator

**Files:**
- Rewrite: `webapp/src/components/OnboardingGuide.tsx`

### Architecture:

```
OnboardingGuide (rendered in Layout)
├── SpotlightOverlay — position:fixed SVG with evenodd cutout hole
├── [Target element] — real DOM, clicks pass through SVG cutout hole
│   └── Glow animation via CSS class injection (not inline styles)
└── OnboardingTooltip — MUI Popper with position:fixed virtual anchor
    └── Paper — skip link + step indicator + instruction text
```

### Complete implementation:

```tsx
import React, { useEffect, useRef, useCallback, useState } from 'react';
import { useOnboardingStore } from '../stores/onboarding.store';
import { useTargetRect } from './onboarding/useTargetRect';
import SpotlightOverlay from './onboarding/SpotlightOverlay';
import OnboardingTooltip from './onboarding/OnboardingTooltip';

// Inject global pulse keyframes
const STYLE_ID = 'onboarding-pulse-style';
const PULSE_CSS = `
@keyframes onboarding-pulse {
  0%, 100% { box-shadow: 0 0 0 3px rgba(0,212,255,0.8), 0 0 20px rgba(0,212,255,0.4); }
  50% { box-shadow: 0 0 0 5px rgba(0,212,255,0.6), 0 0 30px rgba(0,212,255,0.3); }
}
.onboarding-target-glow {
  animation: onboarding-pulse 2s ease-in-out infinite;
  border-radius: 12px;
  position: relative;
  z-index: 1310;
}
.onboarding-target-glow-fixed {
  animation: onboarding-pulse 2s ease-in-out infinite;
  border-radius: 12px;
  z-index: 1310;
}
`;

const OnboardingGuide: React.FC = () => {
  const { active, phase, phases, nextPhase, complete, getPhaseConfig, isIOS } = useOnboardingStore();
  const config = active && phase ? getPhaseConfig(phase, isIOS) : null;
  const { rect, element } = useTargetRect(config?.target ?? null);
  const prevElementRef = useRef<HTMLElement | null>(null);
  const [showTooltip, setShowTooltip] = useState(true);

  // Inject/remove pulse keyframes
  useEffect(() => {
    if (!active) return;
    if (!document.getElementById(STYLE_ID)) {
      const style = document.createElement('style');
      style.id = STYLE_ID;
      style.textContent = PULSE_CSS;
      document.head.appendChild(style);
    }
    return () => {
      document.getElementById(STYLE_ID)?.remove();
    };
  }, [active]);

  // Apply/remove glow class on target element
  useEffect(() => {
    // Remove glow from previous target
    if (prevElementRef.current && prevElementRef.current !== element) {
      prevElementRef.current.classList.remove('onboarding-target-glow', 'onboarding-target-glow-fixed');
    }

    if (element) {
      // FeedbackButton uses position:fixed via Portal — use non-positioning glow class
      const isFixed = window.getComputedStyle(element).position === 'fixed';
      element.classList.add(isFixed ? 'onboarding-target-glow-fixed' : 'onboarding-target-glow');
      prevElementRef.current = element;
    }

    return () => {
      if (element) {
        element.classList.remove('onboarding-target-glow', 'onboarding-target-glow-fixed');
      }
    };
  }, [element]);

  // Click detection on target — advance guide
  useEffect(() => {
    if (!element || !active) return;

    const handler = () => {
      // Let the native click handler execute first (navigation, toggle, etc.)
      // Then advance the guide on next tick
      setTimeout(() => {
        // Phase 1→2: wait for collapse animation (300ms) before showing tooltip
        if (phase === 1) {
          setShowTooltip(false);
          setTimeout(() => {
            nextPhase();
            setShowTooltip(true);
          }, 400);
        } else {
          nextPhase();
        }
      }, 0);
    };

    element.addEventListener('click', handler);
    return () => element.removeEventListener('click', handler);
  }, [element, active, phase, nextPhase]);

  if (!active || !config || !rect) return null;

  const currentIndex = phases.indexOf(phase!);
  const totalSteps = phases.length;

  return (
    <>
      <SpotlightOverlay rect={rect} />
      {showTooltip && (
        <OnboardingTooltip
          rect={rect}
          placement={config.placement}
          i18nKey={config.i18nKey}
          currentIndex={currentIndex}
          totalSteps={totalSteps}
          onSkip={complete}
        />
      )}
    </>
  );
};

export default OnboardingGuide;
```

Key points:
- **CSS class injection** instead of inline style injection — simpler cleanup (just `classList.remove`)
- **Two glow classes**: `onboarding-target-glow` (adds `position:relative` + `z-index`) for elements in normal flow; `onboarding-target-glow-fixed` (z-index only, no position change) for FeedbackButton which is already `position:fixed`
- **Phase 1→2 transition**: hides tooltip during 400ms collapse animation, then shows phase 2
- **Target polling built-in**: `useTargetRect` returns null until `querySelector` finds the element, so lazy-loaded elements (phase 5 invite-share) are handled automatically
- **No route gating**: guide follows user wherever they navigate, just waits for target to appear

---

## Task 4: Add dashboard tour marker to BottomNavigation

**Files:**
- Modify: `webapp/src/components/BottomNavigation.tsx`

Add `dataTour: "nav-dashboard"` to the dashboard nav item (line 76):

```typescript
{
  label: t("nav:navigation.dashboard"),
  icon: <DashboardIcon />,
  path: "/",
  feature: null,
  dataTour: "nav-dashboard",  // ← add this
},
```

---

## Task 5: Update i18n files (7 locales)

**Files:**
- Modify: `webapp/src/i18n/locales/{zh-CN,en-US,ja,zh-TW,zh-HK,en-AU,en-GB}/onboarding.json`

### zh-CN (primary):

```json
{
  "onboarding": {
    "phase1": {
      "title": "折叠面板",
      "content": "点击箭头收起连接面板\n为节点列表腾出空间"
    },
    "phase2": {
      "title": "展开面板",
      "content": "再次点击箭头展开面板\n恢复连接按钮"
    },
    "phase3": {
      "title": "问题反馈",
      "content": "遇到问题？点击这个按钮\n随时提交反馈给我们"
    },
    "phase4": {
      "title": "邀请好友",
      "content": "点击这里进入邀请页\n邀请好友双方都能获得免费时长"
    },
    "phase5": {
      "title": "分享给好友",
      "content": "点击分享按钮\n将邀请链接发送给好友"
    },
    "phase6": {
      "title": "选择套餐",
      "content": "点击这里查看套餐方案\n选择适合你的计划"
    },
    "phase6_ios": {
      "title": "回到仪表板",
      "content": "点击这里回到仪表板\n选择节点开始连接"
    },
    "skip": "跳过引导"
  }
}
```

Remove unused keys: `next`, `back`, `close`, `last` (no longer needed without Next button).

### en-US:

```json
{
  "onboarding": {
    "phase1": {
      "title": "Collapse Panel",
      "content": "Tap the arrow to collapse the connection panel\nMake room for the node list"
    },
    "phase2": {
      "title": "Expand Panel",
      "content": "Tap the arrow again to expand the panel\nRestore the connection buttons"
    },
    "phase3": {
      "title": "Send Feedback",
      "content": "Having issues? Tap this button\nto submit feedback anytime"
    },
    "phase4": {
      "title": "Invite Friends",
      "content": "Tap here to open the invite page\nBoth you and your friend get free time"
    },
    "phase5": {
      "title": "Share with Friends",
      "content": "Tap the share button\nto send the invite link to friends"
    },
    "phase6": {
      "title": "Choose a Plan",
      "content": "Tap here to view plans\nPick the one that suits you"
    },
    "phase6_ios": {
      "title": "Back to Dashboard",
      "content": "Tap here to return to the dashboard\nSelect a node to connect"
    },
    "skip": "Skip Guide"
  }
}
```

### ja, zh-TW, zh-HK, en-AU, en-GB:

Translate the same structure. Keep `phase6_ios` for iOS dashboard variant. en-AU and en-GB can use en-US translations (same English).

---

## Task 6: Remove react-joyride dependency

**Files:**
- Modify: `webapp/package.json`

```bash
cd webapp && yarn remove react-joyride
```

Also remove any `@types/react-joyride` if present.

---

## Task 7: Store unit tests

**Files:**
- Create: `webapp/src/stores/__tests__/onboarding.store.test.ts`

Test the store's phase state machine and platform logic. Mock `window._platform` and `isFeatureEnabled`.

```typescript
import { describe, it, expect, vi, beforeEach } from 'vitest';
import { useOnboardingStore } from '../onboarding.store';

// Mock platform and feature flags before import
vi.mock('../../config/apps', () => ({
  isFeatureEnabled: vi.fn(() => true),
}));

import { isFeatureEnabled } from '../../config/apps';
const mockFeature = vi.mocked(isFeatureEnabled);
```

### Test cases:

**Phase progression (full flow):**
```
start() → phase=1, phases=[1,2,3,4,5,6]
nextPhase() → phase=2
nextPhase() → phase=3
nextPhase() → phase=4
nextPhase() → phase=5
nextPhase() → phase=6
nextPhase() → active=false (completed)
```

**Phase progression (no invite):**
```
isFeatureEnabled('invite') = false
start() → phases=[1,2,3,6]
nextPhase() x3 → phase=6
nextPhase() → active=false
```

**iOS phase 6 config:**
```
window._platform.os = 'ios'
getPhaseConfig(6) → { target: '[data-tour="nav-dashboard"]', i18nKey: 'phase6_ios' }
```

**Non-iOS phase 6 config:**
```
window._platform.os = 'windows'
getPhaseConfig(6) → { target: '[data-tour="nav-purchase"]', i18nKey: 'phase6' }
```

**complete() persists:**
```
complete() → active=false, storage.set called with 'onboarding_completed', true
```

**tryStart() skips when completed:**
```
storage.get returns true → start() NOT called, active remains false
```

**tryStart() starts when not completed:**
```
storage.get returns false → active=true, phase=1
```

Run: `cd webapp && npx vitest run src/stores/__tests__/onboarding.store.test.ts`

---

## Verification

### Dev testing (macOS):
```bash
make dev-standalone   # or make dev-macos
```
1. Clear onboarding completion: in DevTools console run `window._platform.storage.set('onboarding_completed', false)`
2. Reload → login → verify guide starts
3. Walk through all 6 steps by clicking each target
4. Verify tooltip position is correct (anchored to target, not offset)
5. Verify dark overlay covers everything except target (spotlight cutout)
6. Verify target has cyan glow pulse animation
7. Verify "跳过引导" works at any step
8. Verify guide doesn't restart after completion + reload

### Windows zoom testing:
```bash
make dev-windows   # or build + deploy to Windows
```
1. Set Windows display scaling to 150% or 200%
2. Repeat the 6-step walkthrough
3. **Key check:** tooltip, spotlight cutout, and glow must be correctly positioned on the target, not offset

### Edge cases to test:
- Step 1→2: collapse animation completes before step 2 tooltip appears (400ms delay)
- Step 3→4: after navigating to /submit-ticket, invite tab in bottom nav is highlighted
- Step 5: on /invite page, share button may load lazily — tooltip should wait (useTargetRect polls automatically)
- iOS: step 6 highlights dashboard tab, not purchase tab
- No invite feature: steps 4-5 skipped, phase 3→6
- Window resize during guide: SVG overlay redraws, Popper repositions (both use RAF-tracked viewport coords)
- FeedbackButton (position:fixed via Portal): glow applied without changing position property
