# Android ADB Install Helper Design

Desktop app feature: install Kaitu Android APK to a USB-connected phone via ADB protocol, bypassing Chinese OEM sideloading restrictions.

## Background

Chinese Android phones (Huawei, Xiaomi, OPPO, vivo) actively block sideloaded APK installations with multi-layer security: pure mode cloud auditing, signature database checks, APP filing verification, and antivirus scanning. ADB install bypasses all four layers because it goes through the system-level PackageManager directly.

## Architecture

```
┌──────────────────────────────────────────────────┐
│  Tauri Desktop App                               │
│  ┌──────────┐  ┌──────────┐  ┌────────────────┐ │
│  │ webapp   │  │ k2 daemon│  │ adb (on-demand)│ │
│  │ React UI │──│ :1777    │──│ :5037          │ │
│  └──────────┘  └──────────┘  └────────────────┘ │
│       │              │              │             │
│  AndroidInstall /api/helper    ADB USB protocol  │
│  Stepper UI    actions         to phone          │
└──────────────────────────────────────────────────┘
                       │
                 ┌─────┴─────┐
                 │ S3 / CDN  │
                 │ tools.json│
                 │ latest.json│
                 │ APK + adb │
                 └───────────┘
```

### Data Flow

1. Webapp calls `window._k2.run('adb-detect')` etc. — bridge routes `adb-*` actions to `/api/helper` (not `/api/core`)
2. Helper module downloads adb binary on first use from S3/CDN
3. Helper starts adb server, gadb connects via localhost:5037
4. gadb detects device, pushes APK, executes `pm install`
5. APK source: Kaitu from `android/latest.json`, or direct URL for third-party apps (from discovery)

**Bridge routing**: Webapp NEVER directly `fetch` daemon ports (constitutional rule). All `adb-*` actions go through `_k2.run()`. In `tauri-k2.ts`, the `run()` implementation detects `adb-*` action prefix and invokes `daemon_helper_exec` (Rust command → `POST /api/helper`) instead of `daemon_exec` (→ `POST /api/core`).

### Key Decisions

- **USB only** (no WiFi ADB) — WiFi mode exposes adbd to LAN without authentication, security risk. USB is physical authorization.
- **adb on-demand download** — not embedded in Go binary, not bundled as Tauri sidecar. Downloaded from our S3/CDN on first use. Desktop installer size unchanged.
- **adb binary sourcing** — developer manually extracts adb from Google platform-tools, uploads to S3 via `scripts/upload-adb-tools.sh`. We manage `tools.json` ourselves. adb updates are rare (~2-3x/year), no need for automation.
- **`/api/helper` separate endpoint** — not `/api/core`. Different functional domain (device management vs VPN control).
- **Build tags** — `//go:build !mobile` on all helper code. Never compiled into gomobile appext.
- **V1 Android only** — iOS cannot be sideloaded via similar mechanism. iOS keeps existing QR/link flow on DeviceInstall page.

## S3 Layout

```
d0.all7.cc/kaitu/android/
  latest.json                          # APK manifest (existing)
  0.5.0/Kaitu-0.5.0.apk              # APK (existing)
  tools/
    tools.json                         # adb version + hashes (we manage)
    platform-tools-darwin.zip          # Google 原版 zip 镜像 (含 adb universal binary + 其他工具)
    platform-tools-windows.zip         # Google 原版 zip 镜像 (含 adb.exe + DLLs + 其他工具)
```

### tools.json

We mirror Google's original platform-tools zip files as-is (no modification, no repackaging).
Helper downloads the zip, extracts only the `adb` binary (+ DLLs on Windows), caches locally.

```json
{
  "adb": {
    "version": "35.0.2",
    "files": {
      "darwin":  {"url": "platform-tools-darwin.zip",  "hash": "sha256:...", "size": 15711207},
      "windows": {"url": "platform-tools-windows.zip", "hash": "sha256:...", "size": 7708640}
    }
  }
}
```

`url` values are relative filenames — Go code concatenates with CDN base URL (e.g. `androidEndpoints[i] + "/tools/" + url`).

Helper extracts to `{cacheDir}/tools/adb-{version}/`:
- macOS: `platform-tools/adb` (universal binary)
- Windows: `platform-tools/adb.exe` + `AdbWinApi.dll` + `AdbWinUsbApi.dll`

Zip is deleted after extraction. Only needed files are kept.

### Upload Script

