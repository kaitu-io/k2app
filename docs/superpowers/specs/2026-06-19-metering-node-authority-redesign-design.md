# 计量职责重划：节点单一权威 + Center 记录器 + 统一 NodeUsage（全节点）

**状态：** 设计已与产品对齐（2026-06-19，含"共享池也统一为自量+硬断"的决策），待 review → 进 writing-plans。

**Goal（一句话）：** 把**所有节点**（共享 + 私有）的流量计量与超额掐断统一为"节点单一权威"：节点自己量宿主 NIC、按 `.env` 限额本地硬断、定期上报；Center 退化为被动记录器 + 离线/超额派生。计量统一进一张 `NodeUsage` 表（按 NodeID 1:1），退休"provider 账单同步当计量源 + CloudInstance 当计量家 + SlaveNodeLoad 夹带流量"的三处散乱。

**Architecture（2-3 句）：** 节点 sidecar 复用既有 `TrafficMonitor`（`.env` `K2_NODE_BILLING_START_DATE` 月度时钟 + `K2_NODE_TRAFFIC_LIMIT_GB` 限额）作为本地唯一权威，enforcer 据此在"剩余 ≤ 500MB"即 `pause k2s`；reporter 定期把"已用 + 限额 + 周期"上报 Center。Center 不裁决、不下发配额，只把上报写进 `NodeUsage`（按 NodeID）并记 `last_report_at`；**所有节点同一条规则**：`限额 > 0 且 已用 ≥ 限额 − 500MB` → 节点硬断 + Center 隐藏（不再分共享/私有、不再分 95%/100%）。

**Tech Stack：** Go（`docker/sidecar/` 独立 module + `api/` Center）、GORM/MariaDB。**不涉及 k2 submodule。**

> ⚠️ **影响面提示**：本方案从"只碰私有"扩大到"碰全体共享池（所有 App 用户的服务节点）"。决策理由 = 简化部署 + 简化 Center 排查（见 §1.3）。代价 = 更大的回归/灰度面（§8）。

---

## 1. 背景与动机

### 1.1 现状（已读代码核实 2026-06-19）

- **节点侧 `TrafficMonitor`（`traffic.go`/`collector.go`）已是所有节点共用代码**（`main.go:159` 无条件启 Collector）：吃 `K2_NODE_BILLING_START_DATE` + `K2_NODE_TRAFFIC_LIMIT_GB`，`calculateNextCycleEnd/checkAndResetCycle` 已做按 day-of-month 的月度重置，`GetTrafficStats()` 已返回 `{BillingCycleEndAt, MonthlyTrafficLimitBytes, UsedTrafficBytes}`。**当前仅喂 health 上报，不掐断。**
- **私有掐断**（`enforcer.go`+`usage_reporter.go`，`PrivateClaim!=""` 才启）：enforcer 靠 Center `SetQuota` 下发配额硬断，另开了第二个 NIC 读取器（`host_nic.go`）。
- **Center 计量散在三处**：
  1. 共享池超额（`isTunnelOverQuota` 95% 隐藏）读 `CloudInstance.TrafficUsedBytes` ← **provider 账单同步**（`worker_cloud.go`，曾出 netns 读错 bug）。
  2. 节点自报用量 → `SlaveNodeLoad`（仅负载评分用，`slave_api_report.go`）。
  3. 私有 `/slave/usage` → 写 `CloudInstance`（裁决 + lazy epoch reset）。

### 1.2 三个结构性硬伤

1. **计量寄生在 provider 专属的 `CloudInstance` 上**：无 CloudInstance 的节点（住宅/SSH-standalone/BYO）计量裸奔。
2. **IP 匹配脆弱**：`/slave/usage` 按 `ip_address` 撞 `CloudInstance`（IP 回收/撞行，P0 同类雷）。
3. **数据散三处**：排查一个"用量/超额"问题要对三张表三条路 —— 正是运维痛点。

### 1.3 产品决策（2026-06-19 与用户确认）

