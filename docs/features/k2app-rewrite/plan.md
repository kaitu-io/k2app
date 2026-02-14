# Plan: k2app Rewrite

## Meta

| Field | Value |
|-------|-------|
| Feature | k2app-rewrite |
| Spec | docs/features/k2app-rewrite/spec.md (v7) |
| Date | 2026-02-14 |
| Complexity | complex (greenfield, >25 files, 3 tech stacks) |
| Scope | Desktop only (Phase 1). Mobile deferred. |

## AC Mapping

| AC | Test | Task |
|----|------|------|
| k2 submodule, `git clone --recursive` | `test_submodule_init.sh` | F1 |
| `make dev` starts daemon + Vite + Tauri, HMR works | `test_dev_mode.sh` | S3 |
| tauri-plugin-localhost configured | Tauri build succeeds + manual verify | S2 |
| Webapp API calls on macOS (no mixed content) | integration: fetch /ping from webview | I1 |
| Tauri IPC available | test: `invoke("get_version")` from webview | S2 |
| k2 binary bundled as externalBin | `test_build_k2.sh`: binary exists at target path | S3 |
| Service readiness loading state | vitest: ServiceReadiness loading → main UI | W1 |
| Antiblock entry URL resolution | vitest: resolveEntry falls back through CDN sources | W1 |
| Cloud API via antiblock entry | vitest: cloudApi.login calls ${entry}/api/auth/login | W1 |
| VpnClient interface with HttpVpnClient | vitest: HttpVpnClient.connect sends correct HTTP | W1 |
| HttpVpnClient poll→event with dedup | vitest: subscribe deduplicates identical states | W1 |
| MockVpnClient injectable for tests | vitest: createVpnClient(mock) returns mock | W1 |
| createVpnClient() auto-selects | vitest: factory returns HttpVpnClient in non-native env | W1 |
| connect()/disconnect() resolve on "command accepted" | vitest: connect resolves before status changes | W1 |
| subscribe() delivers VpnEvent | vitest: subscribe receives state_change events | W1 |
| All UI uses VpnClient, never direct HTTP | code review: no direct fetch to :1777 outside vpn-client/ | W3 |
| Old kaitu-service cleanup | Rust test: `detect_old_service` + cleanup logic | D1 |
| System tray connect/disconnect/quit | manual + tray menu item count | D2 |
| Auto-updater with CDN endpoints | updater config matches endpoints + pubkey | D3 |
| `make build-macos` → signed DMG | CI build + codesign verify | I1 |
| `make build-windows` → signed NSIS | CI build + signtool verify | I1 |
| Version 0.4.0 upgrade from 0.3.22 | updater semver comparison test | D3 |
| Version propagation from package.json | `test_version_propagation.sh` | S3 |

## Dependency Graph

```
F1 ──┬──→ S1 (webapp scaffold) ──┬──→ W1 (VpnClient+antiblock+cloud) ──┬──→ W2 (auth)
     │                           │                                       ├──→ W3 (dashboard)
     │                           │                                       └──→ W4 (servers, also needs W2)
     │                           └──→ W5 (settings+i18n+layout)
     │
     ├──→ S2 (desktop scaffold) ──┬──→ D1 (service manager)
     │                            ├──→ D2 (system tray)
     │                            └──→ D3 (auto-updater)
     │
     └──→ S3 (build system, needs S1+S2)
                                       │
                           all tasks ──→ I1 (E2E verify) ──→ I2 (CI/CD)
```

---

## Foundation Tasks

### F1: Project Bootstrap

**Scope**: Initialize k2app repo, add k2 submodule, create workspace root
**Files**: `package.json`, `.gitignore`, `.gitmodules`, `k2/` (submodule), `CLAUDE.md`
**Depends on**: none

**Steps**:
1. Add k2 as submodule: `git submodule add ../k2 k2`
2. Create root `package.json`:
   ```json
   { "name": "k2app", "version": "0.4.0", "private": true,
     "workspaces": ["webapp", "desktop"] }
   ```
   Note: `mobile` workspace added later when mobile phase begins.
3. Create `.gitignore`:
   ```
   node_modules/
   dist/
   target/
   build/
   desktop/src-tauri/binaries/
   webapp/public/version.json
   *.log
   .DS_Store
   ```
