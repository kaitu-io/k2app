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

## Constitutional Rule: Work Isolation

**任何非纯只读的代码改动，必须在独立 git worktree + 独立 feature 分支中完成，禁止直接在共享主工作目录的 `main` 分支上编辑应用代码。** main 是随时可能被并行任务/其他 agent 读取的共享状态，直接在其上改代码会污染 `git status`、拖并行工作下水、绕开分支+review 流程。

违反条件（以下行为均被禁止）:
- 在主工作目录（`GIT_DIR == GIT_COMMON` 且 `branch == main`）直接 `Edit`/`Write` 应用代码文件（`webapp/`、`web/`、`api/`、`desktop/`、`mobile/`、`k2/` 等）而未先建立独立 worktree + 分支
- 复用他人/其他并行任务正在使用的 worktree 完成不相关的改动
- 改动完成后直接 commit 到 `main`，跳过分支 + PR/review 流程

例外（不受此规则约束，可直接在 main 上进行）:
- 纯只读调查 / 搜索 / 阅读（不产生代码改动）
- 对 `CLAUDE.md` / `docs/` 等治理与文档类文件的直接修改
- 用户明确指示"直接在当前目录改"时

标准流程：使用 `superpowers:using-git-worktrees` 技能（或原生 `EnterWorktree` 工具）创建隔离工作区 → 新建 `fix/<slug>` / `feat/<slug>` 分支 → 完成改动 + 测试 → 按 [[feedback_check_staged_before_commit]] 规则单独 `git commit --only <paths>` → 用 `superpowers:finishing-a-development-branch` 决定合并 / PR 路径。

---

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
contracts/           Cross-layer contract artifact (api-contract.json) — generated, committed
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
- **Brand 参数化（开途/Overleap 双品牌）**: 后端按 Host→`X-K2-Brand`→kaitu 解析请求品牌；`users.brand` 是出生属性，认证层强制匹配（403003）。客户端 build 时烘焙品牌并恒发 `X-K2-Brand`。**webapp 层**：env `K2_BRAND=kaitu|overleap`（默认 kaitu）→ Vite define `__K2_BRAND__` → `webapp/src/brand/` 注册表（theme/feature gates/i18n `{{brand}}` 插值）；产物纯度守卫 `webapp/scripts/check-brand-purity.sh`。**web 层**：`NEXT_PUBLIC_BRAND=kaitu|overleap` 一套代码两个 Amplify 部署（kaitu.io / overleap.io），互不感知；品牌泄漏守卫 `web/tests/brand-guard.test.ts`（字面量扫描）+ `web/tests/brand-leak-ssr.test.tsx`（渲染面+metadata，能抓字符串拼接）。**品牌字面量只能进 `webapp/src/brand/<brand>.ts` / `web/src/lib/brands.ts`**——静态 import 的页面里的字面量会进另一品牌产物。**跨层契约门**：三层注册表同一概念定义三遍，靠 `contracts/api-contract.json` 锁住交集——由 `api/contract_export_test.go` 从 **Go 活值**导出（品牌注册表真 struct / CORS allow-headers 打真 middleware 收响应头 / 错误码 go/ast 解析 `response.go`），webapp + web 各自 `readFileSync` 它做断言。改任一层的品牌数据后必须 `cd api && UPDATE_CONTRACT=1 go test -run TestExportContract ./...` 重新生成并一起提交，否则门会 FAIL。三条铁律：golden **只读**（自动重写=CI 永远绿）；跑它**必须带 `-count=1`**（golden 在 api/ 模块外，go test 缓存不 recheck 模块外文件 → 手改 golden 迁就代码会拿到陈旧 PASS）；契约文件**必须进 git**（gitignore 的产物=本地绿 CI 瞎）。跨层不变量是**宿主归属**（`host(各层 baseURL) ∈ api.Hosts[该品牌]`）而非字符串相等——api/webapp 用 `www.`、web 用裸域是合法漂移。Spec: `docs/superpowers/specs/2026-07-14-brand-split-design.md`；分层规则见 `api/CLAUDE.md` / `webapp/CLAUDE.md` / `web/CLAUDE.md` 的 "Brand" 段。

## Cross-Layer Domain Vocabulary

Terms you'll encounter in multiple layers. Per-layer extensions live in the layer docs.

