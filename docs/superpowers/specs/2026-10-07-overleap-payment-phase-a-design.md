# Overleap 支付 A 期：堵漏 + 撤回执行器 — 实施设计（v6）

日期：2026-10-07 · 分支 `feat/overleap-trial-payment` · 上位 spec：`2026-10-07-overleap-trial-payment-design.md`

修订记录：
- v1 → v2（review：后端 6/10、文案 4/10）：撤回执行器与结账同意从 B 期提前到 A 期——条款承诺的"折算退款并结束会员"必须在 A 期就能执行，折算也必须有"用户要求立即开始"的同意做依据。
- v2 → v3（review：后端 7/10、文案 6/10）：执行器改为以 `StatutoryRefund` 行为续跑锚点、顺序改为 退款→收回→取消、退款 id 立即落库并按 metadata 查重；时间以**用户提出撤回的时刻** `noticeAt` 计；同意文案收窄；营销词表不再宣传法定权利；补墓碑行字段、截断规则的基准、审批注册、商家地址、同意记录披露。

- v3 → v4（review：后端 8/10、文案 8/10）：撤回按"请求"而非"用户"续跑，每用户至多一个未完成请求、可作废；通知后被扣的续费全额退；退前查 charge 已退 / 拒付；webhook 识别我们自己的撤回退款；`FullRefund` 只用于撤回模式；条款 8.3(c) 与结账同意的续费价措辞对齐。
- v4 → v5（review：后端 8/10、文案 9/10）：作废行不再占用 invoice 唯一键；通知后的续费从 Stripe 已付 invoice 列表枚举、续跑与取消后都重新枚举；退款额封顶在 charge 剩余可退额；作废会补完收回与取消并报告；可按 request_id 续跑；termination 的时刻限定在近 48 小时。
- v5 → v6（review：后端 9/10、文案 10/10）：退款步骤先按 metadata 查自家退款再查 charge 状态；作废前同样查一遍未落库的退款。

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
2. `FOR UPDATE` 用户行。从用户到期里扣掉这笔订阅**尚未用掉的时长** `CurrentPeriodEnd − now`，最低扣到 now；写 `UserProHistory{Type: VipRefund, Days: -n, Reason: reason}`。（实施时由安全审查纠正：原写的 Apple 规则"到期落在 `(now, CurrentPeriodEnd]` 才砍到 now"在叠加入账下失效——先有赠送时长再买，到期整个落在窗口外，全额退款 / 拒付后一天都不收回。按差值扣，赠送时长照样保留。）
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
| `charge.dispute.closed` | — | 只告警（结果、金额、user、是否有关联的 `StatutoryRefund`）；胜诉后客服按条款 7.7 恢复、按 §3.6 补退撤回款 |
| `customer.subscription.deleted` | 提前结束：`ended_at < 事件自身 items.data[0].current_period_end − 3600` | 按差值扣减会员（§2.3），订阅置 `expired` |

### 2.1 归属：charge / dispute → Stripe 订阅

```go
var stripeSubscriptionByPaymentIntent = func(key, pi string) (subID string, err error)
```

`invoicepayment.Client.List{Payment: {Type: "payment_intent", PaymentIntent: pi}, Status: "paid", Expand: ["data.invoice"]}` 返回 `*Iter`：**必须** `for it.Next()` 后检查 `it.Err()`，`Err != nil` → 返回 err（500 重投）。取第一条的 `invoice.parent.subscription_details.subscription.id`；没有 → `("", nil)`。charge 取 `ch.PaymentIntent.ID`，dispute 取 `d.PaymentIntent.ID`。

### 2.2 `revokeStripeForChargeLoss(ctx, pi, reason, alertTag)`

