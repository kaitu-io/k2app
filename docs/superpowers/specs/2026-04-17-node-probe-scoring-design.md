# Node Probe & Scoring System Design

**两个独立消费者，一个共享测量原语**：

1. **App（desktop + mobile + 独立 Web）**：后台静默探测候选节点，UI 展示实测质量（RTT、丢包），让用户看着选。**App 不再做"智能选择"**——server selection 只剩"指定服务器"与"自部署"两种模式。
2. **Headless Linux / 路由器场景（cmd/k2 daemon）**：daemon 用探测结果在 `Subscription.Pick` 里自动选更好的节点，无人值守场景下替换现在纯预算驱动的 `recommendScore`。

**核心转变**：`k2subs://` 从此**只出现在 daemon 路径**（cmd/k2 Linux headless / 未来 gateway），app 永远不接触订阅协议。

**范围说明**：当前 `k2r` gateway（cmd/k2r，TPROXY 软路由）**暂不在本期集成范围**——gateway 今天没有 `k2subs://` 订阅支持（`OutboundProvider` 未接入，见 `k2/gateway/gateway.go` `OnOutboundFatal` 的注释）。本期的"路由器自动选节点"指 cmd/k2 on Linux headless / 家用服务器场景。给 k2r 加 k2subs 支持是独立后续工作，完成后本期的 `probe.Registry` + `Subscription.ScoreSource` 原样复用即可。

---

## 背景与问题

### 现状

| 路径 | 节点选择方式 |
|------|-------------|
| App（所有平台）"智能模式" | `k2subs://` → 客户端（mobile 本地、desktop 由 daemon）weighted-random pick，基于 `recommendScore` |
| App "指定服务器" | 用户从 tunnel 列表手动选一个 k2v5 URL |
| 路由器（k2r） | `k2subs://` → daemon `Subscription.Pick`，基于 `recommendScore` |

`recommendScore`（由 Center `ComputeRecommendScore` 产出）反映节点**流量预算**使用率——即"这个节点还能卖多少带宽"——与用户**实际**访问速度脱钩。两个结果：

1. **App 智能选择不透明**：用户不知道自己被分到哪个节点，也不知道这个节点此刻对自己快不快。
2. **路由器选择不准**：预算充足的节点可能因为拥塞、路由劣化、丢包导致体感差。

### 服务端基础设施已就绪

`k2/wire/echo.go` 已实现完整的 echo probe 协议：

- `EchoProbe(ctx, conn)` — 已建立 QUIC 连接上打开 Stream，发 8 字节 nonce，等 echo 回包，返回 RTT
- `HandleEchoStream` — 服务端 handler，已部署在所有 k2v5 节点
- `EchoProber` 接口 — `StatsProvider` 同级的已注册可选能力

**结论**：服务端零改。客户端与 daemon 层工作。

---

## 目标 / 非目标

### 目标

1. 实现一个共享测量原语 `wire.ProbeURL`：对一个 k2v5 URL 建 QUIC、跑 N 轮 echo、关闭。
2. Daemon 侧内存 `Registry` + 后台 `Service`：定时探测候选集，结果按 URL 键入。
3. Daemon 暴露 `probe` action：webapp 按需触发一次探测，取得 fresh 结果（同时写进 Registry，供 daemon 自己用）。
4. App UI：`CloudTunnelList` 通过现有 `RouteQualityProvider` 钩子读 probe store，每个节点显示 RTT/丢包 chip，排序按实测分。
5. 路由器：`Subscription.Pick` 集成 `ScoreSource`，按 `base × probeScore` 做 **Top-K 加权随机**，冷启动退化为纯 `recommendScore`。
6. **移除 app 智能选择**：`serverMode` 枚举从 `'smart' | 'manual' | 'self_hosted'` 缩减为 `'manual' | 'self_hosted'`。相关 webapp 代码（subs-resolver、SmartServerSelector 的 smart tab、connection.store 的 smart 分支）全部删除。老用户 `serverMode='smart'` 自动迁移到 `'manual'` + 默认选中排序第一的 tunnel。

### 非目标（明确排除）

