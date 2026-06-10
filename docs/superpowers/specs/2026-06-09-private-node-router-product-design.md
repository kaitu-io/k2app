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

> **与现有 tier 体系的关系（已定）**：两个产品完全独立，plan 自然完全独立。专属节点 plan 就是 `Kind=private_node` 的独立 plan 行，**不参与共享池的 `Tier` 配额体系**（`Tier`/`MaxRouterDevice` 是共享池概念）。同名"tier/plan"只是词汇巧合，不共享任何逻辑。
>
> 旁注：现有共享池 tier `family`/`business` 带的 `MaxRouterDevice>0` 是新模型之前的遗留（曾设想共享池也能挂路由器）。新能力矩阵下「路由器只用专属节点」，该遗留配额与新模型正交，是否废弃留作独立清理项，不影响本设计。

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

> **2026-06-11 重大修订**：开通的「建机 + 部署」整体**解耦给一个外部 AI agent**（挂 `kaitu-center` MCP 的 Claude Code 实例），由独立 spec `2026-06-11-private-node-agent-provisioning.md` 定义。Center 本模块**只发出运维意图**（一条 `ProvisioningIntent`），对 docker / `provision-node.sh` / compose / SSH **零认知**。Center 的职责到「发出意图」即止。
>
> **历史**（已被本次取代）：① 最初设想「worker 直接注册 SlaveNode」——与代码现实不符（节点经 sidecar 自注册）。② 2026-06-10 改为「provision worker 直接 `CreateInstance` + cloud-init user-data 跑 `docker/` 部署链」——代码实现并 review 过，但 deployment 层不该住在 Center（把云生命周期耦合进了本模块），故塌缩。**节点自注册这一真实事件**始终是激活的权威触发，三版不变。

### 7.1 设计原则：Center 发意图 + MCP 队列 + agent 管理 + 事件驱动激活

三方解耦，各自一等公民：

- **Center（producer）**：付费 → 建 sub(pending) + 生成 claim → 发 `ProvisioningIntent`(queued)。随即撒手，不碰云。
- **AI agent（consumer，独立 spec）**：认领意图 → 建 VPS + SSH 部署 + 注入身份 → 上报。
- **激活（事件驱动）**：节点 sidecar 自注册带 claim → Center 注册端点匹配 → 激活 sub。**自注册是开通成功的唯一权威**；意图 status 只作运维可见性，agent 掉线也不会让 sub 永久卡住（§step⑤ 超时清扫兜底）。

| 关注点 | 谁 | 机制 |
|--------|-----|------|
| 建 sub + claim | Center | 已实现 |
| 发意图 | Center | `ProvisioningIntent` 表 + `emitProvisioningIntent`（替代旧 `CreateInstance` 路径） |
| 队列递交 | Center↔agent | 3 个 MCP 工具：`list_provisioning_intents` / `claim_provisioning_intent`(原子租约) / `report_provisioning` |
| 建机 | agent | 复用现有 `create_cloud_instance` MCP（CloudInstance 仍 Center 托管） |
| 部署 | agent | 独立 spec：`exec_on_node` SSH 跑 `provision-node.sh` + 专属 compose + 注入 `.env` |
| 激活 | Center | claim 自注册匹配（已实现 §5/Task 7） |
| 超时清扫 | Center | provisioning > T 分钟无节点到场 → failed + Slack（已实现） |

### 7.2 流程

