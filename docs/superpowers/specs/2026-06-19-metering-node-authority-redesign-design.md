# 计量职责重划：节点单一权威 + Center 记录器 + PrivateNodeUsage 表

**状态：** 设计已与产品对齐（2026-06-19），待 review → 进 writing-plans。

**Goal（一句话）：** 把专属线路的流量配额与掐断权威从 Center 搬到节点（sidecar），Center 退化为被动记录器 + 离线探测器；并把"计量镜像"从 provider 专属的 `CloudInstance` 抽离到 private-only 的 1:1 `PrivateNodeUsage` 表。

**Architecture（2-3 句）：** 节点 sidecar 持有自己的配额（来自 `.env`，运维管）、本地拥有月度重置时钟、本地读宿主 NIC 计量、超额即本地 `docker pause k2s`。sidecar 继续定期向 Center 上报"累计字节 + 自己的配额总量 + 节点本地 epoch"。Center 不再裁决、不再 lazy-reset、不再持有权威配额，只把上报写进 `PrivateNodeUsage`（按 SubID）并记录最后上报时间用于离线派生。

**Tech Stack：** Go（`docker/sidecar/` 独立 module + `api/` Center）、GORM/MariaDB。**不涉及 k2 submodule。**

---

## 1. 背景与动机

### 现状（已读代码确认 2026-06-19）

- **节点侧** `docker/sidecar/sidecar/enforcer.go` 已经在本地掐断（超额 `docker pause k2s`，持久化 cutoff 状态，重启自愈）。✅
- **但权威在 Center**：enforcer 的 `quotaTotal/quotaUsed/epochID` 来自 `usage_reporter` 的 `SetQuota`，而那是 Center `/slave/usage` 响应回的 `CloudInstance.TrafficTotalBytes / TrafficUsedBytes / TrafficEpoch`。节点 `.env` 的 `K2_NODE_TRAFFIC_LIMIT_GB` 当前只用于显示，不参与掐断。
- **Center** `slave_api_usage.go` 是"裁判 + lazy epoch reset"：按 IP 撞 `CloudInstance`，算 95% verdict，bump epoch，下发配额。无 cron（lazy reset on heartbeat）。

### 三个结构性硬伤

1. **计量寄生在 provider 专属的 `CloudInstance` 上。** `/slave/usage` 明确写：`无 CloudInstance（共享节点 / 未 sync）→ 不计量放行`。产品方向是任意 VPS / 搬瓦工 / 住宅 IP / BYO，这些可能**没有 CloudInstance 行** → 私有节点计量裸奔。
2. **IP 匹配脆弱**：`WHERE ip_address = node.Ipv4 ORDER BY id DESC` 是 IP 回收/撞行的老雷（P0 同类）。而上报本来带认证（`ipv4:secret` → 直接是某 `SlaveNode`），绕去按 IP 撞 CloudInstance 既脆又多余。
3. **配额总量三份**：`sub.TrafficTotalBytes`（售出）/ `CloudInstance.TrafficTotalBytes`（执行）/ 节点 `.env`（将成权威）。三份漂移 = #16 reconciler 要对账的根。

### 产品决策（2026-06-19 与用户确认）

1. **月度重置时钟归节点，且属于运维人员的工作**（Center 完全不碰 epoch，只跟随节点上报的 epoch）。
2. **节点必须确保上报已用字节**；Center 记录最后上报时间，超过阈值即判该节点 offline。
3. **流量处理 + 掐断完全是 sidecar 的责任**，超了直接掐，且仍定期上报 Center。
4. 计量镜像**从 `CloudInstance` 挪到独立 1:1 `PrivateNodeUsage` 表**（按变化频率 + 职责分离：sub 是慢变业务快照，usage 是 60s 高频运行时反射）。

---

## 2. 最终职责划分