- **Center 聚合探测数据以更新 `recommendScore`**：Phase 2 话题。
- **连接期主动热切换节点**：路由器下接多设备，切换会扰动所有设备。继续依赖失败驱动的 `OutboundProvider.NextURL`。
- **TCP-WS 路径探测**：v1 只测 QUIC。如果 QUIC 测不通，这个节点就不值得选。
- **Target reachability 探测**（"从这个节点能不能访问 Google"）：只测 wire（client ↔ k2s），不测 k2s ↔ target。
- **Desktop/mobile app 再引入任何 `k2subs://` 处理**：app 永远只发 `k2v5://` 给 engine。

---

## 架构

```
                   ┌─────────────────────────┐
                   │ wire.ProbeURL (共享原语) │
                   │  - 建 QUIC + N 轮 echo   │
                   │  - 不 fallback TCP-WS    │
                   │  - 整体 budget 8s        │
                   └─────────────┬───────────┘
                                 │
              ┌──────────────────┼──────────────────┐
              │                  │                  │
     ┌────────▼────────┐ ┌───────▼───────┐ ┌───────▼────────┐
     │ Daemon Service  │ │ Gateway (k2r) │ │  Ad-hoc probe  │
     │ (desktop + k2r) │ │  同 daemon    │ │ (from webapp)  │
     │ 5 min loop      │ │               │ │  via action    │
     └────────┬────────┘ └───────┬───────┘ └───────┬────────┘
              │                  │                  │
              └─────────┬────────┴──────────────────┘
                        ▼
              ┌──────────────────────┐
              │   probe.Registry     │     ← in-memory only
              │   key = URL          │       TTL 15 min
              │   value = ProbeStats │       无磁盘持久化
              └──────────┬───────────┘
                         │
            ┌────────────┼────────────┐
            ▼                         ▼
   ┌─────────────────┐      ┌──────────────────┐
   │ Subscription.   │      │  /api/core       │
   │ Pick(ScoreSrc)  │      │  action=probe    │
   │ (Router/desktop)│      │  → return stats  │
   └─────────────────┘      └────────┬─────────┘
                                     │
                                     ▼
                          ┌──────────────────────┐
                          │  webapp probe.store  │
                          │  (Zustand, TTL 15min)│
                          └──────────┬───────────┘
                                     ▼
                          ┌──────────────────────┐
                          │  CloudTunnelList UI  │
                          │  - Sort by probeScore│
                          │  - RTT/loss chip     │
                          │  - RecommendDot 保留 │
                          └──────────────────────┘
```

---

## 1. Wire 层：ProbeURL

新文件 `k2/wire/probe.go`：

```go
type ProbeStats struct {
    AvgRttMs   float64
    MinRttMs   float64
    MaxRttMs   float64
    JitterMs   float64    // MaxRttMs - MinRttMs
    LossRate   float64    // 超时次数 / N
    Reachable  bool       // false 表示 QUIC handshake 本身失败
    EchoSupported bool    // 旧 k2s 不支持 echo 时 false（见兼容节）
    MeasuredAt time.Time
}

// ProbeURL 建立独立 QUIC 连接，跑 rounds 轮 echo，关闭连接。
// 不走 TransportManager，不 fallback TCP-WS：probe 失败即节点不值得选。
// 超时：单轮 1s，总 budget = 3s handshake + rounds * interval + 2s tail ≤ ctx deadline。
// dd 为 DirectDialer（daemon/appext 各自提供的物理接口绑定 dialer）。
func ProbeURL(
    ctx context.Context,
    rawURL string,
    dd DirectDialer,
    rounds int,
    interval time.Duration,
) (ProbeStats, error)
```

### 为什么独立函数、不复用 `QUICClient`

`QUICClient` 是长连接实例，`TransportManager` 会持有引用做健康管理。probe 是 one-shot：dial → N echoes → close。硬要复用会引入状态管理复杂度。

### 与 `engine/health.echoLoop` 的区别

| | `echoLoop` | `ProbeURL` |
|---|-----------|-----------|
| 目标 | 监控**已连接**隧道健康 | 评估**候选**节点质量 |
| 连接 | 复用 engine 持有的 TransportManager | 新建独立 QUIC，完成即关 |
| 触发 | engine 生命周期内持续 | 调用方决定（5min / on-demand） |
| 失败后果 | 触发 rerace / transport-switch | 更新 Registry 分数 |

