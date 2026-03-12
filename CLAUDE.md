# k2app — Kaitu VPN Client

Tauri v2 desktop + Capacitor 6 mobile app wrapping the k2 Go tunnel core. React webapp frontend shared across platforms. Next.js website for marketing, user self-service, and admin management.

## Quick Commands

```bash
cd web && yarn dev               # Next.js website (Turbopack)
cd web && yarn test              # vitest + playwright
make dev-standalone               # Standalone browser dev (macOS, no Tauri)
make dev-macos                   # Tauri desktop dev (macOS)
make dev-windows                 # Tauri desktop dev (Windows)
make build-macos                 # Signed macOS PKG (universal binary)
make build-windows               # Signed Windows NSIS installer (cross-compiled on macOS via cargo-xwin)
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

## Windows k2 Test Scripts

Test the k2 Go tunnel against the HK k2v5 test server. Configs and scripts are in the repo root / `scripts/`.

**1. Build k2 binary** (from Git Bash, no admin):
```bash
cd k2 && GOOS=windows GOARCH=amd64 go build -tags nowebapp -o ../desktop/src-tauri/binaries/k2-x86_64-pc-windows-msvc.exe ./cmd/k2
```

**2. Start daemon** (requires admin — TUN mode creates virtual NIC):
```powershell
# From PowerShell (auto-elevates via UAC):
.\scripts\start-k2-admin.ps1
```
This starts the daemon in foreground using `k2-test-config.yml` (TUN mode, global routing, debug logs to `C:\Users\david\k2-debug.log`). Press Ctrl+C to stop.

**3. Control from Git Bash** (no admin needed, daemon must be running):
```bash
./scripts/test-k2-ctl.sh up       # Connect tunnel (sends UP to daemon API)
./scripts/test-k2-ctl.sh status   # Connection status JSON
./scripts/test-k2-ctl.sh down     # Disconnect
./scripts/test-k2-ctl.sh logs     # Tail debug log
./scripts/test-k2-ctl.sh test     # Connectivity tests (IP, Google, YouTube, speed)
./scripts/test-k2-ctl.sh debug    # Set log level to debug
./scripts/test-k2-ctl.sh info     # Set log level to info
```

**4. Daemon API** (port 1778 for test, 1777 for app):
```bash
curl -s http://127.0.0.1:1778/ping                                          # Health check
curl -s -X POST http://127.0.0.1:1778/api/core -d '{"action":"status"}'     # Status
```

**Config files:**
- `k2-test-config.yml` — TUN mode (admin required, full VPN, tests HandleUDP/QUIC)
- `k2-test-proxy-config.yml` — Proxy mode (no admin, SOCKS5 on :1080, TCP only)

## Project Structure

```
k2/                  Go core (submodule, read-only — has its own CLAUDE.md)
  engine/            Unified tunnel lifecycle manager (desktop + mobile)
  daemon/            HTTP API shell over engine (desktop only)
  appext/            gomobile type adapter over engine (mobile + macOS sysext)
webapp/              React + MUI frontend (see webapp/CLAUDE.md)
  src/services/      Cloud API (cloudApi, k2api), auth, caching, platform fallbacks
  src/core/          K2 VPN bridge (getK2, waitForK2, polling)
  src/types/         Core interfaces (IK2Vpn, IPlatform, ISecureStorage)
  src/stores/        Zustand state (vpn-machine, connection, config, auth, alert, layout, dashboard, login-dialog, self-hosted, onboarding)
  src/pages/         Route pages (Dashboard, Purchase, Invite, Account, 15+ sub-pages)
  src/components/    Shared UI (LoginDialog, AuthGate, guards, global components)
  src/utils/         Error handling, version compare, tunnel sorting
  src/i18n/          Localization (7 locales, 15+ namespaces)
web/                 Next.js website + admin dashboard (see web/CLAUDE.md)
  src/app/[locale]/  Public pages (install, purchase, account, wallet, releases)
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
.claude/settings.json  Project-level Claude Code config (MCP server registration)
.claude/skills/      Skill files for Claude Code (kaitu-node-ops.md — node ops safety guardrails)
.github/workflows/   CI (push/PR) + Release Desktop (v* tags) + Release OpenWrt
Makefile             Build orchestration — version from package.json, k2 from submodule
docs/plans/          Architecture design docs
  2026-03-05-k2-router-platform-design.md  Router platform: rule engine, k2subs, DNS, build trim
