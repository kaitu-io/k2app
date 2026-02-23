# k2app — Kaitu VPN Client

Tauri v2 desktop + Capacitor 6 mobile app wrapping the k2 Go tunnel core. React webapp frontend shared across platforms. Next.js website for marketing, user self-service, and admin management.

## Quick Commands

```bash
cd web && yarn dev               # Next.js website (Turbopack)
cd web && yarn test              # vitest + playwright
make dev                         # k2 daemon + Vite HMR + Tauri window
make build-macos                 # Signed macOS PKG (universal binary)
make build-macos-fast            # Same, skip notarization (local dev)
make build-windows               # Signed Windows NSIS installer
make build-mobile-android        # gomobile bind + cap sync + assembleRelease
make build-mobile-ios            # gomobile bind + cap sync + xcodebuild archive
make publish-mobile VERSION=x.y.z  # Generate + upload mobile latest.json (phase 2 release)
make dev-android                 # gomobile bind + cap sync + cap run android
make dev-ios                     # cap sync + cap run ios
cd webapp && yarn test           # vitest
cd desktop/src-tauri && cargo test  # Rust tests
cd api && go test ./...          # Center API tests
cd tools/kaitu-ops-mcp && npm run build  # Build MCP server (NodeNext ESM)
cd tools/kaitu-ops-mcp && npm test       # vitest for MCP server
scripts/test_build.sh            # Full build verification (14 checks)
yarn install                     # Always run from root (workspace)
```

## Project Structure

```
k2/                  Go core (submodule, read-only — has its own CLAUDE.md)
  engine/            Unified tunnel lifecycle manager (desktop + mobile)
  daemon/            HTTP API shell over engine (desktop only)
  mobile/            gomobile type adapter over engine (mobile only)
webapp/              React + MUI frontend (see webapp/CLAUDE.md)
  src/services/      Cloud API (cloudApi, k2api), auth, caching, platform fallbacks
  src/core/          K2 VPN bridge (getK2, waitForK2, polling)
  src/types/         Core interfaces (IK2Vpn, IPlatform, ISecureStorage)
  src/stores/        Zustand state (vpn, auth, alert, layout, dashboard, login-dialog)
  src/pages/         Route pages (Dashboard, Purchase, Invite, Account, 15+ sub-pages)
  src/components/    Shared UI (LoginDialog, AuthGate, guards, global components)
  src/utils/         Error handling, version compare, tunnel sorting
  src/i18n/          Localization (7 locales, 15+ namespaces)
web/                 Next.js website + admin dashboard (see web/CLAUDE.md)
  src/app/[locale]/  Public pages (install, purchase, account, wallet, changelog)
  src/app/(manager)/ Admin dashboard (users, orders, nodes, tunnels, EDM, cloud)
api/                 Center API service — Go + Gin + GORM (see api/CLAUDE.md)
  cloudprovider/     Multi-cloud VPS management (AWS, Aliyun, Tencent, Bandwagon)
  cmd/               CLI entry point (start, stop, migrate, health-check)
desktop/             Tauri v2 Rust shell (see desktop/CLAUDE.md)
mobile/              Capacitor 6 mobile app
mobile/plugins/      K2Plugin (Swift + Kotlin) — native VPN bridge
mobile/ios/          Xcode project (App + PacketTunnelExtension)
mobile/android/      Gradle project (app module, flatDir AAR)
tools/kaitu-ops-mcp/ MCP server for AI-driven node ops (TypeScript + @modelcontextprotocol/sdk + ssh2)
  src/                 index.ts (entry), config.ts, ssh.ts, redact.ts, center-api.ts, tools/
scripts/             dev.sh, build-macos.sh, build-mobile-*.sh, test_build.sh
docker/scripts/      Node ops scripts (provision-node.sh, enable-ipv6.sh, etc.)
docs/features/       Feature specs and plans
docs/baselines/      Project capability baseline
docs/knowledge/      Distilled patterns (architecture, testing, gotchas, task-splitting, bugfix)
.claude/settings.json  Project-level Claude Code config (MCP server registration)
.claude/skills/      Skill files for Claude Code (kaitu-node-ops.md — node ops safety guardrails)
.github/workflows/   CI (push/PR) + Release Desktop (v* tags) + Release OpenWrt
Makefile             Build orchestration — version from package.json, k2 from submodule
```