4. Create `CLAUDE.md` with project conventions:
   - k2 is Go submodule at `k2/`, do NOT modify
   - Webapp in `webapp/`, desktop in `desktop/`
   - Build: `make dev` for development, `make build-macos` for release
   - k2 binary built with `-tags nowebapp`
   - Webapp uses VpnClient interface — never direct HTTP to daemon outside vpn-client/
   - webapp subagent tasks should invoke `/word9f-frontend`

**TDD**:
- RED: `test -f k2/go.mod` → fails (no submodule)
- GREEN: Execute steps above → passes
- REFACTOR: n/a (scaffolding)

---

## Feature Tasks

### S1: Webapp Scaffold

**Scope**: Set up React + Vite + Tailwind project in `webapp/`
**Files**: `webapp/**`
**Depends on**: [F1]

**Steps**:
1. `mkdir webapp && cd webapp && yarn init`
2. Install deps:
   - react, react-dom, react-router-dom
   - tailwindcss, @tailwindcss/vite
   - @radix-ui/react-dialog, @radix-ui/react-popover, @radix-ui/react-switch
   - clsx, tailwind-merge, class-variance-authority
   - lucide-react, sonner
   - react-hook-form, @hookform/resolvers, zod
   - zustand
   - i18next, react-i18next, i18next-browser-languagedetector
   - @sentry/react
3. Install devDeps:
   - vite, @vitejs/plugin-react, typescript, @types/react, @types/react-dom
   - vitest, @testing-library/react, @testing-library/jest-dom, jsdom
4. Create `vite.config.ts`:
   - Dev server on port 1420
   - Proxy `/api/*` and `/ping` to `http://127.0.0.1:1777`
5. Create `tsconfig.json`, `index.html`
6. Create `src/lib/cn.ts` (clsx + tailwind-merge utility)
7. Create `src/main.tsx` with minimal React mount
8. Create `src/App.tsx` with placeholder route
9. Verify: `yarn dev` starts on 1420, `yarn build` produces `dist/`

**TDD**:
- RED: `cd webapp && yarn build` → fails (no project)
- GREEN: Scaffold complete → builds to dist/
- REFACTOR: Verify dist/index.html exists, bundle size < 200KB (empty shell)

---

### S2: Desktop Tauri Scaffold

**Scope**: Set up Tauri v2 shell in `desktop/` with plugins and assets from old kaitu
**Files**: `desktop/**`
**Depends on**: [F1]

**Steps**:
1. `mkdir -p desktop/src-tauri/src`
2. Create `desktop/package.json` with `@tauri-apps/cli` devDep
3. Create `desktop/src-tauri/Cargo.toml`:
   ```toml
   [package]
   name = "k2app-desktop"
   version = "0.4.0"
   edition = "2021"

   [dependencies]
   tauri = { version = "2", features = ["tray-icon", "devtools"] }
   tauri-plugin-localhost = "2"
   tauri-plugin-updater = "2"
   tauri-plugin-single-instance = "2"
   tauri-plugin-autostart = "2"
   tauri-plugin-process = "2"
   serde = { version = "1", features = ["derive"] }
   serde_json = "1"
   reqwest = { version = "0.12", features = ["blocking", "json"] }
   log = "0.4"

   [build-dependencies]
   tauri-build = "2"
   ```
4. Create `desktop/src-tauri/tauri.conf.json`:
   ```json
   {
     "productName": "Kaitu",
     "version": "../../package.json",
     "identifier": "io.kaitu.desktop",
     "build": {
       "devUrl": "http://localhost:1420",
       "frontendDist": "../../webapp/dist"
     },
     "app": {
       "withGlobalTauri": true,
       "windows": [{
         "label": "main", "title": "Kaitu.io 开途",
         "width": 430, "height": 956, "minWidth": 320, "minHeight": 568,
         "maxWidth": 480, "center": true, "resizable": true,
         "maximizable": false, "minimizable": false,
         "visible": false, "hiddenTitle": true
       }],
       "security": { "csp": null }
     },
     "bundle": {
       "active": true,
       "targets": ["dmg", "nsis"],
       "icon": [
         "icons/32x32.png", "icons/128x128.png",
         "icons/128x128@2x.png", "icons/icon.icns", "icons/icon.ico"
       ],
       "macOS": {
         "minimumSystemVersion": "12.0",
         "signingIdentity": "Developer ID Application: Wordgate LLC (NJT954Q3RH)",
         "entitlements": "entitlements.plist",
         "hardenedRuntime": true
       },
       "windows": {
         "nsis": { "installMode": "perMachine" }
       },
       "createUpdaterArtifacts": true
     },
     "plugins": {
       "updater": {
         "endpoints": [
           "https://d13jc1jqzlg4yt.cloudfront.net/kaitu/desktop/cloudfront.latest.json",
           "https://d0.all7.cc/kaitu/desktop/d0.latest.json"
         ],
         "pubkey": "dW50cnVzdGVkIGNvbW1lbnQ6IG1pbmlzaWduIHB1YmxpYyBrZXk6IDQwRDc1NDVGRDdDRURFODMKUldTRDNzN1hYMVRYUUxhU2FmRlF5SXljRUdINXYwZDdFT3NQVW1RR0pNUmpuQ3VxcTNlQVZLRUUK"
       }
     }
   }
   ```
