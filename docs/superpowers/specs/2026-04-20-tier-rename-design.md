---
title: Tier Rename + 强约束 + 单一事实源
date: 2026-04-20
status: design — pending review
spec_scope: Spec A (基础设施)
related_docs:
  - 2026-04-20-proxy-purchase-users.md (代付用户调研)
  - 2026-04-08-k2r-router-release-design.md (K2R 路由器，影响 family/business tier 价值)
  - Spec B (TBD): /prices pricing page + 多 tier 商业上线
---

# Tier Rename Design

## 1. 背景与目标

### 现状
- `User.Tier` 和 `Plan.Tier` 默认值是 `'pro'`，无明确档位语义
- `User` / `Plan` 各自存了 `MaxDevice / MaxRouterDevice / MaxLanClient` 配额字段
- 代付（`forUserUUIDs` 字段）功能存在但**零真实付费用户**（见 `2026-04-20-proxy-purchase-users.md`）
- LicenseKey 兑换流程已是"加 N 天会员，不动 tier"的清晰语义

### 目标
1. **重命名 tier**：`pro` → `basic`（默认档），新增 `lite / family / business` 三个档位
2. **配额单一事实源**：删除 User/Plan 的配额字段，配额从 `TierQuotas[user.Tier]` 推导
3. **强约束**：admin 不能在 plan 上微调配额；要变就改全局 Go 常量
4. **删除代付**：API 层拒绝 `forUsers` 字段
5. **零用户感知变化**：本 spec 不动用户购买流程的视觉，只是基础设施替换

### Spec 范围划分
- **本 Spec（A）**：纯技术基础设施，所有用户在迁移后停留在 basic 档，UI 几乎无变化
- **Spec B（后续）**：`/prices` 页面、新建 lite/family/business plans、营销文案 → 商业上线多档位

## 2. 关键设计决策（Q1-Q6 总结）

| # | 问题 | 决策 |
|---|------|------|
| Q1 | tier 配额是强约束还是可覆盖 | **(A) 强约束** —— TierQuotas 单一事实源 |
| Q2 | `pro` 存量数据如何处理 | **(A) 一次性硬迁移** —— UPDATE → 'basic'，删 pro 枚举 |
| Q3 | 用户配额事实源 | **(A) Tier 是事实源** —— 删除 User.MaxDevice 等列 |
| Q4 | TierQuotas 注册表位置 | **(A) Go 代码常量** —— 改配额必须发版 |
| Q5 | 免费/过期用户的 tier | **(A) 没有 free tier** —— 过期用户 ZeroQuota |
| Q6 | 升级/降级语义 | **本 spec 不允许 tier 变更** —— 首次确立后只能 admin 改 |

附加决策：
- **代付**：完全删除（替代方案 LicenseKey 礼品码留给 Spec B 或更后）
- **LicenseKey 现有逻辑**：完全不动，不加 Tier 字段，沿用"加天数不动 tier"语义
- **`/prices` 页面**：本 spec 不做，留给 Spec B

## 3. 架构

### 核心抽象
- **4 个 tier**（lite < basic < family < business）+ 隐式"过期/未付费"状态（配额 0）
- **TierQuotas 注册表**是单一事实源
- **配额查询路径**：
  ```
  quota = TierQuotas[user.Tier]  if user.IsActive()
  quota = ZeroQuota              otherwise
  ```
- **Tier 变更的合法路径**：首次购买（自动）/ Admin 手动改（审计）/ 其他都拒绝

## 4. 数据模型

### `User` 表（`api/model.go`）

| 字段 | 操作 | 说明 |
|------|------|------|
| `Tier` | **修改默认值** | `'pro'` → `'basic'`；CHECK 约束 4 值 |
| `MaxDevice` | **删除** | 改走 TierQuotas |
| `MaxRouterDevice` | **删除** | 同上 |
| `MaxLanClient` | **删除** | 同上 |

### `Plan` 表（`api/model.go`）

| 字段 | 操作 | 说明 |
|------|------|------|
| `Tier` | **修改默认值** | `'pro'` → `'basic'`；CHECK 约束 4 值 |
| `MaxDevice` | **删除** | 配额从 tier 推 |
| `MaxRouterDevice` | **删除** | 同上 |
| `MaxLanClient` | **删除** | 同上 |

### `LicenseKey` 表
**完全不变**。`PlanDays` 字段已是天数快照，与 tier 无关。兑换沿用现有"加天数"逻辑。

