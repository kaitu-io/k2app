# Auto Tunnel Pick Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a webapp-only "Auto" selection (default) that picks one of the top-3 tunnels by `recommendScore` at random on each connect, with stale-selection auto-fallback and cold-start enrichment fix.

**Architecture:** Pure webapp change. Module-level sentinel `Tunnel` + reference-equality helpers in `connection.store.ts`. New pure utility `auto-tunnel-pick.ts`. Connect path resolves the sentinel before calling `_k2.run('up')`. Tunnel list source is the existing `cacheStore` under key `api:tunnels`.

**Tech Stack:** React 18, TypeScript, Zustand, Material-UI 5, Vitest, react-i18next.

**Spec:** [`docs/superpowers/specs/2026-04-27-auto-tunnel-pick-design.md`](../specs/2026-04-27-auto-tunnel-pick-design.md)

---

## File map

| File | Status | Responsibility |
|---|---|---|
| `webapp/src/utils/auto-tunnel-pick.ts` | **New** | Pure pick function: top-3-by-score → random 1 |
| `webapp/src/utils/__tests__/auto-tunnel-pick.test.ts` | **New** | Pick function unit tests |
| `webapp/src/stores/connection.store.ts` | Modify | Sentinel + helpers + actions + connect()/enrich() updates |
| `webapp/src/stores/__tests__/connection.store.test.ts` | Modify | Add cases for Auto resolution, reconcile, enrich fix |
| `webapp/src/components/CloudTunnelList.tsx` | Modify | Render virtual Auto row at top |
| `webapp/src/components/__tests__/CloudTunnelList.test.tsx` | Modify | Add Auto row render + click cases |
| `webapp/src/pages/Dashboard.tsx` | Modify | Use `useEffectiveCloudSelection`; top card Auto display; wire `reconcileSelection`; delete migration aid |
| `webapp/src/pages/__tests__/Dashboard.test.tsx` | Modify | Add Auto display cases |
| `webapp/src/i18n/locales/{zh-CN,en-US,ja,zh-TW,zh-HK,en-AU,en-GB}/dashboard.json` | Modify (×7) | New `auto.*` keys |

`dashboard.autoSelect` already exists in `zh-CN`; will reuse it where the literal "自动选择" was historically used. New keys live under `dashboard.auto.*` namespace to avoid colliding.

---

## Task 1: Add `pickAutoTunnel` pure utility (TDD)

**Files:**
- Create: `webapp/src/utils/auto-tunnel-pick.ts`
- Create: `webapp/src/utils/__tests__/auto-tunnel-pick.test.ts`

- [ ] **Step 1: Write the failing test**

Create `webapp/src/utils/__tests__/auto-tunnel-pick.test.ts`:

```ts
import { describe, it, expect } from 'vitest';
import { pickAutoTunnel } from '../auto-tunnel-pick';
import type { Tunnel } from '../../services/api-types';

function tunnel(id: number, domain: string, recommendScore: number, serverUrl?: string): Tunnel {
  return {
    id,
    domain,
    name: domain,
    protocol: 'k2v5',
    port: 443,
    serverUrl: serverUrl ?? `k2v5://${domain}`,
    node: { name: '', country: '', region: '', ipv4: '', ipv6: '', load: 0, trafficUsagePercent: 0, bandwidthUsagePercent: 0 },
    recommendScore,
  };
}

describe('pickAutoTunnel', () => {
  it('returns null for empty input', () => {
    expect(pickAutoTunnel([])).toBeNull();
  });

  it('returns null when no tunnel has serverUrl', () => {
    const t = [tunnel(1, 'a', 0.9, '')];
    expect(pickAutoTunnel(t)).toBeNull();
  });

  it('returns the only valid tunnel regardless of rng', () => {
    const t = [tunnel(1, 'a', 0.7)];
    expect(pickAutoTunnel(t, () => 0.999)?.domain).toBe('a');
  });

  it('picks index 0 of top-3 when rng=0', () => {
    const t = [
      tunnel(1, 'a', 0.9),
      tunnel(2, 'b', 0.8),
      tunnel(3, 'c', 0.7),
      tunnel(4, 'd', 0.5),
      tunnel(5, 'e', 0.5),
    ];
    expect(pickAutoTunnel(t, () => 0)?.domain).toBe('a');
  });

  it('picks index 1 of top-3 when rng=0.5', () => {
    const t = [
      tunnel(1, 'a', 0.9),
      tunnel(2, 'b', 0.8),
      tunnel(3, 'c', 0.7),
    ];
    expect(pickAutoTunnel(t, () => 0.5)?.domain).toBe('b');
  });

  it('picks index 2 of top-3 when rng=0.99', () => {
    const t = [
      tunnel(1, 'a', 0.9),
      tunnel(2, 'b', 0.8),
      tunnel(3, 'c', 0.7),
    ];
    expect(pickAutoTunnel(t, () => 0.99)?.domain).toBe('c');
  });

  it('treats 0.5 entries as ordinary scores (no special handling)', () => {
    const t = Array.from({ length: 10 }, (_, i) => tunnel(i + 1, `t${i}`, 0.5));
    // top-3 = first 3 in stable input order
    expect(pickAutoTunnel(t, () => 0)?.domain).toBe('t0');
    expect(pickAutoTunnel(t, () => 0.5)?.domain).toBe('t1');
    expect(pickAutoTunnel(t, () => 0.99)?.domain).toBe('t2');
  });

  it('excludes tunnels with score=0 from primary pool', () => {
    const t = [
      tunnel(1, 'zero1', 0),
      tunnel(2, 'zero2', 0),
      tunnel(3, 'good', 0.5),
    ];
    // Only one eligible; pickAutoTunnel returns it independent of rng.
    expect(pickAutoTunnel(t, () => 0.5)?.domain).toBe('good');
  });

  it('falls back to all-with-serverUrl when every score is 0', () => {
    const t = [
      tunnel(1, 'a', 0),
      tunnel(2, 'b', 0),
      tunnel(3, 'c', 0),
    ];
    const picked = pickAutoTunnel(t, () => 0);
    expect(picked?.domain).toBe('a');
  });

  it('excludes tunnels missing serverUrl', () => {
    const t = [
      tunnel(1, 'no-url', 0.9, ''),
      tunnel(2, 'good', 0.5),
    ];
    expect(pickAutoTunnel(t, () => 0)?.domain).toBe('good');
  });
});
```

- [ ] **Step 2: Run test to verify it fails**

```bash
cd webapp && npx vitest run src/utils/__tests__/auto-tunnel-pick.test.ts
```

Expected: FAIL — module not found.

- [ ] **Step 3: Implement the utility**

Create `webapp/src/utils/auto-tunnel-pick.ts`:

```ts
import type { Tunnel } from '../services/api-types';