| 维度 | 节点 / sidecar（单一权威） | Center（被动记录 + 离线探测） |
|---|---|---|
| 配额数字 | **权威**，来自 `.env`（运维写入/管理） | 只存一份 node-sourced 拷贝（显示/预警/可见性，非权威） |
| 月度重置时钟 | **节点本地拥有**，锚点在 `.env`（运维的工作） | 不参与，只跟随节点上报的 `epoch` |
| 计量 | 读宿主 NIC | 不计量 |
| 掐断决策 + 执行 | **节点本地判断 + `docker pause k2s`** | 不判断、不裁决 |
| 上报 | 定期上报「累计字节 + 自己的配额总量 + epoch」 | 收下、按 epoch 取 max、记 `last_report_at` |
| 离线 | —— | `last_report_at` 静默超阈值 → 读时派生 offline（无 cron） |
| 预警 70/80/90 + 客户端「流量耗尽」可见性 | —— | 基于 node-sourced 数字算，不影响真实掐断 |

### 三层配额所有权

| 角色 | 住哪 | 性质 |
|---|---|---|
| 售出业务量（如 2TB，不可变快照） | `PrivateNodeSubscription.TrafficTotalBytes`（已有，不动） | 业务事实 |
| 运营权威（节点据此掐断） | 节点 `.env` `K2_NODE_TRAFFIC_LIMIT_GB`（运维管） | 单一权威 |
| Center 镜像（显示/预警/可见性/离线） | **新 `PrivateNodeUsage`（SubID 1:1）** | node-sourced，非权威 |

---

## 3. 数据模型

### 3.1 新表 `PrivateNodeUsage`（`api/model_private_node.go`）

```go
// PrivateNodeUsage 是专属线路的运行时计量镜像（1:1 PrivateNodeSubscription）。
// 节点是配额与掐断的单一权威；本表只是 Center 侧的 node-sourced 反射，供显示、
// 预警、客户端「流量耗尽」可见性、离线派生使用 —— 绝不参与节点掐断决策。
// 从 CloudInstance 抽离的动机见 spec §1：计量不该寄生在 provider 专属概念上。
type PrivateNodeUsage struct {
	ID        uint64 `gorm:"primarykey" json:"id"`
	CreatedAt int64  `gorm:"autoCreateTime" json:"createdAt"`
	UpdatedAt int64  `gorm:"autoUpdateTime" json:"updatedAt"`

	SubID uint64 `gorm:"uniqueIndex;not null" json:"subId"` // → PrivateNodeSubscription.ID（1:1）

	// node-sourced 反射（节点权威，Center 只记录）
	Epoch           int64 `gorm:"not null;default:0" json:"epoch"`           // 节点上报的计费周期身份（节点拥有，Center 跟随）
	UsedBytes       int64 `gorm:"not null;default:0" json:"usedBytes"`       // 当前 epoch 内累计已用（取 max，幂等抗乱序/重复）
	QuotaTotalBytes int64 `gorm:"not null;default:0" json:"quotaTotalBytes"` // 节点上报的配额总量（来自节点 .env，显示/预警用）
	LastReportAt    int64 `gorm:"not null;default:0;index" json:"lastReportAt"` // 最后一次成功上报 Unix 秒（离线派生）

	// 预警去重（与 Epoch 比对；!= 才发，发后置当前 epoch）
	Warn70SentEpoch       int64 `gorm:"not null;default:0" json:"-"`
	Warn80SentEpoch       int64 `gorm:"not null;default:0" json:"-"`
	Warn90SentEpoch       int64 `gorm:"not null;default:0" json:"-"`
	Exhausted100SentEpoch int64 `gorm:"not null;default:0" json:"-"`
}
```

### 3.2 `CloudInstance`（回归本职）

- `TrafficUsedBytes / TrafficTotalBytes / TrafficResetAt / TrafficEpoch / Warn*SentEpoch` 字段**保留不删**（避免破坏性迁移；provider sync `worker_cloud.go` 仍写它们用于 admin cloud 视图 / provider 账单参考）。
- 这些字段对**专属线路的计量与掐断不再有任何权威性** —— 它们对私有节点降级为"provider 同步来的参考值"。一次性清理（删列）留待后续迁移，不在本次范围。

