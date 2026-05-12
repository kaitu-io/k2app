# App Bypass (Split-Exclude) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Ship a per-app blacklist (split-exclude) feature: users on macOS / Windows / Linux desktop / Android pick apps that should always route direct, bypassing VPN. iOS is unsupported by design.

**Architecture:** Webapp owns user preference (`_platform.storage` key `k2.advanced.app_bypass`). `buildConnectConfig` prepends bypass routes to existing chnroute/global routes — rule engine evaluates first-match-wins. Native bridges expose installed-apps (Android) or running-processes (desktop) for candidate listing. Icons travel via custom `kaitu-icon://` URI scheme so JSON stays slim and browser image cache works. Dashboard's existing Advanced Settings collapse gets a new entry; clicking opens a standalone route `/app-bypass`. Entire Advanced Settings section locks when VPN ≠ idle.

**Tech Stack:** React 18 + MUI 5 + Zustand + React Router 7 (webapp) · Tauri 2 Rust commands + `register_uri_scheme_protocol` (macOS/Win) · Go `net/http` daemon helper + `/proc` enumeration (Linux desktop) · Kotlin Capacitor 7 plugin + `WebViewClient.shouldInterceptRequest` + Android `<queries>` (Android) · vitest + Playwright (tests)

**Spec:** `docs/superpowers/specs/2026-05-12-app-bypass-design.md`

---

## Phase 0 — Blocking Verification (no UI yet)

The spec's §12 says Phase 0 is a **blocking dependency**, not a fallback. Expected outcome: at least one platform's attribution wiring needs k2 submodule work. Document findings before writing any UI.

### Task 0.1: Attribution smoke — macOS sysext

**Files:**
- Create: `docs/superpowers/plans/2026-05-12-app-bypass-phase0-results.md` (working log, updated as tasks 0.1–0.4 run)

- [ ] **Step 1: Build current main with logging level info**

```bash
cd /Users/david/projects/kaitu-io/k2app
make build-macos K2_BUILD_LOG_LEVEL=info
```

Expected: signed Kaitu.app in `desktop/src-tauri/target/release/bundle/macos/`.

- [ ] **Step 2: Write a hand-crafted ClientConfig**

```bash
mkdir -p /tmp/k2-phase0
cat > /tmp/k2-phase0/macos-config.json <<'EOF'
{
  "mode": "tun",
  "log": { "level": "info" },
  "routes": [
    { "via": "direct", "match": { "process_name": ["curl"] } },
    { "via": "k2v5://YOUR_SERVER_URL_HERE", "match": {} }
  ]
}
EOF
```

Replace `YOUR_SERVER_URL_HERE` with a valid k2v5 URL from your account.

- [ ] **Step 3: POST the config to the daemon**

```bash
curl -X POST http://127.0.0.1:1777/api/core \
  -H 'Content-Type: application/json' \
  -d "$(jq -n --slurpfile cfg /tmp/k2-phase0/macos-config.json '{action:"up", params:{config:$cfg[0]}}')"
```

Expected: `{"code":0,...}` and `k2 status` shows `connected`.

- [ ] **Step 4: Compare exit IPs**

```bash
# Terminal curl should show ISP IP (direct bypass)
curl https://ip.kaitu.io/
# A browser visit to https://ip.kaitu.io/ should show server IP (tunneled)
```

Pass: two IPs differ. Fail: both IPs same → process_name attribution is broken on macOS sysext.

- [ ] **Step 5: Record in phase0-results.md**

Add a section:
```
## macOS sysext attribution
Tested on: [date, macOS version]
Server tested: [k2v5 URL]
Curl IP: [ISP IP]
Browser IP: [server IP]
Result: PASS / FAIL
Notes: [if FAIL, where in k2/appext/ does process metadata break?]
```

- [ ] **Step 6: Commit phase0-results.md**

```bash
git add docs/superpowers/plans/2026-05-12-app-bypass-phase0-results.md
git commit -m "docs(plans): app-bypass Phase 0 macOS attribution result"
```

### Task 0.2: Attribution smoke — Windows daemon

**Files:**
- Modify: `docs/superpowers/plans/2026-05-12-app-bypass-phase0-results.md`

- [ ] **Step 1: Build current main on Windows**

```powershell
cd C:\path\to\k2app
make build-windows K2_BUILD_LOG_LEVEL=info
```

Or use a Windows VM if developing on macOS.

- [ ] **Step 2: Build the config**

```powershell
@'
{
  "mode": "tun",
  "log": { "level": "info" },
  "routes": [
    { "via": "direct", "match": { "process_name": ["curl.exe"] } },
    { "via": "k2v5://YOUR_SERVER_URL_HERE", "match": {} }
  ]
}
'@ | Set-Content C:\Users\$env:USERNAME\k2-phase0-win.json
```

- [ ] **Step 3: POST config and verify**

```powershell
$cfg = Get-Content C:\Users\$env:USERNAME\k2-phase0-win.json -Raw
Invoke-RestMethod -Uri 'http://127.0.0.1:1777/api/core' -Method Post -ContentType 'application/json' `
  -Body (ConvertTo-Json -Depth 5 @{action='up'; params=@{config=(ConvertFrom-Json $cfg)}})
```

- [ ] **Step 4: Compare exit IPs**

```powershell
# In cmd terminal
curl.exe https://ip.kaitu.io/
# In Edge browser
# Visit https://ip.kaitu.io/
```

Pass: differ. Fail: same.

- [ ] **Step 5: Record + commit**

Append result to phase0-results.md, commit.

### Task 0.3: Attribution smoke — Linux desktop

**Files:**
- Modify: `docs/superpowers/plans/2026-05-12-app-bypass-phase0-results.md`

- [ ] **Step 1: Build for Linux**

```bash
cd /Users/david/projects/kaitu-io/k2app
GOOS=linux GOARCH=amd64 go build -o /tmp/k2-linux ./k2/cmd/k2
```

Run on a Linux box (VM, container with NET_ADMIN, or real machine).

- [ ] **Step 2: Same procedure as 0.1 with `process_name: ["curl"]`**

```bash
curl -X POST http://127.0.0.1:1777/api/core \
  -H 'Content-Type: application/json' \
  -d '{"action":"up","params":{"config":{"mode":"tun","routes":[{"via":"direct","match":{"process_name":["curl"]}},{"via":"k2v5://YOUR_URL","match":{}}]}}}'
curl https://ip.kaitu.io/
firefox https://ip.kaitu.io/   # or any other browser
```

- [ ] **Step 3: Record + commit**

### Task 0.4: Attribution smoke — Android

**Files:**
- Modify: `docs/superpowers/plans/2026-05-12-app-bypass-phase0-results.md`

- [ ] **Step 1: Build Android with logging**

```bash
make build-android K2_BUILD_LOG_LEVEL=info
```

Install to a real device or emulator.

- [ ] **Step 2: Construct ClientConfig in JS console via webapp dev mode**

Open the app in dev mode (`make dev-android`). In WebView devtools console:

```js
await window._k2.run('up', {
  mode: 'tun',
  log: { level: 'info' },
  routes: [
    { via: 'direct', match: { package_name: ['com.android.chrome'] } },
    { via: 'k2v5://YOUR_URL', match: {} }
  ]
});
```

- [ ] **Step 3: Compare exit IPs**

In Chrome: visit `https://ip.kaitu.io/` → record IP.
In another user app (e.g., Firefox / Brave): visit same URL → record IP.

Pass: differ.

- [ ] **Step 4: Record + commit**

### Task 0.5: macOS helper-name verification (5 high-value apps)

**Files:**
- Modify: `docs/superpowers/plans/2026-05-12-app-bypass-phase0-results.md`

- [ ] **Step 1: Launch Chrome, WeChat, Slack, Zoom, Telegram on macOS**

Open each app, leave running.

- [ ] **Step 2: Enumerate process names per bundle**

```bash
for app in 'Google Chrome' WeChat Slack zoom.us Telegram; do
  echo "=== $app ==="
  ps -ax -o comm | grep -iF "$app" | sort -u
done
```

- [ ] **Step 3: Record actual helper names**

Append a section to phase0-results.md listing each bundle's helper process names — these become unit test fixtures for Task 4.

- [ ] **Step 4: Commit**

```bash
git add docs/superpowers/plans/2026-05-12-app-bypass-phase0-results.md
git commit -m "docs(plans): app-bypass Phase 0 macOS helper-name fixtures"
```

### Task 0.6: Icon scheme POC — Tauri side (macOS)

**Files:**
- Create: `desktop/src-tauri/src/icon_protocol.rs`
- Modify: `desktop/src-tauri/src/main.rs` (register protocol)
- Create: `desktop/src-tauri/test-assets/test-icon-32.png` (any 32×32 PNG)

- [ ] **Step 1: Add the test PNG**

Generate or copy any 32×32 PNG to `desktop/src-tauri/test-assets/test-icon-32.png`. Stage it.

- [ ] **Step 2: Create icon_protocol.rs with a stub returning the test PNG for any path**

```rust
// desktop/src-tauri/src/icon_protocol.rs
use tauri::{http::Response, AppHandle, Manager, UriSchemeContext, Wry};

const TEST_PNG: &[u8] = include_bytes!("../test-assets/test-icon-32.png");

pub fn handle_kaitu_icon(
    _ctx: UriSchemeContext<Wry>,
    _request: tauri::http::Request<Vec<u8>>,
) -> Response<Vec<u8>> {
    Response::builder()
        .status(200)
        .header("Content-Type", "image/png")
        .header("Cache-Control", "public, max-age=86400")
        .body(TEST_PNG.to_vec())
        .unwrap()
}
```

- [ ] **Step 3: Register scheme in main.rs builder**

Find the `tauri::Builder::default()` chain in `desktop/src-tauri/src/main.rs` and add (before `.run(...)`):

```rust
.register_uri_scheme_protocol("kaitu-icon", icon_protocol::handle_kaitu_icon)
```

Also `mod icon_protocol;` near the top.

- [ ] **Step 4: Build + run dev**

```bash
cd /Users/david/projects/kaitu-io/k2app
make dev-macos
```

- [ ] **Step 5: Verify in browser devtools console**

```js
// Inside the dev WebView's devtools console:
const img = document.createElement('img');
img.src = 'kaitu-icon://test/anything';
img.onload = () => console.log('POC PASS — image dimensions:', img.width, img.height);
img.onerror = (e) => console.error('POC FAIL', e);
document.body.appendChild(img);
```

Pass: image renders, dimensions 32×32 logged. Fail: CSP / CORS error → fall back to Plan B in spec §12.4 (HTTPS asset loader) and update spec.

- [ ] **Step 6: Record + commit**

Append icon POC result to phase0-results.md. Commit Rust + assets + log.

```bash
git add desktop/src-tauri/src/icon_protocol.rs desktop/src-tauri/src/main.rs desktop/src-tauri/test-assets/ docs/superpowers/plans/2026-05-12-app-bypass-phase0-results.md
git commit -m "feat(desktop): kaitu-icon URI scheme POC"
```

### Task 0.7: Icon scheme POC — Android Capacitor side

**Files:**
- Modify: `mobile/plugins/k2-plugin/android/src/main/java/io/kaitu/k2plugin/K2Plugin.kt`
- Create: `mobile/plugins/k2-plugin/android/src/main/assets/test-icon-32.png` (same PNG)
- Modify: `docs/superpowers/plans/2026-05-12-app-bypass-phase0-results.md`

- [ ] **Step 1: Stage the test PNG in plugin assets**

