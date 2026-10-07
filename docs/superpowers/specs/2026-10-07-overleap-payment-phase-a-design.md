# Overleap 支付 A 期：堵漏 + 撤回执行器 — 实施设计（v3）

日期：2026-10-07 · 分支 `feat/overleap-trial-payment` · 上位 spec：`2026-10-07-overleap-trial-payment-design.md`

修订记录：
- v1 → v2（review：后端 6/10、文案 4/10）：撤回执行器与结账同意从 B 期提前到 A 期——条款承诺的"折算退款并结束会员"必须在 A 期就能执行，折算也必须有"用户要求立即开始"的同意做依据。
- v2 → v3（review：后端 7/10、文案 6/10）：执行器改为以 `StatutoryRefund` 行为续跑锚点、顺序改为 退款→收回→取消、退款 id 立即落库并按 metadata 查重；时间以**用户提出撤回的时刻** `noticeAt` 计；同意文案收窄；营销词表不再宣传法定权利；补墓碑行字段、截断规则的基准、审批注册、商家地址、同意记录披露。

**A 期目标**：(1) 关掉今天就存在的退款 / 钱包后门；(2) 条款改成最终政策，且政策里的每一句都有代码或明确的操作流程兑现。不做试用、不做用户自助按钮（B 期）。

## 0. 现状（已核对代码与生产库，2026-10-07）

| 项 | 现状 | 证据 |
|---|---|---|
| Stripe `charge.refunded` | 只发 Slack，会员不收回、订阅不取消 | `api/logic_stripe.go` `recordStripeRefundAlert` |
| Stripe `charge.dispute.created` | 只发 Slack | `recordStripeDisputeAlert` |
| Stripe 后台"立即取消" | `customer.subscription.deleted` → 只置 `expired`，`expired_at` 不动，会员用到期末 | `markStripeSubscriptionDeleted` |
| 迟到 `invoice.paid` | revoked 状态不复活，但**时长照加** | `creditStripeInvoice` |
| 每日对账 | cover-through 的 `UPDATE users` 不看订阅是否 revoked | `worker_subscription_reconcile.go:255` |
| 后台 / MCP `refund_order` | 退款进钱包，不看品牌 | `ProcessOrderRefund`（唯一执行点 `logic_order.go:124`，唯一调用方 `logic_approval_callbacks.go:505`） |
| 钱包 `/api/wallet/*`（7 个路由） | 后端不看品牌 | `route.go:221-235` |
| 分销返现入钱包 | 不看收款人品牌 | `addCashbackIncomeInTx`（`logic_wallet.go:165`） |
| 结账 | 无条款同意、无"立即开始"确认 | `api_stripe.go` Checkout 参数 |
| 条款 / 帮助页 | §7 "7 天可退到钱包"、§8 加密货币提现；帮助页 "email us and we'll sort it out"；条款无商家地址 | `sites/overleap/public/legal/terms-of-service.md`、`messages/*/help.json`（20 个语言） |
| 生产数据 | overleap 7 个用户；订单 / 钱包余额 / 提现账户 / 分销商 / 被邀请均为 0；Stripe 订阅 0；Apple 订阅 1（expired） | 2026-10-07 只读查询 |

所有改动对现有数据零影响，无迁移。

## 1. Stripe 收回原语与 revoked 终态

### 1.1 `revokeStripeSubscriptionInTx`

新文件 `logic_stripe_revoke.go`。不复用 Apple 的 `revokeSubscription`（不锁订阅行、加锁顺序与入账相反、原因写死中文）。

```go
// 收回一条 Stripe 订阅撑着的会员并置 revoked。幂等。锁顺序与 creditStripeInvoice 一致：
// 先订阅行，再用户行。调用方包 withDeadlockRetry。
func revokeStripeSubscriptionInTx(ctx, tx, providerSubID, reason string) (found, revokedNow bool, err error)
```

