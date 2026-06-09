# 专属节点路由器产品设计（Private Node / Dedicated VPS Router）

- **日期**: 2026-06-09
- **状态**: 设计待评审
- **作者**: 与 David 协作 brainstorming 产出
- **关联**: 承接现有 k2r 路由器、k2subs 订阅、cloudprovider 多云管理、订阅 entitlement 体系

---

## 1. 背景与问题

### 1.1 起点

路由器（k2r）当前作为共享池的一个客户端形态运行。市场侧希望把路由器**重新定位为"定制线路"产品**：用户购买一台独立 VPS 后，该 VPS 成为 ta 专属的 k2s 节点，路由器接入它。这样路由器不再消耗共享池资源。

### 1.2 核心担心（必须被设计正面回答）

路由器是 **always-on** 设备，覆盖全家所有设备（含电视、流媒体盒子）。一个家庭用路由器天天看 Netflix 4K，单月流量可达数 TB。如果不加约束：

- 在**计量出站流量的云**（如 AWS 标准 EC2 egress ≈ $0.09/GB）上，单台机器 3TB/月 ≈ $260/月 egress 账单 —— 专属节点不但没解决流量成本，反而让它**更严重**。
- 在共享池模型里，这种用户会拖垮所有人。

**结论**：专属节点产品成立的前提是 **流量在供应商侧固定计费 + 客户端侧硬上限断流**。本设计将其作为不可协商的非功能约束（见 §9）。

### 1.3 两类目标客户

| 客户 | 场景 | 流量画像 | IP 诉求 |
|------|------|---------|---------|
| **个人/家庭** | 路由器覆盖全家设备 | 高（流媒体），必须封顶 | 干净可用即可 |
| **企业（TikTok 出海等）** | 多台设备做账号运营/内容发布，按 SSID 走不同国家 IP | 中低（10–50GB/台/月） | **住宅 IP + IP 稳定性是核心卖点** |

---

## 2. 产品模型

### 2.1 两条独立产品线，可共存

| | App 订阅 | 专属节点订阅 |
|---|---|---|
| 入口设备 | 手机 / 桌面 App | 路由器（k2r） |
| 连接对象 | **共享节点池** | **用户自己的专属 VPS** |
| 计费 | 1/2/3/5 年（沿用现有 `Plan.Month`） | 一台主机 × 1/2/3/5 年 |
| 互相关系 | **独立，可同时持有，互不影响** | 同左 |

**两个产品不互斥**：用户可以同时是 App 订阅用户和专属节点用户。它们服务不同设备、不同场景，无交叉。

### 2.2 能力矩阵（产品的硬规则）

| | 共享节点 | 专属节点 |
|---|---|---|
| **App（手机/桌面）** | ✅ | ❌ |
| **路由器（k2r）** | ❌ | ✅ |

- App 永远只用共享节点。
- 路由器永远只用专属节点。
- 这是本期的产品决策，**实现上落成一处可改的策略表**（见 §5.1），将来若要放开（如允许 App 连专属）只改一个 switch。

### 2.3 词汇约定（单一权威）

| 面向用户文案 | 内部代码/API | 含义 |
|---|---|---|
| **共享节点** | `shared` | 共享服务器池（现有产品） |
| **专属节点** | `private` | 用户独占的 VPS 节点（新产品） |

- 中文用户面向场景一律"专属节点 / 共享节点"，**禁止裸词 "Kaitu"，使用"开途"**（遵循品牌规则）。
- 住宅/非住宅是专属节点的 **provider 属性**（`IPType`），不是独立产品名；企业版 = 购买了多台专属节点的用户，不单设产品层级。

---

## 3. 范围与分期

### 3.1 In Scope（本设计覆盖）

- 专属节点订阅的领域模型、购买、自动开通、生命周期、断流、宽限期
- Center 侧能力矩阵落地（单一 resolver + 集中授权）
- k2subs 凭证复用 + 硬化（长效化、宽限期）
- 专属 VPS 的 k2s "只认主人"授权
- cloudprovider 健壮化以支撑住宅/非住宅 provider

### 3.2 分期

