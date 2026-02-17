# Mobile Webapp Bridge v2 — Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Wire Capacitor mobile apps into the split globals architecture by creating a bridge adapter that wraps K2Plugin methods into `window._k2` / `window._platform` interfaces, fix the native state mapping bug, and enable Dashboard to pass config on connect.

**Architecture:** Capacitor bridge (`capacitor-k2.ts`) detects native platform via `Capacitor.isNativePlatform()`, wraps K2Plugin's separate methods into `IK2Vpn.run()` dispatch, maps gomobile's minimal status to webapp's `StatusResponseData`, and injects `_platform` with mobile capabilities. Native-side fix removes the invalid "disconnected"→"stopped" state remap. Dashboard assembles minimal `ClientConfig` for `run('up', config)`.

**Tech Stack:** TypeScript (React 18, @capacitor/core, K2Plugin), Swift (K2Plugin.swift), Kotlin (K2Plugin.kt), Vitest

**Spec**: `docs/features/mobile-webapp-bridge.md` (v2)
**Complexity**: Moderate (8-10 files, new TS module + native fixes + Dashboard change)
**Strategy**: Single branch `feat/mobile-webapp-bridge-v2`, no worktrees (sequential deps, shared entry points)

---

## Task Dependency Graph

```
T1 (Fix native state passthrough) ─┐
                                     ├── T4 (Verification)
T2 (Capacitor webapp bridge) ───────┤
                                     │
T3 (Dashboard config assembly) ─────┘
     └── depends on T2
```

T1 and T2 are parallel (different file sets — native vs webapp). T3 depends on T2. T4 depends on all.

---

## T1: Fix native state passthrough

**Branch**: `feat/mobile-webapp-bridge-v2`
**Depends on**: (none)
**Files**:
- MODIFY: `mobile/ios/Plugin/K2Plugin.swift` — remove "stopped" state mapping
- MODIFY: `mobile/plugins/k2-plugin/android/src/main/java/io/kaitu/k2plugin/K2Plugin.kt` — remove "stopped" state mapping

### Context

Both K2Plugin.swift and K2Plugin.kt remap Go's `"disconnected"` to `"stopped"` in two places:
1. `remapStatusKeys()` — transforms `getStatus()` response
2. `mapVPNStatus()` / `onStateChange()` — transforms event state values

The webapp's `ServiceState` type is `'disconnected' | 'connecting' | 'connected' | 'reconnecting' | 'disconnecting' | 'error'`. The value `"stopped"` is not valid and causes all derived booleans in `vpn.store.ts` to be false (broken UI state).

The fix: remove the state remapping entirely. Pass Go engine states through unchanged.

### RED

No automated test for native code (manual device verification). The webapp unit tests in T2 will validate that the bridge handles `"disconnected"` state correctly.

### GREEN

**Step 1: Fix K2Plugin.swift — `remapStatusKeys()`**

In `mobile/ios/Plugin/K2Plugin.swift`, find the `remapStatusKeys()` function (around line 382-397). Remove the `"disconnected"→"stopped"` mapping:

```swift
private func remapStatusKeys(_ json: [String: Any]) -> [String: Any] {
    let keyMap: [String: String] = [
        "connected_at": "connectedAt",
        "uptime_seconds": "uptimeSeconds",
    ]
    var result: [String: Any] = [:]
    for (key, value) in json {
        let newKey = keyMap[key] ?? key
        result[newKey] = value
    }
    // REMOVED: "disconnected"→"stopped" remapping
    // Go engine states pass through directly to match webapp ServiceState
    return result
}
```

**Step 2: Fix K2Plugin.swift — `mapVPNStatus()`**

Find `mapVPNStatus()` (around line 399-405). Change the default case to return `"disconnected"` instead of `"stopped"`:

```swift
private func mapVPNStatus(_ status: NEVPNStatus) -> String {
    switch status {
    case .connected: return "connected"
    case .connecting, .reasserting: return "connecting"
    default: return "disconnected"
    }
}
```

**Step 3: Fix K2Plugin.kt — `remapStatusKeys()`**

