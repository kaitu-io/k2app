# Random UDID Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace hardware-based UDID generation with random UUID + WebView localStorage storage to eliminate cross-device UDID collisions.

**Architecture:** New `device-udid.ts` module generates UUIDv4, stores via `_platform.storage`, returns SHA-256 hash (32 hex chars). All callers import from this module instead of `_platform.getUdid()`. Rust log_upload receives UDID as IPC parameter. Native UDID code deleted.

**Tech Stack:** TypeScript (webapp), Rust (desktop/src-tauri), vitest (tests)

**Spec:** `docs/plans/2026-03-22-random-udid-design.md`

**Dependency constraint:** Tasks 4+5 (Rust/mobile uploadLogs) MUST complete before Task 8 (native cleanup), because Task 8 deletes `get_hardware_uuid()` / `hashToUdid()` which the old uploadLogs code still calls.

---

### Task 1: Create `device-udid.ts` module + tests

**Files:**
- Create: `webapp/src/services/device-udid.ts`
- Create: `webapp/src/services/__tests__/device-udid.test.ts`

- [ ] **Step 1: Write the test file**

```typescript
// webapp/src/services/__tests__/device-udid.test.ts
import { describe, it, expect, vi, beforeEach } from 'vitest';

// Mock _platform.storage
const mockStorage = {
  get: vi.fn(),
  set: vi.fn(),
  remove: vi.fn(),
  getAll: vi.fn(),
  clear: vi.fn(),
};

// Must mock crypto.randomUUID since jsdom doesn't have it
const MOCK_UUID = 'a1b2c3d4-e5f6-4a7b-8c9d-0e1f2a3b4c5d';

beforeEach(() => {
  vi.clearAllMocks();
  (window as any)._platform = { storage: mockStorage };
  vi.stubGlobal('crypto', {
    randomUUID: () => MOCK_UUID,
    subtle: {
      digest: async (_algo: string, data: ArrayBuffer) => {
        // Simple deterministic mock: return data padded to 32 bytes
        const input = new Uint8Array(data);
        const result = new Uint8Array(32);
        for (let i = 0; i < 32; i++) {
          result[i] = input[i % input.length] ^ 0x42;
        }
        return result.buffer;
      },
    },
  });
});

// Must re-import to reset module-level cache between tests
async function freshImport() {
  vi.resetModules();
  return import('../device-udid');
}

describe('getDeviceUdid', () => {
  it('generates new UDID when storage is empty', async () => {
    mockStorage.get.mockResolvedValue(null);
    mockStorage.set.mockResolvedValue(undefined);
    mockStorage.remove.mockResolvedValue(undefined);

    const { getDeviceUdid } = await freshImport();
    const udid = await getDeviceUdid();

    // Should have stored the raw UUID
    expect(mockStorage.set).toHaveBeenCalledWith('device-udid', MOCK_UUID);
    // Should return 32 hex chars
    expect(udid).toMatch(/^[0-9a-f]{32}$/);
    // Should have cleared stale auth tokens (migration guard)
    expect(mockStorage.remove).toHaveBeenCalledWith('k2.auth.token');
    expect(mockStorage.remove).toHaveBeenCalledWith('k2.auth.refresh');
  });

  it('returns existing UDID from storage without generating', async () => {
    const EXISTING_UUID = 'existing-uuid-from-storage';
    mockStorage.get.mockResolvedValue(EXISTING_UUID);

    const { getDeviceUdid } = await freshImport();
    const udid = await getDeviceUdid();

    // Should NOT generate or store a new UUID
    expect(mockStorage.set).not.toHaveBeenCalled();
    // Should NOT clear tokens
    expect(mockStorage.remove).not.toHaveBeenCalled();
    // Should return 32 hex chars
    expect(udid).toMatch(/^[0-9a-f]{32}$/);
  });

  it('caches result on subsequent calls', async () => {
    mockStorage.get.mockResolvedValue('cached-test');

    const { getDeviceUdid } = await freshImport();
    const first = await getDeviceUdid();
    const second = await getDeviceUdid();

    expect(first).toBe(second);
    // Storage should only be read once
    expect(mockStorage.get).toHaveBeenCalledTimes(1);
  });

  it('throws if _platform.storage is not available', async () => {
    (window as any)._platform = undefined;

    const { getDeviceUdid } = await freshImport();
    await expect(getDeviceUdid()).rejects.toThrow('Platform storage not available');
  });

  it('migration guard is non-fatal if token removal fails', async () => {
    mockStorage.get.mockResolvedValue(null);
    mockStorage.set.mockResolvedValue(undefined);
    mockStorage.remove.mockRejectedValue(new Error('storage error'));

    const { getDeviceUdid } = await freshImport();
    // Should not throw despite remove() failing
    const udid = await getDeviceUdid();
    expect(udid).toMatch(/^[0-9a-f]{32}$/);
  });
});
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd webapp && npx vitest run src/services/__tests__/device-udid.test.ts`
Expected: FAIL — module `../device-udid` does not exist