| Phase | 内容 | 依赖 |
|-------|------|------|
| **Phase 0** | 领域模型 + DB（`PrivateNodeSubscription` 独立表，零耦合现有 subscription） | 无 |
| **Phase 1 (MVP)** | 购买 → Asynq 异步开通（预构建镜像）→ 路由器接入单专属节点 → 2TB/95% 断流 → 能力矩阵 resolver + k2s owner 授权 + 凭证硬化。**仅非住宅 + 包月流量 provider（Lightsail）** | Phase 0 |
| **Phase 2** | 住宅 IP provider 接入 + 弹性 IP（实例替换 IP 不变） + 企业运维能力 | Phase 1 |
| **Phase 3** | 多 SSID 多节点（每 SSID 绑定一个国家节点，SSID 名 = 国家名）+ 节点健康自愈 | Phase 2 |

### 3.3 Out of Scope（明确不做）

- App 连接专属节点（产品决策排除）
- 专属节点的多用户共享（家庭设备走路由器即可，无需账号）
- 共享池现有逻辑的改动（新产品零侵入）
- 订阅 entitlement dual-clock 重构（独立工作流，本产品不踩入）

---

## 4. 领域模型与数据库

### 4.1 三层对象解耦（商业 / 基础设施 / 隧道）

```
PrivateNodeSubscription   →   CloudInstance   →   SlaveNode
  （商业对象）                  （基础设施对象）       （k2s 隧道）
  user 付了 N 年               一台 VPS            一个可连接的 k2v5 节点
  region / ip_type            可被替换/迁移/自愈     class=private + owner
  生命周期 + 宽限期            已有流量字段          能力矩阵的最小单元
```

**关键**：`CloudInstance` 与 `SlaveNode` 已通过 `IPAddress` 关联（现有 query-time join）。VPS 故障/迁移时，可换一台 `CloudInstance` 而**不动** `PrivateNodeSubscription`（计费不变），换实例后重新绑 `SlaveNode`。这就是"实例可坏可换，订阅不动"的解耦。

### 4.2 新增表 `PrivateNodeSubscription`

与现有 `Subscription` 表（Apple/Stripe 续订）和 `User.ExpiredAt`（共享池会员）**零耦合**——独立表、独立生命周期、独立时钟。

```go
// api/entitlement/private_node.go
type PrivateNodeSubscription struct {
    ID        uint64    `gorm:"primarykey" json:"id"`
    CreatedAt time.Time `json:"createdAt"`
    UpdatedAt time.Time `json:"updatedAt"`

    // 归属
    UserID uint64 `gorm:"not null;index" json:"userId"`        // 主人
    PlanID uint64 `gorm:"not null;index" json:"planId"`        // 专属节点套餐（Plan.Kind=private_node）
    OrderID uint64 `gorm:"index" json:"orderId"`               // 触发开通的订单

    // 基础设施绑定（开通后回填）
    CloudInstanceID *uint64 `gorm:"index" json:"cloudInstanceId"` // → CloudInstance.ID，开通中为 NULL
    SlaveNodeID     *uint64 `gorm:"index" json:"slaveNodeId"`     // → SlaveNode.ID，注册后回填

    // 购买时选择 / 套餐属性
    Region            string `gorm:"type:varchar(50);not null" json:"region"`
    IPType            string `gorm:"type:varchar(20);not null" json:"ipType"`           // residential | non_residential
    TrafficTotalBytes int64  `gorm:"not null" json:"trafficTotalBytes"`                 // 流量配额，如 2TB

    // 生命周期（独立时钟，不碰 User.ExpiredAt）
    Status       string `gorm:"type:varchar(20);not null;index" json:"status"` // 见 §6.1 状态机
    PurchasedAt  int64  `gorm:"not null" json:"purchasedAt"`
    ExpiresAt    int64  `gorm:"not null;index" json:"expiresAt"`               // 订阅期满（Unix 秒）
    GraceUntil   int64  `gorm:"not null;default:0" json:"graceUntil"`          // 宽限期末（路由器仍可用）
    SuspendUntil int64  `gorm:"not null;default:0" json:"suspendUntil"`        // 停机保 IP 期末

    // 开通可观测
    ProvisionAttempts int    `gorm:"not null;default:0" json:"provisionAttempts"`
    LastProvisionError string `gorm:"type:text" json:"-"`
}
```

### 4.3 `SlaveNode` 增量字段（隧道侧标识）

