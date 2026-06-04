# k2app — Kaitu VPN Client

Tauri v2 desktop + Capacitor 7 mobile app wrapping the k2 Go tunnel core. React webapp frontend shared across platforms. Next.js website for marketing, user self-service, and admin management.

This file is an **index**. Per-layer details live in each directory's `CLAUDE.md`.

## Quick Commands

```bash
cd web && yarn dev                          # Next.js website (Turbopack)
make dev-standalone                         # Standalone browser dev (macOS, no Tauri)
make dev-macos / make dev-windows           # Tauri desktop dev
make dev-android / make dev-ios             # Mobile dev (gomobile bind + cap sync + cap run)
make build-macos / build-windows / build-linux / build-android / build-ios
make upload-macos / upload-windows / upload-linux / upload-android / upload-web
make publish-mobile VERSION=x.y.z           # Mobile latest.json (phase 2 release)
cd webapp && yarn test                      # vitest
cd desktop/src-tauri && cargo test          # Rust tests
cd api && go test ./...                     # Center API tests
cd mcp && go test ./...                     # Go MCP server tests
scripts/test_build.sh                       # Full build verification (14 checks)
yarn install                                # Always from root (workspace)
```

## Project Structure

```
k2/                  Go core (submodule, read-only — has its own CLAUDE.md)
  engine/            Unified tunnel lifecycle manager (desktop + mobile)
  daemon/            HTTP API shell over engine (desktop only)
  appext/            gomobile type adapter over engine (iOS + Android)
webapp/              React + MUI frontend — shared across Web/Desktop/Mobile
web/                 Next.js website + admin dashboard
api/                 Center API service — Go + Gin + GORM
  cloudprovider/     Multi-cloud VPS management (AWS, Aliyun, Tencent, Bandwagon)
desktop/             Tauri v2 Rust shell (macOS + Windows)
mobile/              Capacitor 7 mobile app
mobile/plugins/      K2Plugin (Swift + Kotlin) — native VPN bridge
tools/kaitu-center/  MCP server (Claude Code) + OpenClaw plugin
tools/kaitu-mail/    OpenClaw email plugin (himalaya CLI, per-account IMAP)
tools/kaitu-signer/  Windows code-signing tray app (SimplySign + pywinauto, SQS-driven)
mcp/                 Go MCP server for Claude Code (k2 user-facing tools)
scripts/             Build, deploy, test helpers (see scripts/CLAUDE.md)
docker/scripts/      Node ops scripts (provision-node.sh, enable-ipv6.sh, etc.)
.claude/             Claude Code project settings + skills
.github/workflows/   CI + Release Desktop + Release OpenWrt
Makefile             Build orchestration — version from package.json
docs/plans/          Architecture design docs
```

## Tech Stack

- Webapp: React 18, TypeScript, Material-UI 5, Zustand, React Router 7, i18next
- Website: Next.js 15, React 19, Tailwind CSS 4, shadcn/ui, next-intl
- Desktop: Tauri v2, Rust
- Core: Go (k2 submodule)
- API: Go, Gin, GORM, MySQL, Redis, Asynq
- Mobile: Capacitor 7, gomobile bind (K2Plugin Swift/Kotlin), `@capawesome/capacitor-android-edge-to-edge-support` for Android 15 edge-to-edge
- Package: yarn workspaces (`webapp`, `desktop`, `mobile`); `web` has independent yarn.lock; `tools/kaitu-center` uses npm
- CI: GitHub Actions (`ci.yml`, `release-desktop.yml`, `build-mobile.yml`, `release-openwrt.yml`, `publish-antiblock.yml`, `release-k2s.yml`)

## Cross-Layer Conventions

Rules that span multiple directories. Layer-specific rules live in the layer docs below.

