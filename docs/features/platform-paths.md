# Feature: Platform Paths + k2rule v0.5.0 Upgrade

## Meta

| Field     | Value                                    |
|-----------|------------------------------------------|
| Feature   | platform-paths                           |
| Version   | v1                                       |
| Status    | implemented                              |
| Created   | 2026-02-22                               |
| Updated   | 2026-02-22                               |

## Version History

| Version | Date       | Summary                                    |
|---------|------------|--------------------------------------------|
| v1      | 2026-02-22 | Initial: unified platform paths + k2rule upgrade |

## Product Requirements

- 升级 k2rule 从 v0.4.1-go 到 v0.5.0-go (v1)
- 统一管理跨平台路径（cache、log），所有平台使用同一套 API (v1)
- 桌面端自动确定平台正确的 cache/log 路径，root 和普通用户都能正确运行 (v1)
- 移动端（iOS/Android）由宿主 App 传入 cache 路径 (v1)

## Background: k2rule v0.5.0-go Breaking Changes

| 变更 | 说明 |
|------|------|
| `CacheDir` 必填 | `Config.Validate()` 报错 `"CacheDir is required"`；`SetDefaults()` 不再自动填 |
| 下载无限重试 | `Init()` 网络失败时指数退避重试（1s→64s cap），永不返回下载错误 |
| 各 Manager 内部兜底移除 | 与 CacheDir 必填一致 |
| API 签名不变 | `Init`、`Match`、`SetTmpRule` 签名完全不变 |
| Windows Mmap | 仍未修复（open P1） |

## 运行模式分析

| 模式 | 命令 | 身份 | 需要 TUN? |
|------|------|------|-----------|
| Daemon | `k2 up` → launchd/systemd | root | 是 |
| Foreground TUN | `sudo k2 run -c x.yaml` (mode=tun) | root | 是 |
| Foreground Proxy | `k2 run -c x.yaml` (mode=proxy) | 普通用户 | 否 |
| Mobile | Engine.Start() | App 沙盒 | 由系统管 |

**方案 A（已选）**：使用 `os.UserCacheDir()/k2/` — 每个用户各自的 cache，权限天然隔离。
root 和普通用户的 k2rule 缓存各存一份，但文件仅几 MB，代价可接受。

## Technical Decisions

- **路径包**: `paths/paths.go` — 新建独立包（职责分离）(v1)
- **桌面路径**: 基于 `os.UserCacheDir()` — root 运行得到 root 的目录，普通用户得到用户目录，权限零冲突 (v1)
- **移动端**: `EngineConfig` 对象传入路径，可扩展不改 Start 签名 (v1)
- **Log 路径**: `paths.LogDir()` 提供平台默认 log 目录 (v1)
- **k2rule CacheDir**: `{CacheDir}/k2rule/` 子目录 (v1)
- **Data 路径**: `config.DefaultClientDir()` 保持现有逻辑不变（`/etc/k2`），仅由 root daemon 使用 (v1)

## Design

### 桌面端完整路径表

#### Cache (k2rule 规则等可丢弃缓存)

基于 `os.UserCacheDir()` + `/k2/`

| 身份 | macOS | Linux | Windows |
|------|-------|-------|---------|
| root | `/var/root/Library/Caches/k2/` | `/root/.cache/k2/` | `%ProgramData%\k2\cache\`* |
| 普通用户 | `~/Library/Caches/k2/` | `~/.cache/k2/` | `%LocalAppData%\k2\cache\` |

> *Windows daemon 以 SYSTEM 身份运行，`os.UserCacheDir()` 返回 SYSTEM profile，与普通用户隔离

#### Log

| 身份 | macOS | Linux | Windows |
|------|-------|-------|---------|
| root | `/var/log/k2/` | `/var/log/k2/` | `%ProgramData%\k2\log\` |
| 普通用户 | `~/Library/Logs/k2/` | `~/.local/state/k2/log/` | `%LocalAppData%\k2\log\` |

> Log 路径需要平台分支：macOS 用 `~/Library/Logs/`，Linux 用 XDG_STATE_HOME，Windows 按身份区分。
> Root 使用系统标准 log 目录（`/var/log/`），与 launchd/systemd 日志位置一致。

#### k2rule CacheDir 具体值

| 场景 | k2rule CacheDir |
|------|----------------|
| macOS root | `/var/root/Library/Caches/k2/k2rule/` |
| macOS user proxy | `~/Library/Caches/k2/k2rule/` |
| Linux root | `/root/.cache/k2/k2rule/` |
| Linux user proxy | `~/.cache/k2/k2rule/` |
| iOS | `{app传入}/k2rule/` |
| Android | `{app传入}/k2rule/` |

### 移动端路径 — EngineConfig 对象

由宿主 App 通过 `EngineConfig` 传入，Go 侧不做平台判断。
gomobile 将 `*EngineConfig` 映射为 ObjC class `MobileEngineConfig` / Java class `mobile.EngineConfig`。

```go
// mobile/config.go
package mobile