`SlaveNode` 新增节点类别与归属，使能力矩阵和授权有最小判定单元：

```go
// SlaveNode 增量
Class            string  `gorm:"type:varchar(20);not null;default:'shared';index"` // shared | private
PrivateOwnerUserID *uint64 `gorm:"index"`                                          // class=private 时 = 主人 UserID
PrivateSubID       *uint64 `gorm:"index"`                                          // → PrivateNodeSubscription.ID
```

- 现有共享节点全部 `Class='shared'`，零迁移影响（默认值即正确）。
- 专属节点在开通注册时写入 `Class='private'` + owner。

### 4.4 `Plan` 增量字段（区分产品线）

```go
// Plan 增量
Kind string `gorm:"type:varchar(20);not null;default:'shared_subscription';index"` // shared_subscription | private_node
```

专属节点套餐额外的开通参数（region 池、ip_type、provider、bundle、流量配额）放一张轻量配置表 `PrivateNodePlanSpec`，避免把开通细节塞进通用 `Plan`：

```go
type PrivateNodePlanSpec struct {
    ID                uint64
    PlanID            uint64 `gorm:"uniqueIndex"`  // → Plan.ID (Kind=private_node)
    Provider          string // aws_lightsail | bandwagon | ...（住宅 provider 待 Phase 2）
    IPType            string // residential | non_residential
    AllowedRegions    string `gorm:"type:text"`    // JSON: 可选地区列表
    ImageID           string // 预构建镜像（含 k2s）
    BundleID          string // provider 实例规格
    TrafficTotalBytes int64  // 流量配额，如 2TB = 2*1024^4
}
```

> **复用注意**：`Plan.Tier` 已有 `family`/`business` 且 `MaxRouterDevice` 配额已存在。专属节点是否复用 tier 配额，还是完全走 `Kind=private_node` 旁路，见 §13 待决问题 Q1。

---

## 5. 架构：两个产品、一套代码、保持清晰

核心原则：**能力矩阵是数据（集中的策略函数），不是散落的 `if isGateway` 条件分支。**

### 5.1 Center 侧 —— 单一 `NodeAccessResolver`

`/api/subs`（路由器）与 `/api/tunnels`（App）都收敛到同一个 resolver。能力矩阵在这里显式表达一次：

```go
// api/entitlement/resolver.go
// 节点访问的唯一决策点 —— 能力矩阵在此，端点不再各自判断
func (r *NodeAccessResolver) Resolve(dev *Device, user *User) (NodeSet, error) {
    if dev.IsGateway {
        // 路由器 → 只能访问"自己的专属节点"
        sub, err := r.privateNodes.ActiveForUser(user.ID)
        if err != nil || sub == nil {
            return nil, ErrNoPrivateNodeEntitlement   // → 402 / 引导购买
        }
        if !sub.IsServiceable() {                     // active 或 grace（见 §6）
            return nil, ErrPrivateNodeNotServiceable  // → 402 + 宽限期文案
        }
        return r.privateNodes.NodesFor(sub), nil       // 该用户的专属 SlaveNode(s)
    }
    // App → 永远共享池
    if user.IsExpired() {
        return nil, ErrNoSharedEntitlement            // → 402（现有行为）
    }
    return r.sharedPool.All(), nil
}
```

端点变薄：`/api/subs` / `/api/tunnels` 只负责 **鉴权 → 调 Resolve → 注入凭证 → 序列化**。矩阵将来要改，只动这一个函数。

### 5.2 k2s 侧 —— 集中授权 `AuthorizeNodeAccess`（封装重点）

共享池 k2s 与专属 VPS k2s 跑**同一个二进制，不分叉**。差异只在节点自报的身份。授权差异集中在 Center 的**一个函数**：

```go
// api/entitlement/authorizer.go
// k2s 保持"哑"：只转发节点身份 + 用户凭证。
// Center 是唯一知道"这个节点是共享还是专属"的地方。
func AuthorizeNodeAccess(user *User, node *SlaveNode) bool {
    switch node.Class {
    case NodeShared:
        return !user.IsExpired()                          // 任意有效共享订阅用户
    case NodePrivate:
        return node.PrivateOwnerUserID != nil &&
            *node.PrivateOwnerUserID == user.ID &&        // 只认主人
            privateSubServiceable(node.PrivateSubID)      // 且订阅 active/grace
    }
    return false
}
```

