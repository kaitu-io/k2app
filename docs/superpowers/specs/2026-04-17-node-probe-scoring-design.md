# Node Probe & Scoring System Design

**目标**：在用户连接前，通过后台静默探测评估各候选节点的实际网络质量，选出吞吐表现最好的节点，替代现有纯预算驱动的 `recommendScore` 作为节点排序主信号。

**背景**：现有 `recommendScore`（`ComputeRecommendScore`）仅反映节点流量预算使用率，与用户实际访问速度无关。`subs-resolver.ts` 的 `pickWeighted` 用此分数加权随机选节点，导致高预算节点不一定网速好。

---

## 核心发现：服务端基础设施已就绪

`k2/wire/echo.go` 已实现完整的 echo probe 协议：

- **`EchoProbe(ctx, conn)`** — 在已建立的 QUIC 连接上打开 Stream，发送 8 字节随机 nonce，等待 echo 回包，返回 RTT
- **`HandleEchoStream`** — 服务端 handler，已部署在所有 k2v5 节点
- **`buildAndProbeQUIC`** — 建 QUIC 连接 + 运行 echo probe 的完整流程

**结论**：服务端无需改动，所有工作在客户端和 daemon 层完成。

---

## 架构总览

```
webapp (后台 ProbeScheduler)
  ↓ window._k2.run('probe', {urls:[...]})
k2 daemon / K2Plugin (native bridge)
  ↓ 对每个 k2v5 URL: buildAndProbeQUIC → N × EchoProbe
k2v5 server (HandleEchoStream — 已部署，无需改动)
  ↑ RTT × N 测量结果
daemon → {url, avgRttMs, minRttMs, maxRttMs, lossRate}[]
  ↑ window._k2.run('probe') 返回值
webapp ProbeScheduler → 写 _platform.storage
webapp subs-resolver → 读 probeScore → 乘以 recommendScore → pickWeighted
```

---

## 1. Wire 层：多次 Echo Probe

现有 `EchoProbe` 每次测一个 RTT。新增 `MultiEchoProbe`，在同一 QUIC 连接上连续发 N 次 probe（N=10，间隔 30ms）：

```go
// k2/wire/echo.go 新增

const (
    ProbeRounds    = 10
    ProbeIntervalMs = 30
)

type ProbeStats struct {
    AvgRttMs float64
    MinRttMs float64
    MaxRttMs float64
    // LossRate = timeouts / N (QUIC stream timeout = echo 无响应)
    LossRate float64
}

// MultiEchoProbe 在已建立连接上运行 N 轮 echo，返回统计结果。
// QUIC stream 超时（context.DeadlineExceeded）计为丢包事件。
func MultiEchoProbe(ctx context.Context, conn *quic.Conn, rounds int, interval time.Duration) (ProbeStats, error)
```

**为什么 QUIC stream timeout 可作为"丢包"信号**：QUIC 会自动重传 Stream 数据，但如果路径严重拥塞，重传时间超过 `echoTimeout`（1s），就会触发 context deadline。这是真实网络质量下降的可靠指标，比直接测 UDP 丢包更稳定（无 QUIC 重传噪音）。

---

## 2. Daemon：新增 `probe` Action

### 接口

```
POST /api/core
{"action": "probe", "params": {"urls": ["k2v5://udid:token@host1:443/...", ...], "timeoutMs": 8000}}
```

```json
{
  "code": 0,
  "data": {
    "results": [
      {"url": "k2v5://...host1...", "avgRttMs": 45.2, "minRttMs": 38.0, "maxRttMs": 68.0, "lossRate": 0.0, "reachable": true},
      {"url": "k2v5://...host2...", "avgRttMs": 0,    "minRttMs": 0,    "maxRttMs": 0,    "lossRate": 1.0, "reachable": false}
    ]
  }
}
```

### 实现逻辑（k2/daemon/daemon.go 新增 case "probe"）

```
1. 并发对每个 URL 执行 probeOne(url, timeout)
2. probeOne:
   a. buildAndProbeQUIC（建 QUIC 连接 + 基础 echo，已有实现）
   b. MultiEchoProbe(conn, N=10, interval=30ms)
   c. Close 连接（不建 TUN，不改路由）
   d. 返回 ProbeStats
3. 并发数上限 = min(len(urls), 5)，防止移动端同时打开过多 QUIC 连接
4. 整体 timeout = params.timeoutMs（建议 8000ms）
```

**与 `up` action 的关键区别**：probe 完成后立即 `conn.Close()`，不进入 engine.Start()，不建 TUN 设备，不改系统路由。

---

## 3. Native Bridge 扩展

### 3.1 IK2Vpn 接口（webapp/src/types/kaitu-core.ts）