// EngineConfig holds platform-specific paths and settings for Engine.Start().
// All path fields are provided by the native host (iOS/Android).
// gomobile exports this as MobileEngineConfig (ObjC) / mobile.EngineConfig (Java).
type EngineConfig struct {
    CacheDir string // 必填：k2rule 缓存等。iOS: NSCachesDirectory, Android: context.getCacheDir()
    LogDir   string // 可选：文件日志目录。空 = 不写文件日志（仅 EventHandler.OnLog）
}

// NewEngineConfig creates an EngineConfig with zero values.
func NewEngineConfig() *EngineConfig {
    return &EngineConfig{}
}
```

```go
// Engine.Start 签名变更
// Before:
func (e *Engine) Start(url string, fd int) error

// After:
func (e *Engine) Start(url string, fd int, cfg *EngineConfig) error
```

#### iOS 调用示例

```swift
let cfg = MobileNewEngineConfig()
cfg.cacheDir = FileManager.default.urls(for: .cachesDirectory, in: .userDomainMask).first!.path
cfg.logDir = FileManager.default.urls(for: .libraryDirectory, in: .userDomainMask).first!
    .appendingPathComponent("Logs").path
try engine.start(url, fd: Int(fd), cfg: cfg)
```

#### Android 调用示例

```kotlin
val cfg = Mobile.newEngineConfig()
cfg.cacheDir = context.cacheDir.absolutePath
cfg.logDir = context.filesDir.resolve("logs").absolutePath
engine.start(url, fd, cfg)
```

#### 可扩展性

未来需要新路径（如 DataDir）只需在 `EngineConfig` 加字段，`Start()` 签名不变：

```go
type EngineConfig struct {
    CacheDir string
    LogDir   string
    DataDir  string // future
}
```

### paths/ 包 API

```go
package paths

// CacheDir 返回当前用户的 cache 根目录 + /k2/。
// 基于 os.UserCacheDir()：
//   macOS root:   /var/root/Library/Caches/k2
//   macOS user:   ~/Library/Caches/k2
//   Linux root:   /root/.cache/k2
//   Linux user:   ~/.cache/k2
//   Windows:      %LocalAppData%\k2\cache
func CacheDir() string

// LogDir 返回当前用户的 log 目录。
// root 使用系统标准 log 目录，普通用户使用用户级目录：
//   macOS root:   /var/log/k2
//   macOS user:   ~/Library/Logs/k2
//   Linux root:   /var/log/k2
//   Linux user:   ~/.local/state/k2/log
//   Windows:      %ProgramData%\k2\log (SYSTEM) 或 %LocalAppData%\k2\log (user)
func LogDir() string
```

**实现要点**：
- `CacheDir()`: `os.UserCacheDir()` + `/k2/`，一行搞定
- `LogDir()`: 需要 `os.Getuid() == 0` 判断（Unix）或 `runtime.GOOS` 分支
- 不需要 build tag，用 `runtime.GOOS` + `os.Getuid()` 运行时判断

### 受影响的调用点

#### 1. daemon/tunnel.go — BuildTunnel

```go
// Before:
ruleCfg := &k2rule.Config{
    CacheDir: cfg.Rule.CacheDir,  // 可能为空 → v0.5.0 报错
}