/**
 * Pick one tunnel from the top-3 by recommendScore at random.
 *
 * - Excludes tunnels with `recommendScore === 0` (project hard-blacklist
 *   convention, see api/CLAUDE.md "Tunnel Scoring").
 * - Excludes tunnels missing `serverUrl` (downstream `_k2.run('up')` would crash).
 * - Falls back to non-empty `serverUrl` pool when zero-filtered pool is empty.
 * - Returns null when nothing has a usable serverUrl — caller MUST surface a user error.
 *
 * Stable score-desc sort: ties keep input order (database / country-sorted order).
 *
 * @param rng injectable randomness for tests; defaults to Math.random
 */
export function pickAutoTunnel(
  tunnels: Tunnel[],
  rng: () => number = Math.random,
): Tunnel | null {
  if (tunnels.length === 0) return null;
  const eligible = tunnels.filter(t => t.recommendScore > 0 && !!t.serverUrl);
  const pool = eligible.length > 0 ? eligible : tunnels.filter(t => !!t.serverUrl);
  if (pool.length === 0) return null;
  const sorted = [...pool].sort((a, b) => b.recommendScore - a.recommendScore);
  const top = sorted.slice(0, 3);
  return top[Math.floor(rng() * top.length)];
}
```

- [ ] **Step 4: Run test to verify it passes**

```bash
cd webapp && npx vitest run src/utils/__tests__/auto-tunnel-pick.test.ts
```

Expected: PASS — all 10 cases green.

- [ ] **Step 5: Commit**

```bash
git add webapp/src/utils/auto-tunnel-pick.ts webapp/src/utils/__tests__/auto-tunnel-pick.test.ts
git commit -m "feat(webapp): add pickAutoTunnel utility (top-3-by-score random)"
```

---

## Task 2: Add Auto sentinel + identity helpers in connection store

**Files:**
- Modify: `webapp/src/stores/connection.store.ts`

- [ ] **Step 1: Add the sentinel constant and identity helper near the top of the file**

Open `webapp/src/stores/connection.store.ts`. After existing imports and before the first `const` block (around line 60-70), add:

```ts
/**
 * Canonical "Auto pick" sentinel domain.
 *
 * Exported so UI props that flow as `selectedDomain: string | null` (e.g.,
 * CloudTunnelList) can compare against this constant instead of repeating
 * the literal `'__auto__'`.
 */
export const AUTO_TUNNEL_DOMAIN = '__auto__';

/**
 * Module-level sentinel Tunnel representing "Auto pick mode".
 *
 * Identity is by reference equality (use `isAutoSelection`). Field values
 * are inert and intentionally unusable: serverUrl is empty so the sentinel
 * can never be passed to `_k2.run('up')` by accident. The `connect()` path
 * resolves the sentinel into a concrete Tunnel via `pickAutoTunnel` before
 * any call into the bridge.
 */
export const AUTO_TUNNEL_SENTINEL: Tunnel = Object.freeze({
  id: -1,
  domain: AUTO_TUNNEL_DOMAIN,
  name: 'Auto',
  protocol: 'k2v5',
  port: 0,
  serverUrl: '',
  node: Object.freeze({
    name: '',
    country: '',
    region: '',
    ipv4: '',
    ipv6: '',
    load: 0,
    trafficUsagePercent: 0,
    bandwidthUsagePercent: 0,
  }) as Tunnel['node'],
  recommendScore: 0,
}) as Tunnel;

/** True when `t` is the Auto sentinel (reference equality). */
export function isAutoSelection(t: Tunnel | null): boolean {
  return t === AUTO_TUNNEL_SENTINEL;
}
```

If `Tunnel` is not imported in this file, add to the top imports:

```ts
import type { Tunnel } from '../services/api-types';
```

(Search the file for `Tunnel` to confirm whether the import already exists.)

- [ ] **Step 2: Type-check**

```bash
cd webapp && npx tsc --noEmit
```

Expected: PASS — no type errors.

- [ ] **Step 3: Commit**

```bash
git add webapp/src/stores/connection.store.ts
git commit -m "feat(webapp): add AUTO_TUNNEL_SENTINEL + isAutoSelection helper"
```

---

## Task 3: Add `useEffectiveCloudSelection` derived selector

**Files:**
- Modify: `webapp/src/stores/connection.store.ts`

- [ ] **Step 1: Add the selector hook**

Append to `webapp/src/stores/connection.store.ts`, after the store definition (after the last `useConnectionStore` export but inside the file):

```ts
/**
 * Returns the effective cloud tunnel selection for UI consumption.
 *
 * - Returns AUTO_TUNNEL_SENTINEL when serverMode='manual' and no concrete
 *   tunnel is selected (the default state — Auto is selected).
 * - Returns the concrete selected tunnel when one is chosen.
 * - Returns null in self_hosted mode (cloud selection does not apply).
 *
 * UI components should use this hook rather than reading `selectedCloudTunnel`
 * directly, so the Auto default surfaces consistently in the list and top card.
 */
