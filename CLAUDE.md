# k2app — Kaitu VPN Client

Tauri v2 desktop + Capacitor 7 mobile app wrapping the k2 Go tunnel core. React webapp frontend shared across platforms. Next.js website for marketing, user self-service, and admin management.

**This file is the only doc loaded on every session.** It carries the map plus the rules that bite before you'd know to look them up. Everything else is a leaf: the repo has ~70 `CLAUDE.md` files, and a directory's own doc loads when you work in that directory. So **layer-specific detail belongs in the layer doc, not here** — putting it here charges every session for it.

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

## Constitutional Rule: Work Isolation

**任何非纯只读的代码改动，必须在独立 git worktree + 独立 feature 分支中完成。** `main` 的工作目录是并行 agent 共享的状态——在其上直接改代码会污染 `git status`、拖别人下水、绕过 review。同理：不要借用别人正在用的 worktree，不要直接 commit 到 `main`。

**例外**：纯只读调查；`CLAUDE.md` / `docs/` 等治理文档；用户明确说"就在这儿改"。

流程：`superpowers:using-git-worktrees`（或 `EnterWorktree`）建隔离区 → `fix/<slug>` / `feat/<slug>` 分支 → 改 + 测 → `git commit --only <paths>`（**先单独查一次 staging**，commit 打的是整个 index）→ `superpowers:finishing-a-development-branch` 决定合并/PR。

## Project Structure

```
k2/                  Go core (submodule, read-only — its own CLAUDE.md tree)
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
contracts/           Cross-layer contract artifact (api-contract.json) — generated, committed
scripts/             Build, deploy, test helpers
docker/scripts/      Node ops scripts (provision-node.sh, enable-ipv6.sh, etc.)
docs/plans/          Architecture design docs
```

## Tech Stack

- Webapp: React 18, TypeScript, Material-UI 5, Zustand, React Router 7, i18next
- Website: Next.js 15, React 19, Tailwind CSS 4, shadcn/ui, next-intl
- Desktop: Tauri v2, Rust · Core: Go (k2 submodule)
- API: Go, Gin, GORM, MySQL, Redis, Asynq
- Mobile: Capacitor 7, gomobile bind (K2Plugin Swift/Kotlin), `@capawesome/capacitor-android-edge-to-edge-support` for Android 15 edge-to-edge
- Package: yarn workspaces (`webapp`, `desktop`, `mobile`); `web` has independent yarn.lock; `tools/kaitu-center` uses npm
- CI: GitHub Actions — see `.github/workflows/`

## Cross-Layer Conventions

Rules that span directories, or that fail silently if you don't know them up front. Layer-specific rules live in the layer docs.