In `mobile/plugins/k2-plugin/android/src/main/java/io/kaitu/k2plugin/K2Plugin.kt`, find `remapStatusKeys()` (around line 102-118). Remove the `"disconnected"→"stopped"` mapping:

```kotlin
private fun remapStatusKeys(obj: JSObject): JSObject {
    val keyMap = mapOf(
        "connected_at" to "connectedAt",
        "uptime_seconds" to "uptimeSeconds",
    )
    val result = JSObject()
    val keys = obj.keys()
    while (keys.hasNext()) {
        val key = keys.next()
        val newKey = keyMap[key] ?: key
        result.put(newKey, obj.get(key))
    }
    // REMOVED: "disconnected"→"stopped" remapping
    // Go engine states pass through directly to match webapp ServiceState
    return result
}
```

**Step 4: Fix K2Plugin.kt — `onStateChange()`**

Find `onStateChange()` (around line 195-200). Remove the mapping:

```kotlin
fun onStateChange(state: String) {
    // Pass through Go engine state directly — matches webapp ServiceState
    val data = JSObject()
    data.put("state", state)
    notifyListeners("vpnStateChange", data)
}
```

### REFACTOR

1. `[MUST]` Search entire K2Plugin.swift and K2Plugin.kt for any remaining `"stopped"` string — should find zero occurrences
2. `[SHOULD]` Update inline comments to explain why states pass through unchanged

---

## T2: Capacitor webapp bridge

**Branch**: `feat/mobile-webapp-bridge-v2`
**Depends on**: (none — parallel with T1)
**Files**:
- NEW: `webapp/src/services/capacitor-k2.ts` — Capacitor bridge adapter
- MODIFY: `webapp/src/main.tsx` — add Capacitor detection branch
- NEW: `webapp/src/services/__tests__/capacitor-k2.test.ts` — unit tests

### Context

Following the `tauri-k2.ts` pattern: detect platform → import bridge → inject globals.

The Capacitor bridge wraps K2Plugin's separate methods into `IK2Vpn.run(action, params)`:
- `run('up', config)` → `K2Plugin.connect({config: JSON.stringify(config)})`
- `run('down')` → `K2Plugin.disconnect()`
- `run('status')` → `K2Plugin.getStatus()` → transform to `StatusResponseData`
- `run('version')` → `K2Plugin.getVersion()`

K2Plugin detection: use `@capacitor/core` Capacitor global's `isNativePlatform()`.

The bridge also registers K2Plugin event listeners (`vpnStateChange`, `vpnError`) that trigger an immediate status re-poll in the VPN store for faster state updates.

### RED

1. **`test_injectCapacitorGlobals_sets_k2`** — After injection, `window._k2` is defined with `run` method
2. **`test_injectCapacitorGlobals_sets_platform`** — After injection, `_platform.os` is `'ios'`, `isMobile` is `true`, `isDesktop` is `false`
3. **`test_k2_run_status_returns_StatusResponseData`** — `_k2.run('status')` returns `SResponse<StatusResponseData>` with `running`, `networkAvailable`, `state` fields
4. **`test_k2_run_up_calls_connect_with_config`** — `_k2.run('up', {server: 'k2v5://...'})` calls `K2Plugin.connect({config: '...'})`
5. **`test_k2_run_down_calls_disconnect`** — `_k2.run('down')` calls `K2Plugin.disconnect()`
6. **`test_k2_run_version_returns_version`** — `_k2.run('version')` calls `K2Plugin.getVersion()` and wraps result
7. **`test_k2_run_up_without_config_returns_error`** — `_k2.run('up')` without params returns error code
8. **`test_platform_getUdid`** — `_platform.getUdid()` calls `K2Plugin.getUDID()`
9. **`test_getK2Source_returns_capacitor`** — After injection, `getK2Source()` returns `'capacitor'`
10. **`test_standalone_still_works`** — Without Capacitor, standalone fallback works (regression)

### GREEN

**Step 1: Create `webapp/src/services/capacitor-k2.ts`**