- [ ] **Step 3: Write the implementation**

```typescript
// webapp/src/services/device-udid.ts
import type { ISecureStorage } from '../types/kaitu-core';

const STORAGE_KEY = 'device-udid';
let cachedUdid: string | null = null;

/**
 * Get or generate a persistent device UDID.
 *
 * First call: reads from _platform.storage.
 * If not found: generates crypto.randomUUID(), stores it, returns SHA-256 hash.
 * Subsequent calls: returns cached value (no I/O).
 *
 * Output: 32 lowercase hex chars (SHA-256 first 16 bytes), same format as previous
 * hardware-based UDID.
 */
export async function getDeviceUdid(): Promise<string> {
  if (cachedUdid) return cachedUdid;

  const storage = window._platform?.storage;
  if (!storage) throw new Error('[DeviceUDID] Platform storage not available');

  let raw = await storage.get<string>(STORAGE_KEY);
  if (!raw) {
    raw = crypto.randomUUID();
    await storage.set(STORAGE_KEY, raw);
    await clearStaleAuthTokens(storage);
  }

  cachedUdid = await hashToUdid(raw);
  return cachedUdid;
}

async function hashToUdid(raw: string): Promise<string> {
  const data = new TextEncoder().encode(raw);
  const hashBuffer = await crypto.subtle.digest('SHA-256', data);
  return Array.from(new Uint8Array(hashBuffer))
    .slice(0, 16)
    .map(b => b.toString(16).padStart(2, '0'))
    .join('');
}

async function clearStaleAuthTokens(storage: ISecureStorage): Promise<void> {
  try {
    await storage.remove('k2.auth.token');
    await storage.remove('k2.auth.refresh');
    console.info('[DeviceUDID] New device UDID generated, cleared stale auth tokens');
  } catch {
    // Non-fatal: worst case user gets a VPN auth error and re-logs in manually
  }
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd webapp && npx vitest run src/services/__tests__/device-udid.test.ts`
Expected: All 5 tests PASS

- [ ] **Step 5: Commit**

```bash
git add webapp/src/services/device-udid.ts webapp/src/services/__tests__/device-udid.test.ts
git commit -m "feat: add random UDID generation module (device-udid.ts)"
```

---

### Task 2: Remove `getUdid` from `IPlatform` interface

**Files:**
- Modify: `webapp/src/types/kaitu-core.ts:142`

- [ ] **Step 1: Remove `getUdid` from `IPlatform`**

In `webapp/src/types/kaitu-core.ts`, delete line 142:

```diff
  // ====== 核心能力 ======

  storage: ISecureStorage;
- getUdid(): Promise<string>;

  // ====== 跨平台能力 ======
```

- [ ] **Step 2: Run type check to see what breaks**

Run: `cd webapp && npx tsc --noEmit 2>&1 | head -40`
Expected: TypeScript errors in bridge files (`tauri-k2.ts`, `capacitor-k2.ts`, `standalone-k2.ts`) and call sites. This is expected — we'll fix them in subsequent tasks.

- [ ] **Step 3: Commit**

```bash
git add webapp/src/types/kaitu-core.ts
git commit -m "refactor: remove getUdid from IPlatform interface"
```

---

### Task 3: Migrate all webapp callers to `getDeviceUdid()`

**Files:**
- Modify: `webapp/src/services/auth-service.ts:1,152-165`
- Modify: `webapp/src/components/LoginDialog.tsx:157`
- Modify: `webapp/src/components/EmailLoginForm.tsx:171,219`
- Modify: `webapp/src/pages/SubmitTicket.tsx:114`
- Modify: `webapp/src/services/beta-auto-upload.ts:23`
- Modify: `webapp/src/services/stats.ts:74-101`

- [ ] **Step 1: Migrate `auth-service.ts`**

Replace the `getUdid()` method (lines 152-165):

