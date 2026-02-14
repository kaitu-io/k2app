# k2app Project Baseline

## Meta

| Field | Value |
|-------|-------|
| Project | k2app |
| Version | 0.4.0 |
| Updated | 2026-02-14 |
| Scope | Desktop (macOS, Windows) + Mobile (iOS, Android) |

## Overview

k2app is a cross-platform VPN client built on the k2 Go core. It replaces the old kaitu 0.3.x Rust-based architecture with a simplified stack: React webapp, Tauri v2 desktop shell, and k2 daemon (Go) for VPN operations.

---

## webapp/ — Frontend Application

**Purpose**: Cross-platform UI for VPN control, server selection, authentication, and settings.

**Capabilities**:
- VpnClient abstraction layer for platform-agnostic daemon communication
  - HttpVpnClient for desktop (HTTP to localhost:1777 with polling→events)
  - MockVpnClient for testing
  - Event subscription with automatic state deduplication
- Cloud API client with antiblock entry URL resolution
  - Multi-CDN fallback (jsDelivr, unpkg, npm mirrors)
  - localStorage cache with background refresh
  - Base64 URL obfuscation
- Authentication flow with email + verification code
  - Token management (access + refresh)
  - UDID-based device identification via VpnClient
- Server list with selection and connection
  - Country flags, latency display
  - Wire URL management
- Dashboard with connection control
  - Connection button (state-aware)
  - Status display, uptime counter
  - Error messaging
- Settings page
  - Language selection (zh-CN, en-US)
  - App version display
  - About section
- i18n support with react-i18next
  - Browser language detection
  - Translation files for common, dashboard, auth, settings
- Service readiness detection
  - Loading state until daemon responds
  - Retry mechanism with timeout
  - Tauri IPC for service installation

**Tech stack**: React 19, TypeScript, Vite, Tailwind CSS v4, Radix UI, Zustand, React Hook Form, Zod, i18next

**Build output**: `webapp/dist/` — static HTML/CSS/JS bundle (shared by desktop and mobile)

---

## mobile/ — Capacitor 6 Mobile App (iOS + Android)

**Purpose**: Mobile VPN client reusing webapp, with gomobile bind replacing Rust UniFFI for Go→native FFI.

**Capabilities**:
- Capacitor 6 shell wrapping same `webapp/dist/` as desktop
- K2Plugin Capacitor native plugin (Swift + Kotlin)
  - `checkReady()`, `getUDID()`, `getVersion()`, `getStatus()`, `getConfig()`
  - `connect(wireUrl)`, `disconnect()`
  - `vpnStateChange` and `vpnError` event listeners
  - Go→JS JSON key remapping (`remapStatusKeys`: snake_case → camelCase)
  - State mapping: Engine `"disconnected"` → webapp `"stopped"`
- iOS PacketTunnelExtension (NE process)
  - gomobile Engine runs in separate NE process
  - `sendProviderMessage("status")` for rich status RPC
  - `NEVPNStatusDidChange` for coarse state events
  - App Group `group.io.kaitu` for shared state
  - Entitlements: NE + App Group (device), App Group only (simulator)
- Android K2VpnService (same process)
  - gomobile Engine runs in app process
  - Foreground service with notification
  - `VpnService.Builder.establish()` provides TUN fd
  - EventHandler bridge → K2Plugin → webapp
- NativeVpnClient webapp implementation
  - Constructor-injected K2Plugin for testability
  - Dynamic import via `initVpnClient()` to avoid desktop bundle bloat
  - `mapState()` for Engine→VpnState mapping

**Tech stack**: Capacitor 6, gomobile bind, Swift (iOS), Kotlin (Android), TypeScript (plugin defs)

**Build output**: IPA (iOS via Xcode), APK (Android via Gradle)

**Key files**:
- `mobile/capacitor.config.ts` — Capacitor configuration
- `mobile/plugins/k2-plugin/` — Capacitor plugin (TS defs + Swift + Kotlin)
- `mobile/ios/App/PacketTunnelExtension/` — iOS NE target
- `mobile/android/app/src/main/java/io/kaitu/K2VpnService.kt` — Android VPN service

---

## desktop/ — Tauri v2 Desktop Shell

**Purpose**: Native desktop shell for macOS and Windows. Embeds webapp, manages k2 daemon service, provides system tray and auto-updater.

**Capabilities**:
- Webapp serving via tauri-plugin-localhost
  - HTTP origin (not HTTPS) to avoid WebKit mixed content blocking
  - Embedded from `webapp/dist/` as frontendDist
- k2 daemon service management
  - Version checking (strips build metadata for comparison)
  - Admin reinstall via osascript (macOS) or PowerShell RunAs (Windows)
  - Old kaitu-service detection and cleanup on upgrade
  - Service ping and wait-for-ready polling
- System tray
  - Show/hide window toggle
  - Connect/disconnect actions (direct HTTP to daemon)
  - Quit command
- Auto-updater
  - CloudFront + d0.all7.cc endpoints
  - Minisign signature verification
  - Seamless 0.3.22 → 0.4.0 upgrade path
- Single instance enforcement
- Window configuration
  - Fixed width (430px), resizable height (956px default)
  - Hidden title bar, centered position
  - macOS-optimized dimensions

**Tech stack**: Rust, Tauri v2, reqwest (HTTP client), serde (JSON)

**Modules**:
- `service.rs` — k2 daemon lifecycle, version checks, old service cleanup
- `tray.rs` — system tray icon and menu
- `updater.rs` — auto-update checks and installation
- `main.rs` — Tauri app setup, window creation, plugin registration

**Build output**: DMG (macOS), NSIS installer (Windows)

---

