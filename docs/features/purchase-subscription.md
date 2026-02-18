# Feature: Purchase & Subscription Flow

## Meta

| Field   | Value                  |
|---------|------------------------|
| Feature | purchase-subscription  |
| Version | v1                     |
| Status  | implemented            |
| Created | 2026-02-18             |
| Updated | 2026-02-18             |

## Overview

Kaitu VPN 的购买与订阅流程，涵盖套餐展示、订单创建、外部支付、支付结果确认、会员授权发放、以及授权历史查看。

核心交互链路：
```
用户选择套餐 → 选择付费对象（自己/成员） → [可选] 输入优惠码
→ 实时预览价格 → 点击支付 → 创建订单 → 跳转外部支付页
→ 支付结果对话框（用户手动确认） → 跳转授权历史页
→ [后台] Wordgate Webhook → MarkOrderAsPaid → 授权生效
```

## Product Requirements

### 1. 套餐展示（Plan Listing）

**页面路径**: `/purchase`
**路由配置**: 非 keep-alive，每次访问重新渲染
**导航入口**: BottomNavigation 固定 tab（已登录显示"充值"，未登录显示"激活"）

- 从 `/api/plans` 获取激活套餐列表（`is_active = true`）
- 套餐按 `month` 升序排列
- 每个套餐卡片显示：套餐名称（i18n key `purchase:plan.pid.{pid}`）、月均价、总价、原价（划线）、省多少钱（Chip）
- `highlight` 标记的套餐显示"热门套餐"斜角彩带（Ribbon）
- 默认选中 `highlight` 套餐，无 highlight 则选第一个
- 使用 SWR 缓存策略：`cacheStore` TTL 5 分钟，有缓存立即返回 + 后台刷新

**套餐 PID 列表**: `1m`, `3m`, `6m`, `1y`, `2y`, `3y`, `4y`, `5y`, `forever`

### 2. 购买对象选择（Member Selection）

**未登录用户**:
- 显示内嵌 `EmailLoginForm` 组件（非 LoginDialog 弹窗），详见 [auth-system.md](auth-system.md) TD-3
- 登录成功后自动刷新购买页

**已登录用户**:
- 显示 `MemberSelection` 组件
- 默认勾选"为自己购买"
- 显示成员列表（来自 `/api/user/members`），默认全选
- 可通过对话框添加新成员（输入邮箱，调用 `POST /api/user/members`）
- 显示已选人数 badge

### 3. 用户状态提示

根据用户状态显示不同 banner：
- **未完成首单**（`!isMembership`）: 黄色警告"完成首次充值，使用云端节点"
- **授权已过期**（`isExpired`）: 红色警告"您的授权已过期，请选择套餐续费"
- **好友推荐横幅**: 有 `inviteCode` 且 `appConfig.inviteReward` 存在时，显示"好友推荐专享：本单立享 N 天额外赠送"

### 4. 优惠码（Campaign Code）

- 点击"我有优惠活动码"展开输入框（Collapse 动画）
- 输入优惠码后自动触发预览订单刷新
- 无效优惠码显示红色错误提示（error code `400001` → `ErrorInvalidCampaignCode`）
- 可清除优惠码并收起输入框
- 优惠码支持大写自动转换（`autoCapitalize: "characters"`）

**后端优惠类型**:
| 类型 | `campaign.type` | `campaign.value` 含义 |
|------|-----------------|----------------------|
| 折扣 | `discount` | 百分比（80 = 8 折） |
| 优惠券 | `coupon` | 固定金额（美分） |

**优惠匹配条件**（`campaign.matcherType`）:
| 匹配器 | 含义 |
|--------|------|
| `first_order` | 仅首单用户 |
| `vip` | 完成过首单的用户 |
| `all` | 所有用户 |

优惠码还有时间窗口（`startAt` / `endAt`）和使用次数限制（`maxUsage`）。

### 5. 价格预览（Order Preview）

- 套餐、优惠码、购买对象任意变化时，自动调用 `POST /api/user/orders` with `preview: true`
- 预览返回 `DataOrder`：原价、优惠减免、应付金额、活动描述
- 总价区域显示：原价划线、应付金额突出、活动描述、好友推荐赠送天数 Chip
- 预览模式不创建真实订单，不生成支付链接

### 6. 下单与支付（Order Creation & Payment）

