# 专属线路 节点侧流量掐断 (Private-Node Traffic Cutoff) 设计

**日期**: 2026-06-16
**状态**: 待用户 review
**承接**: task #57 (Part 2 Phase B — node-side actuation + cutoff smoke);Part 2 Phase A 已落地(host-NIC 计量 + Center 上报)
**关联记忆**: project_private_node_api_deployed_backward_compat_pass / project_dedicated_line_reframe_phase1

---

## 1. 目标 (Goal)

让 **sidecar 成为单台专属线路流量防御的总负责人**:本地高频读真实网卡用量,在配额用尽时**直接掐断该节点的数据面**,使一个 provider(如 AWS Lightsail)会按量计费的节点**永远不会真正超额亏钱**。同时把超额前的预警邮件从 80/95 两档调整为 **70/80/90 三档**。

## 2. 背景 / 为什么 (Why)

路由器版因市场原因重定位为「定制线路版」:买了独立 VPS 线路的用户即可使用路由器功能。直觉上"独立线路 = 不用担心流量超"——**这个假设是错的**。独立 VPS 不是无限流量,它是一个**有月度配额(bundle)的计费资源**。重定位只是把超流量风险从"共享池全员变慢"**搬到了"这一条线的配额 / 这一条线的账单"**。

风险大小取决于 provider 计费模型:

- **硬上限型**(搬瓦工类,超了只限速/断,不计费):用户超额 = 他自己线变慢,我们不亏钱。
- **超额计费型**(Lightsail/AWS,超出包含流量按 ~$0.09/GB 计):用户超额 = **我们吃账单**。一个天天 4K(~7GB/h)的用户一个月可多冲 1TB+,直接变成真金白银的损失。

Part 2 Phase A 已让 Center 在 95% 拦掉**新连接**(`slave_api_device_auth.go`),但**已建立的连接(正在看视频的那条流)不会被切**——这是当前产品形态的真实漏洞。本设计补上节点侧的真掐断。

## 3. 原则 (Principle,用户拍板)

- **核心:不让节点用超流量。**
- **反应及时即可,擦着 100% 过一点点、多花几分钱可接受。** 因此掐点定在 100%,不需要保守缓冲;反应速度由"sidecar 多久看一眼网卡"决定,而非 60 秒的 Center 上报周期。
- **掐断(硬切),不限速。** 对花钱买专属线路的用户,降速体验也差,且实现复杂;直接掐 + 提前三档邮件预警是更清晰的契约。
- **下个计费周期自动恢复。**
- **掐断后无"用户自助加量续命"入口**(等下月 / 找客服 / 再买线),与既有生命周期一致。

## 4. 职责分工 (Responsibility Split)

| 层 | 职责 | 变更 |
|---|---|---|
| **sidecar**(本仓 docker/sidecar) | 真相源 + **防御执行者**:高频读真实网卡用量、到顶掐断数据面、下月自动恢复 | **新增** enforcer + 快速本地循环 + docker 控制 |
| **Center**(api/) | 纯记账 + 通知:记录用量、在 70/80/90 发预警邮件 | 邮件阈值 80/95 → **70/80/90** |
| **k2s / k2v5**(k2 子模块,只读核心) | 跑数据面;被 sidecar 暂停/恢复 | **一字不改** |

理由(与 Part 2 一致的 Option B):k2s 是三端共用的只读协议核心,不应承载"单台机器配额到没到顶"这类与单节点账单耦合的逻辑;Center 当记账员不当警察;**掐断这种"在机器上真动手"的事归 sidecar**——它本就持有真相(网卡字节)与节点凭据。

## 5. 架构与数据流 (Architecture & Data Flow)

### 5.1 既有(Phase A,不动)
- `host_nic.go` `hostNICMeter`:读 `/host/proc/net/dev`,返回**自当前 epoch 起的累计真实字节**(epoch 变更时 rebaseline 归零)。
- `usage_reporter.go` `usageReporter`:**每 60 秒**把累计字节 POST 到 Center `/slave/usage`(Basic-auth `ipv4:secret`)。Center 返回 `NodeUsageResponse{ Verdict, EpochID, QuotaTotal, QuotaUsed, ... }`。epoch 变更时 reporter 调用 `meter.Rebaseline()`。