1. `FOR UPDATE` 订阅行（provider=stripe）。找不到 → `(false,false,nil)`；已 `revoked` → `(true,false,nil)`。
2. `FOR UPDATE` 用户行。用户到期落在 `(now, sub.CurrentPeriodEnd]` 才砍到 now（叠加的赠送时长不误伤）；写 `UserProHistory{Type: VipRefund, Days: -n, Reason: reason}`。
3. 订阅置 `revoked`、`auto_renew=false`。

`reason` 一律英文（overleap 用户在 `/api/user/pro-histories` 看得到）：`"Stripe full refund - ch_…"`、`"Stripe dispute - dp_…"`、`"Withdrawal within 14 days - in_…"`。

### 1.2 revoked 是终态：三道门

- `creditStripeInvoice`：锁到订阅行后，若非首张且 `sub.Status == "revoked"` → 告警 `[STRIPE-CREDIT] invoice on revoked sub` 并 `return nil`；不加时长、不改订阅行。
- `reconcileStripeSubscription` 的 cover-through：`UPDATE users … WHERE id=? AND expired_at<? AND NOT EXISTS (SELECT 1 FROM subscriptions WHERE id=? AND status='revoked')`（MySQL 1093 只限子查询读被更新的表，这里读的是另一张表，合法）。
- 已有：`applyStripeSubscriptionUpdate` / `markStripeSubscriptionDeleted` 对 revoked 短路。

### 1.3 墓碑行

本地没有订阅行（首张 invoice 入账曾失败）时，收回路径用远端订阅（`stripeFetchSubscription`）建一行：

| 字段 | 来源 |
|---|---|
| `UserID` | 远端 `metadata.user_uuid` → 用户；用户品牌必须 `AllowsPayment(stripe)`，否则告警不建 |
| `ProviderSubscriptionID` / `ProviderCustomerID` | 远端订阅 id / customer id |
| `ProductID`（NOT NULL） | 远端 `items.data[0].price.id` |
| `CurrentPeriodEnd` | 远端 `items.data[0].current_period_end` |
| `Status` / `AutoRenew` / `Environment` | `revoked` / false / 按 livemode |

`uniq_provider_sub` 撞键（并发的 `invoice.paid` 先建了行）→ 重跑一次 `revokeStripeSubscriptionInTx`。之后 Stripe 重投那张 invoice 会命中 §1.2 的门，不补发整期会员。`user_uuid` 缺失 → 告警，不建行。

## 2. Stripe 事件

| 事件 | 条件 | 动作 |
|---|---|---|
| `charge.refunded` | 全额：`ch.Refunded \|\| (ch.Amount > 0 && ch.AmountRefunded >= ch.Amount)` | 收回 → 取消 Stripe 订阅 → 告警 |
| `charge.refunded` | 部分 | 只告警——不变（我们自己的撤回退款在 §3 已处理会员） |
| `charge.dispute.created` | 任意（含 `warning_needs_response` 询问） | 收回 → 取消 → 告警 |
| `charge.dispute.closed` | — | 不自动处理；胜诉后客服按条款 7.7 恢复（运营说明） |
| `customer.subscription.deleted` | 提前结束：`ended_at < 事件自身 items.data[0].current_period_end − 3600` | 截断会员（§2.3），订阅置 `expired` |

### 2.1 归属：charge / dispute → Stripe 订阅

```go
var stripeSubscriptionByPaymentIntent = func(key, pi string) (subID string, err error)
```

`invoicepayment.Client.List{Payment: {Type: "payment_intent", PaymentIntent: pi}, Status: "paid", Expand: ["data.invoice"]}` 返回 `*Iter`：**必须** `for it.Next()` 后检查 `it.Err()`，`Err != nil` → 返回 err（500 重投）。取第一条的 `invoice.parent.subscription_details.subscription.id`；没有 → `("", nil)`。charge 取 `ch.PaymentIntent.ID`，dispute 取 `d.PaymentIntent.ID`。

### 2.2 `revokeStripeForChargeLoss(ctx, pi, reason, alertTag)`

