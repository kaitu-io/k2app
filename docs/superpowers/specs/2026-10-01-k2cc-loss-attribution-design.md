# K2CC 丢包归属修复 —— 让控制器看到真实丢包（Phase 2 第一项）

日期：2026-10-01 · 状态：**实施中**（实现已在 `feat/k2cc-loss-attribution`，默认关闭；实验室对比进行中，未上车队） · 范围：`k2/wire/k2cc/`、`k2/wire/cc_summary.go`、`k2/wire/quic.go`（仅服务端 sender 构造点）

## 1. 背景与已确认的事实

三周车队回顾（k2app `scripts/k2cc-obs/`，窗口 9/7–14 与 9/23–30）发现 cc-summary 的 `lossAgg` 系统性偏低。以下每条均已独立验证，**不是推断**：

| # | 事实 | 证据 |
|---|---|---|
| F1 | 晚于 MI 轮换才被 QUIC 判定的丢包，**不会被计入任何 MI** | 确定性单测 `loss_attribution_unit_test.go`（无网络、无随机；变异去掉过滤即红）|
| F2 | 可见比例约一半：丢包判定中位延迟 1.00×RTT，MI 中位时长 2.00×RTT | 集成测试 `loss_attribution_test.go`，FIFO 中继，空闲机器：seen/declared 0.47–0.51 |
| F3 | 车队同一比例：2853 条 ≥100 MB 连接 Σ(lossAgg·sent)/(sent−acked) = 0.47，七个地区 0.41–0.51 | 2026-10-01 回顾 |
| F4 | **所有**丢包消费者都读过滤后的值，没有补记路径 | 效用 `k2cc.go:494`、app-limited 锚点 `:783`、背景丢包 `:1096`、限速探测门 `:765`、ackRate `countFreshLossesLocked` |
| F5 | 过滤器本身是对的：它来自 `48d4f02`（2026-03-02），修的是「上一 MI 的丢包除以新 MI 的 BytesSent → LossRate 300–947 %」。问题不在「过滤」，在「丢弃」 | `TestOnCongestionEvent_StaleLossFiltering` 注释 |
| F6 | 作者当时已意识到可见性问题：`6de5f6b` 把 MinDuration 1×RTT→2×RTT「for QUIC loss detection visibility」——只修了一半 | 提交信息 |
| F7 | 默认集成测试中继每包一个 goroutine+timer，**空闲机器上也乱序**：配置 0 % 丢包，QUIC 判定 34.5 %。所有 `runBulkTransfer` 基准的名义丢包率与实际不符 | 集成测试 `reorder_*` 场景 |
| F8 | α=4 是**解析推导**的，不是在 F7 的假丢包上拟合的（`fb4d27a` 0.5→5 按信号量级；`6de5f6b` 5→4 按容忍边界）| git 历史 |
| F9 | kuic：同一 ACK 帧**先判丢包、后逐包确认**；`OnPacketSent` 对非 ack-eliciting 包也会调（这些包永远不会有 acked/lost 回调）；ECN 拥塞事件以 `lostBytes=0` 调 `OnCongestionEvent` | `sent_packet_handler.go:315/:453/:458-464/:893` |

| F10 | **BW 锚定循环的评估错位一拍**：`processBWAnchored` 的阶段标签指的是「下一拍要设的速率」，所以标签 Drain 下被评估的 MI 实际以上探速率（≈1.10×BW）跑，标签 ProbeUp 下被评估的是巡航 MI。`bgLossEWMA` 采样的是**最容易拥塞的那一拍**，量到的是自己造成的丢包；BW 上升检测永远看不到上探结果 | 确定性测试 `bw_phase_alignment_test.go`：无背景丢包的瓶颈上，legacy 4 个循环后 bgLoss = 6.1 %，对齐后 = 0 |

**F10 改变了本设计的安全前提**：今天丢包只见一半，恰好把「上探丢包 → bgLoss ↑ → `bw/(1−bgLoss)` ↑ → 巡航超发」这条正反馈压在一个低平衡点（硬瓶颈上约 8 %）。**只修归属不修错位，正反馈会放大**。因此两处修正必须同臂上线（§5 第 7 条），并保留两个仅实验室使用的拆分臂来分别归因。车队数据里「bgLoss ≈ 总丢包」也不能再当作「没有自致丢包」的证据。

**尚未确认**（本设计要回答的）：修正后实际吞吐与体验是变好还是变差（回顾里记为 A2，信心 4/10）。

## 2. 目标与非目标