`IK2Vpn.run()` 已是泛型 `run<T>(action, params)` 接口，**无需改动**。webapp 直接调用：

```ts
window._k2.run<ProbeResponse>('probe', { urls, timeoutMs: 8000 })
```

### 3.2 Tauri bridge（desktop/src-tauri/src/）

新增 `probe` 分支到 `handle_k2_run` 命令处理器（与 `up`/`down`/`status` 同级），将请求转发到 daemon HTTP API `/api/core`。**Pattern 与现有 action 完全一致，无新增 IPC 命令类型**。

### 3.3 Capacitor bridge（mobile/plugins/）

K2Plugin.swift 和 K2Plugin.kt 的 `run` 方法已路由所有 action 到 Go engine。probe 走同一路径，但 Go 侧（appext 内部）调用 `MultiEchoProbe` 而非 `engine.Start()`。

**Mobile 特殊注意**：probe 在主 App 进程执行（不是 NE/VPN extension），因为：
- NE 进程有 50MB jetsam 限制
- probe 只需要 QUIC 客户端，不需要 TUN 设备权限
- K2Plugin 的 `run()` 在主进程中执行

---

## 4. Webapp：ProbeScheduler

新文件：`webapp/src/services/probe-scheduler.ts`

### 职责

1. 在后台定期触发 probe（每 **5 分钟**，仅 WiFi/充电时）
2. 将结果写入 `_platform.storage`（keyed by server domain）
3. 结果有效期 **30 分钟**（超过后 subs-resolver 忽略）

### 触发时机

```
a. App 回到前台（visibilitychange → visible）
b. 订阅列表刷新后（subs-resolver resolveTunnel 拿到 fresh 数据时）
c. 定时 setInterval(5min)，仅在 serverMode === 'smart' 时激活
```

### 探测候选集来源

从 subs-resolver 最近一次 `resolveTunnel` 的 `allCandidates`（最多取前 **10** 个，按 `recommendScore` 降序排列）获取候选 URL。

### 存储格式

```ts
// _platform.storage key: 'k2.probe.results'
interface ProbeResultsCache {
  probedAt: number;         // ms epoch
  results: ProbeResult[];
}

interface ProbeResult {
  url: string;              // 完整 k2v5:// URL（含 udid:token）
  domain: string;           // hostname，用于 lookup
  avgRttMs: number;
  minRttMs: number;
  maxRttMs: number;
  lossRate: number;         // [0,1]
  reachable: boolean;
  probeScore: number;       // 计算后缓存，避免重复计算
}
```

### 平台门控

```ts
// probe 仅在 desktop + mobile 上运行
// web 平台跳过（window._k2 是 standalone-k2，无真实 QUIC 实现）
if (window._platform.platformType === 'web') return;
```

---

## 5. 评分公式

### ProbeScore（客户端计算，probe 完成后）

```
probeScore = (1 - lossRate) / (1 + avgRttMs / 200)
```

| 场景 | lossRate | avgRttMs | probeScore |
|------|----------|----------|------------|
| 极佳节点 | 0%   | 30ms  | 0.87 |
| 正常节点 | 0%   | 100ms | 0.67 |
| 一般节点 | 0%   | 200ms | 0.50 |
| 略差节点 | 10%  | 150ms | 0.51 |
| 差节点   | 30%  | 200ms | 0.35 |
| 不可达   | 100% | —     | 0.00 |

200ms 为"正常延迟基准"（符合亚太区大多数场景）。

### FinalScore（subs-resolver 融合）

```
finalScore = probeScore × recommendScore
```

**乘法语义**：
- probeScore 好但预算超标（recommendScore 低）→ 最终分低 → 保护节点不被滥用
- 预算充足但探测质量差 → 最终分低 → 不选网速差的节点
- 两项都好 → 高分
- 任一为 0 → 淘汰（不可达节点或严重超预算节点）

### 无探测数据时的降级

当 probe 缓存不存在或已过期（>30min），`finalScore = recommendScore`，行为与现在完全一致。**零降级风险**。

---

## 6. subs-resolver 改动

`webapp/src/services/subs-resolver.ts` 中的 `effectiveWeight` 函数扩展：

```ts
// 新增：读取探测结果并融合
function effectiveWeight(c: TunnelEntry, probeCache: ProbeResultsCache | null): number {
  const base = c.recommendScore ?? (c.weight > 0 ? c.weight / 100 : 0);
  if (!probeCache || isProbeStale(probeCache)) {
    return base; // 降级：仅用 recommendScore
  }
  const domain = extractDomain(c.url);
  const probeResult = probeCache.results.find(r => r.domain === domain);
  if (!probeResult) {
    return base; // 未被探测到的节点：用 recommendScore（不惩罚，可能是新节点）
  }
  return probeResult.probeScore * base; // 乘法融合
}
```