5. Create `desktop/src-tauri/tauri.bundle.conf.json`:
   ```json
   { "bundle": { "externalBin": ["binaries/k2"] } }
   ```
6. Copy from old kaitu (`kaitu/client/desktop-tauri/src-tauri/`) → `desktop/src-tauri/`:
   - `keys/` (apple_certificate_base64.txt, private.key, private.key.pub)
   - `icons/` (all icon files)
   - `entitlements.plist`
   - `windows-sign.sh`
   - `installer.nsi`, `installer-hooks.nsh` (adapt binary name: kaitu-service → k2)
   - `nsis-languages/` (English.nsh, SimpChinese.nsh)
7. Create `desktop/src-tauri/build.rs` (standard tauri-build)
8. Create `desktop/src-tauri/src/main.rs`:
   - Minimal Tauri app with tauri-plugin-localhost
   - Window creation (hidden initially, show after ready)
   - Module declarations for service, tray, updater (stubs)
9. Verify: `cd desktop/src-tauri && cargo check` compiles

**TDD**:
- RED: `cargo check` in src-tauri → fails (no project)
- GREEN: Scaffold + `cargo check` passes
- REFACTOR: Verify tauri.conf.json parses, window config correct

---

### S3: Build System

**Scope**: Create Makefile and scripts for dev, build, version propagation
**Files**: `Makefile`, `scripts/**`
**Depends on**: [S1, S2]

**Steps**:
1. Create `Makefile`:
   ```makefile
   VERSION := $(shell node -p "require('./package.json').version")
   COMMIT  := $(shell cd k2 && git rev-parse --short HEAD)

   pre-build:
       mkdir -p webapp/public
       echo '{"version":"$(VERSION)"}' > webapp/public/version.json

   build-k2:
       cd k2 && go build -tags nowebapp \
           -ldflags "-X main.version=$(VERSION) -X main.commit=$(COMMIT)" \
           -o ../desktop/src-tauri/binaries/k2-$(TARGET) ./cmd/k2

   build-webapp:
       cd webapp && yarn build

   build-macos: pre-build build-webapp
       $(MAKE) build-k2 TARGET=aarch64-apple-darwin
       $(MAKE) build-k2 TARGET=x86_64-apple-darwin
       cd desktop && yarn tauri build --target universal-apple-darwin

   build-windows: pre-build build-webapp
       $(MAKE) build-k2 TARGET=x86_64-pc-windows-msvc
       cd desktop && yarn tauri build --target x86_64-pc-windows-msvc

   dev: pre-build
       ./scripts/dev.sh

   clean:
       rm -rf webapp/dist desktop/src-tauri/target desktop/src-tauri/binaries/k2-*
   ```
2. Create `scripts/dev.sh`:
   - Build k2 (nowebapp) if binary missing or outdated
   - Start k2 daemon in background: `k2/build/k2 run &`
   - Start Tauri dev: `cd desktop && yarn tauri dev`
   - Trap SIGINT to kill daemon on exit
3. Create `scripts/build-k2.sh` (wrapper with target triple logic)

**TDD**:
- RED: `make pre-build` → fails (no Makefile)
- GREEN: Makefile works, version.json written correctly
- REFACTOR: Test `VERSION` reads 0.4.0, verify all paths resolve