**目标**
1. 控制器使用的每个 MI 丢包率 ≈ 该 MI 自己发出的包的真实丢包率（实验室 FIFO 中继上 seen/declared ≥ 0.85）。
2. 在同一 cc-summary 行里提供**与 variant 无关**的真实丢包度量，让 A/B 两臂可比，也让「车队丢包多少」有一个诚实答案。
3. 修复以服务端按连接 A/B 上线，有事先写定的决策规则，默认关闭，可按节点秒级回滚。

**非目标**
- 不动客户端（上行）sender；不动 β、ε、BW 估计器结构、MI 完成条件。
- 不在本轮调 α 以外的参数（空闲下限、bwDrop 确认等留到 Phase 2 后续项）。
- 不改 cc-summary 既有 key 的含义（只追加）。

## 3. 方案比较

记 `L_seen` 为控制器看到的 MI 丢包率。

### 方案 A：已判定分母（推荐）
MI 只统计**自己的包**（`pn ≥ FirstPacketNumber`）中**已经有结果**的部分：

```
L = LostOwnBytes / (AckedOwnBytes + LostOwnBytes)
```

- 分子与今天相同（过滤保留，F5 的保护不变）；分母从「本 MI 发出的全部字节」换成「本 MI 发出且已被确认或判丢的字节」。今天的公式等于 `L_A × 已判定比例`，已判定比例 ≈ ½ 正是 F2 的来源。
- 时序完全不变：仍在 MI 完成时计算，不引入额外延迟。
- 天然排除非 ack-eliciting 包（F9），天然 ≤ 1。
- Drain 阶段语义不变：Drain MI 只看自己发出的包，ProbeUp 尾部的拥塞丢包仍被过滤在外。
- **代价**：样本只有 MI 前约一半的包（后一半在 MI 完成时尚未判定），噪声变大；并且若拥塞丢包集中在 MI 后段（+ε 探测时队列逐渐堆积），前半样本会**低估拥塞丢包**。背景（随机）丢包无偏。这个偏差必须在实验室量化（§6.2），是选 A 还是 B 的判据。

### 方案 B：延迟评估（PCC Vivace 原生做法）
MI 停止发送后进入「待决」队列，直到其所有包都有结果（或超过 lastSend + PTO）才计算效用；新速率作用于下一个尚未开始的 MI（流水线深度 1）。

- 样本完整、无偏。
- **代价**：决策延迟 +1 个 MI（≈2 RTT）；慢启动退出晚一个 MI，过冲可能加倍；±ε 探测的配对、`probeBaseRate`、app-limited 锚点的时序全部要重排。改动面大、风险高。
- 仅当 §6.2 证明 A 的前半样本偏差不可接受时才考虑。

### 方案 C：窗口事件记账（否决）
`L = 窗口内判丢字节 / (窗口内确认字节 + 判丢字节)`，不区分包属于哪个 MI（ackRate 的做法）。
- 否决理由：Drain MI 的窗口会吃进 ProbeUp（1.10×BW）尾部的拥塞丢包，`bgLossEWMA` 被系统性抬高 → 带宽修正 `bw/(1−bgLoss)` 过大 → 巡航超发。它破坏了 BW 锚定循环赖以成立的前提。

## 4. 对每个丢包消费者的预期影响（方案 A）

| 消费者 | 位置 | 今天（看到约 ½） | 修正后 | 方向 |
|---|---|---|---|---|
| PCC 效用 `U = T − α·L·R − β·ΔRTT` | `:494`、`:841` | 真实丢包边界 `2/(1+α)` = **40 %**（α=4）| 边界 `1/(1+α)` | α 不变则**更保守** |
| 背景丢包 `bgLossEWMA`（Drain）| `:1096` | 约真实值的 ½ | ≈ 真实值 | BW 锚定巡航**更积极**（见下）|
| BW 修正 `bw/(1−bgLoss)` | `:1037`、`:1051`、`:1078`、`:796` | 巡航 ≈ `0.95·C·(1−b)/(1−b/2)` | 巡航 ≈ `0.95·C`（设计本意）| 背景丢包 b=10 % → 巡航 +5.5 %；b=20 % → +11 % |
| app-limited 锚点 `T/(1−L)` | `:783` | 低估容量 | 按设计 | 空闲后恢复更快 |
| 限速探测门 `L < 0.30` | `:765` | 真实 60 % 以下都探测 | 真实 30 % 以下 | 高丢包链路探测减少 |
| ackRate cwnd 补偿 | `countFreshLossesLocked` | 同样只见 ½ | 同步修正（改为不过滤：它是窗口量，没有 MI 归属语义）| 被 `minAckRate=0.95` 钳住，影响 ≤5 % |

