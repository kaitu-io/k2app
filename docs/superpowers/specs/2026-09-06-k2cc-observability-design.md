# K2CC 可观测性 Phase 1 —— 按连接的拥塞控制汇总行 + 观测端去副作用

日期：2026-09-06 · 状态：待用户评审 · 范围：`k2/wire/k2cc/`、`k2/wire/quic.go`、`k2/wire/wire.go`、`k2/engine/health.go`

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

结论：**生产环境两侧都没有一条可用于调参的 per-MI 数据**。先让数据存在，再谈调参。

## 2. 目标与非目标

**目标**
1. 两侧在 INFO 级别各产出一种**按连接、自洽可读**的 CC 汇总日志行，不依赖 debug 级别。
2. 汇总覆盖 Phase 2/3 需要的维度：MI 计数与路径占比（app-limited / 带宽锚定 / 地板）、丢包与 RTT 的分位数、速率与吞吐分位数、当前 bwEst / bgLoss / ε / mode、事件计数。
3. 观测路径对控制器**零副作用**、零数据竞争，可以放心提高采样频率。
4. 零新基建：不加接口、不加存储、不加配置开关。

**非目标（后续阶段）**
- 参数向量 θ 与运行时覆盖、按连接 A/B 变体（Phase 2）。
- Center 侧接收、存储、聚合（Phase 2 末或 Phase 3）。
- 任何算法行为改动（热启动、app-limited 不清零、MI 时长缩放……）—— 要等数据。
- `HealthSnapshot` / SSE / UI 展示新增字段。
- k2p（P2P）连接的服务端汇总。

## 3. 设计总览

```
k2cc.K2CCSender
  ├─ processCompletedMI()  ──▶ ccStats 累加（已在 p.mu 下，每 MI 一次，非每包）
  ├─ Summary() CCSummary   ──▶ 累计值的副本（RLock）
  └─ Snapshot() CCSnapshot ──▶ 纯读，不再触碰 MI 状态

wire (server)  QUICServer.acceptLoop
  └─ sender 传入 handleConn → connContext.cc
       ├─ ccSummaryLoop：每 60 s 取 Summary()，与上次差分，非空则 INFO "DIAG: cc-summary"
       └─ 连接关闭：最后一次差分，final=true

wire (client)  QUICClient.WireStats()
  └─ WireStats.CC = sender.Summary()（累计值）
       └─ engine/health.go 心跳：与上次心跳差分，把窗口值并入 "DIAG: heartbeat"
```

**一个原语解决两侧**：累加器只存累计值；`CCSummary.Sub(prev)` 得到窗口差分。直方图是桶计数，差分就是逐桶相减，分位数在差分结果上算。服务端和客户端的发射器各自持有 `prev`，累加器本身没有 reset 语义，因此 `WireStats()` 被健康采样器 1 Hz 调用也不会吃掉心跳的窗口。

## 4. 数据模型：`k2cc.CCSummary`

全部为自连接建立起的累计值。`Sub(prev CCSummary) CCSummary` 返回窗口差分（计数与直方图相减；`BWEst` / `BGLoss` / `Epsilon` / `Mode` / `MinRTT` 这类「当前值」取 receiver 的值不相减）。

### 4.1 计数（uint64）

| 字段 | 累加位置 | 含义 |
|---|---|---|
| `MIs` | `processCompletedMI` 入口 | 完成的 MI 总数（含 app-limited） |
| `AppLimitedMIs` | app-limited 分支 | 走 app-limited 锚定的 MI |
| `SlowStartMIs` / `DecisionMIs` / `RateAdjMIs` | 入口按 `p.state.Mode`（处理前） | 模式驻留（以 MI 计） |
| `BWAnchoredMIs` | `processBWAnchored` 入口 | 由带宽锚定处理的 MI |
| `FloorMIs` | 每次 rate 更新后 | 更新后 `CurrentRate ≤ arcMinRate` 或（锚定模式）`≤ bwAnchoredMinRate` |
| `BWDrops` / `BWRises` | 现有两处 `slog.Debug` 旁 | 带宽锚定的降/升事件 |
| `SlowStartSaturations` | 慢启动退出条件 3 | 吞吐饱和退出 |
| `RateProbes` | 周期探测触发处 | 每 20 MI 的 +10% 探测 |
| `BytesSent` / `BytesAcked` / `BytesLost` | 入口，来自 MI | 聚合丢包率 = `BytesLost / BytesSent` |

### 4.2 直方图（固定桶，值落入首个 `v ≤ edge` 的桶）