```
① 支付成功 → webhook → applyOrderToBuyer 按 Plan.Kind 分流
     ├─ shared_subscription → 现有 addProExpiredDays（扩 User.ExpiredAt）
     └─ private_node        → 建 PrivateNodeSubscription(status=pending,
                                生成 ProvisionClaimToken, 从 spec 快照 region/ip_type/traffic)
                              + enqueue
        幂等：OrderID uniqueIndex（webhook 重复只建一条）

② handleProvisionPrivateNode（薄；不碰云）
     1. 原子门：UPDATE ... SET status=provisioning WHERE id=? AND status IN(pending,provisioning)
     2. emitProvisioningIntent(sub, spec)：写一条 ProvisioningIntent(status=queued)，携带
          spec(region/bundle_id/image_id/compose_variant/k2_version/traffic/ip_type)
        + identity(claim_token/center_url/domain)
        幂等：intent 表对 sub_id 建 uniqueIndex（重试只一条 open 意图）
     3. 返回；sub.status 停 provisioning。激活交给 ④

③ AI agent（独立 spec，挂 kaitu-center MCP）
     a. claim_provisioning_intent → 原子租约(queued→claimed)，拿 spec+identity
     b. create_cloud_instance（现有 MCP）起机 → report_provisioning(instance_id, ipv4)
     c. exec_on_node SSH：provision-node.sh + 专属节点版 compose，写 .env
          (node_secret 由 agent 生成；K2_PRIVATE_CLAIM=claim_token)
     d. 验 k2v5 起 → 等节点自注册

④ 节点启动 → sidecar 注册 `PUT /slave/nodes/:ipv4` 带 K2_PRIVATE_CLAIM
     → 注册端点：claim 非空且匹配 pending/provisioning 的 sub（按 ProvisionClaimToken）
         → 置 node.Class=private + PrivateOwnerUserID=sub.UserID + PrivateSubID=sub.ID
         → sub.SlaveNodeID 回填, status→active
         → 按 IP 匹配 CloudInstance 回填 sub.CloudInstanceID；intent→succeeded
     → **preserve 规则**：节点已存在(secret 匹配)的 delete+recreate 路径，必须保留 Class/owner/PrivateSubID
        （否则每次重启 sidecar 重注册会把节点打回 shared）
     → claim 缺省 → shared（现有行为零改动）

⑤ 超时清扫 cron：provisioning 超过 T 分钟仍无节点到场 → status=failed + Slack；intent 同步 failed（admin 手动重试/退款）
```

### 7.3 部署层：独立 spec，agent 驱动

部署（建机后把 k2s 服务装上）**不在本模块**——Center 发出 `ProvisioningIntent` 即完成职责。agent 侧手册见独立 spec `2026-06-11-private-node-agent-provisioning.md`，要点：

- agent = 挂 `kaitu-center` MCP 的 Claude Code，循环认领 `queued` 意图。
- 建机复用现有 `create_cloud_instance` MCP；部署用 `exec_on_node` SSH 跑 `provision-node.sh` + **专属节点版 compose**（带 §9.3 流量计量断流 sidecar 配置，区别于共享池 compose）。
- 注入 `/apps/kaitu-slave/.env`：`K2_NODE_SECRET`(agent 生成) + `K2_PRIVATE_CLAIM`(=intent.claim_token) + `K2_CENTER_URL` + `K2_DOMAIN`。
- SSH 凭据：agent 凭 `instance_id` 通过云 API 取（如 Lightsail 默认 key），**不经 intent 传输**。
- 失败经 `report_provisioning(error)` 回流；语义见 §7.5。

> 为什么 agent 而非 cloud-init：cloud-init 是「一次性盲注脚本」，失败无重试/无观测/无补救；AI agent 能读 `exec_on_node` 输出、判断失败、重跑、按需调整——开通是低频高价值操作，agent 的可观测与自愈优于固化脚本。预构建镜像（30–60s 冷启动）仍可作 Q5 提速优化，与本架构正交。

### 7.4 claim-token 安全

- `ProvisionClaimToken` = 每个 sub 一个 32 字节随机串，仅 Center（存 sub 行）+ agent（从认领的 intent 读）+ 注入到那一台 VPS 的 `.env` 知道。
- 客户对自有 VPS 有 root 能读自己的 claim——但只能认领**自己那台**（每台唯一 claim），无法认领他人节点，与 §9.3「篡改只影响自己付费带宽」同款逻辑。
- 注册端点对 claim 缺省/不匹配一律按 shared 处理，不报错（防探测）。
- 激活后 claim 仍可复用（重启走 preserve，不必重验）——简化重注册路径。

### 7.5 开通失败处理