```diff
+ import { getDeviceUdid } from './device-udid';
  // ... (at top of file, with other imports)

- /**
-  * Get device UDID
-  * Uses platform.getUdid() which handles generation and caching
-  * @returns 57-character UDID
-  */
- async getUdid(): Promise<string> {
-   const platform = getPlatform();
-
-   if (!platform.getUdid) {
-     throw new Error('[AuthService] Platform does not support getUdid()');
-   }
-
-   return platform.getUdid();
- },
+ async getUdid(): Promise<string> {
+   return getDeviceUdid();
+ },
```

- [ ] **Step 2: Migrate `LoginDialog.tsx`**

Add import at top, replace line 157:

```diff
+ import { getDeviceUdid } from '../services/device-udid';
  // ...
- const udid = await window._platform!.getUdid();
+ const udid = await getDeviceUdid();
```

- [ ] **Step 3: Migrate `EmailLoginForm.tsx`**

Add import at top, replace lines 171 and 219:

```diff
+ import { getDeviceUdid } from '../services/device-udid';
  // ...
  // Line 171 (verification code login):
- const udid = await window._platform!.getUdid();
+ const udid = await getDeviceUdid();
  // ...
  // Line 219 (password login):
- const udid = await window._platform!.getUdid();
+ const udid = await getDeviceUdid();
```

- [ ] **Step 4: Migrate `SubmitTicket.tsx`**

Add import at top, replace line 114:

```diff
+ import { getDeviceUdid } from '../services/device-udid';
  // ...
- window._platform.getUdid().then((udid) => {
+ getDeviceUdid().then((udid) => {
```

- [ ] **Step 5: Migrate `beta-auto-upload.ts`**

Add import at top, replace line 23:

```diff
+ import { getDeviceUdid } from './device-udid';
  // ...
- const udid = await window._platform.getUdid();
+ const udid = await getDeviceUdid();
```

- [ ] **Step 6: Simplify `stats.ts`**

Replace the entire `getDeviceHash()` function (lines 74-101). The daemon-dependency fallback is dead code now since `getDeviceUdid()` never fails for daemon reasons:

```diff
+ import { getDeviceUdid } from './device-udid';
  // ...

  let _deviceHash: string | null = null;

  async function getDeviceHash(): Promise<string> {
    if (_deviceHash) return _deviceHash;
-   try {
-     const udid = await window._platform?.getUdid();
-     if (udid) {
-       // UDID is already a 32 hex char hash from the native layer — use directly
-       _deviceHash = udid;
-       return _deviceHash;
-     }
-   } catch {
-     // getUdid failed (e.g. Windows daemon not responding)
-   }
-
-   // Fallback: generate a persistent random ID so each device is still unique
-   const FALLBACK_KEY = 'stats_device_id';
-   try {
-     let fallbackId = await window._platform?.storage?.get<string>(FALLBACK_KEY);
-     if (!fallbackId) {
-       fallbackId = crypto.randomUUID();
-       await window._platform?.storage?.set(FALLBACK_KEY, fallbackId);
-     }
-     _deviceHash = await sha256(fallbackId);
-     return _deviceHash;
-   } catch {
-     // storage also failed
-   }
-   return 'unknown';
+   try {
+     _deviceHash = await getDeviceUdid();
+     return _deviceHash;
+   } catch {
+     return 'unknown';
+   }
  }
```

Also delete the now-unused `sha256()` helper function (lines 103-108) — it has no other callers in this file after the fallback removal.

- [ ] **Step 7: Update `debug.html`**

In `webapp/debug.html` line 313, the UDID button calls `callPlatform('getUdid')`. Since `debug.html` is a standalone HTML file that can't import ES modules, change the button to show a message that UDID is now auto-generated:

```diff
- <button class="btn btn-platform" onclick="callPlatform('getUdid')">UDID</button>
+ <button class="btn btn-platform" onclick="appendLog('INFO', 'UDID', 'auto-generated via device-udid.ts (stored in _platform.storage)')">UDID (info)</button>
```

- [ ] **Step 8: Run type check**

Run: `cd webapp && npx tsc --noEmit 2>&1 | head -40`
Expected: Errors only in bridge files (`tauri-k2.ts`, `capacitor-k2.ts`, `standalone-k2.ts`) which still have `getUdid` properties that no longer match `IPlatform`.

- [ ] **Step 9: Commit**

```bash
git add webapp/src/services/auth-service.ts webapp/src/components/LoginDialog.tsx webapp/src/components/EmailLoginForm.tsx webapp/src/pages/SubmitTicket.tsx webapp/src/services/beta-auto-upload.ts webapp/src/services/stats.ts webapp/debug.html
git commit -m "refactor: migrate all callers from _platform.getUdid() to getDeviceUdid()"
```