**封装收益**：
- k2s 二进制统一，无分叉；共享/专属只是配置里 `Class` 与 `host_id` 不同。
- "专属节点只认主人"这条规则**只存在于一处**，审计/测试/修改指向单一函数。
- 节点身份：开通时向 VPS 的 k2s 配置注入 `node_id`（+ class、host_id、node_secret）。k2s 回调 Center 鉴权时携带 → Center 映射 `node → class → policy`。

> **集成点待确认**：现有 k2v5 的 k2s→Center 鉴权回调的确切机制（见 §13 Q2）。本设计规定 *required behavior*，确切接线在 plan 阶段锁定。

### 5.3 领域模型隔离 —— 两个产品是两个一等公民

```
api/entitlement/
  shared_subscription.go   // 现有 App 订阅（共享池）—— 尽量不动
  private_node.go          // 新：PrivateNodeSubscription + 生命周期
  resolver.go              // §5.1 NodeAccessResolver（组合两者）
  authorizer.go            // §5.2 AuthorizeNodeAccess
  provision.go             // §7 开通编排
```

`PrivateNodeSubscription` 与现有 `subscription` 表、`User.ExpiredAt` **零耦合**——这点尤其关键，因为订阅 entitlement 体系正在经历 dual-clock 重构，新产品绝不能踩进那摊正在动手术的逻辑。

---

## 6. 生命周期与宽限期（PM 设计）

路由器无人值守，存在**两个独立的"过期"事件**，必须分开处理。

### 6.1 订阅期满状态机（商业问题）

```
                         ┌──────────── 续费（任意阶段）─────────────┐
                         ▼                                          │
pending → provisioning → active ──期满──> grace ──未续──> suspended ──未续──> deprovisioned
              │  失败×3                  (7天)              (14天)            (终态)
              ▼                       路由器仍可用        VPS停机/保EIP        VPS销毁/释放IP
            failed                                       路由器断连           续费=全新节点
          (告警admin)
```

| 状态 | 路由器可用 | VPS | IP | 说明 |
|------|-----------|-----|-----|------|
| `pending` | ❌ | 未创建 | — | 订单已付，待入队 |
| `provisioning` | ❌ | 创建中 | — | Asynq 开通中 |
| `active` | ✅ | 运行 | 持有 | 正常服务 |
| `grace` | ✅ | 运行 | 持有 | 期满后 7 天缓冲，每日提醒，路由器照常 |
| `suspended` | ❌ | **停机** | **EIP 保留** | 宽限结束后 14 天，续费即恢复**同一 IP**（保 B2B 账号） |
| `deprovisioned` | ❌ | 销毁 | 释放 | 终态，续费 = 全新节点（新 IP） |
| `failed` | ❌ | 无/残留 | — | 开通失败，人工介入或退款 |

**续费提醒**（复用现有 Asynq cron `TaskTypeRenewalReminder`，每日北京 10:30）：
- 期满前 T-30 / T-14 / T-7 / T-3 / T-1 天分级提醒（年付客户给足前置量）
- 宽限期内每日提醒
- 停机期内提醒"IP 即将释放，续费可保留"

**B2B IP 连续性**：`suspended` 阶段保留弹性 IP（Phase 2 EIP 能力），续费立即恢复同一 IP。这是企业版"运维做好了"的卖点之一。

### 6.2 Token 过期（技术问题，用户无感）

| 现状缺口 | 设计 |
|---------|------|
| 路由器 k2subs token = 24h JWT 硬过期，背景刷新循环**只刷 tunnel 列表不刷 token** | k2subs 背景循环内**加 token 滚动续期**（用 refresh_token，30天）：每次刷新在过期前重签 access token，路由器永不因 token 到期掉线 |
| gateway 设备无长效 token 路径 | 真正的访问门控移到 **`/api/subs` 的 `PrivateNodeSubscription.Status` 校验** + **k2s 的 owner 校验**；token 只要滚动有效即可，到期与否不再是访问控制的主轴 |

**设计原则**：token 滚动让"连接性"永不因技术过期中断；"该不该让你连"由订阅生命周期（§6.1）和 owner 授权（§5.2）决定。两件事解耦。