// After:
cacheDir := cfg.Rule.CacheDir
if cacheDir == "" {
    cacheDir = filepath.Join(paths.CacheDir(), "k2rule")
}
ruleCfg := &k2rule.Config{
    CacheDir: cacheDir,
}
```

#### 2. mobile/mobile.go — Engine.Start

```go
// Before:
func (e *Engine) Start(url string, fd int) error
// k2rule.Init(&k2rule.Config{IsGlobal: true, GlobalTarget: k2rule.TargetProxy})

// After:
func (e *Engine) Start(url string, fd int, cfg *EngineConfig) error
// k2rule.Init(&k2rule.Config{
//     IsGlobal:     true,
//     GlobalTarget: k2rule.TargetProxy,
//     CacheDir:     filepath.Join(cfg.CacheDir, "k2rule"),
// })
```

#### 2b. 新增 mobile/config.go

`EngineConfig` struct + `NewEngineConfig()` 构造函数（见上方移动端路径设计）。

#### 3. config/config.go — 注释更新

```go
CacheDir string `yaml:"cache_dir"` // cache directory ("" = os.UserCacheDir()/k2/k2rule)
```

#### 4. daemon/service_darwin.go — launchd log 路径

```go
// Before: logDir := "/tmp/k2"
// After:  logDir := "/var/log/k2"
```

#### 5. 测试修复

所有 `k2rule.Init` 调用点加 `CacheDir: t.TempDir()`。

## Acceptance Criteria

- AC1: `go get github.com/kaitu-io/k2rule@v0.5.0-go` 成功，`go build ./...` 通过 (v1)
- AC2: `paths.CacheDir()` 在 macOS/Linux 返回基于 `os.UserCacheDir()` 的正确路径 (v1)
- AC3: `paths.LogDir()` root 返回 `/var/log/k2/`，普通用户返回用户级 log 目录 (v1)
- AC4: `daemon/tunnel.go` 中 `cfg.Rule.CacheDir` 为空时，自动使用 `paths.CacheDir()/k2rule` (v1)
- AC5: `mobile/Engine.Start(url, fd, cfg)` 接受 `*EngineConfig`，`cfg.CacheDir` 传递给 k2rule.Init (v1)
- AC5b: `mobile/EngineConfig` 被 gomobile 正确导出（`make mobile-ios` / `make mobile-android` 通过）(v1)
- AC6: 所有现有测试通过（`go test ./...`），k2rule 调用点都提供有效 CacheDir (v1)
- AC7: `service_darwin.go` launchd plist 日志路径为 `/var/log/k2/k2.log` (v1)

## Testing Strategy

- paths/ 包：单元测试验证 `runtime.GOOS` 下返回值包含正确子路径 (v1)
- daemon/tunnel.go：验证空 CacheDir 时默认值填充逻辑 (v1)
- mobile/mobile_test.go：验证 cacheDir 参数传递到 k2rule.Init (v1)
- 全量回归：`go test ./...` 通过 (v1)

## Deployment & CI/CD

- `go mod tidy` 更新 go.mod/go.sum (v1)
- `make mobile-ios` / `make mobile-android` 验证 gomobile build (v1)
- Windows 交叉编译仍受 k2rule mmap 阻塞（已知 P1，不在本 feature 范围）(v1)

## Impact Analysis

- **新增 `paths/paths.go`**: 2 个函数 + 测试 (v1)
- **config/config.go**: RuleConfig 注释更新 (v1)
- **daemon/tunnel.go**: CacheDir 默认值填充 (v1)
- **daemon/service_darwin.go**: log 路径从 `/tmp/k2` → `/var/log/k2` (v1)
- **mobile/mobile.go**: Start() 签名变更（接受 `*EngineConfig`）(v1)
- **新增 `mobile/config.go`**: EngineConfig struct + NewEngineConfig() (v1)
- **core/dns/middleware_test.go**: 添加 CacheDir (v1)
- **Scope**: medium — 修改 5 个文件 + 新增 2 个文件