### 3.3 不变量

- **I-Quota**：节点 `.env` 配额是掐断唯一权威；Center 的任何拷贝（PrivateNodeUsage / CloudInstance）都不参与掐断。
- **I-Bundle**（成本天花板，沿用 provisioning 铁律#7）：VPS bundle 含量 ≥ 卖出配额 + headroom。即使 sidecar 整个失效、k2s 裸奔，最坏只是超卖亏毛利，不爆 provider 账单。
- **I-Report**：节点掐断后**仍持续上报**（sidecar 与 k2s 是不同容器，pause k2s 不影响 sidecar 心跳）→ Center 不会把"被掐的活节点"误判 offline。

---

## 4. 节点侧改动（`docker/sidecar/`）

> **关键事实（已读代码 2026-06-19）：节点本地的配额 + 月度时钟早已存在，不要重新发明。**
> sidecar 现在有**两条并行计量路径**，都读宿主 NIC：
> 1. **`Collector` + `TrafficMonitor`**（`collector.go`/`traffic.go`，`main.go:159` **所有节点无条件**启动）：吃 `K2_NODE_BILLING_START_DATE`（yyyy-MM-dd，运维已可设，已接进 compose/entrypoint/demo.env）+ `K2_NODE_TRAFFIC_LIMIT_GB`。`TrafficMonitor.calculateNextCycleEnd/checkAndResetCycle` **已经做按 day-of-month 的月度周期重置**，`GetTrafficStats()` 已返回 `{BillingCycleEndAt, MonthlyTrafficLimitBytes, UsedTrafficBytes}`。当前仅用于 health 上报，**不参与掐断**。
> 2. **`UsageReporter` + `Enforcer` + `HostNICMeter`**（私有节点 `PrivateClaim!=""` 才启）：较新的掐断路径，enforcer 靠 Center `SetQuota` 下发配额。**独立的第二个 NIC 读取器。**
>
> 所以本次节点侧改动的本质是 **统一 + 权威翻转**：让 enforcer 的掐断从已存在的 `TrafficMonitor` 本地配额/周期出发（而不是 Center），并消除两条路径的 NIC 读取重复。**不新增任何 epoch-anchor env。**

### 4.1 enforcer 改吃本地 `TrafficMonitor`（权威翻转）

- `enforcer` 不再消费 Center 回包的 `QuotaTotal/QuotaUsed/EpochID`，也不再需要独立的 `HostNICMeter`。
- 掐断判据改为读 `TrafficMonitor.GetTrafficStats()`：`MonthlyTrafficLimitBytes > 0 && UsedTrafficBytes >= MonthlyTrafficLimitBytes` → `docker pause k2s`；反之 unpause。沿用现有 5s reconcile + `cutoff.state` 持久化（重启重放掐断）。
- 删除 `usage_reporter → enforcer` 的 `SetQuota`/`quotaSink` 链路。让 private 节点的 enforcer + reporter **共用 Collector 已建的同一个 `TrafficMonitor` 实例**（单一 NIC 读取源，杜绝"enforcer 看到的"与"上报的"漂移）。

### 4.2 月度重置时钟 = 复用现成的 `K2_NODE_BILLING_START_DATE`

- **不新增 env。** 节点本地月度周期已由 `TrafficMonitor`（`billing_start_date` 锚点 + day-of-month 重置）拥有，运维通过 `K2_NODE_BILLING_START_DATE` 管理 —— 正是用户说的"时钟归节点 + 运维的工作"。
- epoch 身份 = 计费周期标识，直接用 `BillingCycleEndAt`（Unix 秒，跨周期单调递增）当 epoch id 上报；Center 见更大值即采纳为新 epoch 并清零（§5.1）。周期切换时 `TrafficMonitor` 已自动 rebaseline（`checkAndResetCycle`）。
- **provisioning 须确保写入 `K2_NODE_BILLING_START_DATE`**（当前 skill 只必写 `K2_NODE_TRAFFIC_LIMIT_GB`；缺 billing date 时 `TrafficMonitor` 会报错不启用——见 §8 边界）。