`scripts/upload-adb-tools.sh` — manual one-off script, run when adb version needs updating:

```bash
#!/bin/bash
# Usage: ./scripts/upload-adb-tools.sh <version>
# Prerequisites: download platform-tools zips from Google, place in /tmp/
#
# 1. Extract adb binary from each platform zip
# 2. Rename to our naming convention (adb-{os}-{arch})
# 3. Compute SHA256 hashes
# 4. Generate tools.json
# 5. Upload to S3: s3://d0.all7.cc/kaitu/android/tools/
```

### Local Cache

Uses existing `daemon.cacheDir()` (`~/Library/Caches/k2` on macOS, `%LocalAppData%\k2\cache` on Windows):

```
{cacheDir}/
  tools/
    adb-35.0.2/
      adb                     # macOS: single universal binary
      adb.exe                 # Windows: executable
      AdbWinApi.dll           # Windows: required DLL
      AdbWinUsbApi.dll        # Windows: required DLL
  android/
    0.5.0/Kaitu-0.5.0.apk    # cached APK
```

## API Design

Endpoint: `POST /api/helper`

### adb-detect

Request:
```json
{"action": "adb-detect"}
```

Response:
```json
{
  "code": 0,
  "data": {
    "adb_ready": true,
    "devices": [
      {"serial": "R5CR1234", "state": "device", "model": "SM-G998B"}
    ]
  }
}
```

Device states: `"device"` (authorized), `"unauthorized"` (needs phone tap), `"offline"` (abnormal).

First call triggers adb download if not cached. `adb_ready: false` while downloading, devices empty. Subsequent calls return instantly.

### adb-install

Request:
```json
{"action": "adb-install", "params": {"serial": "R5CR1234", "url": "https://d0.all7.cc/apps/xxx.apk"}}
```

- `serial`: optional when only one device is connected.
- `url`: optional. If omitted, downloads Kaitu APK from `android/latest.json`. If provided, downloads APK from the given URL directly.

Response (immediate, installation runs async):
```json
{"code": 0, "message": "installing"}
```

Only one install at a time. If called while another install is in progress, returns `{"code": 1, "message": "install already in progress"}`.

### adb-status

Request:
```json
{"action": "adb-status"}
```

Response:
```json
{
  "code": 0,
  "data": {
    "phase": "downloading",
    "progress": 45,
    "version": "0.5.0",
    "error": ""
  }
}
```

Phases: `idle` → `prepare_adb` → `downloading` → `pushing` → `installing` → `done` | `error`.

`prepare_adb` phase covers adb download + server startup on first use.
`version` is optional — present for Kaitu (from latest.json), empty for third-party apps (direct URL).

## Go Implementation

### File Structure

```
k2/daemon/
  helper.go              # //go:build !mobile — route registration + handler dispatch
  helper_adb.go          # //go:build !mobile — ADB logic (gadb), download, install
  helper_stub.go         # //go:build mobile  — empty registerHelperRoutes() stub

k2/cmd/k2/
  android_install.go     # CLI subcommand (text-guided mode, direct ADB, no daemon)
```

Note: NOT `helper_android.go` — Go's build system treats `*_android.go` suffix as implicit `GOOS=android` constraint, which is the opposite of intent (this is for desktop only).

### Dependencies

- `github.com/electricbubble/gadb` — pure Go ADB client (TCP to ADB server on :5037)
- No cgo, no libusb, no USB-level code

### Daemon Struct Extension

```go
// daemon.go — add field to Daemon struct
type Daemon struct {
    // ... existing fields ...
    helper *AndroidHelper  // nil on mobile builds (helper_stub.go)
}
```

In `daemon.go Run()`, add after existing route registration:
```go
d.registerHelperRoutes()  // no-op on mobile (helper_stub.go)
```

### helper.go — Route Registration + Initialization

```go
//go:build !mobile

func (d *Daemon) registerHelperRoutes() {
    d.helper = &AndroidHelper{}
    d.mux.HandleFunc("POST /api/helper", d.handleHelper)
}

func (d *Daemon) handleHelper(w http.ResponseWriter, r *http.Request) {
    var req struct {
        Action string         `json:"action"`
        Params map[string]any `json:"params,omitempty"`
    }
    json.NewDecoder(r.Body).Decode(&req)

    switch req.Action {
    case "adb-detect":
        d.helper.handleDetect(w)
    case "adb-install":
        d.helper.handleInstall(w, req.Params)
    case "adb-status":
        d.helper.handleStatus(w)
    default:
        writeJSON(w, Response{Code: 400, Message: "unknown helper action"})
    }
}
```