**前置检查**:
- 必须已登录（未登录点击支付 → 弹出 LoginDialog）
- 必须选择至少一个付费对象
- 套餐列表不能为空

**下单流程**:
1. `POST /api/user/orders` with `preview: false`
2. 后端创建本地 Order 记录
3. 通过 Wordgate SDK 创建支付订单（`CreateAppCustomOrder`）
4. 返回 `payUrl`（外部支付页面链接）和 `order` 数据
5. 前端调用 `window._platform.openExternal(payUrl)` 在外部浏览器打开支付页

**金额计算**:
- 所有金额单位为**美分**（cents）
- `totalAmount = plan.price * quantity`
- 多用户购买时 `title = "{plan.label} x {quantity}用户"`
- 优惠减免后 `payAmount = originAmount - campaignReduceAmount`

### 7. 支付结果对话框（PayResultDialog）

支付页在外部浏览器打开后，app 内弹出支付结果确认对话框：
- 显示订单号（`order.uuid`）、支付金额
- 两个按钮：
  - **支付成功**（绿色）→ 刷新用户资料 → 导航到 `/pro-histories?type=recharge&from=/purchase`
  - **支付失败**（红色）→ 刷新用户资料 → 导航到 `/pro-histories?type=recharge&from=/purchase`
- 注意：两个按钮最终导航相同，用户需到历史页确认实际支付状态
- 提示文案："请在支付完成后，点击下方按钮确认结果"

### 8. 支付确认与授权发放（Webhook → Authorization）

**Wordgate Webhook 流程**（`api_webhook.go`）:
1. 验证 `X-Webhook-Signature` 签名（HMAC + 300s 时间窗口）
2. 解析 `WebhookEventOrderPaid` 事件
3. 根据 `wordgate_order_no` 查找本地订单
4. 调用 `MarkOrderAsPaid()` 在事务中处理：
   - 更新订单状态（`is_paid = true`, `paid_at = now`）
   - 处理邀请购买奖励（`handleInvitePurchaseRewardInTx`，必须在授权之前，因为后续会设置 `IsFirstOrderDone = true`）
   - 为目标用户增加 Pro 授权（`applyOrderToTargetUsers`）
   - 处理分销商返现（`processOrderCashbackInTx`）
5. 支持死锁重试（`withDeadlockRetry`, 最多 3 次，递增延迟）

**注意**: Webhook handler 使用 HTTP status code 而非标准 JSON error response，因为支付提供商依赖 HTTP status 决定是否重试。

### 9. 授权历史（Pro History）

**页面路径**: `/pro-histories`
**路由守卫**: `LoginRequiredGuard`（未登录触发登录弹窗，但不阻止页面渲染）
**Feature flag**: `appConfig.features.proHistory`

- 调用 `GET /api/user/pro-histories?page=N&pageSize=10&type=xxx`
- 支持 `type` URL 参数过滤（`recharge`, `reward`, `invite_purchase_reward` 等）
- 支持 `from` URL 参数控制 BackButton 返回路径（默认 `/account`）
- 分页显示，使用 `Pagit` 组件

**每条记录显示**:
- 类型 Chip（`recharge` = 充值/蓝色, `reward` = 奖励/青色）
- 天数 Chip（`+N天`/绿色）
- 原因文本（`reason`）
- 时间戳
- 充值类型记录额外显示订单详情卡片：
  - 订单号（可复制）
  - 支付状态 Chip（已支付/绿色, 未支付/黄色）
  - 产品名称
  - 支付金额（`¥` 显示，`payAmount / 100` 美元）
  - 支付时间
  - 活动信息（如有）：活动描述 Chip + 折扣金额

**ProHistory 类型枚举**（`VipChangeType`）:
| 值 | 含义 |
|----|------|
| `purchase` | 购买充值 |
| `invite_reward` | 邀请奖励（邀请人获得） |
| `invited_reward` | 被邀请奖励（被邀请人获得） |
| `system_grant` | 系统发放 |

### 10. MembershipGuard 重定向

- 当用户授权过期（`isExpired`）时，访问非白名单页面自动重定向到 `/purchase`
- 白名单页面（不重定向）：`/purchase`, `/account`
- 用户数据加载中或未加载完成时不做重定向（避免闪烁）

## Technical Decisions

### TD-1: 外部支付 + 手动确认模式

**决策**: 支付在外部浏览器完成，app 内无法自动感知支付结果，采用手动确认 + webhook 异步处理模式。