### 4.3 上报体扩字段

`usage_reporter` 上报 `NodeUsageRequest` 增加 `quota_total_bytes`，并用 `TrafficMonitor` 的周期标识填 `epoch_id`：

```go
type NodeUsageRequest struct {
	EpochID         int64 `json:"epoch_id"`          // = TrafficMonitor.BillingCycleEndAt（节点本地周期标识）
	CumulativeBytes int64 `json:"cumulative_bytes"`  // = TrafficMonitor.UsedTrafficBytes（当前周期内已用）
	QuotaTotalBytes int64 `json:"quota_total_bytes"` // = TrafficMonitor.MonthlyTrafficLimitBytes（新增）
	Seq             int64 `json:"seq"`
	Ts              int64 `json:"ts"`
}
```

- 响应只需 ack（`next_report_interval`），reporter 不再依赖响应里的配额/裁决/epoch。
- 注意：JSON tag 必须与 Center `NodeUsageRequest` 逐字一致（CLAUDE.md 跨层约定）。

### 4.4 重启不丢周期内用量（已存在的隐患，须一并修）

`TrafficMonitor` 启动时把 `cycleStartBytes` 重锚到"当前 NIC 累计"（`traffic.go:55-56`）→ **容器重启会把本周期已用清零**（宿主 NIC 计数虽持久，但 cycleStartBytes 被重置）。一旦 enforcer 以它为权威掐断，这个重启清零会让"已用"缩水、掐断点漂移。须把 `cycleStartBytes` + 当前周期标识持久化（与 `cutoff.state` 同机制），重启时恢复而非重锚。**这是把 TrafficMonitor 提升为掐断权威的前置修复。**

---

## 5. Center 侧改动（`api/`）

### 5.1 `slave_api_usage.go` → 纯记录器

```
node := ReqSlaveNode(c)                       // 已认证（ipv4:secret）
subID := node.PrivateSubID                    // nil（shared / private-unowned）→ ack serve，不计量
upsert PrivateNodeUsage by SubID:
  if req.EpochID > usage.Epoch:               // 节点进入新周期 → 跟随
      usage.Epoch = req.EpochID; usage.UsedBytes = req.CumulativeBytes
  else if req.EpochID == usage.Epoch:
      usage.UsedBytes = max(usage.UsedBytes, req.CumulativeBytes)
  // req.EpochID < usage.Epoch（乱序/陈旧）→ 不动 used
  usage.QuotaTotalBytes = req.QuotaTotalBytes // 永远采纳 node-sourced
  usage.LastReportAt = now
响应：{ next_report_interval }                 // 不再回 verdict / quota / epoch
```

**删除**：verdict 计算、lazy epoch reset（Center 不再 author reset）、`trafficStopThreshold*` 常量、Center bump epoch、按 IP 撞 `CloudInstance` 的整段。`NodeUsageResponse` 瘦身为 ack（保留 envelope 形状）。

### 5.2 可见性 / 耗尽派生改读 `PrivateNodeUsage`

- `logic_tunnel_score.go isPrivateTunnelExhausted` 签名从 `(*CloudInstance)` 改为 `(*PrivateNodeUsage)`（或新增按 SubID 解析的 helper），判据 `UsedBytes >= QuotaTotalBytes`。
- 新增 `isPrivateNodeOffline(usage, now)`：`now - LastReportAt > offlineThreshold`（建议 300s，常量可调）。
- `entitlement_resolver.go:70`：解析私有线路时，按 sub 取其 `PrivateNodeUsage`，耗尽**或** offline → 从 `/api/subs` 剔除/标注。