export function useEffectiveCloudSelection(): Tunnel | null {
  return useConnectionStore((s) => {
    if (s.serverMode !== 'manual') return null;
    return s.selectedCloudTunnel ?? AUTO_TUNNEL_SENTINEL;
  });
}
```

- [ ] **Step 2: Type-check**

```bash
cd webapp && npx tsc --noEmit
```

Expected: PASS.

- [ ] **Step 3: Commit**

```bash
git add webapp/src/stores/connection.store.ts
git commit -m "feat(webapp): add useEffectiveCloudSelection selector"
```

---

## Task 4: Add `clearCloudSelection` and `reconcileSelection` actions

**Files:**
- Modify: `webapp/src/stores/connection.store.ts`

- [ ] **Step 1: Extend the store interface**

Find the interface that types the store (search for `selectCloudTunnel: (tunnel: Tunnel) => void;`). Add these two actions next to it:

```ts
clearCloudSelection: () => void;
reconcileSelection: (tunnels: Tunnel[]) => void;
```

- [ ] **Step 2: Implement the actions**

Find the `selectCloudTunnel` implementation (around line 161). Add directly after it, before `selectSelfHosted`:

```ts
clearCloudSelection: () => {
  console.info('[Connection] clearCloudSelection (→ Auto via derivation)');
  set({
    selectedCloudTunnel: null,
    activeTunnel: null,
    serverMode: 'manual',
  });
  void persistServerMode('manual');
},

reconcileSelection: (tunnels) => {
  const { selectedCloudTunnel } = get();
  if (!selectedCloudTunnel || isAutoSelection(selectedCloudTunnel)) return;
  const stillExists = tunnels.some(t => t.domain === selectedCloudTunnel.domain);
  if (!stillExists) {
    console.info('[Connection] selected tunnel ' + selectedCloudTunnel.domain
      + ' offline, falling back to Auto');
    set({ selectedCloudTunnel: null, activeTunnel: null });
  }
},
```

- [ ] **Step 3: Type-check**

```bash
cd webapp && npx tsc --noEmit
```

Expected: PASS.

- [ ] **Step 4: Commit**

```bash
git add webapp/src/stores/connection.store.ts
git commit -m "feat(webapp): add clearCloudSelection + reconcileSelection actions"
```

---

## Task 5: Fix `enrichFromTunnelList` (no longer overwrite selectedCloudTunnel)

**Files:**
- Modify: `webapp/src/stores/connection.store.ts:347-363`
- Modify: `webapp/src/stores/__tests__/connection.store.test.ts`

- [ ] **Step 1: Write failing test for the fix**

Open `webapp/src/stores/__tests__/connection.store.test.ts`. Add a new `describe` block (or new test inside the existing `enrichFromTunnelList` describe if one exists):

```ts
describe('enrichFromTunnelList preserves Auto', () => {
  it('does not overwrite selectedCloudTunnel when null (Auto)', () => {
    // Arrange: connectedTunnel exists from cold-start (cloud source, no country)
    useConnectionStore.setState({
      selectedCloudTunnel: null,
      connectedTunnel: {
        source: 'cloud',
        domain: 'tokyo.kaitu.io',
        name: '',
        country: '',
        load: 0,
      } as ActiveTunnel,
    });
    const tunnels: Tunnel[] = [
      {
        id: 1, domain: 'tokyo.kaitu.io', name: 'Tokyo', protocol: 'k2v5',
        port: 443, serverUrl: 'k2v5://tokyo.kaitu.io',
        node: { name: 'tokyo', country: 'JP', region: 'Tokyo', ipv4: '1.1.1.1',
                ipv6: '', load: 50, trafficUsagePercent: 0, bandwidthUsagePercent: 0 },
        recommendScore: 0.7,
      },
    ];

    // Act
    useConnectionStore.getState().enrichFromTunnelList(tunnels);

    // Assert: connectedTunnel enriched, selectedCloudTunnel still null (Auto preserved)
    const s = useConnectionStore.getState();
    expect(s.connectedTunnel?.country).toBe('JP');
    expect(s.selectedCloudTunnel).toBeNull();
  });
});
```

You may need to add imports at the top of the test file:

```ts
import type { ActiveTunnel } from '../connection.store'; // adjust if not exported
import type { Tunnel } from '../../services/api-types';
```

If `ActiveTunnel` is not exported, use `as any` or skip strict typing for the test fixture.

- [ ] **Step 2: Run test to verify it fails**

```bash
cd webapp && npx vitest run src/stores/__tests__/connection.store.test.ts -t "preserves Auto"
```

Expected: FAIL — `s.selectedCloudTunnel` is the matched tunnel, not null.

- [ ] **Step 3: Apply the fix**

Open `webapp/src/stores/connection.store.ts`. Find `enrichFromTunnelList` (around line 347-363). Locate the `set({...})` call:

```ts
set({
  connectedTunnel: enriched,
  selectedCloudTunnel: match,
  activeTunnel: enriched,
});
```

Remove the `selectedCloudTunnel: match` line so it becomes:

```ts
set({
  connectedTunnel: enriched,
  activeTunnel: enriched,
});
```

- [ ] **Step 4: Run test to verify it passes**

```bash
cd webapp && npx vitest run src/stores/__tests__/connection.store.test.ts -t "preserves Auto"
```

Expected: PASS.

- [ ] **Step 5: Run the full connection.store test file to confirm no regression**

```bash
cd webapp && npx vitest run src/stores/__tests__/connection.store.test.ts
```

Expected: PASS — all existing tests still green. If a pre-existing test asserted `selectedCloudTunnel: match` after enrichment, update it to assert `selectedCloudTunnel` is unchanged — the prior assertion encoded the bug being fixed. Reference: spec §"Cold-start enrichment fix".

- [ ] **Step 6: Commit**

```bash
git add webapp/src/stores/connection.store.ts webapp/src/stores/__tests__/connection.store.test.ts
git commit -m "fix(webapp): enrichFromTunnelList no longer overwrites selectedCloudTunnel