```bash
cp desktop/src-tauri/test-assets/test-icon-32.png mobile/plugins/k2-plugin/android/src/main/assets/
```

- [ ] **Step 2: Override BridgeWebViewClient.shouldInterceptRequest in K2Plugin**

Inside `K2Plugin.kt`, add a `load()` override (or extend existing one) to set a custom WebViewClient:

```kotlin
override fun load() {
    super.load()
    val originalClient = bridge.webViewClient
    bridge.webView.webViewClient = object : BridgeWebViewClient(bridge) {
        override fun shouldInterceptRequest(view: WebView, request: WebResourceRequest): WebResourceResponse? {
            val url = request.url
            if (url.scheme == "kaitu-icon") {
                return try {
                    val stream = context.assets.open("test-icon-32.png")
                    WebResourceResponse("image/png", "binary", stream)
                } catch (e: Exception) {
                    Log.e(TAG, "kaitu-icon POC failed", e)
                    null
                }
            }
            return originalClient.shouldInterceptRequest(view, request)
        }
    }
}
```

Add necessary imports (`android.webkit.*`, `com.getcapacitor.BridgeWebViewClient`).

- [ ] **Step 3: Rebuild plugin and sync**

```bash
cd mobile/plugins/k2-plugin && npm run build
cd /Users/david/projects/kaitu-io/k2app
rm -rf mobile/node_modules/k2-plugin
yarn install --force
cd mobile && npx cap sync
make dev-android
```

- [ ] **Step 4: Verify in WebView devtools**

Use Chrome `chrome://inspect` to attach to the device WebView; in console:

```js
const img = document.createElement('img');
img.src = 'kaitu-icon://test/anything';
img.onload = () => console.log('POC PASS', img.width, img.height);
img.onerror = (e) => console.error('POC FAIL', e);
document.body.appendChild(img);
```

- [ ] **Step 5: Record + commit**

```bash
git add mobile/plugins/k2-plugin/android/ docs/superpowers/plans/
git commit -m "feat(mobile): kaitu-icon shouldInterceptRequest POC"
```

### Task 0.8: Phase 0 decision gate

**Files:**
- Modify: `docs/superpowers/plans/2026-05-12-app-bypass-phase0-results.md`

- [ ] **Step 1: Aggregate results**

Tally: how many of the 4 platforms in Tasks 0.1–0.4 passed? Did Tasks 0.6 + 0.7 both pass?

- [ ] **Step 2: Update the spec if necessary**

Per spec §12.5:
- If <3 attribution platforms pass → STOP, open k2 submodule ticket(s), update plan with new dependency.
- If icon POC failed on either platform → choose fallback (HTTPS asset loader OR lazy-fetch endpoint OR no-icon) and amend spec §5.5/§7.5 with the choice.

- [ ] **Step 3: Commit gate decision**

```bash
git add docs/
git commit -m "docs(plans): app-bypass Phase 0 decision — proceed/block"
```

If proceed → continue with Phase 1.

---

## Phase 1 — Foundation: types, feature flag, i18n keys

### Task 1.1: Add type definitions

**Files:**
- Modify: `webapp/src/types/kaitu-core.ts`

- [ ] **Step 1: Append AppList types**

Add to `webapp/src/types/kaitu-core.ts` (after the `IPlatform` interface, before `ISecureStorage` if present, or at file end):

```ts
export interface RunningApp {
  /** macOS: bundle identifier · Win/Linux: absolute executable path */
  id: string;
  /** Display name */
  label: string;
  /** Names to write into config.process_name — macOS: bundle's full helper set; Win/Linux: [exe basename] */
  processNames: string[];
  /** Custom-scheme URL or undefined; webapp renders <img src={iconUrl}> */
  iconUrl?: string;
}

export interface InstalledApp {
  packageName: string;
  label: string;
  iconUrl?: string;
}

export interface IAppListProvider {
  /** Desktop only (macOS / Windows / Linux desktop) */
  listRunning?(): Promise<RunningApp[]>;
  /** Android only */
  listInstalled?(): Promise<InstalledApp[]>;
}
```

Then add `appList?: IAppListProvider;` to the existing `IPlatform` interface.

- [ ] **Step 2: Type check**

```bash
cd webapp && npx tsc --noEmit
```

Expected: no errors (no consumers yet).

- [ ] **Step 3: Commit**

```bash
git add webapp/src/types/kaitu-core.ts
git commit -m "feat(webapp): add IAppListProvider + RunningApp/InstalledApp types"
```

### Task 1.2: Add feature flag

**Files:**
- Modify: `webapp/src/config/apps.ts`

- [ ] **Step 1: Locate the features object**

```bash
grep -n "features:" webapp/src/config/apps.ts | head -5
```

- [ ] **Step 2: Add `appBypass` flag**

In the `features` object for the active app config, add:

```ts
appBypass: __K2_BUILD_CHANNEL__ === 'beta',
```

If `__K2_BUILD_CHANNEL__` define isn't already in `vite.config.ts`, fall back to `false` and gate by environment in a follow-up. Verify by:

```bash
grep -n "__K2_BUILD_CHANNEL__\|K2_BUILD_CHANNEL" webapp/vite.config.ts webapp/src/env.d.ts
```

If define is missing, use this safer form (always-off until explicitly opted in):

```ts
appBypass: false,  // toggle to true in beta builds
```

- [ ] **Step 3: Update the features TypeScript type union if present**

Search:
```bash
grep -n "features:\s*{" webapp/src/config/apps.ts
```

If features type is structurally typed, no change needed. If there's an explicit `featureFlag:` type alias listing strings (e.g., in Layout.tsx), append `'appBypass'`:

```bash
grep -n "featureFlag?:" webapp/src/components/Layout.tsx
```

Update that union too.

- [ ] **Step 4: Type check**

```bash
cd webapp && npx tsc --noEmit
```

- [ ] **Step 5: Commit**

```bash
git add webapp/src/config/apps.ts webapp/src/components/Layout.tsx
git commit -m "feat(webapp): add appBypass feature flag"
```

### Task 1.3: Add zh-CN i18n keys (in existing dashboard.json)

**Files:**
- Modify: `webapp/src/i18n/locales/zh-CN/dashboard.json`
- Modify: `webapp/src/i18n/locales/namespaces.ts`

- [ ] **Step 1: Add keys to dashboard.json**

Locate the `"dashboard": { ... }` top-level object and add inside:

```json
"advancedSettingsLocked": "VPN 已连接，请先断开后再修改高级设置",
"disconnectVpn": "断开 VPN",
"appBypassEntry": {
  "label": "不走代理的应用",
  "count": "{{count}} 个",
  "empty": "未选"
}
```

Then add a new top-level sibling object (sibling of `"dashboard"`):

```json
"appBypass": {
  "title": "不走代理的应用",
  "description": "这些应用的流量将不走 VPN，直接出网。适合避免被风控的应用（网银、微信、12306 等）。",
  "macMultiUserNote": "此处只列出当前账户启动的应用。",
  "addedSection": "已加入（{{count}}）",
  "availableSection": "可添加",
  "manualAdd": "+ 手动添加",
  "manualAddTitle": "添加进程名",
  "manualAddPlaceholder": "如 chrome.exe / Adobe Photoshop / com.tencent.mm",
  "manualAddConfirm": "添加",
  "manualAddCancel": "取消",
  "manualAddUnsavedConfirm": "未保存的输入将丢失，确认离开？",
  "refresh": "刷新",
  "rescan": "重新检测",
  "rescanResult": "已更新（共 {{count}} 个进程）",
  "processCount": "屏蔽 {{count}} 个进程",
  "kickedOutDueToConnect": "VPN 连接后无法修改，已返回主页",
  "loadFailed": "加载应用列表失败",
  "saveFailed": "保存失败，请重试",
  "uninstalledHint": "应用可能已卸载"
}
```

- [ ] **Step 2: Map appBypass to dashboard namespace**

Edit `webapp/src/i18n/locales/namespaces.ts` `namespaceMapping`:

```ts
"appBypass": "dashboard",
```

(File has a "DO NOT EDIT" header about regeneration — manual edit is fine for this small addition; the regen script will pick it up next run.)

- [ ] **Step 3: Type check + smoke**

```bash
cd webapp && npx tsc --noEmit
cd webapp && yarn dev
```

Open the dev URL, in console: `i18n.t('appBypass:title')` should return `"不走代理的应用"`.

- [ ] **Step 4: Commit**

```bash
git add webapp/src/i18n/locales/zh-CN/dashboard.json webapp/src/i18n/locales/namespaces.ts
git commit -m "i18n(zh-CN): add appBypass + advanced-settings-lock keys"
```

---

## Phase 2 — App Bypass store

### Task 2.1: Create app-bypass store with tests

**Files:**
- Create: `webapp/src/stores/app-bypass.store.ts`
- Create: `webapp/src/stores/__tests__/app-bypass.store.test.ts`

- [ ] **Step 1: Write failing tests first**

```ts
// webapp/src/stores/__tests__/app-bypass.store.test.ts
import { describe, it, expect, beforeEach, vi } from 'vitest';
import { useAppBypassStore } from '../app-bypass.store';

const mockStorage = {
  get: vi.fn(),
  set: vi.fn(),
  remove: vi.fn(),
  has: vi.fn(),
  keys: vi.fn(),
  clear: vi.fn(),
};

beforeEach(() => {
  vi.clearAllMocks();
  mockStorage.get.mockResolvedValue(null);
  mockStorage.set.mockResolvedValue(undefined);
  (window as any)._platform = { storage: mockStorage };
  useAppBypassStore.setState({ entries: [], loaded: false });
});

describe('app-bypass store', () => {
  it('load() reads from _platform.storage and parses v1 shape', async () => {
    mockStorage.get.mockResolvedValueOnce({
      v: 1,
      entries: [{ id: 'com.test', label: 'Test', kind: 'package', names: ['com.test'], addedAt: 1 }],
    });
    await useAppBypassStore.getState().load();
    expect(useAppBypassStore.getState().entries).toHaveLength(1);
    expect(useAppBypassStore.getState().loaded).toBe(true);
    expect(mockStorage.get).toHaveBeenCalledWith('k2.advanced.app_bypass');
  });

  it('load() tolerates missing/corrupt data', async () => {
    mockStorage.get.mockResolvedValueOnce(null);
    await useAppBypassStore.getState().load();
    expect(useAppBypassStore.getState().entries).toEqual([]);
    expect(useAppBypassStore.getState().loaded).toBe(true);
  });

  it('load() tolerates wrong schema version', async () => {
    mockStorage.get.mockResolvedValueOnce({ v: 99, entries: [{}] });
    await useAppBypassStore.getState().load();
    expect(useAppBypassStore.getState().entries).toEqual([]);
  });

  it('add() persists and updates state', async () => {
    useAppBypassStore.setState({ entries: [], loaded: true });
    await useAppBypassStore.getState().add({
      id: 'com.test', label: 'Test', kind: 'package', names: ['com.test'],
    });
    expect(useAppBypassStore.getState().entries).toHaveLength(1);
    expect(useAppBypassStore.getState().entries[0].addedAt).toBeGreaterThan(0);
    expect(mockStorage.set).toHaveBeenCalledWith(
      'k2.advanced.app_bypass',
      expect.objectContaining({ v: 1, entries: expect.any(Array) })
    );
  });

  it('add() de-duplicates by id', async () => {
    useAppBypassStore.setState({
      entries: [{ id: 'com.test', label: 'Test', kind: 'package', names: ['com.test'], addedAt: 1 }],
      loaded: true,
    });
    await useAppBypassStore.getState().add({
      id: 'com.test', label: 'Test 2', kind: 'package', names: ['com.test'],
    });
    expect(useAppBypassStore.getState().entries).toHaveLength(1);
  });

  it('remove() filters by id and persists', async () => {
    useAppBypassStore.setState({
      entries: [
        { id: 'a', label: 'A', kind: 'package', names: ['a'], addedAt: 1 },
        { id: 'b', label: 'B', kind: 'package', names: ['b'], addedAt: 2 },
      ],
      loaded: true,
    });
    await useAppBypassStore.getState().remove('a');
    expect(useAppBypassStore.getState().entries.map(e => e.id)).toEqual(['b']);
    expect(mockStorage.set).toHaveBeenCalled();
  });

  it('clear() empties entries and persists', async () => {
    useAppBypassStore.setState({
      entries: [{ id: 'a', label: 'A', kind: 'package', names: ['a'], addedAt: 1 }],
      loaded: true,
    });
    await useAppBypassStore.getState().clear();
    expect(useAppBypassStore.getState().entries).toEqual([]);
  });

  it('rescan() updates names of a single entry', async () => {
    useAppBypassStore.setState({
      entries: [{ id: '/Apps/Chrome.app', label: 'Chrome', kind: 'process', names: ['Chrome'], addedAt: 1 }],
      loaded: true,
    });
    await useAppBypassStore.getState().rescan('/Apps/Chrome.app', ['Chrome', 'Chrome Helper']);
    expect(useAppBypassStore.getState().entries[0].names).toEqual(['Chrome', 'Chrome Helper']);
  });

  it('save failure logs and keeps in-memory state unchanged', async () => {
    mockStorage.set.mockRejectedValueOnce(new Error('disk full'));
    useAppBypassStore.setState({ entries: [], loaded: true });
    await expect(
      useAppBypassStore.getState().add({ id: 'a', label: 'A', kind: 'package', names: ['a'] })
    ).rejects.toThrow();
    expect(useAppBypassStore.getState().entries).toEqual([]);
  });
});
```

