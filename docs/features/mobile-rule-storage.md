# Feature: Unified Engine + Rule Mode

## Meta

| Field     | Value                                    |
|-----------|------------------------------------------|
| Feature   | unified-engine                           |
| Version   | v1                                       |
| Status    | implemented                              |
| Created   | 2026-02-16                               |
| Updated   | 2026-02-16                               |

## Version History

| Version | Date       | Summary                                                  |
|---------|------------|----------------------------------------------------------|
| v1      | 2026-02-16 | Initial: unified engine, rule mode, mobile storage       |

## Overview

将 desktop `daemon/tunnel.go` 的 `BuildTunnel()` 和 mobile `mobile/mobile.go` 的
`Engine.Start()` 中 80% 重复的隧道组装逻辑提取到共享 `engine/` 包。Desktop daemon
变为 Engine 的薄 HTTP 壳，mobile wrapper 变为 gomobile 类型适配层。统一过程中同时
解决 mobile 缺失的 k2rule 存储目录和规则模式切换问题。

## Product Requirements

- PR1: Desktop 和 Mobile 使用同一个 Engine 组装隧道，消除 80% 代码重复 (v1)
- PR2: Desktop daemon 变为 Engine 的薄 HTTP 壳，不再自行组装隧道 (v1)
- PR3: 用户可在移动端切换路由规则模式：global（全局代理）和 smart（智能分流） (v1)
- PR4: 规则模式切换需断开重连，不支持热切换 (v1)
- PR5: 规则模式选择持久化到本地原生存储，重启后保留 (v1)
- PR6: Go Engine 拥有可写存储目录，k2rule 可下载 GeoIP/域名规则到本地缓存 (v1)
- PR7: iOS 双进程（App + NE）共享同一存储目录（App Group container） (v1)
- PR8: smart 模式下首次连接自动下载规则文件（k2rule lazy load），不阻塞连接建立 (v1)
- PR9: webapp `connect(wireUrl)` 接口签名不变，K2Plugin 内部追加 rule 参数 (v1)

## Technical Decisions

### TD1: 新建 `k2/engine/` 共享包 (v1)

Engine 是唯一的隧道生命周期管理器。核心 API：

```go
// engine/engine.go
type Engine struct { ... }

type Config struct {
    WireURL        string
    FileDescriptor int              // -1 = 自建 TUN (desktop), >=0 = 平台传入 (mobile)
    DataDir        string           // k2rule 缓存目录
    RuleMode       string           // "global" | "smart", 默认 "global"
    DirectDialer   *directdial.Dialer // desktop only, mobile 传 nil
    PreferIPv6     bool             // desktop only
    Mode           string           // "tun" | "proxy", 默认 "tun"
    ProxyListen    string           // proxy 模式监听地址
    DNSExclude     []netip.Addr     // desktop route exclusion 用
    RuleConfig     *k2rule.Config   // 完整 k2rule 配置，nil 时从 RuleMode+DataDir 自动构建
}

func New() *Engine
func (e *Engine) Start(cfg Config) error
func (e *Engine) Stop() error
func (e *Engine) StatusJSON() string
func (e *Engine) SetEventHandler(h EventHandler)
```

理由：Config struct 包含所有平台差异的可选字段。gomobile 无法直接使用此 API
（struct 不支持），所以 `mobile/` 包提供 gomobile 友好的薄壳。

### TD2: `k2/mobile/mobile.go` 变为 gomobile 薄壳 (v1)

```go
// mobile/mobile.go — 仅做类型适配
type Engine struct {
    inner *engine.Engine
}

func NewEngine() *Engine {
    return &Engine{inner: engine.New()}
}

func (e *Engine) Start(url string, fd int, dataDir string) error {
    return e.inner.Start(engine.Config{
        WireURL:        url,
        FileDescriptor: fd,
        DataDir:        dataDir,
        // RuleMode 从 URL query 自动解析
    })
}
```

gomobile 只导出 `Start(string, int, string)` — 三个基础类型参数。

### TD3: `k2/daemon/` 直接使用 `engine.Engine` (v1)