Conflated user intent (selection) with connection state (snapshot); was
silently kicking Auto-mode users out on every cold-start enrichment."
```

---

## Task 6: Resolve Auto sentinel in `connect()`

**Files:**
- Modify: `webapp/src/stores/connection.store.ts` `connect()` action
- Modify: `webapp/src/stores/__tests__/connection.store.test.ts`

- [ ] **Step 1: Write failing tests**

Append to `webapp/src/stores/__tests__/connection.store.test.ts`:

```ts
describe('connect() resolves Auto via pickAutoTunnel', () => {
  beforeEach(() => {
    cacheStore.clear();
    useConnectionStore.setState({
      selectedCloudTunnel: null,
      serverMode: 'manual',
      connectEpoch: 0,
      activeTunnel: null,
      connectedTunnel: null,
    });
  });

  it('dispatches BACKEND_ERROR when cacheStore is empty (Auto + no list)', async () => {
    const dispatchSpy = vi.spyOn(vpnDispatchModule, 'vpnDispatch');
    await useConnectionStore.getState().connect();
    expect(dispatchSpy).toHaveBeenCalledWith('BACKEND_ERROR', expect.objectContaining({
      error: expect.objectContaining({ code: 400 }),
      isRetrying: false,
    }));
  });

  it('resolves Auto via pickAutoTunnel and proceeds to connect', async () => {
    const tunnel: Tunnel = {
      id: 1, domain: 'tokyo.kaitu.io', name: 'Tokyo', protocol: 'k2v5',
      port: 443, serverUrl: 'k2v5://tokyo.kaitu.io',
      node: { name: 'tokyo', country: 'JP', region: 'Tokyo', ipv4: '1.1.1.1',
              ipv6: '', load: 50, trafficUsagePercent: 0, bandwidthUsagePercent: 0 },
      recommendScore: 0.7,
    };
    cacheStore.set('api:tunnels', { items: [tunnel], echConfigList: '' });
    // Stub authService.buildTunnelUrl to return URL synchronously
    vi.spyOn(authService, 'buildTunnelUrl').mockResolvedValue('k2v5://tokyo.kaitu.io?token=t');
    // Stub _k2.run to track call
    const runMock = vi.fn().mockResolvedValue({ code: 0 });
    (window as any)._k2 = { run: runMock };

    await useConnectionStore.getState().connect();

    expect(runMock).toHaveBeenCalledWith('up', expect.anything());
    const state = useConnectionStore.getState();
    expect(state.connectedTunnel?.domain).toBe('tokyo.kaitu.io');
    // selectedCloudTunnel must remain null — re-pick on next connect (decision #2)
    expect(state.selectedCloudTunnel).toBeNull();
  });
});
```

You may need to add at the top of the test file (if missing):

```ts
import { cacheStore } from '../../services/cache-store';
import { authService } from '../../services/auth-service'; // adjust path if needed
import * as vpnDispatchModule from '../vpn-machine.store';
import { vi } from 'vitest';
```

Check existing `connection.store.test.ts` for the actual imports and mocking patterns; reuse those patterns. The intent is: `cacheStore` populated → `_k2.run` called with concrete URL.

- [ ] **Step 2: Run tests to verify they fail**

```bash
cd webapp && npx vitest run src/stores/__tests__/connection.store.test.ts -t "resolves Auto"
```

Expected: FAIL — Auto resolution not yet implemented.

- [ ] **Step 3: Modify `connect()` to resolve the sentinel**

Open `webapp/src/stores/connection.store.ts`. At the top of the file, add the import (if missing):

```ts
import { pickAutoTunnel } from '../utils/auto-tunnel-pick';
import { cacheStore } from '../services/cache-store';
import type { TunnelListResponse } from '../services/api-types';
```

Find `connect:` (around line 210). Locate the existing pre-flight check:

```ts
if (serverMode === 'manual' && !selectedCloudTunnel?.serverUrl) {
  console.warn('[Connection] connect: manual mode but selected tunnel has no serverUrl, aborting');
  vpnDispatch('BACKEND_ERROR', {
    error: { code: 400, message: 'No server selected' },
    isRetrying: false,
  });
  return;
}
```

Replace it with the Auto-resolution block:

```ts
// Resolve Auto sentinel (manual mode + null selection) into a concrete Tunnel.
let resolvedTunnel = selectedCloudTunnel;
if (serverMode === 'manual' && selectedCloudTunnel === null) {
  const cached = cacheStore.get<TunnelListResponse>('api:tunnels');
  const tunnelList = cached?.items ?? [];
  resolvedTunnel = pickAutoTunnel(tunnelList);
  if (!resolvedTunnel) {
    console.warn('[Connection] connect: Auto mode but no tunnel available, aborting');
    vpnDispatch('BACKEND_ERROR', {
      error: { code: 400, message: 'No tunnel available for auto pick' },
      isRetrying: false,
    });
    return;
  }
  console.info('[Connection] auto-pick → ' + resolvedTunnel.domain
    + ' (score=' + resolvedTunnel.recommendScore + ')');
}

if (serverMode === 'manual' && !resolvedTunnel?.serverUrl) {
  console.warn('[Connection] connect: manual mode but resolved tunnel has no serverUrl, aborting');
  vpnDispatch('BACKEND_ERROR', {
    error: { code: 400, message: 'No server selected' },
    isRetrying: false,
  });
  return;
}
```

Then in the rest of `connect()`, replace every reference to `selectedCloudTunnel` (used for connection logic — building URL, snapshot, etc.) with `resolvedTunnel`. Specifically, find these lines (use grep on the file for a precise list — line numbers may have drifted):

- Around line 248-253: `connectedTunnelSnapshot` — use `resolvedTunnel` for `computeCloudActiveTunnel`
- Around line 264: log line with `selectedCloudTunnel?.domain` — use `resolvedTunnel?.domain`
- Around line 274-275: `selectedCloudTunnel?.serverUrl` for `buildTunnelUrl` — use `resolvedTunnel?.serverUrl`

Do NOT change references to `selectedCloudTunnel` that are for state-update concerns (e.g., reading-back current state after connect). The variable name `resolvedTunnel` is for "the actual tunnel we are connecting to in this single connect call".

- [ ] **Step 4: Run test to verify it passes**

```bash
cd webapp && npx vitest run src/stores/__tests__/connection.store.test.ts -t "resolves Auto"
```

Expected: PASS — both tests green.

- [ ] **Step 5: Run full store test suite for regressions**

```bash
cd webapp && npx vitest run src/stores/__tests__/connection.store.test.ts
```

Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add webapp/src/stores/connection.store.ts webapp/src/stores/__tests__/connection.store.test.ts
git commit -m "feat(webapp): connect() resolves Auto sentinel via pickAutoTunnel"
```

---

## Task 7: Add Auto virtual row to CloudTunnelList