1. `pi == ""` → 告警"无法归属，人工处理"，`nil`。
2. 查 `subID`：err → 返回 err；空 → 告警"非订阅扣款"，`nil`。
3. `withDeadlockRetry` 跑 `revokeStripeSubscriptionInTx`；`found=false` → 取远端订阅建墓碑行（§1.3）。
4. 取消（共用 `cancelStripeSubscriptionIfLive(subID, reason)`）：取远端订阅（`stripeFetchSubscription`），`resource_missing` 或状态 `canceled` / `incomplete_expired` → 跳过；否则 `stripeCancelSubscription`（新 seam，`prorate=false`、`invoice_now=false`、`cancellation_details.comment=reason`）。返回 `resource_missing` → 视为完成并告警；其他错误 → 再 Get 一次，已 `canceled` 即视为完成（并发取消的另一方先到）；仍未取消 → 返回 err（500 重投；第 3 步因 revoked 短路，只重试取消）。
5. 告警 `[STRIPE-REFUND]` / `[STRIPE-DISPUTE]`：charge/dispute id、金额、user_id、sub、每一步结果。

### 2.3 提前结束的截断

`markStripeSubscriptionDeleted` 增加：事件里 `ended_at < items.data[0].current_period_end − 3600` 时（后台立即取消、或 `cancel_at` 设在期中），在同一事务里按"先订阅行、再用户行"加锁：从用户到期里扣掉没给到的那段 `事件周期末 − ended_at`，最低扣到 `ended_at`（按差值扣，赠送时长不误伤），写历史 `"Stripe subscription ended early - sub_…"`。**只截短，不延长**。基准用事件自身的周期末，不用本地 `CurrentPeriodEnd`（扣款重试期间对账会把本地值推到未付周期）。

不受影响的情形：期末自然结束（`ended_at` ≈ 周期末）；扣款重试失败后被取消（此时到期早已过，cover-through 只在 `active` 时发生）。

## 3. 撤回执行器（14 天折算，后台触发）

### 3.1 一次撤回请求 = 一个计划

请求：`{UserUUID, SubscriptionID?（可选，缺省取该用户最新一条非 revoked 的 Stripe 订阅）, NoticeAt（必填）, Mode: withdrawal|termination, OperatorID, Source}`。

`NoticeAt` = 用户提出撤回的时刻（邮件时间等），由客服填写；窗口和已用天数都按它算，不按执行时刻。`NoticeAt` 晚于当前时间 → 拒绝。`mode=termination` 时 `NoticeAt` 必须在当前时间前 48 小时内（防止倒填时刻多退）。

计划由两类条目组成，每条对应一张 invoice：
1. **主条目**：该订阅里 `paid_at ≤ NoticeAt` 的最新一张已入账 invoice（候选取 `SubscriptionCredit WHERE provider=stripe AND original_transaction_id=subID ORDER BY id DESC`，逐张取 invoice 核对 `paid_at`）。不用 `ProviderLatestRef`（重放旧 invoice 会把它拨回去）。没有 `paid_at ≤ NoticeAt` 的 → 拒绝（通知早于任何付款，不是撤回）。按 §3.2 折算。
2. **通知后条目**：同一订阅里 `paid_at > NoticeAt` 的已付 invoice（客服处理晚了、期间又续费）→ 各自**全额**退（合同在通知时已结束，这笔不该扣）。**从 Stripe 枚举**（`invoice.List{Subscription, Status: "paid"}`，迭代并检查 `Err()`），不从本地 `SubscriptionCredit`——入账失败 / 延迟的 invoice 也要算进来。枚举在新建、每次续跑、以及第 5 步取消之后各做一次，缺的条目追加到同一 RequestID。

### 3.2 主条目的资格与金额（纯函数，B 期用户按钮复用）

```go
type withdrawTarget struct {             // seam stripeInvoiceForWithdraw(key, invoiceID)
    InvoiceID, PaymentIntentID, Currency string
    PaidAmount int64                       // status=paid 那条 invoice payment 的 amount_paid（不是 invoice.amount_paid）
    PaidAt, PeriodStart, PeriodEnd int64
    CreditKind string                       // 本地 SubscriptionCredit.Kind
    HasConsent bool                         // 本地有该订阅的 SubscriptionConsent
}
func quoteStripeWithdrawal(t withdrawTarget, noticeAt int64, mode string) withdrawQuote
```