**Verification script** (`scripts/test_version_propagation.sh`):
```bash
VERSION=$(node -p "require('./package.json').version")
test "$VERSION" = "0.4.0" || exit 1
make pre-build
test "$(jq -r .version webapp/public/version.json)" = "$VERSION" || exit 1
```

---

### D1: Service Manager

**Scope**: Rust module for k2 service lifecycle, version checking, old kaitu-service cleanup
**Files**: `desktop/src-tauri/src/service.rs`
**Depends on**: [S2]

**Reference**: Port from `kaitu/client/desktop-tauri/src-tauri/src/service.rs` with these changes:
- Old kaitu called `/action/{action}` → k2 uses `POST /api/core` with JSON body
- Old kaitu checked `serviceVersion` in status data → k2 uses `action:version` returning `version` field
- Old kaitu called `k2 svc up` → k2 uses `k2 run --install`
- Old kaitu service label unknown → new k2 uses `io.kaitu.k2` (macOS)

**Steps**:
1. Implement `service.rs`:
   - `ping_service()` → GET `http://127.0.0.1:1777/ping`, check code == 0
   - `check_service_version(app_version)`:
     - POST `/api/core` with `{"action":"version"}`
     - Parse response `data.version` field
     - Compare with `versions_match()` (strip build metadata after `+`)
     - Return: VersionMatch / VersionMismatch / ServiceNotRunning
   - `admin_reinstall_service()`:
     - macOS: derive k2 path from Tauri resource dir (same as old kaitu pattern)
       `osascript -e 'do shell script "<k2-path> run --install" with administrator privileges'`
     - Windows: PowerShell `Start-Process -FilePath '<k2.exe>' -ArgumentList 'run','--install' -Verb RunAs`
   - `detect_old_kaitu_service()`:
     - macOS: check `/Library/LaunchDaemons/io.kaitu.k2.plist` (might be old format) +
       check `~/Library/LaunchAgents/` for any kaitu-related plist
     - Windows: `sc query kaitu-service` to detect old service
   - `cleanup_old_kaitu_service()`:
     - macOS: `launchctl unload` + delete old plist
     - Windows: `sc stop kaitu-service && sc delete kaitu-service`
   - `ensure_service_running(app_version)` — main entry point:
     1. Cleanup old kaitu-service if detected
     2. Check k2 service version
     3. If match → done
     4. If mismatch or not running → `admin_reinstall_service()`
     5. Poll ping every 500ms, timeout 5s
     6. Verify version after start
   - `wait_for_service(timeout_ms, poll_interval_ms)` — poll loop
2. Register Tauri commands: `ensure_service_running`, `admin_reinstall_service`
3. Wire into `main.rs` setup

**TDD**:
- RED: test `versions_match("0.4.0", "0.4.0+abc123")` → true; `versions_match("0.4.0", "0.3.22")` → false
- GREEN: Implement version matching + service check logic
- REFACTOR: Ensure platform-specific code is cleanly separated with `#[cfg]`

---

### D2: System Tray

**Scope**: System tray icon with menu for window toggle, connect/disconnect, quit
**Files**: `desktop/src-tauri/src/tray.rs`
**Depends on**: [S2]

**Steps**:
1. Implement `tray.rs`:
   - `init_tray(app_handle)` — create tray with icon + menu
   - Menu items:
     - "Show/Hide Window" — toggle main window visibility
     - Separator
     - "Connect" / "Disconnect" — POST `/api/core` action:up / action:down via reqwest
     - Separator
     - "Quit" — exit app
   - Tray icon: use app icon from `icons/`
   - Click on tray icon: toggle window
2. Wire into `main.rs`: call `init_tray` in setup

Note: Tray uses direct HTTP to daemon (Rust side), not VpnClient. This is fine —
VpnClient constraint applies to webapp TypeScript code only.

**TDD**:
- RED: Build expects `tray` module → no file
- GREEN: Implement tray with menu items, `cargo check` passes
- REFACTOR: Verify menu item structure

---

### D3: Auto-Updater

**Scope**: Tauri updater for seamless 0.3.22 → 0.4.0 upgrade
**Files**: `desktop/src-tauri/src/updater.rs`
**Depends on**: [S2]

**Steps**:
1. Implement `updater.rs`:
   - `check_for_updates(app_handle)` — Tauri updater API
   - Auto-check on startup (after service is running)
   - Show update notification if available
   - Download + install on user confirmation
