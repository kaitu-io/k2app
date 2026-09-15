# 双通道质量记分板 + 迟滞切换 —— 设计

**日期**：2026-08-10
**范围**：k2 submodule —— `wire/`（transport.go, score.go[新], echo.go, tcpws.go, quic.go, stream.go）+ `engine/`（health.go, engine.go）
**前提依赖**：`fix/echo-timeout-misclassified`（已合入 k2 master，commit a3e9d34）—— 所有基于 echo 的判断要求 `IsEchoUnsupported` 分类正确。

---

## 1. 问题陈述

k2 的 QUIC(主) + TCP-WS(备) 双通道策略在两类线上场景下表现不佳，用户报告：QUIC 健康判断不够准；切到 TCP-WS 后"一会又切回来"；TCP-WS 顶不上去。

### 1.1 坐实的根因（已通过源码走查确认，非假设）

**根因 A —— 回滚探测只验握手、不验数据面（"切回来"震荡的直接原因）**
`TransportManager.quicProbeLoop`（`wire/transport.go:471-508`）在 fallback 期间每 30s 尝试 QUIC 恢复，但只调用 `qc.connect(probeCtx)`（握手）后就 `recordQuicSuccess()` 清除 fallback。**它从不做 EchoProbe**。当 UDP 未被完全封锁但 Short Header 数据被黑洞（GFW QoS/间歇丢包）时，握手照样成功 → 判定"QUIC 已恢复" → 切回 QUIC → 数据又不通 → 反复震荡。这正是 `wire/echo.go` 自己的 gotcha 所警告的"握手成功 ≠ 数据可流"，但恢复路径违反了它。

**根因 B —— 半死 QUIC 不触发 TransportManager fallback**
`handshakeFailStreak`（`wire/quic.go:357`）**只在 QUIC 握手失败时自增**（`quic.go:587/598`）。半死 QUIC 握手成功，`ConsecConnFails()` 恒为 0，永远达不到 `quicConnFailThreshold=3`，所以 `DialTCP/DialUDP` 里的 `SetFallbackActive(true)` 分支（`transport.go:225-227`）永不触发。TransportManager 层面对"半死"完全无感。

**根因 C —— 两套不协调的切换机制**
- 机制 1：TransportManager fallback（握手失败驱动，QUIC 留作 primary，probe 尝试恢复）。
- 机制 2：engine `echoLoop`（`engine/health.go:552`）数据面 echo 连续 3 击 → `reRaceChan` → `outboundReplaceLoop`（`engine/engine.go:1928`）跑**全量 `RaceTransport`**。

半死 QUIC 只有机制 2 能感知，但机制 2 是重量级三路竞速，落到 tcpws-as-primary 后（`ReplacePrimary` 清 `fallbackActive`，primaryType 变 "tcpws"），机制 1 的 probe 因为 `primary` 已不是 `*QUICClient` 而彻底停摆 —— 于是**再也回不到 QUIC**，除非一次网络 Reset。两机制目标冲突、互相踩踏。

**服务端事实**：`QUICServer.handleStream` 处理 `StreamTypeEcho`（`wire/quic.go:1458`）；`TCPWSServer.handleStream`（`wire/tcpws.go:892`）的 switch 只有 Meta/TCP/UDP/default，**不处理 Echo** → 客户端对 TCP-WS 发 echo 会读到 EOF → `IsEchoUnsupported` 返回 true。因此 TCP-WS 主动质量探测需要服务端新增分发。

### 1.2 设计目标

1. **健康判断测量化**：从"QUIC 死没死"（布尔）改为"通道质量分数"（连续量），半死 QUIC 分数持续走低而非维持"healthy"。
2. **切换/回滚不对称迟滞**：切走门槛低、回滚门槛高且必须 echo 验证，结构性消除震荡。
3. **让 TCP-WS 真正顶上**：TCP-WS 活跃且质量好时健康等级为 `healthy`（不再无条件 `degraded`）。
4. **统一两套机制**：通道选择归口到一个带迟滞的选择器；engine re-race 退回到它本来的正交职责（换服务器/端口）。

### 1.2b 与正交 spec 的边界（代理层出网黑洞检测 `ec3d6d1`）

同日有一份正交 spec `2026-08-10-proxy-egress-blackout-detection.md`（基于 dial 计数器 `f70b71a`），两者维度必须严格分开：