- [ ] **Step 2: Run tests to verify they fail**

```bash
cd webapp && npx vitest run src/stores/__tests__/app-bypass.store.test.ts
```

Expected: 9 failing tests with "Cannot find module" or similar.

- [ ] **Step 3: Implement the store**

```ts
// webapp/src/stores/app-bypass.store.ts
import { create } from 'zustand';

export interface AppBypassEntry {
  id: string;
  label: string;
  kind: 'process' | 'package';
  names: string[];
  iconUrl?: string;
  addedAt: number;
}

interface AppBypassStorageShape {
  v: 1;
  entries: AppBypassEntry[];
}

const STORAGE_KEY = 'k2.advanced.app_bypass';

interface AppBypassState {
  entries: AppBypassEntry[];
  loaded: boolean;
}

interface AppBypassActions {
  load(): Promise<void>;
  add(entry: Omit<AppBypassEntry, 'addedAt'>): Promise<void>;
  remove(id: string): Promise<void>;
  clear(): Promise<void>;
  /** rescan: replace names of one entry (by id) with a fresh helper-name set */
  rescan(id: string, names: string[]): Promise<void>;
}

async function persist(entries: AppBypassEntry[]): Promise<void> {
  const payload: AppBypassStorageShape = { v: 1, entries };
  await window._platform.storage.set(STORAGE_KEY, payload);
}

export const useAppBypassStore = create<AppBypassState & AppBypassActions>()((set, get) => ({
  entries: [],
  loaded: false,

  async load() {
    try {
      const stored = await window._platform.storage.get<AppBypassStorageShape>(STORAGE_KEY);
      if (stored && stored.v === 1 && Array.isArray(stored.entries)) {
        set({ entries: stored.entries, loaded: true });
      } else {
        set({ entries: [], loaded: true });
      }
    } catch (err) {
      console.warn('[AppBypassStore] load failed:', err);
      set({ entries: [], loaded: true });
    }
  },

  async add(entry) {
    const current = get().entries;
    if (current.some(e => e.id === entry.id)) return;
    const next = [...current, { ...entry, addedAt: Date.now() }];
    await persist(next);
    set({ entries: next });
  },

  async remove(id) {
    const next = get().entries.filter(e => e.id !== id);
    await persist(next);
    set({ entries: next });
  },

  async clear() {
    await persist([]);
    set({ entries: [] });
  },

  async rescan(id, names) {
    const next = get().entries.map(e =>
      e.id === id ? { ...e, names: [...new Set(names)] } : e
    );
    await persist(next);
    set({ entries: next });
  },
}));
```

- [ ] **Step 4: Re-run tests**

```bash
cd webapp && npx vitest run src/stores/__tests__/app-bypass.store.test.ts
```

Expected: 9 PASS.

- [ ] **Step 5: Commit**

```bash
git add webapp/src/stores/app-bypass.store.ts webapp/src/stores/__tests__/app-bypass.store.test.ts
git commit -m "feat(webapp): app-bypass store with persistence + tests"
```

### Task 2.2: Wire app-bypass store into initializeAllStores

**Files:**
- Modify: `webapp/src/stores/index.ts`

- [ ] **Step 1: Export the store**

After the existing `// ============ Config Store ============` block (around line 43), add:

```ts
// ============ App Bypass Store ============
export { useAppBypassStore } from './app-bypass.store';
```

- [ ] **Step 2: Add import for internal use**

After existing internal imports (around line 69), add:

```ts
import { useAppBypassStore } from './app-bypass.store';
```

- [ ] **Step 3: Call load() in initializeAllStores**

Inside `initializeAllStores()` body, after `useConfigStore.getState().loadConfig().then(...)` (around line 87), add:

```ts
useAppBypassStore.getState().load();  // fire-and-forget; sets loaded=true when done
```

- [ ] **Step 4: Run all store tests**

```bash
cd webapp && npx vitest run src/stores/__tests__/
```

Expected: all PASS (no regressions).

- [ ] **Step 5: Commit**

```bash
git add webapp/src/stores/index.ts
git commit -m "feat(webapp): wire app-bypass store into initializeAllStores"
```

---

## Phase 3 — Route emission

### Task 3.1: Add buildBypassRoutes + integrate into buildConnectConfig

**Files:**
- Modify: `webapp/src/stores/config.store.ts`
- Create: `webapp/src/stores/__tests__/build-bypass-routes.test.ts`

- [ ] **Step 1: Write failing tests**

```ts
// webapp/src/stores/__tests__/build-bypass-routes.test.ts
import { describe, it, expect, beforeEach } from 'vitest';
import { buildBypassRoutes } from '../config.store';
import type { AppBypassEntry } from '../app-bypass.store';

beforeEach(() => {
  (window as any)._platform = { os: 'macos' };
});

describe('buildBypassRoutes', () => {
  it('returns [] for empty entries', () => {
    expect(buildBypassRoutes([])).toEqual([]);
  });

  it('emits one process_name route for desktop entries', () => {
    const entries: AppBypassEntry[] = [
      { id: 'a', label: 'A', kind: 'process', names: ['a', 'a-helper'], addedAt: 1 },
      { id: 'b', label: 'B', kind: 'process', names: ['b'], addedAt: 2 },
    ];
    const routes = buildBypassRoutes(entries);
    expect(routes).toEqual([
      { via: 'direct', match: { process_name: ['a', 'a-helper', 'b'] } },
    ]);
  });

  it('emits one package_name route for Android entries', () => {
    const entries: AppBypassEntry[] = [
      { id: 'com.test', label: 'T', kind: 'package', names: ['com.test'], addedAt: 1 },
    ];
    (window as any)._platform = { os: 'android' };
    expect(buildBypassRoutes(entries)).toEqual([
      { via: 'direct', match: { package_name: ['com.test'] } },
    ]);
  });

  it('emits two routes when both kinds present (cross-platform import scenario)', () => {
    const entries: AppBypassEntry[] = [
      { id: 'a', label: 'A', kind: 'process', names: ['a'], addedAt: 1 },
      { id: 'com.b', label: 'B', kind: 'package', names: ['com.b'], addedAt: 2 },
    ];
    const routes = buildBypassRoutes(entries);
    expect(routes).toHaveLength(2);
    expect(routes[0].match.process_name).toEqual(['a']);
    expect(routes[1].match.package_name).toEqual(['com.b']);
  });

  it('dedupes names across entries', () => {
    const entries: AppBypassEntry[] = [
      { id: 'a', label: 'A', kind: 'process', names: ['shared', 'unique-a'], addedAt: 1 },
      { id: 'b', label: 'B', kind: 'process', names: ['shared', 'unique-b'], addedAt: 2 },
    ];
    const routes = buildBypassRoutes(entries);
    expect(routes[0].match.process_name).toEqual(['shared', 'unique-a', 'unique-b']);
  });

  it('returns [] when platform.os is ios (iOS guard)', () => {
    (window as any)._platform = { os: 'ios' };
    const entries: AppBypassEntry[] = [
      { id: 'a', label: 'A', kind: 'process', names: ['a'], addedAt: 1 },
    ];
    expect(buildBypassRoutes(entries)).toEqual([]);
  });
});
```

- [ ] **Step 2: Run tests to verify fail**

```bash
cd webapp && npx vitest run src/stores/__tests__/build-bypass-routes.test.ts
```

Expected: 6 failing with "buildBypassRoutes is not a function" or similar.

- [ ] **Step 3: Implement buildBypassRoutes + wire into buildConnectConfig**

In `webapp/src/stores/config.store.ts`:

3a. Add this exported helper near the top (after other imports):

```ts
import type { AppBypassEntry } from './app-bypass.store';
import type { RouteConfig } from '../types/client-config';

export function buildBypassRoutes(entries: AppBypassEntry[]): RouteConfig[] {
  if (window._platform?.os === 'ios') return [];
  if (entries.length === 0) return [];

  const processNames = [...new Set(
    entries.filter(e => e.kind === 'process').flatMap(e => e.names)
  )];
  const packageNames = [...new Set(
    entries.filter(e => e.kind === 'package').flatMap(e => e.names)
  )];

  const routes: RouteConfig[] = [];
  if (processNames.length > 0) routes.push({ via: 'direct', match: { process_name: processNames } });
  if (packageNames.length > 0) routes.push({ via: 'direct', match: { package_name: packageNames } });
  return routes;
}
```

3b. Inside the existing `buildConnectConfig:` action (around line 421), read bypass entries and prepend:

```ts
buildConnectConfig: (params?: ConnectConfigParams | string) => {
  const { defaultVia, countryVia, country, autoDetect, telemetry } = get();
  // ---- NEW: pull bypass entries (uses module-level import) ----
  const bypassEntries = useAppBypassStore.getState().entries;
  const preset = derivePreset(defaultVia, countryVia);
  const serverUrl = typeof params === 'string' ? params : params?.serverUrl;

  const baseRoutes = buildRoutes(defaultVia, countryVia, country, serverUrl);
  const result: ClientConfig = {
    ...CLIENT_CONFIG_DEFAULTS,
    mode: 'tun',
    log: { ...CLIENT_CONFIG_DEFAULTS.log, level: __K2_BUILD_LOG_LEVEL__ },
    routes: [
      ...buildBypassRoutes(bypassEntries),  // ---- NEW: prepend ----
      ...baseRoutes,
    ],
  };

  if (telemetry.ruleMissEnabled) {
    result.telemetry = { rule_miss: { enabled: true } };
  }

  console.debug('[ConfigStore] buildConnectConfig:'
    + ' preset=' + preset
    + ', defaultVia=' + defaultVia
    + ', countryVia=' + (countryVia ?? 'null')
    + ', country=' + (country ?? 'null')
    + ', autoDetect=' + autoDetect
    + ', routes=' + (result.routes?.length ?? 0)
    + ', bypassEntryCount=' + bypassEntries.length   // ---- NEW: privacy-safe count only ----
    + ', serverUrl=' + (serverUrl ?? 'none')
    + ', logLevel=' + result.log?.level
    + ', mode=' + result.mode
    + ', ruleMissTelemetry=' + telemetry.ruleMissEnabled);
  return result;
},
```

