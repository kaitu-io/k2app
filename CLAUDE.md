# k2app - Kaitu VPN Desktop Client

## Project Structure
- `k2/` — Go core (git submodule, do NOT modify)
- `webapp/` — React + Vite + Tailwind frontend
- `desktop/` — Tauri v2 desktop shell (Rust)
- `docs/` — Feature specs and plans

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

## Webapp Subagent Tasks
- Always invoke `/word9f-frontend` for frontend decisions