2. Updater config already in `tauri.conf.json` (S2):
   - Endpoints: CloudFront + d0.all7.cc (same as old kaitu)
   - Public key for signature verification (same as old kaitu)
3. Wire into `main.rs`: spawn updater check after service startup

**TDD**:
- RED: test updater config has valid endpoints → no config
- GREEN: Config in tauri.conf.json, updater module compiles
- REFACTOR: Verify pubkey matches old kaitu's updater key

---

### W1: VpnClient Abstraction + Antiblock + Cloud API + Service Readiness

**Scope**: VpnClient interface with HttpVpnClient, MockVpnClient, antiblock entry resolution,
Cloud API client, service readiness UI. This is the core abstraction that ALL other
webapp tasks depend on.
**Files**: `webapp/src/vpn-client/**`, `webapp/src/api/**`, `webapp/src/stores/vpn.store.ts`, `webapp/src/components/ServiceReadiness.tsx`
**Depends on**: [S1]

**Steps**:
1. Create `webapp/src/vpn-client/types.ts` — all types:
   ```typescript
   export type VpnState = 'stopped' | 'connecting' | 'connected' | 'disconnecting' | 'error';

   export interface VpnStatus {
     state: VpnState;
     connectedAt?: string;
     uptimeSeconds?: number;
     error?: string;
     wireUrl?: string;
   }

   export interface VersionInfo {
     version: string;
     go: string;
     os: string;
     arch: string;
   }

   export interface VpnConfig {
     wireUrl?: string;
     configPath?: string;
   }

   export type ReadyState =
     | { ready: true; version: string }
     | { ready: false; reason: 'not_running' | 'version_mismatch' | 'not_installed' };

   export type VpnEvent =
     | { type: 'state_change'; state: VpnState }
     | { type: 'error'; message: string };

   export interface VpnClient {
     connect(wireUrl: string): Promise<void>;
     disconnect(): Promise<void>;
     checkReady(): Promise<ReadyState>;
     getStatus(): Promise<VpnStatus>;
     getVersion(): Promise<VersionInfo>;
     getUDID(): Promise<string>;
     getConfig(): Promise<VpnConfig>;
     subscribe(listener: (event: VpnEvent) => void): () => void;
     destroy(): void;
   }
   ```

2. Create `webapp/src/vpn-client/http-client.ts` — desktop implementation:
   ```typescript
   export class HttpVpnClient implements VpnClient {
     private baseUrl: string;
     private pollInterval: ReturnType<typeof setInterval> | null = null;
     private lastState: VpnState | null = null;
     private listeners: Set<(event: VpnEvent) => void> = new Set();

     constructor(baseUrl = import.meta.env.DEV ? '' : 'http://127.0.0.1:1777') {
       this.baseUrl = baseUrl;
     }

     // connect() resolves when command accepted (HTTP 200), NOT when VPN is up
     async connect(wireUrl: string): Promise<void> {
       await this.coreAction('up', { wire_url: wireUrl });
     }

     async disconnect(): Promise<void> {
       await this.coreAction('down');
     }

     async checkReady(): Promise<ReadyState> {
       try {
         const ok = await this.ping();
         if (!ok) return { ready: false, reason: 'not_running' };
         const ver = await this.getVersion();
         return { ready: true, version: ver.version };
       } catch {
         return { ready: false, reason: 'not_running' };
       }
     }

     subscribe(listener: (event: VpnEvent) => void): () => void {
       this.listeners.add(listener);
       if (!this.pollInterval) this.startPolling();
       return () => {
         this.listeners.delete(listener);
         if (this.listeners.size === 0) this.stopPolling();
       };
     }

     // Internal: poll status every 2s, emit events on state change (deduplicated)
     private startPolling() { ... }
     private stopPolling() { ... }
     private emit(event: VpnEvent) { ... }
   }
   ```

3. Create `webapp/src/vpn-client/mock-client.ts` — test doubles:
   ```typescript
   export class MockVpnClient implements VpnClient {
     public connectCalls: string[] = [];
     public disconnectCalls: number = 0;
     private statusToReturn: VpnStatus = { state: 'stopped' };
     // ... setters for test control, listener simulation
   }
   ```