- **Version source of truth**: Root `package.json` `version` field. Tauri reads via `../../package.json`; k2 binary gets it via ldflags. Bump here first.
- **k2 submodule read-only rule**: Do not edit `k2/` from the parent worktree unless the task explicitly targets the k2 repo. Built with `-tags nowebapp` (headless). Binary output → `desktop/src-tauri/binaries/`.
- **Go→JS JSON key convention**: Go `json.Marshal` outputs snake_case; JS/TS expects camelCase. Native bridges (`K2Plugin.swift`/`kt`, Tauri bridges) must remap at the boundary.
- **Go `json.Marshal` escapes `&` as `\u0026`**: Tests that assert raw JSON strings with URLs will fail. Unmarshal to `map[string]any` and assert on deserialized values.
- **Docker on Apple Silicon**: Always `--platform linux/amd64` for server images. Go binary needs `GOARCH=amd64`.
- **Log rotation (unified)**: All platforms — 20 MB / 3 backups / 7 days / gzip. Go via `config.SetupLogging` (lumberjack), Tauri via plugin-log (20 MB / KeepOne), iOS/Android via `NativeLogger` (20 MB truncate-to-0). Upload modules are read-only — never truncate source files.
- **Build-time log level**: Single env var `K2_BUILD_LOG_LEVEL` (default `debug`) controls all platforms at build time. Go: ldflags `-X config.buildLogLevel`. Rust: `option_env!("K2_BUILD_LOG_LEVEL")`. Vite: `__K2_BUILD_LOG_LEVEL__` define. Production: `make build-macos K2_BUILD_LOG_LEVEL=info` or set via CI env. Runtime `SetLogLevel()` always overrides.
- **Artifact naming**: Desktop uses `Kaitu_{VERSION}_{ARCH}.{EXT}` (underscore-separated). Mobile uses `kaitu/android/` CDN layout. See `desktop/CLAUDE.md` / `mobile/CLAUDE.md` for full details.
- **Linux desktop = embedded Go binary, no Tauri**: `cmd/k2` ships a single Go binary with the React webapp embedded via `//go:embed` in `k2/webui`. Users install via `curl -fsSL https://kaitu.io/i/k2 | sudo bash` — downloads tarball + `.sha256`, verifies, runs `packaging/linux/install.sh`. macOS and Windows continue to use the Tauri shell. See `k2/webui/CLAUDE.md` for install flow details.
- **Workspace layout**: Root `yarn install` provisions `webapp`, `desktop`, `mobile`. `web/` and `tools/kaitu-center/` have independent lockfiles — install there separately when touching them.

## Cross-Layer Domain Vocabulary

Terms you'll encounter in multiple layers. Per-layer extensions live in the layer docs.

- **ClientConfig** — Universal config contract: Go `config.ClientConfig` ≡ TS `ClientConfig`. Webapp assembles it and passes to `_k2.run('up', config)`. Outbounds live in `routes: [{via, match}]` — no top-level `server` field. See `k2/engine/engine.go buildRouteEntries`.
- **Engine** — Unified tunnel lifecycle manager (`k2/engine/`) used by both desktop daemon and mobile wrapper.
- **k2subs** — Subscription URL scheme (`k2subs://udid:token@host/api/subs`). Resolves to a list of `k2v5://` tunnels via `/api/subs`. **Desktop daemon only** (persistent `Subscription` with refresh loop + Phase-B hot-swap + probe-driven scoring). **Mobile is manual-only** — webapp passes a single `k2v5://` URL to `_k2.run('up')`. See `mobile/CLAUDE.md` "Server Selection" and `k2/config/subscription.go`.
- **probe.Registry** — In-memory per-URL QUIC-probe measurement cache (`k2/probe/`). Consumed by daemon's background probe loop, the `/api/core probe` action, and `Subscription.Pick` via `ScoreSource`. Flake tolerance: first `score==0` returns `ok=false` (neutral), two consecutive zeros confirm hard-exclude. TTL 15 min.
- **recommendScore** — Canonical `[0.0, 1.0]` tunnel recommendation signal (higher = better). Computed by `api.ComputeRecommendScore` (commit `9e12d0b`). Emitted on `/api/tunnels` (Dashboard `RecommendDot`) and `/api/subs` (daemon + webapp weighted picks). Non-cloud nodes default to `0.5` neutral. Legacy `weight` field still dual-emitted as `round(score*100)`.
- **LicenseKeyBatch** — 授权码批次：独立于活动码的分发单位。Batch 存渠道标签 (`sourceTag`)、兑换条件 (`recipientMatcher`)、过期时间。统计维度包含兑换率和兑换→付费转化率。创建需审批。
- **EngineError** — Structured error type (`k2/engine/error.go`): `{Code int, Category string, Message string}`. HTTP-aligned codes (101 NetworkUnavailable, 400 BadConfig, 401 AuthRejected, 402 PaymentRequired, 403 Forbidden, 408 Timeout, 502 ProtocolError, 503 ServerUnreachable, 570 ConnectionFatal). Categories: `client` / `network` / `server` / `target`.
- **NetEvent** — Network state change event (Signal + 7 platform fields). Platforms construct it, gomobile exports as `EngineNetEvent` (iOS) / `engine.NetEvent` (Android). Routes through `netCoordinator` which distinguishes 网络断了 / 恢复 / 接口变了. Legacy `OnNetworkChanged()` maps to `SignalChanged`.
- **transformStatus()** — Bridge-layer webapp boundary: normalizes `"stopped"`→`"disconnected"` and synthesizes `"error"` state. Details in `webapp/CLAUDE.md`.