1. **月度重置时钟归节点 + 运维管**（Center 不碰 epoch，只跟随节点上报）。
2. **节点必须定期上报已用字节**；Center 记 `last_report_at`，静默超阈值判 offline。
3. **流量处理 + 掐断是节点（sidecar）的责任**，超了直接掐，且仍定期上报。
4. 计量从 `CloudInstance` 抽到独立表（按变化频率 + 职责分离）。
5. **共享池也统一为"自量 + 硬断"**。理由（用户原话）：**简化部署方案 + 简化 Center 问题排查**。
6. **单一阈值，所有节点一致**（用户 2026-06-19 进一步简化）：废弃"共享 95% 软隐藏 + 100% 硬断"的两道阈值，改为一条规则 —— **`限额 > 0 且 已用 ≥ 限额 − 500MB` 即硬断**（预留 500MB 缓冲，覆盖上报延迟、防 provider 超量计费）。
   - 掐断时 sidecar 持续上报"已用 ≥ 限额−500MB"，Center 据此立即把节点从隧道列表隐藏 → 客户端刷新看不到 → 自动改挑别的。靠确定信号驱动（不依赖离线计时器），同一个数字同时驱动"节点拉闸"与"Center 隐藏"。
   - 取消软隐藏的增量：硬断会断掉该节点上的活动连接（旧 95% 软隐藏只挡新连接）。等同"节点宕机/重启"，客户端（桌面自动重挑 / 手机重连取刷新后列表）本就会处理；一次可恢复抖动。用户已接受。
   - **守卫**：`限额 = 0`（不限量）永不掐 —— 否则 `已用 ≥ 0 − 500MB` 恒真会误掐。绝对 500MB 对 TB 级限额=贴满额留缓冲；仅当限额 < 500MB 才会显得过早（产品无此场景）。

---

## 2. 最终职责划分（适用所有节点）

| 维度 | 节点 / sidecar（单一权威） | Center（被动记录 + 派生） |
|---|---|---|
| 配额数字 | **权威**，来自 `.env`（运维写入/管理；0 = 不限不掐） | 只存 node-sourced 拷贝（显示/预警/可见性，非权威） |
| 月度重置时钟 | **节点本地拥有**，锚点 `K2_NODE_BILLING_START_DATE`（运维的工作） | 不参与，只跟随节点上报的 `epoch` |
| 计量 | 读宿主 NIC（`TrafficMonitor`） | 不计量 |
| 掐断决策 + 执行 | **节点本地判断 + `docker pause k2s`**（`限额>0 且 已用 ≥ 限额−500MB`） | 不判断、不裁决 |
| 上报 | 定期上报「已用 + 限额 + 周期标识」 | 收下、按 epoch 取 max、记 `last_report_at` |
| 可见性 | —— | 同一条规则派生：`限额>0 且 已用 ≥ 限额−500MB` → 从列表隐藏（所有节点一致，无共享/私有之分） |
| 离线 | —— | `last_report_at` 静默超阈值 → 读时派生 offline（无 cron） |
| 预警 70/80/90 | —— | 私有线路基于 node-sourced 数字算（共享池无客户预警） |

### 2.1 统一的好处（对应决策 1.3.5）

- **一条上报路**：所有节点自报 → `/slave/usage` → 一张 `NodeUsage` 表。排查只看一处。
- **一套部署**：所有节点同一 compose + 同一组 `.env`（限额 + billing date）。不再分"私有加 X、共享不加"。
- **退休** provider 账单同步当计量源（连带其 netns bug 类）、`CloudInstance` 当计量家、`SlaveNodeLoad` 夹带流量。
- 节点侧 `TrafficMonitor` 本就共用 → 只需把 enforcer + reporter 的"私有 gate"去掉，让它们在所有节点运行。

### 2.2 三层配额所有权

| 角色 | 住哪 | 性质 |
|---|---|---|
| 售出业务量（私有线路，如 2TB 不可变快照） | `PrivateNodeSubscription.TrafficTotalBytes`（已有，不动） | 业务事实（仅私有） |
| 运营权威（节点据此掐断，所有节点） | 节点 `.env` `K2_NODE_TRAFFIC_LIMIT_GB`（运维管） | 单一权威 |
| Center 镜像（显示/预警/可见性/离线，所有节点） | **新 `NodeUsage`（NodeID 1:1）** | node-sourced，非权威 |

---

## 3. 数据模型

### 3.1 新表 `NodeUsage`（`api/model.go`，按 NodeID 1:1）