## Key Conventions

- **Split globals architecture**: Frontend uses `window._k2` (VPN control) and `window._platform` (platform capabilities). Cloud API via `cloudApi.request()` in `src/services/`.
- **VPN control boundary**: All VPN operations go through `window._k2.run(action, params)`. Never direct HTTP to `:1777` from webapp.
- **Cloud API boundary**: All cloud API calls go through `cloudApi` / `k2api` in `src/services/`. Auth headers and token refresh handled automatically.
- **Error display**: `response.message` is debug-only. Users see i18n text mapped from `response.code`. Never show raw backend messages.
- **Version source of truth**: Root `package.json` (0.4.0). Tauri reads via `../../package.json` reference. k2 binary gets it via ldflags.
- **k2 submodule**: Read-only. Built with `-tags nowebapp` (headless mode). Binary output to `desktop/src-tauri/binaries/`.
- **i18n**: zh-CN primary, en-US secondary, plus ja, zh-TW, zh-HK, en-AU, en-GB. 15+ namespaces. New text goes to zh-CN first.
- **MUI dark theme**: Material-UI 5 with custom theme tokens. No light mode.
- **Webapp subagent tasks**: Always invoke `/word9f-frontend` for frontend decisions.
- **Go→JS JSON key convention**: Go `json.Marshal` outputs snake_case. JS/TS expects camelCase. Native bridge layers (K2Plugin.swift/kt) must remap at boundary.
- **Bridge transformStatus() mandatory**: Every bridge (`tauri-k2.ts`, `capacitor-k2.ts`) must implement `transformStatus()`. No pass-through of raw backend state. Daemon outputs `"stopped"` but webapp expects `"disconnected"`. Error synthesis (`disconnected + error → "error"`) also happens in bridge. Error field is a structured object `{code, message}` (daemon v2+) or string (old daemon — bridge normalizes both to `ControlError`).
- **VPN state contract**: `reconnecting` is a transient engine signal (engine state stays `connected`). `disconnecting` is UI-only optimistic state. Backend never emits either directly. `error` is synthesized by bridge from `disconnected + lastError`.
- **`.gitignore` for native platforms**: Never ignore entire source directories (`mobile/ios/`, `mobile/android/`). Only ignore build artifacts.
- **Capacitor plugin loading**: Use `registerPlugin('K2Plugin')` from `@capacitor/core`. Never dynamic npm import.
- **Capacitor iOS router fix**: `AppBridgeViewController` in `mobile/ios/App/App/` overrides `router()` with `FixedCapacitorRouter` to fix Capacitor 6.x empty-path bug (`URL(fileURLWithPath: "")` resolves to cwd). Main.storyboard must reference this subclass, NOT `CAPBridgeViewController`.
- **gomobile Swift API**: Generated methods use `throws` pattern, NOT NSError out-parameter.
- **iOS extension targets**: Must have `CFBundleExecutable`, `CFBundleVersion` in Info.plist. Build settings NOT inherited from project.
- **Local Capacitor plugin sync**: `file:` plugins are copied to `node_modules/`. After editing: `rm -rf node_modules/k2-plugin && yarn install --force` before `cap sync`.
- **Android VPN prepare context**: `VpnService.prepare()` must use Activity context, not Application context.
- **sing-tun monitor instance sharing**: `NewNetworkMonitor()` returns the same `DefaultInterfaceMonitor` for both `engine.Config.NetworkMonitor` (callback path) and `engine.Config.InterfaceMonitor` (tun.Options self-exclusion). Never split into two instances — sing-tun calls `RegisterMyInterface(tunName)` on the tun.Options instance to exclude TUN-self route changes from triggering reconnect.
- **Network change events are debug-only**: `reconnecting` is a microsecond-duration transient state — the 2s polling loop will never catch it. Events from Android `vpnStateChange` and iOS EventBridge are logged (`console.debug` / `NSLog`) but do NOT update the VPN store. Polling remains the sole UI state source.
- **K2Plugin dual-CDN pattern**: All manifest fetching in K2Plugin (iOS/Android) uses `fetchManifest(endpoints)` helper — ordered array, try CloudFront first, fall back to S3 direct. Returns `(data, baseURL)` tuple. Download URLs are resolved via `resolveDownloadURL(url, baseURL)`: relative paths are prepended with baseURL, absolute `http(s)://` URLs pass through unchanged (backward compat).
- **Mobile two-phase release**: CI (`build-mobile.yml`) uploads artifacts to versioned S3 paths (`s3://kaitu-releases/{channel}/{version}/...`) but never updates `latest.json`. Human runs `make publish-mobile VERSION=x.y.z` after QA to publish the version pointer. Mirror of desktop `publish-release.sh` pattern.
- **K2Plugin definitions.ts rebuild required**: After editing `mobile/plugins/k2-plugin/src/definitions.ts`, run `npm run build` inside the plugin dir BEFORE the standard `rm -rf node_modules/k2-plugin && yarn install --force` step. Without rebuilding dist/, node_modules gets stale type definitions.
- **API file naming**: `api_*.go` handlers, `logic_*.go` business logic, `model*.go` data, `worker_*.go` background jobs, `slave_api*.go` node APIs.
- **API response pattern**: HTTP status always 200. Error state in JSON `code` field. Use `Success()`, `Error()`, `ListWithData()` helpers.
- **NodeNext imports**: `tools/kaitu-ops-mcp/` uses `"module": "NodeNext"`. All relative imports must use `.js` extension in `.ts` source (e.g., `import { x } from './config.js'`). SDK subpath imports also need `.js`: `'@modelcontextprotocol/sdk/server/mcp.js'`.
- **Website pages are Server Components + force-static**: `web/` public pages use `async` Server Components with `export const dynamic = 'force-static'`. Never add `"use client"` to route-level pages in `web/src/app/[locale]/`. Interactive sub-components use Client Component composition. See `web/CLAUDE.md` for conventions.
- **Website namespace registry**: Adding a new `messages/{locale}/*.json` file in `web/` requires adding the namespace name to `web/messages/namespaces.ts`. Missing entry = silent key passthrough.
- **Website routing in locale components**: Use `usePathname` and `Link` from `@/i18n/routing` (NOT `next/navigation`/`next/link`) inside `web/src/app/[locale]/` and `web/src/components/`.
- **macOS NE mode**: On macOS, VPN is managed via Network Extension — no k2 daemon process. `daemon_exec` IPC routes to `ne_action()` in `ne.rs` via `#[cfg(target_os = "macos")]`. Windows/Linux keep daemon HTTP at :1777. The `admin_reinstall_service_macos()` function was removed (dead code since T3 routes macOS to `ne::admin_reinstall_ne()`).