- **ClientConfig** — Universal config contract: Go `config.ClientConfig` ≡ TS `ClientConfig`. Webapp assembles it and passes to `_k2.run('up', config)`. Outbounds live in `routes: [{via, match}]` — no top-level `server` field. See `k2/engine/engine.go buildRouteEntries`.
- **Engine** — Unified tunnel lifecycle manager (`k2/engine/`) used by both desktop daemon and mobile wrapper.
- **k2subs** — Subscription URL scheme (`k2subs://udid:token@host/api/subs`). Resolves to a list of `k2v5://` tunnels via `/api/subs`. **Desktop daemon only** (persistent `Subscription` with refresh loop + Phase-B hot-swap + probe-driven scoring). **Mobile is manual-only** — webapp passes a single `k2v5://` URL to `_k2.run('up')`. See `mobile/CLAUDE.md` "Server Selection" and `k2/config/subscription.go`.
- **probe.Registry** — In-memory per-URL QUIC-probe measurement cache (`k2/probe/`). Consumed by daemon's background probe loop, the `/api/core probe` action, and `Subscription.Pick` via `ScoreSource`. Flake tolerance: first `score==0` returns `ok=false` (neutral), two consecutive zeros confirm hard-exclude. TTL 15 min.
- **recommendScore** — Canonical `[0.0, 1.0]` tunnel recommendation signal (higher = better). Computed by `api.ComputeRecommendScore` (`api/logic_tunnel_score.go`). **Time-gated usage-sensitivity model**: `score = 1 − trafficRatio · w(timeRatio)` where `w(t) = 0.15 + 0.85·t²`. The usage penalty's weight `w` rises from a 0.15 floor at cycle start to 1.0 at cycle end → early cycle is generous (high score even at heavy usage), late cycle is strict (near-cap nodes steered away). True exhaustion is handled by the hard cutoff / hide path (`isNodeOverQuota`), not the score. (Replaced the earlier `trafficRatio − timeRatio` pacing model + warmup/headroom.) Emitted on `/api/tunnels` (Dashboard `RecommendDot`) and `/api/subs` (daemon + webapp weighted picks). Non-cloud nodes default to `0.5` neutral. Legacy `weight` field still dual-emitted as `round(score*100)`.
- **LicenseKeyBatch** — 授权码批次：独立于活动码的分发单位。Batch 存渠道标签 (`sourceTag`)、兑换条件 (`recipientMatcher`)、过期时间。统计维度包含兑换率和兑换→付费转化率。创建需审批。
- **EngineError** — Structured error type (`k2/engine/error.go`): `{Code int, Category string, Message string}`. HTTP-aligned codes (101 NetworkUnavailable, 400 BadConfig, 401 AuthRejected, 402 PaymentRequired, 403 Forbidden, 408 Timeout, 502 ProtocolError, 503 ServerUnreachable, 570 ConnectionFatal). Categories: `client` / `network` / `server` / `target`.
- **NetEvent** — Network state change event (Signal + 7 platform fields). Platforms construct it, gomobile exports as `EngineNetEvent` (iOS) / `engine.NetEvent` (Android). Routes through `netCoordinator` which distinguishes 网络断了 / 恢复 / 接口变了. Legacy `OnNetworkChanged()` maps to `SignalChanged`.
- **transformStatus()** — Bridge-layer webapp boundary: normalizes `"stopped"`→`"disconnected"` and synthesizes `"error"` state. Details in `webapp/CLAUDE.md`.
- **Brand** — Registry-backed enum (`kaitu` / `overleap`, `api/brand.go`) driving per-brand hosts/CORS/payment-channels/node-visibility. Resolved per-request (Host→`X-K2-Brand`→kaitu), immutable on `users.brand` once set, enforced at auth (403003 on mismatch). Spec + full design: `docs/superpowers/specs/2026-07-14-brand-split-design.md`; backend rules in `api/CLAUDE.md` "Brand" section.

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
| [`docs/marketing/brand-naming-strategy.md`](docs/marketing/brand-naming-strategy.md) | 对等双品牌命名（Overleap 海外 / 开途·Kaitu 中国，k2 协议层全球共享）+ SEO 关键词矩阵 |
| [`docs/marketing/content-calendar-2026-Q2.md`](docs/marketing/content-calendar-2026-Q2.md) | 13 周双轨内容日历（Kaitu zh-CN + Overleap en-US），W1-W13 |
| [`docs/marketing/audits/`](docs/marketing/audits/) | CRO / ASO 审查快照（按日期） |

**品牌架构**（2026-07-14 修订）：Overleap（海外）/ 开途·Kaitu（中国）**完全隔离**的两个独立产品品牌——不再是"母品牌/子产品"层级，任何面向用户的语境都不互相提及（法务文书署名 Overleap LLC 除外）；~~Kaitu by Overleap~~ 跨语境衔接句已作废。详见 `brand-naming-strategy.md`。技术/后端侧的品牌隔离机制见上文 Cross-Layer Domain Vocabulary "Brand" 词条 + `api/CLAUDE.md`。

**剩余待对齐**：0。