### 5.2 新增:enforcer(快速本地掐断循环)

```
            ┌─────────────── sidecar 容器 ───────────────┐
  /host/proc│  hostNICMeter ──读──┐                       │
            │                     ├─> usageReporter ─60s─>│──> Center /slave/usage
            │                     │     (记账+取 QuotaTotal)│      (Center 发 70/80/90 邮件)
            │                     │         │ SetQuota      │
            │                     │         v               │
            │                     └─> enforcer ─每5s─> 比对 │
            │                              │  用量 >= 配额?   │
            │                              v                 │
  docker.sock<──── pause/unpause k2v5 (对账循环,自愈) ──────┘
```

- **enforcer 持有**:docker 客户端、当前 `quotaTotalBytes`、当前 `epochID`、`meter` 引用、内部"是否已由我掐断"状态。
- **取配额**:reporter 每个 60s 周期从 Center 响应拿到 `QuotaTotal`/`EpochID`,调 `enforcer.SetQuota(epochID, quotaTotal)` 写入(加锁,因现在两个 goroutine 都碰)。
- **快速循环**(默认 **5 秒**,env 可调):
  1. `used = meter.CumulativeBytes()`(读 `/host/proc/net/dev`,便宜)
  2. 若 `quotaTotal > 0 && used >= quotaTotal` → **应掐**;否则 → **应通**。
  3. **对账(reconcile,自愈)**:应掐且 k2v5 在跑 → `docker pause k2v5`;应通且 k2v5 被我暂停 → `docker unpause k2v5`。每个 tick 都对账,即使 docker 守护重启把容器弄活了,下一 tick 会再掐。
- **epoch 变更(下月)**:reporter 已 rebaseline meter(used 归零)。下一 tick `used(≈0) < quotaTotal` → 自动 unpause,线路恢复。**无需独立"恢复"逻辑,对账循环天然处理。**

### 5.3 掐断机制选型(关键决定)

**选 `docker pause/unpause k2v5`(经 docker socket),不选 iptables。**

- sidecar 在 bridge 网络,其 netns 内的 iptables **管不到宿主机的转发/物理网卡**;要在宿主机 netns 动 iptables 需 `network_mode: host` 或 `pid:host`+privileged+nsenter,会破坏现有 docker DNS(`K2V4_HOST=k2v4-slave`)且更危险。
- `k2v5` 是数据面;暂停它 → 代理流量停 → 宿主机网卡停涨(仅剩 sidecar↔Center 心跳的极小流量)。`pause` 冻结进程(现有连接挂起)、`unpause` 瞬时恢复,比 `stop/start` 更适合"临时掐断"语义,且不丢容器。
- **provider 无关**:不依赖任何云厂商 API,Lightsail/搬瓦工/SSH 裸机一视同仁。

代价:sidecar 需挂载 `/var/run/docker.sock`(宿主机 root 等价权限)。对我们自管的单一用途专属节点可接受;见 §8 安全。

## 6. Center 侧改动(仅邮件阈值)

`api/worker_private_node_traffic_warning.go`:`warnThreshold80/95` → **70/80/90 三档**。
- 模型 `CloudInstance`:`Warn80SentEpoch`/`Warn95SentEpoch` → `Warn70SentEpoch`/`Warn80SentEpoch`/`Warn90SentEpoch`(按 epoch 去重,新增 Warn70/Warn90 列,弃 Warn95 列)。
- 去重逻辑:同一轮从高到低判定(90>80>70),只发一封。
- EDM 模板 `private-node-traffic-warn` 文案适配三档(已有模板,改 percent 取值范围措辞)。

**Center 不做掐断判定**(verdict=stop 的真执行交节点)。既有 95% 新连接闸门(`slave_api_device_auth.go`)保留作纵深防御,不冲突。

## 7. 阈值与时序决定

- **掐点 = 100% of `QuotaTotal`**(`used >= quotaTotal`),不留保守缓冲(用户接受微量超额)。
- **快速循环间隔默认 5s**(env `K2_CUTOFF_POLL_INTERVAL` 可调),真机 smoke 调优。
- **超额上界估算**(掐前最坏多跑):线速 × 间隔。100Mbps × 5s ≈ 62MB;1Gbps × 5s ≈ 625MB。对 TB 级配额是 0.03%~0.06%,即"几分钱",符合原则。