```go
// NodeUsage 是节点流量的运行时计量镜像（1:1 SlaveNode）。节点是配额与掐断的单一
// 权威；本表只是 Center 侧的 node-sourced 反射，供显示、预警、隐藏/可见性、离线
// 派生 —— 绝不参与节点掐断决策。覆盖所有节点（共享 + 私有）；从 CloudInstance
// 抽离的动机见 §1.2。按 NodeID 键：用量是"这台节点的 NIC 做了多少"的属性，与
// 它属于谁（私有 sub）正交。节点 unregister→recreate 换 NodeID 时镜像会短暂重置，
// 但节点持有真实计数（持久化基线 §4.4），下个上报周期即补齐 —— 纯显示层 cosmetic。
type NodeUsage struct {
	ID        uint64 `gorm:"primarykey" json:"id"`
	CreatedAt int64  `gorm:"autoCreateTime" json:"createdAt"`
	UpdatedAt int64  `gorm:"autoUpdateTime" json:"updatedAt"`

	NodeID uint64 `gorm:"uniqueIndex;not null" json:"nodeId"` // → SlaveNode.ID（1:1）

	// node-sourced 反射（节点权威，Center 只记录）
	Epoch           int64 `gorm:"not null;default:0" json:"epoch"`              // = 节点 BillingCycleEndAt（节点拥有，Center 跟随）
	UsedBytes       int64 `gorm:"not null;default:0" json:"usedBytes"`          // 当前 epoch 内累计已用（取 max，幂等抗乱序/重复）
	QuotaTotalBytes int64 `gorm:"not null;default:0" json:"quotaTotalBytes"`    // 节点上报的限额（来自 .env；0=不限）
	LastReportAt    int64 `gorm:"not null;default:0;index" json:"lastReportAt"` // 最后一次成功上报 Unix 秒（离线派生）

	// 预警去重（与 Epoch 比对；!= 才发，发后置当前 epoch）—— 私有线路用
	Warn70SentEpoch       int64 `gorm:"not null;default:0" json:"-"`
	Warn80SentEpoch       int64 `gorm:"not null;default:0" json:"-"`
	Warn90SentEpoch       int64 `gorm:"not null;default:0" json:"-"`
	Exhausted100SentEpoch int64 `gorm:"not null;default:0" json:"-"`
}
```

### 3.2 `CloudInstance` / `SlaveNodeLoad`（计量退役）

- `CloudInstance` 的 `TrafficUsedBytes/TrafficTotalBytes/TrafficResetAt/TrafficEpoch/Warn*SentEpoch` **保留不删**（避免破坏性迁移），但**不再是任何节点计量/掐断/可见性的权威源**。`worker_cloud.go` provider 同步可继续写它们供 admin cloud 视图当 provider 账单参考，或后续清理。
- `SlaveNodeLoad` 的 `UsedTrafficBytes/MonthlyTrafficLimitBytes/BillingCycleEndAt` 不再被超额/可见性消费（负载评分本身不依赖它们）；可保留为历史或后续清理。
- 一次性删列留待后续迁移，不在本次范围。

### 3.3 不变量

- **I-Cutoff-Rule**：唯一掐断/隐藏规则 = `QuotaTotalBytes > 0 && UsedBytes >= QuotaTotalBytes - quotaCutoffReserveBytes`，其中 `quotaCutoffReserveBytes = 500 << 20`（500 MiB）。节点与 Center 用同一常量；`限额=0` 永不掐（不限量）。
- **I-Quota**：节点 `.env` 限额是掐断唯一权威；Center 任何拷贝都不参与掐断。
- **I-Bundle**（成本天花板，沿用 provisioning 铁律#7）：VPS bundle 含量 ≥ 限额 + headroom。即使 sidecar 整个失效、k2s 裸奔，最坏只是超量服务亏点，不爆 provider 账单。
- **I-Report**：节点掐断后**仍持续上报**（sidecar 与 k2s 是不同容器，pause k2s 不影响 sidecar）→ Center 立即据"已用≥限额"隐藏节点；不依赖离线计时器。
- **I-Hide-on-Exhaust**：节点从列表消失由 I-Cutoff-Rule 同一确定信号驱动（≠ offline 派生）。offline 是另一条独立信号（节点没上报了）。

---

## 4. 节点侧改动（`docker/sidecar/`）

> 节点本地配额 + 月度时钟已存在（`TrafficMonitor` + `K2_NODE_BILLING_START_DATE` + `K2_NODE_TRAFFIC_LIMIT_GB`，已接进 compose/entrypoint/demo.env）。本次是**统一 + 权威翻转 + 去私有 gate**，不新增 epoch-anchor env。

