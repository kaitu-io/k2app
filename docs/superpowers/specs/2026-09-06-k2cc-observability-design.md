# K2CC 可观测性 Phase 1 —— 按连接的拥塞控制汇总行 + 观测端去副作用

日期：2026-09-06 · 状态：评审修订版（v2，含实施推演）· 范围：`k2/wire/k2cc/`、`k2/wire/quic.go`、`k2/wire/quic_p2p.go`、`k2/wire/wire.go`、`k2/engine/health.go`

## 1. 背景

k2cc（`wire/k2cc/k2cc.go`）是 PCC Vivace 变体：效用函数 `U = T − α·L·R − β·ΔRTT` 的梯度上升，外加 BBR 风格带宽锚定循环和 AIMD-ε 自适应步长。约三十个常量写死，所有连接、两个方向、所有地区共用一组参数（GFW 模式 α=4、β=1）。

用户目标是「把关键指标做成线性维度，埋日志点，后续用数据微调，让 k2cc 适应各种环境」。分三阶段：**Phase 1 可观测性（本文）→ Phase 2 参数向量 θ + 服务端按连接 A/B → Phase 3 用真实分布校准基准矩阵并调参**。数据落点已定：先节点本地日志，跑对字段后再汇总到 Center。

### 1.1 已核实的现状（2026-09-06）

| 事实 | 证据 |
|---|---|
| 客户端发布构建 `K2_BUILD_LOG_LEVEL=info`，k2cc 全部 `slog.Debug` 行不可见 | `release-desktop.yml` / `build-mobile.yml` |
| 客户端唯一带 CC 字段的日志是 30 s 一次的 `DIAG: heartbeat`，只有平滑后的**上行** `loss` / `rttMs` / `minRttMs` | `engine/health.go:353` |
| 车队 `.env` 为 `K2_LOG_LEVEL=info`，服务端 k2s.log 里 **0** 条 `k2cc:` 行 | jp-tokyo.aws.wm01：18 MB、1885 条 `metadata auth OK`、0 条 CC；hk.aws.wm01 同 |
| 服务端（下载方向）sender 创建后引用被丢弃，没有任何 `Snapshot()` 句柄 | `wire/quic.go:1306` |
| k2cc 日志行不带 `remote` / `udid`，即使开 debug 也无法归属到连接 | `k2cc.go` 各 `slog.Debug` |
| **观测端扰动控制器**：`Snapshot()`（健康采样器 1 Hz 调用）→ `mi.Throughput()` 会把活 MI 的 `IsAppLimited` 粘性置真 | `k2cc.go:277`、`:1388` |
| `OnCongestionEventEx` 在锁外写 ackRate 跟踪器，`Snapshot()` / `clampedAckRate()` 在锁内读 → 数据竞争 | `k2cc.go:1276` |
| 客户端每次重连都新建 sender（`c.ccSender = sender`），断开时置 nil | `wire/quic.go:653`、`:1044` |
| kuic 的 `*quic.Conn` 提供 `Context()`，连接关闭即取消 | `third_party/kuic/connection.go:817` |

结论：**生产环境两侧都没有一条可用于调参的 per-MI 数据**。先让数据存在，再谈调参。

## 2. 目标与非目标

**目标**
1. 两侧在 INFO 级别各产出一种**按连接、单行自洽可读**的 CC 汇总日志行，不依赖 debug 级别。
2. 汇总覆盖 Phase 2/3 需要的维度：MI 计数与路径占比（app-limited / 带宽锚定 / 地板）、丢包与 RTT 的分位数、速率与吞吐分位数、当前 bwEst / bgLoss / ε / mode、事件计数。
3. 观测路径对控制器**零副作用**、零数据竞争，可以放心提高采样频率。
4. 零新基建：不加接口、不加存储、不加配置开关。

**非目标（后续阶段）**
- 参数向量 θ 与运行时覆盖、按连接 A/B 变体（Phase 2）。
- Center 侧接收、存储、聚合（Phase 2 末或 Phase 3）。
- 任何算法行为改动（热启动、app-limited 不清零、MI 时长缩放、stale-ACK 过滤……）—— 要等数据。
- `HealthSnapshot` / SSE / UI 展示新增字段。

## 3. 设计总览