## Tech Stack

- Webapp: React 18, TypeScript, Material-UI 5, Zustand, React Router 7, i18next
- Website: Next.js 15, React 19, Tailwind CSS 4, shadcn/ui, next-intl
- Desktop: Tauri v2, Rust
- Core: Go (k2 submodule)
- API: Go, Gin, GORM, MySQL, Redis, Asynq
- Mobile: Capacitor 6, gomobile bind (K2Plugin Swift/Kotlin)
- Package: yarn workspaces (`webapp`, `desktop`, `mobile`); `web` has independent yarn.lock; `tools/kaitu-ops-mcp` has independent npm
- CI: GitHub Actions (`ci.yml`, `release-desktop.yml`, `build-mobile.yml`, `release-openwrt.yml`)
- Ops MCP: Node.js 22+, TypeScript (NodeNext), `@modelcontextprotocol/sdk`, `ssh2`, `smol-toml`

## Domain Vocabulary

- **IK2Vpn** — VPN control interface (`window._k2`): single `run(action, params)` method
- **IPlatform** — Platform capabilities interface (`window._platform`): storage, UDID, clipboard, openExternal, updater, uploadLogs
- **cloudApi** — Cloud API HTTP module with auth injection and token refresh
- **Engine** — Unified tunnel lifecycle manager (k2/engine/) used by both desktop daemon and mobile wrapper
- **ClientConfig** — Universal config contract: Go `config.ClientConfig` = TS `ClientConfig`. Webapp assembles from Cloud API + user preferences, passes to `_k2.run('up', config)`.
- **Rule mode** — Routing strategy: "global" (proxy all) or "smart" (GeoIP split). Configured via `ClientConfig.rule.global`.
- **Antiblock** — Multi-CDN entry URL resolution for Cloud API in blocked regions
- **AuthGate** — Startup gate: checks service readiness + version match before showing main UI
- **LoginDialog** — Global modal for all auth flows (no `/login` route)
- **Keep-alive tabs** — Tab pages mount once, hide with `visibility:hidden` when inactive
- **Design tokens** — MUI theme + CSS variables for dark-only theme
- **Center** — Backend API service (`api/`): auth, user management, orders, tunnels, cloud management
- **transformStatus()** — Bridge normalization function in `tauri-k2.ts` and `capacitor-k2.ts`. Converts raw backend state to webapp's `StatusResponseData`: normalizes `"stopped"`→`"disconnected"`, synthesizes `"error"` from `disconnected + lastError`, maps timestamp fields. Handles both structured `{code, message}` error objects and legacy string errors.
- **OnNetworkChanged()** — gomobile-exported Engine method (`k2/mobile/mobile.go`) that resets wire connections after network change. Emits transient `"reconnecting"` signal, calls `wire.ResetConnections()`, then `"connected"`. State stays `StateConnected`.
- **Resettable** — Optional Go interface (`k2/wire/transport.go`) that wire implementations can satisfy to support `ResetConnections()`. Used by engine via type assertion.
- **NetworkChangeNotifier** — Optional Go interface (`k2/engine/network.go`) for platform-specific network change detection. Desktop: `singTunMonitor` adapter in `k2/daemon/network_monitor.go` wraps sing-tun `NetworkUpdateMonitor` + `DefaultInterfaceMonitor`. Mobile: platforms call `OnNetworkChanged()` directly from native bridge. Engine starts/closes the monitor as part of lifecycle. Non-fatal if platform doesn't support it.
- **MonitorFactory** — Daemon testability pattern (parallel to `EngineStarter`): `func() (engine.NetworkChangeNotifier, any, error)`. Production default calls `NewNetworkMonitor()`. Tests inject a mock factory. The returned `DefaultInterfaceMonitor` instance MUST be the same instance passed to `engine.Config.InterfaceMonitor` (sing-tun self-exclusion via `RegisterMyInterface`).
- **EngineError** — Structured error type (`k2/engine/error.go`): `{Code int, Message string}`. Produced by `ClassifyError(err error) *EngineError`. HTTP-aligned codes: 400 (BadConfig), 401 (AuthRejected), 403 (Forbidden), 408 (Timeout), 502 (ProtocolError), 503 (ServerUnreachable), 570 (ConnectionFatal/fallback). Priority chain: `net.Error.Timeout()` first, then string patterns, then fallback.
- **vpn-types.ts** — Canonical TS VPN type file (`webapp/src/services/vpn-types.ts`). Replaces old `control-types.ts`. Contains `ControlError`, `StatusResponseData`, `ServiceState`, error code constants, and `getErrorI18nKey()`. Error codes aligned 1:1 with `EngineError` codes from k2 engine.
- **fetchManifest()** — K2Plugin helper (iOS Swift + Android Kotlin) that tries an ordered endpoint array and returns `(manifestData, baseURL)`. CloudFront endpoint is tried first; S3 direct is fallback. 10s connect timeout per endpoint. All K2Plugin update methods share this helper.
- **resolveDownloadURL()** — K2Plugin helper that prepends a CDN base URL to a relative manifest `url` field. If the `url` is already absolute (`http://` or `https://`), it is returned unchanged for backward compatibility.
- **nativeUpdateReady** — Capacitor event emitted by Android K2Plugin when a new APK has been silently downloaded and is ready to install. Payload: `{version, size, path}`. Bridge wires this to `_platform.updater.isUpdateReady = true`.
- **nativeUpdateAvailable** — Capacitor event emitted by iOS K2Plugin when a newer version is found in App Store. Payload: `{version, appStoreUrl}`. Bridge wires this to `_platform.updater.isUpdateReady = true` and stores the App Store URL for `applyUpdateNow()`.
- **publish-mobile.sh** — Manual mobile release script (`scripts/publish-mobile.sh`). Phase 2 of mobile release: validates S3 artifacts exist, downloads, computes sha256+size, generates relative-URL `latest.json`, uploads to `{channel}/latest.json`. Supports `--dry-run` and `--s3-base=PATH` (local mock for testing).
- **kaitu-ops-mcp** — MCP server for AI-driven node operations (`tools/kaitu-ops-mcp/`). TypeScript + `@modelcontextprotocol/sdk`. Two tools: `list_nodes` (Center API discovery via `X-Access-Key`) + `exec_on_node` (SSH direct to nodes). stdout redaction runs on every response.
- **kaitu-node-ops skill** — Claude Code skill file (`.claude/skills/kaitu-node-ops.md`). Dual-architecture identification (k2v5 vs k2-slave), container dependency chain, `.env` variables, standard ops table, 7 safety guardrails. Activated by triggers: "node ops", "k2v5", "exec on node", etc.
- **redactStdout()** — MCP server function (`tools/kaitu-ops-mcp/src/redact.ts`). Strips env-var-style secrets (`KEY_NAME=[REDACTED]`) and 64-char hex strings from SSH stdout before returning to Claude. Technical security backstop for accidental secret exposure.
- **KaituTunnel.appex** — macOS NE App Extension containing PacketTunnelProvider + gomobile engine. Lives in `Kaitu.app/Contents/PlugIns/`. Communicates with main app via NEVPNManager + `sendProviderMessage`.
- **libk2_ne_helper.a** — Swift static library wrapping NEVPNManager for C FFI. Exposes `k2ne_install` / `k2ne_start` / `k2ne_stop` / `k2ne_status` / `k2ne_reinstall` / `k2ne_set_state_callback`. Returns ServiceResponse JSON strings.
- **ne.rs** — Rust NE bridge module (`desktop/src-tauri/src/ne.rs`, macOS only). Routes `daemon_exec` IPC to Swift NE helper via C FFI. Replaces `ensure_service_running` with `ensure_ne_installed` on macOS.
- **ensure_ne_installed** — macOS startup: installs NE VPN profile via `k2ne_install()`. Replaces `ensure_service_running` (which does daemon ping + version check + osascript install). No daemon process required on macOS.

