# Auto Tunnel Pick — Design

**Date**: 2026-04-27
**Scope**: webapp (React) — Tauri Desktop / Capacitor Mobile / standalone Web
**Out of scope**: Go core (`k2/`), daemon, native bridges, Center API

## Problem

The cloud tunnel list returned by `GET /api/tunnels/k2v4` is unsorted server-side; the webapp sorts it alphabetically by `node.country` for display (`CloudTunnelList.tsx:83-85`). Each tunnel carries a `recommendScore ∈ [0, 1]` (`api/api_tunnel.go:168`, computed by `ComputeRecommendScore`), but the score is currently used only by the visual `RecommendBar` — it does not influence selection.

Users on first launch (or after the `smart` mode was retired) get whichever country sorts first alphabetically auto-selected (`Dashboard.tsx:194-216` migration aid). This is arbitrary and unrelated to tunnel quality.

## Goal

Add a webapp-only "Auto" selection that picks one of the top-3 tunnels by `recommendScore` at random on each connect, surfacing it as the **default selection** for users who have not chosen a specific tunnel.

## Non-goals

- No changes to Go core, daemon, native bridges, or Center API.
- No reintroduction of `serverMode === 'smart'` or daemon-side `Subscription` / k2subs flow.
- No probe-quality weighting (left as future work — hooks already exist via `probe.store`).
- No persistence of the user's most recent concrete tunnel choice (preserves current behavior).

## Decisions (recorded from brainstorming)

| # | Decision | Rationale |
|---|---|---|
| 1 | Form factor: virtual "Auto" item rendered as the first row of `CloudTunnelList` (option B in brainstorming) | Matches user mental model "first position in the list"; minimal type/refactor surface. |
| 2 | Random timing: re-pick on every connect; stable within an active connected session (option C) | Lets bad picks self-heal on retry; doesn't shuffle IPs while connected. |
| 3 | Connected display: top card shows `⚡ Auto · current: JP - Tokyo` (option C); list item shows ordinary selected styling (option a) | User retains visibility into actual connection without losing mode abstraction. |
| 4 | Tie-breaking: stable sort by score desc, no special handling of `0.5` (option A) | User explicitly rejected special-casing the neutral default. |
| 5 | Pool exclusion: drop `recommendScore === 0` (CLAUDE.md "hard blacklist" convention); fall back to all tunnels with `serverUrl` if zero-filtered pool is empty | Honors existing project convention without re-deriving it. |
| 6 | Persistence: **no new storage key**. Auto is derived from `selectedCloudTunnel === null && serverMode === 'manual'`. The previous concrete selection is not persisted (current behavior). | YAGNI; default = Auto on first launch and after every restart, satisfying "default to Auto" requirement zero-cost. |
| 7 | Platform: all (Desktop / Mobile / Web) | Pure webapp logic; no bridge changes. |
| 8 | Sentinel representation: a single module-level `AUTO_TUNNEL_SENTINEL: Tunnel` constant; identity via reference equality, never via `domain === '__auto__'` magic-string scan. | Single source of truth; `isAutoSelection(t)` helper centralises the check. |

## Architecture

### State model

`connection.store.ts`:
- Field shape unchanged: `selectedCloudTunnel: Tunnel | null`.
- A new module-level constant `AUTO_TUNNEL_SENTINEL: Tunnel` (frozen object literal). Fields chosen to be inert: `domain = '__auto__'`, `recommendScore = 0`, empty `node`, no `serverUrl`. The sentinel is never passed to `_k2.run('up')` and never escapes the store/UI boundary.
- A derived selector `useEffectiveCloudSelection(): Tunnel | null` returns `AUTO_TUNNEL_SENTINEL` when `serverMode === 'manual' && selectedCloudTunnel === null`, otherwise returns `selectedCloudTunnel` as-is.
- A pure helper `isAutoSelection(t: Tunnel | null): boolean` checks `t === AUTO_TUNNEL_SENTINEL` (reference equality).

### Random pick utility

New file `webapp/src/utils/auto-tunnel-pick.ts`:

```ts
export function pickAutoTunnel(
  tunnels: Tunnel[],
  rng: () => number = Math.random,
): Tunnel | null {
  if (tunnels.length === 0) return null;
  const eligible = tunnels.filter(t => t.recommendScore > 0 && t.serverUrl);
  const pool = eligible.length > 0 ? eligible : tunnels.filter(t => t.serverUrl);
  if (pool.length === 0) return null;
  const sorted = [...pool].sort((a, b) => b.recommendScore - a.recommendScore);
  const top = sorted.slice(0, 3);
  return top[Math.floor(rng() * top.length)];
}
```

Properties:
- Pure, deterministic given an injected `rng`.
- Excludes `recommendScore === 0` (project hard-blacklist convention).
- Excludes tunnels with no `serverUrl` (avoids downstream `buildConnectConfig` crash).
- Fallback path when zero-filtered pool is empty: still picks from non-empty `serverUrl` tunnels (so all-zero-score states still produce a selection).
- Returns `null` only when no tunnel has any `serverUrl` — caller must surface a user-facing error.

### Tunnel list source for Auto resolution

The connect store does not own the tunnel list — `CloudTunnelList` fetches it via `cloudApi.get('/api/tunnels/k2v4')` and writes the response to the shared `services/cache-store.ts` under key `api:tunnels`. The same cache is read by `Dashboard.enrichConnectedTunnel` (line 109). The connect store reads from the same place when resolving Auto, avoiding any new state plumbing.

If the cache is empty (e.g., the user hits Connect before the first list fetch completes), the Auto resolution returns `null` and `connect()` surfaces a user-facing error — see Error handling.

### Cold-start enrichment fix (`enrichFromTunnelList`)

The existing `enrichFromTunnelList` action (`connection.store.ts:347-363`) sets `selectedCloudTunnel: match` as part of cold-start enrichment when VPN survives an app restart. This silently overwrites Auto mode (`selectedCloudTunnel === null`) with a concrete tunnel every time enrichment runs, kicking the user out of Auto.

Fix: **`enrichFromTunnelList` must only update `connectedTunnel` and `activeTunnel`, never `selectedCloudTunnel`.** `selectedCloudTunnel` represents user intent (their pick); `connectedTunnel` represents connection state. Conflating them was already incorrect (a user who picked B mid-connection would see their selection rolled back to A on cold-start) — Auto mode makes the bug user-visible.

Concretely, change the `set({...})` call from:

```ts
set({ connectedTunnel: enriched, selectedCloudTunnel: match, activeTunnel: enriched });
```

to:

```ts
set({ connectedTunnel: enriched, activeTunnel: enriched });
```

Side effect on existing users: cold-start no longer "fills in" the list-row highlight for the connected tunnel via `selectedCloudTunnel`. The top card still shows it via `connectedTunnel` (unchanged), and the list-row display can use `connectedTunnel.domain` as a separate "currently connected" badge if visual parity is desired (out of scope for this spec; current top-card display is sufficient).

### Stale-selection auto-fallback

If the server-side tunnel list shrinks (a tunnel is retired / removed by ops) while the user has that tunnel selected, the user would otherwise see "no row selected" in the list (the `selectedCloudTunnel` reference points to a tunnel no longer present), inconsistent with the "default to Auto" behavior across restart.

Fix: every time `cacheStore` receives a fresh tunnel list, the connect store reconciles `selectedCloudTunnel` against the new list. If the currently selected concrete tunnel is no longer present, clear `selectedCloudTunnel` to `null` — Auto is then derived automatically.

Add a new action `reconcileSelection(tunnels: Tunnel[])` on the connect store:

```ts
reconcileSelection: (tunnels) => {
  const { selectedCloudTunnel } = get();
  if (!selectedCloudTunnel || isAutoSelection(selectedCloudTunnel)) return;
  const stillExists = tunnels.some(t => t.domain === selectedCloudTunnel.domain);
  if (!stillExists) {
    console.info('[Connection] selected tunnel ' + selectedCloudTunnel.domain
      + ' offline, falling back to Auto');
    set({ selectedCloudTunnel: null });
  }
},
```