```
k2cc.K2CCSender
  ├─ processCompletedMI()  ──▶ ccStats 累加（已在 p.mu 下，每 MI 一次，非每包）
  ├─ Summary() CCSummary   ──▶ 累计值的副本（RLock，只读）
  └─ Snapshot() CCSnapshot ──▶ 纯读，不再触碰 MI 状态

wire (server)  QUICServer.acceptLoop / quic_p2p.go
  └─ sender 传入 handleConn → connContext.cc
       └─ ccSummaryLoop goroutine（每连接一个，独占 prev/seq/lastEmit）
            ├─ ticker 60 s：cur=Summary()，win=cur.Sub(prev)，win.MIs>0 才发 INFO "DIAG: cc-summary"
            └─ state.done 或 conn.Context().Done() 触发：发 final=true 后退出

wire (client)  QUICClient.WireStats()
  └─ WireStats.CC = sender.Summary()（累计值；无 sender 时零值）
       └─ engine/health.go 心跳：ws.CC.Sub(hm.lastCC) 取 30 s 窗口，并入 "DIAG: heartbeat"
```

**一个原语解决两侧**：累加器只存累计值；`CCSummary.Sub(prev)` 得到窗口差分。直方图是桶计数，差分就是逐桶相减，分位数在差分结果上算。服务端和客户端的发射器各自持有 `prev`，累加器本身没有 reset 语义，因此 `WireStats()` 被健康采样器 1 Hz 调用也不会吃掉心跳的窗口。

**纪元（epoch）**：每个 sender 在构造时从包级原子计数器领取 `SenderID`，写进 `CCSummary`。`Sub(prev)` 发现 `prev.SenderID != s.SenderID` 时把 prev 视为零值。这是推演中发现的必要项：客户端重连会换 sender，累计值从零重来，没有纪元判断差分会 uint64 下溢。计数相减一律饱和减法作为第二道保险。

## 4. 数据模型：`k2cc.CCSummary`

全部为自 sender 创建起的累计值，结构体只含定长数组与标量（无 slice、无指针），值拷贝即安全。

### 4.1 身份

| 字段 | 含义 |
|---|---|
| `SenderID uint64` | 纪元；0 表示「没有 sender」（TCP-WS 兜底期、连接尚未建立） |

### 4.2 计数（uint64）

| 字段 | 累加位置 | 含义 |
|---|---|---|
| `MIs` | `processCompletedMI` 入口 | 完成的 MI 总数（含 app-limited） |
| `AppLimitedMIs` | app-limited 分支 | 走 app-limited 锚定的 MI |
| `SlowStartMIs` / `DecisionMIs` / `RateAdjMIs` | 入口按处理前的 `p.state.Mode` | 模式驻留（以 MI 计），三者之和恒等于 `MIs` |
| `BWAnchoredMIs` | `processBWAnchored` 入口 | 由带宽锚定处理的 MI |
| `FloorMIs` | `processCompletedMI` 的 `defer` 收尾 | 处理完成后 `CurrentRate ≤ bwAnchoredMinRate·1.001`，或非锚定模式下 `≤ arcMinRate·1.001` |
| `BWDrops` / `BWRises` | 现有两处 `slog.Debug` 旁 | 带宽锚定的降/升事件 |
| `SlowStartSaturations` | 慢启动退出条件 3 | 吞吐饱和退出 |
| `RateProbes` | 周期探测触发处 | 每 20 MI 的 +10% 探测 |
| `BytesSent` / `BytesAcked` / `BytesLost` | 入口，来自 MI | 聚合丢包率 = `BytesLost / BytesSent` |

`FloorMIs` 用 `defer` 是因为 app-limited 分支提前 `return`，收尾必须覆盖所有出口。

### 4.3 直方图（固定桶，值落入首个 `v ≤ edge` 的桶）

| 直方图 | 样本 | 桶上界 | 跳过条件 |
|---|---|---|---|
| `LossHist [10]uint64` | 每 MI `LossRate()`（%） | `0, 0.5, 1, 2, 5, 10, 20, 35, 50, 100` | 无 |
| `RTTHist [12]uint64` | 每 MI `AvgRTT()`（ms） | `10, 20, 35, 50, 75, 100, 150, 200, 300, 500, 1000, +∞` | `AvgRTT()==0`（60 s 强制完成的空 MI） |
| `RateHist [12]uint64` | 每 MI `TargetRate`（Mbps） | `1, 2, 5, 10, 20, 30, 50, 100, 200, 300, 500, +∞` | 无 |
| `TputHist [12]uint64` | 每 MI `Throughput()`（Mbps） | 同上 | 无（0 落第一桶，是真实信息） |