- **Version source of truth**: Root `package.json` `version`. Tauri reads it via `../../package.json`; the k2 binary gets it via ldflags. Bump here first, and pass the value through verbatim — an invented suffix makes an upgrade look like a downgrade.
- **k2 submodule is read-only**: Do not edit `k2/` from the parent worktree unless the task explicitly targets the k2 repo. Built with `-tags nowebapp` (headless); binary → `desktop/src-tauri/binaries/`.
- **Go→JS JSON keys**: Go `json.Marshal` emits snake_case; JS/TS expects camelCase. Native bridges (`K2Plugin.swift`/`.kt`, Tauri bridges) remap at the boundary.
- **Go `json.Marshal` escapes `&` as `\u0026`**: asserting on raw JSON strings containing URLs will fail. Unmarshal to `map[string]any` and assert on values.
- **Docker on Apple Silicon**: always `--platform linux/amd64` for server images; the Go binary needs `GOARCH=amd64`.
- **Log rotation**: the 20 MB cap is universal; **retention is not** — Go `config.SetupLogging` (lumberjack) keeps 3 backups / 7 days / gzip, Tauri plugin-log keeps only one (`KeepOne`), iOS/Android `NativeLogger` truncates to 0. Don't assume 3 backups exist off the Go path. **Upload modules are read-only — never truncate a source file.**
- **Build-time log level**: `K2_BUILD_LOG_LEVEL` (default `debug`) is the single knob across Go / Rust / Vite. Production: `make build-macos K2_BUILD_LOG_LEVEL=info`. Runtime `SetLogLevel()` always wins.
- **Artifact naming**: Desktop `Kaitu_{VERSION}_{ARCH}.{EXT}` (underscores); mobile uses the `kaitu/android/` CDN layout; overleap desktop builds are `Overleap_{VERSION}_{ARCH}.{EXT}` under CDN `/overleap/desktop/`. Details in `desktop/CLAUDE.md` / `mobile/CLAUDE.md`.
- **Linux desktop has no Tauri**: `cmd/k2` is one Go binary with the webapp embedded via `//go:embed` (`k2/webui`). Install is `curl -fsSL https://kaitu.io/i/k2 | sudo bash` — pulls a tarball + `.sha256`, verifies, runs `packaging/linux/install.sh`. macOS/Windows still ship the Tauri shell. (This paragraph is the only current record — `k2/webui/CLAUDE.md` documents the embed package, not the install, and the 2026-03 Linux spec still names the retired `/install-linux.sh` URL.)
- **Workspace layout**: root `yarn install` provisions `webapp`/`desktop`/`mobile`. `web/` and `tools/kaitu-center/` have their own lockfiles — install there separately.
- **Brand 参数化（开途 / Overleap 双品牌）**: 后端按 Host→`X-K2-Brand`→kaitu 解析请求品牌；`users.brand` 是**出生属性**，认证层强制匹配（403003）。客户端 build 时烘焙品牌并恒发 `X-K2-Brand`。分层机制见 `api/CLAUDE.md` / `webapp/CLAUDE.md` / `web/CLAUDE.md` 的 "Brand" 段；设计见 `docs/superpowers/specs/2026-07-14-brand-split-design.md`。**以下三条会让你本地全绿而线上/CI 是瞎的**：
  - **品牌字面量只能进 `webapp/src/brand/<brand>.ts` / `web/src/lib/brands.ts`** —— 静态 import 的页面里写死的字面量会被打进**另一个品牌**的产物。
  - **改任一层的品牌数据后必须重生成跨层契约**：`cd api && UPDATE_CONTRACT=1 go test -count=1 -run TestExportContract ./...`，产物与代码一起提交。`contracts/api-contract.json` 由 `api/contract_export_test.go` 从 **Go 活值**导出（不是手写清单），锁住三层注册表的交集 —— 这条契约门**只有本文件记录**，api/CLAUDE.md 的 Brand 段没有。**必须带 `-count=1`**（golden 在 api/ 模块外，go test 缓存不 recheck 模块外文件 → 手改 golden 迁就代码会拿到陈旧 PASS）；golden **只读**（自动重写 = CI 永远绿）；契约文件**必须进 git**（gitignore 掉 = 本地绿 CI 瞎）。
  - **跨层不变量是宿主归属**（`host(各层 baseURL) ∈ api.Hosts[该品牌]`）**而非字符串相等** —— api/webapp 用 `www.`、web 用裸域是合法漂移，别"修"它。

## Cross-Layer Domain Vocabulary

Terms that cross layer boundaries. Each layer's doc extends its own.