| 维度 | 本 spec（质量记分板） | 代理层出网黑洞检测 |
|---|---|---|
| 管什么 | **通道选择**（QUIC vs TCP-WS，同一节点） | **节点可用性**（该节点能否出网） |
| 信号源 | wire 层 echo + handshake + WireStats，**排除 proxy-layer 错误** | **proxy-layer 错误是唯一输入**（dial 全灭） |
| 动作 | 换通道 | 换节点（re-race Phase B） |

两者的分界正是 proxy-layer 错误归属：它进不了通道分数（换通道无用），只进节点可用性。**顺序约束**：黑洞检测 spec 声明它须排在本 spec 的 Phase 1/2 **之后**（依赖本 spec 对 re-race 语义的收窄，见 §4.2）。本 plan 不实现黑洞检测，但 Phase 1/2 完成后为它铺好前提。

### 1.3 非目标（YAGNI）

- 不做 active-active 双通道分流（复杂度爆炸，与"半死时省着用"矛盾）。
- 不做跨通道存量会话迁移（切换只影响新 dial，存量连接自然存亡 —— 保持现状语义）。
- 不做服务端下发评分（纯客户端本地决策）。
- 不改 `RaceTransport` 启动竞速逻辑（它已 echo 验证，语义正确）。
- 不改 relay 层指标驱动（`ErrDatagramRelayUnhealthy` 仍走 per-request stream 降级，不进通道分数 —— 遵守 `wire/VOCABULARY.md`）。

---

## 2. 架构总览

核心变化：通道选择从"布尔 fallbackActive + 握手计数"升级为"双通道分数 + 迟滞 FSM"。

```
                    ┌───────────────────────────────────┐
                    │  TransportManager (wire/transport.go)│
   DialTCP/UDP ───→ │                                     │
   Handshake        │   activeChannel ←── channelSelector │  (重构 fallbackActive)
                    │                        │ 读分数        │
                    │        ┌───────────────┴──────────┐  │
                    │        │ scoreQUIC      scoreTCPWS │  │  ← wire/score.go [新]
                    │        └───┬───────────────┬───────┘  │
                    └────────────┼───────────────┼──────────┘
   availability:  echo 成败 + handshake 成败 (纯 transport 层, 不含 dial)
   quality(quic): K2CC 丢包率 + RTT 膨胀 (WireStats)
   quality(tcpws):echo RTT 膨胀 (WireStats 对 tcpws 恒 0)
   (dial 成败 = 观测专用, 归节点可用性检测, 不入通道分数)
```

分层职责（遵守 k2 Layer Boundary Rules）：
- **wire 层**拥有通道选择（现在也归它管），channelSelector 是 TransportManager 内部状态机。
- **engine 层** healthMonitor 继续做整体健康 FSM + echoLoop；echoLoop 扩展为**驱动两条通道的分数更新**（而非仅 primary 探测），但不再拥有"切换"决策。
- 上层通过既有 `StatsProvider` 接口读分数（新增字段），不做 concrete type assertion。

---

## 3. 组件设计

### 3.1 `wire/score.go` —— ChannelScore（新增）

每条通道（QUIC / TCP-WS）一个 `ChannelScore`，合成三类信号，全部 EWMA 平滑（α=0.3，与 `engine/health.go` 现有 `ewmaAlpha` 对齐）。

| 信号类别 | QUIC 来源 | TCP-WS 来源 | 采集成本 |
|---|---|---|---|
| availability：echo 成功率（主动） | 现有 `EchoProbe` | **新增 smux echo 流**（复用 `StreamTypeEcho=0x04`；服务端新增分发；老服务器→unsupported→该信号缺席） | 按需，见 §3.3 |
| availability：handshake 成败 | `connect()` 成功/失败（即 `handshakeFailStreak` 的样本源） | 同 | 零 |
| 传输质量 | K2CC `PacketLossRate` + RTT/MinRTT（`WireStats`） | **echo RTT 膨胀比**（见下"来源修正"） | 零/按需 |