### helper_stub.go — Mobile Build Stub

```go
//go:build mobile

func (d *Daemon) registerHelperRoutes() {}
// d.helper remains nil; never accessed on mobile.
```

### helper_adb.go — Core Logic

```go
//go:build !mobile

type AndroidHelper struct {
    mu       sync.Mutex
    adbPath  string          // path to cached adb binary
    client   *gadb.Client    // gadb connection to localhost:5037
    state    InstallState    // current install progress
}

type InstallState struct {
    Phase    string `json:"phase"`    // idle/prepare_adb/downloading/pushing/installing/done/error
    Progress int    `json:"progress"` // 0-100
    Version  string `json:"version"`
    Error    string `json:"error"`
}
```

#### adb Preparation

```go
func (h *AndroidHelper) prepareAdb() error {
    // 1. Check local cache {cacheDir}/tools/adb-{version}/
    // 2. If missing:
    //    a. Fetch tools/tools.json from CDN (multi-endpoint fallback)
    //    b. Lookup key: runtime.GOOS ("darwin" or "windows")
    //    c. Download platform-tools zip
    //    d. Verify SHA256 hash of zip
    //    e. Extract only needed files to cache dir:
    //       macOS:   platform-tools/adb
    //       Windows: platform-tools/adb.exe, AdbWinApi.dll, AdbWinUsbApi.dll
    //    f. Delete zip
    // 3. Remove OS quarantine flags:
    //    macOS:   exec xattr -d com.apple.quarantine <path>
    //    Windows: os.Remove(path + ":Zone.Identifier") for each file
    // 4. chmod +x (macOS)
    // 5. Start adb server:
    //    a. Try gadb.NewClient() first (reuse existing server, e.g. Android Studio)
    //    b. If connects, verify with DeviceList() — if protocol error, kill stale server:
    //       exec.Command(adbPath, "kill-server").Run()
    //    c. If no server or killed stale: exec.Command(adbPath, "start-server").Run()
    //    d. Then gadb.NewClient()
}
```

#### Push Progress Tracking

gadb's `Push(io.Reader, ...)` accepts an `io.Reader`. Wrap with a counting reader for progress:

```go
type progressReader struct {
    reader   io.Reader
    total    int64
    current  int64
    callback func(current, total int64)
}

func (pr *progressReader) Read(p []byte) (n int, err error) {
    n, err = pr.reader.Read(p)
    pr.current += int64(n)
    pr.callback(pr.current, pr.total)
    return
}
```

#### Install Flow (async goroutine)

```go
func (h *AndroidHelper) doInstall(serial string, apkURL string) {
    // Phase: prepare_adb
    //   h.prepareAdb() — download adb if needed, start server

    // Phase: downloading
    //   If apkURL == "": fetch android/latest.json, get URL + version + hash, use CDN fallback
    //   If apkURL != "": download directly from the given URL (no hash, trust HTTPS)
    //   Track download progress via Content-Length header + counting reader

    // Phase: pushing
    //   device.Push(progressReader{file, size, callback}, "/data/local/tmp/app.apk", ...)

    // Phase: installing
    //   device.RunShellCommand("pm", "install", "-r", "/data/local/tmp/app.apk")
    //   Parse output for error codes (INSTALL_FAILED_*)

    // Cleanup
    //   device.RunShellCommand("rm", "/data/local/tmp/app.apk")

    // Phase: done (or error with parsed message)
}
```

### CDN Fallback

Reuse existing multi-endpoint pattern from `k2/cmd/k2/upgrade.go`:

```go
var androidEndpoints = []string{
    "https://d13jc1jqzlg4yt.cloudfront.net/kaitu/android",
    "https://d0.all7.cc/kaitu/android",
}
```

Both `tools/tools.json` and `latest.json` try endpoints in order.

## CLI Subcommand

`k2 android-install` — standalone text-guided flow, no daemon required. Directly uses the same AndroidHelper logic.

