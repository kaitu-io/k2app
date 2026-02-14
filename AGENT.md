# k2app — Kaitu VPN Desktop Client

Tauri v2 desktop app wrapping the k2 Go tunnel core. React webapp frontend served via tauri-plugin-localhost.

## Quick Commands

```bash
make dev                         # k2 daemon + Vite HMR + Tauri window
make build-macos                 # Signed macOS PKG (universal binary)
make build-macos-fast            # Same, skip notarization (local dev)
make build-windows               # Signed Windows NSIS installer
cd webapp && yarn test           # vitest (95 tests)
cd desktop/src-tauri && cargo test  # Rust tests (4 tests)
scripts/test_build.sh            # Full build verification (14 checks)
yarn install                     # Always run from root (workspace)
```

## Project Structure

```
k2/                  Go core (submodule, read-only — has its own AGENT.md)
webapp/              React + Vite + Tailwind frontend (see webapp/AGENT.md)
desktop/             Tauri v2 Rust shell (see desktop/AGENT.md)
scripts/             dev.sh, build-k2.sh, test_build.sh, test_version_propagation.sh
docs/features/       Feature specs and plans
docs/baselines/      Project capability baseline
docs/knowledge/      Distilled patterns (architecture, testing, gotchas, task-splitting, bugfix)
.github/workflows/   CI (push/PR) + Release Desktop (v* tags)
Makefile             Build orchestration — version from package.json, k2 from submodule
```

## Key Conventions

- **VpnClient boundary**: All webapp→daemon communication goes through `webapp/src/vpn-client/`. Never direct HTTP to `:1777` outside that module.
- **Antiblock**: Cloud API entry resolution via CDN JSONP (`webapp/src/api/antiblock.ts`). Only exception to VpnClient boundary.
- **Version source of truth**: Root `package.json` (0.4.0). Tauri reads via `../../package.json` reference. k2 binary gets it via ldflags.
- **k2 submodule**: Read-only. Built with `-tags nowebapp` (headless mode). Binary output to `desktop/src-tauri/binaries/`.
- **i18n**: zh-CN default, en-US secondary. Browser language detection. Keys namespaced by page (common, dashboard, auth, settings).
- **Webapp subagent tasks**: Always invoke `/word9f-frontend` for frontend decisions.

## Tech Stack

- Frontend: React 19, TypeScript, Tailwind CSS v4, Zustand, React Router
- Desktop: Tauri v2, Rust
- Core: Go (k2 submodule)
- Package: yarn workspaces (`webapp`, `desktop`)
- CI: GitHub Actions (`ci.yml`, `release-desktop.yml`)

## Domain Vocabulary

- **VpnClient** — Platform abstraction (HttpVpnClient desktop, MockVpnClient test, NativeVpnClient mobile deferred)
- **Antiblock** — Multi-CDN entry URL resolution for Cloud API in blocked regions
- **Service readiness** — Daemon ping + version check before showing main UI
- **Version matching** — Strip build metadata after `+` for semver comparison
- **Old service cleanup** — Remove kaitu-service 0.3.x on first k2app 0.4.0 launch

## Layer Docs (read on demand)

```
webapp/AGENT.md          Frontend modules, stores, testing, i18n
desktop/AGENT.md         Tauri shell, Rust modules, config
k2/AGENT.md              Go core architecture, wire protocol, daemon API
```