`Percentile(p) (edge float64, overflow bool)` 返回**包含第 ⌈p·n⌉ 个样本的桶的上界**（保守估计，`lossP90=10` 读作「p90 落在 (5, 10] 里」）；`overflow=true` 表示落在 `+∞` 桶，格式化为 `>500` / `>1000`。空直方图返回 `(0,false)`。另外精确记录 `LossMax`、`TputMax`（float64，取 max 不进桶；`Sub` 对它们取 receiver 值）。

### 4.4 当前值（`Sub` 不相减，取 receiver）

`BWEst`（bgLoss 校正后）、`BGLoss`、`Epsilon`、`MinRTT`、`Mode`。

内存：约 46 个 uint64 + 几个标量 ≈ 450 B / 连接；iOS NE 的 50 MB 预算无感。

## 5. 日志行

### 5.1 服务端：`DIAG: cc-summary`（INFO）

**发射器独占状态**。`ccSummaryLoop(log, ctx, cc, conn, done)` 每连接一个 goroutine，自己持有 `prev`、`seq`、`lastEmit`、`start`；`handleConn` 不再另发 final，只负责像现在一样 `close(state.done)`。理由：final 与周期行共用 `prev`，分在两个 goroutine 里就得加锁或者接受重复计数。goroutine 同时监听 `state.done` 与 `conn.Context().Done()`，任一触发即发 final 并退出——后者保证 `handleConn` 任何异常路径都不会把 ticker 留成孤儿。

```go
var ccSummaryInterval = 60 * time.Second // 未导出，测试缩短

for {
    select {
    case <-ticker.C:            emit(false)
    case <-done:                emit(true); return
    case <-conn.Context().Done(): emit(true); return
    }
}
```

**跳过规则**（推演修订：只看 MI，不看字节）：周期行在窗口 `win.MIs == 0` 时不发，`prev`/`lastEmit` 不推进，下一次的 `windowS` 自然覆盖空闲期；final 行在生命周期 `cur.MIs == 0` 时不发。理由：一个完成的 MI 至少要 10 个 ACK 或 30 s，认证即断、被 enforce 拒绝、扫描器之类的连接完成不了一个 MI，字节数再小也没有 CC 信息可报，按字节判会在扫描风暴时刷屏。

**格式化与发射分离**：`ccSummaryAttrs(win, cur CCSummary, seq int, final bool, windowS, uptimeS int, udid string) []any` 是纯函数，单测直接断言；`emit` 只做 `log.Info("DIAG: cc-summary", attrs...)`。

字段（key=value，与现有 DIAG 风格一致；Mbps 与 % 均 `%.1f`，分位数为桶上界整数或 `>N`）：

```
DIAG: cc-summary remote=1.2.3.4:5678 udid=<32hex|""> seq=3 final=false windowS=60 uptimeS=181
  mis=42 appLimited=31 bwAnchored=8 floor=0 slowStart=2 bwDrop=0 bwRise=1 ssSat=1 probes=0
  sentMB=38.2 ackedMB=36.9 lossAgg=3.4 lossP50=2 lossP90=10 lossMax=14.2
  rttP50=50 rttP90=75 minRttMs=41
  rateP50=30 tputP50=20 tputMax=48.7
  bwEst=47.1 bgLoss=1.2 epsilon=0.090 mode=decision
```

`remote` 来自 `connLog`（已 `slog.With`）；`udid` 每次发射时 `ctx.dev.get()`，metadata 流落地前为空串，后续行会带上。服务端 INFO 日志本就含 `udid+remote`（`metadata auth OK`），无新增隐私面。

**日志量预算**：jp-tokyo.aws.wm01 六小时 1885 次认证 ≈ 5 连接/分钟；假设并发活跃 100 条且全部非空闲，最坏 100 行/分钟 × ~400 B ≈ 2.4 MB/h，与现有 ~3 MB/h 同量级；「窗口内完成过 MI」的门会砍掉大半。lumberjack 20 MB 轮转不变。