### 并发控制

`ProbeURL` 本身是单次调用。并发由调用方（`probe.Service` 或 daemon action）控制，默认 `max 4 concurrent`——mobile 上保护 iOS NE 50MB budget（虽然 probe 在主进程跑，但主进程资源也要克制），桌面 / 路由器同样避免打开过多 QUIC。

---

## 2. probe 包：Registry + Service

新包 `k2/probe/`：

### 2.1 Registry

```go
// k2/probe/registry.go
type Registry struct {
    mu      deadlock.Mutex
    results map[string]entry  // key: URL
    ttl     time.Duration     // default 15 min
}

type entry struct {
    stats wire.ProbeStats
    score float64  // 预算好的 probeScore，避免 hot-path 重算
}

func NewRegistry(ttl time.Duration) *Registry

// Record 写入测量结果；计算 probeScore 并缓存。
func (r *Registry) Record(url string, stats wire.ProbeStats)

// Score 查询：ok=false 表示无数据或已过期。
// 热路径（Subscription.Pick 每次调）——必须 O(1)。
func (r *Registry) Score(url string) (score float64, ok bool)

// Snapshot 为 API 返回提供只读快照。
func (r *Registry) Snapshot() map[string]wire.ProbeStats
```

**Registry 不持久化**：probe 数据本质是"此刻此设备到此节点"的实测，跨进程无意义。重启后空 Registry 导致冷启动——见 §5 冷启动降级。

### 2.2 Service

```go
// k2/probe/service.go
type URLSource interface {
    CandidateURLs() []string  // 当前应该探测的候选集
}

type Service struct { ... }

func NewService(
    reg *Registry,
    src URLSource,
    dd wire.DirectDialer,
    cadence time.Duration,      // 5 min default
    maxConcurrent int,          // 4 default
    rounds int,                 // 8 default
    interval time.Duration,     // 50ms default
) *Service

func (s *Service) Start(ctx context.Context)  // 后台 goroutine
func (s *Service) Close()
```

Service 内部循环：

```
每 cadence:
  urls := src.CandidateURLs()
  sem := make(chan struct{}, maxConcurrent)
  for _, u := range urls:
    go probeOne(u, sem, reg)  // sem 控制并发
```

### 2.3 评分公式

```go
func computeScore(s wire.ProbeStats) float64 {
    if !s.Reachable {
        return 0
    }
    if !s.EchoSupported {
        return -1  // sentinel：表示"reachable 但 score unknown"
    }
    rttScore := 1.0 / (1.0 + s.AvgRttMs/150.0)       // 150ms 基准
    lossScore := 1.0 - s.LossRate                     // [0,1]
    jitterPenalty := clamp(1.0-(s.JitterMs-50)/200, 0.7, 1.0)
    return lossScore * rttScore * jitterPenalty
}
```

| 场景 | avgRtt | loss | jitter | score |
|------|-------|------|--------|-------|
| 极佳 | 30ms  | 0%   | 10ms   | 0.83  |
| 正常 | 100ms | 0%   | 30ms   | 0.60  |
| 一般 | 200ms | 0%   | 50ms   | 0.43  |
| 略差 | 150ms | 10%  | 80ms   | 0.47  |
| 差   | 200ms | 30%  | 150ms  | 0.22  |
| 不可达 | —   | 100% | —      | 0.00  |
| 旧 k2s | 正常 | —    | —      | -1 (unknown) |

Sentinel `-1` 让消费者能显式降级（UI 显示问号、Pick 回落 base weight）。

---

## 3. Daemon 集成

### 3.1 `probe` action

新增 `/api/core` action（`k2/daemon/daemon.go`）：

```
POST /api/core
{"action": "probe", "params": {"urls": ["k2v5://..."], "timeoutMs": 8000}}

→ {"code": 0, "data": {
    "results": [
      {"url": "k2v5://...", "avgRttMs": 45.2, "minRttMs": 38, "maxRttMs": 68,
       "jitterMs": 30, "lossRate": 0.0, "reachable": true, "echoSupported": true,
       "probeScore": 0.73, "measuredAt": "2026-04-17T10:00:00Z"}
    ]
  }}
```