```
$ k2 android-install

📱 Kaitu Android 安装助手
========================

步骤 1/4: 开启开发者选项
  在手机上: 设置 → 关于手机 → 连续点击7次"版本号"
  完成后按 Enter 继续...

步骤 2/4: 开启 USB 调试
  在手机上: 设置 → 开发者选项 → 开启"USB调试"
  完成后按 Enter 继续...

步骤 3/4: USB 连接
  正在准备工具... 完成
  请用数据线连接手机到电脑...
  检测中... 发现设备: Samsung SM-G998B
  ⚠️  请在手机上点击"允许USB调试"
  检测中... 设备已授权 ✓

步骤 4/4: 安装 Kaitu
  下载最新版本 v0.5.0... 32.5MB [████████░░] 80%
  推送到手机... [██████████] 100%
  安装中...
  ✅ 安装完成！打开手机即可使用 Kaitu。
```

## Webapp UI

### Entry Points

Two entry points:

1. **DeviceInstall.tsx** — add a card for "安卓 USB 安装助手" alongside existing QR/link flow. Only visible on desktop platform. Navigates to `/android-install` (installs Kaitu by default).

2. **Discovery iframe** — via bridge postMessage, navigates to `/android-install` with app params (installs any recommended app).

### Route

```
/android-install?name=AppName&icon=https://...&desc=描述&apk=https://...
```

All params optional. When omitted, defaults to Kaitu (name/icon/desc from app config, APK from `latest.json`).

### Page / Component Split

```
AndroidInstall.tsx (页面 — 组装确定的 props，处理平台检测)
  ├── 非桌面平台 → 显示"请在电脑端使用此功能"
  ├── 有 URL params (apk) → 第三方应用，直接组装 props
  └── 无 URL params → Kaitu 默认安装，apkUrl="" 让 daemon 从 latest.json 获取
  └── 传入确定的 props → <AndroidInstallStepper />

AndroidInstallStepper.tsx (纯 UI 组件 — 零差异逻辑)
  └── props: { name, icon, desc, apkUrl }  ← 总是确定的值
```

**AndroidInstall.tsx** — page, handles data source + platform guard:

```typescript
// webapp/src/pages/AndroidInstall.tsx
interface AppInfo {
  name: string
  icon: string
  desc: string
  apkUrl: string   // "" = daemon uses latest.json (Kaitu), non-empty = direct URL
}

function AndroidInstall() {
  const [searchParams] = useSearchParams()
  const { t } = useTranslation('android-install')
  const platform = window._platform

  // Guard: only available on desktop (daemon required)
  if (!platform || !['macos', 'windows', 'linux'].includes(platform.os)) {
    return <PlatformNotSupported message={t('desktop_only')} />
  }

  const apk = searchParams.get('apk')

  const appInfo: AppInfo = apk
    ? {
        // Third-party app from discovery — all params from URL
        name: searchParams.get('name') || 'Unknown',
        icon: searchParams.get('icon') || '',
        desc: searchParams.get('desc') || '',
        apkUrl: apk,
      }
    : {
        // Default: install Kaitu
        // apkUrl="" tells daemon to fetch from latest.json (no frontend CDN access needed)
        name: 'Kaitu',
        icon: '/images/kaitu-icon.png',
        desc: t('kaitu_desc'),
        apkUrl: '',
      }

  return <AndroidInstallStepper {...appInfo} />
}
```

Note: `apkUrl=""` for Kaitu default mode. The page does NOT fetch `latest.json` from CDN — that would be a cross-origin issue (webapp runs in Tauri webview). Instead, the Go daemon fetches `latest.json` when `adb-install` is called without a URL. This keeps all external HTTP requests in the daemon layer.

**AndroidInstallStepper.tsx** — pure UI component, no data source logic:

```typescript
// webapp/src/components/AndroidInstallStepper.tsx
interface AndroidInstallStepperProps {
  name: string     // always provided — display name
  icon: string     // always provided — icon URL or local path
  desc: string     // always provided — description text
  apkUrl: string   // "" = Kaitu default (daemon fetches latest.json), non-empty = direct URL
}
```

MUI Stepper with 4 steps. Header shows the app name + icon + description.
`serial` is internal state: set in Step 3 when device detected, passed to Step 4's API call.
Progress text uses `name` prop (e.g., "正在下载 Kaitu..."), never depends on `version` from daemon.

**Step 1 — 开发者选项** (manual confirm)
- Brand selector tabs: 华为 / 小米 / OPPO / vivo / 三星 / 其他
- Per-brand screenshot + text instructions
- Button: "我已完成，下一步"