- **取 invoice**：`invoice.Get` 加 `AddExpand("payments")`；取 `status == "paid"` 的 payment，用它的 `amount_paid` 与 `payment.payment_intent`（裸 ID）；`status_transitions.paid_at`；周期与 `extractStripeInvoiceFacts` 同一取法。
- **合格（仅 `mode=withdrawal`）**：`CreditKind == "purchase"`（新订阅首付；换档 = 新订阅），或 `CreditKind == "renewal"` 且周期 ≥ 360 天（年付续费）；月付续费不合格。窗口：付款日 D = `paid_at` 的 UTC 日期，`NoticeAt < D+16 日 00:00 UTC`（第 14 天结束再留 1 天余量）。
- **`mode=termination`**（条款 8.3：我们主动终止）：跳过上面的合格性。
- **已用天数**：使用起点 `u = max(PeriodStart, PaidAt)`（扣款重试导致晚付时，没服务的天数不算用户的）；`TotalDays = max(1, round((PeriodEnd − u)/86400))`；`UsedDays = clamp(floor((NoticeAt − u)/86400) + 1, 1, TotalDays)`（含付款当天，按 24 小时计）。`NoticeAt ≥ PeriodEnd` → 退款 0。
- **金额**：`PaidAmount × (TotalDays − UsedDays) / TotalDays`，向下取整。
- **无同意记录**：仅 `mode=withdrawal` 时 → `FullRefund=true`，退 `PaidAmount`（没有折算依据），执行时告警。`mode=termination` 不看同意，照常折算。
- `PaidAmount == 0` 或算出 0：不退款，但收回与取消照做（withdrawal 模式下 `PaidAmount == 0` 直接不合格：没有可退的钱，用户关续费即可）。

### 3.3 `StatutoryRefund` 表

```go
type StatutoryRefund struct {
    ID uint64; CreatedAt, UpdatedAt int64
    RequestID string `index`                  // 同一请求的所有条目共享
    UserID uint64 `index`
    ProviderSubscriptionID string
    InvoiceID string `index`
    ActiveInvoiceID *string `uniqueIndex`       // = InvoiceID（pending/done 时），abandoned 时置 NULL——一笔付款同时只能有一条有效处理；作废后可重新处理
    PaymentIntentID string `index`
    Kind string                                 // primary | post_notice
    Amount int64; Currency string; UsedDays, TotalDays int; FullRefund bool
    NoticeAt int64; Mode string                 // withdrawal | termination
    StripeRefundID string
    RefundNote string                           // 跳过退款的原因（charge 已退 / 拒付中 / 金额 0）
    Status string                               // pending | done | abandoned
    OperatorID uint64; Source string             // admin（B 期加 user）
}
```

**每个用户同时最多一个未完成请求**（存在 `pending` 行即视为有未完成请求）。

### 3.4 `executeStripeWithdrawal(ctx, req)`

1. **续跑或拒绝**：请求带 `request_id` → 续跑那个请求（必须属于该用户且有 `pending` 行）。不带时，该用户有 `pending` 行 →
   - (订阅, NoticeAt, Mode) 与那批行一致 → 续跑那批（不重新判定资格；窗口过期、订阅已 revoked 都不影响）。
   - 不一致 → 返回错误，写明未完成请求的 RequestID 与 invoice，让客服用 `request_id` 续跑或先作废。
   续跑时先重新枚举通知后条目（§3.1）。
2. **新建**：按 §3.1 / §3.2 生成计划；主条目不合格 → 返回原因。新 RequestID，所有条目插 `pending`（唯一索引挡重复）。
3. **逐条退款**（`Amount > 0` 且 `StripeRefundID` 为空），顺序固定：
   1. **先查自家退款**：`stripeFindWithdrawalRefund(pi, invoiceID)` 用 `refund.List{PaymentIntent}` 迭代（`for it.Next()` 后检查 `it.Err()`，出错即中止，**不能**当"没找到"），找 `metadata.center_withdrawal == invoiceID`。找到 → 记下它的 id 与金额，本条完成（崩溃在"退款成功、id 未落库"之间时由此恢复）。
   2. 没找到，再查 charge 状态（取 PaymentIntent 时 `AddExpand("latest_charge")`）：`Refunded` 或 `Disputed` → 不退，写 `RefundNote`，继续（拒付胜诉后 `Disputed` 仍为 true，也跳过——客服人工处理）。否则退款额封顶在 `charge.Amount − charge.AmountRefunded`，被封顶时差额写进 `RefundNote`。
   3. `refund.New{PaymentIntent, Amount, Metadata{center_withdrawal: invoiceID, center_request: RequestID}}`，`Params.SetIdempotencyKey("withdraw-"+invoiceID)`。幂等键 Stripe 只留 24 小时，第 1 小步的查重才是主防线。
   4. **退款 id 立即单独提交**到该行。