4. Create `webapp/src/vpn-client/index.ts` — factory:
   ```typescript
   import { HttpVpnClient } from './http-client';
   import type { VpnClient } from './types';

   let instance: VpnClient | null = null;

   export function createVpnClient(override?: VpnClient): VpnClient {
     if (override) { instance = override; return override; }
     if (!instance) instance = new HttpVpnClient();
     return instance;
   }

   export function getVpnClient(): VpnClient {
     if (!instance) throw new Error('VpnClient not initialized');
     return instance;
   }
   ```
   Note: `NativeVpnClient` (native-client.ts) deferred to mobile phase.

5. Create `webapp/src/api/antiblock.ts` — entry URL resolution:
   ```typescript
   const STORAGE_KEY = 'k2_entry_url';
   const DEFAULT_ENTRY = 'https://w.app.52j.me';
   const CDN_SOURCES = [
     'https://cdn.jsdelivr.net/npm/unlock-it/config.js',
     'https://unpkg.com/unlock-it/config.js',
   ];

   export async function resolveEntry(): Promise<string> {
     const cached = localStorage.getItem(STORAGE_KEY);
     if (cached) {
       refreshEntryInBackground();
       return cached;
     }
     const entry = await fetchEntryFromCDN();
     return entry ?? DEFAULT_ENTRY;
   }

   async function fetchEntryFromCDN(): Promise<string | null> { ... }  // JSONP
   function decodeEntries(encoded: string[]): string[] { ... }  // base64
   ```

6. Create `webapp/src/api/cloud.ts` — Cloud API client:
   ```typescript
   import { resolveEntry } from './antiblock';

   export const cloudApi = {
     getAuthCode: (email: string) => cloudRequest('POST', '/api/auth/code', { email }),
     login: (email: string, code: string, udid: string) =>
       cloudRequest('POST', '/api/auth/login', { email, code, udid }),
     refreshToken: (refreshToken: string) =>
       cloudRequest('POST', '/api/auth/refresh', { refreshToken }),
     getUserInfo: () => cloudRequest('GET', '/api/user/info'),
     getTunnels: () => cloudRequest('GET', '/api/tunnels'),
     getAppConfig: () => cloudRequest('GET', '/api/app/config'),
   };
   ```

7. Create `webapp/src/api/types.ts` — Cloud API types:
   ```typescript
   export interface ApiResponse<T = unknown> {
     code: number;
     message: string;
     data?: T;
   }
   ```

8. Create `webapp/src/stores/vpn.store.ts` — Zustand store wrapping VpnClient:
   ```typescript
   import { getVpnClient } from '../vpn-client';

   export const useVpnStore = create<VpnStore>((set, get) => ({
     state: 'stopped' as VpnState,
     ready: null as ReadyState | null,
     error: null,

     init: async () => {
       const client = getVpnClient();
       const ready = await client.checkReady();
       set({ ready });
       if (ready.ready) {
         client.subscribe((event) => {
           if (event.type === 'state_change') set({ state: event.state });
           if (event.type === 'error') set({ error: event.message });
         });
       }
     },

     connect: async (wireUrl: string) => {
       set({ state: 'connecting' });  // optimistic
       await getVpnClient().connect(wireUrl);
     },

     disconnect: async () => {
       set({ state: 'disconnecting' });  // optimistic
       await getVpnClient().disconnect();
     },
   }));
   ```

9. Create `webapp/src/components/ServiceReadiness.tsx`:
   - Uses `useVpnStore().ready`
   - `{ ready: true }` → render children
   - `{ ready: false, reason: 'not_running' }` → "Starting service..." + Tauri IPC
   - Retry with `vpnClient.checkReady()` every 500ms, timeout 10s
   - Error + "Retry" button on timeout

10. Wire into `App.tsx` as top-level wrapper

**TDD**:
- RED: vitest: `HttpVpnClient.connect('k2v5://...')` sends POST /api/core with action:up
- GREEN: Implement HttpVpnClient
- RED: vitest: `subscribe()` emits state_change event; deduplicates identical states
- GREEN: Implement poll→event with dedup
- RED: vitest: `createVpnClient(mock)` returns the mock; `createVpnClient()` returns HttpVpnClient
- GREEN: Implement factory
- RED: vitest: `resolveEntry()` returns cached → CDN → default fallback chain
- GREEN: Implement antiblock
- RED: vitest: `cloudApi.login()` calls `${entry}/api/auth/login` with correct body
- GREEN: Implement cloud client
- RED: vitest: ServiceReadiness shows loading then content
- GREEN: Implement component with vpn store
- REFACTOR: Ensure types match k2 daemon API exactly