## 8. 权限与 compose 改动 (docker/docker-compose.yml)

`k2-sidecar` 服务新增:
- `volumes: - /var/run/docker.sock:/var/run/docker.sock`(掐断需要)
- 新 env:`K2_CUTOFF_ENABLED`(默认随 `K2_PRIVATE_CLAIM` 派生)、`K2_CUTOFF_POLL_INTERVAL`(默认 5s)。

**安全**:docker.sock = 宿主机 root 等价。仅在专属节点镜像启用;不记录任何凭据;enforcer 只对固定容器名 `k2v5` 执行 pause/unpause,不接受外部指令。共享池节点不挂 socket、不启 enforcer(见 §9)。

## 9. 共享池零影响 (Shared-Pool Safety)

enforcer 与 reporter 同一开关:**仅当 `PrivateClaim != ""` 时在 main.go 构造**。共享池节点:不读配额、不掐断、不挂 docker.sock → 行为 byte-identical,零新增调用路径。

## 10. 失败模式 (Failure Modes)

| 情况 | 行为 |
|---|---|
| Center 不可达(刷不到新配额) | enforcer **保留上次已知 quotaTotal 继续执行**;不会因 Center 抖动误放行 |
| 从未拿到 Center 响应(全新节点) | `quotaTotal==0` → 不掐(全新节点用量≈0,无超额风险);拿到首个响应后开始 |
| 读网卡失败(meter err) | **该 tick 不改变掐断状态**(fail-safe:不因读取错误误掐已通的线) |
| docker.sock 不可用 / pause 失败 | 记 `DIAG` 错误日志(可被监控捕获),无法掐断=已知降级;不 panic |
| docker 守护重启复活 k2v5 | 下一 tick 对账重新 pause(自愈) |

## 11. 真机验证计划 (Real-Machine Smoke — 不可省)

掐断是"在真实机器上真动手"的功能,**本地测不算数**,必须在真 VPS 上跑:
1. 配小额度(如 1GB)节点,客户端连上跑流量,确认越 70/80/90 各收到一封邮件(Center)。
2. 持续跑到 >100%,确认 enforcer 在 ~一个 poll 间隔内 `docker pause k2v5`,客户端流量**当场断**(含已建立连接)。
3. 确认掐断后宿主机网卡用量基本停涨(只剩心跳)。
4. 模拟 epoch 翻转(或手动 +1 epoch),确认 enforcer 自动 `unpause`,线路恢复。
5. 确认共享池节点完全不受影响(无 enforcer、无 socket)。

## 12. 风险与信心 (Risk & Confidence)

- **代码层(单元/集成可验)**:meter 已绿、reporter 已绿、enforcer + 对账逻辑可单元测(注入 fake docker client + fake meter)、Center 三档邮件可 mock DB 测。这部分目标 **8.5/10**。
- **业务触达(端到端真掐)**:依赖真机 docker 行为、网卡读数延迟、pause 是否真停流——**未真机前封顶 6-7/10**,与既有 node/client-side 规矩一致。
- **主要风险**:① docker.sock 权限/可用性因节点而异;② `pause` 是否对所有连接类型(QUIC/TCP-WS)都立即止流需真机确认;③ 间隔 5s 的实际超额在高线速节点上的真值。

## 13. 范围外 (Out of Scope)

- 限速(rate-limit)方案:已否决,选硬掐。
- 用户自助加量/续命入口。
- 多节点订阅 / 平滑升级(task #18)。
- iptables/tc 宿主机级管控(本设计用 docker 控制替代)。

---

## 实现任务拆分(预览,详见后续 plan)

1. **sidecar enforcer + 快速对账循环**(docker client、SetQuota、reconcile、fail-safe)— 单元测注入 fake docker/meter。
2. **reporter → enforcer 接线**(每周期 SetQuota;meter 加锁因多 goroutine)。
3. **main.go 接线**(PrivateClaim 门控构造 enforcer + 启快速循环)。
4. **compose**:挂 docker.sock + 新 env。
5. **Center 邮件三档**(worker 70/80/90 + 模型列 + EDM 文案)— mock DB 测。
6. **真机 smoke**(task #57,封顶 6-7)。