4. **收回**：`revokeStripeSubscriptionInTx(reason="Withdrawal within 14 days - <主 invoice>"`；termination 用 `"Service terminated by Overleap - <sub>"`)，已 revoked 短路。先收回再取消：取消触发的 `customer.subscription.deleted` 遇 revoked 短路。
5. **取消**：`cancelStripeSubscriptionIfLive`。之后再枚举一次通知后条目；有新增 → 追加行并回到第 3 步处理它们。
6. 该请求所有行置 `done`；告警频道记一条。

任一步失败返回错误；重提同一请求从第 1 步续跑。执行器在调 Stripe 时从不持有数据库事务，没有锁顺序问题；两个并发执行在 24 小时内用同一幂等键，Stripe 只产生一笔退款。

**作废**：`POST /app/stripe-withdrawals/:request_id/abandon {reason}`：先对每个有 PI 的行跑一次 `stripeFindWithdrawalRefund`，把找到的退款 id 落库；然后若任一行有退款 id，补做收回与取消（避免"钱退了、会员还在"）；然后把 `pending` 行置 `abandoned`、`ActiveInvoiceID=NULL`（已退的款不撤回，记录保留）；返回并告警已完成的步骤（各行退款 id、是否已收回、是否已取消）。用于卡死的请求（例如 Stripe 持续拒绝退款），之后人工处理或重新发起——重新发起时 metadata 查重会找到已发出的退款，不会重复退。

### 3.5 与 §2 webhook 的交互

- `revokeStripeForChargeLoss` 与部分退款告警：先按 PI 查 `StatutoryRefund`（只认 `status IN (pending, done)`）。有 → 这是我们自己的撤回退款：收回原因用撤回原因，告警标 `[WITHDRAWAL]` 而不是 `[STRIPE-REFUND]`；收回与取消照常执行（都幂等）。
- `cancelStripeSubscriptionIfLive`：Cancel 返回非 404 错误 → 再 Get 一次，状态已 `canceled` 即视为完成（并发取消的另一方先到）。

### 3.6 入口（A 期只给后台）

- `/app`（`AdminRequired()`，即超管同步执行，审批行与 `order_refund` 一样是留痕）：
  - `GET /app/users/:uuid/stripe-withdrawal?notice_at=&subscription_id=&mode=` → 计划预览（每条的金额、天数、是否全额、原因）。
  - `POST /app/users/:uuid/stripe-withdrawal {notice_at, subscription_id?, mode, reason, request_id?}` → `SubmitApproval("stripe_withdrawal", …)`。
  - `POST /app/stripe-withdrawals/:request_id/abandon`。
- `worker_integration.go` 注册 `RegisterApprovalCallback("stripe_withdrawal", …)`；`logic_approval.go` 的 `actionDisplayNames` 加"Stripe 撤回退款"。
- MCP（`tools/kaitu-center`）：`quote_stripe_withdrawal`、`withdraw_stripe_subscription`（可带 `request_id` 续跑）、`abandon_stripe_withdrawal`，照 `admin-orders.ts` 的退款工具写；`notice_at` 必填。
- 客服流程写进 `docs/customer-service/README.md`：收到撤回请求 → 记下用户消息时间 → quote → 回邮件确认金额 → withdraw（14 天内完成）。**不要在 Stripe 后台手工部分退款**。条款 8.3 的主动终止用 `mode=termination`。必须**直接**跑执行器结束服务，不要先用别的方式停掉账号再补跑（超过 48 小时执行器会拒绝原时刻，改用当前时刻会多收用户没享受服务的天数）。执行器因拒付跳过退款的请求（`RefundNote` 记了拒付）：拒付结案判我们胜、或只是询问时，按已存的金额在用户通知后 14 天内人工补退（条款 7.4），并把补退的退款 id 写进该行 `RefundNote`；`charge.dispute.closed` 加进告警，让客服知道结案。