3c. Add the import at the top:

```ts
import { useAppBypassStore } from './app-bypass.store';
```

- [ ] **Step 4: Re-run tests**

```bash
cd webapp && npx vitest run src/stores/__tests__/build-bypass-routes.test.ts src/stores/__tests__/config.store.test.ts
```

Expected: all PASS (build-bypass-routes new tests + config.store existing tests).

- [ ] **Step 5: Commit**

```bash
git add webapp/src/stores/config.store.ts webapp/src/stores/__tests__/build-bypass-routes.test.ts
git commit -m "feat(webapp): prepend bypass routes in buildConnectConfig + tests"
```

---

## Phase 4 — Native bridges (only platforms that passed Phase 0)

> If any platform was cut in Phase 0 Task 0.8, skip its task here.

### Task 4.1: macOS Tauri command `list_running_apps`

**Files:**
- Create: `desktop/src-tauri/src/app_list.rs`
- Modify: `desktop/src-tauri/src/main.rs`
- Modify: `desktop/src-tauri/Cargo.toml`
- Create: `desktop/src-tauri/src/app_list_test.rs` (unit tests)

- [ ] **Step 1: Add Cargo dependencies (macOS)**

Edit `desktop/src-tauri/Cargo.toml` `[target.'cfg(target_os = "macos")'.dependencies]` section:

```toml
[target.'cfg(target_os = "macos")'.dependencies]
libproc = "0.14"
objc2 = "0.5"
objc2-foundation = "0.2"
objc2-app-kit = "0.2"
```

- [ ] **Step 2: Write failing test for pure helper-set extraction**

Create `desktop/src-tauri/src/app_list_test.rs`:

```rust
#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn helpers_under_bundle_collected() {
        // Given a list of (pid, exe_path) and a bundle_url, the helper
        // collector should return basenames of all paths inside that bundle.
        let bundle_url = "/Applications/TestApp.app";
        let pid_paths = vec![
            (100, "/Applications/TestApp.app/Contents/MacOS/TestApp"),
            (101, "/Applications/TestApp.app/Contents/Frameworks/TestApp Helper.app/Contents/MacOS/TestApp Helper"),
            (102, "/Applications/Other.app/Contents/MacOS/Other"),  // not in bundle
        ];
        let helpers = collect_helper_basenames(bundle_url, &pid_paths);
        assert_eq!(helpers, vec!["TestApp".to_string(), "TestApp Helper".to_string()]);
    }

    #[test]
    fn dedupes_helper_names() {
        let bundle_url = "/Applications/Foo.app";
        let pid_paths = vec![
            (100, "/Applications/Foo.app/Contents/MacOS/Foo"),
            (101, "/Applications/Foo.app/Contents/MacOS/Foo"),  // same exe, multiple PIDs
        ];
        let helpers = collect_helper_basenames(bundle_url, &pid_paths);
        assert_eq!(helpers, vec!["Foo".to_string()]);
    }
}
```

- [ ] **Step 3: Create app_list.rs with pure helper + command shell**

```rust
// desktop/src-tauri/src/app_list.rs
use serde::Serialize;

#[derive(Serialize)]
#[serde(rename_all = "camelCase")]
pub struct RunningApp {
    pub id: String,
    pub label: String,
    pub process_names: Vec<String>,
    pub icon_url: Option<String>,
}

/// Pure helper: given a bundle URL and (pid, exe_path) list, return
/// basenames of paths inside the bundle, deduplicated and preserving
/// first-seen order.
fn collect_helper_basenames(bundle_url: &str, pid_paths: &[(i32, &str)]) -> Vec<String> {
    let mut seen = std::collections::HashSet::new();
    let mut out = Vec::new();
    for (_, path) in pid_paths {
        if !path.starts_with(bundle_url) { continue; }
        let basename = std::path::Path::new(path)
            .file_name()
            .and_then(|s| s.to_str())
            .unwrap_or("");
        if basename.is_empty() { continue; }
        if seen.insert(basename.to_string()) {
            out.push(basename.to_string());
        }
    }
    out
}

#[cfg(target_os = "macos")]
mod macos {
    use super::*;
    use libproc::proc_pid;
    use objc2::rc::Id;
    use objc2_app_kit::NSRunningApplication;
    use objc2_app_kit::NSWorkspace;

    pub fn enumerate() -> Result<Vec<RunningApp>, String> {
        let workspace = unsafe { NSWorkspace::sharedWorkspace() };
        let running = unsafe { workspace.runningApplications() };

        // Snapshot all PIDs + exe paths once
        let pids = proc_pid::listpids(proc_pid::ProcType::ProcAllPIDS)
            .map_err(|e| format!("listpids: {e}"))?;
        let pid_paths: Vec<(i32, String)> = pids
            .iter()
            .filter_map(|&pid| {
                proc_pid::pidpath(pid as i32).ok().map(|p| (pid as i32, p))
            })
            .collect();

        let mut out = Vec::new();
        for app in running.iter() {
            let Some(bundle_id) = (unsafe { app.bundleIdentifier() }) else { continue; };
            let Some(label) = (unsafe { app.localizedName() }) else { continue; };
            let Some(bundle_url) = (unsafe { app.bundleURL() }) else { continue; };
            let bundle_id_str = bundle_id.to_string();
            let label_str = label.to_string();
            let bundle_url_path = unsafe { bundle_url.path() }.map(|s| s.to_string()).unwrap_or_default();

            let pid_path_refs: Vec<(i32, &str)> = pid_paths.iter().map(|(p, s)| (*p, s.as_str())).collect();
            let process_names = collect_helper_basenames(&bundle_url_path, &pid_path_refs);
            if process_names.is_empty() { continue; }

            let icon_url = format!(
                "kaitu-icon://bundle/{}",
                urlencoding::encode(&bundle_id_str)
            );
            out.push(RunningApp {
                id: bundle_id_str,
                label: label_str,
                process_names,
                icon_url: Some(icon_url),
            });
        }
        Ok(out)
    }
}

#[cfg(target_os = "windows")]
mod windows {
    use super::*;
    use sysinfo::System;

    pub fn enumerate() -> Result<Vec<RunningApp>, String> {
        let mut sys = System::new_all();
        sys.refresh_processes();
        let mut seen_exe: std::collections::HashMap<String, RunningApp> = Default::default();
        for proc in sys.processes().values() {
            let Some(exe_path) = proc.exe().and_then(|p| p.to_str()) else { continue; };
            if seen_exe.contains_key(exe_path) { continue; }
            let basename = std::path::Path::new(exe_path)
                .file_name().and_then(|s| s.to_str()).unwrap_or("").to_string();
            if basename.is_empty() { continue; }
            let icon_url = format!("kaitu-icon://exe/{}", urlencoding::encode(exe_path));
            seen_exe.insert(exe_path.to_string(), RunningApp {
                id: exe_path.to_string(),
                label: basename.clone(),
                process_names: vec![basename],
                icon_url: Some(icon_url),
            });
        }
        Ok(seen_exe.into_values().collect())
    }
}

#[tauri::command]
pub async fn list_running_apps() -> Result<Vec<RunningApp>, String> {
    #[cfg(target_os = "macos")] return macos::enumerate();
    #[cfg(target_os = "windows")] return windows::enumerate();
    #[cfg(not(any(target_os = "macos", target_os = "windows")))]
    return Ok(Vec::new());
}

#[cfg(test)]
mod tests {
    use super::*;
    // (test block from Step 2)
}
```

Add to `Cargo.toml` `[dependencies]`:

```toml
urlencoding = "2.1"
sysinfo = "0.30"  # for Windows; macOS already pulls libproc
```

- [ ] **Step 4: Register command in main.rs**

In `desktop/src-tauri/src/main.rs`, add `mod app_list;` near other mods, and add to `invoke_handler`:

```rust
.invoke_handler(tauri::generate_handler![
    // ... existing commands ...
    app_list::list_running_apps,
])
```

- [ ] **Step 5: Run Rust tests**

```bash
cd desktop/src-tauri && cargo test --target x86_64-apple-darwin app_list -- --nocapture
```

Expected: 2 tests PASS.

- [ ] **Step 6: Smoke compile + invoke from webapp dev**

```bash
make dev-macos
```

In webapp devtools console:

```js
const apps = await window.__TAURI__.core.invoke('list_running_apps');
console.log('Got', apps.length, 'apps. First:', apps[0]);
```

Expected: array with bundle ids + processNames arrays.

- [ ] **Step 7: Commit**

```bash
git add desktop/src-tauri/src/app_list.rs desktop/src-tauri/src/main.rs desktop/src-tauri/Cargo.toml desktop/src-tauri/Cargo.lock
git commit -m "feat(desktop): list_running_apps Tauri command (macOS + Windows)"
```

### Task 4.2: Tauri icon protocol — production handler

**Files:**
- Modify: `desktop/src-tauri/src/icon_protocol.rs` (replace POC stub from Task 0.6)

- [ ] **Step 1: Replace POC stub with real handler**

```rust
// desktop/src-tauri/src/icon_protocol.rs
use tauri::{http::Response, UriSchemeContext, Wry};

#[cfg(target_os = "macos")]
fn icon_for_bundle(bundle_id: &str) -> Option<Vec<u8>> {
    use objc2::rc::Id;
    use objc2_app_kit::NSWorkspace;
    use objc2_foundation::{NSData, NSString};
    let workspace = unsafe { NSWorkspace::sharedWorkspace() };
    let id_ns = NSString::from_str(bundle_id);
    let url = unsafe { workspace.URLForApplicationWithBundleIdentifier(&id_ns) }?;
    let img = unsafe { workspace.iconForFile(&url.path()?) };
    let tiff = unsafe { img.TIFFRepresentation() }?;
    // Convert TIFF -> PNG via NSBitmapImageRep
    use objc2_app_kit::NSBitmapImageRep;
    let rep = unsafe { NSBitmapImageRep::imageRepWithData(&tiff) }?;
    let png_type = objc2_app_kit::NSBitmapImageFileType::PNG;
    let png = unsafe { rep.representationUsingType_properties(png_type, &Default::default()) }?;
    let bytes: &[u8] = unsafe { png.bytes() };
    Some(bytes.to_vec())
}

#[cfg(target_os = "windows")]
fn icon_for_exe(exe_path: &str) -> Option<Vec<u8>> {
    // Use winapi's ExtractIconExW + GdiPlus to PNG-encode
    // For brevity: stub that returns a default app icon PNG asset.
    // Real impl: use `windows` crate's Shell::ExtractIconExW + Gdi+ Bitmap::Save.
    None
}

pub fn handle_kaitu_icon(
    _ctx: UriSchemeContext<Wry>,
    request: tauri::http::Request<Vec<u8>>,
) -> Response<Vec<u8>> {
    let uri = request.uri().to_string();
    // Expected: kaitu-icon://<kind>/<url-encoded-id>
    let payload: Option<Vec<u8>> = uri.strip_prefix("kaitu-icon://").and_then(|rest| {
        let mut parts = rest.splitn(2, '/');
        let kind = parts.next()?;
        let id_encoded = parts.next()?;
        let id = urlencoding::decode(id_encoded).ok()?.into_owned();
        match kind {
            #[cfg(target_os = "macos")]
            "bundle" => icon_for_bundle(&id),
            #[cfg(target_os = "windows")]
            "exe" => icon_for_exe(&id),
            _ => None,
        }
    });

    match payload {
        Some(bytes) => Response::builder()
            .status(200)
            .header("Content-Type", "image/png")
            .header("Cache-Control", "public, max-age=86400")
            .body(bytes)
            .unwrap(),
        None => Response::builder()
            .status(404)
            .body(Vec::new())
            .unwrap(),
    }
}
```