实现：并发调用 `wire.ProbeURL`（max 4），同时把结果 `Registry.Record`（双用途——UI 拿结果 + daemon 自动选点受益）。单请求总 timeout = `params.timeoutMs`（默认 8000）。

### 3.2 `Subscription.Pick` ScoreSource 集成

`k2/config/subscription.go` 改动：

```go
// 新增
type ScoreSource interface {
    Score(url string) (score float64, ok bool)
}

// Subscription 新增字段
type Subscription struct {
    ...
    scoreSource ScoreSource  // 可选；nil 时退化为纯 recommendScore
}

func (s *Subscription) SetScoreSource(src ScoreSource)

// Pick 改造：
// 1. 对每个候选 t: eff(t) = base(t) * adjustment(t)
//      adjustment(t) = probe.Score(t.URL) if ok && score >= 0 else 1.0
//      score == -1 (EchoSupported=false) → 视为 1.0，不惩罚
//      score == 0 (unreachable) → eff = 0，直接淘汰
// 2. 在 eff > 0 的节点中，按 eff 降序取 Top-K (K=5)
// 3. 在 Top-K 内做加权随机（权重 = eff）
// 4. Top-K 不足 5 时取全部正分节点
// 5. 所有节点 eff = 0 → 回落到 allZero 分支（均匀随机），与现在一致
```

Top-K = 5 是个经验值：够保证多样性（单个节点瞬时抖动时有备胎），又不被拖后腿节点占用采样概率。

### 3.3 Daemon 启动时装配

```go
// daemon.Run()
reg := probe.NewRegistry(15 * time.Minute)
svc := probe.NewService(reg, &daemonURLSource{d}, d.dialer, 5*time.Minute, 4, 8, 50*time.Millisecond)
svc.Start(ctx)
// 在 doUp 里，当创建 subSession 时：
//   sess.sub.SetScoreSource(reg)
```

`daemonURLSource` 返回当前所有 `subSession` 的订阅内 tunnel URL 合集。

### 3.4 k2r 网关（非本期）

k2r gateway（`k2/gateway/gateway.go`）目前不走 Subscription 路径——配置直接收单个 k2v5:// URL，`engine.Config.OutboundProvider` 未注入（见 `gateway.OnOutboundFatal` 的 comment："Gateway does not use k2subs:// subscriptions"）。给它加 k2subs 支持是独立工作：需要镜像 `daemon.resolveSubscriptions` 逻辑、实现 `OutboundProvider.NextURL`、在 `doUp` / `closeTunnel` 里管理 subSession 生命周期。

完成那个独立工作后，本期的 `probe.Registry` + `Subscription.ScoreSource` 接线方式与 daemon 完全相同——同一个 Registry 可被 gateway 复用，无新增协议或原语。

---

## 4. Webapp 集成

### 4.1 删除清单

| 文件 | 行数 | 处理 |
|------|------|------|
| `webapp/src/services/subs-resolver.ts` | 284 | **删除** |
| `webapp/src/services/__tests__/subs-resolver.test.ts` | 371 | **删除** |
| `webapp/src/stores/__tests__/connection.store.smart.test.ts` | 467 | **删除** |
| `webapp/src/components/SmartServerSelector.tsx` | 192 | 重构：改名 `ServerSelector`，去掉 smart tab |
| `webapp/src/stores/connection.store.ts` | — | 见 §4.2 |
| `webapp/src/pages/Dashboard.tsx` | — | 去掉 `smartDisplayTunnel`、`smart` 分支 |

### 4.2 `connection.store` 改造

```ts
// 修改
serverMode: 'manual' | 'self_hosted'  // 删除 'smart'
// 删除
smartCountry, setSmartCountry, persistSmartCountry, SMART_COUNTRY_STORAGE_KEY

// loadServerMode 添加 migration:
//   老值 'smart' → 'manual'
//   并在 loadServerMode 完成后，如 selectedCloudTunnel 为空，
//   调用 selectFirstTunnelByProbeScore() 自动选一个
```

### 4.3 新增：probe store + service