```go
// daemon/daemon.go — doUp() 改造
func (d *Daemon) doUp(wireURL, configPath string, pid int) error {
    cfg := d.buildEngineConfig(wireURL, configPath)
    eng := engine.New()
    if err := eng.Start(cfg); err != nil { ... }
    d.engine = eng
    ...
}
```

`daemon/tunnel.go` 的 `BuildTunnel()` 被 `engine.Start()` 替代，可删除。
daemon 仅保留：HTTP API、持久化状态、自动重连、进程监控。

### TD4: Engine 统一隧道组装流程 (v1)

Engine.Start() 内部流程：

```
1. k2rule Init (从 Config.RuleMode + DataDir 或 Config.RuleConfig)
2. wire.ParseURL(WireURL)
3. [如果 PreferIPv6 && wireCfg.IPv6 可用] 替换 Host
4. [如果 DirectDialer != nil] 绑定到 transport
5. 构建 Transports (QUIC + TCPWS)
6. 构建 Router
7. 构建 Provider:
   - fd >= 0 → NewTUNProvider(fd)
   - fd == -1 && Mode == "proxy" → NewProxyProvider
   - fd == -1 && Mode == "tun" → NewTUNProvider(自建, DNSExclude)
8. 构建 ClientTunnel
9. 启动:
   - fd >= 0 (mobile) → prov.Start(ctx, &dnsHandler{...})  // DNS 中间件拦截
   - fd < 0 (desktop) → tunnel.Start(ctx)                   // route exclusion
```

Desktop 独有功能（DirectDialer、IPv6 优选、Proxy 模式、DNS 排除）通过 Config
可选字段注入，mobile 不设置则跳过。

### TD5: `rule=smart|global` 由 K2Plugin 原生层追加到 URL (v1)

webapp `connect(wireUrl)` 签名不变。K2Plugin 在发起连接前读取本地存储的
ruleMode，追加 `&rule=xxx` 到 wireUrl。

### TD6: Engine 从 URL 或 Config 获取 RuleMode (v1)

优先级：`Config.RuleConfig`（desktop 完整配置）> `Config.RuleMode` > URL query `rule=` > 默认 `global`。

Engine 内部逻辑：
```go
if cfg.RuleConfig != nil {
    k2rule.Init(cfg.RuleConfig)  // desktop: 完整 k2rule 配置
} else {
    // mobile: 从 RuleMode + DataDir 构建
    isGlobal := ruleMode != "smart"
    k2rule.Init(&k2rule.Config{
        IsGlobal: isGlobal, GlobalTarget: k2rule.TargetProxy,
        CacheDir: cfg.DataDir,
    })
}
```

### TD7: 存储目录 (v1)

| 平台    | dataDir                                            |
|---------|----------------------------------------------------|
| Desktop | 不传 DataDir，通过 RuleConfig.CacheDir 控制 (~/.cache/k2rule/) |
| iOS     | App Group container (`group.io.kaitu`) + `/k2`     |
| Android | `context.filesDir.absolutePath`                    |

### TD8: ruleMode 持久化 (v1)

| 平台    | 存储位置                                           |
|---------|----------------------------------------------------|
| iOS     | `UserDefaults(suiteName: kAppGroup)` key `ruleMode` |
| Android | `SharedPreferences("k2vpn")` key `ruleMode`        |
| Desktop | 不适用（config.yaml 中 `rule.global` 字段）         |

### TD9: iOS NE 冷启动 (v1)

ruleMode 已内嵌在 providerConfiguration 的 wireUrl 中。NE 冷启动直接从
wireUrl 解析 rule 参数，无需额外读取 UserDefaults。

## Acceptance Criteria

### Engine 统一

- AC1: 新建 `k2/engine/` 包，包含 Engine struct + Config struct + Start/Stop/StatusJSON (v1)
- AC2: Engine.Start() 统一实现隧道组装流程：k2rule → transport → router → provider → tunnel (v1)
- AC3: fd >= 0 时使用平台 TUN fd + DNS middleware 拦截模式 (v1)
- AC4: fd == -1 时自建 TUN（desktop）+ route exclusion 模式 (v1)
- AC5: Config.DirectDialer != nil 时绑定到 transport (v1)
- AC6: Config.PreferIPv6 == true 时检查并替换 wireCfg.Host (v1)
- AC7: Config.Mode == "proxy" 时使用 ProxyProvider (v1)