- [ ] **Step 2: Build + smoke**

```bash
cd /Users/david/projects/kaitu-io/k2app && make dev-macos
```

Console:

```js
const img = new Image();
img.src = 'kaitu-icon://bundle/com.apple.Safari';
img.onload = () => console.log('Safari icon:', img.width);
document.body.appendChild(img);
```

Expected: Safari icon renders (likely 32×32 or 128×128 depending on NSImage).

- [ ] **Step 3: Commit**

```bash
git add desktop/src-tauri/src/icon_protocol.rs
git commit -m "feat(desktop): kaitu-icon real handler (macOS bundle icons)"
```

> **Windows icon handler is intentionally stubbed** (returns 404 → webapp falls back to first-letter Avatar). If Windows icon support is required for v1, open follow-up ticket; spec §5.5 allows graceful fallback.

### Task 4.3: Linux daemon `/api/helper` action `app-list-running`

**Files:**
- Modify: `k2/daemon/helper.go`
- Create: `k2/daemon/helper_app_list.go`
- Create: `k2/daemon/helper_app_list_test.go`

- [ ] **Step 1: Write failing test**

```go
// k2/daemon/helper_app_list_test.go
//go:build linux

package daemon

import (
    "testing"
)

func TestParseProcExeBasename(t *testing.T) {
    cases := []struct {
        link string
        want string
    }{
        {"/usr/bin/curl", "curl"},
        {"/usr/local/bin/firefox", "firefox"},
        {"", ""},
        {"/bin/", ""},
    }
    for _, c := range cases {
        got := parseProcExeBasename(c.link)
        if got != c.want {
            t.Errorf("parseProcExeBasename(%q) = %q, want %q", c.link, got, c.want)
        }
    }
}
```

- [ ] **Step 2: Run test to verify fail**

```bash
cd k2 && go test ./daemon/ -run TestParseProcExeBasename
```

Expected: undefined: parseProcExeBasename.

- [ ] **Step 3: Implement helper_app_list.go**

```go
// k2/daemon/helper_app_list.go
//go:build linux

package daemon

import (
    "encoding/json"
    "net/http"
    "os"
    "path/filepath"
    "strconv"
)

type appListRunningResp struct {
    Apps []runningAppJSON `json:"apps"`
}

type runningAppJSON struct {
    ID           string   `json:"id"`
    Label        string   `json:"label"`
    ProcessNames []string `json:"process_names"`
}

func parseProcExeBasename(link string) string {
    if link == "" || link == "/" {
        return ""
    }
    return filepath.Base(filepath.Clean(link))
}

func (d *Daemon) handleAppListRunning(w http.ResponseWriter, r *http.Request) {
    selfPid := os.Getpid()
    entries, err := os.ReadDir("/proc")
    if err != nil {
        d.writeHelperError(w, "READ_PROC", err.Error())
        return
    }
    seen := map[string]runningAppJSON{}
    for _, e := range entries {
        pid, err := strconv.Atoi(e.Name())
        if err != nil || pid == selfPid {
            continue
        }
        link, err := os.Readlink("/proc/" + e.Name() + "/exe")
        if err != nil {
            continue  // permission denied or zombie — skip
        }
        basename := parseProcExeBasename(link)
        if basename == "" {
            continue
        }
        if _, dup := seen[link]; dup {
            continue
        }
        seen[link] = runningAppJSON{
            ID:           link,
            Label:        basename,
            ProcessNames: []string{basename},
        }
    }
    apps := make([]runningAppJSON, 0, len(seen))
    for _, app := range seen {
        apps = append(apps, app)
    }
    json.NewEncoder(w).Encode(map[string]any{
        "code": 0,
        "data": appListRunningResp{Apps: apps},
    })
}
```

- [ ] **Step 4: Route the action in handleHelper**

In `k2/daemon/helper.go`, find the switch over `req.Action` and add:

```go
case "app-list-running":
    d.handleAppListRunning(w, r)
    return
```

- [ ] **Step 5: Run tests**

```bash
cd k2 && go test ./daemon/ -run TestParseProcExeBasename
cd k2 && go test ./daemon/...
```

Expected: PASS.

- [ ] **Step 6: Manual smoke**

```bash
GOOS=linux go build -o /tmp/k2 ./k2/cmd/k2
# (run on linux box)
/tmp/k2 run -c /etc/k2/config.yml &
curl -X POST http://127.0.0.1:1777/api/helper \
  -H 'Content-Type: application/json' \
  -d '{"action":"app-list-running"}'
```

Expected: JSON with `data.apps[*]` array; each entry has `id`, `label`, `process_names`.

- [ ] **Step 7: Commit**

```bash
git add k2/daemon/helper.go k2/daemon/helper_app_list.go k2/daemon/helper_app_list_test.go
cd k2 && git commit -m "feat(daemon): /api/helper app-list-running action (Linux)"
cd ..
git add k2  # k2 submodule reference update
git commit -m "chore: bump k2 submodule — app-list-running daemon helper"
```

### Task 4.4: Android manifest `<queries>` LAUNCHER filter

**Files:**
- Modify: `mobile/android/app/src/main/AndroidManifest.xml`

- [ ] **Step 1: Add `<queries>` element**

In `mobile/android/app/src/main/AndroidManifest.xml`, after the `<uses-permission>` elements and before `<application>`, add:

```xml
<queries>
    <intent>
        <action android:name="android.intent.action.MAIN" />
        <category android:name="android.intent.category.LAUNCHER" />
    </intent>
</queries>
```

- [ ] **Step 2: Build to verify manifest validity**

```bash
cd /Users/david/projects/kaitu-io/k2app && make build-android
```

Expected: build succeeds. Manifest merger should not warn.

- [ ] **Step 3: Commit**

```bash
git add mobile/android/app/src/main/AndroidManifest.xml
git commit -m "feat(mobile): Android manifest <queries> LAUNCHER filter"
```

### Task 4.5: Android K2Plugin.listInstalledApps method

**Files:**
- Modify: `mobile/plugins/k2-plugin/android/src/main/java/io/kaitu/k2plugin/K2Plugin.kt`
- Modify: `mobile/plugins/k2-plugin/src/definitions.ts`
- Modify: `mobile/plugins/k2-plugin/src/web.ts`
- Create: `mobile/plugins/k2-plugin/android/src/test/java/io/kaitu/k2plugin/K2PluginUtilsTest.kt` (extend if exists)

- [ ] **Step 1: Add TypeScript definition**

In `mobile/plugins/k2-plugin/src/definitions.ts`, add to the `K2PluginInterface`:

```ts
listInstalledApps(): Promise<{ apps: Array<{ packageName: string; label: string; iconUrl?: string }> }>;
```

In `mobile/plugins/k2-plugin/src/web.ts`, add a stub that throws "unavailable on web".

- [ ] **Step 2: Implement Kotlin method**

In `K2Plugin.kt`, add:

```kotlin
@PluginMethod
fun listInstalledApps(call: PluginCall) {
    try {
        val pm = context.packageManager
        val intent = Intent(Intent.ACTION_MAIN).addCategory(Intent.CATEGORY_LAUNCHER)
        val resolved = pm.queryIntentActivities(intent, 0)
        val seen = mutableSetOf<String>()
        val apps = JSArray()
        for (info in resolved) {
            val appInfo = info.activityInfo.applicationInfo
            val pkg = appInfo.packageName ?: continue
            if (pkg == context.packageName) continue
            if (!seen.add(pkg)) continue
            val label = pm.getApplicationLabel(appInfo).toString()
            val iconUrl = "kaitu-icon://package/" + java.net.URLEncoder.encode(pkg, "UTF-8")
            val entry = JSObject().apply {
                put("packageName", pkg)
                put("label", label)
                put("iconUrl", iconUrl)
            }
            apps.put(entry)
        }
        call.resolve(JSObject().apply { put("apps", apps) })
    } catch (e: Exception) {
        call.reject("LIST_INSTALLED_FAILED", e)
    }
}
```

Imports: `android.content.Intent`, `com.getcapacitor.JSArray`, `com.getcapacitor.JSObject`.

- [ ] **Step 3: Add JVM unit test for label/iconUrl shape**

Use mock `PackageManager` or test the deterministic parts (e.g., `iconUrl` formatter) in a pure helper. If full mocking is heavy for v1, ship without unit test here and rely on manual smoke in Step 5.

- [ ] **Step 4: Rebuild plugin + sync**

```bash
cd mobile/plugins/k2-plugin && npm run build
cd /Users/david/projects/kaitu-io/k2app
rm -rf mobile/node_modules/k2-plugin && yarn install --force
cd mobile && npx cap sync
```

- [ ] **Step 5: Smoke**

```bash
make dev-android
```

In WebView devtools (via `chrome://inspect`):

```js
const { K2Plugin } = await import('k2-plugin');
const res = await K2Plugin.listInstalledApps();
console.log('Got', res.apps.length, 'apps. First few:', res.apps.slice(0, 5));
```

Expected: ≥ 5 user apps with label + iconUrl. Verify icons render via `<img src={app.iconUrl}>`.

- [ ] **Step 6: Commit**

```bash
git add mobile/plugins/k2-plugin/
git commit -m "feat(mobile): K2Plugin.listInstalledApps + icon URL emission"
```

### Task 4.6: Android K2Plugin WebViewClient icon interception (production)

**Files:**
- Modify: `mobile/plugins/k2-plugin/android/src/main/java/io/kaitu/k2plugin/K2Plugin.kt` (replace POC handler from Task 0.7)

- [ ] **Step 1: Replace test-PNG return with real icon extraction**

In the `shouldInterceptRequest` override added during Task 0.7, replace the asset-file body with:

```kotlin
if (url.scheme == "kaitu-icon" && url.host == "package") {
    val packageName = url.pathSegments?.firstOrNull()?.let {
        java.net.URLDecoder.decode(it, "UTF-8")
    } ?: return null
    return try {
        val pm = context.packageManager
        val drawable = pm.getApplicationIcon(packageName)
        val bitmap = drawableToBitmap(drawable, 64, 64)
        val stream = java.io.ByteArrayOutputStream()
        bitmap.compress(android.graphics.Bitmap.CompressFormat.PNG, 100, stream)
        val responseHeaders = mapOf("Cache-Control" to "public, max-age=86400")
        WebResourceResponse(
            "image/png", "binary", 200, "OK", responseHeaders,
            java.io.ByteArrayInputStream(stream.toByteArray())
        )
    } catch (e: Exception) {
        Log.w(TAG, "kaitu-icon failed for $packageName", e)
        null  // 404
    }
}
```

And the `drawableToBitmap` helper:

```kotlin
private fun drawableToBitmap(drawable: android.graphics.drawable.Drawable, w: Int, h: Int): android.graphics.Bitmap {
    if (drawable is android.graphics.drawable.BitmapDrawable && drawable.bitmap != null) {
        return android.graphics.Bitmap.createScaledBitmap(drawable.bitmap, w, h, true)
    }
    val bmp = android.graphics.Bitmap.createBitmap(w, h, android.graphics.Bitmap.Config.ARGB_8888)
    val canvas = android.graphics.Canvas(bmp)
    drawable.setBounds(0, 0, canvas.width, canvas.height)
    drawable.draw(canvas)
    return bmp
}
```

- [ ] **Step 2: Rebuild + smoke**

```bash
cd mobile/plugins/k2-plugin && npm run build && cd /Users/david/projects/kaitu-io/k2app
rm -rf mobile/node_modules/k2-plugin && yarn install --force
cd mobile && npx cap sync
make dev-android
```

In WebView console:

```js
const img = new Image();
img.src = 'kaitu-icon://package/com.android.chrome';
img.onload = () => console.log('Chrome icon:', img.width);
document.body.appendChild(img);
```

Expected: Chrome icon renders.

- [ ] **Step 3: Commit**

```bash
git add mobile/plugins/k2-plugin/
git commit -m "feat(mobile): kaitu-icon real handler via WebViewClient (Android)"
```

---

## Phase 5 — Bridge JS plumbing

### Task 5.1: Wire `_platform.appList` in tauri-k2.ts (macOS / Windows)

**Files:**
- Modify: `webapp/src/services/tauri-k2.ts`

- [ ] **Step 1: Locate the IPlatform construction**

```bash
grep -n "platform\s*=\s*{\|os:\|storage:" webapp/src/services/tauri-k2.ts
```

- [ ] **Step 2: Add appList provider**

Inside the section that constructs `window._platform = { ... }`, add:

```ts
appList: {
  listRunning: async () => {
    const result = await invoke<RunningApp[]>('list_running_apps');
    return result;
  },
},
```

(Import `invoke` from `@tauri-apps/api/core` if not already, and `RunningApp` from `../types/kaitu-core`.)

- [ ] **Step 3: Type check**

```bash
cd webapp && npx tsc --noEmit
```

- [ ] **Step 4: Commit**

```bash
git add webapp/src/services/tauri-k2.ts
git commit -m "feat(webapp): tauri-k2 expose appList.listRunning"
```

### Task 5.2: Wire `_platform.appList` in capacitor-k2.ts (Android)

**Files:**
- Modify: `webapp/src/services/capacitor-k2.ts`

- [ ] **Step 1: Find IPlatform construction**

```bash
grep -n "_platform\s*=\s*{\|Capacitor.getPlatform" webapp/src/services/capacitor-k2.ts | head
```

- [ ] **Step 2: Add appList provider for Android only**

```ts
import { Capacitor } from '@capacitor/core';
import { K2Plugin } from 'k2-plugin';

// inside the platform construction:
appList: Capacitor.getPlatform() === 'android' ? {
  listInstalled: async () => {
    const res = await K2Plugin.listInstalledApps();
    return res.apps;
  },
} : undefined,
```

- [ ] **Step 3: Type check + smoke**

```bash
cd webapp && npx tsc --noEmit
```

- [ ] **Step 4: Commit**

```bash
git add webapp/src/services/capacitor-k2.ts
git commit -m "feat(webapp): capacitor-k2 expose appList.listInstalled (Android only)"
```

### Task 5.3: Wire `_platform.appList` in standalone-k2.ts (Linux desktop branch)

**Files:**
- Modify: `webapp/src/services/standalone-k2.ts`

- [ ] **Step 1: Add Linux desktop branch**

Inside the `_platform = { ... }` construction:

```ts
appList: window._platform?.platformType === 'desktop' ? {
  listRunning: async () => {
    const resp = await fetch('/api/helper', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ action: 'app-list-running' }),
    }).then(r => r.json());
    if (resp.code !== 0) throw new Error(resp.message || 'app-list-running failed');
    // Remap snake_case → camelCase per project convention
    return (resp.data?.apps ?? []).map((a: any) => ({
      id: a.id,
      label: a.label,
      processNames: a.process_names ?? [],
      iconUrl: undefined,  // Linux desktop has no icons in v1
    }));
  },
} : undefined,
```

Note: `_platform.platformType` is set earlier by `/api/platform` fetch (per `k2/webui/CLAUDE.md`). If not yet set at this line, capture from response synchronously or guard. In current standalone-k2.ts the platform shape is built after `/api/platform` is loaded — confirm by reading the file.

- [ ] **Step 2: Type check**

```bash
cd webapp && npx tsc --noEmit
```

- [ ] **Step 3: Commit**

```bash
git add webapp/src/services/standalone-k2.ts
git commit -m "feat(webapp): standalone-k2 expose appList for Linux desktop daemon"
```

---

## Phase 6 — UI

### Task 6.1: ConnectedSettingsLock component

**Files:**
- Create: `webapp/src/components/ConnectedSettingsLock.tsx`
- Create: `webapp/src/components/__tests__/ConnectedSettingsLock.test.tsx`

- [ ] **Step 1: Write failing tests**

```tsx
// webapp/src/components/__tests__/ConnectedSettingsLock.test.tsx
import { describe, it, expect, beforeEach, vi } from 'vitest';
import { render, screen, fireEvent } from '@testing-library/react';
import ConnectedSettingsLock from '../ConnectedSettingsLock';
import { useVPNMachineStore, vpnMachineDispatch } from '../../stores';

vi.mock('../../stores', async () => {
  const actual = await vi.importActual<any>('../../stores');
  return {
    ...actual,
    vpnMachineDispatch: vi.fn(),
  };
});

beforeEach(() => {
  useVPNMachineStore.setState({ state: 'idle' } as any);
  vi.clearAllMocks();
});

describe('ConnectedSettingsLock', () => {
  it('renders children unmodified when vpnState is idle', () => {
    render(<ConnectedSettingsLock><div data-testid="child">x</div></ConnectedSettingsLock>);
    const child = screen.getByTestId('child');
    expect(child).toBeInTheDocument();
    expect(child.parentElement?.style.pointerEvents).toBe('');
  });

  it('renders Alert + locks pointer events when not idle', () => {
    useVPNMachineStore.setState({ state: 'connected' } as any);
    render(<ConnectedSettingsLock><div data-testid="child">x</div></ConnectedSettingsLock>);
    expect(screen.getByText(/请先断开/)).toBeInTheDocument();
    expect(screen.getByText(/断开 VPN/)).toBeInTheDocument();
  });

  it('clicking 断开 VPN dispatches USER_DISCONNECT', () => {
    useVPNMachineStore.setState({ state: 'connected' } as any);
    render(<ConnectedSettingsLock><div /></ConnectedSettingsLock>);
    fireEvent.click(screen.getByText(/断开 VPN/));
    expect(vpnMachineDispatch).toHaveBeenCalledWith('USER_DISCONNECT');
  });
});
```

- [ ] **Step 2: Run failing**

```bash
cd webapp && npx vitest run src/components/__tests__/ConnectedSettingsLock.test.tsx
```

- [ ] **Step 3: Implement**

```tsx
// webapp/src/components/ConnectedSettingsLock.tsx
import { Box, Alert, Button } from '@mui/material';
import { useTranslation } from 'react-i18next';
import { useVPNMachineStore, vpnMachineDispatch } from '../stores';

export default function ConnectedSettingsLock({ children }: { children: React.ReactNode }) {
  const { t } = useTranslation();
  const vpnState = useVPNMachineStore(s => s.state);
  const locked = vpnState !== 'idle';

  if (!locked) return <>{children}</>;

  return (
    <Box>
      <Alert
        severity="info"
        sx={{ mb: 1.5 }}
        action={
          <Button size="small" onClick={() => vpnMachineDispatch('USER_DISCONNECT')}>
            {t('dashboard:disconnectVpn')}
          </Button>
        }
      >
        {t('dashboard:advancedSettingsLocked')}
      </Alert>
      <Box sx={{ pointerEvents: 'none', opacity: 0.45 }}>
        {children}
      </Box>
    </Box>
  );
}
```

- [ ] **Step 4: Run tests pass**

```bash
cd webapp && npx vitest run src/components/__tests__/ConnectedSettingsLock.test.tsx
```

- [ ] **Step 5: Commit**

```bash
git add webapp/src/components/ConnectedSettingsLock.tsx webapp/src/components/__tests__/ConnectedSettingsLock.test.tsx
git commit -m "feat(webapp): ConnectedSettingsLock component"
```

### Task 6.2: Add bypass entry row + wrap Advanced Settings in lock

**Files:**
- Modify: `webapp/src/pages/Dashboard.tsx`

- [ ] **Step 1: Import lock + store + nav**

At top of Dashboard.tsx:

```tsx
import ConnectedSettingsLock from '../components/ConnectedSettingsLock';
import { useAppBypassStore } from '../stores';
import { useNavigate } from 'react-router-dom';
import ChevronRightIcon from '@mui/icons-material/ChevronRight';
```

- [ ] **Step 2: Add entry row, wrap RoutingModeSelector**

Find the existing `<Collapse in={showAdvancedSettings}>` block (around line 620). Replace its inner content with:

```tsx
<Collapse in={showAdvancedSettings}>
  <ConnectedSettingsLock>
    <RoutingModeSelector />
    {appConfig.features.appBypass && window._platform?.appList && (
      <ListItemButton
        onClick={() => navigate('/app-bypass')}
        sx={{ mt: 1.5, borderRadius: 1, border: 1, borderColor: 'divider' }}
      >
        <ListItemText
          primary={t('dashboard:appBypassEntry.label')}
          secondary={
            bypassCount > 0
              ? t('dashboard:appBypassEntry.count', { count: bypassCount })
              : t('dashboard:appBypassEntry.empty')
          }
        />
        <ChevronRightIcon />
      </ListItemButton>
    )}
  </ConnectedSettingsLock>
</Collapse>
```

And declare `const bypassCount = useAppBypassStore(s => s.entries.length);` and `const navigate = useNavigate();` near the top of the component body. Add `ListItemButton, ListItemText` to MUI imports.

- [ ] **Step 3: Type check + dev smoke**

```bash
cd webapp && npx tsc --noEmit && yarn dev
```

Visual: open Dashboard → expand Advanced Settings → see new entry row. Toggle VPN connected via mock → row + RoutingModeSelector locked.

- [ ] **Step 4: Commit**

```bash
git add webapp/src/pages/Dashboard.tsx
git commit -m "feat(webapp): app-bypass entry row + Advanced Settings lock"
```

### Task 6.3: AppBypass page skeleton

**Files:**
- Create: `webapp/src/pages/AppBypass.tsx`

- [ ] **Step 1: Create the page component**