```ts
// webapp/src/stores/probe.store.ts
interface ProbeState {
  results: Map<string, ProbeResult>;  // key: tunnel domain
  lastUpdated: number;                 // ms epoch
  inFlight: Set<string>;               // 正在测量的 domain（UI 显示 skeleton）
  record(results: ProbeResult[]): void;
  markInFlight(domains: string[]): void;
  getScore(domain: string): number | null;  // null = no data or stale
}

// webapp/src/services/probe-service.ts
export async function runProbe(tunnels: Tunnel[]): Promise<void> {
  // 1. 平台检查：仅 desktop + mobile（web fallback 跳过）
  // 2. VPN 状态检查：仅 idle 或 serviceDown（未连接）
  // 3. 网络类型检查（mobile）：仅 WiFi（可用户覆盖）
  // 4. markInFlight
  // 5. await window._k2.run('probe', { urls, timeoutMs: 8000 })
  // 6. store.record(response.data.results)
}
```

### 4.4 UI 钩子

`CloudTunnelList.tsx` 现状：

```tsx
const neutralQualityProvider = useMemo(() => ({ getRouteQuality: () => 0 }), []);
const sortedTunnels = useMemo(() =>
  sortTunnelsByRecommendation(tunnels, neutralQualityProvider),
  [tunnels, neutralQualityProvider]
);
```

改为：

```tsx
const probeQualityProvider = useMemo(() => ({
  getRouteQuality: (domain: string) => useProbeStore.getState().getScore(domain) ?? 0,
}), []);
```

每个 `ListItem` 旁边新增 `ProbeChip` 组件：

```tsx
<ProbeChip
  result={probeStore.results.get(domain)}
  loading={probeStore.inFlight.has(domain)}
/>
```

`ProbeChip` 显示：
- `loading` → MUI Skeleton（"测量中..."）
- `result.reachable && result.echoSupported` → `"45ms"` + 丢包 ≥ 5% 时 "⚠ 10%"
- `result.reachable && !result.echoSupported` → `"?"` 图标（旧服务器）
- `!result.reachable` → `"—"` 灰字（不可达）
- `result` 不存在（从未探测）→ 空（不占位）

`RecommendDot` **保留原状**——独立展示"Center 预算信号"（容量）。两者并列让用户看到分歧："Center 说这节点健康（绿点）但你到它 300ms（RTT chip）" 是真实信息。

### 4.5 触发时机

```
App 启动 / 首次展示 CloudTunnelList:
  → 阻塞式 runProbe（显示 Skeleton，超时 5s 后强制显示列表）

VPN 断开（idle 状态进入）:
  → 立即 runProbe（非阻塞）

每 5 min 定时（仅 VPN idle 状态）:
  → runProbe

App 回前台（visibilitychange → visible）+ 上次测量 > 2min:
  → runProbe

触发集：`/api/tunnels/k2v4` 返回的全部 tunnels（通常 < 30 个，8s budget 内够）。
```

### 4.6 平台门控

- `platformType === 'web'`（标准浏览器 standalone）：跳过。`standalone-k2` 没有真实 QUIC。
- `platformType === 'desktop'`：daemon 在跑，probe action 走 HTTP 到 127.0.0.1:1777。
- `platformType === 'mobile'`：
  - 必须 VPN **未连接**（避免双封装）
  - 默认仅 WiFi（设置项可改"所有网络"）
  - 回前台立即测一次

---

## 5. 边界情况（spec 必须显式）

### 5.1 路由器冷启动

Registry 为空时，`Subscription.Pick` 的 `ScoreSource.Score()` 全部返回 `ok=false` → `adjustment=1.0` → 完全退化为纯 `recommendScore`。这是**预期行为**，不是 bug。

**Warmup 时间**：Service 启动后第一轮 probe 约需 `8s`（受 maxConcurrent=4 限制，30 节点约 60s）。这期间选节点用 base weight，无感知。

### 5.2 连接期不做主动热切换

路由器下接多设备。主动切换节点意味着所有 TCP 连接断裂，浏览器、视频、在线会议全中断。所以：

- 连接期的 Probe 结果**只更新 Registry**，不驱动切换
- 切换仍由**失败驱动**：engine `OutboundProvider.NextURL` → `Pick(excluded)` 自然会选 Registry 里分高的节点
- 未来如果有"用户主动换节点"按钮，手动切换自然受益于 Registry