### `Order` 表
- `Order` struct 本身**没有** `ForUsers` / `ForMyself` 字段
- 这两个值通过 `Order.Meta` (json 列) 存储，访问器：`order.GetForUsers() []string` / `order.GetForMyself() bool`
- DB `meta` 列**保留**（历史数据可查），新代码不写入这两个 key
- 访问器函数保留（兼容历史订单查询），但应用代码不再调用

### 常量删除
```go
// 删除：
const DefaultMaxDevice = 5
const DefaultMaxRouterDevice = 0
```

## 5. TierQuotas 注册表 + 配额查询 API

### 新文件 `api/tier.go`

```go
package center

const (
    TierLite     = "lite"
    TierBasic    = "basic"
    TierFamily   = "family"
    TierBusiness = "business"
)

type TierQuota struct {
    MaxDevice       int `json:"maxDevice"`
    MaxRouterDevice int `json:"maxRouterDevice"`
    MaxLanClient    int `json:"maxLanClient"` // -1 表示无限
}

type TierInfo struct {
    Name string `json:"name"`
    Rank int    `json:"rank"`
    TierQuota
}

var TierQuotas = map[string]TierInfo{
    TierLite:     {Name: TierLite,     Rank: 1, TierQuota: TierQuota{1,  0, 0}},
    TierBasic:    {Name: TierBasic,    Rank: 2, TierQuota: TierQuota{5,  0, 0}},
    TierFamily:   {Name: TierFamily,   Rank: 3, TierQuota: TierQuota{8,  1, 20}},
    TierBusiness: {Name: TierBusiness, Rank: 4, TierQuota: TierQuota{20, 3, -1}},
}

var ZeroQuota = TierQuota{}

func AllTiers() []TierInfo { /* 按 rank 升序 */ }
func IsValidTier(t string) bool { _, ok := TierQuotas[t]; return ok }
```

### Quota 查询方法（`api/model.go`）

```go
func (u *User) Quota() TierQuota {
    // ExpiredAt 检查，假设 IsPro() 已存在；如不存在则改为 u.ExpiredAt > time.Now().Unix()
    if !u.IsPro() {
        return ZeroQuota
    }
    info, ok := TierQuotas[u.Tier]
    if !ok {
        log.Errorf(ctx, "user %d has invalid tier=%q, falling back to basic", u.ID, u.Tier)
        return TierQuotas[TierBasic].TierQuota
    }
    return info.TierQuota
}

func (p *Plan) Quota() TierQuota { /* 同上风格 */ }
```

### JSON 序列化（`MarshalJSON` 注入旧字段）

`Plan` 和 `User` 的 JSON 输出**保留** `maxDevice / maxRouterDevice / maxLanClient` 字段（webapp v0.4.x 老版本依赖）。来源从 DB 列改为动态计算：

```go
func (p Plan) MarshalJSON() ([]byte, error) {
    type Alias Plan
    quota := p.Quota()
    return json.Marshal(&struct {
        Alias
        MaxDevice       int `json:"maxDevice"`
        MaxRouterDevice int `json:"maxRouterDevice"`
        MaxLanClient    int `json:"maxLanClient"`
    }{ Alias: Alias(p), MaxDevice: quota.MaxDevice, ... })
}
```

## 6. 购买流程

### `applyOrderToTargetUsers`（重命名为 `applyOrderToBuyer`）
```go
func applyOrderToBuyer(ctx, tx, order *Order) error {
    var buyer User
    tx.First(&buyer, order.UserID)

    // Tier 处理：首次购买写入，后续保持
    if buyer.IsFirstOrderDone == nil || !*buyer.IsFirstOrderDone {
        buyer.Tier = order.Plan().Tier
    }
    // 后续购买 tier 不动（API 层已校验匹配）

    days := calcPlanDays(plan)
    addProExpiredDays(ctx, tx, &buyer, VipPurchase, order.ID, days, "订单支付 - "+order.UUID)
    return nil
}
```

### 入口校验（`POST /api/orders`）

handler 层先拒绝代付（在 binding 后立即检查 request body）：