**原因**:
- Wordgate 支付集成使用 redirect-based 支付流（PayPal、Stripe 等），需在浏览器中完成
- App 内 WebView 无法可靠拦截支付回调
- Webhook 是支付状态的唯一可靠来源

**后果**: 支付成功/失败按钮均导航到历史页，由用户自行确认。真实状态由 webhook 异步更新。

### TD-2: Preview/Create 复用同一 API

**决策**: `POST /api/user/orders` 通过 `preview` 参数同时承担价格预览和实际下单功能。

**原因**: 保证预览价格与实际订单金额完全一致，避免前后端价格计算不一致。

**实现**: `preview: true` 时只计算不保存、不创建 Wordgate 订单、不返回 `payUrl`。

### TD-3: 内嵌 EmailLoginForm（非 LoginDialog）

**决策**: Purchase 页面使用内嵌式登录表单而非全局 LoginDialog。

**原因**: 购买页是核心转化路径，内嵌登录降低操作步骤、减少弹窗打断感，提高转化率。LoginDialog 仍用于其他场景（如点击支付按钮时未登录的兜底）。

### TD-4: SWR 缓存策略

**决策**: 套餐列表、App 配置、成员列表均使用 Stale-While-Revalidate 模式。

| 数据 | Cache Key | TTL |
|------|-----------|-----|
| 套餐列表 | `api:plans` | 300s (5 min) |
| App 配置 | `api:app_config` | 600s (10 min) |
| 成员列表 | `api:user_members` | 180s (3 min) |
| 用户信息 | `api:user_info` | 3600s (1 hour) |

有缓存时立即返回 + 后台静默刷新。登录状态变化时重新加载套餐列表。

### TD-5: 金额单位为美分

**决策**: 前后端所有金额字段以美分（cents）为单位传输，前端显示时除以 100。

**原因**: 避免浮点精度问题，与 Wordgate 支付系统对齐。

### TD-6: 多用户批量购买

**决策**: 支持同时为自己和多个成员购买授权，单个订单处理。

**实现**:
- `forMyself: boolean` + `forUserUUIDs: string[]` 参数
- 后端验证 `forUserUUIDs` 必须是当前用户的 delegate member（`DelegateID = user.ID`）
- `quantity = (forMyself ? 1 : 0) + len(forUserUUIDs)`
- 总金额 = `plan.price * quantity`
- 单价 = `totalPayAmount / quantity`（传给 Wordgate）

### TD-7: 死锁重试机制

**决策**: Webhook 支付处理使用 `withDeadlockRetry` 包装事务，最多重试 3 次。

**原因**: 两个并发订单涉及相同邀请人时，邀请奖励写入可能导致 MySQL 死锁（Error 1213）。递增延迟（10ms, 20ms, 30ms）重试解决。

## Key Files

### Frontend

| 文件 | 职责 |
|------|------|
| `webapp/src/pages/Purchase.tsx` | 购买主页面：套餐列表、价格预览、下单、PayResultDialog |
| `webapp/src/pages/ProHistory.tsx` | 授权历史页：分页列表、类型过滤、订单详情卡片 |
| `webapp/src/components/MembershipGuard.tsx` | 过期用户重定向到 `/purchase` |
| `webapp/src/components/MemberSelection.tsx` | 购买对象选择：自己 + 成员列表 + 添加成员 |
| `webapp/src/components/EmailLoginForm.tsx` | 内嵌登录/注册表单（验证码 + 密码两种方式） |
| `webapp/src/components/LoginRequiredGuard.tsx` | 未登录路由守卫（触发 LoginDialog，不阻止渲染） |
| `webapp/src/services/api-types.ts` | TS 类型定义：Plan, Order, Campaign, ProHistory, CreateOrderRequest |
| `webapp/src/services/cloud-api.ts` | Cloud API 客户端：auth 注入、401 refresh、SResponse 格式 |
| `webapp/src/services/cache-store.ts` | SWR 缓存层（TTL + 后台刷新） |
| `webapp/src/hooks/useUser.ts` | 用户信息 hook：isMembership, isExpired 派生状态 |
| `webapp/src/i18n/locales/zh-CN/purchase.json` | 购买页 i18n（套餐名、按钮文案、错误提示） |
| `webapp/src/i18n/locales/zh-CN/account.json` | 账户页 i18n（proHistory 部分） |
| `webapp/src/config/apps.ts` | Feature flags（proHistory, memberManagement） |
| `webapp/src/components/BottomNavigation.tsx` | 底部导航：Purchase tab 入口 |