两条路径方向相反：PCC 梯度路径变保守，BW 锚定路径变积极。车队约 20 % 的 MI 处于 BW 锚定，但批量传输窗口里它占主导，所以**净效果无法靠推理得出，必须测**。

### 4.1 α 的取值
α=4 的推导前提（代码注释：`gradient ∝ 1 − L(1+α)`，边界 `L = 1/(1+α)` = 20 %；提交 `6de5f6b` 写的是 25 %，两处不一致）针对的是**测得**的 L。今天真实边界约 40 %。修正后可选：

| 臂 | α | 真实丢包边界 | 含义 |
|---|---|---|---|
| `fix-a4` | 4 | 20 % | 保持常数不变，只修测量 |
| `fix-a3` | 3 | 25 % | 回到 `6de5f6b` 写明的设计意图 |
| `fix-a15` | 1.5 | 40 % | PCC 路径行为中性，隔离出「修测量」对 BW 锚定路径的净影响 |

车队真实丢包：每连接 p90 ≈ 25 %，AU 均值 ≈ 21 %（F3 口径，含乱序误判，是上限）。`fix-a4` 会让高丢包地区在 PCC 路径上更容易被压到下限，风险最大。实验室阶段三个都跑，车队阶段按 §6.2 结果至多保留两个。

## 5. 实现要点（方案 A）

1. `MonitorInterval` 增加 `AckedOwnBytes`；`OnPacketAcked` 中 `mi.PacketsSent > 0 && number ≥ mi.FirstPacketNumber` 时累加。`BytesLost` 已经只含自己的包，不变。
2. `LossRate()` 按配置分派：`legacy` = 今天公式；`resolved` = §3 A 公式。已判定包数 < 5 时回退到 legacy（低速 MI 的样本保护）。
3. `OnCongestionEvent`：`lostBytes == 0`（ECN 事件）直接返回，两种模式都一样（今天是加 0，行为不变）。
4. `K2CCConfig` 增加 `LossAttribution`（`legacy` | `resolved`）与既有 `Alpha`；`NewCCSenderGFWMode` 行为不变（legacy, α=4）。新增 `NewCCSenderVariant(name)`。
5. `resolved` 模式下 ackRate 统计不再调用 `countFreshLossesLocked`。
7. **阶段对齐**（F10）：MI 创建时记下设定其速率的阶段 `ranPhase`；`processBWAnchored` 拆成「评估」（对齐模式按 `mi.ranPhase`，legacy 按标签）与「调度」（两种模式相同：6 巡航 + 1 上探 + 1 排空）。未带阶段的 MI（刚进入锚定模式的第一拍）不做评估。
8. **诊断字段**（cc-summary 追加，契约只增不改）：`variant`、`declLoss`（事件时刻的真实丢包，与臂无关）、`staleLoss`（被过滤丢弃的占比）、`resolved`（MI 被评判时已有结果的字节占比）、`lossCruise` / `lossProbe` / `lossDrain`（按 MI 实际所跑阶段统计的自有包丢包率——上线后直接读出每个增益的代价）。
6. 服务端 `wire/quic.go:1322`：读环境变量 `K2_CC_VARIANTS`（如 `control:50,fix-a3:25,fix-a15:25`），每条连接按权重随机选臂；未设置 = 100 % control，**部署即空操作**。客户端（`:602`）与 k2p（`quic_p2p.go`）不动。

## 6. 验证计划（先实验室，后车队）

### 6.1 第 0 步：修基准测试中继（独立 PR，先于一切）
`runBulkTransfer` 改用 FIFO 中继，重跑收敛 / 下降 / 瓶颈基准，**重新记录基线**并调整门槛（今天的门槛是在约 35–45 % 假丢包上定的，F7）。不改任何算法代码。没有这一步，§6.2 的对比没有意义。

### 6.2 实验室对比（每臂同一矩阵）
矩阵：RTT {100, 200, 300} ms × 随机丢包 {0, 2, 5, 10, 20} % × 瓶颈 {无, 20 Mbps, 50 Mbps}，FIFO 中继。每格记录：稳态吞吐、QUIC 判定丢包、RTT 膨胀、收敛 MI 数、下限驻留。