## Layer Docs (read on demand)

```
webapp/CLAUDE.md                    Frontend: split globals, services, stores, i18n, components
web/CLAUDE.md                       Website: Next.js pages, admin dashboard, API proxy
desktop/CLAUDE.md                   Tauri shell, Rust modules, config
api/CLAUDE.md                       Center API: routes, middleware, models, workers, cloudprovider
k2/CLAUDE.md                        Go core architecture, wire protocol, daemon API
docs/knowledge/                     Distilled patterns from all executed features
```

### k2 Submodule Docs (read-only, has its own word9f ecosystem)

```
k2/docs/features/                   Tunnel-level feature specs (8 features)
  cloud-webapp/                     Cloud API integration spec
  mobile-sdk/                       gomobile SDK spec
  private-ip-guard/                 Private IP protection spec
  zero-config-stealth/              Zero-config stealth spec
  logging-and-tun-defaults/         Logging + TUN defaults spec
k2/docs/knowledge/                  Go core patterns (5 files)
  architecture-decisions.md         L4 proxy, wire interfaces, provider callbacks
  bugfix-patterns.md                base64, transport, certs, DNS
  framework-gotchas.md              sing-tun, smux, QUIC, gomobile
  testing-strategies.md             Mocking, E2E, platform tags
  task-splitting.md                 Foundation-first, parallel independence
k2/docs/contracts/                  API contracts
  webapp-daemon-api.md              Daemon HTTP API (POST /api/core actions, CORS, states)
k2/docs/todos/                      k2 backlog (p0/p1/p2 priority)
```