### 4.1 enforcer 改吃本地 `TrafficMonitor`，且在所有节点运行

- `main.go` 去掉 `PrivateClaim != ""` 这道 gate：**所有节点**都启 reporter + enforcer。
- enforcer 不再消费 Center 回包的 `QuotaTotal/QuotaUsed/EpochID`，也不再需要独立 `HostNICMeter`。
- 掐断判据读 `TrafficMonitor.GetTrafficStats()`，套用 I-Cutoff-Rule：`MonthlyTrafficLimitBytes > 0 && UsedTrafficBytes >= MonthlyTrafficLimitBytes - quotaCutoffReserveBytes` → `docker pause k2s`；反之 unpause。**限额 0 = 不限不掐**（未设限额的老共享节点行为不变）。沿用 5s reconcile + `cutoff.state` 持久化。
- 删 `usage_reporter → enforcer` 的 `SetQuota`/`quotaSink`；让 enforcer + reporter **共用 Collector 已建的同一个 `TrafficMonitor`**（单一 NIC 读取源，杜绝漂移）。

### 4.2 月度时钟 = 复用 `K2_NODE_BILLING_START_DATE`

- **不新增 env**。epoch 身份 = `BillingCycleEndAt`（跨周期单调递增）上报；Center 见更大值即采纳新 epoch 并清零（§5.1）。周期切换由 `TrafficMonitor.checkAndResetCycle` 自动 rebaseline。
- **部署须确保所有节点 `.env` 写 `K2_NODE_BILLING_START_DATE`**（缺它 `NewTrafficMonitor` 报错不启用 → 该节点不计量不掐断，仅 I-Bundle 兜底，见 §8）。

### 4.3 上报体扩字段

```go
type NodeUsageRequest struct {
	EpochID         int64 `json:"epoch_id"`          // = TrafficMonitor.BillingCycleEndAt
	CumulativeBytes int64 `json:"cumulative_bytes"`  // = TrafficMonitor.UsedTrafficBytes
	QuotaTotalBytes int64 `json:"quota_total_bytes"` // = TrafficMonitor.MonthlyTrafficLimitBytes（新增）
	Seq             int64 `json:"seq"`
	Ts              int64 `json:"ts"`
}
```

- 响应只需 ack（`next_report_interval`），reporter 不再依赖回包的配额/裁决/epoch。
- JSON tag 必须与 Center `NodeUsageRequest` 逐字一致（CLAUDE.md 跨层约定）。

### 4.4 重启不丢周期内用量（既有隐患，须一并修）

`TrafficMonitor` 启动把 `cycleStartBytes` 重锚到当前 NIC（`traffic.go:55-56`）→ 容器重启会把本周期已用清零。今天只喂 health 无所谓，一旦升为掐断权威会让掐断点漂移。须把 `cycleStartBytes` + 当前周期标识持久化（与 `cutoff.state` 同机制），重启恢复而非重锚。**这是把 TrafficMonitor 提为掐断权威的前置修复。**

---

## 5. Center 侧改动（`api/`）

### 5.1 `slave_api_usage.go` → 纯记录器（所有节点）

```
node := ReqSlaveNode(c)                       // 已认证（ipv4:secret）→ NodeID
upsert NodeUsage by NodeID:
  if req.EpochID > usage.Epoch:               // 节点进入新周期 → 跟随
      usage.Epoch = req.EpochID; usage.UsedBytes = req.CumulativeBytes
  else if req.EpochID == usage.Epoch:
      usage.UsedBytes = max(usage.UsedBytes, req.CumulativeBytes)
  // req.EpochID < usage.Epoch（乱序/陈旧）→ 不动 used
  usage.QuotaTotalBytes = req.QuotaTotalBytes // 永远采纳 node-sourced
  usage.LastReportAt = now
响应：{ next_report_interval }                 // 不再回 verdict / quota / epoch
```

**删除**：verdict 计算、lazy epoch reset、`trafficStopThreshold*` 常量、Center bump epoch、按 IP 撞 `CloudInstance`。`NodeUsageResponse` 瘦身为 ack（保留 envelope 形状）。**端点不再私有专属**（所有节点都调）。

### 5.2 可见性 / 超额 / 离线派生改读 `NodeUsage`，单一阈值