```go
type CreateOrderRequest struct {
    PlanUUID  string   `json:"planUUID" binding:"required"`
    ForUsers  []string `json:"forUsers,omitempty"`  // 仅用于检测老客户端
    ForMyself *bool    `json:"forMyself,omitempty"` // 仅用于检测老客户端
}

func handleCreateOrder(c *gin.Context) {
    var req CreateOrderRequest
    if err := c.ShouldBindJSON(&req); err != nil { ... }

    // 拒绝代付（老客户端兼容性提示）
    if len(req.ForUsers) > 0 || (req.ForMyself != nil && !*req.ForMyself) {
        Error(c, ErrorProxyPurchaseDeprecated,
            "代付款功能已下线，不再支持为他人购买。请让对方使用自己的账号购买。")
        return
    }

    // tier 校验
    if err := validatePurchase(buyer, plan); err != nil { ... }
}

func validatePurchase(buyer *User, plan *Plan) error {
    // 首次购买放行（IsFirstOrderDone 是 *bool，需 nil 判断）
    if buyer.IsFirstOrderDone == nil || !*buyer.IsFirstOrderDone {
        return nil
    }
    // 后续必须 tier 匹配
    if plan.Tier != buyer.Tier {
        return ErrTierMismatch  // HTTP 422
    }
    return nil
}
```

### 错误码（`api/response.go` 新增）
- `ErrorTierMismatch` (422 范畴) — 错误消息：`您当前为「{currentTier}」档，无法购买「{planTier}」档套餐。如需变更档位请联系客服。`
- `ErrorProxyPurchaseDeprecated` (422) — 错误消息：`代付款功能已下线，不再支持为他人购买。请让对方使用自己的账号购买。`

> 注意：`webapp/src/utils/errorCode.ts` 必须同步新增（API 错误码宪法）。

### Admin 改 Tier
- 新 endpoint `PUT /api/admin/users/:id/tier`
- 入参：`{tier: "family", reason: "客服升级，工单 #1234"}`
- 服务端记 `AdminAuditLog`：操作人 / 目标用户 / from→to / reason / 时间
- 不修改 ExpiredAt / IsFirstOrderDone

## 7. API 变更

### 新 endpoint
| Endpoint | 描述 |
|---------|------|
| `GET /api/tiers` | 嵌套结构 `{tiers: [{name, rank, quotas..., plans: [...]}]}` |
| `GET /api/admin/tiers` | 同上 + admin-only 字段 |
| `PUT /api/admin/users/:id/tier` | 修改用户 tier，必走审计 |

### 保留 endpoint（向后兼容）
- `GET /api/plans`：内部委托给 tiers 后 flatten，老客户端零改动

### 老 endpoint 行为变化
- `POST /api/orders` 带 `forUsers` 字段 → HTTP 422 + 友好错误（不再生效）

## 8. 数据迁移

### Schema 变更（单次部署）
- **修改默认值**：`User.Tier` 和 `Plan.Tier` 默认 `'pro'` → `'basic'`
- **删除列**：`User.MaxDevice / MaxRouterDevice / MaxLanClient` 和 `Plan.MaxDevice / MaxRouterDevice / MaxLanClient`

### 数据回填 SQL（`Migrate()` 末尾跑，幂等保护）

**重要**：GORM AutoMigrate **不会修改已有列的 `DEFAULT` 值**。修改 struct tag 的 `default:'basic'` 不够，必须手动 ALTER。

```sql
-- 1. 先改默认值（GORM AutoMigrate 不会做这步）
ALTER TABLE plans MODIFY COLUMN tier VARCHAR(30) NOT NULL DEFAULT 'basic';
ALTER TABLE users MODIFY COLUMN tier VARCHAR(30) NOT NULL DEFAULT 'basic';

-- 2. 回填存量数据
UPDATE plans SET tier='basic' WHERE tier IN ('pro', '') OR tier IS NULL;
UPDATE users SET tier='basic' WHERE tier IN ('pro', '') OR tier IS NULL;
```

幂等：用 `SHOW COLUMNS LIKE 'max_device'` 判断，存在才执行 DROP COLUMN。MODIFY COLUMN 和 UPDATE 本身幂等（重复执行无副作用）。

### 部署阶段（建议分两阶段，DROP 不可逆）

| Phase | 内容 | 时间窗口 |
|-------|------|---------|
| **Phase 1** | 新代码 + 修改默认值 + 数据回填 + 代付 API 返回 422 | 一次发版 |
| **Phase 2** | DROP COLUMN 旧配额列 | Phase 1 之后 ≥1 周 |

如果接受单次部署的风险，也可一次完成。

### 部署前 checklist
- [ ] 全量 DB backup（RDS snapshot）
- [ ] Staging 完整跑通迁移
- [ ] 监控告警阈值（兑换/购买失败率 > 1% 触发）
- [ ] 启动时手动跑两次 Migrate 验证幂等