---

## 7. 自动开通编排（架构 + 安全）

### 7.1 支付与开通异步解耦

VPS 开通需 30s–5min，**不能阻塞支付回调**。复用现有 WordGate webhook → 订单 → Asynq。

```
用户支付成功
  → WordGate webhook (/api/webhook → handleWordgateOrderPaidEvent)
  → MarkOrderAsPaid 事务内：识别 Plan.Kind
       ├─ shared_subscription → applyOrderToBuyer（现有，扩 User.ExpiredAt）
       └─ private_node        → 创建 PrivateNodeSubscription(status=pending)
                                + Asynq.Enqueue(TaskTypeProvisionPrivateNode)
  → (异步 Worker) handleProvisionPrivateNodeTask
```

### 7.2 开通 Worker（新 Asynq 任务，匹配现有 pattern）

```go
const TaskTypeProvisionPrivateNode = "private_node:provision"
const TaskTypeDeprovisionPrivateNode = "private_node:deprovision"

func handleProvisionPrivateNodeTask(ctx, payload) error {
    sub := loadSub(payload.SubID)

    // 1. 幂等锁 + 状态门：仅 pending 可进入（防 webhook 重复触发）
    if !sub.transitionTo(StatusProvisioning) { return nil /* 已处理，忽略 */ }

    // 2. 调 cloudprovider.CreateInstance（现有接口）
    //    opts: provider/region/imageID(预构建含k2s)/bundleID
    res, err := provider.CreateInstance(ctx, opts)
    if err != nil { return retryOrFail(sub, err) }  // Asynq 重试×3 指数退避

    // 3. 轮询实例 Ready（GetInstanceStatus，带 timeout）
    inst := waitReady(res.InstanceID, timeout=5min)

    // 4. 写 CloudInstance（含 TrafficTotalBytes=2TB）+ 注册 SlaveNode(Class=private, owner)
    //    cloud-init 已在镜像内注入 k2s + node 身份（node_id/host_id/node_secret），无需现场安装
    bindInstance(sub, inst); registerPrivateSlaveNode(sub, inst)

    // 5. status → active；推送 + EDM 通知用户"专属节点已就绪"
    sub.transitionTo(StatusActive); notifyReady(sub)
    return nil
}
```

### 7.3 VPS 初始化：预构建镜像（非现场安装）

| 方式 | 速度 | 可靠性 | 选用 |
|------|------|--------|------|
| 现场 apt install k2s | 3–5min | 依赖外部下载，易失败 | ❌ |
| SSH bootstrap | 中 | 需开放 SSH | ❌ |
| **预构建镜像 + cloud-init 注入身份** | 30–60s | 无外部依赖 | ✅ |

镜像内预装 k2s，cloud-init 只注入一个 config（含 `node_id/host_id/node_secret` + Center 回调地址）。复用现有 `list_cloud_images` / `ImageID`。

### 7.4 开通失败处理

```
失败原因：云 API 错误 / 容量不足 / 超时
  ├─ Asynq 自动重试 3 次（指数退避）
  ├─ 3 次仍失败 → status=failed → 告警 admin（Slack）
  └─ admin：手动重试 或 触发退款流程
```

---

## 8. 安全

| 边界 | 设计 |
|------|------|
| **VPS 网络隔离** | 入站仅开 k2s 协议端口；SSH 不公网暴露，管理走云 console/SSM |
| **节点凭证** | `node_secret` 开通时注入，仅用于 k2s↔Center 回调鉴权；不入日志 |
| **owner 授权** | §5.2 `AuthorizeNodeAccess`：专属节点 k2s 只接受主人，防"猜到 IP + 自带有效 token"蹭用 |
| **防重复开通** | §7.2 状态机：仅 `pending` 可进入 `provisioning`，webhook 重复触发被忽略 |
| **凭证吊销** | 复用 `Device.TokenIssueAt` 重置机制（无需黑名单）；订阅终止时吊销 gateway 设备 |
| **路由器凭证持久化** | 复用 k2subs URL 持久化（`/etc/k2r/state.json`，root 可写，重启自动重连） |
| **k2subs URL 不入日志** | token 字段日志脱敏（现有约定） |