**Step 2 — USB 调试** (manual confirm)
- Same brand-specific layout
- Remind user about the confirmation dialog on phone
- Button: "我已完成，下一步"

**Step 3 — 连接设备** (auto-detect)
- On enter: poll `window._k2.run('adb-detect')` every 2s
- States:
  - Preparing: "正在准备工具..." (adb_ready=false, first-time download)
  - Waiting: "请用数据线连接手机..." (adb_ready=true, no devices)
  - Unauthorized: "检测到设备，请在手机上点击【允许USB调试】" (state=unauthorized)
  - Connected (single): "设备已连接: Samsung SM-G998B" (state=device → auto-advance to Step 4, set `serial` internal state)
  - Multiple devices: show device list as radio buttons (`serial` + `model` from adb-detect), user selects one → set `serial` → advance to Step 4
- Troubleshooting hints after 30s: check cable type, try different USB port, install driver (Windows)

**Step 4 — 自动安装** (fully automatic)
- Call `window._k2.run('adb-install', { serial, url: apkUrl })`
  - `serial`: from Step 3 internal state
  - `url`: from Stepper props.apkUrl ("" for Kaitu, non-empty for third-party)
- Poll `window._k2.run('adb-status')` every 1s
- Linear progress bar + phase text (uses `name` prop, not `version` from daemon):
  - "正在下载 {name}... 45%"
  - "正在推送到手机... 80%"
  - "正在安装..."
  - "安装完成！打开手机即可使用 {name}"
- Failure: error message + retry button

### Discovery iframe Integration

iframe side (kaitu.io):
```typescript
// User clicks "install to Android" on a recommended app
window.parent.postMessage({
  type: 'bridge_navigate',
  path: '/android-install',
  params: {
    name: 'SomeApp',
    icon: 'https://kaitu.io/apps/someapp/icon.png',
    desc: '应用描述',
    apk: 'https://d0.all7.cc/apps/someapp-1.0.apk'
  }
}, '*')
```

Parent side: add `bridge_navigate` to `Discover.tsx`'s existing `window.addEventListener('message')` handler (NOT `useKaituBridge` — that hook uses Tauri IPC `listen()`, not `postMessage`):

```typescript
// Discover.tsx — extend existing handleMessage function
const handleMessage = (event: MessageEvent) => {
  if (event.origin !== 'https://www.kaitu.io') return;

  if (event.data?.type === 'external-link' && event.data?.url) {
    window._platform!.openExternal?.(event.data.url).catch(console.error);
  }

  // New: navigate to internal route (e.g. /android-install)
  if (event.data?.type === 'bridge_navigate' && event.data?.path) {
    const { path, params } = event.data;
    navigate(`${path}?${new URLSearchParams(params)}`);
  }
};
```

This uses the same `postMessage` + origin check pattern as the existing `external-link` handler.

### Frontend API Call Pattern

```typescript
// All adb-* actions go through the bridge (constitutional rule: no direct daemon HTTP)
const res = await window._k2.run('adb-detect')
const res = await window._k2.run('adb-install', { serial, url: apkUrl })
const res = await window._k2.run('adb-status')
```

Bridge routing in `tauri-k2.ts`:
```typescript
run: async <T = any>(action: string, params?: any): Promise<SResponse<T>> => {
  // ...existing code...
  const command = action.startsWith('adb-') ? 'daemon_helper_exec' : 'daemon_exec';
  const response = await invoke<ServiceResponse>(command, { action, params: wrappedParams });
  // ...existing response handling...
}
```

Rust side: `daemon_helper_exec` POSTs to `http://127.0.0.1:{port}/api/helper` (same pattern as `daemon_exec` → `/api/core`).

### i18n

New namespace: `android-install`. Texts for all 4 steps + error messages across 7 locales (zh-CN primary, then en-US, ja, zh-TW, zh-HK, en-AU, en-GB). App-specific text (`name`, `desc`) comes from URL params, not i18n.

### Brand Screenshots

Static assets in `webapp/public/images/android-guide/`:
```
huawei-developer-options.png
huawei-usb-debug.png
xiaomi-developer-options.png
xiaomi-usb-debug.png
oppo-developer-options.png
oppo-usb-debug.png
vivo-developer-options.png
vivo-usb-debug.png
samsung-developer-options.png
samsung-usb-debug.png
```

Content preparation task, independent of code implementation.

## Error Handling