> **来源修正 1（TCP-WS 无 WireStats RTT，审查坐实）**：`TCPWSClient.WireStats()`（`wire/tcpws.go:148`）**只返回 UDP drop 计数 + `Transport="tcpws"`，RTT/MinRTT/LossRate 全为 0**（smux 无 CC 统计）。因此 TCP-WS 的 quality **不能**取自 WireStats，只能取自 **echo RTT**（主动探测，`echoRtt / minEchoRtt` 膨胀比）。QUIC 与 TCP-WS 的 quality 公式形状一致但输入不同源；ChannelScore 必须分别建模，不能假设 WireStats 对两通道对称有效。

> **来源修正 2（proxy-layer dial 结果必须排除在通道分数外，ec3d6d1/f70b71a 坐实）**：**availability 绝不能取"dial 成功率"**。原因：① `core/ClientTunnel` 已有 dial 计数器（`f70b71a`，按 `isProxyLayerError` 细分 `dialOk/dialFail/directOk/directFail`），是**观测专用**，喂 heartbeat 与正交的节点可用性检测（`ec3d6d1` 代理层出网黑洞检测），**不喂通道分数**；② 半死 QUIC 的 `DialTCP` 失败恰好是"读响应超时 → `ProxyLayerError`"（`wire/quic.go:727`），归 proxy 层，**dial 信号根本测不出半死**；③ 换通道救不了"节点出不了网"，把 proxy-layer 失败计入通道 availability 会触发无意义切换。故 availability **只由 echo（主动，纯 transport 层）+ handshake 成败（transport 层）驱动**，两者天然绕开 proxy-layer。这与 f70b71a commit message 的边界声明一致："Channel scoring in wire/ deliberately excludes proxy-layer errors"。

**分数公式（纯函数，初始参数，可调）**：

```
score        = availability × quality                    ∈ [0,1]
availability = EWMA(样本)  样本 ∈ {echo 成败, handshake 成败}  # 失败权重×2；纯 transport 层，不含 dial
quality_quic = clamp(1 − loss×2, 0, 1) × clamp(minRtt/rtt, 0, 1)   # loss+rtt 取自 WireStats
quality_tcpws= clamp(minEchoRtt / echoRtt, 0, 1)                    # echo RTT 膨胀比；无 echo → 0.5
```

- `availability` 的样本源**仅** echo 探测成败 + `connect()` handshake 成败（见"来源修正 2"），失败样本用双倍权重更新 EWMA ——"失败要快跌、恢复要慢升"，与 fail-safe 原则互补。
- **信号缺席 = 中性 0.5（fail-safe 硬规则）**：某信号无数据（老服务器无 echo、刚建连无被动样本）时该项记 0.5，**绝不因数据缺失产生切换**。源自 #3463 echo 误分类事故的宪法级约束。
- **样本时效性（审查坐实的缺陷）**：QUIC 一旦变为备用通道，不再有 dial 流量 → 其被动 `availability` 会**停在切换前的陈旧 1.0**，若直接参与回滚判断会假装 QUIC 健康。故每个信号带 `lastSampleAt`；**超过 3× 该信号当前探测间隔未更新的样本，回退为中性 0.5**。这保证"备用通道的分数只反映它当前被主动探测出来的质量"，陈旧的主用期分数不会污染回滚决策。

`ChannelScore` 是纯计算 + `atomic`/单锁字段，`Update(sample)` 与 `Value(now)` 均为微秒级临界区，不跨 I/O（遵守 Concurrency Rules）。所有权：每个 Dialer 持有自己的 ChannelScore；TransportManager 读两条通道的 `Value(now)` 做决策。

> 与既有 `wire/probe.go ProbeStats.Score()` 的关系：probe.go 的分数用于**节点选择**（哪台服务器），一次性快照，`-1` 哨兵表示 unsupported。本设计的 ChannelScore 用于**通道选择**（同一服务器 QUIC vs TCP-WS），持续 EWMA。两者正交，公式风格刻意对齐（都是 `availability×rtt×...` 形状）但不共享代码 —— 避免"测试抄公式"陷阱，各自独立单测。

### 3.2 `wire/transport.go` —— channelSelector 迟滞 FSM（重构 fallbackActive）

用一个显式状态机替换现有的裸 `fallbackActive atomic.Bool`：

```
状态: activeChannel ∈ {quic, tcpws}
      flapCount     int      // QUIC→TCPWS→QUIC 完整往返次数
      nextReviewAt  time.Time// 回滚评审窗口开启时刻
```