## k2/ — Go VPN Core (Submodule)

**Purpose**: Git submodule to kaitu-io/k2. VPN tunnel engine, HTTP API server, service manager. Built with `-tags nowebapp` for k2app (no embedded webapp).

**Capabilities** (subset relevant to k2app):
- HTTP API server on :1777
  - POST /api/core — action router (up, down, status, version, get_config, speedtest)
  - GET /ping — health check
  - GET /api/device/udid — device unique ID
  - GET /metrics — memory, goroutines, GC stats
- k2v5 tunnel protocol
- Auto-reconnect with state persistence
- Service self-management
  - `k2 run --install` — install as launchd (macOS) or Windows Service
  - ServiceManager.Uninstall() — stop and remove service
- UDID generation (platform-specific)
  - macOS: IOPlatformSerialNumber from IOKit
  - Windows: MachineGuid from registry
  - Linux: /etc/machine-id
- wintun.dll embedding (Windows only)

**Build command**: `go build -tags nowebapp -ldflags "-X main.version=... -X main.commit=..." ./cmd/k2`

**Build output**: Single binary (`k2` or `k2.exe`) bundled as Tauri externalBin

---

## scripts/ — Build Automation

**Purpose**: Build orchestration scripts for development and release.

**Capabilities**:
- `dev.sh` — Start k2 daemon + Vite dev server + Tauri dev mode
  - Builds k2 if missing or outdated
  - Starts daemon in background
  - Launches Tauri with HMR-enabled Vite
  - Traps SIGINT to clean up daemon
- `build-k2.sh` — Cross-platform k2 binary compilation
  - Target triple detection and mapping
  - nowebapp tag enforcement
  - Version + commit ldflags injection
- `test_version_propagation.sh` — Verify version flows from package.json to all outputs
  - Extracts VERSION from package.json
  - Runs `make pre-build`
  - Validates version.json matches
- `build-mobile-ios.sh` — iOS build pipeline (gomobile → xcframework → cap sync → xcodebuild)
- `build-mobile-android.sh` — Android build pipeline (gomobile → AAR → cap sync → Gradle)

---

## Makefile — Build Orchestration

**Purpose**: Top-level build targets for development and release.

**Capabilities**:
- Version extraction from package.json (single source of truth)
- Commit hash extraction from k2 submodule
- Pre-build step: generate webapp/public/version.json
- k2 binary build with ldflags injection
- Webapp build (Vite)
- macOS release build (universal binary, DMG)
- Windows release build (NSIS installer)
- Mobile iOS build (gomobile + xcodebuild)
- Mobile Android build (gomobile + Gradle)
- Dev mode launcher (calls scripts/dev.sh)
- Clean targets (dist, target, binaries)

**Key variables**:
- `VERSION` — extracted from package.json via Node.js
- `COMMIT` — extracted from k2 submodule git history
- `TARGET` — target triple for k2 binary (aarch64-apple-darwin, x86_64-pc-windows-msvc, etc.)

---

## .github/workflows/ — CI/CD

**Purpose**: GitHub Actions pipelines for continuous integration and release builds.

**Capabilities**:
- CI pipeline (ci.yml)
  - Lint (eslint, cargo clippy)
  - Test (vitest, cargo test)
  - Type check (tsc, TypeScript strict mode)
  - Build verification (webapp, desktop)
- Desktop release pipeline (release-desktop.yml)
  - Matrix build (macOS, Windows)
  - Submodule checkout (--recursive)
  - Node 20, Go 1.24, Rust stable setup
  - Full build (webapp + k2 + Tauri)
  - Signing and notarization (macOS)
  - Artifact upload to CDN
  - latest.json update for Tauri updater
- Mobile build pipeline (build-mobile.yml)
  - Manual dispatch with platform selection (ios/android/both)
  - iOS job: macos-latest, Go 1.24, Node 20, gomobile, Xcode, CocoaPods
  - Android job: ubuntu-latest, Go 1.24, Node 20, Java 17, Android SDK, gomobile
  - Artifact upload (30-day retention)

---

## docs/features/ — Feature Specifications

**Purpose**: Feature planning and specification documents.

**Capabilities**:
- Feature specs (spec.md) — product requirements, architecture, acceptance criteria
- Plans (plan.md) — task breakdown, dependency graphs, TDD approach
- Contracts (if applicable) — API contracts, data formats

**Current features**:
- k2app-rewrite (implemented) — full rewrite from kaitu 0.3.x to k2app 0.4.0
- mobile-rewrite (implemented) — iOS + Android clients with Capacitor + gomobile

---

## docs/knowledge/ — Distilled Knowledge

**Purpose**: Patterns, decisions, and lessons learned from feature implementations.

**Capabilities**:
- Architecture decisions — VpnClient abstraction, versioning, antiblock, service cleanup
- Testing strategies — dependency injection, mocking, integration scripts
- Framework gotchas — WebKit mixed content, Vite proxy, Tauri config, Zustand async

**Entries link to validating tests** for bidirectional traceability.

---

## docs/baselines/ — Project Baselines

**Purpose**: Snapshot of current codebase capabilities (this file).

**Updated after**: Feature implementations, rewrites, major refactors.

---

## Root Configuration Files

**Purpose**: Project-wide configuration for package management, build tools, and version control.

**Capabilities**:
- package.json — Workspace root, version source of truth (0.4.0), yarn workspaces (webapp, desktop, mobile)
- .gitignore — Exclusions for node_modules, dist, target, binaries, mobile build artifacts
- .gitmodules — k2 submodule at `k2/` pointing to kaitu-io/k2
- CLAUDE.md — Project conventions for AI agents (VpnClient boundary, antiblock, version source)

---