---

### Task 4: Update Rust `log_upload.rs` to receive UDID as parameter

**Files:**
- Modify: `desktop/src-tauri/src/log_upload.rs:662-676`
- Modify: `webapp/src/services/tauri-k2.ts:260-262`

**IMPORTANT:** This task MUST complete before Task 8 (native cleanup). Task 8 deletes `get_hardware_uuid()` which this file currently calls.

- [ ] **Step 1: Modify Rust `upload_service_log_command`**

In `desktop/src-tauri/src/log_upload.rs`, change the IPC command (lines 662-676):

```diff
  #[tauri::command]
  pub async fn upload_service_log_command(
      params: UploadLogParams,
+     udid: String,
  ) -> Result<UploadLogResult, String> {
      tokio::task::spawn_blocking(move || {
-         let udid = crate::service::get_hardware_uuid().unwrap_or_else(|_| "unknown".into());
          if params.reason == "beta-auto-upload" {
              upload_auto(&udid)
          } else {
              upload_service_log(params, udid)
          }
      })
      .await
      .map_err(|e| format!("Task failed: {}", e))
  }
```

- [ ] **Step 2: Update Tauri bridge `uploadLogs` to pass UDID**

In `webapp/src/services/tauri-k2.ts`, modify `uploadLogs` (lines 260-262):

```diff
+ import { getDeviceUdid } from './device-udid';
  // ... (at top of file, with other imports)

  uploadLogs: async (params): Promise<{ success: boolean; error?: string; s3Keys?: Array<{ name: string; s3Key: string }> }> => {
-   return await invoke<{ success: boolean; error?: string; s3Keys?: Array<{ name: string; s3Key: string }> }>('upload_service_log_command', { params });
+   const udid = await getDeviceUdid();
+   return await invoke<{ success: boolean; error?: string; s3Keys?: Array<{ name: string; s3Key: string }> }>('upload_service_log_command', { params, udid });
  },
```

- [ ] **Step 3: Verify Rust compiles**

Run: `cd desktop/src-tauri && cargo check 2>&1 | tail -5`
Expected: Compiles successfully (no errors). Note: `get_hardware_uuid` is still present but now unused in log_upload — that's OK, it gets cleaned up in Task 7.

- [ ] **Step 4: Commit**

```bash
git add desktop/src-tauri/src/log_upload.rs webapp/src/services/tauri-k2.ts
git commit -m "refactor: pass UDID from webapp to Rust log_upload instead of reading hardware UUID"
```

---

### Task 5: Update mobile plugin `uploadLogs` to accept UDID parameter

**Files:**
- Modify: `mobile/plugins/k2-plugin/src/definitions.ts:32`
- Modify: `mobile/plugins/k2-plugin/ios/Plugin/K2Plugin.swift:592-604`
- Modify: `mobile/plugins/k2-plugin/android/.../K2Plugin.kt:607-622`
- Modify: `webapp/src/services/capacitor-k2.ts:258-266`

**Problem:** Same as Rust log_upload — mobile `uploadLogs` internally calls `hashToUdid(identifierForVendor)` / `hashToUdid(ANDROID_ID)` for S3 path construction. After UDID migration, webapp sends random UDID to API but S3 path contains hardware UDID → mismatch. Also, if we delete `hashToUdid` in Task 8 cleanup, `uploadLogs` breaks compilation.

**Fix:** Add `udid` parameter to `uploadLogs` plugin interface. Native code uses the passed UDID instead of generating its own.

- [ ] **Step 1: Add `udid` to plugin TypeScript interface**

In `mobile/plugins/k2-plugin/src/definitions.ts` line 32:

```diff
- uploadLogs(options: { email?: string; reason: string; feedbackId?: string; platform?: string; version?: string }): Promise<{ success: boolean; error?: string; s3Keys?: Array<{ name: string; s3Key: string }> }>;
+ uploadLogs(options: { email?: string; reason: string; feedbackId?: string; platform?: string; version?: string; udid?: string }): Promise<{ success: boolean; error?: string; s3Keys?: Array<{ name: string; s3Key: string }> }>;
```

- [ ] **Step 2: Update iOS K2Plugin.swift uploadLogs**

In `mobile/plugins/k2-plugin/ios/Plugin/K2Plugin.swift`, modify `uploadLogs` (line 592+). Replace the internal UDID generation (lines 602-604) with parameter reading:

```diff
  @objc func uploadLogs(_ call: CAPPluginCall) {
      let feedbackId = call.getString("feedbackId")
+     let passedUdid = call.getString("udid")

      Task {
          // ...
-             // Get UDID for S3 key
-             let raw = UIDevice.current.identifierForVendor?.uuidString ?? UUID().uuidString
-             let udid = hashToUdid(raw)
+             // Get UDID for S3 key — prefer passed UDID, fallback to hardware
+             let udid: String
+             if let passed = passedUdid, !passed.isEmpty {
+                 udid = passed
+             } else {
+                 let raw = UIDevice.current.identifierForVendor?.uuidString ?? UUID().uuidString
+                 udid = hashToUdid(raw)
+             }
```

- [ ] **Step 3: Update Android K2Plugin.kt uploadLogs**

In `mobile/plugins/k2-plugin/android/.../K2Plugin.kt`, modify `uploadLogs` (line 607+). Replace the internal UDID generation (lines 621-622):

```diff
  fun uploadLogs(call: PluginCall) {
      val feedbackId = call.getString("feedbackId")
+     val passedUdid = call.getString("udid")

      Thread {
          // ...
-             val raw = android.provider.Settings.Secure.getString(context.contentResolver, android.provider.Settings.Secure.ANDROID_ID)
-             val udid = K2PluginUtils.hashToUdid(raw)
+             val udid = if (!passedUdid.isNullOrEmpty()) {
+                 passedUdid
+             } else {
+                 val raw = android.provider.Settings.Secure.getString(context.contentResolver, android.provider.Settings.Secure.ANDROID_ID)
+                 K2PluginUtils.hashToUdid(raw)
+             }
```

- [ ] **Step 4: Update Capacitor bridge to pass UDID**

In `webapp/src/services/capacitor-k2.ts`, modify `uploadLogs` (lines 258-266):

```diff
+ import { getDeviceUdid } from './device-udid';
  // ... (at top of file, with other imports)

  uploadLogs: async (params) => {
+   const udid = await getDeviceUdid();
    const result = await K2Plugin.uploadLogs({
      email: params.email ?? undefined,
      reason: params.reason,
      feedbackId: params.feedbackId,
      platform: params.platform,
      version: params.version,
+     udid,
    });
    return result;
  },
```

- [ ] **Step 5: Rebuild plugin**

Run: `cd mobile/plugins/k2-plugin && npm run build`
Expected: Build succeeds. `dist/` files regenerated with `udid` in `uploadLogs` interface.

- [ ] **Step 6: Commit**

```bash
git add mobile/plugins/k2-plugin/ webapp/src/services/capacitor-k2.ts
git commit -m "refactor: pass UDID from webapp to mobile plugin uploadLogs"
```

---

### Task 6: Delete `getUdid` from all three desktop/web bridge files

**Files:**
- Modify: `webapp/src/services/tauri-k2.ts:226-232`
- Modify: `webapp/src/services/capacitor-k2.ts:225-228`
- Modify: `webapp/src/services/standalone-k2.ts:47-56,74`

- [ ] **Step 1: Remove `getUdid` from `tauri-k2.ts`**

Delete lines 226-232:

```diff
  storage: webSecureStorage,

- getUdid: async (): Promise<string> => {
-   const response = await invoke<ServiceResponse>('get_udid');
-   if (response.code === 0 && response.data?.udid) {
-     return response.data.udid;
-   }
-   throw new Error('Failed to get UDID from daemon');
- },
-
  openExternal: async (url: string): Promise<void> => {
```

- [ ] **Step 2: Remove `getUdid` from `capacitor-k2.ts`**

Delete lines 225-228:

```diff
  storage: webSecureStorage,

- getUdid: async (): Promise<string> => {
-   const result = await K2Plugin.getUDID();
-   return result.udid;
- },
-
  openExternal: async (url: string): Promise<void> => {
```

- [ ] **Step 3: Remove `getDaemonUdid` and `getUdid` from `standalone-k2.ts`**

Delete the `getDaemonUdid` function (lines 47-56) and the `getUdid` property (line 74):

```diff
- /**
-  * Get device UDID from the daemon's /api/device/udid endpoint.
-  * In standalone/router mode, UDID generation is the daemon's responsibility.
-  */
- async function getDaemonUdid(): Promise<string> {
-   const resp = await fetch('/api/device/udid');
-   const json = await resp.json();
-   if (json.code === 0 && json.data?.udid) return json.data.udid;
-   throw new Error('Failed to get UDID from daemon');
- }
  // ...
  export const standalonePlatform: IPlatform = {
    ...webPlatform,
    os: 'web',
    version: 'standalone',
    arch: 'unknown',
    commit: typeof __K2_BUILD_COMMIT__ !== 'undefined' ? __K2_BUILD_COMMIT__ : '',
-   getUdid: getDaemonUdid,
    storage: webSecureStorage,
    setDevEnabled: () => {},
  };
```