| Scenario | Detection | User Message |
|----------|-----------|-------------|
| Charging-only cable | adb-detect returns empty list | "未检测到设备。请确认使用的是数据线而非充电线，或尝试换USB口" |
| Windows driver missing | adb-detect returns empty on Windows | "未检测到设备。点击安装USB驱动" + download driver |
| Phone unauthorized | device.state = "unauthorized" | "请在手机屏幕上点击【允许USB调试】，建议勾选【始终允许】" |
| Insufficient storage | pm install returns INSTALL_FAILED_INSUFFICIENT_STORAGE | "手机存储空间不足，请清理后重试" |
| Same version installed | pm install -r succeeds (overwrites) | Normal flow, no special handling |
| adb download failed | HTTP error or hash mismatch | "工具下载失败，请检查网络后重试" |
| CDN unreachable | Download timeout | Fallback to secondary CDN endpoint |
| Multiple devices | adb-detect returns >1 device | Show device list, user selects one |
| USB disconnect mid-install | push/pm error | "设备连接中断，请重新插入USB线" → back to Step 3 |
| Existing adb server conflict | gadb connects to wrong/stale server | Try existing server first; if protocol error, kill and restart with our adb |

### Timeouts

| Operation | Timeout | On Timeout |
|-----------|---------|------------|
| adb binary download | 60s | Retry / fallback CDN |
| APK download | 120s | Retry / fallback CDN |
| Push to device | 120s | USB error hint |
| pm install | 60s | Phone confirmation hint |
| Device detection poll | No timeout | User can exit manually |

## Security

- **adb tools trust**: HTTPS transport + SHA256 hash verification (from tools.json).
- **Kaitu APK trust**: HTTPS + SHA256 from `latest.json` + CDN fallback.
- **Third-party APK trust**: HTTPS transport only. No separate hash verification. Trust chain: discovery iframe origin whitelist (kaitu.io only) → bridge postMessage → HTTPS download. Discovery page content is controlled by us.
- **OS quarantine removal**: macOS `xattr -d com.apple.quarantine`, Windows delete `Zone.Identifier` ADS. Applied after download.
- **USB only**: No WiFi ADB. WiFi mode exposes adbd on port 5555 to entire LAN without authentication.
- **No adb in mobile builds**: All helper code uses `//go:build !mobile` tag. Zero inclusion in gomobile appext output.
- **adb server lifecycle**: Helper tries to reuse existing adb server (Android Studio compatibility). Only starts its own if none available.

## Scope

### V1 (this design)
- Android USB install only
- Generic installer: Kaitu (default) + any third-party app via URL params
- 4-step guided flow in webapp + CLI
- Page/component split: AndroidInstall.tsx (data) + AndroidInstallStepper.tsx (UI)
- Discovery iframe integration via `bridge_navigate` postMessage
- On-demand adb download from our S3/CDN (Google original zips mirrored)
- Single device install (serial auto-detected, multi-device shows selector)
- Brand-specific setup guides (screenshots + text)

### Future (not in V1)
- iOS install helper (requires libimobiledevice, different approach)
- WiFi ADB for remote update scenarios
- Batch install to multiple devices
- Auto-update push (detect connected device → check version → prompt upgrade)

## Build Integration: adb Tools Sync

### Goal

`make build-macos` / `make build-windows` automatically ensures adb tools are up-to-date on S3 before building. No manual `upload-adb-tools.sh` step needed. CI does the same check.

### Mechanism

The script compares local adb version (from extracted platform-tools) against remote `tools.json` on S3. If versions match, skip. If local is newer or remote is missing, upload.

### Makefile Target

```makefile
# --- adb tools sync ---
sync-adb-tools:
	@bash scripts/sync-adb-tools.sh

# Add as dependency to desktop builds
build-macos: pre-build build-webapp build-k2-macos sync-adb-tools
	bash scripts/build-macos.sh

build-windows: pre-build build-webapp build-k2-windows sync-adb-tools simplisign-login
	...
```

`sync-adb-tools` runs early and fast (single HTTP HEAD + version compare). Does not block build if S3 is unreachable (warning only).

### scripts/sync-adb-tools.sh