---

### W2: Auth Flow

**Scope**: Login UI, token management, auth store (uses cloud.ts + VpnClient from W1)
**Files**: `webapp/src/pages/Login.tsx`, `webapp/src/stores/auth.store.ts`
**Depends on**: [W1]

**Steps**:
1. Create `webapp/src/stores/auth.store.ts`:
   - `token`, `refreshToken`, `user`, `isLoggedIn`, `login()`, `logout()`
   - Token stored in localStorage
   - `login(email, code)`: `vpnClient.getUDID()` → `cloudApi.login(email, code, udid)` → store tokens
   - Auto-refresh on app startup using stored refreshToken
2. Create `webapp/src/pages/Login.tsx`:
   - Email + verification code form (React Hook Form + Zod)
   - Login button → Cloud API → store token
   - Redirect to dashboard on success

**TDD**:
- RED: vitest: auth store login sets token → no store
- GREEN: Implement store + Cloud API client
- RED: vitest: Login page renders form and submits → no page
- GREEN: Implement Login page
- REFACTOR: Extract form validation schema

---

### W3: Dashboard + VPN Control

**Scope**: Main dashboard with connection button, status display, uptime.
All VPN operations through VpnClient — no direct HTTP calls.
**Files**: `webapp/src/pages/Dashboard.tsx`, `webapp/src/components/ConnectionButton.tsx`
**Depends on**: [W1]

**Steps**:
1. Create `webapp/src/components/ConnectionButton.tsx`:
   - Big connect/disconnect button
   - Uses `useVpnStore()` for state + connect/disconnect actions
   - State-aware: "Connect" / "Connecting..." / "Connected" / "Disconnect"
   - CVA variants for each state
   - Disabled during transitional states (connecting, disconnecting)
2. Create `webapp/src/pages/Dashboard.tsx`:
   - Connection status display (from `useVpnStore().state`)
   - ConnectionButton
   - Uptime counter (when connected, computed from subscribe events)
   - Error display (when error state)

**TDD**:
- RED: vitest: ConnectionButton renders "Connect" in stopped state, "Connected" in connected state
- GREEN: Implement button with variants
- RED: vitest: Dashboard shows error message in error state
- GREEN: Implement Dashboard
- REFACTOR: Extract status display into sub-component

---

### W4: Server List

**Scope**: Server selection and connection
**Files**: `webapp/src/pages/Servers.tsx`, `webapp/src/components/ServerList.tsx`, `webapp/src/stores/servers.store.ts`
**Depends on**: [W1, W2]

**Steps**:
1. Create `webapp/src/stores/servers.store.ts`:
   - `servers`, `selectedServer`, `isLoading`
   - `fetchServers()` → `cloudApi.getTunnels()` (from api/cloud.ts)
   - `selectServer(id)` → stores selection, returns wire_url
2. Create `webapp/src/components/ServerList.tsx`:
   - List of servers with country flags, names, latency
   - Selected server highlighted
   - Click to select → `useVpnStore().connect(wire_url)`
3. Create `webapp/src/pages/Servers.tsx`:
   - ServerList + search/filter
   - Connect action on server select

**TDD**:
- RED: vitest: servers store fetches and stores list → no store
- GREEN: Implement store
- RED: vitest: ServerList renders items → no component
- GREEN: Implement component
- REFACTOR: Add loading skeleton

---

### W5: Settings + i18n + Layout

**Scope**: App layout shell, bottom navigation, i18n, settings page
**Files**: `webapp/src/pages/Settings.tsx`, `webapp/src/i18n/**`, `webapp/src/components/Layout.tsx`, `webapp/src/components/BottomNav.tsx`
**Depends on**: [S1]

**Steps**:
1. Create `webapp/src/i18n/index.ts`:
   - i18next init with zh-CN (default) and en-US
   - Browser language detection
2. Create `webapp/src/i18n/locales/zh-CN/` and `en-US/`:
   - `common.json`, `dashboard.json`, `auth.json`, `settings.json`
   - Start with essential keys only, port from old kaitu translations
3. Create `webapp/src/components/Layout.tsx`:
   - App shell wrapper (header + content + bottom nav)
4. Create `webapp/src/components/BottomNav.tsx`:
   - Dashboard / Servers / Settings tabs
   - React Router NavLink integration