1. `pi == ""` → 告警"无法归属，人工处理"，`nil`。
2. 查 `subID`：err → 返回 err；空 → 告警"非订阅扣款"，`nil`。
3. `withDeadlockRetry` 跑 `revokeStripeSubscriptionInTx`；`found=false` → 取远端订阅建墓碑行（§1.3）。
4. 取消（共用 `cancelStripeSubscriptionIfLive(subID, reason)`）：取远端订阅（`stripeFetchSubscription`），`resource_missing` 或状态 `canceled` / `incomplete_expired` → 跳过；否则 `stripeCancelSubscription`（新 seam，`prorate=false`、`invoice_now=false`、`cancellation_details.comment=reason`）。返回 `resource_missing` → 视为完成并告警；其他错误 → 返回 err（500 重投；第 3 步因 revoked 短路，只重试取消）。
5. 告警 `[STRIPE-REFUND]` / `[STRIPE-DISPUTE]`：charge/dispute id、金额、user_id、sub、每一步结果。

### 2.3 提前结束的截断

`markStripeSubscriptionDeleted` 增加：事件里 `ended_at < items.data[0].current_period_end − 3600` 时（后台立即取消、或 `cancel_at` 设在期中），在同一事务里按"先订阅行、再用户行"加锁：用户到期落在 `(ended_at, 事件周期末]` → 截到 `max(ended_at, 0)`，写历史 `"Stripe subscription ended early - sub_…"`。**只截短，不延长**。基准用事件自身的周期末，不用本地 `CurrentPeriodEnd`（扣款重试期间对账会把本地值推到未付周期）。

不受影响的情形：期末自然结束（`ended_at` ≈ 周期末）；扣款重试失败后被取消（此时到期早已过，cover-through 只在 `active` 时发生）。

## 3. 撤回执行器（14 天折算，后台触发）

### 3.1 资格与金额（纯函数，B 期用户按钮复用）

```go
type withdrawTarget struct {             // 从 Stripe 取，seam stripeInvoiceForWithdraw(key, invoiceID)
    InvoiceID, PaymentIntentID, Currency string
    PaidAmount int64                       // 已付那条 invoice payment 的 amount_paid（不是 invoice.amount_paid）
    PaidAt, PeriodStart, PeriodEnd int64
    CreditKind string                       // 本地 SubscriptionCredit.Kind
    HasConsent bool                         // 本地有该订阅的 SubscriptionConsent
}
type withdrawQuote struct {
    Eligible bool; Reason string            // 英文，给客服看
    UsedDays, TotalDays int; RefundAmount int64; FullRefund bool
    WindowEndsAt int64
}
func quoteStripeWithdrawal(t withdrawTarget, noticeAt int64) withdrawQuote
```

- **对象**：该订阅最新一条 `SubscriptionCredit`（`provider=stripe AND original_transaction_id=subID ORDER BY id DESC`）对应的 invoice。不用 `ProviderLatestRef`（重放旧 invoice 会把它拨回去）。
- **取 invoice**：`invoice.Get` 展开 `payments`；取 `status == "paid"` 的那条 payment，用它的 `amount_paid` 和 `payment.payment_intent`（ID）；`status_transitions.paid_at`；周期与 `extractStripeInvoiceFacts` 同一取法（period end 最大的 line）。
- **合格的付款**：`CreditKind == "purchase"`（新订阅首付；换档 = 新订阅），或 `CreditKind == "renewal"` 且周期 ≥ 360 天（年付续费）。月付续费不合格。
- **时刻**：`noticeAt` = 用户提出撤回的时刻（邮件时间等），**必填**，由客服填写；窗口和已用天数都按它算，不按执行时刻算。`noticeAt` 晚于当前时间 → 拒绝。
- **窗口**：付款日 D = `paid_at` 的 UTC 日期；截止 = D+16 日 00:00 UTC（第 14 天结束再留 1 天余量）。`noticeAt < 截止` 合格。
- **已用天数**：使用起点 `u = max(PeriodStart, PaidAt)`（扣款重试导致晚付时，没服务的天数不算用户的）；`TotalDays = max(1, round((PeriodEnd − u) / 86400))`；`UsedDays = clamp(floor((noticeAt − u) / 86400) + 1, 1, TotalDays)`（含付款当天，按 24 小时计）。
- **金额**：`RefundAmount = PaidAmount × (TotalDays − UsedDays) / TotalDays`，整数向下取整。
- **无同意记录**（`HasConsent=false`，webhook 写入曾失败）→ 没有折算依据：`FullRefund=true`，`RefundAmount = PaidAmount`，执行时告警。
- `PaidAmount == 0`（全额优惠券）→ 不合格（没有可退的钱；用户仍可关续费）。