```
失败原因：意图无人认领(agent 离线) / 建机失败 / 部署失败 / 节点超时未到场
  ├─ agent 侧瞬时错 → agent 自重试 或 report_provisioning(failed)
  ├─ intent 租约超时未 report → 回 queued 供再认领（lease deadline）
  ├─ sub provisioning > T 分钟无节点到场 → 超时清扫置 failed + Slack（权威闸门，不依赖 agent 自报）
  └─ admin：手动重试（重置 sub→pending 重 enqueue）或 触发退款流程
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

### 9.1 2TB 配额 + 95% 断流（Center 集中裁决）

- 购买页明示流量配额（如 **2TB/月**），写入 `PrivateNodePlanSpec.TrafficTotalBytes` → `CloudInstance.TrafficTotalBytes`。
- **断流裁决集中在 Center**（便于统一控制、调阈值、灰度），但裁决依赖 k2s 提供低延迟可靠的流量计量（§9.3）。
- 当 `TrafficUsedBytes / TrafficTotalBytes ≥ 95%`：Center 标记节点不可服务，并通过 usage heartbeat 响应令 k2s **拒绝新连接**；同时推送+EDM 通知用户。
- `TrafficResetAt` 到期重置周期。

### 9.2 断流的用户体验

- 95% 触发：提前通知"本月流量即将用尽"。
- 100% 或断流后：路由器侧明确提示"本月流量已用尽"，而非静默失败（避免用户误判产品故障）。
- 重置或升级套餐后恢复。

### 9.3 k2s 流量计量与上报方案（断流可靠性的根基）

**为什么不能只靠 provider API**：Lightsail `GetInstanceStatus` 的流量计数**滞后数小时**，且统计的是**整机流量**（含 OS 更新等非 VPN 流量），无法支撑低延迟、精确到 VPN 隧道的断流。因此 k2s 必须自带计量。

#### 计量点（metering）

- 在 k2s 会话层累加每条连接的 rx+tx 字节到**节点级原子计数器**（一台 VPS = 一个专属节点 = 一个计量主体）。
- 热路径只做 `atomic.AddInt64`，无 syscall、无锁，零额外开销。
- 计量对象是**实际中转的代理字节**，即真实 VPN 吞吐，比 provider 的整机口径更准。
- > 集成点：确认 k2s 现有连接级字节计数（K2CC metrics / k2s.log 已有吞吐统计，应可直接接入），plan 阶段锁定。

#### 上报：累计值 + 单一 heartbeat 通道

**累计而非增量**：上报"自 epoch 起的累计字节"，Center 存 `max(已见)`。幂等，对丢包/重复/乱序天然鲁棒（增量模型丢一条 = 永久少算，重复 = 双算，脆弱）。

**Usage heartbeat**（一个通道承载计量 + 断流执行 + epoch 重置 + 节点存活）：

```
k2s ──POST /api/node/usage──> Center
  请求: { node_id, epoch_id, cumulative_bytes, seq, ts }   // node_secret 鉴权（§8）
  响应: { verdict: serve|throttle|stop, epoch_id, quota_total, quota_used,
          epoch_hard_ceiling_bytes, next_report_interval }

  触发: max(next_report_interval(默认60s), 累计增量 ≥ 500MB) 混合触发
  k2s 据 verdict 执行: stop=拒绝新连接(可选 drain 现有), throttle=限速(预留), serve=正常