### 回滚预案
| 故障 | 应对 |
|------|------|
| 数据迁移 SQL 报错 | 回滚代码 + DB backup 恢复 |
| MarshalJSON bug | hotfix |
| Tier 校验误拒 | hotfix；DB 列已删需 backup 恢复 |

## 9. UI 变更（不含 `/prices`）

### Manager 后台（`web/src/app/(manager)/`）

#### Plan 编辑器
- **删除**：`MaxDevice / MaxRouterDevice / MaxLanClient` 输入框；Tier dropdown 中的 `pro`
- **修改 Tier dropdown**：4 选项（lite/basic/family/business），默认 basic
- **新增 Tier 配额预览卡片**（只读，选中 tier 后展示）
- **约束**：admin 可改 plan 的 tier，不能改配额数字

#### User 编辑器
- **新增 Tier 修改操作**：下拉选 4 tier + reason 必填 + 走审计
- **修改 User 详情显示**：MaxDevice 等不再从 DB 取，从 `TierQuotas[user.Tier]` 推导

### Webapp（`webapp/src/pages/Purchase.tsx`）
- **删除**："为他人购买" toggle、Receiver UUID 输入框、`forUserUUIDs` / `forMyself` state
- **Plan 过滤**：`user.tier == plan.tier` 过滤（首次除外）
- **当前用户感知**：所有人 tier='basic'，所有 plans tier='basic' → 视觉零变化

### Web 网站（`web/src/app/[locale]/purchase/PurchaseClient.tsx`）
- 同 Webapp

### `MembershipBenefits.tsx`（已存在）
- 配额数字（如 "5 设备"）改为从 `/api/tiers` 动态读
- 未登录用户显示 basic 配额作为默认

### i18n 文案新增（7 个 locales：zh-CN/zh-HK/en/ja/ru/fa/ar）
- `manager.plans.tierQuotaPreview` — "档位配额预览"
- `manager.plans.tierLockedHint` — "配额由档位决定，如需调整请联系研发"
- `manager.users.tierChange` — "修改档位"
- `manager.users.tierChangeReason` — "变更原因（必填）"
- `purchase.tierLocked` — "您当前为 {tier} 档。如需升级请联系客服。"
- `purchase.proxyPurchaseDeprecated` — "代付款功能已下线，请让对方使用自己的账号购买。"

## 10. 测试策略

### 单元测试（pure function）
- `TestTierQuotas_AllValid` — 4 个 tier 都在 map，rank 严格递增
- `TestIsValidTier` — 合法/非法 tier 校验
- `TestUser_Quota_Active` — active user → 正确配额
- `TestUser_Quota_Expired` — expired user → ZeroQuota
- `TestUser_Quota_InvalidTier` — 脏数据 → 降级 basic + 错误日志（用 testlog.Capture 验证）
- `TestPlan_Quota` — plan tier → 正确配额

### JSON 序列化测试
- `TestPlanMarshalJSON_QuotaInjection` — 序列化包含 maxDevice 等字段
- `TestUserMarshalJSON_QuotaInjection` — 同上
- `TestUserMarshalJSON_ExpiredZeroQuota` — 过期 user 序列化 → 0
- 注意 `&` 转义：用 `map[string]any` 解析后断言

### 购买校验测试（mock DB）
- `TestPurchase_FirstTimeAnyTier` — 任意 tier plan 可买，user.tier 写入 plan.tier
- `TestPurchase_SubsequentSameTier` — tier 匹配 → 通过
- `TestPurchase_SubsequentDifferentTier` — 不匹配 → 422 ErrTierMismatch
- `TestPurchase_RejectForUsers` — 带 `forUsers` → 422 ErrorProxyPurchaseDeprecated
- `TestPurchase_TierUnchangedOnRenewal` — 续费同 tier → user.Tier 字段不变

### API endpoint 测试
- `TestGetTiers_StructureValid` — 4 个 tier 按 rank 排序
- `TestGetTiers_PlansFlatFields` — plan 节点带 maxDevice 等冗余字段
- `TestGetTiers_NoAuth` — 公开 endpoint
- `TestAdminChangeUserTier_AuditLogged` — 改 tier → AdminAuditLog 记录
- `TestAdminChangeUserTier_InvalidTier` — 非法 tier → 422
- `TestAdminChangeUserTier_NoAdmin` — 非 admin → 403