### 5.3 旧 k2s 兼容

当 `wire.ProbeURL` 遇到不支持 echo 的服务器（`wire.isEchoUnsupported(err) == true`）：
- `ProbeStats.Reachable = true`
- `ProbeStats.EchoSupported = false`
- `ProbeStats.AvgRttMs = 0`（handshake 成功即 reachable，但 RTT 未测出）
- Registry 存 `score = -1`（sentinel）
- `Subscription.Pick`：sentinel → `adjustment = 1.0`，不惩罚
- UI：`ProbeChip` 显示 `?` 图标

### 5.4 探测流量预算

路由器：5 min × 30 节点 × 8 轮 echo ≈ 30 × (10KB handshake + 8 × 16B echo) ≈ 300KB / 5min = 3.6 MB/小时。家用路由无所谓。

Mobile：同样量。默认 WiFi-only。设置项允许用户开启"所有网络探测"（给无 WiFi 用户）。

**用户可见开关**（webapp 设置页）：
- "自动测量节点速度"（默认开）
- "仅 WiFi 时测量"（默认开，mobile only）

### 5.5 Mobile 双封装

Mobile 的 NE/VpnService 在独立进程。主进程发出的 QUIC 流量默认走 VPN（如果 VPN 连着）。probe 必须在 VPN idle 时执行——v1 的保证。

实现层：`probe-service.ts::runProbe()` 入口处检查 `useVPNMachineStore.getState().state`，仅当 `'idle' || 'serviceDown'` 时执行。

Desktop：daemon `DirectDialer` 绑定物理接口（sing-tun 的 route exclusion），QUIC 流量绕 TUN，不存在此问题。

### 5.6 Smart mode 用户迁移

升级后 `loadServerMode` 读到 `'smart'`：
```ts
if (resolvedMode === 'smart') {
  resolvedMode = 'manual';
  // 首次进入 Dashboard 时，如果还没 activeTunnel，
  // 自动选 sortedTunnels[0]（probeScore 最高；冷启动时 recommendScore 最高）
}
```

一次性引导（可选 P1）：用户升级后第一次打开 Dashboard，显示一次轻量 Snackbar：
> "智能选择已升级：现在你可以看到每个节点的实测速度。已自动为你选中当前最快的节点。"

---

## 6. 数据流时序

### App（典型连接流程）

```
用户打开 app
  ├── Dashboard mount → CloudTunnelList 加载
  ├── /api/tunnels/k2v4 → tunnels[]
  └── probe-service.runProbe(tunnels)
        ├── markInFlight(domains)  [UI 显示 Skeleton]
        └── _k2.run('probe', {urls})
              └── daemon 并发 wire.ProbeURL
                    └── 结果 → response + Registry.Record
        └── store.record(results)   [UI 更新 chip + 重排序]

用户点击某 tunnel
  └── connection.store.selectCloudTunnel(tunnel)
        └── setServerMode('manual'), activeTunnel = tunnel

用户点连接
  └── connection.store.connect()
        ├── buildConnectConfig({ serverUrl: tunnel.serverUrl })
        └── _k2.run('up', config)   // config.routes[0].via = k2v5://
```

### 路由器

```
k2r 启动
  ├── NewSubscription(k2subs URL) → sub
  ├── probe.NewRegistry + NewService(reg, urlSource, ...)
  ├── sub.SetScoreSource(reg)
  └── svc.Start(ctx)

5 min 循环（svc 内）
  └── for each URL: wire.ProbeURL → reg.Record

engine 请求替换节点（NextURL）
  └── sub.Pick(excluded) 使用 reg 打 Top-K 加权随机
```

---

## 7. 变更范围汇总

