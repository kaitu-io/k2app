# k2app — Kaitu VPN Client

Tauri v2 desktop + Capacitor 6 mobile app wrapping the k2 Go tunnel core. React webapp frontend shared across platforms.

## Quick Commands

```bash
make dev                         # k2 daemon + Vite HMR + Tauri window
make build-macos                 # Signed macOS PKG (universal binary)
make build-macos-fast            # Same, skip notarization (local dev)
make build-windows               # Signed Windows NSIS installer
make build-mobile-android        # gomobile bind + cap sync + assembleRelease
make build-mobile-ios            # gomobile bind + cap sync + xcodebuild archive
make dev-android                 # gomobile bind + cap sync + cap run android
make dev-ios                     # cap sync + cap run ios
cd webapp && yarn test           # vitest
cd desktop/src-tauri && cargo test  # Rust tests
cd api && go test ./...          # Center API tests
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
api/                 Center API service — Go + Gin + GORM (see api/CLAUDE.md)
  cloudprovider/     Multi-cloud VPS management (AWS, Aliyun, Tencent, Bandwagon)
  cmd/               CLI entry point (start, stop, migrate, health-check)
desktop/             Tauri v2 Rust shell (see desktop/CLAUDE.md)
mobile/              Capacitor 6 mobile app
mobile/plugins/      K2Plugin (Swift + Kotlin) — native VPN bridge
mobile/ios/          Xcode project (App + PacketTunnelExtension)
mobile/android/      Gradle project (app module, flatDir AAR)
scripts/             dev.sh, build-macos.sh, build-mobile-*.sh, test_build.sh
docs/features/       Feature specs and plans
docs/baselines/      Project capability baseline
docs/knowledge/      Distilled patterns (architecture, testing, gotchas, task-splitting, bugfix)
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
- **Bridge transformStatus() mandatory**: Every bridge (`tauri-k2.ts`, `capacitor-k2.ts`) must implement `transformStatus()`. No pass-through of raw backend state. Daemon outputs `"stopped"` but webapp expects `"disconnected"`. Error synthesis (`disconnected + error → "error"`) also happens in bridge.
- **VPN state contract**: `reconnecting` is a transient engine signal (engine state stays `connected`). `disconnecting` is UI-only optimistic state. Backend never emits either directly. `error` is synthesized by bridge from `disconnected + lastError`.
- **`.gitignore` for native platforms**: Never ignore entire source directories (`mobile/ios/`, `mobile/android/`). Only ignore build artifacts.
- **Capacitor plugin loading**: Use `registerPlugin('K2Plugin')` from `@capacitor/core`. Never dynamic npm import.
- **gomobile Swift API**: Generated methods use `throws` pattern, NOT NSError out-parameter.
- **iOS extension targets**: Must have `CFBundleExecutable`, `CFBundleVersion` in Info.plist. Build settings NOT inherited from project.
- **Local Capacitor plugin sync**: `file:` plugins are copied to `node_modules/`. After editing: `rm -rf node_modules/k2-plugin && yarn install --force` before `cap sync`.
- **Android VPN prepare context**: `VpnService.prepare()` must use Activity context, not Application context.
- **API file naming**: `api_*.go` handlers, `logic_*.go` business logic, `model*.go` data, `worker_*.go` background jobs, `slave_api*.go` node APIs.
- **API response pattern**: HTTP status always 200. Error state in JSON `code` field. Use `Success()`, `Error()`, `ListWithData()` helpers.

## Tech Stack

- Frontend: React 18, TypeScript, Material-UI 5, Zustand, React Router 7, i18next
- Desktop: Tauri v2, Rust
- Core: Go (k2 submodule)
- API: Go, Gin, GORM, MySQL, Redis, Asynq
- Mobile: Capacitor 6, gomobile bind (K2Plugin Swift/Kotlin)
- Package: yarn workspaces (`webapp`, `desktop`, `mobile`)
- CI: GitHub Actions (`ci.yml`, `release-desktop.yml`, `build-mobile.yml`, `release-openwrt.yml`)

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
- **transformStatus()** — Bridge normalization function in `tauri-k2.ts` and `capacitor-k2.ts`. Converts raw backend state to webapp's `StatusResponseData`: normalizes `"stopped"`→`"disconnected"`, synthesizes `"error"` from `disconnected + lastError`, maps timestamp fields.
- **OnNetworkChanged()** — gomobile-exported Engine method (`k2/mobile/mobile.go`) that resets wire connections after network change. Emits transient `"reconnecting"` signal, calls `wire.ResetConnections()`, then `"connected"`. State stays `StateConnected`.
- **Resettable** — Optional Go interface (`k2/wire/transport.go`) that wire implementations can satisfy to support `ResetConnections()`. Used by engine via type assertion.

## Layer Docs (read on demand)

```
webapp/CLAUDE.md         Frontend: split globals, services, stores, i18n, components
desktop/CLAUDE.md        Tauri shell, Rust modules, config
api/CLAUDE.md            Center API: routes, middleware, models, workers, cloudprovider
k2/CLAUDE.md             Go core architecture, wire protocol, daemon API
docs/knowledge/          Distilled patterns from all executed features
```