### Desktop 迁移

- AC8: `daemon/tunnel.go` 的 `BuildTunnel()` 删除，daemon.doUp() 改用 engine.Engine (v1)
- AC9: daemon 现有 HTTP API（up/down/status/version/get_config/ping）行为不变 (v1)
- AC10: daemon 自动重连、状态持久化、进程监控逻辑不变 (v1)
- AC11: `cargo test` (desktop Rust) + `yarn test` (webapp) 通过 (v1)

### Mobile 迁移

- AC12: `mobile/mobile.go` 变为 `engine.Engine` 的 gomobile 薄壳 (v1)
- AC13: `Engine.Start(url, fd, dataDir)` — gomobile 导出三个基础类型参数 (v1)

### Rule Mode

- AC14: Engine 从 URL query 解析 `rule=smart|global`，默认 `global` (v1)
- AC15: `rule=smart` → k2rule `IsGlobal: false` + CacheDir；`rule=global` → `IsGlobal: true` (v1)
- AC16: K2Plugin.swift 新增 `setRuleMode`，connect() 追加 `&rule=xxx` (v1)
- AC17: K2Plugin.kt 新增 `setRuleMode`，startVpnService() 追加 `&rule=xxx` (v1)
- AC18: PacketTunnelProvider 传 App Group `/k2` 路径作为 dataDir (v1)
- AC19: K2VpnService 传 `filesDir` 作为 dataDir (v1)
- AC20: definitions.ts + native-client.ts 新增 setRuleMode 声明和实现 (v1)
- AC21: Dashboard UI 调用 setRuleMode 后断开重连生效 (v1)

## Testing Strategy

- **engine 包单元测试**: Config 各组合（mobile/desktop/proxy）的组装逻辑 (v1)
- **daemon 回归测试**: api_test.go 验证 up/down/status 行为不变 (v1)
- **mobile 集成测试**: 手动验证 iOS/Android setRuleMode + connect + dataDir (v1)
- **iOS NE 冷启动**: 杀进程后系统自动重连，验证 rule 参数正确传递 (v1)
- **Desktop 端到端**: `make dev` 验证连接/断开/重连/自动重连全流程 (v1)

## Deployment & CI/CD

- gomobile bind 重新构建 AAR/xcframework (v1)
- CI 需确保 `go test ./engine/...` 通过 (v1)
- 无 CI 流程变更，复用现有 pipeline (v1)

## Impact Analysis

- 新建文件 (v1):
  - `k2/engine/engine.go` — Engine struct, Start(), Stop(), StatusJSON()
  - `k2/engine/config.go` — Config struct + 默认值
  - `k2/engine/dns_handler.go` — dnsHandler (从 mobile/mobile.go 移出)
  - `k2/engine/event.go` — EventHandler 接口 + state 常量
- 重构文件 (v1):
  - `k2/daemon/daemon.go` — Daemon.doUp() 改用 engine.Engine
  - `k2/daemon/tunnel.go` — BuildTunnel() 删除
  - `k2/mobile/mobile.go` — 变为 engine.Engine 的 gomobile 薄壳
- 修改文件 (v1):
  - `K2Plugin.swift` — setRuleMode() + connect() URL 拼接
  - `K2Plugin.kt` — setRuleMode() + startVpnService() URL 拼接
  - `PacketTunnelProvider.swift` — 传 dataDir 给 engine.start()
  - `K2VpnService.kt` — 传 dataDir 给 engine.start()
  - `definitions.ts` — setRuleMode 声明
  - `native-client.ts` — setRuleMode 代理调用
- 不变 (v1):
  - `wire.ParseURL()` — 已忽略未知参数
  - `config/config.go` — ClientConfig/RuleConfig 不变
  - `provider/tun_*.go` — 不变
  - `directdial/` — 不变
  - `daemon/api.go` — HTTP handler 不变（调用的 doUp/doDown 签名不变）
  - `daemon/state.go` — 持久化逻辑不变
  - `webapp/connect()` 签名不变
- 范围：large — 4 新文件，3 重构，6 修改