5. Create `webapp/src/pages/Settings.tsx`:
   - Language selector (zh-CN / en-US)
   - App version display (from `/version.json`)
   - About section
6. Update `webapp/src/App.tsx`:
   - Route definitions: `/` → Dashboard, `/servers` → Servers, `/settings` → Settings, `/login` → Login
   - Layout wrapper for authenticated routes
   - Auth guard redirect

**TDD**:
- RED: vitest: i18n returns Chinese string for known key → no setup
- GREEN: Implement i18n with translations
- RED: vitest: Layout renders nav + outlet → no component
- GREEN: Implement Layout + BottomNav
- REFACTOR: Extract route config to constants

---

### I1: End-to-End Build Verification

**Scope**: Full build + dev mode verification on macOS (and Windows CI)
**Files**: `scripts/test_build.sh`
**Depends on**: [D1, D2, D3, W3, W5]

**Steps**:
1. Dev mode: `make dev`
   - Verify Vite starts on 1420
   - Verify k2 daemon starts on 1777
   - Verify Tauri window opens
   - Verify webapp loads with no mixed content errors
   - Verify /ping reaches daemon through Vite proxy
2. Full macOS build: `make build-macos`
   - Verify DMG produced
   - Verify code signing: `codesign --verify --deep`
   - Verify k2 binary inside app bundle
   - Verify webapp assets in app bundle
3. Version check:
   - Verify all version sources read 0.4.0
4. Full Windows build (CI): `make build-windows`
   - Verify NSIS installer produced
   - Verify k2.exe inside installer

**TDD**: Integration test script, no unit tests.

---

### I2: CI/CD Pipeline

**Scope**: GitHub Actions for CI + release builds
**Files**: `.github/workflows/**`
**Depends on**: [I1]

**Steps**:
1. Create `.github/workflows/ci.yml`:
   - Trigger: push to main, PR
   - Jobs: lint (eslint), test (vitest), type-check (tsc), cargo-check
2. Create `.github/workflows/release-desktop.yml`:
   - Trigger: tag `v*` push
   - Matrix: macOS (macos-latest), Windows (windows-latest)
   - Steps per platform:
     - Checkout with submodules (`--recursive`)
     - Setup Node 20, Go 1.24, Rust
     - `yarn install`
     - `make build-macos` or `make build-windows`
     - macOS: import cert, notarize
     - Windows: sign via `windows-sign.sh`
     - Upload artifacts to CDN
     - Update `latest.json` for updater
   - Env secrets: APPLE_CERTIFICATE, APPLE_ID, TAURI_SIGNING_PRIVATE_KEY, etc.
   - Note: Windows cross-compile may need native runner (k2rule limitation)
3. Adapt patterns from old kaitu workflow

**TDD**: Dry-run with `act` or first real tag push.

---

## Execution Order

```
Phase 1: Foundation
  F1 (bootstrap)               → ~0.5 day

Phase 2: Scaffolds (parallel)
  S1 (webapp scaffold)         → ~1 day
  S2 (desktop scaffold)        → ~1-2 days
  S3 (build system, after S1+S2) → ~1 day

Phase 3: Shell + Core (parallel)
  D1, D2, D3 (desktop shell)   → parallel, ~1 day each
  W1 (VpnClient + API)         → ~2 days
  W5 (settings+i18n+layout)    → ~1-2 days

Phase 4: Features (parallel after W1)
  W2, W3, W4                   → parallel, ~1-2 days each

Phase 5: Integration
  I1 (E2E verification)        → ~1 day
  I2 (CI/CD)                   → ~1 day
```

## Execution Notes

- **webapp subagent tasks**: always invoke `/word9f-frontend` for frontend decisions
- **NativeVpnClient** (`native-client.ts`): deferred to mobile phase, not in this plan
- **VpnClient is the boundary**: all webapp→backend communication goes through VpnClient.
  Cloud API (antiblock) is the exception — it goes directly from webapp to Cloud API (no VPN backend involved).

## Out of Scope (Phase 2+)

- Mobile (iOS/Android) — NativeVpnClient + Capacitor Plugin + MobileAPI
- SpeedTest UI — blocked on k2 speedtest mobile support
- Device management page
- Invite system
- Wallet/purchase system
- Advanced settings (developer settings, rules config)