```typescript
/**
 * Capacitor Mobile Bridge
 *
 * Injects window._k2 (VPN control via K2Plugin) and window._platform (mobile capabilities)
 * when running inside a Capacitor native shell (iOS/Android).
 *
 * Detection: Capacitor.isNativePlatform() from @capacitor/core.
 * Pattern: Same as tauri-k2.ts — detect → import → inject globals.
 */

import { Capacitor } from '@capacitor/core';
import { K2Plugin } from 'k2-plugin';
import type { IK2Vpn, IPlatform, SResponse } from '../types/kaitu-core';
import type { StatusResponseData, ControlError } from '../services/control-types';
import { webSecureStorage } from './secure-storage';

/**
 * Check if running in Capacitor native environment.
 */
export function isCapacitorNative(): boolean {
  return Capacitor.isNativePlatform();
}

/**
 * Transform K2Plugin.getStatus() response to webapp's StatusResponseData.
 * K2Plugin returns minimal fields; we fill in required defaults.
 */
function mapStatus(raw: {
  state: string;
  connectedAt?: string;
  uptimeSeconds?: number;
  error?: string;
}): StatusResponseData {
  const state = raw.state as StatusResponseData['state'];
  const isRunning = state === 'connecting' || state === 'connected' || state === 'reconnecting';

  let startAt: number | undefined;
  if (raw.connectedAt) {
    startAt = Math.floor(new Date(raw.connectedAt).getTime() / 1000);
  }

  let error: ControlError | undefined;
  if (raw.error) {
    error = { code: 570, message: raw.error };
  }

  return {
    state,
    running: isRunning,
    networkAvailable: true,
    startAt,
    error,
    retrying: false,
  };
}

/**
 * Inject Capacitor-specific _k2 and _platform globals.
 * Must be called before store initialization.
 */
export async function injectCapacitorGlobals(): Promise<void> {
  const platform = Capacitor.getPlatform(); // 'ios' | 'android'

  // Get app version from K2Plugin
  let version = 'unknown';
  try {
    const readyResult = await K2Plugin.checkReady();
    version = readyResult.version ?? version;
  } catch {
    console.warn('[K2:Capacitor] checkReady failed, using default version');
  }

  // Inject _k2: VPN control via K2Plugin
  const capacitorK2: IK2Vpn = {
    run: async <T = any>(action: string, params?: any): Promise<SResponse<T>> => {
      try {
        switch (action) {
          case 'up': {
            if (!params) {
              return {
                code: -1,
                message: 'Config required for mobile connect',
              };
            }
            const configJSON = typeof params === 'string' ? params : JSON.stringify(params);
            await K2Plugin.connect({ config: configJSON });
            return { code: 0, message: 'ok' };
          }

          case 'down': {
            await K2Plugin.disconnect();
            return { code: 0, message: 'ok' };
          }

          case 'status': {
            const raw = await K2Plugin.getStatus();
            const data = mapStatus(raw);
            return { code: 0, message: 'ok', data: data as T };
          }

          case 'version': {
            const info = await K2Plugin.getVersion();
            return { code: 0, message: 'ok', data: info as T };
          }

          default:
            return { code: -1, message: `Unknown action: ${action}` };
        }
      } catch (err) {
        return {
          code: -1,
          message: err instanceof Error ? err.message : String(err),
        };
      }
    },
  };

  // Inject _platform: mobile capabilities
  const capacitorPlatform: IPlatform = {
    os: platform as IPlatform['os'],
    isDesktop: false,
    isMobile: true,
    version,

    storage: webSecureStorage,

    getUdid: async (): Promise<string> => {
      const result = await K2Plugin.getUDID();
      return result.udid;
    },

    writeClipboard: async (text: string): Promise<void> => {
      if (navigator.clipboard) {
        await navigator.clipboard.writeText(text);
      }
    },

    readClipboard: async (): Promise<string> => {
      if (navigator.clipboard) {
        return navigator.clipboard.readText();
      }
      return '';
    },

    debug: (message: string) => console.debug('[K2:Capacitor]', message),
    warn: (message: string) => console.warn('[K2:Capacitor]', message),
  };

  (window as any)._k2 = capacitorK2;
  (window as any)._platform = capacitorPlatform;

  // Register K2Plugin event listeners for faster state updates
  setupEventListeners();

  console.info(`[K2:Capacitor] Injected - os=${platform}, version=${version}`);
}

/**
 * Register K2Plugin event listeners.
 * Events supplement the 2s polling in vpn.store — they trigger immediate re-poll.
 */
function setupEventListeners(): void {
  K2Plugin.addListener('vpnStateChange', (data) => {
    console.debug('[K2:Capacitor] vpnStateChange:', data.state);
    // The vpn.store polls every 2s via _k2.run('status').
    // Event just logs — poll picks up the change within 2s.
    // A future optimization could trigger an immediate poll here.
  });

  K2Plugin.addListener('vpnError', (data) => {
    console.warn('[K2:Capacitor] vpnError:', data.message);
  });
}
```

