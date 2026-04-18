# Webapp Dashboard Score Display — Restore Bar, Deauto-Probe

**Date**: 2026-04-18
**Status**: Design
**Author**: brainstorm session with user

## Problem

On the Dashboard tunnel list each row currently shows two score indicators:

- `<ProbeChip>` — live RTT/loss from `window._k2.run('probe', ...)` (k2-side score)
- `<RecommendDot>` — emoji 🟢🟡🔴 from Cloud API `recommendScore` (API-side score)

Two side-by-side scoring signals are noisy and the auto-probe (mount + 5-min
interval) causes background activity that is no longer wanted at this point
in the UI lifecycle. We also want to return to the previous vertical-bar
visual that existed before commit `9e12d0b`, but drive it with the newer
`recommendScore` [0,1] instead of the retired `budgetScore` [-1,+1].

## Goals

1. Dashboard shows exactly one score indicator per tunnel — a vertical bar
   driven by `recommendScore`.
2. No automatic `probe` action invocation from any webapp code path.
3. Keep every piece of probe infrastructure intact so a future UI feature
   can wire it in without re-implementing anything.

## Non-Goals

- Redesigning the `probe` action itself. It is already an independent
  action (`window._k2.run('probe', {urls, timeoutMs})`) and stays as is.
- Adding a new user-facing probe trigger (button, settings toggle, etc.).
  That belongs to a future UI design once we decide where the probe fits.
- Changing daemon-side probe behavior (`handleProbe`, `probe.Registry`,
  `probe.Service`, `Subscription.Pick`). They keep running — they do not
  depend on the webapp calling `probe`.

## Constraints

- `recommendScore` semantics and thresholds stay identical to the
  `RecommendDot` they replace: ≥0.6 green, ≥0.3 yellow, <0.3 red,
  `undefined` → not rendered.
- Bar height direction: **higher score = taller bar** (recommendation, not
  load). This inverts the old `VerticalLoadBar`'s "higher = worse" mapping
  because the underlying signal flipped from `budgetScore` to
  `recommendScore`.
- All infrastructure retained (probe-service, probe.store, ProbeChip,
  ProbeResult/ProbeResponse types, tunnel-sort util, i18n
  `dashboard.probe.*` keys, bridge `probe` action, daemon handler).

## Design

### Component replacement

Delete `webapp/src/components/RecommendDot.tsx` and add
`webapp/src/components/RecommendBar.tsx`:

```tsx
interface RecommendBarProps {
  score: number | undefined;
}

export function RecommendBar({ score }: RecommendBarProps) {
  if (score === undefined) return null;

  const clamped = Math.max(0, Math.min(1, score));
  const heightPct = Math.round(clamped * 100);

  const color =
    clamped >= 0.6 ? 'success.main'
    : clamped >= 0.3 ? 'warning.main'
    : 'error.main';

  return (
    <Box sx={{
      width: 4,
      height: 24,
      bgcolor: 'action.hover',
      borderRadius: 1,
      display: 'flex',
      alignItems: 'flex-end',
    }}>
      <Box sx={{
        width: '100%',
        height: `${heightPct}%`,
        bgcolor: color,
        borderRadius: 1,
        transition: 'height 0.3s ease',
      }} />
    </Box>
  );
}
```

Dimensions mirror the historical `VerticalLoadBar` (4×24 px). The outer
container uses `display: flex; alignItems: flex-end` so the inner fill
grows from the bottom — a taller colored segment always means a better
pick, consistent with the "higher score = better" semantic.

### CloudTunnelList simplification

In `webapp/src/components/CloudTunnelList.tsx`:

1. Remove the imports: `runProbe` from `probe-service`, `useProbeStore`
   from `probe.store`, `ProbeChip`. Replace `RecommendDot` import with
   `RecommendBar`.
2. Remove the `probeQualityProvider` `useMemo` and the
   `sortTunnelsByRecommendation` call. Replace with a direct alphabetical
   sort:
   ```ts
   const sortedTunnels = useMemo(
     () => [...tunnels].sort((a, b) => a.node.country.localeCompare(b.node.country)),
     [tunnels]
   );
   ```
3. Remove the mount-effect that called `runProbe(tunnels)` and the
   5-minute `setInterval` retriggering it.
4. In the list-item JSX, drop the `<ProbeChip>` element and replace
   `<RecommendDot score={tunnel.recommendScore} />` with
   `<RecommendBar score={tunnel.recommendScore} />`. The surrounding
   `<Box sx={{ mr: 2, display: 'flex', alignItems: 'center', gap: 1 }}>`
   wrapper stays — it now contains only the bar.