**`resolveTunnel` 更新**：函数签名不变，内部 `pickWeighted` 调用前先异步读 probe cache（非阻塞，失败则降级）。

---

## 7. 数据流时序

```
App 启动
  ├── loadServerMode() [connection.store]
  ├── resolveTunnel(subsUrl) [subs-resolver] → 拿到 allCandidates
  └── ProbeScheduler.onCandidatesAvailable(allCandidates)
        └── 异步 probe（不阻塞连接）

用户点连接
  └── resolveTunnel(subsUrl)
        ├── 读 probeCache（同步，storage.get）
        └── pickWeighted(candidates, probeCache) → 选最佳节点
              └── _k2.run('up', config)

后台 5min 定时
  └── ProbeScheduler.runProbe()
        ├── _k2.run('probe', {urls: top10Candidates})
        └── 写 storage('k2.probe.results', results)
```

---

## 8. 错误处理

| 场景 | 行为 |
|------|------|
| probe action 超时（8s）| 部分节点结果缺失，有结果的节点正常评分，缺失的节点用 recommendScore |
| _k2.run('probe') 返回非 0 | 整体 probe 失败，本次跳过，不清空旧缓存 |
| storage 读/写失败 | 降级为 recommendScore，不影响连接 |
| 所有节点不可达（probeScore=0）| pickWeighted 退化到 allZero 分支（均匀随机），与当前行为一致 |
| 未探测到的候选节点 | 使用 recommendScore，不惩罚（可能是新节点） |

---

## 9. 安全考量

- probe 使用真实 `udid:token` 凭据（与正常连接相同），服务端会校验 auth
- probe 结果存储在加密存储（`_platform.storage` = AES-256-GCM）
- probe 不向任何第三方端点发送请求（仅访问已知 k2v5 服务器）
- 并发数限制（max 5）防止探测本身消耗过多带宽

---

## 10. 测试策略

### 单元测试

| 测试 | 文件 |
|------|------|
| `MultiEchoProbe` 正常 N 轮 | `k2/wire/echo_test.go` |
| `MultiEchoProbe` 部分超时 → lossRate | `k2/wire/echo_test.go` |
| `probeScore` 公式边界值（loss=1, rtt=0, rtt=∞）| `webapp/src/services/probe-scheduler.test.ts` |
| `effectiveWeight` 有/无 probeCache 两种路径 | `webapp/src/services/subs-resolver.test.ts` |
| probe 缓存过期（>30min）→ 降级 | `webapp/src/services/probe-scheduler.test.ts` |

### 集成测试

| 测试 | 验收条件 |
|------|---------|
| daemon `probe` action 返回真实节点结果 | `avgRttMs > 0`, `reachable: true` |
| probe 后 subs-resolver 选的节点 probeScore 最高 | 多次 resolveTunnel 的选择分布 |
| 探测失败不影响正常连接 | probe 返回错误时 connect 仍走 recommendScore |

---

## 11. 变更范围汇总

| 文件/模块 | 类型 | 说明 |
|-----------|------|------|
| `k2/wire/echo.go` | 新增函数 | `MultiEchoProbe`, `ProbeStats` |
| `k2/daemon/daemon.go` | 新增 case | `probe` action handler |
| `desktop/src-tauri/src/` | 新增分支 | `probe` → daemon HTTP 转发 |
| `mobile/plugins/K2Plugin.swift` | 无改动* | run() 已路由所有 action |
| `mobile/plugins/K2Plugin.kt` | 无改动* | 同上 |
| `webapp/src/services/probe-scheduler.ts` | 新增文件 | 后台调度 + 缓存管理 |
| `webapp/src/services/subs-resolver.ts` | 改动 | `effectiveWeight` 融合 probeScore |
| `webapp/src/stores/connection.store.ts` | 改动 | 订阅刷新后触发 ProbeScheduler |
| `webapp/src/types/kaitu-core.ts` | 无改动 | run() 泛型已覆盖 |

*K2Plugin 的 Go 侧（appext 内部）需要新增 probe 分支，但 Swift/Kotlin 桥接层无需改动。

---

## 12. 不在此范围内

- Center 聚合用户探测数据、更新 recommendScore（此为未来 Phase 2）
- 连接后的实时带宽监测 + 热切换
- **Desktop 节点选择优化**：Desktop 的 k2subs 由 daemon 服务端解析（`Subscription.Pick`），`subs-resolver.ts` 不参与，因此本方案的 probeScore 融合仅对 **mobile** 生效。Desktop 可以通过 `probe` action 获取测量数据，但将结果反馈进 daemon `Subscription.Pick` 是独立的工作，需改动 k2 submodule，不在此范围。