- **单一派生函数**（无 Class 分支，套 I-Cutoff-Rule）：`isNodeOverQuota(usage)` = `QuotaTotalBytes > 0 && UsedBytes >= QuotaTotalBytes - quotaCutoffReserveBytes`。共享池（`/api/tunnels`）与私有（`/api/subs` via `entitlement_resolver`）都用它隐藏。
- 现有 `isTunnelOverQuota` / `isPrivateTunnelExhausted` 两个函数（签名 `(*CloudInstance)`、阈值 95%/100%）**合并为一个** `isNodeOverQuota`（读该节点 `NodeUsage`，按 NodeID 解析），调用点统一切过去。
- 新增 `isNodeOffline(usage, now)`：`now - LastReportAt > offlineThreshold`（建议 300s）。隐藏判定 = 超额 **或** 离线。

### 5.3 预警 worker 改读 `NodeUsage`

- `worker_private_node_traffic_warning.go`：扫 active 私有线路，经 `sub.SlaveNodeID → NodeUsage` 读用量 + `Warn*SentEpoch`，去重键 = `Epoch`。cron 不变。

### 5.4 用户面板 / DTO

- `api_user_private_node.go`：`TrafficUsedBytes` 改取该线路节点的 `NodeUsage.UsedBytes`；耗尽判断改调 `isNodeOverQuota`（剩余≤500MB，去 95% 常量）。
- `type.go` DataPrivateNodeSub 注释来源「CloudInstance」→「NodeUsage」。

### 5.5 provisioning / provider 同步

- 卖出配额经 NodeOperation `params.trafficTotalBytes` → provisioning agent 写节点 `.env`（既定流程）。`slave_api_node.go linkCloudInstanceQuota` 不再为掐断服务，可删/降级为 informational（#16 reconciler 因此消亡）。
- `worker_cloud.go` provider 同步**不再是计量权威**；保留与否取决于 admin cloud 视图是否仍要 provider 账单参考（§3.2）。

---

## 6. 迁移与部署

1. `AutoMigrate` 建 `node_usages`（纯增量）。
2. **无需 backfill**：节点是配额+epoch 权威，下一个上报周期（≤报告间隔）自动写满镜像。可选从 CloudInstance 拷 `Warn*SentEpoch` 避免私有重发预警（cosmetic）。
3. `CloudInstance`/`SlaveNodeLoad` 流量列保留（§3.2）。
4. **部署迁移（共享池，新增工作量）**：给所有共享节点 `.env` 写 `K2_NODE_TRAFFIC_LIMIT_GB`（= bundle 含量或安全分数）+ `K2_NODE_BILLING_START_DATE`。**未设限额（=0）的节点：不掐断、不隐藏**（行为同今日"无限"，安全回退）。
5. **部署序 + 兼容**：
   - 新 Center 先上：对**旧 sidecar** 安全 —— 旧私有 sidecar 靠 Center 回包配额掐断，新 Center 回包瘦身 → 旧私有节点过渡期**不硬断**（靠 I-Bundle 兜底）；旧共享 sidecar 本就不调 `/slave/usage`，无影响。
   - 新 sidecar 后铺（#76，现在 gate 整个池）：本地掐断不依赖 Center，多带的 `quota_total_bytes` 旧 Center 忽略 → 安全。
   - **runbook 必须写明过渡窗口**：新 Center 上线后到新 sidecar 全量铺开前，硬断依赖 I-Bundle，不依赖软件。

---

## 7. #16 处置

- **reconciler/告警半 → 关闭（设计上消亡）**：Center 不再持有权威配额，无漂移可对账。
- **deprovision 清 `bound_ipv4`/绑定半 → 已完成**（main `c6c40780`），归 P0 身份单一权威（#77），保留。
- 任务板：#16 标记 closed（superseded by 本 spec）。

---

## 8. 边界与残留风险