**Files:**
- Modify: `webapp/src/components/CloudTunnelList.tsx`
- Modify: `webapp/src/components/__tests__/CloudTunnelList.test.tsx`

- [ ] **Step 1: Write failing tests**

Open `webapp/src/components/__tests__/CloudTunnelList.test.tsx`. Add a new describe block:

```ts
import { AUTO_TUNNEL_DOMAIN } from '../../stores/connection.store';

describe('Auto virtual row', () => {
  // Test setup mirrors existing tests' mocking pattern (cacheStore preload, etc.).
  // Reuse helpers already in the file.

  it('renders Auto row as the first list item', async () => {
    // Preload cacheStore with a couple tunnels (helper from existing tests, e.g. seedCachedTunnels)
    seedCachedTunnels([
      { domain: 'a.kaitu.io', country: 'AR' /* etc */ },
      { domain: 'b.kaitu.io', country: 'JP' /* etc */ },
    ]);
    const { getAllByRole } = render(
      <CloudTunnelList selectedDomain={AUTO_TUNNEL_DOMAIN} onSelect={() => {}} />
    );
    const items = await screen.findAllByRole('listitem');
    // First item should be the Auto row
    expect(items[0]).toHaveTextContent(/自动/);
  });

  it('marks Auto row as selected when selectedDomain === AUTO_TUNNEL_DOMAIN', async () => {
    seedCachedTunnels([{ domain: 'a.kaitu.io', country: 'AR' }]);
    render(<CloudTunnelList selectedDomain={AUTO_TUNNEL_DOMAIN} onSelect={() => {}} />);
    const radios = await screen.findAllByRole('radio');
    expect(radios[0]).toBeChecked(); // Auto row's radio
  });

  it('calls onSelect with the Auto sentinel-shaped tunnel when clicked', async () => {
    const onSelect = vi.fn();
    seedCachedTunnels([{ domain: 'a.kaitu.io', country: 'AR' }]);
    render(<CloudTunnelList selectedDomain={null} onSelect={onSelect} />);
    const items = await screen.findAllByRole('listitem');
    await userEvent.click(items[0]);
    expect(onSelect).toHaveBeenCalledWith(
      expect.objectContaining({ domain: AUTO_TUNNEL_DOMAIN }),
      expect.any(String) || undefined,
    );
  });
});
```

Adapt to the existing test file's helpers and mocking style (look at neighboring `describe` blocks for the exact patterns). The tests assert:
1. Auto row is rendered first
2. It is selected when `selectedDomain === AUTO_TUNNEL_DOMAIN`
3. Clicking it calls `onSelect` with the sentinel

- [ ] **Step 2: Run test to verify it fails**

```bash
cd webapp && npx vitest run src/components/__tests__/CloudTunnelList.test.tsx -t "Auto virtual row"
```

Expected: FAIL — Auto row not rendered.

- [ ] **Step 3: Implement the Auto row**

Open `webapp/src/components/CloudTunnelList.tsx`. Add to imports:

```ts
import FlashOnIcon from '@mui/icons-material/FlashOn';
import { AUTO_TUNNEL_SENTINEL, AUTO_TUNNEL_DOMAIN } from '../stores/connection.store';
```

Inside the `<List>` rendering (around line 366-419), modify the JSX to render an Auto row before `sortedTunnels.map(...)`:

```tsx
<List disablePadding sx={{ px: 2 }}>
  {/* Auto virtual row — first item, ahead of country-sorted tunnels */}
  <ListItem
    key="auto"
    disableGutters
    onClick={() => {
      console.debug('[CloudTunnelList] tunnelClick: AUTO');
      !disabled && onSelect(AUTO_TUNNEL_SENTINEL, echConfigList);
    }}
    sx={{
      borderRadius: 2,
      mb: 0.5,
      minHeight: 64,
      bgcolor: selectedDomain === AUTO_TUNNEL_DOMAIN ? colors.selectedBg : undefined,
      cursor: disabled ? 'not-allowed' : 'pointer',
      opacity: disabled ? '0.6 !important' : 1,
      transition: 'all 0.2s ease',
      '&:hover': {
        bgcolor: disabled ? undefined : 'action.hover',
        transform: disabled ? 'none' : 'scale(1.01)',
      },
    }}
  >
    <ListItemIcon sx={{ minWidth: 40, fontSize: 24 }}>
      <FlashOnIcon sx={{ fontSize: 24, color: 'primary.main' }} />
    </ListItemIcon>
    <ListItemText
      primary={t('dashboard:auto.title')}
      secondary={t('dashboard:auto.subtitle')}
      primaryTypographyProps={{ fontWeight: 600, fontSize: '0.9rem' }}
      secondaryTypographyProps={{ fontSize: '0.75rem' }}
    />
    <Radio
      checked={selectedDomain === AUTO_TUNNEL_DOMAIN}
      color="primary"
      value={AUTO_TUNNEL_DOMAIN}
      sx={{ '& .MuiSvgIcon-root': { fontSize: 24 } }}
    />
  </ListItem>

  {sortedTunnels.map((tunnel) => {
    /* existing per-tunnel rendering, unchanged */
    /* ... */
  })}
</List>
```

The Auto row uses `AUTO_TUNNEL_DOMAIN` as the marker for `selectedDomain` comparison; concrete tunnel rows compare against their own `tunnel.domain.toLowerCase()` as before. Since `AUTO_TUNNEL_DOMAIN === '__auto__'` cannot collide with a real tunnel domain, the existing concrete-row logic is unaffected.

Note: the Auto row deliberately omits `RecommendBar` (no per-tunnel score to show).

- [ ] **Step 4: Run tests**

```bash
cd webapp && npx vitest run src/components/__tests__/CloudTunnelList.test.tsx
```

Expected: PASS — new Auto cases pass; existing tests unchanged.

- [ ] **Step 5: Type-check**