### 5.3 预警 worker 改读 `PrivateNodeUsage`

- `worker_private_node_traffic_warning.go`：扫 active 线路，读其 `PrivateNodeUsage`（`UsedBytes/QuotaTotalBytes` + `Warn*SentEpoch`，去重键 = `Epoch`）。`worker_integration.go` cron 不变（这是发邮件，不是判掐断）。

### 5.4 用户面板 / DTO

- `api_user_private_node.go`：`TrafficUsedBytes` 改取 `PrivateNodeUsage.UsedBytes`；耗尽判断改 `UsedBytes >= QuotaTotalBytes`（去掉 95% 常量依赖）。
- `type.go:594-595` DataPrivateNodeSub 注释「来自 CloudInstance」→「来自 PrivateNodeUsage」。

### 5.5 provisioning 链路

- `slave_api_node.go linkCloudInstanceQuota`（写 sold quota 到 CloudInstance）**不再为掐断服务**，可保留为 informational 或删除；其 best-effort 失败已无成本后果（#16 reconciler 因此消亡）。卖出配额经 NodeOperation `params.trafficTotalBytes` → provisioning agent 写入节点 `.env`（已是 skill 既定流程）。

### 5.6 admin cloud 视图

- `api_admin_cloud.go` 保持读 `CloudInstance`（provider 同步视图，shared+private 通用）。专属线路的权威用量经 `PrivateNodeUsage`（user 面板 / 专属 admin 视图）呈现。**边界明确：admin cloud = provider 视角；PrivateNodeUsage = 节点权威视角。** 不在本次合并两者。

---

## 6. 迁移

1. `AutoMigrate` 建 `private_node_usages` 表（纯增量，无破坏）。
2. **无需 backfill**：节点是配额 + epoch 权威，下一个上报周期（≤60s）即自动写满镜像。可选一次性从 CloudInstance 拷 `Warn*SentEpoch` 以避免重发预警（cosmetic，非必须）。
3. `CloudInstance` traffic 列保留（§3.2）。
4. 部署序：Center 先上（能收新字段、建表、纯记录器对旧 sidecar 仍兼容——旧 sidecar 不发 `quota_total_bytes` 时镜像 QuotaTotalBytes=0 → 不耗尽、不误掐，安全）→ 再铺新 sidecar（#76）。

### 旧/新 sidecar 兼容

- **旧 sidecar + 新 Center**：旧 sidecar 仍按 Center 回包掐断，但新 Center 回包不再带配额 → 旧 enforcer `haveQuota` 永不为真 → **旧节点不掐断**。⚠️ 因此新 Center 上线后，私有节点掐断依赖新 sidecar 铺开（#76）。过渡期成本由 I-Bundle 兜底。**部署 runbook 必须显式写明这一过渡窗口。**
- **新 sidecar + 旧 Center**：新 sidecar 本地掐断（不依赖 Center），上报多带的 `quota_total_bytes` 被旧 Center 忽略 → 安全。

---

## 7. #16 处置

- **reconciler/告警半 → 关闭（设计上消亡）**：Center 不再持有权威配额，无漂移可对账。
- **deprovision 清 `bound_ipv4`/绑定半 → 已完成**（main `c6c40780`），归属 P0 身份单一权威（#77），保留。
- 任务板：#16 标记 closed（superseded by 本 spec）。

---

## 8. 边界与残留风险