```tsx
// webapp/src/pages/AppBypass.tsx
import { useEffect, useState, useCallback } from 'react';
import { useNavigate } from 'react-router-dom';
import { useTranslation } from 'react-i18next';
import {
  Box, Typography, Stack, Avatar, IconButton, Button, CircularProgress,
} from '@mui/material';
import RefreshIcon from '@mui/icons-material/Refresh';
import ArrowBackIcon from '@mui/icons-material/ArrowBack';
import { useAppBypassStore, useVPNMachineStore, useAlertStore } from '../stores';
import type { RunningApp, InstalledApp } from '../types/kaitu-core';

type Candidate =
  | { kind: 'process'; id: string; label: string; processNames: string[]; iconUrl?: string }
  | { kind: 'package'; id: string; label: string; iconUrl?: string };

export default function AppBypass() {
  const { t } = useTranslation();
  const navigate = useNavigate();
  const entries = useAppBypassStore(s => s.entries);
  const [candidates, setCandidates] = useState<Candidate[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState('');

  // Page-level VPN guard
  useEffect(() => {
    const check = (state: string) => {
      if (state !== 'idle') {
        navigate('/', { replace: true });
        useAlertStore.getState().showInfo(t('dashboard:appBypass.kickedOutDueToConnect'));
      }
    };
    if (useVPNMachineStore.getState().state !== 'idle') {
      check(useVPNMachineStore.getState().state);
      return;
    }
    return useVPNMachineStore.subscribe(s => s.state, check);
  }, [navigate, t]);

  const refresh = useCallback(async () => {
    setLoading(true);
    setError('');
    try {
      const appList = window._platform?.appList;
      if (appList?.listInstalled) {
        const apps = await appList.listInstalled();
        setCandidates(apps.map(a => ({ kind: 'package', id: a.packageName, label: a.label, iconUrl: a.iconUrl })));
      } else if (appList?.listRunning) {
        const apps = await appList.listRunning();
        setCandidates(apps.map(a => ({ kind: 'process', id: a.id, label: a.label, processNames: a.processNames, iconUrl: a.iconUrl })));
      }
    } catch (e) {
      setError(t('dashboard:appBypass.loadFailed'));
    } finally {
      setLoading(false);
    }
  }, [t]);

  useEffect(() => { refresh(); }, [refresh]);

  const addedIds = new Set(entries.map(e => e.id));
  const available = candidates.filter(c => !addedIds.has(c.id));

  return (
    <Box sx={{ p: 2, maxWidth: 700, mx: 'auto' }}>
      <Stack direction="row" alignItems="center" sx={{ mb: 2 }}>
        <IconButton onClick={() => navigate(-1)}><ArrowBackIcon /></IconButton>
        <Typography variant="h6" fontWeight={700} sx={{ flex: 1 }}>
          {t('dashboard:appBypass.title')}
        </Typography>
      </Stack>
      <Typography variant="body2" color="text.secondary" sx={{ mb: 2 }}>
        {t('dashboard:appBypass.description')}
      </Typography>
      {window._platform?.os === 'macos' && (
        <Typography variant="caption" color="text.disabled" sx={{ display: 'block', mb: 2 }}>
          {t('dashboard:appBypass.macMultiUserNote')}
        </Typography>
      )}

      {error && <Typography color="error" sx={{ mb: 1 }}>{error}</Typography>}

      {/* Added section + Available section to be filled in subsequent tasks */}
      <Box>
        <Stack direction="row" alignItems="center" sx={{ mt: 3, mb: 1 }}>
          <Typography variant="subtitle1" sx={{ flex: 1 }}>
            {t('dashboard:appBypass.availableSection')}
          </Typography>
          <IconButton onClick={refresh} size="small" disabled={loading}>
            <RefreshIcon />
          </IconButton>
        </Stack>
        {loading && <CircularProgress size={20} />}
      </Box>
    </Box>
  );
}
```

- [ ] **Step 2: Verify it compiles**

```bash
cd webapp && npx tsc --noEmit
```

- [ ] **Step 3: Commit**

```bash
git add webapp/src/pages/AppBypass.tsx
git commit -m "feat(webapp): AppBypass page skeleton + VPN guard"
```

### Task 6.4: AppBypass list rendering — added + available sections

**Files:**
- Modify: `webapp/src/pages/AppBypass.tsx`

- [ ] **Step 1: Add added-section rendering**

Above the `{/* Added section + Available section ... */}` placeholder, insert:

```tsx
{entries.length > 0 && (
  <Box>
    <Typography variant="subtitle1" sx={{ mt: 2, mb: 1 }}>
      {t('dashboard:appBypass.addedSection', { count: entries.length })}
    </Typography>
    <Stack spacing={1}>
      {entries.map(e => (
        <Stack key={e.id} direction="row" alignItems="center" spacing={1.5} sx={{ p: 1.5, border: 1, borderColor: 'divider', borderRadius: 1 }}>
          <Avatar src={e.iconUrl} variant="rounded" sx={{ width: 32, height: 32 }}>
            {e.label[0]?.toUpperCase()}
          </Avatar>
          <Box sx={{ flex: 1, minWidth: 0 }}>
            <Typography variant="body2" fontWeight={600} noWrap>{e.label}</Typography>
            <Typography variant="caption" color="text.secondary">
              {t('dashboard:appBypass.processCount', { count: e.names.length })}
            </Typography>
          </Box>
          <Button size="small" color="error" onClick={() => useAppBypassStore.getState().remove(e.id)}>
            ✕
          </Button>
        </Stack>
      ))}
    </Stack>
  </Box>
)}
```

- [ ] **Step 2: Add available-section rendering**

Replace the `{loading && <CircularProgress size={20} />}` line with:

```tsx
{loading ? (
  <CircularProgress size={20} />
) : (
  <Stack spacing={1}>
    {available.map(c => (
      <Stack key={c.id} direction="row" alignItems="center" spacing={1.5} sx={{ p: 1.5, border: 1, borderColor: 'divider', borderRadius: 1 }}>
        <Avatar src={c.iconUrl} variant="rounded" sx={{ width: 32, height: 32 }}>
          {c.label[0]?.toUpperCase()}
        </Avatar>
        <Box sx={{ flex: 1, minWidth: 0 }}>
          <Typography variant="body2" fontWeight={600} noWrap>{c.label}</Typography>
        </Box>
        <Button
          size="small"
          variant="outlined"
          onClick={() => {
            if (c.kind === 'process') {
              useAppBypassStore.getState().add({ id: c.id, label: c.label, kind: 'process', names: c.processNames, iconUrl: c.iconUrl });
            } else {
              useAppBypassStore.getState().add({ id: c.id, label: c.label, kind: 'package', names: [c.id], iconUrl: c.iconUrl });
            }
          }}
        >
          +
        </Button>
      </Stack>
    ))}
  </Stack>
)}
```

- [ ] **Step 3: Type check + dev smoke**

```bash
cd webapp && npx tsc --noEmit && yarn dev
```

Manually navigate to `/app-bypass` (you can add a temporary link or hardcode `features.appBypass = true` in apps.ts).

- [ ] **Step 4: Commit**

```bash
git add webapp/src/pages/AppBypass.tsx
git commit -m "feat(webapp): AppBypass list rendering — added + available"
```

### Task 6.5: AppBypass manual-add dialog

**Files:**
- Modify: `webapp/src/pages/AppBypass.tsx`

- [ ] **Step 1: Add dialog state + UI**

Inside `AppBypass` component, add state:

```tsx
const [manualOpen, setManualOpen] = useState(false);
const [manualInput, setManualInput] = useState('');
const isAndroid = window._platform?.os === 'android';
```

Update the "available" section header to include a "+ 手动添加" button:

```tsx
<Stack direction="row" alignItems="center" sx={{ mt: 3, mb: 1 }}>
  <Typography variant="subtitle1" sx={{ flex: 1 }}>
    {t('dashboard:appBypass.availableSection')}
  </Typography>
  <Button size="small" onClick={() => setManualOpen(true)}>
    {t('dashboard:appBypass.manualAdd')}
  </Button>
  <IconButton onClick={refresh} size="small" disabled={loading}>
    <RefreshIcon />
  </IconButton>
</Stack>
```

Add the dialog at the bottom of the page's JSX:

```tsx
<Dialog
  open={manualOpen}
  onClose={() => {
    if (manualInput !== '' && !confirm(t('dashboard:appBypass.manualAddUnsavedConfirm'))) return;
    setManualOpen(false);
    setManualInput('');
  }}
  maxWidth="xs"
  fullWidth
>
  <DialogTitle>{t('dashboard:appBypass.manualAddTitle')}</DialogTitle>
  <DialogContent>
    <TextField
      autoFocus
      fullWidth
      value={manualInput}
      onChange={e => setManualInput(e.target.value)}
      placeholder={t('dashboard:appBypass.manualAddPlaceholder')}
    />
  </DialogContent>
  <DialogActions>
    <Button onClick={() => { setManualInput(''); setManualOpen(false); }}>
      {t('dashboard:appBypass.manualAddCancel')}
    </Button>
    <Button
      variant="contained"
      disabled={manualInput.trim() === ''}
      onClick={async () => {
        const v = manualInput.trim();
        const kind: 'process' | 'package' = isAndroid ? 'package' : 'process';
        await useAppBypassStore.getState().add({
          id: 'manual:' + v,
          label: v,
          kind,
          names: [v],
        });
        setManualInput('');
        setManualOpen(false);
      }}
    >
      {t('dashboard:appBypass.manualAddConfirm')}
    </Button>
  </DialogActions>
</Dialog>
```

Add imports: `Dialog, DialogTitle, DialogContent, DialogActions, TextField`.

- [ ] **Step 2: Type check + dev smoke**

```bash
cd webapp && npx tsc --noEmit && yarn dev
```

Click "+ 手动添加", enter a process name, confirm it appears in the added section.

- [ ] **Step 3: Commit**

```bash
git add webapp/src/pages/AppBypass.tsx
git commit -m "feat(webapp): AppBypass manual-add dialog"
```

### Task 6.6: AppBypass per-entry rescan button (desktop only)

**Files:**
- Modify: `webapp/src/pages/AppBypass.tsx`

- [ ] **Step 1: Add rescan handler + button**

Inside the entries map (Task 6.4 Step 1), for entries where `kind === 'process'` and the platform has `listRunning`, add a refresh icon button next to the ✕:

```tsx
{e.kind === 'process' && window._platform?.appList?.listRunning && (
  <IconButton
    size="small"
    title={t('dashboard:appBypass.rescan')}
    onClick={async () => {
      try {
        const running = await window._platform!.appList!.listRunning!();
        const match = running.find(r => r.id === e.id);
        if (!match) return;
        await useAppBypassStore.getState().rescan(e.id, match.processNames);
        useAlertStore.getState().showSuccess(
          t('dashboard:appBypass.rescanResult', { count: match.processNames.length })
        );
      } catch (err) {
        useAlertStore.getState().showError(t('dashboard:appBypass.loadFailed'));
      }
    }}
  >
    <RefreshIcon fontSize="small" />
  </IconButton>
)}
```

- [ ] **Step 2: Dev smoke**

```bash
cd webapp && yarn dev
```

Open Chrome (or any running app); add to bypass; click rescan; verify toast says "已更新 (共 N 个进程)".

- [ ] **Step 3: Commit**

```bash
git add webapp/src/pages/AppBypass.tsx
git commit -m "feat(webapp): AppBypass per-entry rescan button"
```

### Task 6.7: Register `/app-bypass` route

**Files:**
- Modify: `webapp/src/App.tsx`

- [ ] **Step 1: Import the page (lazy)**

In `App.tsx`, near other lazy imports:

```tsx
const AppBypass = lazy(() => import('./pages/AppBypass'));
```

- [ ] **Step 2: Add the route**

Inside `<Routes><Route path="/" element={<Layout/>}>` block, after one of the existing non-tab routes (e.g., after `tunnels`):

```tsx
{appConfig.features.appBypass && (
  <Route path="app-bypass" element={
    <Suspense fallback={null}>
      <AppBypass />
    </Suspense>
  } />
)}
```

- [ ] **Step 3: Dev smoke**

```bash
cd webapp && yarn dev
```

Navigate to `/app-bypass` directly. Page should render.

- [ ] **Step 4: Commit**

```bash
git add webapp/src/App.tsx
git commit -m "feat(webapp): register /app-bypass route"
```

---

## Phase 7 — i18n translations

### Task 7.1: Translate to 6 locales