- **ClientConfig** — Universal config contract: Go `config.ClientConfig` ≡ TS `ClientConfig`. Webapp assembles it, passes it to `_k2.run('up', config)`. Outbounds live in `routes: [{via, match}]` — there is **no** top-level `server` field. See `k2/engine/engine.go buildRouteEntries`.
- **Engine** — Unified tunnel lifecycle manager (`k2/engine/`), shared by the desktop daemon and the mobile wrapper.
- **k2subs** — Subscription URL scheme (`k2subs://udid:token@host/api/subs`), resolved to `k2v5://` tunnels via `/api/subs`. **Desktop daemon only** — mobile is manual-only, webapp hands `_k2.run('up')` a single `k2v5://` URL. Don't assume symmetry. See `mobile/CLAUDE.md` "Server Selection".
- **probe.Registry** — In-memory per-URL QUIC-probe cache (`k2/probe/`), read by the daemon probe loop, `/api/core probe`, and `Subscription.Pick`. **Flake tolerance**: a first `score==0` returns `ok=false` (neutral); only two consecutive zeros hard-exclude. TTL 15 min.
- **recommendScore** — Canonical `[0.0, 1.0]` tunnel recommendation signal (higher = better), from `api.ComputeRecommendScore`. Non-cloud nodes get `0.5` neutral, never 0. Legacy `weight` is dual-emitted as `round(score*100)`. Model + rules: `api/CLAUDE.md` "Tunnel Scoring".
- **EngineError** — `{Code int, Category string, Message string}` (`k2/engine/error.go`). Code ranges are load-bearing and **must never be mixed**: `1xx` network (101 NetworkUnavailable), `4xx` client (400 BadConfig, 401 AuthRejected, 402 PaymentRequired, 403 Forbidden, 408 Timeout), `5xx` server (502 ProtocolError, 503 ServerUnreachable, 570 ConnectionFatal). Categories: `client`/`network`/`server`/`target`.
- **NetEvent** — Network state change (Signal + 7 platform fields), constructed by platforms, exported by gomobile as `EngineNetEvent` (iOS) / `engine.NetEvent` (Android). Routed through `netCoordinator`, which separates 网络断了 / 恢复 / 接口变了. Legacy `OnNetworkChanged()` → `SignalChanged`. Details: `k2/engine/CLAUDE.md`, `k2/appext/CLAUDE.md`.
- **transformStatus()** — Bridge-layer webapp boundary: `"stopped"`→`"disconnected"`, synthesizes `"error"`. See `webapp/CLAUDE.md`.
- **Brand** — Registry-backed enum (`kaitu` / `overleap`, `api/brand.go`) driving per-brand hosts/CORS/payment-channels/node-visibility. Resolved per-request (Host→`X-K2-Brand`→kaitu), immutable on `users.brand` once set, enforced at auth (403003 on mismatch). Spec + full design: `docs/superpowers/specs/2026-07-14-brand-split-design.md`; backend rules in `api/CLAUDE.md` "Brand" section.

## Layer Docs

Loaded on demand when you work in the directory — read the layer doc before changing that layer.

| Doc | Scope |
|-----|-------|
| [`webapp/CLAUDE.md`](webapp/CLAUDE.md) | React frontend: split globals, bridge contract, VPN state machine, services, stores, i18n, components |
| [`web/CLAUDE.md`](web/CLAUDE.md) | Next.js website + admin dashboard, API proxy, Velite content |
| [`desktop/CLAUDE.md`](desktop/CLAUDE.md) | Tauri shell, Rust modules, storage encryption, PKG install, artifact naming, S3 log upload |
| [`mobile/CLAUDE.md`](mobile/CLAUDE.md) | Capacitor + gomobile, K2Plugin, iOS/Android VPN architecture, APK signing, ASO rules |
| [`api/CLAUDE.md`](api/CLAUDE.md) | Center API: routes, middleware, models, workers, tunnel scoring, cloudprovider |
| [`mcp/CLAUDE.md`](mcp/CLAUDE.md) | Go MCP server: tools, auth flow, Center/daemon clients, Tauri session sharing |
| [`tools/kaitu-center/CLAUDE.md`](tools/kaitu-center/CLAUDE.md) | TypeScript MCP/OpenClaw tools, NodeNext conventions |
| [`scripts/CLAUDE.md`](scripts/CLAUDE.md) | Build/deploy/test helpers, Windows k2 test workflow |
| [`k2/CLAUDE.md`](k2/CLAUDE.md) | Go core: wire protocol, daemon API, engine internals (submodule, read-only) — plus nested docs under `k2/engine/`, `k2/appext/`, `k2/webui/`, and specs in `k2/docs/` |

## Marketing / Brand

**品牌架构**：**Overleap（海外）与 开途·Kaitu（中国）是完全隔离的两个独立品牌**——不是母子层级，任何面向用户的语境都不互相提及（唯一例外：法务文书署名 Overleap LLC）。协议层 k2 / k2cc / k2s / k2r 全球共享，不属于任一品牌。中文用户面禁用 "Kaitu" 裸词（用「开途」）；海外面禁用 "Kaitu" 裸词（Google 会纠错成 kaitai）。

策略 / 审查 / 内容日历见 [`docs/marketing/README.md`](docs/marketing/README.md)（目录索引 + 更新规则）。单一事实源是 [`.agents/product-marketing-context.md`](.agents/product-marketing-context.md)（路径硬编码，所有 `marketing-skills:*` 启动时自动读，**不可挪动**）。