| 文件/模块 | 类型 | 说明 |
|-----------|------|------|
| `k2/wire/probe.go` | 新增 | `ProbeURL`, `ProbeStats` |
| `k2/wire/probe_test.go` | 新增 | unit + 与 HandleEchoStream 的 in-memory server 集成测试 |
| `k2/probe/registry.go` | 新增 | 内存 Registry + TTL |
| `k2/probe/service.go` | 新增 | 后台 Service |
| `k2/probe/*_test.go` | 新增 | unit tests |
| `k2/config/subscription.go` | 改动 | `ScoreSource` 接口 + `Pick` Top-K 逻辑 |
| `k2/config/subscription_test.go` | 改动 | 补 ScoreSource + Top-K 测试 |
| `k2/daemon/daemon.go` | 改动 | 装配 Registry/Service；新增 `probe` action |
| `k2/daemon/outbound_provider.go` | 无改动 | `Pick` 自动受益 |
| `k2/gateway/` | **非本期** | gateway 未支持 k2subs，需先做 subscription 支持（独立工作），完成后复用本期的 Registry |
| `desktop/src-tauri/src/` | 无改动 | `handle_k2_run` 已泛型转发 |
| `mobile/plugins/K2Plugin.*` | 无改动 | `run()` 已泛型转发 |
| `webapp/src/services/subs-resolver.ts` | **删除** | -284 |
| `webapp/src/services/__tests__/subs-resolver.test.ts` | **删除** | -371 |
| `webapp/src/stores/__tests__/connection.store.smart.test.ts` | **删除** | -467 |
| `webapp/src/components/SmartServerSelector.tsx` | 简化 + 改名 | 去 smart tab → `ServerSelector.tsx` |
| `webapp/src/stores/connection.store.ts` | 改动 | serverMode 缩小枚举 + migration |
| `webapp/src/pages/Dashboard.tsx` | 改动 | 去 smartDisplayTunnel 及相关 |
| `webapp/src/services/probe-service.ts` | 新增 | probe API 封装 |
| `webapp/src/stores/probe.store.ts` | 新增 | Zustand store |
| `webapp/src/components/ProbeChip.tsx` | 新增 | UI chip |
| `webapp/src/components/CloudTunnelList.tsx` | 改动 | 接 probeQualityProvider + 渲染 ProbeChip |
| `webapp/src/services/api-types.ts` | 改动 | 新增 `ProbeResult` 类型 |
| `webapp/src/types/kaitu-core.ts` | 无改动 | `run<T>()` 已泛型 |

净代码变化：webapp 删除约 1000 行（含测试），新增约 300 行 + k2 侧新增约 400 行。**净减 300 行**。

---

## 8. 测试策略

### 8.1 Wire 层

| 测试 | 文件 |
|------|------|
| `ProbeURL` 正常 8 轮 echo（与内存 server 对测） | `k2/wire/probe_test.go` |
| `ProbeURL` handshake 失败 → Reachable=false | 同上 |
| `ProbeURL` 旧 server（不支持 echo）→ EchoSupported=false, Reachable=true | 同上 |
| `ProbeURL` 部分轮丢包 → LossRate 正确 | 同上 |
| `ProbeURL` ctx cancel 及时返回 | 同上 |

### 8.2 probe 包

| 测试 | 文件 |
|------|------|
| `Registry.Record` + `Score` 基础 | `k2/probe/registry_test.go` |
| `Registry` TTL 过期返回 ok=false | 同上 |
| `Registry` score 计算公式边界（loss=0/1, rtt=0/∞, jitter=0/∞） | 同上 |
| `Service.Start` 循环触发 probe（mock URLSource） | `k2/probe/service_test.go` |
| `Service` maxConcurrent 限制生效 | 同上 |

### 8.3 Subscription

| 测试 | 文件 |
|------|------|
| `Pick` with ScoreSource: Top-K 加权随机分布 | `k2/config/subscription_test.go` |
| `Pick` with ScoreSource nil: 行为与改造前一致 | 同上 |
| `Pick` with score = -1 (sentinel): 不惩罚 | 同上 |
| `Pick` with score = 0: 淘汰 | 同上 |
| `Pick` cold start（全 `ok=false`）: 退化为 base | 同上 |

### 8.4 Daemon

| 测试 | 文件 |
|------|------|
| `probe` action 返回结果 + Registry 写入 | `k2/daemon/daemon_probe_test.go` |
| `probe` 超时部分完成：完成的有结果，未完成忽略 | 同上 |
| Service 与 subSession 生命周期协同 | 同上 |

### 8.5 Webapp