Also update the file header comment (line 9):

```diff
- *   window._platform = { os, storage, getUdid, ... }    (platform capabilities)
+ *   window._platform = { os, storage, ... }              (platform capabilities)
```

- [ ] **Step 4: Run type check — should pass clean now**

Run: `cd webapp && npx tsc --noEmit`
Expected: No errors. All bridges now match the updated `IPlatform` (without `getUdid`).

- [ ] **Step 5: Commit**

```bash
git add webapp/src/services/tauri-k2.ts webapp/src/services/capacitor-k2.ts webapp/src/services/standalone-k2.ts
git commit -m "refactor: remove getUdid from all bridge implementations"
```

---

### Task 7: Update all tests

**Files:**
- Modify: `webapp/src/services/__tests__/auth-service-v2.test.ts`
- Modify: `webapp/src/services/__tests__/stats.test.ts`
- Modify: `webapp/src/services/__tests__/tauri-k2.test.ts`
- Modify: `webapp/src/services/__tests__/standalone-k2-v2.test.ts`
- Modify: `webapp/src/services/__tests__/capacitor-k2.test.ts`
- Modify: `webapp/src/services/__tests__/web-platform-v2.test.ts`
- Modify: `webapp/src/stores/__tests__/config.store.test.ts`
- Modify: `webapp/src/stores/__tests__/self-hosted.store.test.ts`
- Modify: `webapp/src/stores/__tests__/onboarding.store.test.ts`
- Modify: `webapp/src/services/__tests__/consumer-migration.test.ts`
- Modify: `webapp/src/pages/__tests__/SubmitTicket.test.tsx`
- Modify: `webapp/src/types/__tests__/kaitu-core.test.ts`

Strategy: There are two categories of test changes needed:

**Category A — Tests that mock `_platform.getUdid`:** These need to mock `device-udid.ts` instead.
**Category B — Tests that assert `getUdid` exists on `IPlatform`:** These need to be removed or updated.

- [ ] **Step 1: Add device-udid mock to test files that call getUdid**

For tests that mock `_platform.getUdid` and test code that calls it, add this at the top:

```typescript
vi.mock('../../services/device-udid', () => ({
  getDeviceUdid: vi.fn().mockResolvedValue('test-udid-abc123'),
}));
```

Then remove `getUdid: vi.fn()...` from mock `_platform` objects.

Apply to these files (each file may need path adjustment for `vi.mock`):
- `auth-service-v2.test.ts` — mock `../device-udid`, remove `mockPlatform.getUdid`, update `getUdid` test block
- `stats.test.ts` — mock `../device-udid`, remove `_platform.getUdid` mocks, update fallback test
- `capacitor-k2.test.ts` — remove `test_platform_getUdid` test
- `tauri-k2.test.ts` — remove `_platform.getUdid invokes get_udid IPC command` test
- `standalone-k2-v2.test.ts` — remove `should have window._platform.getUdid as a function` test
- `SubmitTicket.test.tsx` — mock `../../services/device-udid`, remove `getUdid` from mock platform

- [ ] **Step 2: Remove `getUdid` from mock IPlatform objects in store tests**

These tests create mock `_platform` objects that include `getUdid`. Remove the property:
- `config.store.test.ts` — remove `getUdid: vi.fn()...` from mock
- `self-hosted.store.test.ts` — remove `getUdid: vi.fn()...` from mock
- `onboarding.store.test.ts` — remove `getUdid: vi.fn()` from mock
- `consumer-migration.test.ts` — remove `getUdid: vi.fn()...` from mock

- [ ] **Step 3: Update `kaitu-core.test.ts`**

Remove or update the `IPlatform has getUdid` describe block (lines 148-182). Since `getUdid` is no longer on `IPlatform`, these tests are now invalid. Delete the entire describe block. Also remove `getUdid` from any mock `IPlatform` objects elsewhere in this file.

- [ ] **Step 4: Update `web-platform-v2.test.ts`**

The test at line 111 asserts `webPlatform does NOT have getUdid`. Since `getUdid` is no longer on `IPlatform`, this test is now redundant — delete it. If there are other `getUdid`-related assertions in this file, delete those too.

