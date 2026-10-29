# k2app — Kaitu VPN Client

Tauri desktop + Capacitor mobile clients around the k2 Go core, plus sites and Center API. **Loads every session — only the map, cross-layer rules and silent-failure rules; layer detail goes in the layer doc.**

## Quick Commands

```bash
cd web && yarn dev                          # Next.js website (Turbopack)
make dev-standalone                         # Standalone browser dev (macOS, no Tauri)
make dev-macos / make dev-windows           # Tauri desktop dev
make dev-android / make dev-ios             # Mobile dev (gomobile bind + cap sync; dev-ios deploys a plugged-in iPhone via scripts/deploy-ios-device.sh, cap run = simulator fallback)
make build-macos / build-windows / build-linux / build-android / build-ios
make upload-macos / upload-windows / upload-linux / upload-android
git push origin main:website                # Website deploy — Amplify builds `website`, a pure mirror of main. Never commit to it.
make publish-android VERSION=x.y.z BRAND=kaitu   # Mobile latest.json (phase 2 release)
make publish-ios     VERSION=x.y.z BRAND=kaitu   # both delegate to scripts/publish-mobile.sh
cd webapp && yarn test                      # vitest
cd desktop/src-tauri && cargo test          # Rust tests
cd api && go test ./...                     # DB tests silently SKIP without center/config.yml — see api/CLAUDE.md
cd mcp && go test ./...                     # Go MCP server tests
scripts/test_build.sh                       # Full build verification (count is dynamic)
# Pre-merge tests (GitHub CI runs only after push to main): `labtest` MCP test_plan → test_run → test_wait / test_logs — see scripts/CLAUDE.md
```

## Constitutional Rule: Work Isolation

**任何非纯只读的代码改动，必须在独立 git worktree + 独立 feature 分支中完成。** `main` 工作目录是并行 agent 共享的状态——直接改会污染 `git status`、拖别人下水、绕过 review。也不要借用别人的 worktree、不要直接 commit 到 `main`。

**例外**：纯只读调查；`CLAUDE.md` / `docs/` 等治理文档；用户明确说"就在这儿改"。

流程：`superpowers:using-git-worktrees`（或 `EnterWorktree`）建隔离区 → `fix/<slug>` / `feat/<slug>` 分支 → 改 + 测 → `git commit --only <paths>`（**先单独查一次 staging**，commit 打的是整个 index）→ `superpowers:finishing-a-development-branch` 决定合并/PR。

## Repository Map

A directory's `CLAUDE.md` auto-loads once you read a file under it; under `k2/` they stack (`k2/CLAUDE.md` + each nested one on the path). Read the layer doc before changing a layer.

| Dir | What | Doc |
|-----|------|-----|
| `k2/` | Go core, own module `github.com/kaitu-io/k2`: engine, daemon (desktop), appext (gomobile), wire, server, CLIs | `k2/CLAUDE.md` + nested; specs `k2/docs/` |
| `third_party/{kuic,smux}` | Forks k2 `replace`s in | none — `kuic/MAINTAINING.md`, `smux/K2_PATCHES.md` |
| `api/` | Center API — Gin, GORM, MySQL, Redis, Asynq; `cloudprovider/` multi-cloud VPS; `api/cmd/` = separate module (`center` binary) | `api/CLAUDE.md` |
| `webapp/` | React 18, MUI 5, Zustand, React Router 7, i18next — shared UI | `webapp/CLAUDE.md` |
| `web/` | kaitu.io + admin — Next.js 15, React 19, Tailwind 4, shadcn/ui, next-intl | `web/CLAUDE.md` |
| `sites/overleap/` | overleap.io — standalone Next app, no code shared with `web/` | `sites/overleap/CLAUDE.md` |
| `desktop/` | Tauri v2 Rust shell (macOS + Windows) | `desktop/CLAUDE.md` |
| `mobile/` | Capacitor 8; `mobile/plugins/` = K2Plugin (Swift/Kotlin) VPN bridge | `mobile/CLAUDE.md` |
| `mcp/` | Go MCP server, user-facing k2 tools | `mcp/CLAUDE.md` |
| `tools/kaitu-center/` | TS admin/ops MCP server (npm) | `tools/kaitu-center/CLAUDE.md` |
| `tools/kaitu-mail/` | OpenClaw email plugin (himalaya) | none |
| `tools/kaitu-signer/` | Windows signing tray app (SQS) — current CI chain doesn't call it | none — chain: `scripts/CLAUDE.md` |
| `tessera/` | Tessera protocol proving ground, own module | `tessera/CLAUDE.md` |
| `docker/` | `k2s/` node image; `sidecar/` metering + quota cutoff (Go module); `scripts/` node ops | `docker/sidecar/CLAUDE.md`, skill `kaitu-node-ops` |
| `packaging/linux/` | Linux tarball `install.sh` + systemd unit | none |
| `contracts/` | Committed goldens: `api-contract.json` ← `api/contract_export_test.go`; `bridge-api.json` ← webapp `bridge-contract.test.ts`; `webapp-support-floor.json` hand-edited, read by `scripts/ci/web-ota-manifest.mjs` | — |
| `scripts/` | Build / deploy / CI / labtest | `scripts/CLAUDE.md` |
| `.github/` | Workflows | `.github/CLAUDE.md` |
| `docs/` | `superpowers/specs/` current designs (`plans/` older); `marketing/`, `customer-service/`; layer reference in `docs/<layer>/` | — |