| 场景 | 行为 | 兜底 |
|---|---|---|
| sidecar 单独挂、k2s 没挂 | 路由器直连 k2s:443 仍可用，但**无掐断**；Center 无上报 → 读时判 offline（但节点其实在服务） | I-Bundle：超卖亏毛利，不爆账单。自计量固有代价，用户已接受 node-authority |
| 节点上报中断（网络/Center 抖） | 节点本地继续掐断（不依赖 Center）；Center 镜像陈旧 → 可能误判 offline | offline 仅影响可见性提示，不影响节点真实服务；恢复上报即自愈 |
| 上报乱序/重复 | epoch 内取 max，跨 epoch 跟随更大 epoch | 幂等 |
| 时钟漂移（节点本地月界 vs 计费周期） | 节点按 `K2_NODE_BILLING_START_DATE` 算，运维负责锚点正确 | 运维职责；锚点错只影响重置时点，不影响总量掐断 |
| 缺 `K2_NODE_BILLING_START_DATE` | `NewTrafficMonitor` 报错（`billingStartDate is required`）→ Collector 计量不启用 → enforcer 无本地权威 → **私有节点不掐断** | provisioning 必写该 env（§4.2）；部署前校验。否则退化成 I-Bundle 兜底 |
| 配额改档（升/降级线路） | 节点 `.env` 配额固定；MVP 不支持原地升级（[[#18]] defer） | 改档 = 重开线/客服改 `.env` 重启，与既定 MVP 一致 |

---

## 9. 测试策略（TDD）

- **节点侧**（`docker/sidecar` 单元）：enforcer 读 `TrafficMonitor` 掐断（`used>=limit`，不再依赖 Center）；周期切换触发 rebaseline；**重启恢复 `cycleStartBytes` 不清零**（§4.4 回归测）；上报体含 `quota_total_bytes` + 周期标识 epoch。
- **Center 侧**（集成，真 dev MySQL，`skipIfNoConfig`）：
  - `/slave/usage`：private 节点 upsert PrivateNodeUsage（epoch 取 max / 跟随新 epoch / 采纳 quota_total / 记 last_report_at）；shared/private-unowned → ack 不计量。
  - `isPrivateTunnelExhausted(PrivateNodeUsage)` + `isPrivateNodeOffline`：耗尽/离线派生。
  - `entitlement_resolver`：耗尽或离线 → 剔除/标注。
  - 预警 worker：读 PrivateNodeUsage，去重键 = Epoch。
  - 旧 sidecar 兼容：不带 quota_total_bytes → QuotaTotalBytes=0 → 不耗尽不误掐。
- 整数算术（字节 * 百分比不溢出 int64）沿用现有约定。

---

## 10. 文件清单

**节点侧** `docker/sidecar/`
- 改：`sidecar/enforcer.go`（改吃 `TrafficMonitor` 本地配额/周期，删 `SetQuota`/`HostNICMeter` 依赖）、`sidecar/usage_reporter.go`（上报扩 `quota_total_bytes` + 用周期标识填 epoch、不消费回包配额）、`sidecar/traffic.go`（持久化 `cycleStartBytes`+周期标识，重启恢复——§4.4）、`main.go`（让 private 节点 enforcer+reporter 共用 Collector 的 `TrafficMonitor` 实例）。
- **不新增 env**（复用 `K2_NODE_BILLING_START_DATE` + `K2_NODE_TRAFFIC_LIMIT_GB`）。可能删：`sidecar/host_nic.go`（若 NIC 读取统一到 `TrafficMonitor`）、reporter 的 `quotaSink` 接口。
- provisioning skill：确保 `.env` 必写 `K2_NODE_BILLING_START_DATE`（§4.2）。

**Center 侧** `api/`
- 建：`model_private_node.go`（+`PrivateNodeUsage`）。
- 改：`slave_api_usage.go`（纯记录器）、`logic_tunnel_score.go`（耗尽/离线改读 usage）、`entitlement_resolver.go`、`worker_private_node_traffic_warning.go`、`api_user_private_node.go`、`type.go`（注释/来源）、`slave_api_node.go`（linkCloudInstanceQuota 降级，可选）。
- 迁移：`AutoMigrate` 注册 `PrivateNodeUsage`。

**不动**：k2 submodule；`CloudInstance` 结构（列保留）；`worker_cloud.go` provider sync；`api_admin_cloud.go`。