**切走（QUIC → TCP-WS），任一满足即切**：
1. **快速通路**（保留现有语义）：`ConsecConnFails() >= 3`（握手连续失败，UDP 彻底封锁）→ 秒切。
2. **质量通路**（新增，解根因 B）：`score(tcpws) > score(quic) × 1.5` 且**持续 ≥ 10s** → 切。半死场景由此路生效。
   - **前置条件（审查坐实的顺序依赖）**：质量通路要能比较，备用通道必须已有**非陈旧**分数。但 §3.3 规定主通道健康时移动端对备用通道 0 主动探测 → 此时 `score(tcpws)` 是陈旧/中性值，无法据以切换。解法：**主通道 score 跌破 0.7 是唯一的"预警触发器"**，它先驱动 §3.3 的频率控制器把备用通道探测升频（5s），攒够 ≥2 个新样本后质量通路才被允许评估。在备用通道尚无新样本的窗口内，只有快速通路（握手失败）能切走。这避免了"切到一条从没探测过、可能已死的备用通道"。

**切回（TCP-WS → QUIC），全部满足才切**（解根因 A，且不制造"永不回家"的反向缺陷）：
1. `now >= nextReviewAt`（在回滚评审窗口内）；
2. 后台探测完成 **握手 + 连续 3 次 EchoProbe 全部通过**（`quicEchoRecovered`，不再是握手即切回）。

**切回为什么不做分数比较**（审查坐实的设计修正，2026-08-10）：回退期间 QUIC 不承载任何真实流量，其 loss/RTT 质量**无法测量**，`ChannelScore.quality` 只能停在中性 0.5 → `score(quic) ≤ 0.5`。若切回门槛写成 `score(quic) > score(tcpws) × 1.2`，面对一条健康的 tcpws（分数近 1.0）**在数学上永不成立** —— 用户会被永久钉死在有状态的 tcpws/smux 回退路径上（车队规模下的真实成本回归），与"QUIC 是无状态战略首选"的前提相悖。此门槛**也无法靠 Task 7 补救**：`QualityFromEchoRTT` 是自相对量（minRTT/rtt），两条健康通道都≈1.0，1.2× 仍不可达 —— 只有 tcpws 自身劣化才会触发，那是"被赶走"而非"回家"。

**因此切回的健康证明就是 `quicEchoRecovered` 本身**（一次全新握手 + 3 次连续 echo，比任何 EWMA 分数都强的即时可用性证明）；防震荡由 **3 次 echo 门 + 递增的 flap 窗口**（下）承担，不由分数承担。这恢复了"探测成功即回 QUIC"的原始语义，但探测现在是**诚实的 echo 验证**（根因 A 已修）。切回决策落地后立即消费并清零 `quicEchoRecovered`，下次回退须重新赢得验证。

**迟滞方向性说明**（切走仍是分数驱动的不对称迟滞；切回是 echo 门驱动）：
- 当前在 QUIC：仅当 `score_tcpws / score_quic > 1.5` 才切走 TCP-WS（对离开 QUIC 保守）。
- 当前在 TCP-WS：QUIC 数据面 echo 连续 3 击通过 + 过 flap 窗口即切回（对回家积极）。不对称性由此体现 —— 离开靠"分数显著劣化"，回家靠"数据面证明恢复"，两侧信号不同源，刻意如此。

**震荡惩罚（收敛保证）**：每完成一次 QUIC→TCPWS→QUIC 完整往返，`flapCount++`，`nextReviewAt` 的窗口翻倍：`30s → 60s → 120s → … → 上限 600s`。`Reset()`（网络切换）把 flapCount 与窗口清零 —— 换了网络，旧震荡历史无意义。

**per-request fallback 保留不变**：单次 dial 失败仍即时试另一通道（`transport.go:232` 逻辑），proxy-layer 错误照旧不触发 fallback（`isProxyLayerError`）—— 用户请求不为策略延迟买单。这层语义完全不动。

**并发**：channelSelector 的状态用 TransportManager 现有的 `tm.mu` 保护（读/写字段级临界区）；后台探测的 I/O（connect+echo）在锁外执行，check-release-act-reacquire（遵守 Concurrency Rules，与现有 quicProbeLoop 同构）。

### 3.3 探测频率控制器（自适应，省移动端电量）

