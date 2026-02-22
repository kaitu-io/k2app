# Plan: Platform Paths + k2rule v0.5.0 Upgrade

## Meta

| Field | Value |
|-------|-------|
| Feature | platform-paths |
| Spec | docs/features/platform-paths.md |
| Date | 2026-02-22 |
| Complexity | moderate |

## AC Mapping

| AC | Test | Task |
|----|------|------|
| AC1: k2rule v0.5.0 升级，go build 通过 | `go build ./...` | T1 |
| AC2: paths.CacheDir() 返回正确路径 | `TestCacheDir` | T2 |
| AC3: paths.LogDir() root/user 返回正确路径 | `TestLogDir`, `TestLogDir_Root` | T2 |
| AC4: daemon CacheDir 默认值填充 | `TestBuildTunnel_DefaultCacheDir` | T3 |
| AC5: mobile Engine.Start 接受 *EngineConfig | `TestEngine_Start_InvalidURL` (updated), `TestEngine_Start_NilConfig` | T4 |
| AC5b: EngineConfig gomobile 导出 | `make mobile-ios` / `make mobile-android` | T4 |
| AC6: 所有测试通过 | `go test ./...` | T1, T3, T4 |
| AC7: service_darwin.go log 路径 /var/log/k2/ | `TestLaunchdLogPath` (visual inspect, no unit test — plist is string template) | T3 |

## Dependency Graph

```
T1 (k2rule upgrade + test fix) → T2 (paths/ 包) → T3 (daemon 集成)
                                                 → T4 (mobile 集成)
```

T3 和 T4 互相独立，可并行（无文件重叠）。

## Foundation Tasks

### T1: Upgrade k2rule to v0.5.0-go + fix broken tests

**Scope**: 升级 go.mod 中 k2rule 依赖版本，修复所有因 CacheDir 必填导致的编译/测试失败。此任务用 `t.TempDir()` 作为临时 CacheDir 让所有现有测试先跑通，后续任务再引入 paths 包。
**Files**:
- `go.mod` (版本升级)
- `go.sum` (自动更新)
- `core/dns/middleware_test.go` (initK2Rule 加 CacheDir)
- `mobile/mobile.go` (Start 中 k2rule.Init 加临时 CacheDir — 用 `os.TempDir()` 占位)
- `mobile/mobile_test.go` (测试中 Start 调用适配)
- `daemon/tunnel.go` (k2rule.Init 加 CacheDir — 用 `os.TempDir()` 占位)
**Depends on**: none
**TDD**:
- RED: `go get github.com/kaitu-io/k2rule@v0.5.0-go && go build ./...` 编译失败（CacheDir 缺失）
  - Test functions: 现有 `TestHandleQuery_*`, `TestEngine_*` 全部 fail
- GREEN: 所有 k2rule.Init 调用点加 CacheDir（临时值），编译通过，测试恢复
- REFACTOR:
  - [SHOULD] 统一 initK2Rule helper 签名，减少重复
**Acceptance**: `go test ./...` 全部通过，`go build ./cmd/k2 ./cmd/k2s` 编译成功

### T2: Create paths/ package

**Scope**: 新建 `paths/` 包，实现 `CacheDir()` 和 `LogDir()`。纯新增包，不修改任何现有文件。
**Files**:
- `paths/paths.go` (新增)
- `paths/paths_test.go` (新增)
**Depends on**: none (与 T1 并行安全 — 纯新增文件，无重叠)
**TDD**:
- RED: 写测试断言 CacheDir/LogDir 返回值包含正确子路径
  - Test functions: `TestCacheDir`, `TestCacheDir_ContainsK2`, `TestLogDir`, `TestLogDir_Root`
- GREEN:
  - `CacheDir()`: `os.UserCacheDir()` + `filepath.Join(..., "k2")`
  - `LogDir()`: 按 `runtime.GOOS` + `os.Getuid()` 分支
- REFACTOR:
  - [SHOULD] 添加 EnsureDir helper（创建目录如不存在）— 视需要
**Acceptance**: `go test ./paths/...` 通过，`TestCacheDir` 在当前平台返回正确路径

## Feature Tasks

### T3: Wire paths/ into daemon + launchd fix

**Scope**: 将 `daemon/tunnel.go` 中的临时 CacheDir 替换为 `paths.CacheDir()`，更新 `service_darwin.go` launchd log 路径，更新 `config/config.go` 注释。
**Files**:
- `daemon/tunnel.go` (CacheDir 默认值 → `paths.CacheDir()/k2rule`)
- `daemon/service_darwin.go` (log 路径 `/tmp/k2` → `/var/log/k2`)
- `config/config.go` (RuleConfig.CacheDir 注释更新)
**Depends on**: [T1, T2]
**TDD**:
- RED: 无新增测试函数（BuildTunnel 依赖真实 wire URL + TUN，难以单元测试）。验证方式：代码审查 + `go build ./...`
  - Test functions: (existing tests remain passing)
- GREEN:
  - `daemon/tunnel.go`: `if cfg.Rule.CacheDir == ""` → `filepath.Join(paths.CacheDir(), "k2rule")`
  - `service_darwin.go`: `logDir := "/var/log/k2"`
  - `config.go`: 注释 `// cache directory ("" = paths.CacheDir()/k2rule)`
- REFACTOR:
  - [SHOULD] 删除 T1 中的 `os.TempDir()` 占位（被 paths.CacheDir() 替代）
**Acceptance**: `go build ./cmd/k2` 通过，daemon/tunnel.go 使用 paths.CacheDir()，launchd plist 指向 `/var/log/k2/k2.log`

### T4: Wire EngineConfig into mobile SDK

**Scope**: 新增 `mobile/config.go`（EngineConfig struct），修改 `Engine.Start()` 签名接受 `*EngineConfig`，更新所有测试。
**Files**:
- `mobile/config.go` (新增 — EngineConfig, NewEngineConfig)
- `mobile/mobile.go` (Start 签名变更，k2rule.Init 用 cfg.CacheDir)
- `mobile/mobile_test.go` (所有 Start 调用适配新签名)
**Depends on**: [T1]
**TDD**:
- RED: 写新测试验证 EngineConfig 传递
  - Test functions: `TestEngine_Start_WithEngineConfig`, `TestEngine_Start_NilConfig`, `TestEngineConfig_NewEngineConfig`
- GREEN:
  - 新增 `mobile/config.go`: EngineConfig struct + NewEngineConfig()
  - 修改 `Start(url string, fd int, cfg *EngineConfig) error`
  - k2rule.Init 使用 `filepath.Join(cfg.CacheDir, "k2rule")`
  - nil cfg 处理：返回 error `"engine: EngineConfig is required"`
- REFACTOR:
  - [MUST] 删除 T1 中 mobile.go 的 `os.TempDir()` 占位（被 cfg.CacheDir 替代）
  - [SHOULD] 更新现有测试用 helper 构造 EngineConfig
**Acceptance**: `go test ./mobile/...` 通过，`TestEngine_Start_WithEngineConfig` 验证 CacheDir 传递

## AGENT.md Updates (post-merge)

After all tasks merge, update:
- `AGENT.md`: k2rule 版本 v0.4.1-go → v0.5.0-go
- `AGENT.md`: 新增 `paths/` 到 Layer Docs
- `docs/todos/p1-platform.md`: 更新 k2rule 相关条目