```bash
#!/bin/bash
set -euo pipefail

# --- Config ---
S3_BUCKET="s3://d0.all7.cc/kaitu/android/tools"
CDN_BASE="https://d0.all7.cc/kaitu/android/tools"
LOCAL_ADB_DIR="tools/adb-platform-tools"  # checked into repo or gitignored
TOOLS_JSON="tools.json"

# --- Step 1: Check if local adb binaries exist ---
if [ ! -d "$LOCAL_ADB_DIR" ]; then
    echo "[sync-adb-tools] No local adb binaries found in $LOCAL_ADB_DIR, skipping"
    echo "[sync-adb-tools] To update: download Google platform-tools, extract adb, place in $LOCAL_ADB_DIR/"
    exit 0
fi

# --- Step 2: Read local version ---
LOCAL_VERSION=$(cat "$LOCAL_ADB_DIR/VERSION" 2>/dev/null || echo "unknown")

# --- Step 3: Fetch remote version ---
REMOTE_VERSION=$(curl -sf "$CDN_BASE/$TOOLS_JSON" | python3 -c "import sys,json; print(json.load(sys.stdin)['adb']['version'])" 2>/dev/null || echo "none")

# --- Step 4: Compare ---
if [ "$LOCAL_VERSION" = "$REMOTE_VERSION" ]; then
    echo "[sync-adb-tools] adb tools up-to-date (v$LOCAL_VERSION), skipping upload"
    exit 0
fi

echo "[sync-adb-tools] Local v$LOCAL_VERSION != Remote v$REMOTE_VERSION, uploading..."

# --- Step 5: Compute hashes + generate tools.json ---
json_entry() {
    local file="$1" name="$2"
    local hash size
    hash="sha256:$(shasum -a 256 "$file" | awk '{print $1}')"
    size=$(stat -f%z "$file" 2>/dev/null || stat --printf="%s" "$file")
    echo "\"url\": \"$name\", \"hash\": \"$hash\", \"size\": $size"
}

DARWIN_ZIP="$LOCAL_ADB_DIR/platform-tools-darwin.zip"
WINDOWS_ZIP="$LOCAL_ADB_DIR/platform-tools-windows.zip"

# Google original zips — no modification, mirror as-is
cat > "$LOCAL_ADB_DIR/$TOOLS_JSON" <<EOF
{
  "adb": {
    "version": "$LOCAL_VERSION",
    "files": {
      "darwin":  {$(json_entry "$DARWIN_ZIP" "platform-tools-darwin.zip")},
      "windows": {$(json_entry "$WINDOWS_ZIP" "platform-tools-windows.zip")}
    }
  }
}
EOF

# --- Step 6: Upload to S3 (original zips, unmodified) ---
aws s3 cp "$LOCAL_ADB_DIR/$TOOLS_JSON" "$S3_BUCKET/$TOOLS_JSON" --content-type "application/json"
[ -f "$DARWIN_ZIP" ]  && aws s3 cp "$DARWIN_ZIP"  "$S3_BUCKET/platform-tools-darwin.zip"
[ -f "$WINDOWS_ZIP" ] && aws s3 cp "$WINDOWS_ZIP" "$S3_BUCKET/platform-tools-windows.zip"

echo "[sync-adb-tools] Uploaded adb tools v$LOCAL_VERSION to S3"
```

### Local Directory Structure

```
tools/adb-platform-tools/       # gitignored (zips too large for git)
  VERSION                        # plain text: "35.0.2" (committed to git)
  platform-tools-darwin.zip      # Google 原版，直接从 Google 下载放这里
  platform-tools-windows.zip     # Google 原版，直接从 Google 下载放这里
```

开发者只需：下载 Google zip → 放到这个目录 → 更新 VERSION → 下次构建自动同步。零修改原文件。

### Developer Workflow

1. Download new platform-tools from Google (when adb updates)
2. Extract adb binaries into `tools/adb-platform-tools/`, update `VERSION` file
3. Next `make build-macos` or `make build-windows` auto-detects version mismatch → uploads to S3
4. Done. No separate upload step needed.

### CI Behavior

CI runs `sync-adb-tools` as part of the build. Two scenarios:

- **adb binaries present in CI workspace** (cached or checked out): compares version, uploads if newer
- **adb binaries absent** (typical CI, since gitignored): prints info message, skips gracefully. adb upload is a dev-machine responsibility, not CI's job.

The `sync-adb-tools` target **never fails the build** — S3 upload failure or missing local files produce warnings only (`exit 0`).

### .gitignore Addition

```
# adb platform-tools (Google original zips, managed via S3)
tools/adb-platform-tools/*.zip
```

`VERSION` file IS committed so CI/build can detect when a developer has bumped the version.