```bash
cd webapp && npx tsc --noEmit
```

Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add webapp/src/components/CloudTunnelList.tsx webapp/src/components/__tests__/CloudTunnelList.test.tsx
git commit -m "feat(webapp): render Auto virtual row at top of CloudTunnelList"
```

---

## Task 8: Wire Dashboard to Auto (top card + selection prop + reconcile)

**Files:**
- Modify: `webapp/src/pages/Dashboard.tsx`
- Modify: `webapp/src/pages/__tests__/Dashboard.test.tsx`

- [ ] **Step 1: Write failing tests**

Open `webapp/src/pages/__tests__/Dashboard.test.tsx`. Add cases:

```ts
import { AUTO_TUNNEL_DOMAIN, useConnectionStore } from '../../stores/connection.store';

describe('Dashboard with Auto selection', () => {
  it('shows Auto as default selection when no concrete tunnel picked', async () => {
    // Set serverMode='manual', selectedCloudTunnel=null
    useConnectionStore.setState({ serverMode: 'manual', selectedCloudTunnel: null });
    seedCachedTunnels([{ domain: 'a.kaitu.io', country: 'AR' }]);
    render(<Dashboard />);
    const radios = await screen.findAllByRole('radio');
    // First radio (Auto row) should be checked
    expect(radios[0]).toBeChecked();
  });

  it('top card shows "Auto" + current hit when connected via Auto', async () => {
    useConnectionStore.setState({
      serverMode: 'manual',
      selectedCloudTunnel: null,
      connectedTunnel: {
        source: 'cloud',
        domain: 'tokyo.kaitu.io',
        name: 'Tokyo',
        country: 'JP',
        load: 50,
      } as any,
    });
    render(<Dashboard />);
    expect(screen.getByText(/自动选择/)).toBeInTheDocument();
    // Subtitle shows the resolved tunnel
    expect(screen.getByText(/JP/)).toBeInTheDocument();
  });

  it('clicking concrete tunnel switches selection away from Auto', async () => {
    useConnectionStore.setState({ serverMode: 'manual', selectedCloudTunnel: null });
    seedCachedTunnels([{ domain: 'a.kaitu.io', country: 'AR' }]);
    render(<Dashboard />);
    const items = await screen.findAllByRole('listitem');
    await userEvent.click(items[1]); // first concrete row (Auto is items[0])
    const state = useConnectionStore.getState();
    expect(state.selectedCloudTunnel?.domain).toBe('a.kaitu.io');
  });

  it('clicking Auto row from concrete selection clears back to Auto', async () => {
    seedCachedTunnels([{ domain: 'a.kaitu.io', country: 'AR' }]);
    useConnectionStore.setState({
      serverMode: 'manual',
      selectedCloudTunnel: { domain: 'a.kaitu.io', /* ... */ } as Tunnel,
    });
    render(<Dashboard />);
    const items = await screen.findAllByRole('listitem');
    await userEvent.click(items[0]); // Auto row
    expect(useConnectionStore.getState().selectedCloudTunnel).toBeNull();
  });
});
```

Use the same helper patterns the existing `Dashboard.test.tsx` uses for setup. Adapt fixture types to whatever helpers exist.

- [ ] **Step 2: Run tests to verify they fail**

```bash
cd webapp && npx vitest run src/pages/__tests__/Dashboard.test.tsx -t "Dashboard with Auto"
```

Expected: FAIL — Dashboard wiring not yet updated.

- [ ] **Step 3: Wire `useEffectiveCloudSelection` and remove migration aid**

Open `webapp/src/pages/Dashboard.tsx`.

3a. Add import at the top:

```ts
import { AUTO_TUNNEL_SENTINEL, AUTO_TUNNEL_DOMAIN, useEffectiveCloudSelection, isAutoSelection } from '../stores/connection.store';
```

3b. Find the `manualSelectedDomain` derivation (line 224):

```ts
const manualSelectedDomain = activeTunnel?.source === 'cloud' ? activeTunnel.domain : null;
```

Replace with:

```ts
const effectiveCloudSelection = useEffectiveCloudSelection();
const manualSelectedDomain =
  effectiveCloudSelection === null
    ? null
    : isAutoSelection(effectiveCloudSelection)
      ? AUTO_TUNNEL_DOMAIN
      : effectiveCloudSelection.domain;
```

3c. Find the `onSelect` callback wired into `CloudTunnelList` (search for `onSelect=` in the render). The callback currently calls `selectCloudTunnel(tunnel)`. Update it to dispatch on the Auto sentinel:

```ts
const handleCloudTunnelSelect = useCallback((tunnel: Tunnel, _ech?: string) => {
  if (isAutoSelection(tunnel)) {
    useConnectionStore.getState().clearCloudSelection();
  } else {
    useConnectionStore.getState().selectCloudTunnel(tunnel);
  }
  // Preserve any existing post-select side effects (e.g., activeTunnel cmp); copy from current handler.
}, []);
```

If the existing handler does more than `selectCloudTunnel(tunnel)` (e.g., reads `activeTunnel`, fires telemetry), preserve those side effects in both branches as appropriate.

3d. Delete the migration-aid effect (lines 194-216 in the current file):

```ts
// Migration aid: users whose persisted serverMode was 'smart' land on
// 'manual' without a selectedCloudTunnel — auto-select the first sorted
// tunnel ...
const serverModeLoaded = useConnectionStore((s) => s.serverModeLoaded);
const selectedCloudTunnel = useConnectionStore((s) => s.selectedCloudTunnel);