用户已确认：热备连接本身开销可忽略（空闲 QUIC/TCP-WS 连接 ~几十 KB + 一个 fd + 协议自带 keepalive），真正的成本是移动端 echo 探测唤醒蜂窝无线电。故按主通道状态自适应调频：

| 状态 | 主通道 echo | 备用通道 echo |
|---|---|---|
| 主通道 score ≥ 0.7（健康） | 现有指数信任表（1s→16s，`echoIntervals`） | 桌面：60s 低频；移动：**0 主动探测**，仅靠协议 keepalive（smux 10s ping / QUIC keepalive） |
| 主通道 score < 0.7（下滑） | 收紧到基础间隔 1s | 升频到 5s —— **切换前把对比数据攒够** |
| 刚切走后（QUIC 变备用） | 按 `nextReviewAt` 窗口退避探测 | （新主）按指数信任表 |

平台区分通过 `engine.Config` 新增字段（appext 显式传入 `MobilePowerSaving bool`），而非 build-tag —— 可测、可在 daemon/appext 各自控制。默认桌面 false、移动 true。

### 3.4 TCP-WS echo 支持（服务端 + 客户端）

- **服务端**（`wire/tcpws.go handleStream`）：新增 `case StreamTypeEcho:` → 调用一个 smux 版 `HandleEchoStream`（读 `echoNonceLen` 字节回写；echo.go 现有 `HandleEchoStream` 签名是 `*quic.Stream`，需抽出一个基于 `io.ReadWriter` 的公共实现，QUIC/smux 各自适配 —— 遵守"抽公共、不复制"）。
- **客户端**（`TCPWSClient`）：实现 `EchoProber` 接口（新增 `RunEchoProbe`），打开 smux 流写 nonce 读回。
- **向后兼容**：老 k2s 无 TCP-WS echo → 客户端读 EOF → `IsEchoUnsupported=true` → echo 信号缺席 → ChannelScore 的 echo-availability 与 quality 均按 §3.1 记中性 0.5（TCP-WS 仍有 handshake-availability 这一 transport 层信号），策略平滑退化，永不因缺 echo 误切。

---

## 4. engine 层配合改动

### 4.1 `health.go assess()` —— 删除"fallback 即 degraded"

现状 `assess()`（`health.go:339-341`）：`ws.WireFallback → HealthDegraded` 无条件成立。

改为：健康等级由**当前活跃通道的分数**决定，而非"是不是在 fallback"。TCP-WS 活跃且 `score(tcpws) ≥ 0.7` → `healthy`。`WireFallback` 仅作为 `DIAG: heartbeat` 的观测字段保留，不再驱动健康降级。这是"让 TCP-WS 名正言顺服务"的态度性修正 —— TCP-WS 从"耻辱降级态"变成正常通道。

### 4.2 `echoLoop` —— 驱动两通道分数，不再拥有切换

- 现状：`echoLoop`（`health.go:536`）每 TM 一个 goroutine，只探 `Primary()`，3 击 → reRace。
- 改为：探测结果除了喂 `echoStep`（re-race 用），**同时喂对应通道的 ChannelScore.Update**。备用通道按 §3.3 频率也探测并更新其分数。
- re-race 语义**保留但收窄**：echo 3 击仍触发 `reRaceChan`，但语义明确为"**当前活跃通道也救不了 → 换服务器/端口**"这个正交维度。通道内 QUIC↔TCPWS 的切换由 §3.2 channelSelector 独立负责，不再经过 re-race。这解开根因 C 的两机制冲突。