**Step 2: Update `webapp/src/main.tsx` — add Capacitor detection**

Replace the platform detection block (lines 27-37):

```typescript
  // Inject platform-specific globals
  if (window.__TAURI__) {
    console.info('[WebApp] Tauri detected, injecting Tauri bridge...');
    const { injectTauriGlobals } = await import('./services/tauri-k2');
    await injectTauriGlobals();
  } else {
    // Check for Capacitor native (must be before standalone check)
    const { Capacitor } = await import('@capacitor/core');
    if (Capacitor.isNativePlatform()) {
      console.info('[WebApp] Capacitor native detected, injecting Capacitor bridge...');
      const { injectCapacitorGlobals } = await import('./services/capacitor-k2');
      await injectCapacitorGlobals();
    } else if (!window._k2 || !window._platform) {
      console.warn('[WebApp] Globals missing, injecting standalone implementation...');
      const { ensureK2Injected } = await import('./services/standalone-k2');
      ensureK2Injected();
    } else {
      console.info('[WebApp] K2 and platform already injected by host');
    }
  }
```

**Step 3: Write tests — `webapp/src/services/__tests__/capacitor-k2.test.ts`**

```typescript
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';

// Mock @capacitor/core
vi.mock('@capacitor/core', () => ({
  Capacitor: {
    isNativePlatform: vi.fn(() => true),
    getPlatform: vi.fn(() => 'ios'),
  },
}));

// Mock k2-plugin
const mockK2Plugin = {
  checkReady: vi.fn(),
  getUDID: vi.fn(),
  getVersion: vi.fn(),
  getStatus: vi.fn(),
  getConfig: vi.fn(),
  connect: vi.fn(),
  disconnect: vi.fn(),
  addListener: vi.fn(),
  checkWebUpdate: vi.fn(),
  checkNativeUpdate: vi.fn(),
  applyWebUpdate: vi.fn(),
  downloadNativeUpdate: vi.fn(),
  installNativeUpdate: vi.fn(),
};

vi.mock('k2-plugin', () => ({
  K2Plugin: mockK2Plugin,
}));

import { injectCapacitorGlobals, isCapacitorNative } from '../capacitor-k2';
import { getK2Source } from '../standalone-k2';

describe('capacitor-k2', () => {
  let originalK2: any;
  let originalPlatform: any;

  beforeEach(() => {
    originalK2 = window._k2;
    originalPlatform = window._platform;
    delete (window as any)._k2;
    delete (window as any)._platform;
    vi.clearAllMocks();

    // Default mock implementations
    mockK2Plugin.checkReady.mockResolvedValue({ ready: true, version: '0.4.0' });
    mockK2Plugin.addListener.mockResolvedValue({ remove: vi.fn() });
  });

  afterEach(() => {
    (window as any)._k2 = originalK2;
    (window as any)._platform = originalPlatform;
  });

  describe('isCapacitorNative', () => {
    it('returns true in native environment', () => {
      expect(isCapacitorNative()).toBe(true);
    });
  });

  describe('injectCapacitorGlobals', () => {
    beforeEach(async () => {
      await injectCapacitorGlobals();
    });

    it('sets window._k2 with run method', () => {
      expect(window._k2).toBeDefined();
      expect(typeof window._k2.run).toBe('function');
    });

    it('sets window._platform with correct mobile properties', () => {
      expect(window._platform).toBeDefined();
      expect(window._platform.os).toBe('ios');
      expect(window._platform.isDesktop).toBe(false);
      expect(window._platform.isMobile).toBe(true);
      expect(window._platform.version).toBe('0.4.0');
    });

    it('registers event listeners', () => {
      expect(mockK2Plugin.addListener).toHaveBeenCalledWith('vpnStateChange', expect.any(Function));
      expect(mockK2Plugin.addListener).toHaveBeenCalledWith('vpnError', expect.any(Function));
    });
  });

  describe('_k2.run dispatch', () => {
    beforeEach(async () => {
      await injectCapacitorGlobals();
    });

    it('run("status") returns StatusResponseData format', async () => {
      mockK2Plugin.getStatus.mockResolvedValueOnce({
        state: 'connected',
        connectedAt: '2026-02-17T12:00:00Z',
        uptimeSeconds: 3600,
      });

      const result = await window._k2.run('status');

      expect(result.code).toBe(0);
      expect(result.data).toMatchObject({
        state: 'connected',
        running: true,
        networkAvailable: true,
        retrying: false,
      });
      expect(result.data.startAt).toBeGreaterThan(0);
    });

    it('run("status") with disconnected state', async () => {
      mockK2Plugin.getStatus.mockResolvedValueOnce({
        state: 'disconnected',
      });

      const result = await window._k2.run('status');

      expect(result.data).toMatchObject({
        state: 'disconnected',
        running: false,
        networkAvailable: true,
      });
    });

    it('run("status") with error maps to ControlError', async () => {
      mockK2Plugin.getStatus.mockResolvedValueOnce({
        state: 'disconnected',
        error: 'connection refused',
      });

      const result = await window._k2.run('status');

      expect(result.data.error).toEqual({
        code: 570,
        message: 'connection refused',
      });
    });

    it('run("up", config) calls K2Plugin.connect with JSON config', async () => {
      mockK2Plugin.connect.mockResolvedValueOnce(undefined);
      const config = { server: 'k2v5://test@host:443' };

      const result = await window._k2.run('up', config);

      expect(mockK2Plugin.connect).toHaveBeenCalledWith({
        config: JSON.stringify(config),
      });
      expect(result.code).toBe(0);
    });

    it('run("up") without config returns error', async () => {
      const result = await window._k2.run('up');

      expect(result.code).toBe(-1);
      expect(result.message).toContain('Config required');
      expect(mockK2Plugin.connect).not.toHaveBeenCalled();
    });

    it('run("down") calls K2Plugin.disconnect', async () => {
      mockK2Plugin.disconnect.mockResolvedValueOnce(undefined);

      const result = await window._k2.run('down');

      expect(mockK2Plugin.disconnect).toHaveBeenCalled();
      expect(result.code).toBe(0);
    });

    it('run("version") calls K2Plugin.getVersion and wraps result', async () => {
      mockK2Plugin.getVersion.mockResolvedValueOnce({
        version: '0.4.0',
        go: 'embedded',
        os: 'ios',
        arch: 'arm64',
      });

      const result = await window._k2.run('version');

      expect(result.code).toBe(0);
      expect(result.data).toMatchObject({ version: '0.4.0', os: 'ios' });
    });

    it('run returns error on plugin exception', async () => {
      mockK2Plugin.getStatus.mockRejectedValueOnce(new Error('Plugin unavailable'));

      const result = await window._k2.run('status');

      expect(result.code).toBe(-1);
      expect(result.message).toContain('Plugin unavailable');
    });
  });

  describe('_platform', () => {
    beforeEach(async () => {
      await injectCapacitorGlobals();
    });

    it('getUdid calls K2Plugin.getUDID', async () => {
      mockK2Plugin.getUDID.mockResolvedValueOnce({ udid: 'device-123' });

      const udid = await window._platform.getUdid();

      expect(mockK2Plugin.getUDID).toHaveBeenCalled();
      expect(udid).toBe('device-123');
    });
  });

  describe('getK2Source', () => {
    it('returns capacitor after Capacitor injection', async () => {
      await injectCapacitorGlobals();
      expect(getK2Source()).toBe('capacitor');
    });
  });
});

describe('standalone fallback (regression)', () => {
  let originalK2: any;
  let originalPlatform: any;

  beforeEach(() => {
    originalK2 = window._k2;
    originalPlatform = window._platform;
    delete (window as any)._k2;
    delete (window as any)._platform;
    delete (window as any).__TAURI__;
  });

  afterEach(() => {
    (window as any)._k2 = originalK2;
    (window as any)._platform = originalPlatform;
  });

  it('standalone injection still works without Capacitor', async () => {
    const { ensureK2Injected, getK2Source } = await import('../standalone-k2');
    ensureK2Injected();

    expect(window._k2).toBeDefined();
    expect(window._platform).toBeDefined();
    expect(getK2Source()).toBe('standalone');
  });
});
```