```

- Center 收到上报 → 更新 `CloudInstance.TrafficUsedBytes` → 算 95% → 回 verdict。断流延迟 ≤ 一个 heartbeat 周期（≤60s）。
- **超冲分析**：60s 内即便 100Mbps 满速也仅 ~750MB，对 2TB 配额 95% 阈值占比 0.04%，可忽略。

#### 计费周期完全由 Center 控制（策略集中）

**Center 是配额/计费周期的唯一权威；k2s 不持有任何策略**——只持有「当前 `epoch_id` + 该 epoch 累计字节」两个状态。

| 谁掌握 | 内容 |
|--------|------|
| **Center** | 配额总量、计费周期与重置时点（`TrafficResetAt` → bump `epoch_id`）、95% 阈值、serve/throttle/stop 裁决、epoch 身份、上报节奏（`next_report_interval`） |
| **k2s** | 诚实计数、上报累计、执行裁决；收到新 `epoch_id` 即清零续计 |

- **重置由 Center 驱动**：k2s 永不按自己时钟重置，只在 heartbeat 响应带新 `epoch_id` 时清零 → 杜绝时钟漂移/重启错位。改周期/配额/阈值/灰度全在 Center，k2s 零改动零感知。
- **分区时 Center 仍控得住**：k2s 连不上 Center 时沿用最后一次 verdict 继续服务+累计；verdict 额外携带 `epoch_hard_ceiling_bytes`，k2s 即便离线也本地强制执行此上限。策略值（上限）仍由 Center 下发，k2s 只是离线执行——控制权不旁落，且 provider API 兜底守住资金硬顶。

#### 持久化（抗重启）

- k2s 每 10s / 优雅退出时把累计值（含 `epoch_id`）落本地盘。
- 重启后同 epoch 内从落盘值**续累计**，不清零。
- 本地盘丢失时，heartbeat 首次响应携带 Center 侧 `quota_used` 作 baseline 恢复。

#### Epoch / 月度重置

- `TrafficResetAt` 到期：Center bump `epoch_id` + 清零 `TrafficUsedBytes`，下一个 heartbeat 响应带**新 epoch_id** → k2s 清零本地计数并续计。`epoch_id` 防止跨周期的旧上报污染新周期。

#### 双信号 defense-in-depth

| 信号 | 角色 | 特性 |
|------|------|------|
| **k2s heartbeat** | 主信号 | 低延迟、精确到 VPN 流量；但用户对自有 VPS 有 root，理论可篡改 k2s 少报 |
| **provider API**（`sync_cloud_instances`） | 兜底 | 滞后、整机口径；但抗篡改、是计费真相 |

- Center 断流 = `min(k2s_reported, provider_reported)` 任一先到 95%。
- 篡改的真实风险很低：用户少报只是多用 **ta 自己付费的固定带宽**，真正硬顶是 Lightsail 流量包 + provider overage 计费，兜底信号守住资金风险。

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

- ~~**Q1 — tier 复用**~~ ✅ **已定**：完全独立。专属节点走 `Kind=private_node` 旁路，不碰共享池 `Tier` 体系（见 §4.4）。
- **Q2 — k2s↔Center 鉴权回调机制**：现有 k2v5 的 k2s 如何向 Center 校验用户？`AuthorizeNodeAccess` 的确切接线点需在 plan 阶段读 k2s 源码锁定。
- **Q3 — 弹性 IP**：Lightsail 静态 IP（static IP）在实例停机/替换后保持，确切 API 与配额（Phase 2 起需要）。
- ~~**Q4 — 断流落点**~~ ✅ **已定**：Center 集中裁决，k2s 提供 usage heartbeat 计量+执行（§9.3）。剩余子项 → Q6。
- **Q6 — k2s 字节计数接入点**：确认 k2s 现有连接级 rx/tx 计数器（K2CC/k2s.log 吞吐统计）能否直接累加到节点级 epoch 计数器，plan 阶段读 k2s 源码锁定。
- **Q5 — 镜像构建管线**：含 k2s 的预构建镜像如何随版本更新、各 provider/region 如何分发。

---

## 14. 现有基础 vs 新增工作量

| 模块 | 现状 | 新增 |
|------|------|------|
| cloudprovider 创建/删除/状态/换IP | ✅ 已有（Lightsail/Bandwagon/Aliyun/Tencent） | provider 健壮化；Phase 2 住宅 provider |
| 流量同步（TrafficUsed/Total/Reset） | ✅ `sync_cloud_instances` + CloudInstance 字段（兜底信号） | k2s usage heartbeat 主信号 + `/api/node/usage` 端点 + 95% 断流判定 + 通知 |
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