### 3.2 `StatutoryRefund` 表

```go
type StatutoryRefund struct {
    ID uint64; CreatedAt, UpdatedAt int64
    UserID uint64 `index`
    ProviderSubscriptionID string
    InvoiceID string `uniqueIndex`              // 一笔付款只能撤回一次
    PaymentIntentID string
    Amount int64; Currency string; UsedDays, TotalDays int; FullRefund bool
    NoticeAt int64
    StripeRefundID string
    Status string                               // pending | done
    Mode string                                 // withdrawal | termination（条款 8.3）
    OperatorID uint64; Source string             // admin（B 期加 user）
}
```

### 3.3 `executeStripeWithdrawal(ctx, req)`

`req = {UserUUID, NoticeAt, Mode, OperatorID, Source}`。顺序：**退款 → 收回 → 取消 → 完成**。

1. **续跑优先**：该用户有 `status=pending` 的行 → 直接用行里存的字段，不重新算资格（窗口过期、订阅已 revoked 都不影响续跑）。
2. 否则新建：取该用户最新一条非 revoked 的 Stripe 订阅 → 目标 invoice → `quote`。`Mode=withdrawal` 不合格 → 返回带原因的错误。`Mode=termination`（条款 8.3：我们主动终止）→ 跳过合格性，按 `noticeAt = 终止时刻` 折算未用部分。插 `pending` 行（唯一索引挡并发 / 重复）。
3. **退款**（`Amount > 0` 且 `StripeRefundID` 为空时）：
   - 先查重：`stripeFindWithdrawalRefund(pi, invoiceID)` 列出该 PI 的退款，找 `metadata.center_withdrawal == invoiceID`。找到 → 用它。
   - 没有 → `refund.New{PaymentIntent, Amount, Metadata{center_withdrawal: invoiceID}}`，`Params.SetIdempotencyKey("withdraw-"+invoiceID)`（Stripe 只保留 24 小时，所以查重才是主防线，幂等键是第二道）。
   - **退款 id 立即单独提交**到行上，再进下一步。
4. **收回**：`revokeStripeSubscriptionInTx(reason)`（已 revoked 短路）。先收回再取消：取消触发的 `customer.subscription.deleted` 遇 revoked 短路，撤回的历史记录不会被截断规则抢先写成"ended early"。
5. **取消**：`cancelStripeSubscriptionIfLive`。
6. 行置 `done`；告警频道记一条。

任一步失败返回错误；重试（新的审批单）从第 1 步续跑。Stripe 随后的 `charge.refunded` 是部分退款，只告警；`FullRefund` 时是全额退款 → §2 流程遇 revoked 短路，取消已完成则跳过。

### 3.4 入口（A 期只给后台）