**Step 4: Run tests**

Run: `cd webapp && npx vitest run src/services/__tests__/capacitor-k2.test.ts --reporter=verbose`
Expected: all tests pass

**Step 5: Run full test suite**

Run: `cd webapp && npx vitest run`
Expected: all existing tests pass (no regressions)

**Step 6: Type check**

Run: `cd webapp && npx tsc --noEmit`
Expected: no new type errors

### REFACTOR

1. `[MUST]` Full `vitest run` passes — no regressions
2. `[MUST]` `tsc --noEmit` passes
3. `[SHOULD]` `capacitor-k2.ts` doesn't import from `standalone-k2.ts` or `tauri-k2.ts` (no circular deps)
4. `[SHOULD]` `setupEventListeners()` is not exported (internal implementation detail)
5. `[SHOULD]` `mapStatus()` is not exported (internal, tested indirectly through `run('status')`)

---

## T3: Dashboard config assembly

**Branch**: `feat/mobile-webapp-bridge-v2`
**Depends on**: T2
**Files**:
- MODIFY: `webapp/src/pages/Dashboard.tsx` — assemble config and pass to `_k2.run('up', config)`

### Context

Currently Dashboard calls `window._k2.run('up')` with no params (line 227). On desktop, the daemon has persisted state so this works. On mobile, the Capacitor bridge requires explicit config (T2 returns error for `run('up')` without params).