Call site: `Dashboard.handleTunnelsLoaded` (line 188-192) — extend it from `setCloudTunnels(tunnels); enrichFromTunnelList(tunnels);` to also call `reconcileSelection(tunnels)`. Also call from the warm-start cache read at `Dashboard.tsx:107-114` (the existing cache enrichment path) so reconciliation runs whenever the list is observed, not only on fresh fetches.

Edge cases:
- VPN still connected through the retired tunnel: only `selectedCloudTunnel` clears; `connectedTunnel` (the active connection snapshot) is unaffected. Top card shows `⚡ Auto · current: <retired-tunnel-info>` until user disconnects or daemon errors out.
- Retired tunnel had `domain` collision with a new tunnel (very unlikely): the new tunnel is treated as "still exists" — selection survives, points to the new tunnel record. Acceptable; domain is the canonical identity.

### Connect path

`connection.store.ts` `connect()` resolves the sentinel before downstream logic:

```ts
const { selectedCloudTunnel, serverMode } = get();

let resolvedTunnel = selectedCloudTunnel;
if (serverMode === 'manual' && selectedCloudTunnel === null) {
  // Read tunnel list from the shared cacheStore (key 'api:tunnels'), not from
  // Dashboard component-local state. cacheStore is the same source CloudTunnelList
  // and Dashboard.enrichConnectedTunnel already use, so it is always populated by
  // the time the user can click Connect.
  const cached = cacheStore.get<TunnelListResponse>('api:tunnels');
  const tunnelList = cached?.items ?? [];
  resolvedTunnel = pickAutoTunnel(tunnelList);
  if (!resolvedTunnel) {
    vpnDispatch('BACKEND_ERROR', {
      error: { code: 400, message: 'No tunnel available for auto pick' },
      isRetrying: false,
    });
    return;
  }
  console.info('[Connection] auto-pick → ' + resolvedTunnel.domain
    + ' (score=' + resolvedTunnel.recommendScore + ')');
}
// from here, all existing connect() logic uses resolvedTunnel in place of selectedCloudTunnel.
```