| 场景 | 行为 | 兜底 |
|---|---|---|
| **任意节点硬断（剩余≤500MB）** | 该节点活动连接被断；同一信号让 Center 立即隐藏它，新连接转走 | 等同节点宕机：桌面自动重挑 / 手机重连取刷新列表；500MB 缓冲防 provider 超量；用户已接受活动连接被断 |
| sidecar 单独挂、k2s 没挂 | 无掐断；Center 无上报 → 读时判 offline（但节点其实在服务） | I-Bundle 兜底；恢复上报即自愈 |
| 节点上报中断 | 节点本地继续掐断（不依赖 Center）；镜像陈旧 → 可能误判 offline | offline 仅影响可见性，不影响真实服务 |
| 上报乱序/重复 | epoch 内取 max，跨 epoch 跟随更大 epoch | 幂等 |
| 缺 `K2_NODE_BILLING_START_DATE` | `TrafficMonitor` 不启用 → 该节点不计量不掐断 | provisioning/部署必写；否则退化 I-Bundle 兜底 |
| 限额未设（=0） | 不限不掐不隐藏（同今日无限节点） | 安全回退；上限额前靠 I-Bundle |
| **影响面（决策 1.3.5 的代价）** | 改动触及全体共享池 = 所有 App 用户服务节点 | 更狠的回归测 + 更慢灰度（#46）；新 sidecar 全量铺开是硬门 |
| 配额改档（升/降级私有线路） | 节点 `.env` 限额固定；MVP 不支持原地升级 | 改档 = 重开线/客服改 `.env` 重启（[[#18]] defer） |

---

## 9. 测试策略（TDD）

- **节点侧**（`docker/sidecar` 单元）：enforcer 读 `TrafficMonitor` 掐断（`used >= limit − 500MB`；`限额=0` 不掐；`限额<500MB` 边界）；周期切换 rebaseline；**重启恢复 `cycleStartBytes` 不清零**（§4.4 回归测）；上报体含 `quota_total_bytes` + 周期 epoch；所有节点（去私有 gate）都启 reporter+enforcer。
- **Center 侧**（集成，真 dev MySQL，`skipIfNoConfig`）：
  - `/slave/usage`：任意节点 upsert `NodeUsage`（epoch 取 max / 跟随新 epoch / 采纳 quota_total / 记 last_report_at）。
  - 可见性派生：`isNodeOverQuota`（剩余≤500MB 隐藏，含 `限额=0 不掐`、`限额<500MB 边界`两个 case）/ offline 隐藏。共享 + 私有同一函数。
  - `entitlement_resolver`：私有耗尽或离线 → 剔除。
  - 预警 worker：经 `SlaveNodeID → NodeUsage`，去重键 = Epoch。
  - 旧 sidecar 兼容：共享旧 sidecar 不调 `/slave/usage` → 无 NodeUsage 行 → 不隐藏（同今日）。
- 整数算术（字节 * 百分比不溢出 int64）沿用现有约定。

---

## 10. 文件清单

**节点侧** `docker/sidecar/`
- 改：`main.go`（去 `PrivateClaim` gate，所有节点启 reporter+enforcer 并共用 Collector 的 `TrafficMonitor`）、`sidecar/enforcer.go`（改吃 `TrafficMonitor`，删 `SetQuota`/`HostNICMeter`）、`sidecar/usage_reporter.go`（上报扩字段 + 用周期标识填 epoch、不消费回包）、`sidecar/traffic.go`（持久化 `cycleStartBytes`+周期标识，重启恢复 §4.4）。
- **不新增 env**（复用 `K2_NODE_BILLING_START_DATE` + `K2_NODE_TRAFFIC_LIMIT_GB`）。可能删：`sidecar/host_nic.go`（NIC 读取统一到 `TrafficMonitor`）、reporter 的 `quotaSink`。

**Center 侧** `api/`
- 建：`model.go`（+`NodeUsage`）。
- 改：`slave_api_usage.go`（纯记录器，全节点）、`logic_tunnel_score.go`（合并 `isTunnelOverQuota`+`isPrivateTunnelExhausted`→单一 `isNodeOverQuota` 读 NodeUsage + `isNodeOffline` + `quotaCutoffReserveBytes` 常量）、`entitlement_resolver.go`、`api_tunnel.go`（共享池隐藏读 NodeUsage）、`worker_private_node_traffic_warning.go`、`api_user_private_node.go`、`type.go`、`slave_api_node.go`（linkCloudInstanceQuota 降级/删）。
- 迁移：`AutoMigrate` 注册 `NodeUsage`。

**部署**：所有共享节点 `.env` 补 `K2_NODE_TRAFFIC_LIMIT_GB` + `K2_NODE_BILLING_START_DATE`；新 sidecar 全量铺开（#76）。

**不动**：k2 submodule；`CloudInstance`/`SlaveNodeLoad` 结构（列保留）；`worker_cloud.go` provider 同步（降为 informational）；`api_admin_cloud.go`（provider 视图）。