- 路由在 `/app`（`AdminRequired()`）：`GET /app/users/:uuid/stripe-withdrawal?notice_at=` → quote；`POST /app/users/:uuid/stripe-withdrawal {notice_at, mode, reason}` → `SubmitApproval("stripe_withdrawal", …)`。
- `worker_integration.go` 注册 `RegisterApprovalCallback("stripe_withdrawal", executeApprovalStripeWithdrawal)`；`logic_approval.go` 的 `actionDisplayNames` 加中文名。超管请求同步执行，失败审批单记 `failed`，重提即续跑（§3.3 第 1 步）。
- MCP（`tools/kaitu-center`）：`quote_stripe_withdrawal`、`withdraw_stripe_subscription`，照 `admin-orders.ts` 的退款工具写；`notice_at` 必填。
- 客服流程写进 `docs/customer-service/README.md`：收到撤回请求 → 记下用户消息时间 → quote → 回邮件确认金额 → withdraw。**不要在 Stripe 后台手工部分退款**。条款 8.3 的主动终止用 `mode=termination`。

## 4. 结账同意（折算的依据）

`api_stripe_checkout` 加：
- `consent_collection.terms_of_service = "required"`；
- `custom_text.terms_of_service_acceptance.message`（v1，≤1200 字符，markdown 链接）：