两个必须回答的问题：
1. **方案 A 的前半样本偏差**：在「瓶颈 + 随机丢包」格子里，分别对 ProbeUp / Cruise / Drain / PCC ±ε 的 MI，比较 `L_resolved` 与该 MI 自己包的最终真实丢包率（测试探针可在 MI 完成后继续记账）。判据：各阶段偏差 |Δ| ≤ 2 个百分点 → 用 A；否则转方案 B 并重新评审。
2. **§4 的吞吐预测是否成立**：有背景丢包的格子里，`fix-*` 的 BW 锚定巡航吞吐应比 control 高约 `b/2`；若没有，说明 §4 的模型错了，停下重想。

### 6.3 车队 A/B
- cc-summary **追加** key（不改既有 key，符合观测规格 §11 的契约）：`variant=`、`declLoss=`（与臂无关的真实丢包：窗口内全部 `lostBytes>0` 事件 ÷（该值 + 窗口内确认字节），不经过滤）。`lossAgg` 保留各臂控制器看到的值——在 fix 臂里它应 ≈ `declLoss`，这本身就是上线后的第一条正确性检查。
- 灰度：1 个节点（`K2_CC_VARIANTS` 设到 50/25/25）跑 24 h → 读回 cc-summary 确认两个新 key 与分臂比例 → 全车队。回滚：删环境变量重启 k2s。
- 事先写定的决策规则（按批量窗口 `appLimited<50% && mis≥5`，按地区分层，≥ 1 周）：
  - **主指标**：批量窗口 `tputP50`；≥10 MB 会话的字节 / 时长。
  - **护栏**：`declLoss` 不得比 control 高 1 个百分点以上；`rttP50/minRtt` 不得上升 > 10 %；下限驻留不得上升。
  - 采纳条件：主指标不差于 control、护栏全部满足，且在样本量足够的地区（每臂 ≥ 2000 个批量窗口）中至少 5/7 一致。统计用分层自助法 95 % 置信区间，不看点估计。
  - 任一臂触发护栏 → 立即从 `K2_CC_VARIANTS` 摘除该臂。

## 7. 测试计划

- 单测：`resolved` 模式下 `TestLossAttribution_LateDeclaredLossIsCountedNowhere` 的同一构造应得出每 MI 10 %（新测试）；`legacy` 模式保持 5 %（原测试不变）。已判定包数 < 5 的回退；ECN 零字节事件；非 ack-eliciting 包不进分母。
- 集成：`TestIntegration_LossAttributionAccuracy` 增加 `resolved` 臂，断言 seen/declared ≥ 0.85；legacy 臂继续钉在 [0.25, 0.75]。
- 变体选择：权重解析、未设置时 100 % control、非法值回退 control 并告警。
- `go test ./lint/`（并发规则）+ 变异验证（每个新断言至少一次）。

## 8. 需要评审拍板的问题

1. 方案 A 为首选、B 仅作后备——同意吗？
2. 车队 A/B 保留哪几个 α 臂？建议实验室三个都跑，车队只带 `fix-a3` 和 `fix-a15` 各 25 %（`fix-a4` 在高丢包地区风险最大，除非实验室结果推翻这个判断）。
3. 第 0 步（换 FIFO 中继并重设基准门槛）会让现有基准数字全部变化，是否接受作为独立 PR 先行合入？

## 8. 实验室矩阵结果（2026-10-01，PVE ubuntu VM 4 vCPU，load <0.3，每格 4 次 × 12 s）

`K2CC_LAB=1 go test ./wire/k2cc/ -run TestLab_VariantMatrix`（linux/amd64 交叉编译后在 VM 上跑；本机 load 30–220 不可用）。

| 场景 | control goodput 中位 [min–max] | fix-a4 | fix-a3 | fix-a15 | control 见/真 | fix 见/真 |
|---|---|---|---|---|---|---|
| cap20 rtt50 loss0 | 19.0 [18.8–19.2] | 19.2 | 19.0 | 18.8 | 19.4/37.9 | 32–34/32–35 |
| cap20 rtt50 loss5 | 18.7 [17.6–19.1] | 18.9 | 18.9 | 18.7 | 19.6/37.9 | 33–35/33–35 |
| cap20 rtt50 loss15 | 17.5 [16.8–18.1] | 16.6 | 18.1 | 17.5 | 20.2/38.3 | 39–40/39–40 |
| cap20 rtt200 loss0 | 11.7 [10.5–13.6] | 12.0 | 13.2 | 10.5 | 17.5/33.3 | 37–38/36–37 |
| cap20 rtt200 loss5 | 11.0 [10.3–12.1] | 12.0 | 11.9 | 12.3 | 17.6/33.5 | 39–40/38–39 |
| cap20 rtt200 loss15 | 10.4 [7.0–11.2] | 10.7 | 11.0 | 10.4 | 19.9/38.8 | 39–42/38–41 |
| nocap rtt200 loss5 | 38.1 [37.6–38.5] | 38.2 | 38.0 | 38.1 | 3.3/6.2 | 5.3–6.0/5.5–5.8 |
| nocap rtt200 loss20 | 26.2 [21.4–31.2] | 27.4 | 29.2 | 30.7 | 10.4/20.6 | 21–22/20.5–21.4 |