## 4. 结账同意（折算的依据）

`api_stripe_checkout` 加：
- `consent_collection.terms_of_service = "required"`；
- `custom_text.terms_of_service_acceptance.message`（v1，≤1200 字符，markdown 链接）：

> I agree to the [Terms of Service](https://overleap.io/terms). My subscription starts now and renews automatically at the regular price shown (or a new price you tell me about in advance) until I cancel. I ask for the service to begin immediately. I understand that if I withdraw within 14 days of my first payment, or of an annual renewal, my refund will be reduced for the days already used.

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
- 新 §8.3(c) 改为："(c) Unused subscription time will not be refunded, except as set out in Section 7, at the end of this Section, or where required by law;"
- 新 §8.3 末尾加：
  > If we terminate under Section 8.2(b) or 8.2(d), we will refund the unused part of any prepaid period for subscriptions billed by us. For subscriptions bought through the App Store, you can request a refund from Apple.
- 新 §11.1 联系方式加商家邮寄地址：`Overleap LLC, {ADDRESS}`。
- 文末 "Model withdrawal form"（CCR Sch 3 措辞）：
  > To: Overleap LLC, {ADDRESS}, support@overleap.io
  > I hereby give notice that I cancel my contract for the supply of the following service: Overleap subscription.
  > Ordered on: ___ · Name: ___ · Address: ___ · Account email: ___ · Date: ___

**`{ADDRESS}` 由用户提供；拿到之前条款不发布。**

### 6.2 隐私政策与删号页

- `privacy-policy.md` 1.3 末尾加 "and the billing country your payment provider gives us"。4.3 后补："This includes records of the subscription terms you agreed to at checkout (time, country and wording), kept as proof of your consent." 更新日期。
- `delete-account.md`：更新日期。第 41 行 → "Unused subscription time is **not** refunded and cannot be restored afterwards. If you're within 14 days of your first payment for a subscription billed by us (not through the App Store), or of an annual renewal, email support@overleap.io before deleting your account to withdraw with a prorated refund (Terms, Section 7)." §4 保留数据清单加"Subscription consent records — kept with payment records"。§5 加："Deleting your account does not cancel a renewing subscription. Cancel it first on your account page, or in your App Store settings for iPhone purchases."

### 6.3 站点 messages（20 个语言）

- `help.json` `billing.items.refund.answer`（英文基准）："You can withdraw and get a refund for the days you haven't used within 14 days of your first payment for a subscription billed by us (not through the App Store), or of an annual renewal. Email support@overleap.io. App Store purchases are refunded by Apple."
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
| 16 | quote 表驱动 | 首付 / 年付续费合格；月付续费、窗口外、0 元不合格；窗口截止 D+16 00:00 前后；第 13 天通知、第 16 天执行 → 合格且 used=13；晚付按 `max(start, paid_at)`；无同意：withdrawal 全额、termination 仍折算；`NoticeAt` 在未来 / 早于任何付款 → 拒；`NoticeAt ≥ PeriodEnd` → 0 | 改 `+1`、改窗口、改起点、termination 也全额 |
| 17 | invoice 解析 | 先失败后成功的两条 payment 取 paid 那条及其 amount_paid | 取第一条 |
| 18 | 执行器正常路径 | 退款金额、metadata、幂等键；收回；取消；done | — |
| 19 | 执行器续跑 | 在退款后 / 收回后 / 取消后失败各一次 → 续跑完成，不重复退款、不重判资格（窗口已过仍续跑） | 续跑前先判资格 |
| 20 | 超 24 小时续跑 | 退款已在 Stripe、行上无 id → 按 metadata 找到，不新建；全额条目在"退款成功、id 未落库"时崩溃 → 续跑记下退款 id 而非跳过备注；查重迭代器出错 → 中止，不新建 | 去掉查重 / 查重挪到 charge 检查之后 / 忽略 `it.Err()` |
| 21 | 执行中途到达 deleted 事件 | 遇 revoked 短路，历史是 "Withdrawal…" | 把取消挪到收回前 |
| 22 | 请求不一致 | 有 invoice X 的 pending 请求，再提 Y（或不同 mode）→ 报错，X 未被续跑 | 按用户续跑 |
| 23 | 作废 | 同一 invoice：退款后卡住 → abandon（补做收回与取消、返回报告）→ 重新发起 → metadata 找到已发退款，不重复退；退款已发但 id 未落库 → abandon 仍查到并收回 | 作废不清 ActiveInvoiceID / 不补收回 / 作废不查 metadata |
| 24 | 通知后被扣的续费 | 第 13 天通知、第 30 天续费、第 31 天执行 → 主条目折算 + 续费全额；该续费未入账（`invoice.paid` 失败）也被枚举到；计划建好后、取消前又扣一笔 → 追加并全额退 | 从本地 credit 枚举 / 不重新枚举 |
| 25 | charge 已退 / 拒付中 / 部分已退 | 前两者不调退款、写 RefundNote、收回与取消照做；部分已退 → 封顶到剩余额 | 去掉检查 / 去掉封顶 |
| 26 | 全额撤回时 webhook 先到 | 历史只有一条且是撤回原因；告警标 `[WITHDRAWAL]` | 去掉按 PI 查 StatutoryRefund |
| 27 | 并发取消 | Cancel 返回非 404 错误、再 Get 为 canceled → 成功 | 不再 Get |
| 28 | termination 模式 | 跳过合格性、折算；0 元只收回与取消；`NoticeAt` 超出 48 小时 → 拒 | termination 也全额 |
| 29a | `charge.dispute.closed` | 告警含结果与关联的 `StatutoryRefund`；200；无写入 | 从事件 switch 删掉该分支 |
| 29 | 审批与续跑 | 注册回调、显示名；POST 建审批单；带 `request_id` 续跑；§3.5 查找忽略 abandoned 行 | 不注册 / 查找不过滤状态 |
| 30 | 结账参数 | `consent_collection`、`custom_text` 存在 | 删参数 |
| 31 | `checkout.session.completed` | 写 `SubscriptionConsent`；重投不重复；国家为空可写 | — |
| 32 | 钱包路由守卫 | 生产 `SetupRouter()` 枚举 `/api/wallet*`，≥7；overleap 全部 `ErrorNotSupported`；kaitu `GET /api/wallet` 不是；无用户 → 拒 | 去掉任一路由的门 |
| 33 | `ProcessOrderRefund` / `api_admin_refund_order` 拒 overleap | 无写入、无审批单；含已有 pending 审批被执行 | 删门 |
| 34 | 返现收款人无钱包 | 不入账 | 删门 |
| 35 | 契约 | golden 含 `wallet`；webapp 断言一致 | webapp overleap 改 `wallet:true` |
| 36 | webapp 删号对话框 | overleap 无钱包行，kaitu 有 | — |
| 37 | MCP 工具 | 参数校验（`notice_at` 必填）、调用路径 | — |

环境：worktree 已拷 `center/config.yml`；DB 走 devdb（当前 pve）；`-v` 下 0 SKIP；handler 测试单跑与全量各一次。webapp `yarn test`；`tools/kaitu-center` 测试；`sites/overleap` lint / build。

## 8. 文档

- `api/CLAUDE.md` 支付段：Stripe 收回规则、revoked 终态三道门、提前结束截断、撤回执行器与续跑、"不要在 Stripe 后台手工部分退款"。
- 上位 spec §8 分期表：A 期内容同步为 v6。
- 内部说明：已用天数按 24 小时计，从 `max(周期起点, 付款时刻)` 起；后台立即取消时，用户若有超出该周期的赠送时长则不截断（"赠送时长不误伤"，有意为之）；拒付胜诉后 charge 的 `Disputed` 仍为 true，执行器会跳过退款，需人工处理；作废后重新发起时，已发出的退款按 metadata 复用，即使新的 `NoticeAt` 算出不同金额也不补差（没有补差路径，需要时人工处理）。

## 9. 上线清单（顺序有依赖）

1. ~~用户提供邮寄地址~~ ✅ 2026-10-08：主体改为 Wordgate LLC（Overleap 运营公司 = Stripe 收单主体），地址 30 N Gould St Ste R, Sheridan, WY 82801。
2. ✅ 2026-10-08 Stripe Dashboard（acct_1RjVqKDe0r2BKV9U, Wordgate LLC）：ToS URL = `https://overleap.io/terms`；webhook `we_1UCbaTDe0r2BKV9UUC24ftgm` 补订 `charge.dispute.closed`（共 7 个事件）；Portal 默认配置本就是期末取消。**未开退款收据邮件**——账户与 NextPay / WordGate 共用，账户级开关会波及别家客户。共用账户还引出代码修复：别家订阅的退款 / 拒付不得取消（`revokeStripeSubscription` 的 `ours`）。
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
10. 条款 8.3 主动终止时 App Store 订阅只指向 Apple；Apple 拒退时我们是否需要补偿未用部分（CRA Sch 2）。
11. 条款 4.2 违规终止"不退款"。

## 11. 不做

- 用户自助撤回按钮、`DataSubscription` 撤回字段、试用、提醒邮件（B 期）。
- 拒付胜诉自动恢复。
- 邀请码跨品牌绑定。
- kaitu 品牌任何行为变化（钱包门对 kaitu 恒放行；kaitu 不走 Stripe）。

## 12. 实施记录（2026-10-08）

实施中由安全审查 / 测试纠正、与上文设计不同的地方（以代码为准）：

1. **扣减量**：收回与提前结束截断都只扣 `Subscription.PaidThrough`（已发放付费时长覆盖到的时刻）以内的部分，见 §1.1 的更正。PaidThrough 在入账、对账 cover-through 时按**实际发放的时长**累加（入账晚到时按 invoice 周期末记会少扣），扣减后回收（避免两条路径重复扣）；为 0 的老行退回 `CurrentPeriodEnd`。
2. **revoked 第四道门**：`subscription.updated` 的写带 `status<>revoked`（先读后写之间并发收回会被覆盖）；对账 cover-through 改为事务内先锁订阅行再判断。
3. **执行器**：合格性按 Stripe invoice 的 `billing_reason` 判（`subscription_create` = 首付，`subscription_cycle` + 周期 ≥360 天 = 年付续费），不读本地 `SubscriptionCredit.Kind`——primary 与通知后条目统一从 Stripe 已付 invoice 列表取。新增请求占位锁（primary 行 `LockedUntil`）；退款额 = min(计划额 − charge 上已有退款, charge 剩余)；退款返回失败 / 取消状态不当成功；幂等键带金额。
4. **钱包路由**是 8 个（不是 7 个）。
5. **按设计保留、写明**：任一张 invoice 全额退款 / 拒付都终止整个订阅（"全额退款 = 终止合作"）；无同意记录的订阅撤回全额退。
6. **后续（不在 A 期）**：Apple 的 `revokeSubscription`（开途 + overleap 共用）有同样的叠加入账收回放行问题，待决定是否修；对账推进 `CurrentPeriodEnd` 后续费入账的 `priorPeriodEnd` 偏大、可能少算赠送时长（原有问题）。
7. **终审修正**：`subscription.deleted` 截断不用 `CurrentPeriodEnd` 兜底（PaidThrough 为 0 的老行只告警不截——截断走普通续费失败，按可能已被对账推远的周期末截会误扣赠送时长）；预览路由放到 staff 组（客服有 `orders` 读权限可报价），执行 / 作废仍只给超管；同意记录按 `client_reference_id` 显式匹配，空值不落记录。