> **关于"加密存库"**：经评审，k2v5 凭证是运行时构造（沿用用户登录体系），**不静态存 DB**。`PrivateNodeSubscription` 只存元数据（region/ip_type/status/expires_at），非敏感。开启云厂商存储卷加密即可，应用层不额外加密（避免无谓复杂度）。真正的安全靠：防重复开通 + 到期销毁 + k2s owner 校验 + 凭证可吊销。

---

## 9. 非功能约束：流量控制（回答 §1.2 核心担心）

**铁律**：专属节点**只能**跑在流量固定计费的供应商上（Phase 1 = AWS Lightsail，含固定流量包）。**禁止**用计量 egress 的标准云实例。

### 9.1 2TB 配额 + 95% 断流

- 购买页明示流量配额（如 **2TB/月**），写入 `PrivateNodePlanSpec.TrafficTotalBytes` → `CloudInstance.TrafficTotalBytes`。
- **复用现有流量同步基础设施**：`sync_cloud_instances` 循环已从 provider（Lightsail `GetInstanceStatus`）同步 `TrafficUsedBytes`。
- 当 `TrafficUsedBytes / TrafficTotalBytes ≥ 95%`：**停止该节点的访问**（k2s 拒绝新连接 / Center 在 resolve 返回不可服务），并推送+EDM 通知用户。
- `TrafficResetAt` 到期重置周期。

### 9.2 断流的用户体验

- 95% 触发：提前通知"本月流量即将用尽"。
- 100% 或断流后：路由器侧明确提示"本月流量已用尽"，而非静默失败（避免用户误判产品故障）。
- 重置或升级套餐后恢复。

---

## 10. Cloud Provider 健壮化（住宅 / 非住宅）

住宅 IP 与非住宅 IP 是**不同的供应链**，不是同一 provider 的开关：AWS 不提供住宅 IP。

- **Phase 1**：仅非住宅（`aws_lightsail`，已实现）。
- **Phase 2**：接入住宅 IP provider，实现现有 `cloudprovider.Provider` 接口；`IPType` 在 `PrivateNodePlanSpec` 与 `PrivateNodeSubscription` 中区分。
- **企业价值 = 运维**：开途替企业把节点的创建/换 IP/健康/续期运维做好（这正是企业版溢价来源）。`ChangeIP`、`GetInstanceStatus`、`sync_cloud_instances` 已具备基础。

本期**重点是把 provider 抽象做健壮**（统一 create/delete/status/changeIP 的错误处理、超时、容量回退），而非堆砌新 provider。

---

## 11. 多 SSID 绑定（Phase 3 方向，本期仅记录设计）

k2r 当前是纯 L3 透明代理，**不碰 WiFi/UCI**。多 SSID 是 Phase 3。

### 11.1 架构

```
SSID "HongKong" → br-hk → VLAN 10 → TPROXY → k2v5://hk-node
SSID "Japan"    → br-jp → VLAN 20 → TPROXY → k2v5://jp-node
```

- 一个 VPS = 一个 IP = 一个国家。所以**多 SSID 多国家 = 多 host = 天然 B2B 功能**，逻辑自洽。
- SSID 命名直接用 IP 国家名（香港 = `HongKong`）。

### 11.2 关键：确定性绑定，不走 subs 加权 Pick

- 共享池的 subs `Pick` 是"挑最优节点"的加权随机。**专属节点要的是确定性映射**：SSID-A 永远 → HK 节点。
- 因此：subs 负责"投递用户全部专属节点列表 + 凭证 + 健康"；**SSID→节点的固定映射放在路由器配置层（UCI / gateway config）**。两种路由模型不可混。
- k2r 需新增：UCI wireless 管理（建/删 SSID）+ 每 SSID 独立网桥/VLAN + 每 VLAN 独立 TPROXY 指向对应出口。
- **硬件约束**：多 SSID + VLAN 隔离 + 策略路由依赖路由器硬件能力，Phase 3 需出硬件兼容矩阵。

---

## 12. 测试策略