### 5.2 客户端：并入 `DIAG: heartbeat`

`WireStats` 新增 `CC k2cc.CCSummary`（`QUICClient.WireStats()` 填 `sender.Summary()`；TCP-WS 与无 sender 时为零值，`SenderID=0`）。`wire` 已 import `k2cc`，`k2cc` 只依赖 kuic，无环。

`healthMonitor` 新增 `lastCC k2cc.CCSummary`，仅在心跳发射时更新：`win := ws.CC.Sub(hm.lastCC)`，追加字段：

```
ccMis=12 ccAppLimited=11 ccFloor=0 ccLossP90=0 ccRttP90=75 ccBwEst=3.1 ccBgLoss=0.0 ccMode=decision
```

**纪元行为（推演）**：重连 → 新 `SenderID` → 第一次心跳的窗口就是新连接的全部累计，正确；切到 TCP-WS → `SenderID=0` → 窗口全零，`fallback=true` 已说明原因；切回 QUIC → 再新纪元。`Reset()` 同理。

**方向提示写进字段名旁的注释与 CLAUDE.md**：客户端这组是**上行**（VPN 用户几乎不上传，`ccAppLimited ≈ ccMis` 是常态，不是故障）；下载方向的同名指标只在服务端 `cc-summary` 行。这条误读在工单排查里犯过，必须钉死。

## 6. 观测端去副作用

1. `MonitorInterval.Throughput()` 改为纯函数。app-limited 判定拆成 `MonitorInterval.appLimited() bool`，只在 `processCompletedMI` 入口调用一次并写入 `mi.IsAppLimited`。传入的是 `miCopy`（`OnPacketAcked` 在 `Complete()` 之前拷贝，`EndTime` 为零 → `Duration()` 用 `time.Now()`，与现在等价）。`Snapshot()` 读吞吐不再改状态。
2. `OnCongestionEventEx` 的 `p.ackRate.record(...)` 移入 `p.mu` 临界区（每 ACK 帧一次、O(1)，成本可忽略）。
3. `Snapshot()` 与 `Summary()` 在 RLock 下**只读**，用两个变异测试钉住，**先在未修复代码上跑红再修**：
   - 纯度：`sender.state.CurrentMI` 设为 `TargetRate=150M`、`StartTime=now−1s`、`BytesSent=150 KB`（≥ `minSnapshotBytes`，且远低于期望的 50%）；连续调用 `Snapshot()`；断言 `IsAppLimited` 仍为 false。不需要 sleep。
   - 竞争：`-race` 下一个 goroutine 循环 `Snapshot()`+`Summary()` 50 ms，主 goroutine 循环 `OnPacketSent`/`OnPacketAcked`/`OnCongestionEventEx`；旧代码必报 ackRate 竞争。

## 7. 服务端接线细节

- `acceptLoop`：`sender := k2cc.NewCCSenderGFWMode(DefaultMaxRate)`；`conn.SetCongestionControl(sender)`；`go s.handleConn(conn, sender)`。
- `quic_p2p.go:50`：同样把它在 `:20` 自建的 sender 传入。不做 P2P 特判——「P2P 非目标」指不为它设计额外行为，不是把它排除在汇总之外。
- `handleConn(conn, cc)`：`connContext.cc = cc`；`state` 构造后 `go s.ccSummaryLoop(connLog, ctx, cc, conn, state.done)`。`cc == nil` 时 loop 立即返回。
- `handleConn` 末尾不变。

## 8. 测试计划