**Files:**
- Modify: `webapp/src/i18n/locales/{en-US,en-AU,en-GB,ja,zh-TW,zh-HK}/dashboard.json`

- [ ] **Step 1: Copy zh-CN structure to each locale**

For each locale folder, open `dashboard.json` and add the same `advancedSettingsLocked`, `disconnectVpn`, `appBypassEntry` keys (under `dashboard`), and the new top-level `appBypass` object. Use LLM-assisted translation. Recommended values:

| Key | en-US | ja | zh-TW |
|---|---|---|---|
| `advancedSettingsLocked` | "VPN is connected. Disconnect first to change advanced settings." | "VPN 接続中です。詳細設定を変更するには切断してください。" | "VPN 已連線，請先斷開後再修改進階設定" |
| `disconnectVpn` | "Disconnect VPN" | "VPN を切断" | "斷開 VPN" |
| `appBypass.title` | "Apps That Bypass VPN" | "VPN をバイパスするアプリ" | "不走代理的應用" |
| `appBypass.description` | "Traffic from these apps will not go through the VPN. Useful for apps that flag VPN IPs (banking, payment, etc.)." | (translate similarly) | (translate similarly) |
| ...rest | (translate)| ... | ... |

- [ ] **Step 2: Verify locale files have parity with zh-CN**

```bash
cd webapp && for f in en-US en-AU en-GB ja zh-TW zh-HK; do
  node -e "const cn=require('./src/i18n/locales/zh-CN/dashboard.json'); const lo=require('./src/i18n/locales/$f/dashboard.json'); function keys(o,p=''){return Object.keys(o).flatMap(k=>typeof o[k]==='object'?keys(o[k],p+k+'.'):[p+k]);} const cnK=keys(cn); const loK=keys(lo); const missing=cnK.filter(k=>!loK.includes(k)); console.log('$f missing:', missing);"
done
```

Expected: `[]` for each locale.

- [ ] **Step 3: Commit**

```bash
git add webapp/src/i18n/locales/
git commit -m "i18n: translate app-bypass to 6 locales"
```

---

## Phase 8 — Tests

### Task 8.1: E2E Playwright spec (web/Linux path)

**Files:**
- Create: `webapp/e2e/app-bypass.spec.ts`

- [ ] **Step 1: Write the spec**

```ts
// webapp/e2e/app-bypass.spec.ts
import { test, expect } from '@playwright/test';

test('app-bypass entry visible + navigates to page', async ({ page }) => {
  await page.goto('/');
  await page.getByText('高级设置').click();
  await expect(page.getByText('不走代理的应用')).toBeVisible();
  await page.getByText('不走代理的应用').click();
  await expect(page).toHaveURL(/\/app-bypass/);
  await expect(page.getByRole('heading', { name: '不走代理的应用' })).toBeVisible();
});

test('manual-add flow adds entry and persists in count', async ({ page }) => {
  await page.goto('/app-bypass');
  await page.getByText('+ 手动添加').click();
  await page.getByPlaceholder('如 chrome.exe').fill('test-process.exe');
  await page.getByText('添加').click();
  await expect(page.getByText('test-process.exe')).toBeVisible();
  await page.goto('/');
  await page.getByText('高级设置').click();
  await expect(page.getByText('1 个')).toBeVisible();
});

test('Advanced Settings locks when VPN connected', async ({ page }) => {
  await page.goto('/');
  await page.getByText('高级设置').click();
  // Inject mock VPN connected state
  await page.evaluate(() => {
    (window as any).useVPNMachineStore.setState({ state: 'connected' });
  });
  await expect(page.getByText(/请先断开/)).toBeVisible();
  await expect(page.getByText('断开 VPN')).toBeVisible();
});
```

- [ ] **Step 2: Run**

```bash
cd webapp && npx playwright test e2e/app-bypass.spec.ts
```

Expected: 3 PASS.

- [ ] **Step 3: Commit**

```bash
git add webapp/e2e/app-bypass.spec.ts
git commit -m "test(webapp): app-bypass E2E (entry, manual-add, lock)"
```

### Task 8.2: Privacy invariant test

**Files:**
- Create: `webapp/src/stores/__tests__/app-bypass-privacy.test.ts`

- [ ] **Step 1: Write test**

```ts
// webapp/src/stores/__tests__/app-bypass-privacy.test.ts
import { describe, it, expect, beforeEach, vi } from 'vitest';
import { useAppBypassStore } from '../app-bypass.store';
import { useConfigStore } from '../config.store';

describe('app-bypass privacy invariant', () => {
  beforeEach(() => {
    (window as any)._platform = {
      os: 'macos',
      storage: { get: vi.fn().mockResolvedValue(null), set: vi.fn().mockResolvedValue(undefined) },
    };
    useAppBypassStore.setState({
      entries: [
        { id: 'com.wechat', label: 'WeChat', kind: 'process', names: ['WeChat', 'WeChatAppEx'], addedAt: 1 },
      ],
      loaded: true,
    });
  });

  it('buildConnectConfig console.debug does NOT log entry names', () => {
    const debugSpy = vi.spyOn(console, 'debug').mockImplementation(() => {});
    useConfigStore.getState().buildConnectConfig('k2v5://test@host:443/');
    const allDebugCalls = debugSpy.mock.calls.flat().join(' ');
    expect(allDebugCalls).not.toContain('WeChat');
    expect(allDebugCalls).not.toContain('WeChatAppEx');
    expect(allDebugCalls).toContain('bypassEntryCount=1');
    debugSpy.mockRestore();
  });
});
```

- [ ] **Step 2: Run + commit**

```bash
cd webapp && npx vitest run src/stores/__tests__/app-bypass-privacy.test.ts
git add webapp/src/stores/__tests__/app-bypass-privacy.test.ts
git commit -m "test(webapp): app-bypass privacy invariant (no names in debug logs)"
```

---

## Phase 9 — DoD validation

### Task 9.1: Full-platform build verification

- [ ] **Step 1: Build all platforms**

```bash
cd /Users/david/projects/kaitu-io/k2app
make build-macos K2_BUILD_LOG_LEVEL=info
make build-windows K2_BUILD_LOG_LEVEL=info     # if cross-compilable; else run on Win host
make build-linux K2_BUILD_LOG_LEVEL=info
make build-android K2_BUILD_LOG_LEVEL=info
```

Expected: all builds succeed.

- [ ] **Step 2: Run `scripts/test_build.sh` for the full verification suite**

```bash
scripts/test_build.sh
```

Expected: all 14 checks pass.

### Task 9.2: Manual QA checklist

- [ ] **Run through spec §13 Manual QA items:**

- [ ] macOS: Icon渲染（至少 5 个 entry icon 可见）
- [ ] macOS: Chrome 全 helper 命中（加入 → 连 VPN → tcpdump 公网网卡，浏览 Chrome → 流量出现在公网网卡）
- [ ] macOS/Win/Android: 微信支付不被风控（加微信 → 连 VPN → 扫码支付 → 无风控）
- [ ] 任一平台：删除已卸载 app entry（加入 → 卸载 → 删除 entry 仍可用）
- [ ] macOS: 重新检测（加 Chrome（运行中） → 关 → 重启 → 点 rescan → helper 名集合更新）
- [ ] Android 11/12/13/14：`<queries>` 实测看到 ≥ 30 user apps
- [ ] 任一 desktop：connected-guard 视觉（连 VPN → Section opacity 0.45 + Alert + 按钮，控件不可点）

Document results in PR description.

### Task 9.3: Privacy invariant docs + release notes

**Files:**
- Modify: `webapp/CLAUDE.md` (Domain Vocabulary)
- Create: `docs/release-notes/<next-version>.md` (or update existing release notes file)

- [ ] **Step 1: Add to webapp/CLAUDE.md Domain Vocabulary**

```md
- **App Bypass (Split-Exclude)** — Per-app blacklist routing apps to direct. List
  is stored in `_platform.storage` under `k2.advanced.app_bypass` and never
  appears in logs, uploaded feedback zips, telemetry, or Sentry breadcrumbs.
  See `docs/superpowers/specs/2026-05-12-app-bypass-design.md`.
```

- [ ] **Step 2: Add release notes entry**

```md
### App Bypass (Split-Exclude)

You can now exclude specific apps from the VPN tunnel. Useful for banking,
WeChat, 12306, and similar apps that flag VPN IPs.

**How:** Dashboard → Advanced Settings → "不走代理的应用". Pick from running
processes (macOS / Windows / Linux desktop) or installed apps (Android).

**Note:** The Advanced Settings section now locks while the VPN is connected.
To change rule mode or app bypass, disconnect first. (Previously, changes could
be saved while connected but had no effect — this just makes the behavior
explicit.)

iOS is not supported (OS-level limitation).
```

- [ ] **Step 3: Commit**

```bash
git add webapp/CLAUDE.md docs/release-notes/
git commit -m "docs: app-bypass privacy invariant + release notes"
```

### Task 9.4: Final spec coverage audit

- [ ] **Step 1: Walk through DoD §13 of the spec**

Verify each checkbox can be ticked:
- [ ] Phase 0 §12.2 attribution: ≥3 platforms passed (or cuts documented)
- [ ] Phase 0 §12.3 helper-names: 5 macOS app names in test fixtures
- [ ] Phase 0 §12.4 icon POC: passed (or fallback documented)
- [ ] Unit tests coverage ≥ 80% on new files (`webapp/src/stores/app-bypass.store.ts` + `buildBypassRoutes`)
- [ ] E2E Playwright spec passes
- [ ] 7 locales translated and committed
- [ ] Feature flag `apps.ts features.appBypass` set per build channel
- [ ] Release notes mention RoutingModeSelector lock side effect
- [ ] Privacy invariant documented in `webapp/CLAUDE.md`
- [ ] All-platform builds pass
- [ ] Manual QA documented in PR description

- [ ] **Step 2: Final commit (if any doc updates)**

```bash
git status
# if clean, ready to open PR
```

---

## Self-Review

**Spec coverage:** All sections 2–13 of the spec map to plan tasks:
- §2 Scope → Phase 0 platform cuts + Phase 1 feature flag
- §3 Architecture → Phase 4 native bridges + Phase 5 JS plumbing
- §4 Semantics → Task 3.1 buildBypassRoutes
- §5 UI → Phase 6 (entries 6.1–6.7)
- §6 Storage → Task 2.1
- §7 Native bridges → Tasks 4.1–4.6
- §8 Privacy → Task 8.2 + Task 9.3
- §9 Feature flag → Task 1.2
- §10 Trade-offs (just documentation, no impl tasks needed)
- §11 i18n → Task 1.3 + Task 7.1
- §12 Phase 0 → Tasks 0.1–0.8
- §13 DoD → Task 9.4

**Placeholder scan:** Replaced all "TBD" patterns. Two explicit gaps acknowledged:
- Task 4.2 Windows icon handler is stubbed (returns 404 → fallback). Spec §5.5 allows this.
- Task 0.x results unknown until run — plan handles this with the §12.5 decision protocol.

**Type consistency:** `AppBypassEntry`, `RunningApp`, `InstalledApp`, `IAppListProvider` defined in Task 1.1; used consistently in Tasks 2.1, 3.1, 5.1, 5.2, 5.3, 6.3, 6.4. Function names: `buildBypassRoutes`, `useAppBypassStore`, `ConnectedSettingsLock` — used identically across all references.

---

## Execution Handoff

Plan complete. Save location: `docs/superpowers/plans/2026-05-12-app-bypass-implementation.md`.

Pick execution mode after committing the plan.