| 测试 | 文件 |
|------|------|
| `probe.store` record + getScore + TTL | `probe.store.test.ts` |
| `probe-service.runProbe` 平台门控（web 跳过） | `probe-service.test.ts` |
| `probe-service.runProbe` VPN 连接中跳过 | 同上 |
| `probe-service.runProbe` WiFi-only 门控（mobile） | 同上 |
| `CloudTunnelList` 按 probeScore 排序 | 现有 `CloudTunnelList.test.tsx` 扩展 |
| `ProbeChip` 渲染 4 种状态（loading/ok/unsupported/unreachable） | `ProbeChip.test.tsx` |
| `connection.store` smart → manual migration | 现有 connection.store.test.ts 扩展 |

### 8.6 回归

- 现有 `CloudTunnelList.test.tsx` — 需更新 `neutralQualityProvider` 相关断言
- 现有 `connection.store.test.ts` — 验证 `serverMode` 枚举缩小
- E2E（playwright）：手动连接流程不受影响

---

## 9. 发布计划

分两轮：

### Round 1：k2 侧（独立，不影响 app）

1. `wire.ProbeURL` + test
2. `probe` 包 + test
3. `Subscription` ScoreSource 集成 + test
4. Daemon `probe` action + test
5. Gateway 装配（与 daemon 对齐）
6. 发 k2r 新版本——路由器立即受益

这轮没有任何 app 可见变化。Daemon `probe` action 可以发，webapp 还没用。

### Round 2：webapp 侧

1. 新增 `probe.store` + `probe-service` + `ProbeChip`
2. `CloudTunnelList` 接入 probeQualityProvider
3. 删除 subs-resolver、SmartServerSelector 的 smart tab、connection.store 的 smart 分支
4. Migration 逻辑 + 一次性 Snackbar
5. Desktop + mobile 同步发

关键回归点：
- 老用户的 `serverMode='smart'` 必须无缝降级（进 app 直接能连）
- Probe 失败时 UI 不卡（chip 为空，列表正常用）
- VPN 连接流程完全不依赖 probe（即便 probe 永远不触发，app 照常工作）

---

## 10. 安全 & 隐私

- `ProbeURL` 用 `k2v5://` URL 里的 `udid:token` 凭据（与正常连接相同）。服务端校验与 TCP/UDP 请求一致。
- 不向第三方端点发送任何数据（只访问已知 k2v5 节点）。
- Probe 结果仅在本地 Registry / Zustand store，不上报 Center。（未来 Center 聚合是 Phase 2 的显式决策。）
- 并发上限（4）防止探测本身消耗过多带宽。
- Mobile WiFi-only 默认开，避免未告知用户消耗蜂窝数据。

---

## 11. 未来工作（明确非本期）

- **Center 聚合客户端探测数据**：群体智能更新 `recommendScore`。需要设计上报协议 + 隐私处理。
- **连接期主动热切换**：路由器下接多设备时风险大，需先设计"不破坏活跃 TCP"的迁移策略（可能结合 MPTCP / QUIC migration）。
- **Target reachability 探测**：从节点出去打 Google/YouTube 等，识别"VPN 连着但 YouTube 打不开"的场景。
- **历史趋势**：保留 probe 历史（过去 24h）做 sparkline 图表，让用户看到节点稳定性。
- **智能选择回归**：如果未来发现用户强需求，可以在 UI 里加"自动/手动"开关，`'auto'` 分支本质上就是 UI 端复用 Registry 的 Top-K 逻辑——和路由器代码对齐。

---

## 12. Spec Self-Review

- **Placeholder 扫描**：无 TBD / TODO。
- **内部一致性**：Architecture 图、变更清单、测试策略三者覆盖一致；ScoreSource 在 §2.1 定义、§3.2 使用、§8.3 测试。
- **Scope 检查**：单份 spec，实现可在两个 round 内完成。分两 round 发布正是因为这是独立的——k2 侧先发，webapp 侧后发。
- **歧义检查**：Top-K 的 K=5、Probe cadence=5min、rounds=8、interval=50ms、maxConcurrent=4 都给了具体数字；未来调优时改数字即可。Sentinel `-1` vs `0` 语义已显式。