> **实现校正（final review I1/I4 —— 以此段为准，上一条是设计意图）**：re-race 并没有、也不需要退出通道维度，因为它跑的是全量 `RaceTransport`；真正会踩踏 channelSelector 的是**特定拓扑下的击数累加**，所以收窄落在 `probeOnce` 的 strike 门（`suppressStrike`，engine/health.go），而不是 `reRaceChan` 本身：
> - **fallback 期间 primary 仍是 QUIC**（`ActiveChannel()=="tcpws"` 且 `primaryChannel=="quic"`）：这条路已知是死的（正因为死了才切走），它的 echo 超时不再累加击数。否则数秒内 3 击 → re-race → `RaceTransport` 关掉 echo 失败的 QUIC 且不保留 backup → `ReplacePrimary(tcpws)` 抹掉 selector 的整个 fallback 状态（flap 历史、`quicEchoRecovered`、回滚机制），用户**再也回不到 QUIC**；半死场景里 re-race 几乎总是跑赢 10s 的质量路径。该路径的恢复由 wire `runProbeOnce` 独占。
> - **primary 就是 tcpws 且 `TCPRecentlyHealthy(30s)`**：本分支让 `TCPWSClient` 成了 `EchoProber`，tcpws primary 首次进入 1-16s / 2s 超时的探测表；8 字节 echo 与大流量共用同一条 TCP 连接，HoL 阻塞下连续 3 次超过 2s 完全可能发生在一条工作正常的连接上，而 re-race 会关掉正在服务传输的那个会话。
> - 两条门**都只拦击数，不拦记分**：`RecordEchoResult` 照常喂分数，selector 和 heartbeat 仍看得见真实劣化。没有近期 TCP 成功证据时，tcpws 击数照常累加。
> - 其余 echo 死亡照旧触发 re-race —— 这正是它"通道选择已经救不了这个 outbound"的本职。

### 4.3 兜底不变

两通道都烂时：channelSelector 不空转（维持当前通道），healthMonitor 现有 `HealthCritical → maybeAct → onReconnect`（`health.go:404`，10s cooldown + TCP health gate）路径兜底全量重连。职责不重叠：channelSelector 管"选哪条"，healthMonitor 管"都不行就整体重连"。

---

## 5. 可观测性

沿用 `DIAG:` 三层宪法（`k2/CLAUDE.md`）。

- `DIAG: heartbeat` 新增字段：`scoreQuic`、`scoreTcpws`、`activeChannel`（进 `HealthSnapshot` → ring → 桌面 SSE，工单可查）。
- `DIAG: transport-switch` 的 `reason` 扩展枚举（**以落地名为准**）：`quic-quality-degraded`（质量切走）、`quic-recovered`（echo 验证后切回）、`quic-handshake-threshold`（保留）、`quic-race-lost`（tcpws 赢下初始竞速，QUIC 以 fallback 态装为 primary）、`flap-damped`（想切回但被观察窗口拦，WARN 一次/窗口）。设计稿早期写的 `score-degraded` / `score-recovered` **没有落地**，勿据此写 grep/告警。
- 新增 `DIAG: channel-review`（INFO，阈值门控：仅每次回滚评审发生时记录）—— 字段 `activeChannel, scoreQuic, scoreTcpws, flapCount, nextReviewInS, decision`；`decision` 取值 `stay` / `switch-to-quic`（该事件只从回滚分支发出，没有 `switch-to-tcpws`）。加入 `k2/CLAUDE.md` 保留事件名表。

---

## 6. 错误处理与边界

| 场景 | 行为 |
|---|---|
| 老服务器（无 TCP-WS echo） | TCP-WS 靠 handshake-availability + 中性 quality；策略退化为"近似只修根因 A/B 的最小版"，不误切、不震荡 |
| echo 错误分类 | 沿用 `IsEchoUnsupported` 修复后语义（timeout 三重判定：`context.DeadlineExceeded` / `os.ErrDeadlineExceeded` / `net.Error.Timeout()`）；fail-closed |
| 两通道都烂 | channelSelector 维持现状 + healthMonitor critical 全量重连兜底 |
| UDP relay 不健康 | `ErrDatagramRelayUnhealthy` 照旧 per-request stream 降级，**不计入** ChannelScore availability（relay 层不驱动 wire 层，遵守 VOCABULARY.md） |
| 节点出不了网（proxy-layer 全灭） | **不计入** ChannelScore（换通道无用）；由正交的节点可用性检测处理（§1.2b）。dial 计数器已在 `core/ClientTunnel`（f70b71a），wire 层不重复 |
| 网络切换 Reset | `TransportManager.Reset()` 清 flapCount/窗口/`quicEchoRecovered`/`backupFresh`/两条分数 EWMA（`ChannelScore.Reset`，保留 `probeInterval`）**以及 `tcpwsMinEchoRTT` 棘轮**；`freshAfterReset` 语义保留。棘轮是必须项：EWMA 还能靠 staleness 衰减回中性，最小 RTT 只降不升、永不衰减，5ms 局域网基线带进 80ms 移动网会把 `QualityFromEchoRTT` 钉在 ~0.06，进程余生质量路径静默失效 |
| Stop 期间切换 TOCTOU | 沿用现有 P2 已知风险边界（QUIC idle 30s 回收），不新增未受保护路径 |