> I agree to the [Terms of Service](https://overleap.io/terms). My subscription starts now and renews automatically at the price shown until I cancel. I ask for the service to begin immediately. I understand that if I withdraw within 14 days of my first payment, or of an annual renewal, my refund will be reduced for the days already used.

（`https://overleap.io/terms` 已验证 307 到 `/en-GB/terms`。）

`checkout.session.completed`（现在只打日志）改为：`consent.terms_of_service == "accepted"` 时写

```go
type SubscriptionConsent struct {
    ID uint64; CreatedAt int64
    UserID uint64 `index`                       // client_reference_id（user UUID）→ id
    CheckoutSessionID string `uniqueIndex`
    ProviderSubscriptionID string `index`
    TextVersion string                           // "2026-10-v1"
    AcceptedAt int64                             // event.created
    Country string                               // customer_details.address.country，可为空
}
```

不自动删（≥3 年，加州 ARL；隐私政策披露见 §6.2）。用户找不到 → 告警并 `nil`（同意记录缺失不阻断入账；撤回时按 §3.1 全额退）。

**部署前置（硬性）**：Stripe Dashboard → Settings → Public details 填 Terms of service URL，否则带 `consent_collection` 的 Checkout 创建直接报错，购买全断。先填 URL，再部署 api。

## 5. 钱包对 overleap 关闭

- `BrandConfig.Wallet bool`（kaitu true / overleap false），导出进契约 `brands.<b>.wallet`；webapp 契约测试断言 `features.wallet === contract.brands[b].wallet`（web/ 只比品牌 id 集合，不受影响——已核对）。
- 中间件 `WalletRequired()`：`ReqUser(c) == nil` → `ErrorNotLogin`；`!Wallet` → `ErrorNotSupported`。7 个 `/api/wallet*` 路由都挂在 `AuthRequired()` 之后（读用户品牌：`AuthRequired` 对 admin 免品牌检查，请求品牌对 admin 不可信）。
- `ProcessOrderRefund` 锁用户行后拒绝无钱包品牌；`api_admin_refund_order` 预校验同样拒绝，返回中文说明（"overleap 订单：Stripe 用撤回工具；Apple 由 Apple 退款"），不建审批单。
- `addCashbackIncomeInTx`：收款人品牌无钱包 → 不入账，告警，返回 nil。
- **不做（后续）**：邀请码跨品牌绑定（品牌隔离问题，与退款无关，生产为 0）。

## 6. 文案（只改 overleap）

### 6.1 `terms-of-service.md`

`Last updated: 2026-10`。§7 全文替换：

> ### 7. Cancellation, Withdrawal and Refunds
>
> 7.1 **Turning off renewal**: You can turn off automatic renewal at any time. You keep access until the end of the period you've paid for and won't be charged again. Turning off renewal does not by itself withdraw from the contract or give you a refund.
>
> 7.2 **14-day right to withdraw**: For subscriptions billed by us (not through the App Store), you may withdraw within 14 days after each of these payments: (a) the first payment for a new subscription, including a subscription to a different plan; and (b) each renewal payment of an annual plan. Renewal payments of monthly plans are not covered.
>
> 7.3 **Refund amount**: Because you asked us to start the Service immediately, the refund is reduced in proportion to the days of the paid period that have passed when you tell us you are withdrawing, counting the day of payment. Your subscription and your access end when the refund is issued.
>
> 7.4 **How to withdraw**: Tell us by email at support@overleap.io (ideally from your account's email address, so we can find you quickly) or in any other clear way. You may use the model withdrawal form at the end of these Terms, but you don't have to. We will confirm the amount and refund it to your original payment method within 5 business days, and in any case within 14 days.
>
> 7.5 **App Store purchases**: Subscriptions bought through Apple's App Store are billed by Apple. Refunds are handled by Apple under its own policy; we can't issue them. If Apple refunds a purchase, the access it paid for ends.
>
> 7.6 **Other refunds**: Apart from Sections 7.2–7.5 and 8.3, payments are not refundable, including for partly used periods. If the Service is not provided as described or with reasonable care, you may be entitled to a remedy such as a price reduction under consumer law. Nothing in these Terms limits rights you have under the laws of your country.
>
> 7.7 **Chargebacks**: If a payment is fully refunded or disputed with your bank or card issuer, the subscription it paid for ends and your access stops. If a dispute is resolved in our favour, contact us and we will restore your access for the rest of the paid period.

- 删除 §8 "Wallet and Withdrawals"，后续节号前移（§9→§8 …）。全站只有 ToS 4.3→4.2、隐私 4.2→1.4 两处带节号的引用，都不受影响。
- 新 §8.1：
  > **User Termination**: You may terminate this Agreement at any time by deleting your account. Deleting your account does not cancel a renewing subscription — cancel it first on your account page (or in your App Store settings), or you will continue to be charged. Unused time is not refunded on deletion; if you are entitled to a refund under Section 7.2, request it before deleting your account.
- 新 §8.3 末尾加：
  > If we terminate under Section 8.2(b) or 8.2(d), we will refund the unused part of any prepaid period for subscriptions billed by us. For subscriptions bought through the App Store, you can request a refund from Apple.
- 新 §11.1 联系方式加商家邮寄地址：`Overleap LLC, {ADDRESS}`。
- 文末 "Model withdrawal form"（CCR Sch 3 措辞）：
  > To: Overleap LLC, {ADDRESS}, support@overleap.io
  > I hereby give notice that I cancel my contract for the supply of the following service: Overleap subscription.
  > Ordered on: ___ · Name: ___ · Address: ___ · Account email: ___ · Date: ___

**`{ADDRESS}` 由用户提供；拿到之前条款不发布。**

### 6.2 隐私政策与删号页

- `privacy-policy.md` 4.3 后补："This includes records of the subscription terms you agreed to at checkout (time, country and wording), kept as proof of your consent." 更新日期。
- `delete-account.md`：更新日期。第 41 行 → "Unused subscription time is **not** refunded and cannot be restored afterwards. If you're within 14 days of your first payment for a subscription on our website, or of an annual renewal, email support@overleap.io before deleting your account to withdraw with a prorated refund (Terms, Section 7)." §4 保留数据清单加"Subscription consent records — kept with payment records"。§5 加："Deleting your account does not cancel a renewing subscription. Cancel it first on your account page, or in your App Store settings for iPhone purchases."

### 6.3 站点 messages（20 个语言）

- `help.json` `billing.items.refund.answer`（英文基准）："You can withdraw and get a refund for the days you haven't used within 14 days of your first payment for a subscription on our website, or of an annual renewal. Email support@overleap.io. App Store purchases are refunded by Apple."
- `help.json` `billing.items.manage.answer` 末尾加："Cancelling stops renewal; it doesn't refund — see “Can I get a refund?”"
- `pricing.json` `faqSubtitle`："Payment, cancelling and what the plan covers."
- 翻译：邮箱、App Store、Apple 原样；de/fr 用法定词（Widerruf / rétractation）但范围必须是"首付 + 年付续费"；ar/fa 邮箱放在分句末尾、后面不跟标点。

### 6.4 其他

- webapp `DeleteAccountDialog.tsx:148`：`deleteLoseWallet` 只在 `features.wallet` 时显示。
- `.agents/product-marketing-context.md:269` 英文必用词删 "7-day refund"，改 "cancel anytime"；禁用词加一条："Overleap 不把退款 / 撤回当卖点（法定权利不能宣传成产品特色，英国 CPUT / DMCCA 禁止）"。中文那行是开途的，不动。
- kb 分支 `docs/customer-service/overleap/04`：退款段改为"有 14 天折算撤回（首付 / 年付续费）；机器人不判资格、不报金额；收集账号邮箱和付款日期 → 转人工"。`03` 第 38 行补"如在 14 天内，先申请撤回再删号"。`docs/customer-service/README.md` 加客服撤回流程（§3.4）。

## 7. 测试（先红后绿；实现后做变异验证，变异用 scratchpad 备份还原）

| # | 测试 | 断言 | 变异 |
|---|---|---|---|
| 1 | 全额退款 | 到期≈now；sub revoked；`VipRefund` 历史、Reason 英文；cancel 被调 | 删 revoke |
| 2 | 部分退款 | 无变化；cancel 未调 | 全额判定改 `AmountRefunded > 0` |
| 3 | 赠送时长保护 | 到期 > 周期末 → 不动，sub revoked | 去掉 `<= CurrentPeriodEnd` |
| 4 | 取消失败后重投 | 第一次 500；第二次不重复写历史、cancel 再调 | 去掉 revoked 短路 |
| 5 | 远端已取消 | 不调 cancel | 去掉状态跳过 |
| 6 | cancel 返回 resource_missing | 200 | 当错误处理 |
| 7 | invoice payments 迭代器出错 | 500 | 忽略 `it.Err()` |
| 8 | 无 PI / 非订阅扣款 | 200，无写入 | — |
| 9 | 本地无订阅行 | 墓碑行字段正确（ProductID、周期、user、revoked）；cancel 被调 | 不建墓碑 |
| 10 | 墓碑撞键 | 重跑收回，最终 revoked | 去掉重跑 |
| 11 | 墓碑后迟到 `invoice.paid` / 收回后迟到续费 | 不加时长，告警 | 删 creditStripeInvoice 的门 |
| 12 | 对账遇 revoked | 到期不变 | 删 NOT EXISTS |
| 13 | 先全额退款后拒付 | 第二次不改数据、不调 cancel | — |
| 14 | 拒付 | 同 #1 | 删 dispute 分支 |
| 15 | deleted 截断 | 立即取消 → 截到 ended_at；期末自然结束不截；本地周期被推远时以事件周期为准；绝不延长 | 删截断 / 改用本地周期 |
| 16 | quote 表驱动 | 首付 / 年付续费合格；月付续费、窗口外、0 元不合格；窗口截止 D+16 00:00 前后；用户第 13 天提出、第 16 天执行 → 合格且 used=13；晚付按 `max(start, paid_at)`；无同意 → 全额；`noticeAt` 在未来 → 拒 | 改 `+1`、改窗口、改起点 |
| 17 | invoice 解析 | 先失败后成功的两条 payment 取 paid 那条及其 amount_paid | 取第一条 |
| 18 | 执行器正常路径 | 退款金额、metadata、幂等键；收回；取消；done | — |
| 19 | 执行器续跑 | 在退款后 / 收回后 / 取消后失败各一次 → 续跑完成，不重复退款、不重算资格（窗口已过仍续跑） | 续跑前先算资格 |
| 20 | 超 24 小时续跑 | 退款已在 Stripe、行上无 id → 按 metadata 找到，不新建 | 去掉查重 |
| 21 | 执行中途到达 deleted 事件 | 遇 revoked 短路，历史是 "Withdrawal…" | 把取消挪到收回前 |
| 22 | termination 模式 | 跳过合格性，折算未用部分 | — |
| 23 | 审批 | 注册回调、显示名；POST 建审批单 | 不注册 |
| 24 | 结账参数 | `consent_collection`、`custom_text` 存在 | 删参数 |
| 25 | `checkout.session.completed` | 写 `SubscriptionConsent`；重投不重复；国家为空可写 | — |
| 26 | 钱包路由守卫 | 生产 `SetupRouter()` 枚举 `/api/wallet*`，≥7；overleap 全部 `ErrorNotSupported`；kaitu `GET /api/wallet` 不是；无用户 → 拒 | 去掉任一路由的门 |
| 27 | `ProcessOrderRefund` / `api_admin_refund_order` 拒 overleap | 无写入、无审批单；含已有 pending 审批被执行 | 删门 |
| 28 | 返现收款人无钱包 | 不入账 | 删门 |
| 29 | 契约 | golden 含 `wallet`；webapp 断言一致 | webapp overleap 改 `wallet:true` |
| 30 | webapp 删号对话框 | overleap 无钱包行，kaitu 有 | — |
| 31 | MCP 工具 | 参数校验（`notice_at` 必填）、调用路径 | — |

环境：worktree 已拷 `center/config.yml`；DB 走 devdb（当前 pve）；`-v` 下 0 SKIP；handler 测试单跑与全量各一次。webapp `yarn test`；`tools/kaitu-center` 测试；`sites/overleap` lint / build。

## 8. 文档

- `api/CLAUDE.md` 支付段：Stripe 收回规则、revoked 终态三道门、提前结束截断、撤回执行器与续跑、"不要在 Stripe 后台手工部分退款"。
- 上位 spec §8 分期表：A 期内容同步为 v3。
- 内部说明：已用天数按 24 小时计，从 `max(周期起点, 付款时刻)` 起。

## 9. 上线清单（顺序有依赖）

1. **用户提供** Overleap LLC 邮寄地址（填 `{ADDRESS}`）。
2. **Stripe Dashboard**（用户操作）：填 Terms of service URL；webhook 端点确认订阅了 `charge.refunded`、`charge.dispute.created`、`checkout.session.completed`、`customer.subscription.deleted`；Billing Portal 关闭"立即取消"（只留期末取消）；打开退款收据邮件。
3. 合并 → `make deploy-api`（center-deploy）。
4. `git push origin main:website`（overleap 条款 / 隐私 / 帮助页）。
5. `webapp/x.y.z-overleap` tag（删号对话框）。
6. MCP 工具按 `tools/kaitu-center` 的 CLAUDE.md 发布。
7. kb 分支合并后 `scripts/sync-kb.sh overleap …` 同步知识库。

## 10. 请英国 / 欧盟律师确认（不阻塞 A 期上线，上线后尽快）

1. VPN 是"服务"还是"数字内容"（决定能否折算）。
2. CCR reg 16：确认邮件需含"要求立即开始"的确认（Stripe 收据不含）——可能要在 B 期欢迎邮件里补。
3. 欧盟撤回按钮（CRD Art 11a，2026-06-19 生效）：A 期只有邮件渠道能否接受到 B 期上按钮。
4. DMCCA 续费冷静期的触发范围与折算基数。
5. 拒付期间即停止服务是否可接受。
6. 条款 12.3（新 11.3）适用法律未定义、10.2（新 9.2）责任上限。
7. "立即开始"的请求与条款同意捆绑在一个勾选里；欧盟指引倾向单独、明确的主动请求。
8. `custom_text` 只有英文，而 Stripe Checkout 会按用户语言本地化。
9. 14 天内在 Billing Portal 点"取消"是否本身构成 reg 32(3) 的撤回声明。

## 11. 不做

- 用户自助撤回按钮、`DataSubscription` 撤回字段、试用、提醒邮件（B 期）。
- 拒付胜诉自动恢复。
- 邀请码跨品牌绑定。
- kaitu 品牌任何行为变化（钱包门对 kaitu 恒放行；kaitu 不走 Stripe）。