## Layer Docs

| Doc | Scope |
|-----|-------|
| [`webapp/CLAUDE.md`](webapp/CLAUDE.md) | React frontend: split globals, bridge contract, VPN state machine, services, stores, i18n, components |
| [`web/CLAUDE.md`](web/CLAUDE.md) | Next.js website + admin dashboard, API proxy, Velite content |
| [`desktop/CLAUDE.md`](desktop/CLAUDE.md) | Tauri shell, Rust modules, storage encryption, PKG install, artifact naming, S3 log upload |
| [`mobile/CLAUDE.md`](mobile/CLAUDE.md) | Capacitor + gomobile, K2Plugin, iOS/Android VPN architecture, APK signing, ASO rules |
| [`api/CLAUDE.md`](api/CLAUDE.md) | Center API: routes, middleware, models, workers, cloudprovider |
| [`mcp/CLAUDE.md`](mcp/CLAUDE.md) | Go MCP server: tools, auth flow, Center/daemon clients, Tauri session sharing |
| [`tools/kaitu-center/CLAUDE.md`](tools/kaitu-center/CLAUDE.md) | TypeScript MCP/OpenClaw tools, NodeNext conventions |
| [`scripts/CLAUDE.md`](scripts/CLAUDE.md) | Build/deploy/test helpers, Windows k2 test workflow |
| [`k2/CLAUDE.md`](k2/CLAUDE.md) | Go core: wire protocol, daemon API, engine internals (submodule) |

### k2 Submodule Docs (read-only)

See `k2/CLAUDE.md` for architecture and `k2/docs/` for feature specs, knowledge base, API contracts, and backlog.

## Marketing Docs

Marketing 策略 / 审查 / 内容日历统一放在 [`docs/marketing/`](docs/marketing/README.md)。开新 marketing 话题前先读 README 索引。

| Doc | Scope |
|-----|-------|
| [`docs/marketing/README.md`](docs/marketing/README.md) | 目录索引 + 已知冲突点 + 工作方式 |
| [`.agents/product-marketing-context.md`](.agents/product-marketing-context.md) | 单一事实源：品牌 / ICP / JTBD / 竞品 / 异议 / 声调（路径硬编码，所有 `marketing-skills:*` 自动引用） |
| [`docs/marketing/brand-naming-strategy.md`](docs/marketing/brand-naming-strategy.md) | 品牌命名层级（Overleap 母 / Kaitu 中国产品 / k2 协议）+ SEO 关键词矩阵 |
| [`docs/marketing/content-calendar-2026-Q2.md`](docs/marketing/content-calendar-2026-Q2.md) | 13 周双轨内容日历（Kaitu zh-CN + Overleap en-US），W1-W13 |
| [`docs/marketing/audits/`](docs/marketing/audits/) | CRO / ASO 审查快照（按日期） |

**品牌架构**（2026-04-21 对齐）：**Overleap 母品牌 / Kaitu 中国产品** 层级结构 —— 海外统一 Overleap、中国统一 开途 / Kaitu、跨语境（footer / ToS / 英文 press）用 "Kaitu by Overleap"。详见 `brand-naming-strategy.md`。

**剩余待对齐**：0 —— 全部 3 个冲突已 resolved (2026-04-21)。