useEffect(() => {
  if (!serverModeLoaded) return;
  if (serverMode !== 'manual') return;
  if (selectedCloudTunnel) return;
  if (cloudTunnels.length === 0) return;

  const sorted = [...cloudTunnels].sort((a, b) =>
    a.node.country.localeCompare(b.node.country)
  );
  const first = sorted[0];
  if (first) {
    console.info('[Dashboard] auto-select first tunnel for migrating user:', first.domain);
    selectCloudTunnel(first);
  }
}, [serverModeLoaded, serverMode, selectedCloudTunnel, cloudTunnels, selectCloudTunnel]);
```

Delete the entire block (comment + `useEffect`). Auto-as-default supersedes it.

After deleting, check whether `serverModeLoaded` is referenced elsewhere in the file. If only the migration aid used it, that derived store read line can also be removed; if used elsewhere, keep it.

3e. Update top "current server" card. Search the file for the current server card rendering (look for usage of `displayTunnel` or `connectedTunnel` near top-of-page rendering). Locate where the country/name is displayed. Wrap it with an Auto-aware branch:

```tsx
{isAutoSelection(effectiveCloudSelection) ? (
  <Box>
    <Typography variant="subtitle1" fontWeight={600}>
      ⚡ {t('dashboard:auto.title')}
    </Typography>
    <Typography variant="body2" color="text.secondary">
      {connectedTunnel
        ? t('dashboard:auto.connected', {
            country: connectedTunnel.country || '',
            name: connectedTunnel.name || connectedTunnel.domain,
          })
        : t('dashboard:auto.notConnected')}
    </Typography>
  </Box>
) : (
  /* existing concrete tunnel rendering */
)}
```

Adapt the exact JSX to match the existing card layout (font sizes, gaps, etc.).

3f. Wire `reconcileSelection` into the tunnel-loaded callback. Find `handleTunnelsLoaded` (line 188-192):

```ts
const handleTunnelsLoaded = useCallback((tunnels: Tunnel[]) => {
  setCloudTunnels(tunnels);
  enrichFromTunnelList(tunnels);
}, [enrichFromTunnelList]);
```

Add the reconcile call:

```ts
const handleTunnelsLoaded = useCallback((tunnels: Tunnel[]) => {
  setCloudTunnels(tunnels);
  enrichFromTunnelList(tunnels);
  useConnectionStore.getState().reconcileSelection(tunnels);
}, [enrichFromTunnelList]);
```

Also wire it into the warm-start cache enrichment effect at lines 107-114:

```ts
useEffect(() => {
  if (connectedTunnel?.source === 'cloud' && !connectedTunnel.country) {
    const cached = cacheStore.get<TunnelListResponse>('api:tunnels');
    if (cached?.items) {
      enrichFromTunnelList(cached.items);
      useConnectionStore.getState().reconcileSelection(cached.items);
    }
  }
}, [connectedTunnel, enrichFromTunnelList]);
```

- [ ] **Step 4: Run tests**

```bash
cd webapp && npx vitest run src/pages/__tests__/Dashboard.test.tsx
```

Expected: PASS — new Auto cases green, existing cases regress-free.

- [ ] **Step 5: Type-check**

```bash
cd webapp && npx tsc --noEmit
```

Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add webapp/src/pages/Dashboard.tsx webapp/src/pages/__tests__/Dashboard.test.tsx
git commit -m "feat(webapp): wire Dashboard to Auto selection (top card + reconcile)"
```

---

## Task 9: i18n keys (zh-CN first, then 6 locales)

**Files:**
- Modify: `webapp/src/i18n/locales/zh-CN/dashboard.json`
- Modify: `webapp/src/i18n/locales/en-US/dashboard.json`
- Modify: `webapp/src/i18n/locales/ja/dashboard.json`
- Modify: `webapp/src/i18n/locales/zh-TW/dashboard.json`
- Modify: `webapp/src/i18n/locales/zh-HK/dashboard.json`
- Modify: `webapp/src/i18n/locales/en-AU/dashboard.json`
- Modify: `webapp/src/i18n/locales/en-GB/dashboard.json`

- [ ] **Step 1: Add zh-CN keys**

Open `webapp/src/i18n/locales/zh-CN/dashboard.json`. Inside the existing `"dashboard": { ... }` object, add a sibling `"auto"` object:

```json
"auto": {
  "title": "自动选择",
  "subtitle": "在最优三个节点中随机选择",
  "connected": "当前命中：{{country}} · {{name}}",
  "notConnected": "未连接",
  "noTunnelAvailable": "暂无可用节点"
}
```

Place under the top-level `"dashboard"` namespace, sibling to `"dashboard"` keys (i.e., access via `t('dashboard:auto.title')` from components).

- [ ] **Step 2: Add en-US translation**

```json
"auto": {
  "title": "Auto",
  "subtitle": "Random pick from top 3 servers",
  "connected": "Connected: {{country}} · {{name}}",
  "notConnected": "Not connected",
  "noTunnelAvailable": "No server available"
}
```

- [ ] **Step 3: Add ja translation**

```json
"auto": {
  "title": "自動選択",
  "subtitle": "上位3つのサーバーからランダムに選択",
  "connected": "接続中: {{country}} · {{name}}",
  "notConnected": "未接続",
  "noTunnelAvailable": "利用可能なサーバーがありません"
}
```

- [ ] **Step 4: Add zh-TW translation**

```json
"auto": {
  "title": "自動選擇",
  "subtitle": "在最佳三個節點中隨機選擇",
  "connected": "當前命中：{{country}} · {{name}}",
  "notConnected": "未連接",
  "noTunnelAvailable": "暫無可用節點"
}
```

- [ ] **Step 5: Add zh-HK translation**

```json
"auto": {
  "title": "自動選擇",
  "subtitle": "喺最佳三個節點中隨機選擇",
  "connected": "當前命中：{{country}} · {{name}}",
  "notConnected": "未連接",
  "noTunnelAvailable": "暫無可用節點"
}
```

- [ ] **Step 6: Add en-AU translation**

```json
"auto": {
  "title": "Auto",
  "subtitle": "Random pick from top 3 servers",
  "connected": "Connected: {{country}} · {{name}}",
  "notConnected": "Not connected",
  "noTunnelAvailable": "No server available"
}
```

- [ ] **Step 7: Add en-GB translation**

```json
"auto": {
  "title": "Auto",
  "subtitle": "Random pick from top 3 servers",
  "connected": "Connected: {{country}} · {{name}}",
  "notConnected": "Not connected",
  "noTunnelAvailable": "No server available"
}
```

- [ ] **Step 8: Verify JSON parses for all 7 files**

```bash
cd webapp && for loc in zh-CN en-US ja zh-TW zh-HK en-AU en-GB; do
  python3 -c "import json; json.load(open('src/i18n/locales/$loc/dashboard.json'))" && echo "$loc OK"
done
```

Expected: 7× `<locale> OK` lines.

- [ ] **Step 9: Run dashboard tests to confirm i18n keys resolve**

```bash
cd webapp && npx vitest run src/pages/__tests__/Dashboard.test.tsx
```

Expected: PASS.

