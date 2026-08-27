# 订阅授权收敛设计:cover-through + 对账兜底 + 互斥补口

日期:2026-08-22
状态:待用户审阅
范围:api/(Center 后端)+ webapp/(kaitu 购买面)。provider 中立(Apple IAP + Stripe)。

## 1. 问题

两个用户可见缺陷,同一根因:

1. **苹果订阅中却"过期"**:授权门是纯 `user.ExpiredAt > now`(`entitlement_authorize.go:19` 共享节点),而 `ExpiredAt` 靠逐笔 credit 的 delta 累加维护。任何一笔续订通知/verify 丢失,`ExpiredAt` 就落后于活跃订阅——Apple 照常扣费,用户被拒服务。
2. **订阅中仍可在 web /purchase 充值**:Stripe checkout 已有防双扣门(`api_stripe.go:74`),但 WordGate 下单口 `api_create_order` 没有——活跃订阅用户可叠加一次性充值,造成双付与期限混淆。

## 2. 架构裁决

**ExpiredAt 保持唯一读侧真相;写侧用 cover-through 收敛;对账 cron 兜底;互斥补 WordGate 缺口。**

否决的替代方案:

- **派生式授权**(读时算 `max(ExpiredAt, sub.CurrentPeriodEnd)`):ExpiredAt 读取点散布多处(`AuthorizeNodeAccess`、webapp `isMembership`、`slave_api_node`、daemon 侧缓存),改读侧必须审计全部读者,漏一处即静默 bug。写侧收敛只碰三个写点。
- **只做 cover-through 不做对账**:webhook 丢失时 `CurrentPeriodEnd` 自身陈旧,cover-through 覆盖不上,残余缝隙永在。对账是把这类工单永久关死的唯一手段,且基础设施(`GetAllSubscriptionStatuses` + cron worker 模式)已齐,成本一个常规 worker。

## 3. 组件

### 3.1 写侧收敛(cover-through)

`logic_entitlement.go` 的 `coverThrough(currentExpiredAt, throughTs)` 已定义、从未被调用。接线三处:

- **`creditAppleTransaction`**(`logic_apple_iap.go` gift/renewal 两分支之后):现有 `applyGiftCredit`/`applyRenewalCredit` 结果再过一道 `coverThrough(newExpiry, newPeriodEnd)`。语义:无论 delta 怎么算,入账后 ExpiredAt 至少覆盖到本周期末。
- **`creditStripeInvoice`**(`logic_stripe.go` 同构位置):同样接线,保持双 provider 对称。
- **`applyRenewalInfo`**(`logic_apple_iap.go:579` grace 分支):status 落为 `grace` 时 `coverThrough(user.ExpiredAt, GracePeriodExpiresDate)`——Apple 重试扣费期间不断服务。billing_retry 无 grace 期限字段,不覆盖(由授权门外的 `isSubscriptionLive` 语义兜住展示,服务以 ExpiredAt 为准)。

不变式:

- `coverThrough` 只延长、从不缩短(INV:单调)。
- 退款回收仍走 `applyClawback`(INV2 不破坏)。
- delta/`SubscriptionCredit` 审计账本原样保留——cover-through 修正的是净值正确性,不是记账方式。
- `AuthorizeNodeAccess` 与一切读取点**零改动**。

### 3.2 互斥补口(WordGate)

`api_create_order` 在品牌支付渠道门之后、tier 校验附近,插入与 `api_stripe.go:74` 逐字同构的守卫:

```go
// 防重叠(防双扣):已有任一 provider 的活跃续订订阅 → 拒绝一次性充值。
if len(GetActiveSubscriptions(user.ID)) > 0 {
    Error(c, ErrorConflict, "you already have an active subscription")
    return
}
```

- 判据复用 `GetActiveSubscriptions`(内含 `isSubscriptionLive` 精筛:active 需 `CurrentPeriodEnd > now`,grace/billing_retry 无条件活跃)。
- 错码复用 `ErrorConflict`,与 Stripe 门同码同文案——前端一套处理。
- **preview 请求同样拦截**:购买 UI 尽早拿到反馈,而非付款瞬间才失败。

### 3.3 对账兜底(reconciliation cron)