## Cross-Layer Conventions

- **Version source of truth**: root `package.json` `version` (Tauri reads `../../package.json`, k2 gets it via ldflags). Bump here first, pass it through verbatim — an invented suffix makes an upgrade look like a downgrade.
- **`make pre-build` rewrites tracked files**: prerequisite of every `dev-*` and platform `build-*` target (`build-macos` runs it inside `scripts/build-macos.sh`). `sync-version.sh` writes the version into `desktop/src-tauri/Cargo.toml`, `mobile/android/app/build.gradle`, `K2Helpers.swift`, `App.xcodeproj/project.pbxproj` — expect them modified after any `make`. It and CI `check-versions` are sed-pattern based: reformat one of those files and propagation stops silently.
- **`pre-build` also overwrites `k2/rule/embed/krs.tar.gz`** (tracked 66 KB placeholder) with the ~2 MB CDN archive — never commit it. Offline, dev builds silently embed the placeholder; only `EMBED_REQUIRE_FULL=1` (release CI) fails. `scripts/check-embed-size.sh` fails CI if the committed blob > 300 KB.
- **Recursive `make` inherits only `K2_BRAND`** (exported env), not the make variable `BRAND`: a bare `make build-webapp` in a script silently falls back to `BRAND ?= kaitu` — write `BRAND=$BRAND`. Scripts running tools outside make (`build-mobile-*.sh` → `npx cap sync`) re-export `K2_BRAND` themselves.
- **k2 is in-tree, no submodules** (`k2/`, `third_party/`): `git worktree add` gives a buildable tree; the old `kaitu-io/k2` repo is retired — never commit there. No other module here imports k2. Go `replace` isn't transitive: an importer (e.g. `overleap/box` in the **external** repo `~/projects/kaitu-io/overleap`) needs its own kuic + smux `replace`. Wire/server changes must keep both brands' clients working.
- **Desktop sidecar is headless**: `-tags=release` (`k2/Makefile` `DESKTOP_TAGS`); `k2/webui/embed.go` is `linux && !nowebapp` (`nowebapp` is a k2r/dev tag, not desktop).
- **Go JSON**: emits snake_case (bridges remap to camelCase at the boundary) and escapes `&` as `\u0026` — assert on unmarshalled values, not raw strings.
- **Docker on Apple Silicon**: `--platform linux/amd64` for server images, `GOARCH=amd64`.
- **Log rotation**: 20 MB cap nearly everywhere (the iOS NE's own `NativeLogger.swift` copy: 50 MB); **retention differs** — Go `config.SetupLogging` (lumberjack) 3 backups / 7 days / gzip, Tauri plugin-log 1 (`KeepOne`), K2Plugin `NativeLogger` truncates to 0. **Upload modules are read-only — never truncate a source file.**
- **Build-time log level**: `K2_BUILD_LOG_LEVEL` (default `debug`) is the one knob for Go / Rust / Vite (prod: `=info`); runtime `SetLogLevel()` wins.
- **Release tags**: the suffix table exists only in `scripts/ci/release-plan.mjs` — edit the script, never the YAML (a test guards it; suffixes in `scripts/CLAUDE.md`). **Linux desktop is kaitu-only**; **overleap Android is Play-only**. Artifact naming: desktop/mobile docs.
- **Linux desktop has no Tauri**: `k2/cmd/k2` embeds the webapp (`k2/webui`); `kaitu.io/i/k2` = `web/public/i/k2`, which runs the tarball's `packaging/linux/install.sh` (the old spec's `/install-linux.sh` is retired). See `k2/cmd/k2/CLAUDE.md`.
- **Web OTA 只由 `webapp/x.y.z[-brand]` tag 发布**：裸 tag=双品牌，`-kaitu`/`-overleap`=单品牌，`x.y.z` 必须 == `package.json`。**push main 什么都不发**，`v*` app tag 也不连带——webapp 改动不推 `webapp/*` tag 就到不了任何已装客户端。gate 拒绝倒退时，往回发须 dispatch 勾 `allow_rollback`。细节见 `scripts/CLAUDE.md`。
- **Workspace layout**: root `yarn install` (always from root) covers `webapp`/`desktop`/`mobile`; `web/`, `sites/overleap/` (yarn) and `tools/kaitu-center/` (npm) install separately.
- **Brand（开途 / Overleap 双品牌）**：客户端 build 时烘焙品牌并恒发 `X-K2-Brand`；`users.brand` 是**出生属性**，认证层强制匹配（403003）。机制见 api/webapp/web 文档的 "Brand" 段。**以下三条本地全绿而线上/CI 是瞎的**：
  - **品牌字面量只能进 `webapp/src/brands/<brand>/index.ts` / `web/src/lib/brands.ts`** —— 静态 import 的页面里写死的字面量会被打进**另一个品牌**的产物。
  - **改任一层品牌数据后必须重生成 `contracts/api-contract.json`**，命令与陷阱见 `api/CLAUDE.md` Brand 段（唯一事实源）。
  - **跨层不变量是宿主归属**（`host(各层 baseURL) ∈ api.Hosts[该品牌]`）**而非字符串相等** —— api/webapp 用 `www.`、web 用裸域是合法漂移，别"修"它。