### Backend

| 文件 | 职责 |
|------|------|
| `api/route.go` | 路由注册：`/api/plans`, `/api/user/orders`, `/api/user/pro-histories` |
| `api/api_plan.go` | `GET /api/plans` — 获取激活套餐列表 |
| `api/api_order.go` | `POST /api/user/orders` — 创建/预览订单；`GET /api/user/pro-histories` — 授权历史 |
| `api/api_webhook.go` | Wordgate 支付 webhook 接收与处理 |
| `api/logic_order.go` | `MarkOrderAsPaid` — 支付成功后的主调度（授权 + 邀请奖励 + 返现） |
| `api/logic_campaign.go` | 优惠码逻辑：查找、匹配、应用（折扣/优惠券）、使用次数统计 |
| `api/logic_member.go` | `addProExpiredDays` — 增加 Pro 授权天数；`applyOrderToTargetUsers` |
| `api/model.go` | GORM 模型：Order, Plan, Campaign, UserProHistory, VipChangeType |
| `api/type.go` | API DTO：DataOrder, DataPlan, DataProHistory |

## API Endpoints

| Method | Path | Auth | 说明 |
|--------|------|------|------|
| `GET` | `/api/plans` | None | 获取激活套餐列表 |
| `GET` | `/api/app/config` | None | 获取 App 配置（含邀请奖励配置） |
| `POST` | `/api/user/orders` | Required | 创建/预览订单 |
| `GET` | `/api/user/pro-histories` | Required | 获取授权历史（分页 + 类型过滤） |
| `GET` | `/api/user/members` | Required | 获取成员列表 |
| `POST` | `/api/user/members` | Required | 添加成员 |
| `POST` | `/webhook/wordgate` | Signature | Wordgate 支付回调 |

## Acceptance Criteria

### 套餐展示
- [ ] 套餐列表按月数升序显示
- [ ] highlight 套餐默认选中且显示"热门套餐"彩带
- [ ] 显示月均价、总价、原价划线、省钱金额
- [ ] 套餐数据有 5 分钟 SWR 缓存
- [ ] 套餐加载中显示 loading 状态，空列表显示空状态

### 登录与购买对象
- [ ] 未登录用户看到内嵌 EmailLoginForm
- [ ] EmailLoginForm 支持验证码登录和密码登录两种方式
- [ ] 未激活用户可输入邀请码（支持 cookie 自动填充）
- [ ] 已登录用户看到 MemberSelection（自己 + 成员列表）
- [ ] 可添加新成员（通过邮箱）
- [ ] 未选择任何付费对象时支付按钮禁用

### 价格预览
- [ ] 切换套餐、输入优惠码、改变购买对象时自动刷新预览价格
- [ ] 无效优惠码显示 `ErrorInvalidCampaignCode` 错误提示
- [ ] 优惠码清除后错误提示同步清除
- [ ] 多用户购买时显示"Pro授权 N 个月 x M 用户"
- [ ] 好友推荐赠送天数 Chip 正确显示

### 下单与支付
- [ ] 未登录点击支付 → 弹出 LoginDialog
- [ ] 下单成功 → 外部浏览器打开支付页
- [ ] PayResultDialog 显示订单号和支付金额
- [ ] 支付成功/失败均导航到 `/pro-histories?type=recharge&from=/purchase`
- [ ] 导航前刷新用户资料

### Webhook 与授权
- [ ] Webhook 签名验证通过后处理支付
- [ ] 订单标记已支付 + 记录支付时间
- [ ] 邀请购买奖励在授权之前处理
- [ ] 目标用户 Pro 授权天数正确增加
- [ ] 分销商返现正确发放
- [ ] 已支付订单幂等处理（不重复发放）
- [ ] 死锁场景自动重试

### 授权历史
- [ ] 分页显示授权历史记录
- [ ] 支持按 type URL 参数过滤
- [ ] 充值记录显示订单详情（订单号、金额、状态、活动信息）
- [ ] 订单号可复制
- [ ] BackButton 根据 `from` 参数返回正确页面

### MembershipGuard
- [ ] 过期用户访问非白名单页面重定向到 `/purchase`
- [ ] `/purchase` 和 `/account` 不受重定向影响
- [ ] 用户数据加载中不做重定向