No other changes to CloudTunnelList. The rest of the component (tunnel
fetch via `cloudApi.get('/api/tunnels/k2v4')`, Radio selection, flag
icon, etc.) is unaffected.

### What does NOT change

- `webapp/src/services/probe-service.ts` — `runProbe()` is preserved
  verbatim.
- `webapp/src/stores/probe.store.ts` — kept as-is.
- `webapp/src/components/ProbeChip.tsx` — kept as-is (no consumer after
  this change, but retained for future UI).
- `webapp/src/services/api-types.ts` — `ProbeResult`, `ProbeResponse`,
  `Tunnel.recommendScore`, `TunnelInstance.recommendScore` all unchanged.
- `webapp/src/utils/tunnel-sort.ts` — utility kept (unused after this
  change, available for future feature).
- `webapp/src/i18n/locales/*/dashboard.json` — `probe.*` keys kept.
- Bridge contract `window._k2.run('probe', ...)` — unchanged.
- Daemon `handleProbe` in `k2/daemon/daemon.go` — unchanged. Its internal
  caller `probe.Service` continues to drive `Subscription.Pick` scoring.

### Data flow after change

```
GET /api/tunnels/k2v4
  → Tunnel[] with recommendScore in [0,1]
  → CloudTunnelList state
  → sort alphabetically by country
  → render <RecommendBar score={recommendScore}>
```

No `probe` action fires from the webapp under any Dashboard interaction.

## Testing

### Unit tests

- New `webapp/src/components/__tests__/RecommendBar.test.tsx` with 7
  cases covering the thresholds and render behavior:
  - score=0.9 → 90% height, success color
  - score=0.6 → 60% height, success color (inclusive lower bound)
  - score=0.45 → 45% height, warning color
  - score=0.3 → 30% height, warning color (inclusive lower bound)
  - score=0.15 → 15% height, error color
  - score=0 → 0% height, error color
  - score=undefined → component returns null (nothing rendered)
- Modify `webapp/src/components/__tests__/CloudTunnelList.test.tsx`:
  - Remove assertions referencing `ProbeChip` or `runProbe`.
  - Add assertion: sortedTunnels order is alphabetical by `node.country`.
- Leave `webapp/src/services/__tests__/probe-service.test.ts`
  untouched — it covers `runProbe()` which remains exported.
- Delete `webapp/src/components/__tests__/RecommendDot.test.tsx`.

### Manual verification

1. `cd webapp && yarn dev` → open Dashboard. Each tunnel row displays
   flag + name + vertical bar; no chip, no emoji.
2. Open browser devtools Network tab. On page load, refresh, and 10
   minutes of idle observation: zero requests matching
   `POST /api/core` with `action:"probe"`.
3. Mock `recommendScore` values across the three bands (0.9 / 0.45 /
   0.15) by intercepting the `/api/tunnels/k2v4` response or manually
   editing state — confirm bar height and color transitions match the
   thresholds above.
4. Type check: `cd webapp && npx tsc --noEmit` clean.

## Out of Scope / Future Work

- How and where a future UI will surface the probe capability (e.g. a
  "test all servers" button, a diagnostic panel, an admin-only tool).
  That will be its own design cycle. When that design lands, it can
  directly consume the preserved `runProbe()` / `probe.store` /
  `ProbeChip`.

## Files Touched

**Modified**
- `webapp/src/components/CloudTunnelList.tsx`
- `webapp/src/components/__tests__/CloudTunnelList.test.tsx`

**Added**
- `webapp/src/components/RecommendBar.tsx`
- `webapp/src/components/__tests__/RecommendBar.test.tsx`

**Deleted**
- `webapp/src/components/RecommendDot.tsx`
- `webapp/src/components/__tests__/RecommendDot.test.tsx`

**Untouched but relevant (explicitly preserved)**
- `webapp/src/services/probe-service.ts`
- `webapp/src/services/__tests__/probe-service.test.ts`
- `webapp/src/stores/probe.store.ts`
- `webapp/src/components/ProbeChip.tsx`
- `webapp/src/services/api-types.ts` (ProbeResult, ProbeResponse, recommendScore)
- `webapp/src/utils/tunnel-sort.ts`
- `webapp/src/i18n/locales/*/dashboard.json` (`probe.*` keys)
- `k2/daemon/daemon.go` (`handleProbe`), `k2/daemon/api.go` (`probe` case)