## Cross-Layer Domain Vocabulary

- **ClientConfig** — Go `config.ClientConfig` ≡ TS; webapp passes it to `_k2.run('up', config)`. Outbounds are `routes: [{via, match}]` — **no** top-level `server` field.
- **Engine** — tunnel lifecycle manager (`k2/engine/`), shared by desktop daemon and mobile.
- **k2subs** — `k2subs://` subscription → `k2v5://` tunnels, **desktop daemon only**; mobile passes one `k2v5://` URL (`mobile/CLAUDE.md`).
- **probe.Registry** — QUIC-probe cache (`k2/probe/`); one `score==0` is neutral, two in a row exclude.
- **recommendScore** — `[0.0, 1.0]` tunnel score from `api.ComputeRecommendScore` (`api/CLAUDE.md` "Tunnel Scoring").
- **EngineError** — `{Code, Category, Message}` (`k2/engine/error.go`): `1xx` network / `4xx` client / `5xx` server, **never mixed**; **Timeout is `108`, not `408`**. Table: `k2/engine/CLAUDE.md`.
- **NetEvent** — network change sent to `appext.Engine.NotifyNetEvent`, wired in the app targets (`PacketTunnelProvider.swift`, `K2VpnService.kt`), not `mobile/plugins/` (`k2/appext/CLAUDE.md`).
- **transformStatus()** — webapp bridge: `"stopped"`→`"disconnected"`, synthesizes `"error"`.
- **Brand** — enum `kaitu` / `overleap` (`api/brand.go`): per-brand hosts/CORS/payment/node visibility.

## Marketing / Brand

**品牌架构**：**Overleap（海外）与 开途·Kaitu（中国）是完全隔离的两个独立品牌**——不是母子层级，任何面向用户的语境都不互相提及（唯一例外：法务文书署名——开途侧沿用现有署名，Overleap 侧署名其运营公司 Wordgate LLC，即 Stripe 收单主体）。协议层 k2 / k2cc / k2s / k2r 全球共享，不属于任一品牌。中文用户面禁用 "Kaitu" 裸词（用「开途」）；海外面禁用 "Kaitu" 裸词（Google 会纠错成 kaitai）。

策略 / 审查 / 内容日历见 [`docs/marketing/README.md`](docs/marketing/README.md)。单一事实源是 [`.agents/product-marketing-context.md`](.agents/product-marketing-context.md)（路径硬编码，所有 `marketing-skills:*` 启动时自动读，**不可挪动**）。
