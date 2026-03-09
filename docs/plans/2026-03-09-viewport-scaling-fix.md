# Viewport Scaling Fix — transform → zoom

## Root Cause

`setupViewportScaling()` in `main.tsx` applies `document.body.style.transform = scale(0.914)` on screens narrower than 430px (e.g. iPhone 393px). This breaks `position: fixed` for ALL descendants of body.

**CSS spec**: A `transform` on any element creates a new containing block. `position: fixed` children become relative to the transformed element instead of the viewport.

**Affected components**:
- react-joyride (OnboardingGuide) — overlay + tooltip use `position: fixed` + `getBoundingClientRect()` → spotlight misaligned
- FeedbackButton — `position: fixed`, but useDraggable already compensates via `getBodyScale()`
- ServiceAlert — `position: fixed` at top
- UpdateNotification — `position: fixed` at top
- MUI Portals (Dialogs, Popovers) — rendered as direct body children

## Why Viewport Scaling Exists

1. **Desktop (Tauri)**: UI designed at 430px width. Windows 1080p laptops with narrow windows need scaling. Introduced in commit `1aef5a3`.
2. **Mobile (Capacitor)**: Extended to iOS in commit `038f5fd` because FeedbackButton was positioned offscreen on 393px-wide iPhones.
3. **Applied to body (not #root)**: So MUI Portals (Dialog, Popover, Menu) also scale — they render as direct children of `<body>`.

## Fix: Replace `transform: scale()` with CSS `zoom`

CSS `zoom` achieves the same visual scaling but does NOT create a new containing block:

| Behavior | `transform: scale()` | `zoom` |
|----------|---------------------|--------|
| Visual scaling | Yes | Yes |
| Creates containing block | **Yes** (breaks fixed) | **No** |
| `position: fixed` works | Relative to body | Relative to viewport |
| `getBoundingClientRect()` | Transformed coordinates | Viewport coordinates |
| MUI Portals scaled | Yes (body children) | Yes (body children) |
| Pointer event coords | Need scale compensation | Direct viewport coords |

**Browser support**: Chrome/Safari/Edge (always), Firefox 126+ (June 2024). Our minimum targets are well above this.

### Changes

#### 1. `webapp/src/main.tsx` — Replace transform with zoom

```typescript
// Before:
document.body.style.width = `${DESIGN_WIDTH}px`;
document.body.style.height = `${windowHeight / scale}px`;
document.body.style.transform = `scale(${scale})`;
document.body.style.transformOrigin = "top left";

// After:
document.body.style.zoom = `${scale}`;

// Reset:
document.body.style.zoom = "";
```

With `zoom`, the browser automatically handles width/height layout. The body renders at its natural width (constrained by viewport), and `zoom` scales it visually. We no longer need to set `body.style.width` or `body.style.height` explicitly.

However, we DO want the body to lay out at 430px (design width) and then zoom down. So:

```typescript
if (scale < 1) {
  document.body.style.width = `${DESIGN_WIDTH}px`;
  document.body.style.height = `${windowHeight / scale}px`;
  document.body.style.zoom = `${scale}`;
} else {
  document.body.style.width = "";
  document.body.style.height = "";
  document.body.style.zoom = "";
}
```

Remove `transformOrigin` lines entirely (zoom scales from top-left by default).

#### 2. `webapp/src/hooks/useDraggable.ts` — Update getBodyScale()

```typescript
// Before:
function getBodyScale(): number {
  const t = document.body.style.transform;
  if (!t) return 1;
  const m = t.match(/scale\(([\d.]+)\)/);
  return m ? parseFloat(m[1]) : 1;
}

// After:
function getBodyScale(): number {
  const z = document.body.style.zoom;
  if (!z || z === '1') return 1;
  return parseFloat(z) || 1;
}
```

**Important**: With `zoom`, `getBoundingClientRect()` returns viewport coordinates directly. The `clientX / scale` compensation in pointer handlers may no longer be needed. Need to test: if zoom-space coordinates match clientX/clientY, remove the scale division. If not, keep it.

Testing approach: Deploy, drag feedback button on iPhone, verify position tracks finger accurately.

#### 3. `webapp/src/components/OnboardingGuide.tsx` — No changes needed

Once body uses `zoom` instead of `transform`, `position: fixed` works correctly. react-joyride's `getBoundingClientRect()` calculations will align with the viewport. The current code should work as-is.

#### 4. Verify other fixed-position components

- **ServiceAlert**: `position: fixed`, `top: 0` — should work correctly with zoom
- **UpdateNotification**: `position: fixed`, `top: 0` — should work correctly with zoom
- **FeedbackButton**: Uses useDraggable with scale compensation — update getBodyScale() (step 2)

### Side Effects Analysis

**Removing body transform and using zoom**:
- No visual layout change — same 430px→393px scaling, same visual result
- `position: fixed` elements immediately work correctly (react-joyride, ServiceAlert, etc.)
- MUI Portals (Dialogs, Menus, Popovers) still scale correctly (they're body children)
- No content reflow or jump — zoom is applied at page load, same as transform was

**Potential risk**:
- `zoom` affects how `getBoundingClientRect()` reports coordinates. The useDraggable hook divides `clientX` by scale — this may need adjustment if zoom already aligns the coordinate spaces. Must verify on device.
- CSS `zoom` is relatively new in Firefox (126+). If any user runs Firefox < 126 on desktop, they won't get scaling. This is acceptable — it only affects narrow windows, and the app is functional at any width.

### Testing

1. `cd webapp && npx tsc --noEmit` — type check
2. `cd webapp && npx vitest run` — unit tests
3. iPhone physical device:
   - Onboarding wizard: all 5 phases spotlight aligned
   - FeedbackButton: drag works correctly, position tracks finger
   - ServiceAlert: displayed at top of viewport
   - MUI Dialog (e.g. LoginDialog): centered on screen
4. Desktop (Tauri) narrow window (< 430px):
   - UI scales down proportionally
   - Dialogs centered correctly
5. Desktop full-width (>= 430px):
   - No zoom applied, everything normal