The Dashboard already has UI state for:
- `selectedCloudTunnel: Tunnel | null` — has `.url` field with wire URL
- `activeRuleType: string` — `'global'` or `'chnroute'`

We need to assemble a minimal `config.ClientConfig` JSON from these values.

Go's `config.SetDefaults()` fills in DNS, proxy, log, listen defaults. The webapp only needs to provide:
- `server`: the wire URL (from selected tunnel)
- `rule.global`: boolean (from rule type selection)

### RED

No new test file — this is a small UI change. Covered by existing Dashboard tests + T4 manual verification.

### GREEN

**Step 1: Modify `handleToggleConnection` in `Dashboard.tsx`**

Find the `handleToggleConnection` callback (around line 212-233). Change the `run('up')` call to pass config:

```typescript
  const handleToggleConnection = useCallback(async () => {
    if (isDisconnected && !activeTunnelInfo.domain) {
      console.warn('[Dashboard] No tunnel selected');
      return;
    }

    try {
      if (!isDisconnected || isRetrying) {
        console.info('[Dashboard] Stopping VPN...');
        setOptimisticState('disconnecting');
        await window._k2.run('down');
      } else {
        console.info('[Dashboard] Starting VPN...');
        setOptimisticState('connecting');

        // Assemble minimal config for mobile + desktop
        const config: Record<string, any> = {};
        if (selectedCloudTunnel?.url) {
          config.server = selectedCloudTunnel.url;
        }
        config.rule = {
          global: activeRuleType === 'global',
        };

        await window._k2.run('up', config);
      }
    } catch (err) {
      console.error('Connection operation failed', err);
      setOptimisticState(null);
    }
  }, [isDisconnected, isRetrying, activeTunnelInfo.domain, selectedCloudTunnel, activeRuleType, setOptimisticState]);
```

Note: `selectedCloudTunnel` needs to be added to the dependency array.