- [ ] **Step 5: Run all tests**

Run: `cd webapp && npx vitest run`
Expected: All tests pass. Zero failures.

- [ ] **Step 6: Commit**

```bash
git add webapp/src/services/__tests__/ webapp/src/stores/__tests__/ webapp/src/pages/__tests__/ webapp/src/types/__tests__/
git commit -m "test: update all tests for getDeviceUdid() migration"
```

---

### Task 8: Delete native UDID code (Rust, Swift, Kotlin)

**Files:**
- Modify: `desktop/src-tauri/src/service.rs` — delete `hash_to_udid`, `get_udid`, `get_udid_native`, `get_hardware_uuid`, `get_raw_hardware_id` + related tests
- Modify: `desktop/src-tauri/src/main.rs:111` — remove `service::get_udid` from IPC handler list
- Modify: `desktop/src-tauri/src/ne.rs:133` — delete `get_udid_native` + test at line 494-504
- Modify: `mobile/plugins/k2-plugin/ios/Plugin/K2Plugin.swift` — delete `getUDID` method (L171-174). KEEP `hashToUdid` — still used as fallback in `uploadLogs`
- Modify: `mobile/plugins/k2-plugin/ios/Plugin/K2Plugin.m` — delete `getUDID` export (L6)
- Modify: `mobile/plugins/k2-plugin/android/.../K2Plugin.kt` — delete `getUDID` method (L112-117). KEEP `K2PluginUtils.hashToUdid` — still used as fallback in `uploadLogs`
- Modify: `mobile/plugins/k2-plugin/src/definitions.ts` — delete `getUDID` interface
- Modify: `mobile/plugins/k2-plugin/src/web.ts` — delete `getUDID` fallback

**IMPORTANT:** Tasks 4 and 5 must be completed first. After those tasks, `log_upload.rs` and mobile plugins no longer call `get_hardware_uuid()` / `hashToUdid()` internally, so it's safe to delete.

- [ ] **Step 1: Delete UDID functions from `service.rs`**

Delete these functions and their tests:
- `hash_to_udid()` (line 286-290)
- `get_udid()` (line 301-305)
- `get_udid_native()` (line 307-314)
- `get_hardware_uuid()` (line 316-319) — verify `log_upload.rs` no longer calls it first!
- `get_raw_hardware_id()` (line 322-368)
- Tests: `test_hash_to_udid_format`, `test_hash_to_udid_deterministic`, `test_hash_to_udid_different_inputs`, `test_hash_to_udid_uuid_input`, `test_hash_to_udid_short_input`, `test_windows_udid_wmi_available`

Also remove the `use sha2::{Sha256, Digest};` import if it's only used by `hash_to_udid`. Check with: `grep -n 'Sha256\|Digest' desktop/src-tauri/src/service.rs`

- [ ] **Step 2: Remove `get_udid` from IPC handler list in `main.rs`**

In `desktop/src-tauri/src/main.rs` line 111, remove `service::get_udid` from `tauri::generate_handler![]`.

- [ ] **Step 3: Delete `get_udid_native` from `ne.rs`**

Delete the `get_udid_native()` function (line 133+) and its test `test_get_udid_macos_native` (lines 494-531, including the `cfg` macro wrapper).

- [ ] **Step 4: Verify Rust compiles**

Run: `cd desktop/src-tauri && cargo check 2>&1 | tail -5`
Expected: Compiles successfully.

- [ ] **Step 5: Run Rust tests**

Run: `cd desktop/src-tauri && cargo test 2>&1 | tail -20`
Expected: All remaining tests pass.

- [ ] **Step 6: Delete `getUDID` from K2Plugin.swift**

In `mobile/plugins/k2-plugin/ios/Plugin/K2Plugin.swift`, delete ONLY the `getUDID` method (lines 171-174):
```swift
    @objc func getUDID(_ call: CAPPluginCall) {
        let raw = UIDevice.current.identifierForVendor?.uuidString ?? UUID().uuidString
        call.resolve(["udid": hashToUdid(raw)])
    }
```
**KEEP `hashToUdid`** (lines 176-182) — it's still used as fallback in `uploadLogs` (line 604).

- [ ] **Step 7: Delete `getUDID` export from K2Plugin.m**

In `mobile/plugins/k2-plugin/ios/Plugin/K2Plugin.m`, delete line 6:
```objc
    CAP_PLUGIN_METHOD(getUDID, CAPPluginReturnPromise);
```