| 直方图 | 样本 | 桶上界 |
|---|---|---|
| `LossHist` | 每 MI `LossRate()`（%） | `0, 0.5, 1, 2, 5, 10, 20, 35, 50, 100` |
| `RTTHist` | 每 MI `AvgRTT()`（ms） | `10, 20, 35, 50, 75, 100, 150, 200, 300, 500, 1000, +∞` |
| `RateHist` | 每 MI `TargetRate`（Mbps） | `1, 2, 5, 10, 20, 30, 50, 100, 200, 300, 500, +∞` |
| `TputHist` | 每 MI `Throughput()`（Mbps） | 同上 |

分位数 `Percentile(p)` 返回**包含第 ⌈p·n⌉ 个样本的桶的上界**（保守估计，`lossP90=10` 读作「p90 落在 (5, 10] 里」）。`+∞` 桶打印为 `>500`。另外精确记录 `LossMax`、`TputMax`（float64，取 max 不进桶）。

### 4.3 当前值（不差分）

`BWEst`、`BGLoss`、`Epsilon`、`MinRTT`、`Mode`（取自 sender 当前状态）。

内存：全部字段约 60 个 uint64 / float64，每连接不到 1 KB；iOS NE 的 50 MB 预算无感。

## 5. 日志行

### 5.1 服务端：`DIAG: cc-summary`（INFO）

发射器 `ccSummaryLoop(ctx, cc, done)` 每连接一个 goroutine，`time.Ticker` 60 s（未导出变量 `ccSummaryInterval`，测试可缩短），`done` 关闭即退出；连接关闭后由 `handleConn` 在 `handleDatagrams` 返回处补一条 `final=true`。

**跳过规则**：周期行在窗口内 `MIs == 0 && BytesSent == 0` 时不发；final 行在整个连接生命周期 `MIs == 0 && BytesSent == 0` 时不发（探测类、认证即断的连接不产生噪音）。

字段（key=value，与现有 DIAG 风格一致；Mbps 与 % 均 `%.1f`）：

```
DIAG: cc-summary remote=1.2.3.4:5678 udid=<32hex|""> seq=3 final=false windowS=60 uptimeS=181
  mis=42 appLimited=31 bwAnchored=8 floor=0 slowStart=2 bwDrop=0 bwRise=1 ssSat=1 probes=0
  sentMB=38.2 ackedMB=36.9 lossAgg=3.4 lossP50=2 lossP90=10 lossMax=14.2
  rttP50=50 rttP90=75 minRttMs=41
  rateP50=30 tputP50=20 tputMax=48.7
  bwEst=47.1 bgLoss=1.2 epsilon=0.090 mode=decision
```

`udid` 来自 `ctx.dev.get()`，metadata 流落地前可能为空，后续行会带上；服务端 INFO 日志本就含 `udid+remote`（`metadata auth OK`），无新增隐私面。

**日志量预算**：jp-tokyo.aws.wm01 六小时 1885 次认证 ≈ 5 连接/分钟；假设并发活跃 100 条且全部非空闲，最坏 100 行/分钟 × ~400 B ≈ 2.4 MB/h，与现有 ~3 MB/h 同量级；跳过规则会砍掉大半。lumberjack 20 MB 轮转不变。

### 5.2 客户端：并入 `DIAG: heartbeat`

`WireStats` 新增 `CC k2cc.CCSummary`（QUIC-only，TCP-WS 为零值）。`healthMonitor` 持有 `lastCC`，心跳时 `ws.CC.Sub(hm.lastCC)` 取 30 s 窗口，追加字段：

```
ccMis=12 ccAppLimited=11 ccFloor=0 ccLossP90=0 ccRttP90=75 ccBwEst=3.1 ccBgLoss=0.0 ccMode=decision
```

**方向提示写进字段名旁的注释与 CLAUDE.md**：客户端这组是**上行**（VPN 用户几乎不上传，`ccAppLimited ≈ ccMis` 是常态，不是故障）；下载方向的同名指标只在服务端 `cc-summary` 行。这条误读在工单排查里犯过，必须钉死。

## 6. 观测端去副作用

1. `MonitorInterval.Throughput()` 改为纯函数。app-limited 判定拆成 `MonitorInterval.appLimited() bool`，只在 `processCompletedMI` 入口调用一次并写入 `mi.IsAppLimited`。`Snapshot()` 读吞吐不再改状态。
2. `OnCongestionEventEx` 的 `p.ackRate.record(...)` 移入 `p.mu` 临界区（每 ACK 帧一次、O(1)，成本可忽略）。
3. `Snapshot()` 与 `Summary()` 在 RLock 下**只读**，用测试钉住：`-race` 下并发 `Snapshot()`/`Summary()` × ACK/loss 回调；以及「活 MI 供包不足时连续调用 `Snapshot()`，`IsAppLimited` 必须保持 false」的变异测试（先在未修复代码上确认它会红）。

