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
cd webapp && yarn test           # vitest (279 tests)
cd desktop/src-tauri && cargo test  # Rust tests (4 tests)
scripts/test_build.sh            # Full build verification (14 checks)
yarn install                     # Always run from root (workspace)
```

## Project Structure

```
k2/                  Go core (submodule, read-only — has its own CLAUDE.md)
webapp/              React + Vite + Tailwind frontend (see webapp/CLAUDE.md)
  src/vpn-client/    VPN backend abstraction (Http/Native/Mock)
  src/platform/      Platform capabilities abstraction (Tauri/Capacitor/Web)
  src/stores/        Zustand state (vpn, auth, user, purchase, invite, ui, servers, login-dialog)
  src/pages/         Route pages (Dashboard, Purchase, Invite, Account, 15+ sub-pages)
  src/components/    Shared UI (LoginDialog, guards, global components)
  src/api/           Cloud API + antiblock
  src/i18n/          Localization (zh-CN, en-US)
desktop/             Tauri v2 Rust shell (see desktop/CLAUDE.md)
mobile/              Capacitor 6 mobile app
mobile/plugins/      K2Plugin (Swift + Kotlin) — native VPN bridge
mobile/ios/          Xcode project (App + PacketTunnelExtension)
mobile/android/      Gradle project (app module, flatDir AAR)
scripts/             dev.sh, build-macos.sh, build-mobile-*.sh, test_build.sh
docs/features/       Feature specs and plans
docs/baselines/      Project capability baseline
docs/knowledge/      Distilled patterns (architecture, testing, gotchas, task-splitting, bugfix)
.github/workflows/   CI (push/PR) + Release Desktop (v* tags)
Makefile             Build orchestration — version from package.json, k2 from submodule
```

## Key Conventions

- **VpnClient boundary**: All webapp→daemon communication goes through `webapp/src/vpn-client/`. Never direct HTTP to `:1777` outside that module.
- **PlatformApi boundary**: All platform-specific operations (clipboard, external browser, locale sync) go through `webapp/src/platform/`. Factory auto-detects Tauri/Capacitor/Web.
- **Antiblock**: Cloud API entry resolution via CDN JSONP (`webapp/src/api/antiblock.ts`). Only exception to VpnClient boundary.
- **Version source of truth**: Root `package.json` (0.4.0). Tauri reads via `../../package.json` reference. k2 binary gets it via ldflags.
- **k2 submodule**: Read-only. Built with `-tags nowebapp` (headless mode). Binary output to `desktop/src-tauri/binaries/`.
- **i18n**: zh-CN default, en-US secondary. Browser language detection. Keys namespaced by page (common, dashboard, auth, settings, nav, purchase, invite, account, feedback).
- **Dark-only theme**: No light mode. All design tokens in `webapp/src/app.css` as CSS variables. Components use `bg-[--color-*]` — zero hardcoded colors.
- **Webapp subagent tasks**: Always invoke `/word9f-frontend` for frontend decisions.
- **Go→JS JSON key convention**: Go `json.Marshal` outputs snake_case (`connected_at`). JS/TS expects camelCase (`connectedAt`). Native bridge layers (K2Plugin.swift, K2Plugin.kt) must remap keys at the boundary. Never pass raw Go JSON to webapp without key remapping.
- **`.gitignore` for native platforms**: Never ignore entire source directories (`mobile/ios/`, `mobile/android/`). Only ignore build artifacts (`Pods/`, `build/`, `libs/`, `.gradle/`). Source files must always be visible to git.
- **Mobile bootstrap**: `main.tsx` must `await initVpnClient()` before React render. Mobile uses async dynamic imports (`NativeVpnClient`, `@capacitor/core`).
- **Capacitor plugin loading**: Use `registerPlugin('K2Plugin')` from `@capacitor/core`. Never use dynamic npm `import('k2-plugin')` — it fails in WebView at runtime.
- **gomobile Swift API**: Generated methods use `throws` pattern in Swift, NOT NSError out-parameter. Always use `try`/`catch`.
- **iOS extension targets**: Must have `CFBundleExecutable`, `CFBundleVersion` in Info.plist. Build settings (`CURRENT_PROJECT_VERSION`, `MARKETING_VERSION`) are NOT inherited from project — set per-target.

## Tech Stack

- Frontend: React 19, TypeScript, Tailwind CSS v4, Zustand, React Router
- Desktop: Tauri v2, Rust
- Core: Go (k2 submodule)
- Mobile: Capacitor 6, gomobile bind (K2Plugin Swift/Kotlin)
- Package: yarn workspaces (`webapp`, `desktop`, `mobile`)
- CI: GitHub Actions (`ci.yml`, `release-desktop.yml`, `build-mobile.yml`)

## Domain Vocabulary

- **VpnClient** — Platform abstraction (HttpVpnClient desktop, MockVpnClient test, NativeVpnClient mobile)
- **PlatformApi** — Cross-platform capabilities (TauriPlatform, CapacitorPlatform, WebPlatform)
- **Antiblock** — Multi-CDN entry URL resolution for Cloud API in blocked regions
- **Service readiness** — Daemon ping + version check before showing main UI
- **Version matching** — Strip build metadata after `+` for semver comparison
- **Old service cleanup** — Remove kaitu-service 0.3.x on first k2app 0.4.0 launch
- **Keep-alive tabs** — Tab pages mount once, then hide (visibility:hidden) when inactive
- **LoginDialog** — Global modal for all auth flows (replaced `/login` route)
- **Design tokens** — CSS variables in `app.css` for dark-only theme

## Layer Docs (read on demand)

```
webapp/CLAUDE.md         Frontend modules, stores, testing, i18n, platform abstraction
desktop/CLAUDE.md        Tauri shell, Rust modules, config
k2/CLAUDE.md             Go core architecture, wire protocol, daemon API
docs/knowledge/          Distilled patterns from all executed features
```