- [ ] **Step 8: Delete `getUDID` from K2Plugin.kt**

In `mobile/plugins/k2-plugin/android/.../K2Plugin.kt`, delete ONLY the `getUDID` method (lines 112-117):
```kotlin
    @PluginMethod
    fun getUDID(call: PluginCall) {
        val raw = Settings.Secure.getString(context.contentResolver, Settings.Secure.ANDROID_ID)
        val ret = JSObject()
        ret.put("udid", K2PluginUtils.hashToUdid(raw))
        call.resolve(ret)
    }
```
**KEEP `K2PluginUtils.hashToUdid`** — still used as fallback in `uploadLogs` (line 622).

- [ ] **Step 9: Delete `getUDID` from plugin TypeScript definitions**

In `mobile/plugins/k2-plugin/src/definitions.ts`, delete `getUDID` from the interface.
In `mobile/plugins/k2-plugin/src/web.ts`, delete `getUDID` fallback method.

- [ ] **Step 10: Rebuild plugin dist**

Run: `cd mobile/plugins/k2-plugin && npm run build`
Expected: Build succeeds, `dist/` files regenerated without `getUDID`.

- [ ] **Step 11: Commit**

```bash
git add desktop/src-tauri/src/service.rs desktop/src-tauri/src/main.rs desktop/src-tauri/src/ne.rs mobile/plugins/k2-plugin/
git commit -m "cleanup: delete native UDID generation code (Rust, Swift, Kotlin)"
```

---

### Task 9: Update documentation

**Files:**
- Modify: `webapp/CLAUDE.md`
- Modify: `CLAUDE.md` (root)

- [ ] **Step 1: Update `webapp/CLAUDE.md`**

1. In the architecture diagram (around line 113), remove `getUdid()` from `IPlatform`:

```diff
  │ window._platform: IPlatform     │
  │   os, version                   │
  │   storage: ISecureStorage        │
- │   getUdid(), syncLocale()       │
+ │   syncLocale()                  │
```

2. In the troubleshooting section (around line 261), update:

```diff
- | Login fails with 422 | All login paths must include `udid` from `window._platform!.getUdid()` in POST body. Backend requires UDID for device association. |
+ | Login fails with 422 | All login paths must include `udid` from `getDeviceUdid()` (in `services/device-udid.ts`) in POST body. Backend requires UDID for device association. |
```

3. In the Hard Rules DO section (around line 42), update:

```diff
- - Platform capabilities via window._platform (storage, getUdid, clipboard, etc.)
+ - Platform capabilities via window._platform (storage, clipboard, etc.)
+ - Device UDID via getDeviceUdid() from services/device-udid.ts (NOT _platform)
```

- [ ] **Step 2: Update root `CLAUDE.md`**

In the Key Conventions section, after the `Bridge transformStatus() mandatory` entry, add or verify there's no stale `getUdid` reference. Search: `grep -n getUdid CLAUDE.md`. If found, update.

- [ ] **Step 3: Commit**

```bash
git add webapp/CLAUDE.md CLAUDE.md
git commit -m "docs: update CLAUDE.md for UDID migration to device-udid.ts"
```

---

### Task 10: Final verification

- [ ] **Step 1: Run webapp tests**

Run: `cd webapp && npx vitest run`
Expected: All tests pass.

- [ ] **Step 2: Run Rust tests**

Run: `cd desktop/src-tauri && cargo test`
Expected: All tests pass.

- [ ] **Step 3: Run TypeScript type check**

Run: `cd webapp && npx tsc --noEmit`
Expected: No errors.

- [ ] **Step 4: Verify no stale `getUdid` references in source code**

Run: `grep -r "getUdid\|get_udid\|getUDID" webapp/src/ --include='*.ts' --include='*.tsx' | grep -v __tests__ | grep -v node_modules | grep -v '.d.ts'`
Expected: Zero matches (all references removed from production code).

Run: `grep -r "get_udid\|get_hardware_uuid\|get_raw_hardware_id\|hash_to_udid" desktop/src-tauri/src/ | grep -v "// " | grep -v test`
Expected: Zero matches in non-test code.

- [ ] **Step 5: Verify no stale `_platform.getUdid` in test code**

Run: `grep -r "_platform.*getUdid\|_platform!.getUdid" webapp/src/ --include='*.ts' --include='*.tsx'`
Expected: Zero matches (all test mocks updated).

- [ ] **Step 6: Final commit (if any fixes needed)**

Only if previous steps revealed issues. Otherwise, done.