---

## 7. 测试策略

遵守记忆中的测试教训（变异验证、测试不抄公式、绿测试可能没到目标）。

1. **纯函数单测**（`score_test.go`, `transport_selector_test.go`[新]）：
   - ChannelScore 公式全覆盖：availability 失败双倍权重、quality_quic 半死走低、**quality_tcpws 取 echo RTT（非 WireStats）**、**信号缺席→0.5**、**陈旧样本(>3×间隔)→回退 0.5**、EWMA 收敛。
   - 顺序依赖：备用通道无新样本时质量通路不切走（只允许快速通路）；score<0.7 预警升频后攒够 ≥2 样本才放行质量通路。
   - 迟滞 FSM 全转移矩阵：切走两条路径（分数驱动，在 QUIC 时 ratio≤1.5 不动）、切回二条件（`now>=nextReviewAt` + `quicEchoRecovered`，不做分数比较）、flapCount 窗口翻倍序列（30→60→120→…→600 封顶）、Reset 清零、切回后 `quicEchoRecovered` 消费清零。
   - 参照现有 `echoStep`（`health.go:511`）的纯函数抽取模式，无 time/IO/log。

2. **wire 契约测试扩展**（`contract_test.go` 现有 11 个 + 新增 4 个）：
   - 半死 QUIC（握手通、echo 超时/高 RTT）→ ≤ 阈值秒数内切 TCP-WS（**根因 B 回归**）。
   - 恢复：QUIC 数据面恢复 → 必须"握手 + 连续 3 次 echo 通过（`quicEchoRecovered`）+ 过 flap 窗口"才切回；**只握手不 echo 绝不切回**（**根因 A 回归**）。**反向回归**：tcpws 健康（分数高）+ QUIC echo 已验证 → 回滚**必须**触发（钉死切回不做分数比较的修正，防止"永不回家"）。
   - flap 场景：反复触发 → nextReviewAt 窗口指数增长，`DIAG: flap-damped` 出现。
   - 老服务器：TCP-WS 无 echo → 永不因信号缺席切换。

3. **变异验证（必做）**：先写"回滚探测只握手就切回"的**失败测试**，确认它在改动前的 quicProbeLoop 上**红**、在新 channelSelector 上**绿**。这直接钉死根因 A 的修复真实生效，而非测试自欺。

4. **TCP-WS echo 端到端**（`tcpws_echo_test.go`[新]）：客户端 `RunEchoProbe` ↔ 服务端新 `case StreamTypeEcho` 往返；老服务器路径断言 `IsEchoUnsupported`。

5. **race 测试**：`go test -race -timeout 300s ./wire/...` 覆盖 channelSelector 并发读写 + 后台探测；`go test ./engine/...` 覆盖 echoLoop 双通道更新。注意 wire race 需 300s+（`k2/CLAUDE.md`）。

6. **真机 smoke**（实现阶段 plan 内，按 release confidence framework）：桌面 + iOS/Android 各一次；**代码信心与业务信心分两个数打分**；无真机 smoke 的功能改动信心封顶 6-7。

---

## 8. 落地阶段（供 writing-plans 拆解）

设计为单一 spec，但实现建议两阶段以便快速止血 + 独立验证：

- **Phase 1（止血根因 A/B，改动小、可独立发布）**：
  - `score.go` ChannelScore + 迟滞 FSM 骨架；
  - 回滚探测加 echo 验证 + 连续 3 次通过 + flap 窗口（不对称迟滞：离开靠分数劣化、回家靠 echo 证明；直接修"切回来"震荡且不制造"永不回家"）；
  - `handshakeFailStreak` 之外，echo 连续失败也计入切走（修"半死不切换"）；
  - `assess()` 删除 fallback→degraded。
- **Phase 2（TCP-WS 真正顶上 + 自适应省电）**：
  - TCP-WS echo（服务端 + 客户端 `EchoProber`）；
  - 备用通道主动评分 + 探测频率控制器 + 平台省电字段；
  - `echoLoop` 双通道分数驱动 + re-race 职责收窄。

Phase 1 的所有产物都是 Phase 2 的子集，无返工。