Result:
- `connectedTunnel` snapshot is built from `resolvedTunnel` → top-card "current hit" displays the actual tunnel.
- `lastServerUrl` writes the URL of `resolvedTunnel` → cold-start enrichment reverse-lookup works unchanged.
- `selectedCloudTunnel` remains `null` after connect → next connect re-runs `pickAutoTunnel` (decision #2).

### UI components

**`CloudTunnelList.tsx`**

- Render the Auto virtual item ahead of `sortedTunnels` (not part of the array, only the rendered output).
- Auto item visuals: lightning icon `⚡`, primary text from i18n `dashboard:auto.title`, subtitle from `dashboard:auto.subtitle` ("在最优三个节点中随机选择" / "Random pick from top 3").
- No `RecommendBar` / `ProbeChip` on the Auto item (per-tunnel signals don't apply).
- Selected styling identical to a normal selected tunnel row.
- Selected detection: `(isAutoSelection(effective) && isAutoRow(item)) || (!isAutoRow(item) && effective?.domain === item.domain)`.
- Click handler:
  - Auto row → call new store action `clearCloudSelection()` (sets `selectedCloudTunnel: null`, `serverMode: 'manual'`).
  - Concrete row → existing `selectCloudTunnel(tunnel)` path.

**`Dashboard.tsx` top "current server" card**

- When `effective` is the Auto sentinel: render two-line layout — primary `⚡ 自动选择` (i18n), secondary `当前命中：{country} · {name}` from `connectedTunnel` if connected, or `未连接` placeholder.
- When `effective` is a concrete tunnel: keep existing rendering.
- **Remove** the migration-aid effect at `Dashboard.tsx:194-216` ("auto-pick first sorted tunnel for migrating users") — superseded by Auto-as-default.

**i18n**

New keys under `dashboard.json`:

```json
{
  "auto": {
    "title": "自动选择",
    "subtitle": "在最优三个节点中随机选择",
    "connected": "当前命中：{{country}} · {{name}}",
    "notConnected": "未连接",
    "noTunnelAvailable": "暂无可用节点"
  }
}
```

`zh-CN` first; manually translate to `en-US`, `ja`, `zh-TW`, `zh-HK`, `en-AU`, `en-GB`.

## Data flow (sequence)

```
1. Cold start
   ├─ initializeAllStores() runs
   ├─ selectedCloudTunnel = null (no persistence, current behavior)
   ├─ serverMode = 'manual' (persisted) OR 'self_hosted' (persisted)
   └─ if serverMode='manual': useEffectiveCloudSelection() → AUTO_TUNNEL_SENTINEL → list shows Auto selected

2. User clicks "Connect"
   ├─ connect() called
   ├─ resolvedTunnel = pickAutoTunnel(cloudTunnels)
   ├─ connectedTunnel snapshot built from resolvedTunnel
   ├─ lastServerUrl persisted as resolvedTunnel.serverUrl URL
   └─ _k2.run('up', config) called with concrete URL

3. User clicks a concrete tunnel row
   ├─ selectCloudTunnel(tunnel) → selectedCloudTunnel = tunnel
   ├─ useEffectiveCloudSelection() → that tunnel
   └─ Auto row deselected, concrete row selected

4. User clicks Auto row again
   ├─ clearCloudSelection() → selectedCloudTunnel = null
   └─ useEffectiveCloudSelection() → AUTO_TUNNEL_SENTINEL → Auto row selected

5. App restart while VPN still up (cold-start enrichment)
   ├─ selectedCloudTunnel = null, serverMode='manual' → Auto by default
   ├─ lastServerUrl loaded, reverse-lookup populates connectedTunnel
   └─ Top card: "⚡ Auto · current: JP - Tokyo"

6. Disconnect → reconnect
   ├─ disconnect() clears connectedTunnel + lastServerUrl
   ├─ User clicks Connect again
   └─ pickAutoTunnel runs fresh (new top-3 pick possible)
```

## Error handling

| Scenario | Behavior |
|---|---|
| Auto + empty `cloudTunnels` (list not yet loaded) | `pickAutoTunnel` returns null → `connect()` dispatches `BACKEND_ERROR { code: 400 }` mapped to i18n `dashboard:auto.noTunnelAvailable` |
| Auto + all tunnels have `score === 0` | Falls back to non-empty `serverUrl` pool — does not pick a 0-score tunnel unless that's the only option |
| Auto + only 1 valid tunnel | Top = `[t]`, picked unconditionally |
| Auto + 2 valid tunnels | Top = 2 entries, 50/50 random |
| User toggles selection mid-connect | Existing `connectEpoch` + vpn-machine state guard rejects re-entry; selection change does not affect in-flight request |
| Disconnect → immediate reconnect | Standard path; `pickAutoTunnel` runs again, may pick a different tunnel (decision #2) |
| Cold start with VPN still active | `lastServerUrl` reverse-lookup populates `connectedTunnel`; `selectedCloudTunnel` stays null (Auto derived) |
| Picked tunnel is dead / blocked | Falls into existing vpn-machine `BACKEND_ERROR` → user retries → re-picks (self-healing) |
| User had concrete tunnel X selected; X retired from server-side list | `reconcileSelection` clears `selectedCloudTunnel` to `null` on next list refresh → list shows Auto selected |
| User had X selected, X retired, VPN still connected via X | `selectedCloudTunnel` clears to Auto; `connectedTunnel` keeps showing X until user disconnects; on next Connect `pickAutoTunnel` picks Y from new list, k2 switches |

## Testing

**Vitest unit — `webapp/src/utils/__tests__/auto-tunnel-pick.test.ts`**

| Case | Expectation |
|---|---|
| Empty input | Returns `null` |
| All score=0 | Falls back to non-empty `serverUrl` pool |
| All score=0 and all missing `serverUrl` | Returns `null` |
| Single valid tunnel | Returns it (independent of rng) |
| `[0.9, 0.8, 0.7, 0.5, 0.5]` rng=0 | Picks the 0.9 entry |
| `[0.9, 0.8, 0.7, 0.5, 0.5]` rng=0.5 | Picks the 0.8 entry |
| `[0.9, 0.8, 0.7, 0.5, 0.5]` rng=0.99 | Picks the 0.7 entry |
| 100 tunnels all 0.5 | Top-3 = first 3 in input order; 0.5 not specially handled |
| Mixed pool with score=0 entries | 0-score tunnels never appear in result |
| Mixed pool with missing `serverUrl` | Those tunnels never appear |

**Vitest store — `webapp/src/stores/__tests__/connection.store.test.ts` (extend)**

| Case | Expectation |
|---|---|
| `connect()` with `serverMode='manual'`, `selectedCloudTunnel=null`, `cacheStore` populated with tunnels | `pickAutoTunnel` is called, `_k2.run('up')` receives a concrete URL, `connectedTunnel` reflects the resolved tunnel |
| `connect()` with `selectedCloudTunnel=null`, `cacheStore` empty / unset | Dispatches `BACKEND_ERROR`, `_k2.run('up')` not called |
| `connect()` with concrete `selectedCloudTunnel` | `pickAutoTunnel` not called (regression guard) |
| Two consecutive Auto connects with disconnect between | `pickAutoTunnel` invoked twice (decision #2 verification) |
| `enrichFromTunnelList` called with `selectedCloudTunnel = null` and a matching tunnel | `connectedTunnel` + `activeTunnel` populated; `selectedCloudTunnel` stays `null` (Auto preserved across cold-start enrichment) |
| `reconcileSelection` called with concrete `selectedCloudTunnel` that is missing from the new list | `selectedCloudTunnel` clears to `null` (Auto) |
| `reconcileSelection` called with concrete `selectedCloudTunnel` that is present in the new list | `selectedCloudTunnel` unchanged |
| `reconcileSelection` called when already in Auto (`selectedCloudTunnel === null`) | No-op |

**Vitest component — `webapp/src/pages/__tests__/Dashboard.test.tsx` (extend)**

| Case | Expectation |
|---|---|
| `selectedCloudTunnel=null` + `serverMode='manual'` + non-empty list | First list row is the Auto virtual item, marked selected |
| Auto selected + `connectedTunnel = {country: 'JP', name: 'Tokyo'}` | Top card renders "⚡ 自动选择" + "当前命中：JP · Tokyo" |
| User clicks a concrete tunnel | `selectedCloudTunnel` becomes that tunnel; Auto row no longer selected |
| User clicks Auto row from concrete selection | `selectedCloudTunnel` returns to `null`; Auto row selected |

**Regression**

- Keep all existing tests for `connection.store`, `Dashboard`, `CloudTunnelList`.
- Delete the `smart→manual migration aid` test cases (if any) when removing `Dashboard.tsx:194-216`.

## Files touched

| File | Change |
|---|---|
| `webapp/src/utils/auto-tunnel-pick.ts` | **New** — pure pick function |
| `webapp/src/utils/__tests__/auto-tunnel-pick.test.ts` | **New** — unit tests |
| `webapp/src/stores/connection.store.ts` | Add `AUTO_TUNNEL_SENTINEL`, `useEffectiveCloudSelection`, `isAutoSelection`, `clearCloudSelection`, `reconcileSelection`; modify `connect()` to resolve the sentinel; remove `selectedCloudTunnel: match` from `enrichFromTunnelList` |
| `webapp/src/components/CloudTunnelList.tsx` | Render Auto virtual item; selection click dispatch |
| `webapp/src/pages/Dashboard.tsx` | Update top card rendering for Auto; remove migration aid (lines 194-216) |
| `webapp/src/i18n/locales/*/dashboard.json` (×7) | Add `auto.*` keys |
| `webapp/src/stores/__tests__/connection.store.test.ts` | Extend with Auto cases |
| `webapp/src/pages/__tests__/Dashboard.test.tsx` | Extend with Auto cases |

No backend changes. No bridge changes. No persistence migration.

## Open questions

None — all brainstorming questions resolved.