- [ ] **Step 10: Commit**

```bash
git add webapp/src/i18n/locales/*/dashboard.json
git commit -m "i18n(webapp): add dashboard.auto.* keys (7 locales)"
```

---

## Task 10: Map Auto error code to user-facing message

**Files:**
- Modify: `webapp/src/utils/errorCode.ts`

- [ ] **Step 1: Inspect existing error code mapping**

```bash
grep -n "code === 400\|case 400\|getErrorMessage" /Users/david/projects/kaitu-io/k2app/webapp/src/utils/errorCode.ts | head
```

The codebase already maps generic `400` errors. The Auto-no-tunnel case dispatches `BACKEND_ERROR` with `{ code: 400, message: 'No tunnel available for auto pick' }`. Decide:

- If existing 400 handling shows a generic "Bad request" message and the engineer wants an Auto-specific message, add a sub-key check on `message` content OR introduce a new client-side code per webapp/CLAUDE.md ranges (`500-579: Frontend-only VPN/action/API errors`).

Pick the simpler path: introduce a client-only code `570` (or next free 5xx range slot per `webapp/CLAUDE.md` "Code ranges") and dispatch with that code instead of 400 from `connect()`.

- [ ] **Step 2: If introducing a new code, update the dispatch**

Open `webapp/src/stores/connection.store.ts`. Change the Auto-no-tunnel `BACKEND_ERROR` dispatch (added in Task 6):

```ts
vpnDispatch('BACKEND_ERROR', {
  error: { code: 570, message: 'No tunnel available for auto pick' },
  isRetrying: false,
});
```

(Use whatever frontend-only code is free — check `errorCode.ts` `ERROR_CODES` enum for the next available slot in 500-579 not already used. If 570 is taken, use the next free.)

- [ ] **Step 3: Add the error code constant + i18n mapping**

Open `webapp/src/utils/errorCode.ts`. In `ERROR_CODES` enum:

```ts
NO_TUNNEL_AVAILABLE_AUTO = 570, // adjust if 570 is taken
```

In `getErrorMessage()`:

```ts
case ERROR_CODES.NO_TUNNEL_AVAILABLE_AUTO:
  return t('dashboard:auto.noTunnelAvailable');
```

- [ ] **Step 4: Type-check + run tests**

```bash
cd webapp && npx tsc --noEmit && npx vitest run
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add webapp/src/utils/errorCode.ts webapp/src/stores/connection.store.ts
git commit -m "feat(webapp): map Auto-no-tunnel error to dashboard:auto.noTunnelAvailable"
```

---

## Task 11: End-to-end smoke test (manual — run locally)

**Files:** none (manual verification)

- [ ] **Step 1: Run the standalone dev server**

```bash
cd /Users/david/projects/kaitu-io/k2app && make dev-standalone
```

Wait for the dev server to print its URL.

- [ ] **Step 2: Open in browser, confirm Auto is default selected**

Navigate to `http://localhost:5173` (or whatever port make dev-standalone prints). Log in if prompted.

In the cloud server list:
- The first row should be the Auto virtual row (⚡ icon, "自动选择" / "Auto" label)
- Its radio should be checked
- Top "current server" card should show "⚡ 自动选择" + "未连接"

- [ ] **Step 3: Click Connect**

Verify in browser DevTools console:
- A log line `[Connection] auto-pick → <domain> (score=<n>)`
- VPN state machine transitions to `connecting` → `connected`
- Top card updates to "⚡ 自动选择" + "当前命中：<country> · <name>"
- The actual connected tunnel matches the auto-pick log

- [ ] **Step 4: Disconnect, click Connect again**

Verify:
- Possibly a different tunnel is picked (top-3 random — may collide; run a few times)
- Each connect emits a fresh `auto-pick →` log

- [ ] **Step 5: Click a concrete tunnel row, then click Connect**

Verify:
- Selection moves to the concrete row (Auto row no longer checked)
- `[Connection] auto-pick →` log does NOT appear
- Top card shows the concrete tunnel directly (no "Auto" wrapper)

- [ ] **Step 6: Click the Auto row from a concrete selection**

Verify:
- Selection moves back to Auto
- `[Connection] clearCloudSelection (→ Auto via derivation)` log appears

- [ ] **Step 7: Smoke pass complete**

If all 6 prior verifications pass, mark this task complete. If any fails, file a follow-up task with reproduction steps and fix before merging.

- [ ] **Step 8: Final type-check + full test sweep**

```bash
cd webapp && npx tsc --noEmit && npx vitest run
```

Expected: PASS — clean tsc, all tests green.

---

## Self-Review

**Spec coverage:**
- Decision 1 (virtual row form factor): Task 7 ✓
- Decision 2 (re-pick on every connect, stable in session): Task 6 + smoke step 4 ✓
- Decision 3 (top card double display + ordinary list selected style): Task 8 step 3e + 7 ✓
- Decision 4 (no special handling of 0.5): Task 1 test "treats 0.5 entries as ordinary scores" ✓
- Decision 5 (drop score=0, fallback when zero-filtered empty): Task 1 tests ✓
- Decision 6 (no persistence; default = null = Auto): Task 3 (selector derives Auto from null) + Task 8 step 3d (delete migration aid) ✓
- Decision 7 (all platforms): pure webapp, automatic ✓
- Decision 8 (sentinel + reference equality): Task 2 ✓
- Cold-start enrichment fix: Task 5 ✓
- Stale-selection auto-fallback (`reconcileSelection`): Task 4 (impl) + Task 8 step 3f (wire) ✓

All spec sections have a task.

**Placeholder scan:** No "TBD", "TODO", "implement later", or vague "add error handling" steps. Each step has either exact code, exact commands, or exact verification criteria.

**Type consistency:** `AUTO_TUNNEL_SENTINEL`, `AUTO_TUNNEL_DOMAIN`, `isAutoSelection`, `useEffectiveCloudSelection`, `clearCloudSelection`, `reconcileSelection`, `pickAutoTunnel` — all referenced consistently across Tasks 1-10. The connection store interface gains `clearCloudSelection: () => void` and `reconcileSelection: (tunnels: Tunnel[]) => void`; Task 4 adds both.