结论：

1. **计量**：control 在全部场景只见真实丢包的约一半；fix 各臂整体误差 ≤1.3 pt，单阶段最差 4.1 pt（多数 ≤3.2）。目标达成。
2. **F10 实证**：`lab-resolved-only` 在 cap20/rtt50 把 bwEst 估到 45–49 Mbps、真实丢包 50%；fix 各臂无此现象。两修必须同臂。
3. **吞吐**：各臂差异全部落在 run 间波动内，实验室分辨不出 α=4/3/1.5，也**不能**声称有速度收益。α 的选择只能靠车队 A/B。
4. **bgLoss 仍被拥塞丢包污染**：cap20 loss0 的真实背景丢包是 0，control 报 20%，fix 报 8–12%——改善但未归零。
5. **与本修复无关的策略健康问题**：policer（20 ms burst）链路上所有臂真实丢包 32–40%，bwEst 高于 cap（23–29 对 20）。可能是 policer 模型过苛（无队列）；车队里用 `declLoss` / `lossCruise` 判断真实线路是否如此。

## 9. 上线前验证（2026-10-01）

**control ≡ 线上（实证，不是推理）。** 临时差分夹具（未入库）：把 origin/master 与本分支的 `wire/k2cc` 各拷一份，`time.Now` 换成同一个假时钟，同一个模拟网络（RTT 20–300 ms 且会跳变、背景丢包 0–30%、policer 0/5/20/100 Mbps、app-limited、乱序、ECN 事件、非 ack-eliciting 包）驱动两者，**每次回调后**比对 cwnd / Snapshot / HasPacingBudget / CanSend / TimeUntilSend / 每个完成 MI 的全部字段。

| 对比 | 结果 |
|---|---|
| master vs master（夹具确定性） | 30 seeds 无分歧 |
| master vs `control` | **300 seeds × 30 s：1.05 亿个包、40 522 个 MI 全部逐位一致** |
| master vs 未知臂名 / 空名 | 60 seeds 无分歧 |
| master vs fix-a4 / a3 / a15 / lab-resolved-only / lab-align-only（判别式） | 58 / 58 / 58 / 54 / 50 of 60 seeds 出现分歧 |

车队 k2（b18bee3）到 origin/master 之间 `k2cc.go` / `summary.go` / `cc_summary.go` 无改动，所以结论对车队版本同样成立。

**独立审查的发现与处置：**
- ECN 事件（`lostBytes==0`）的提前返回原先在 `ensureCurrentMI` 之前，首个回调恰为 ECN 时 MI #0 晚建几微秒 → 已挪到之后（654034d），差分夹具含 ECN 事件。
- `lab-*` 臂可被 `K2_CC_VARIANTS` 选中 → `ParseVariants` 拒绝（654034d）。
- **fix 臂有第三处行为差异（此前未声明）**：resolved 模式下 `OnCongestionEventEx` 不做 stale 过滤，全部丢包计入 ackRate，而 ackRate 经 `rateToCwnd` 放大 cwnd。保留（时间窗量没有 MI 归属，过滤无意义），但车队 A/B 评估的是「分母 + 对齐 + ackRate 全量」这一整体；`lab-align-only` 不含此项。
- aligned 模式下 app-limited 且 throughput==0 的 MI 之后，下一 MI 标为 None 而速率未变 → 偶尔少一个评估样本，保守方向，不处理。

**全量测试（PVE ubuntu VM，load <0.3）**：`wire/k2cc` 完整套件含三个基准门槛 PASS（HK 96.9% / JP 54.6% / US 41.5% / overall 76.6%）。

**试点**：`canary/k2cc-lossattr-on-fleet`（33edd87）= 车队 k2 b18bee3 + 仅本改动的生产文件。节点 jp-tokyo.aws.wm04，不推镜像：`/apps/k2s/docker-compose.override.yml` 把 `/apps/k2s/canary/k2s` 挂到 `/app/k2s` 并设 `K2_CC_VARIANTS`。阶段 1 `control:100`（2026-10-01 08:54:32Z 起）；阶段 2 `control:50,fix-a4:50`。回退：删 override + `sudo docker compose up -d`。