| 层 | 测试 | 驱动方式 / 判别式 |
|---|---|---|
| k2cc 单元 | `hist` 分桶与 `Percentile` | 手算样本集；空直方图 `(0,false)`；全在 `+∞` 桶 `overflow=true`；n=1 |
| k2cc 单元 | `CCSummary.Sub` | 同纪元逐项相减；异纪元 prev 视为零；饱和减法（构造 prev > cur）；当前值取 receiver |
| k2cc 单元 | 累加器各路径 | 复用现有 `TestAppLimited_*` 的手工 MI 构造直接调 `processCompletedMI`：app-limited → `AppLimitedMIs+1`；慢启动饱和 → `SlowStartSaturations+1`；`bwEst` 有效时 Cruise 掉速 → `BWDrops+1`；`CurrentRate` 压到 5M → `FloorMIs+1`；`SlowStart+Decision+RateAdj == MIs` |
| k2cc 单元 | `Snapshot()` 纯度、`-race` 竞争 | §6 的两个变异测试 |
| k2cc 集成 | 现有 `runBulkTransfer`（丢包 10%、限速）后读 `env.serverCC.Summary()` | `MIs > 0`、`LossHist` 非空、`lossAgg` 与注入丢包同量级、`BytesSent ≥ BytesAcked − 一个 BDP`（见 §12.3 的 stale-ACK 说明） |
| wire 服务端 | `ccSummaryAttrs` 纯函数 | 字段齐全、`>500` 渲染、零窗口 |
| wire 服务端 | `ccSummaryLoop` 缩短 `ccSummaryInterval` 到 50 ms，`recordCollector` 抓 slog，走现有 QUIC client/server 测试对 | 周期行 ≥1、final 行恰 1；`seq` 递增；认证后行内 `udid` 非空；只认证不传数据的连接 **0** 行 |
| engine | `statsEchoStub.stats.CC` 注入 + 直接调 `hm.sample()` 30 次触发心跳，`slogHandlerFunc` 抓取 | 第一轮 `ccMis=5`；把 `CC.MIs` 改 8 再 30 次 → `ccMis=3`；换 `SenderID` → `ccMis=8` |
| 全量 | `go test ./wire/... ./engine/...`、`lint/concurrency_lint_test.go` | 全绿；新增 `RLock` 点加 `// concurrency:allow — 纯拷贝` 通过 AST 门 |

机器负载常年 30–70，k2cc 单测被 timeout 杀不是回归信号，按 CLAUDE.md 约定改用单文件 + 降载复跑判断。

## 9. 发布、验证与回滚

**服务端（不需要发客户端）**：k2 分支合并 → k2app 子模块指针 bump → k2s 镜像 → 先一台 canary（jp-tokyo.aws.wm01，流量大、日志有基线）滚 `K2_VERSION`。一小时后验证：

| 检查 | 判据 |
|---|---|
| 行存在 | `grep -c 'DIAG: cc-summary'` 持续增长 |
| 不变量 | 抽 20 行：`appLimited ≤ mis`；`slowStart+decision+rateAdj`（由 mode 驻留推出）`= mis`；`lossAgg ≤ lossMax`；`seq` 单连接递增；final 行每 remote 至多 1 条 |
| 正对照 | 一条 `serverUDPConn: closed` / `HandleTCP` 字节大的连接，其 `cc-summary` 的 `ackedMB` 同量级 |
| 反对照 | 一条只有 `metadata auth OK` 随即 `AcceptStream done` 的连接，**没有** cc-summary 行 |
| 体积 | 一小时日志增量 < 3 MB |
| 资源 | `docker stats k2s` 内存一小时内无单调增长（goroutine 泄漏的代理指标） |

再按 `kaitu-node-ops` §5 滚车队。回滚 = 回退镜像 tag。

**客户端**：随下一个版本发布；iOS NE 内存增量为固定大小结构，无需单独评估。

## 10. 文档更新

- `wire/k2cc/CLAUDE.md`：Observability 段改写（`Summary()`、`CCSummary`、纪元、直方图语义、纯度约束）。
- `wire/CLAUDE.md`：`quic.go` 条目补 `cc-summary` 发射器与跳过规则；`WireStats.CC` 字段与方向提示。
- `engine/CLAUDE.md`：心跳新字段与「上行」提示。
- k2app 记忆：支持工单排查流程改为 grep `DIAG: cc-summary`。

## 11. 后续阶段接口约定

`cc-summary` 行是 Phase 2/3 的数据契约：Phase 2 只**追加** `variant=<name>` 字段，不改既有 key；Center 聚合（Phase 2 末）按同一 key 集解析。`CCSummary` 结构体导出、字段稳定，Phase 2 的 θ 覆盖只增加 `K2CCConfig` 字段，不动累加器。

## 12. 实施推演：发现的问题与修正记录

逐段模拟「真的把代码写出来、跑起来」得到的修正，已全部并入上文，这里留痕便于评审对照 v1。

