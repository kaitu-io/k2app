# k2app - Kaitu VPN Desktop Client

## Project Structure
- `k2/` — Go core (git submodule, do NOT modify)
- `webapp/` — React + Vite + Tailwind frontend
- `desktop/` — Tauri v2 desktop shell (Rust)
- `scripts/` — Build automation scripts
- `docs/features/` — Feature specs and plans
- `docs/baselines/` — Project capability baselines
- `docs/knowledge/` — Distilled patterns and decisions

## Build Commands
- `make dev` — Start k2 daemon + Vite + Tauri dev mode
- `make build-macos` — Build signed macOS DMG
- `make build-windows` — Build signed Windows NSIS installer

## Key Conventions
- k2 binary built with `-tags nowebapp` (headless mode)
- Webapp uses VpnClient interface for all daemon communication — never direct HTTP to `:1777` outside `webapp/src/vpn-client/`
- Cloud API goes through antiblock entry resolution (`webapp/src/api/antiblock.ts`)
- Version source of truth: root `package.json` version field (0.4.0)
- Tauri reads version from `../../package.json`

## Tech Stack
- Frontend: React 19, TypeScript, Tailwind CSS v4, Radix UI, Zustand, React Hook Form + Zod
- Desktop: Tauri v2, Rust
- Core: Go (k2 submodule)
- Package manager: yarn (workspaces)

## Testing
- Webapp: vitest + @testing-library/react
- Desktop: cargo test
- Run webapp tests: `cd webapp && yarn test`
- Run desktop tests: `cd desktop/src-tauri && cargo test`

## Key Modules

**webapp/src/vpn-client/** — Platform abstraction for VPN control
- `types.ts` — VpnClient interface, VpnStatus, VpnEvent, ReadyState
- `http-client.ts` — Desktop implementation (HTTP to :1777 + polling)
- `mock-client.ts` — Test double for unit tests
- `index.ts` — Factory with dependency injection

**webapp/src/api/** — Cloud API and antiblock
- `cloud.ts` — Cloud API client (login, servers, user)
- `antiblock.ts` — Entry URL resolution with CDN fallback
- `types.ts` — API response types

**webapp/src/stores/** — Zustand state management
- `vpn.store.ts` — VPN state and VpnClient wrapper
- `auth.store.ts` — Authentication and token management
- `servers.store.ts` — Server list and selection

**desktop/src-tauri/src/** — Tauri shell modules
- `service.rs` — k2 daemon lifecycle and version checks
- `tray.rs` — System tray menu
- `updater.rs` — Auto-update logic
- `main.rs` — App setup and window configuration

## Domain Vocabulary

**VpnClient** — Platform-agnostic interface for VPN operations. Desktop uses HttpVpnClient (HTTP to daemon), mobile uses NativeVpnClient (Capacitor bridge), tests use MockVpnClient.

**Antiblock** — Entry URL resolution system for Cloud API access in blocked regions. Uses multi-CDN fallback (jsDelivr, unpkg) with base64 obfuscation.

**Service readiness** — Webapp checks if k2 daemon is running and version-compatible before showing main UI. Uses `checkReady()` with ping + version verification.

**Version matching** — Compares app and service versions by stripping build metadata after `+`. Example: `0.4.0` matches `0.4.0+abc123`.

**Old service cleanup** — On first launch of k2app 0.4.0, detects and removes old kaitu-service from 0.3.x to prevent port conflicts.

## Webapp Subagent Tasks
- Always invoke `/word9f-frontend` for frontend decisions