| 层 | 测试 |
|----|------|
| `NodeAccessResolver` | 纯函数单测：能力矩阵全格（app×shared ✅、app×private ❌、router×shared ❌、router×private ✅）+ 过期/宽限/不可服务态 |
| `AuthorizeNodeAccess` | 单测：shared 任意有效用户、private 仅主人、非主人拒绝、订阅各状态 |
| 生命周期状态机 | 单测：全状态转移 + 续费从 grace/suspended 恢复 + 失败×3→failed |
| 开通 Worker | 集成测（真 dev MySQL）：幂等（重复 webhook 仅开一台）+ 失败重试 + Ready 轮询超时 |
| 流量断流 | 单测 95% 阈值 + 集成测同步循环触发断流 |
| Provider | 现有 cloudprovider 测套扩展；Lightsail create/status/delete |
| 端到端 smoke | 真机：买 → 开通 → 路由器接入 → 跑流量 → 断流 → 续费恢复（发布前必跑，功能改动封顶 6–7/10 无真机 smoke） |

---

## 13. 待决问题（plan 阶段前需锁定）

- **Q1 — tier 复用**：专属节点是否复用现有 `Plan.Tier`（`family`/`business` 已带 `MaxRouterDevice` 配额），还是完全走 `Kind=private_node` 旁路？倾向旁路（解耦），但需确认与现有 tier 配额展示是否冲突。
- **Q2 — k2s↔Center 鉴权回调机制**：现有 k2v5 的 k2s 如何向 Center 校验用户？`AuthorizeNodeAccess` 的确切接线点需在 plan 阶段读 k2s 源码锁定。
- **Q3 — 弹性 IP**：Lightsail 静态 IP（static IP）在实例停机/替换后保持，确切 API 与配额（Phase 2 起需要）。
- **Q4 — 断流落点**：95% 断流由 k2s 本地执行（更快）还是 Center resolve 拒绝（更集中）？倾向双层：Center 标记 + k2s 本地兜底。
- **Q5 — 镜像构建管线**：含 k2s 的预构建镜像如何随版本更新、各 provider/region 如何分发。

---

## 14. 现有基础 vs 新增工作量

| 模块 | 现状 | 新增 |
|------|------|------|
| cloudprovider 创建/删除/状态/换IP | ✅ 已有（Lightsail/Bandwagon/Aliyun/Tencent） | provider 健壮化；Phase 2 住宅 provider |
| 流量同步（TrafficUsed/Total/Reset） | ✅ `sync_cloud_instances` + CloudInstance 字段 | 95% 断流判定 + 通知 |
| Asynq 任务队列 + cron | ✅ 已有（含 RenewalReminder） | Provision/Deprovision 任务；专属节点续费提醒 |
| 支付 webhook | ✅ WordGate → MarkOrderAsPaid | 按 `Plan.Kind` 分流到专属节点开通 |
| k2subs 凭证（device-bound/可吊销/持久化） | ✅ 已运行（k2r 已用） | token 滚动续期 + gateway 宽限期 |
| 设备类区分 | ✅ `Device.IsGateway` | resolver 据此分流 |
| 能力矩阵 / owner 授权 | ❌ | `NodeAccessResolver` + `AuthorizeNodeAccess` + `SlaveNode.Class/owner` |
| 领域模型 | ❌ | `PrivateNodeSubscription` + `PrivateNodePlanSpec` + `Plan.Kind` |
| 多 SSID（UCI/VLAN/TPROXY） | ❌ | Phase 3 全新 |

---

## 附录 A：关键文件路径（实现指引）

| 组件 | 路径 |
|------|------|
| Plan / CloudInstance / Device / SlaveNode 模型 | `api/model.go` |
| cloudprovider 接口 | `api/cloudprovider/provider.go`；`aws_lightsail.go` 等 |
| 支付 webhook | `api/api_webhook.go`（`handleWordgateOrderPaidEvent`） |
| 订单→会员 | `api/logic_order.go`（`MarkOrderAsPaid`）、`api/logic_member.go`（`applyOrderToBuyer`/`addProExpiredDays`） |
| Asynq 任务 | `api/worker_integration.go` |
| /api/subs | `api/api_subs.go` |
| 鉴权中间件 | `api/middleware.go`；token `api/logic_auth.go` |
| k2subs 解析/刷新 | `k2/config/subscription.go`；`k2/subscription/manager.go`、`resolve.go` |
| 路由器持久化 | `k2/gateway/state.go`（`/etc/k2r/state.json`） |
| 新增 entitlement 包 | `api/entitlement/`（resolver/authorizer/private_node/provision） |