1. **客户端纪元缺失（会出错）**。v1 的 `Sub(prev)` 没有考虑重连换 sender，累计值归零后差分下溢。修正：`SenderID` 纪元 + 饱和减法（§3、§4.1）。
2. **final 行的归属（会出错）**。v1 让 `handleConn` 在关闭处发 final，而 `prev` 在发射器 goroutine 里，两边共享状态。修正：发射器独占状态，`done` / `conn.Context().Done()` 触发 final（§5.1）。
3. **stale-ACK 归因是已有的边界伪影**。`OnPacketAcked` 不按包号过滤，上一 MI 在途包的 ACK 记进新 MI 的 `BytesAcked`，短窗口里 `ackedMB` 可能略大于 `sentMB`，`tputMax` 偶尔偏高。不在本阶段修；`cc-summary` 会把它的量级暴露出来（§13 模式 F），集成测试的断言按此放宽一个 BDP。
4. **跳过规则改为只看 MI**。v1 的「MI 或字节」门会让被拒绝的连接、扫描器都留下 final 行。修正见 §5.1。
5. **`FloorMIs` 的收尾点**。app-limited 分支提前 return，收尾改用 `defer`（§4.2）。
6. **空 MI 的 RTT 样本**。60 s 强制完成的 MI 可能没有 RTT 样本，`AvgRTT()==0` 时跳过 RTT 直方图，其余直方图照常（§4.3）。
7. **测试可行性核对**：`recordCollector`（wire）、`slogHandlerFunc`（engine）、`statsEchoStub`、直接调 `processCompletedMI` 的手工 MI 模式、`env.serverCC` 都已存在，无需新造测试基础设施。

## 13. 预期数据形态与决策表

Phase 1 上线一周后，按节点/地区聚合 `cc-summary`，预期会看到以下几类模式。每一类对应一个 Phase 2/3 的动作，这就是「埋点是为了什么」的具体答案。

| # | 观测模式 | 解读 | 触发的后续动作 |
|---|---|---|---|
| A | 多数连接 `appLimited/mis > 0.8` 且 `bwEst = 0` | 带宽估计器只吃非 app-limited 的 MI，浏览型流量永远喂不饱它 → `bwEst.valid()` 永远为假 → app-limited 锚定的地板一直是 30 Mbps 冷启动值（`b4c026e` 的 BW-aware 地板从未生效）。对 5 Mbps 级被限速的中国用户，**每次页面加载都在 30M 起步、自伤丢包** | Phase 3 首选：让 app-limited MI 以 `T/(1−L)` 提供 bwEst 下界，或按 (设备, 节点) 持久化带宽先验做热启动。预期效果：限速用户 `lossAgg` 明显下降 |
| B | `floor > 0` 频繁且 `lossAgg > 20%` | PCC 梯度在 `L > 1/(1+α) = 20%` 后恒负，速率滑到地板 | Phase 2 在这些节点 A/B α ∈ {4, 6, 8} |
| C | `rttP90 / minRttMs > 2` 且 `bwAnchored` 占比高 | 巡航期仍有排队膨胀，β=1 对 RTT 惩罚不够 | Phase 2 A/B β |
| D | `bwDrop` 频繁而 `lossP50` 很低 | `bwDropThreshold=0.5` 把应用侧的正常波动当成带宽下降，估计器反复重置 | Phase 2 A/B `bwDropThreshold`，或要求连续两个 MI 才判降 |
| E | 远节点（US/EU）批量流的每分钟 `mis` 很少 | MI 最短 2×RTT，300 ms 链路上一轮带宽锚定循环要 5 s，收敛太慢 | Phase 3：MI 最短时长按 RTT 分段缩放 |
| F | 短窗口 `ackedMB > sentMB`、`tputMax` 偏离 `tputP50` 数倍 | stale-ACK 归因伪影的量级 | 决定是否给 ACK 加包号过滤（与丢包过滤对称） |
| G | 按地区的 `rttP50` / `lossP50` / `rateP50` 分布 | 真实环境矩阵 | Phase 3：用它替换 `TestConvergence_Benchmark` 手拍的 30/150/300 ms × 0–35% 矩阵 |

**Phase 1 自身的成功判据**：§9 的 canary 检查全过，且一周内至少能对模式 A 与 G 给出有数字的回答（A 是「app-limited 占比多少、bwEst=0 的连接占多少」，G 是各地区三个分位数）。拿不到这两个答案，说明字段设计有缺口，先补字段再进 Phase 2。