```

## Key Conventions

- **Split globals architecture**: Frontend uses `window._k2` (VPN control) and `window._platform` (platform capabilities). Cloud API via `cloudApi.request()` in `src/services/`.
- **VPN control boundary**: All VPN operations go through `window._k2.run(action, params)`. Never direct HTTP to `:1777` from webapp.
- **Cloud API boundary**: All cloud API calls go through `cloudApi` / `k2api` in `src/services/`. Auth headers and token refresh handled automatically.
- **Error display**: `response.message` is debug-only. Users see i18n text mapped from `response.code`. Never show raw backend messages.
- **Version source of truth**: Root `package.json` — `version` is beta (0.4.0-beta.2), `releaseVersion` is stable (0.3.22). Tauri reads via `../../package.json` reference. k2 binary gets it via ldflags. Web `next.config.ts` exposes both as `NEXT_PUBLIC_BETA_VERSION` and `NEXT_PUBLIC_DESKTOP_VERSION`.
- **k2 submodule**: Read-only. Built with `-tags nowebapp` (headless mode). Binary output to `desktop/src-tauri/binaries/`.
- **i18n**: zh-CN primary, en-US secondary, plus ja, zh-TW, zh-HK, en-AU, en-GB. 15+ namespaces. New text goes to zh-CN first.
- **MUI dark theme**: Material-UI 5 with custom theme tokens. No light mode.
- **Webapp subagent tasks**: Always invoke `/word9f-frontend` for frontend decisions.
- **Go→JS JSON key convention**: Go `json.Marshal` outputs snake_case. JS/TS expects camelCase. Native bridge layers (K2Plugin.swift/kt) must remap at boundary.
- **Bridge transformStatus() mandatory**: Every bridge (`tauri-k2.ts`, `capacitor-k2.ts`) must implement `transformStatus()`. No pass-through of raw backend state. Daemon outputs `"stopped"` but webapp expects `"disconnected"`. Error synthesis (`disconnected + error → "error"`) also happens in bridge.
- **VPN state machine**: `vpn-machine.store.ts` defines 7 explicit states (`idle`, `connecting`, `connected`, `reconnecting`, `disconnecting`, `error`, `serviceDown`) with a transition table. All state changes go through `dispatch(event, payload)`. No optimistic timeouts — state persists until a backend event changes it. `serviceDown` is an explicit state with immediate recovery via `SERVICE_REACHABLE`.
- **VPN state contract**: `reconnecting` is a transient engine signal (engine state stays `connected`). `error` is synthesized by bridge `transformStatus()` from `disconnected + lastError`.
- **`.gitignore` for native platforms**: Never ignore entire source directories (`mobile/ios/`, `mobile/android/`). Only ignore build artifacts.
- **NodeNext imports**: `tools/kaitu-ops-mcp/` uses `"module": "NodeNext"`. All relative imports must use `.js` extension in `.ts` source.
- **MCP tools save-to-file**: `download_device_log` saves to `/tmp/kaitu-device-logs/` and returns file path + metadata (no content). `exec_on_node` saves stdout > 4k chars to `/tmp/kaitu-exec-output/`. Use Read tool to inspect files.
- **Lazy wire connection**: `engine.Start()` "connected" means TUN+routes are ready. Wire handshake to server happens on first app dial.
- **Go json.Marshal escapes `&` as `\u0026`**: Tests asserting raw JSON strings with URLs will fail. Unmarshal to `map[string]any` and assert on deserialized values.
- **Docker on Apple Silicon**: Always `--platform linux/amd64` for server images. Go binary needs `GOARCH=amd64`.
- **macOS PKG install order**: Preinstall runs OLD binary, postinstall runs NEW. Always `launchctl unload` before overwriting plist.
- **RegExp `/g` flag state persists**: Module-level global regex retains `lastIndex` between calls. Reset before each `.replace()`.
- **EngineConfig.Debug dual log output**: `appext.EngineConfig.Debug = true` enables `io.MultiWriter(file, stderr)` so Go engine logs appear in Xcode console / logcat. Native side sets via `#if DEBUG` (Swift) / `BuildConfig.DEBUG` (Kotlin). Release builds default false.
- **K2Plugin local sync**: `file:` plugins are copied (not symlinked) to `node_modules/`. Makefile `dev-ios`/`dev-android`/`build-mobile-*` targets auto-run `rm -rf node_modules/k2-plugin && yarn install --force` before `cap sync`.
- **iOS App Group**: `group.io.kaitu` — shared container for App process + NE process logs. Both `K2Plugin.swift` and `PacketTunnelProvider.swift` use `kAppGroup = "group.io.kaitu"`.
- **S3 log upload prefix**: Desktop uses `desktop/{version}/{udid}/{date}/logs-{ts}-{id}.tar.gz`, mobile uses `mobile/.../.zip`. Legacy `service-logs/`/`feedback-logs/` prefixes still supported by Lambda.

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
- **Center** — Backend API service (`api/`): auth, user management, orders, tunnels, cloud management
- **transformStatus()** — Bridge normalization: `"stopped"`→`"disconnected"`, synthesizes `"error"` from `disconnected + lastError`. Handles both structured `{code, message}` and legacy string errors.
- **EngineError** — Structured error type (`k2/engine/error.go`): `{Code int, Message string}`. HTTP-aligned codes: 400 (BadConfig), 401 (AuthRejected), 403 (Forbidden), 408 (Timeout), 502 (ProtocolError), 503 (ServerUnreachable), 570 (ConnectionFatal).
- **OnNetworkChanged()** — gomobile-exported method that resets wire connections after network change. Emits transient `"reconnecting"` then `"connected"`. State stays `StateConnected`.

## Layer Docs (read on demand)

```
webapp/CLAUDE.md                    Frontend: split globals, services, stores, i18n, components
web/CLAUDE.md                       Website: Next.js pages, admin dashboard, API proxy
desktop/CLAUDE.md                   Tauri shell, Rust modules, config
mobile/CLAUDE.md                    Capacitor mobile, K2Plugin, iOS/Android VPN architecture
api/CLAUDE.md                       Center API: routes, middleware, models, workers, cloudprovider
k2/CLAUDE.md                        Go core architecture, wire protocol, daemon API
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

### Design Plans

```
docs/plans/
  2026-03-04-budget-score.md              Budget score feature
  2026-03-04-invite-page-redesign.md      Invite page redesign
  2026-03-04-onboarding-guide-design.md   Onboarding guide
  2026-03-05-k2-router-platform-design.md Router platform: rule engine, k2subs, DNS, build trim
  2026-03-05-self-hosted-design.md        Self-hosted tunnel support
  2026-03-06-webapp-architecture-refactor.md  VPN state machine + connection store refactoring
  2026-03-06-usage-analytics-design.md    Usage analytics design
  2026-03-06-usage-analytics-impl.md      Usage analytics implementation plan
  2026-03-08-device-log-verification.md   Device log & feedback ticket E2E verification
```