## 7. 服务端接线细节

- `acceptLoop`：`sender := k2cc.NewCCSenderGFWMode(DefaultMaxRate)`；`conn.SetCongestionControl(sender)`；`go s.handleConn(conn, sender)`。
- `handleConn(conn, cc)`：`connContext.cc = cc`；`state.done` 建立后 `go s.ccSummaryLoop(connLog, ctx, cc, state.done)`；`handleDatagrams` 返回、`close(state.done)` 之后调用 `s.emitCCSummary(connLog, ctx, cc, final=true)`。
- 发射器 nil-safe：`cc == nil`（部分测试）直接返回。
- `handleConn` 有两个调用点：`acceptLoop`（`quic.go:1313`）与 `quic_p2p.go:50`。后者在 `quic_p2p.go:20` 已经自建 sender，同样传入即可，不做任何 P2P 特判——「P2P 非目标」指不为它设计额外行为，不是把它排除在汇总之外。

## 8. 测试计划

| 层 | 测试 | 判别式 |
|---|---|---|
| k2cc 单元 | `hist` 分桶与 `Percentile` | 手算样本集，含空直方图、全在 +∞ 桶、n=1 |
| k2cc 单元 | `CCSummary.Sub` | 计数与桶逐项相减；当前值取 receiver |
| k2cc 单元 | 累加器各路径 | 驱动 app-limited / 慢启动饱和 / 带宽锚定降升 / 地板，断言对应计数恰好 +1；复用现有测试的构造方式 |
| k2cc 单元 | `Snapshot()` 纯度（变异测试） | 未修复代码上该测试必红 |
| k2cc 单元 | `-race`：并发 `Snapshot()`/`Summary()` × `OnPacketAcked`/`OnCongestionEventEx` | 无 race 报告 |
| k2cc 集成 | 现有 `runBulkTransfer`（丢包 10%、限速）后读服务端 sender 的 `Summary()` | `MIs > 0`、`LossHist` 非空、`lossAgg` 与中继注入丢包同量级 |
| wire 服务端 | `ccSummaryLoop` 缩短间隔 + 假 sender 或真传输 | 周期行、final 行各出现；空闲窗口不出行；`udid` 落地后出现在行内 |
| engine | 心跳测试 | 新字段存在；窗口差分正确（两次心跳间只算增量） |
| 全量 | `go test ./wire/... ./engine/...`、`lint/concurrency_lint_test.go` | 全绿；新增 `Lock()` 点通过 AST 门 |

机器负载常年 30–70，k2cc 单测被 timeout 杀不是回归信号，按 CLAUDE.md 约定改用单文件 + 降载复跑判断。

## 9. 发布、验证与回滚

**服务端（不需要发客户端）**：k2 分支合并 → k2app 子模块指针 bump → k2s 镜像 → 先一台 canary（建议 jp-tokyo.aws.wm01，流量大、日志有基线）滚 `K2_VERSION`，验证：`grep -c 'DIAG: cc-summary' k2s.log` 增长、一小时日志增量 < 3 MB、抽 3 条线看字段自洽（`appLimited ≤ mis`、`lossAgg` 与 `lossP50/P90` 同量级）。再按 `kaitu-node-ops` §5 滚车队。回滚 = 回退镜像 tag。

**客户端**：随下一个版本发布；iOS NE 内存增量为固定大小结构，无需单独评估。

**验证工具的独立判别式**（`feedback_verification_tool_needs_its_own_discriminator`）：canary 上拿一条已知重度下载的连接（`serverUDPConn: closed` / `HandleTCP` 字节大）对照其 `cc-summary` 的 `ackedMB` 同量级；再拿一条只认证不传数据的连接确认**没有** final 行。

## 10. 文档更新

- `wire/k2cc/CLAUDE.md`：Observability 段改写（`Summary()`、`CCSummary`、直方图语义、纯度约束）。
- `wire/CLAUDE.md`：`quic.go` 条目补 `cc-summary` 发射器；`WireStats.CC` 字段与方向提示。
- `engine/CLAUDE.md`：心跳新字段与「上行」提示。
- k2app 记忆：支持工单排查流程改为 grep `DIAG: cc-summary`。

## 11. 后续阶段接口约定

`cc-summary` 行是 Phase 2/3 的数据契约：Phase 2 只**追加** `variant=<name>` 字段，不改既有 key；Center 聚合（Phase 2 末）按同一 key 集解析。`CCSummary` 结构体导出、字段稳定，Phase 2 的 θ 覆盖只增加 `K2CCConfig` 字段，不动累加器。