---

## 8b. 2026-09-15 修订：移动端有损 QUIC 逃生（工单 #3747）

**现象**：Android 0.4.10，QUIC 赢下竞速后链路对 UDP 大量丢包（运营商 QoS：握手、小 echo 都过，大流量全丢），167 秒内健康重连 3 次，每次拨回同一条 QUIC，浏览器什么都打不开；同一设备的 DNS 控制面走 TCP-WS 一直是通的。

**四道门为何全关**（源码坐实）：快速路要握手失败（握手是好的）；质量路要 `backupFresh ≥ 2`（§3.3 的移动端零探测让它永远是 0）；健康重连的 `Reset()` 每 10s 把记分板、`backupFresh`、flap 历史全清零再拨同一条 QUIC；echo 三击 re-race 在竞速里 QUIC 800ms 内又赢。

**修订四项（互相咬合，缺一不可）**：
1. **移动端探测门改丢包驱动**：`probeBackupOnce` 的省电早退改为「health FSM 为 critical 时允许」（`primaryLossCritical`），5s 节奏。不用 `ScoreQUIC < 0.7`：分数含 RTT 项，蜂窝 bufferbloat 会把健康下载压到 ~0.5，而 tcpws 首次在负载下探测得到的 minRTT 基线使其比值 ≈ 1.0，会把健康下载误切走。critical 只由丢包可达，且要连续 10 个健康样本才退出，自带迟滞。
2. **健康重连退避**：首次立即（"连接陈旧"值得一试），同一 critical 期内再次重连 30s → 60s → 120s 封顶（`actionCooldownBase/Max`）。10 个健康样本或网络事件归零。给选择器留出 ~20s 无干扰测量窗。
3. **健康重连保留记分板**：新增 `TransportManager.ResetConnections()`（只重置连接 + `freshAfterReset`），`engine.reconnect(preserveScoreboard=true)` 走它；网络切换仍用全清的 `Reset()`。否则回滚后再次 critical 会把 `flapCount` 清零，形成 30s 周期永久震荡。
4. **选择器加固**：`backupFresh` 改为 `ChannelScore.FreshSamples(now)`（staleness 窗口内计数，终审 follow-up #1）；质量路切走加目标下限 `minSwitchTargetScore = 0.3`（严格高于未知的 0.25：切换要目标的正向证据，QUIC 归零时不能"矮子里拔将军"）。

**合并后时间线**（丢包自 t0）：t0+7 critical、重连#1；t0+12 再 critical，探测门开；t0+17/22 两个 tcpws 样本；比值自 t0+7 起成立，t0+22 切到 TCP-WS；重连#2 最早 t0+37，抢不到。切换后 tcpws WireStats 无丢包字段 → healthy → 退避归零。

**残余（不在本次范围）**：切走后 QUIC 回滚探测在此类链路上 echo 仍能过，按 flap 窗口 30→600s 回滚再切走，每轮 ~20s 劣化（§3.2 设计的收敛方式）；两通道皆坏维持现状；客户端 `loss` 只见上行。信心：根因 10/10，代码 9.5/10（每道门有单测 + 变异验证），业务封顶 7/10（无真机有损网络 smoke）。

## 9. 影响面与风险

- **改动面**：`wire/transport.go`（重构核心）、新增 `wire/score.go`、`wire/echo.go`（抽公共 echo handler）、`wire/tcpws.go`（服务端 echo + 客户端 EchoProber）、`wire/stream.go`（无改，复用 StreamTypeEcho）、`engine/health.go`（assess + echoLoop）、`engine/config.go`（省电字段）、`engine/engine.go`（re-race 职责收窄）。
- **协议兼容**：TCP-WS echo 是**新增** stream type 分发，老客户端/老服务端任意组合仍互通（未知 type→Close，客户端 unsupported 降级）。无 wire 格式破坏。
- **高风险点**：channelSelector 重构触及所有 Dial 路径的热路径 + 并发；必须过 race + 契约 + 变异三重门，且真机验证前业务信心不超过 7。
- **回滚策略**：Phase 1/2 各自独立 commit；channelSelector 保留一个 env 开关（如 `K2_CHANNEL_SELECTOR_LEGACY=1` 回退到旧 fallbackActive 行为）作为线上快速回滚阀，Phase 2 稳定后移除。