**Step 2: Verify desktop compatibility**

On desktop, the daemon's `doUp()` accepts `config.ClientConfig` JSON via the `params` field. The Tauri bridge passes params through to `daemon_exec` IPC → Rust → `core_action()` which POSTs to the daemon. The daemon should accept the config in the request body.

If the daemon ignores the params field when it has persisted state, this change is backwards-compatible. If the daemon uses the provided config, it's an improvement (explicit config from UI).

**Step 3: Run existing tests**

Run: `cd webapp && npx vitest run src/pages --reporter=verbose`
Expected: all Dashboard tests pass

### REFACTOR

1. `[MUST]` Existing Dashboard tests pass
2. `[SHOULD]` Config assembly is inline (no separate function) — it's simple enough

---

## T4: Verification

**Branch**: `feat/mobile-webapp-bridge-v2`
**Depends on**: T1, T2, T3
**Files**: No new files — verification only

### Steps

1. **Type check**: `cd webapp && npx tsc --noEmit` — passes
2. **Webapp tests**: `cd webapp && npx vitest run` — all pass
3. **Build check**: `cd webapp && yarn build` — produces dist/
4. **Grep for "stopped"**: `grep -r '"stopped"' mobile/ios/Plugin/ mobile/plugins/k2-plugin/android/` — zero results (T1 verified)
5. **Manual smoke test** (if device available):
   - Build iOS: `cd mobile && npx cap sync ios && open ios/App/App.xcworkspace`
   - Build Android: `cd mobile && npx cap sync android && npx cap open android`
   - Verify in debug.html: K2Plugin.getStatus() returns `state: "disconnected"` (not "stopped")
   - Verify main app: server list loads, tunnel selection works, connect button triggers VPN

### Commit

Stage all changed files and commit with message:

```
feat: add Capacitor mobile bridge — K2Plugin adapter + globals injection + config assembly

Wire window._k2 through Capacitor K2Plugin adapter for mobile.
Inject window._platform with mobile capabilities (os, storage, UDID).
Fix native state mapping (remove invalid "stopped" state).
Dashboard now passes config to _k2.run('up', config).

Aligns mobile bridge with split globals architecture (v2).
```

---

## AC Coverage Matrix

| AC | Test | Task |
|----|------|------|
| AC1: `run('status')` returns StatusResponseData | `test_k2_run_status_returns_StatusResponseData` | T2 |
| AC2: `run('up', config)` calls K2Plugin.connect | `test_k2_run_up_calls_connect_with_config` | T2 |
| AC3: `run('down')` calls K2Plugin.disconnect | `test_k2_run_down_calls_disconnect` | T2 |
| AC4: `_platform.os` returns ios/android | `test_injectCapacitorGlobals_sets_platform` | T2 |
| AC5: `_platform.getUdid()` via K2Plugin | `test_platform_getUdid` | T2 |
| AC6: Native state passthrough (no "stopped") | Manual grep + webapp status test | T1, T4 |
| AC7: `getK2Source()` returns 'capacitor' | `test_getK2Source_returns_capacitor` | T2 |
| AC8: Standalone fallback regression | `test_standalone_still_works` | T2 |
| AC9: Dashboard passes config | Dashboard test + manual verification | T3, T4 |

---

## Execution Notes

- **yarn install from root** after any package.json changes (workspace requirement)
- T1 (native) and T2 (webapp) touch different file sets — safe to implement on same branch sequentially
- `k2-plugin` package resolution: the webapp imports `k2-plugin` which is registered via `mobile/plugins/k2-plugin/`. In the webapp test environment, it's mocked. In the built app, Capacitor resolves it at runtime via `registerPlugin`.
- The `@capacitor/core` import in `main.tsx` is tree-shaken in Tauri/web builds since it's behind the `isNativePlatform()` check which returns false outside Capacitor.
- If `@capacitor/core` is not yet in webapp's dependencies, add it: `cd webapp && yarn add @capacitor/core`
- After modifying K2Plugin.swift/kt source: `rm -rf node_modules/k2-plugin && yarn install --force` before `cap sync` (local plugin sync gotcha)