新增 `worker_subscription_reconcile.go`,模式照抄 `worker_renewal_reminder.go`(每日 Asynq cron + `Unique` 防重):

- **扫描对象**:`status IN (active, grace, billing_retry)` 且 `current_period_end < now + 48h` 的订阅(即将到期或已过期仍标活跃的行)。
- **Apple**:`appstore.GetAllSubscriptionStatuses(bundleId, originalTransactionId)`(库 v1.5.32 已内置,prod→sandbox 回退)。用返回的最新 transaction/renewal 信息走**现有** `creditAppleTransaction` / `applyRenewalInfo` 路径入账——不新开第二条写路径,dedup 由现有 `SubscriptionCredit` 幂等键保证。
- **Stripe**:`subscription.Get(providerSubscriptionID)` 拉当前状态与 `current_period_end`,同样回灌现有 `creditStripeInvoice` 语义(状态映射走 `stripeSubStatus`)。缺失的 invoice 事件由 Stripe 侧 webhook 重放兜底,对账只负责纠正 status/period 陈旧。
- **终态处理**:provider 返回 expired/revoked → 走现有 `applyRenewalInfo`/撤销语义落终态,绝不复活 revoked(现有铁律)。
- 失败容忍:单订阅对账失败记日志跳过,不阻塞整轮;每轮汇总日志(纠正 N 条 / 失败 M 条)。

### 3.4 前端提醒(webapp)

- `Purchase.tsx:879` 已按 `affordance.mode !== 'subscribe'` 切换 manage/status 面板——保留。强化 manage 文案为明确提醒:"你已有活跃订阅(渠道 + 到期日),此处不可重复购买;变更请前往 Apple 订阅设置 / Stripe 管理页"(走 i18n,中文用「开途」语境)。
- 新增对后端 `ErrorConflict` 的兜底处理:下单接口返回该码时展示同一提醒(防前端状态陈旧的竞态漏网),不展示裸错误串。
- `isMembership` / 到期日显示零改动——cover-through 之后 `expiredAt` 自然 ≥ 订阅周期末。

## 4. 测试

- **单测(api)**:
  - cover-through 接线:ExpiredAt 落后活跃订阅 → 入账后跳至 `newPeriodEnd`;ExpiredAt 领先(存量充值)→ 不缩短;grace → 覆盖到 `GracePeriodExpiresDate`;Apple/Stripe 两侧对称各测。
  - WordGate 门:活跃订阅(active/grace/billing_retry 三态)→ 拒 + `ErrorConflict`;无订阅/终态订阅 → 放行;preview 同样被拒。
  - 对账 worker:period 陈旧 + provider 返回新周期 → status/period/ExpiredAt 三者收敛;provider 返回终态 → 落终态;查询失败 → 跳过不炸。
  - 变异验证:注释掉 coverThrough 接线,相应断言必须变红(防"绿测试没跑到目标代码")。
- **api handler 测试跑法**:新 handler 测试必须 `-run` 单跑与全量各跑一次(center 包共享全局状态,两种跑法结果可能不同)。
- **前端(vitest)**:manage 文案渲染;`ErrorConflict` 兜底展示。

## 5. 上线与迁移

- 纯 Center 后端 + webapp 改动,无客户端二进制依赖,无 DB schema 变更。
- **一次性 backfill**(部署清单项):对"有活跃订阅但 `expired_at < current_period_end`"的存量用户跑一遍 coverThrough 等价 SQL,立即纠正当前被误判过期的付费用户。先 SELECT 报数,人工过目再 UPDATE。
- 前向兼容:`coverThrough` 是 max 语义,老数据只会被延长不会被缩短;worker 首轮会自然收敛陈旧行。
- 部署顺序:api 先行(门 + cover-through + worker),webapp 文案随后(门的错误响应在文案上线前也可读)。

## 6. 明确不做(YAGNI)

- 不改 `AuthorizeNodeAccess` / 任何读侧逻辑。
- 不加 Apple 侧后端"新购门"(StoreKit 侧由 Apple 自身阻止重复订阅,前端 affordance 已不兜售)。
- 不做实时轮询/推送,对账每日一轮足够(webhook 是主通道,对账只是兜底)。
- 不迁移记账模型,delta 审计账本原样保留。