### 兼容性回归测试
- `TestPlanResponse_LegacyFieldsPresent` — JSON 必须有 maxDevice 等
- `TestUserResponse_LegacyFieldsPresent` — 同上
- `TestOldClientWithForUsers_GracefulError` — 老客户端 forUsers 请求 → 422 + 友好错误

### 前端测试（vitest）
- `Purchase.tsx` regression：渲染时不应出现 "为他人购买"
- `Purchase.tsx`：plans 按 user.tier 过滤
- Manager `plans/page.tsx`：tier dropdown 4 选项无 'pro'，无 MaxDevice 输入框

### 不写自动化测试的部分
**数据迁移**：依靠运维 checklist（DB backup + staging dry-run + 启动时手动跑两次验证幂等）+ code review。SQL 改动走二次校验。

### 覆盖率目标
- 新增代码（`tier.go`、`/api/tiers`、`MarshalJSON`、`/api/admin/users/:id/tier`）：**≥ 90% 行覆盖**
- 修改代码（`logic_member.go applyOrderToBuyer`）：**100% 分支覆盖**

## 11. 风险与缓解

| 风险 | 缓解 |
|------|------|
| MarshalJSON bug 导致客户端 quota 显示错乱 | 兼容性回归测试 + 部署后监控客户端字段缺失上报 |
| DROP COLUMN 不可逆 | DB backup + 分两阶段部署（Phase 1 不删列） |
| Tier 校验误拒老用户 | 100% 分支覆盖测试 + 部署后监控 422 拒绝率 |
| 老客户端发 forUsers 报错 | 友好错误消息 + 用户公告（实际无人使用，影响为 0） |
| GORM 默认值修改不生效 | Staging 验证；如不生效手动跑 ALTER COLUMN SET DEFAULT |

## 12. Out of Scope（留给 Spec B）

1. **`/prices` 页面**：tier 比较 + 时长 toggle + CTA
2. **新建 lite/family/business 的 plans**（运营在 manager 后台创建）
3. **营销文案**：每个 tier 的 tagline + value props（7 个 locales × 4 个 tier）
4. **"推荐档位"标记**：basic? family? 商业决策
5. **K2R 路由器与 family/business tier 的产品融合**：family 解锁 1 路由器、business 解锁 3 路由器（需要前端展示 + 路由器配对流程）
6. **代付替代方案**：LicenseKey 礼品码购买流程（如果商业上有需求）

## 13. 后续工作触发

- **路由器潜在用户营销**：见 `2026-04-20-proxy-purchase-users.md` 标注的 4 个用户，K2R 发布时优先触达
- **Spec B 立项时机**：本 spec 上线稳定 + 商业团队确定多档位定价策略后

---

## Appendix A: Tier 配额矩阵

| Tier | Rank | MaxDevice | MaxRouterDevice | MaxLanClient |
|------|------|-----------|-----------------|--------------|
| lite | 1 | 1 | 0 | 0 |
| **basic** ⭐ (默认) | 2 | 5 | 0 | 0 |
| family | 3 | 8 | 1 | 20 |
| business | 4 | 20 | 3 | -1 (无限) |

## Appendix B: Tier 转换路径

```
[新用户 IsFirstOrderDone=false]
  ↓ 首次购买 (任何 tier 的 plan)
[user.Tier = plan.Tier, IsFirstOrderDone=true]
  ↓ 续费 (必须同 tier，否则 422 拒绝)
[user.Tier 不变, ExpiredAt 延期]
  
[Admin 后台手动改 tier]
  ↓ PUT /api/admin/users/:id/tier (审计)
[user.Tier 变更, ExpiredAt 不变]

[过期用户]
  ExpiredAt < now → user.Quota() 返回 ZeroQuota
  Tier 字段保留（"曾经付过费的档位"）
```

## Appendix C: API 兼容性矩阵

| 客户端版本 | 老字段 maxDevice | 新字段 tier | 行为 |
|-----------|-----------------|------------|------|
| webapp v0.4.x（旧） | ✅ 读取 | ✅ 读取（已存在） | 零改动，正常工作 |
| webapp v0.5.x（新） | ✅ 读取（兼容） | ✅ 读取 + 用 tier 过滤 | 新增 tier 过滤逻辑 |
| 老的 mobile（任何版本） | ✅ 读取 | 忽略 | 零改动 |
| 第三方集成（无） | N/A | N/A | N/A |
