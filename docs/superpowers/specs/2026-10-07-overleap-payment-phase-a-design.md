# Overleap 支付 A 期：堵漏 + 撤回执行器 — 实施设计（v2）

日期：2026-10-07 · 分支 `feat/overleap-trial-payment` · 上位 spec：`2026-10-07-overleap-trial-payment-design.md`
v1 → v2：经两路 review（后端 6/10、文案 4/10）修订。主要变化：撤回执行器（后台触发）与结账同意从 B 期提前到 A 期——条款承诺的"折算退款并结束会员"必须在 A 期就能执行，折算也必须有"用户要求立即开始"的同意记录做依据。

**A 期目标**：(1) 关掉今天就存在的退款 / 钱包后门；(2) 条款改成最终政策，且政策里的每一句都有代码能兑现。不做试用、不做用户自助按钮（B 期）。

## 0. 现状（已核对代码与生产库，2026-10-07）

| 项 | 现状 | 证据 |
|---|---|---|
| Stripe `charge.refunded` | 只发 Slack，会员不收回、订阅不取消 | `api/logic_stripe.go` `recordStripeRefundAlert` |
| Stripe `charge.dispute.created` | 只发 Slack | `recordStripeDisputeAlert` |
| Stripe 后台"立即取消" | `customer.subscription.deleted` → 只置 `expired`，`expired_at` 不动，会员用到期末 | `markStripeSubscriptionDeleted` |
| 迟到 `invoice.paid` | revoked 状态不复活，但**时长照加** | `creditStripeInvoice` |
| 每日对账 | cover-through 的 `UPDATE users` 不看订阅是否 revoked | `worker_subscription_reconcile.go:255` |
| 后台 / MCP `refund_order` | 退款进钱包，不看品牌 | `ProcessOrderRefund`（唯一执行点，`logic_order.go:124`，唯一调用方 `logic_approval_callbacks.go:505`） |
| 钱包 `/api/wallet/*`（7 个路由） | 后端不看品牌 | `route.go:221-235` |
| 分销返现入钱包 | 不看收款人品牌 | `addCashbackIncomeInTx`（`logic_wallet.go:165`） |
| 结账 | 无条款同意、无"立即开始"确认 | `api_stripe.go` Checkout 参数 |
| 条款 / 帮助页 | §7 "7 天可退到钱包"、§8 加密货币提现；帮助页 "email us and we'll sort it out" | `sites/overleap/public/legal/terms-of-service.md`、`messages/*/help.json`（20 个语言） |
| 生产数据 | overleap 7 个用户；订单 / 钱包余额 / 提现账户 / 分销商 / 被邀请均为 0；Stripe 订阅 0；Apple 订阅 1（expired） | 2026-10-07 只读查询 |

所有改动对现有数据零影响，无迁移。

## 1. 统一的 Stripe 收回原语

新函数（`logic_stripe_revoke.go`），**不复用** Apple 的 `revokeSubscription`（它不锁订阅行、加锁顺序与入账相反、原因文案写死中文）：

```go
// revokeStripeSubscription 收回一条 Stripe 订阅撑着的会员并置 revoked。幂等。
// 锁顺序与 creditStripeInvoice 一致：先订阅行，再用户行。调用方包 withDeadlockRetry。
func revokeStripeSubscriptionInTx(ctx, tx, providerSubID, reason string) (revoked bool, err error)
```

1. `SELECT … FOR UPDATE` 订阅行（provider=stripe）。找不到 → `(false, nil)`；已是 `revoked` → `(false, nil)`。
2. `FOR UPDATE` 用户行。规则同 Apple：用户到期落在 `(now, sub.CurrentPeriodEnd]` 才砍到 now（叠加的赠送时长不误伤）；写 `UserProHistory{Type: VipRefund, Days: -n, Reason: reason}`。
3. 订阅置 `revoked`、`auto_renew=false`。

`reason` 一律英文（overleap 用户在 `/api/user/pro-histories` 看得到），例：`"Stripe full refund - ch_…"`、`"Stripe dispute - dp_…"`、`"Withdrawal within 14 days - in_…"`。

**revoked 是终态，三处补门**：
- `creditStripeInvoice`：锁到订阅行后，若非首张且 `sub.Status == "revoked"` → 告警 `[STRIPE-CREDIT] invoice on revoked sub` 并 `return nil`，不加时长、不改订阅行。
- `reconcileStripeSubscription` 的 cover-through：`UPDATE users … WHERE id=? AND expired_at<? AND NOT EXISTS (SELECT 1 FROM subscriptions WHERE id=? AND status='revoked')`。
- 已有：`applyStripeSubscriptionUpdate` / `markStripeSubscriptionDeleted` 对 revoked 短路。

**墓碑行**：本地没有订阅行（首张 invoice 入账曾失败）时，收回路径用 Stripe 订阅的 `metadata.user_uuid`（结账时写入，`api_stripe.go:90`）建一行 `status=revoked` 的订阅。之后 Stripe 重投那张 invoice，会命中上面 `creditStripeInvoice` 的门，不会补发整期会员。`user_uuid` 缺失 → 告警，不建行。

## 2. Stripe 事件 → 收回 + 取消

| 事件 | 条件 | 动作 |
|---|---|---|
| `charge.refunded` | 全额：`ch.Refunded \|\| (ch.Amount > 0 && ch.AmountRefunded >= ch.Amount)` | 收回 + 取消 Stripe 订阅 + 告警 |
| `charge.refunded` | 部分 | 只告警（运营补偿或我们自己的撤回退款，§3 已处理会员）——不变 |
| `charge.dispute.created` | 任意（含 `warning_needs_response` 询问） | 收回 + 取消 + 告警 |
| `charge.dispute.closed` | — | 不自动处理；胜诉后客服按条款 7.7 恢复（运营说明） |
| `customer.subscription.deleted` | `ended_at < sub.CurrentPeriodEnd`（后台立即取消，而非期末自然结束） | **会员截到 `ended_at`**（同 `(now, periodEnd]` 规则），订阅置 `expired`。这是"部分退款 + 立即取消"的手工路径兜底，后台没走执行器也不会漏收 |

### 2.1 归属：charge / dispute → Stripe 订阅

```go
var stripeSubscriptionByPaymentIntent = func(key, pi string) (subID string, err error)
```

- `invoicepayment.Client.List{Payment: {Type: "payment_intent", PaymentIntent: pi}, Status: "paid", Expand: ["data.invoice"]}`。返回的是 `*Iter`：**必须** `for it.Next()` 后检查 `it.Err()`；`Err != nil` → 返回 err（500 重投）。只取第一条的 `invoice.parent.subscription_details.subscription.id`；没有 → `("", nil)`。
- charge 取 `ch.PaymentIntent.ID`，dispute 取 `d.PaymentIntent.ID`（v82 两者 JSON 里的裸 ID 都能反序列化进 `.ID`，已核对）。

### 2.2 处理流程 `revokeStripeForChargeLoss(ctx, pi, reason, alertTag)`

1. `pi == ""` → 告警"无法归属，人工处理"，`nil`。
2. 查 `subID`：err → 返回 err；空 → 告警"非订阅扣款"，`nil`。
3. `withDeadlockRetry`：`revokeStripeSubscriptionInTx`。没有本地行 → 第 4 步拿到远端订阅后建墓碑行。
4. 取远端订阅（复用已有 seam `stripeFetchSubscription`）：`resource_missing` → 视为已取消；状态 `canceled` / `incomplete_expired` → 跳过取消；否则 `stripeCancelSubscription(subID, reason)`（新 seam，`prorate=false`、`invoice_now=false`、`cancellation_details.comment=reason`）。取消返回 `resource_missing` → 视为完成并告警；其他错误 → 返回 err（500 重投，第 3 步因 revoked 跳过，只重试取消）。
5. 告警 `[STRIPE-REFUND]` / `[STRIPE-DISPUTE]`：charge/dispute id、金额、user_id、sub、每一步结果。

seam 拆成"取"和"取消"两个，"已取消 → 跳过"分支可测。

## 3. 撤回执行器（14 天折算，后台触发）

### 3.1 资格与金额（纯函数，B 期用户按钮复用）

```go
type withdrawQuote struct {
    Eligible  bool; Reason string          // 不合格的原因（英文，给客服看）
    InvoiceID, PaymentIntentID, Currency string
    AmountPaid, RefundAmount int64        // 最小货币单位
    UsedDays, TotalDays int; WindowEndsAt int64
}
func quoteStripeWithdrawal(credit SubscriptionCredit, inv stripeInvoiceForWithdraw, now int64) withdrawQuote
```

- 对象：订阅的最近一张已入账 invoice（`sub.ProviderLatestRef`），从 Stripe 取 `amount_paid`、`currency`、`status_transitions.paid_at`、付款 PI（`invoice.payments` 展开 `data.payment.payment_intent`）、计费周期（与 `extractStripeInvoiceFacts` 同一取法：period end 最大的那条 line）。seam：`stripeInvoiceForWithdraw(key, invoiceID)`。
- 合格的付款：该 invoice 的 `SubscriptionCredit.Kind == "purchase"`（新订阅的首付；换档 = 新订阅，Billing Portal 换档已被对账哨兵禁止），**或** `Kind == "renewal"` 且周期 ≥ 360 天（年付续费）。月付续费不合格。
- 窗口：`now <= paid_at + 14×86400`。
- 金额：`TotalDays = round((end-start)/86400)`；`UsedDays = min(TotalDays, floor((now-start)/86400) + 1)`（含付款当天）；`RefundAmount = AmountPaid × (TotalDays-UsedDays) / TotalDays`，整数向下取整。`AmountPaid == 0`（全额优惠券）→ 不合格。B 期试用转付费时首张付费 invoice 的周期从试用结束起算，试用天数天然不计入。
- 已有 `StatutoryRefund` 记录 → 不合格（"already withdrawn"）。

### 3.2 执行

新表：
```go
type StatutoryRefund struct {
    ID uint64; CreatedAt, UpdatedAt int64
    UserID uint64 `index`; ProviderSubscriptionID string
    InvoiceID string `uniqueIndex`           // 一笔付款只能撤回一次
    Amount int64; Currency string; UsedDays, TotalDays int
    StripeRefundID string; Status string     // pending | done
    OperatorID uint64; Source string         // "admin"（B 期加 "user"）
}
```

`executeStripeWithdrawal(ctx, userID, operatorID, source)`：
1. 找用户最新一条未 revoked 的 Stripe 订阅；取 invoice；`quote`。不合格 → 返回带原因的错误。
2. 插 `StatutoryRefund{status=pending}`（唯一索引挡并发 / 重复）。已有 `pending`（上次中途失败）→ 继续用它，不重算金额。
3. Stripe 退款：`refund.New{PaymentIntent, Amount, Metadata{center_withdrawal: invoiceID}}`，幂等键 `withdraw-<invoiceID>`（重试不会退两次）。seam `stripeCreateRefund`。
4. 取消订阅（同 §2.2 第 4 步）。
5. `revokeStripeSubscriptionInTx(reason="Withdrawal within 14 days - <invoice>")`。
6. `StatutoryRefund` 置 `done`、写 refund id。告警频道发一条记录。

任一步失败返回错误，操作员重试从第 2 步续上（pending 行 + 幂等键 + revoked 短路保证不重复）。Stripe 随后发来的 `charge.refunded` 是部分退款，只告警；`customer.subscription.deleted` 遇到 revoked 短路。

### 3.3 入口（A 期只给后台）

- `GET /app/users/:uuid/stripe-withdrawal` → quote（客服先告诉用户金额）。
- `POST /app/users/:uuid/stripe-withdrawal` → `SubmitApproval("stripe_withdrawal", …)`，执行回调 = `executeStripeWithdrawal`（对标 `order_refund`）。
- MCP（`tools/kaitu-center`）：`quote_stripe_withdrawal`、`withdraw_stripe_subscription` 两个工具，照 `admin-orders.ts` 的 refund 工具写。
- 运营说明写进 `docs/customer-service/` 内部文档（见 §6）：收到撤回请求 → quote → 回邮件确认金额 → withdraw；不要在 Stripe 后台手工部分退款（那样会员只会在立即取消时被截断，见 §2 兜底）。

## 4. 结账同意（折算的法律依据）

`api_stripe_checkout` 的 Checkout 参数加：
- `consent_collection.terms_of_service = "required"`；
- `custom_text.terms_of_service_acceptance.message`（v1，≤1200 字符，Stripe 支持 markdown 链接）：

> I agree to the [Terms of Service](https://overleap.io/terms). My subscription starts now and renews automatically at the price shown until I cancel. I ask for the service to begin immediately, and understand that if I withdraw within 14 days of a payment, my refund will be reduced for the days already used.

`checkout.session.completed`（现在只打日志）改为：`consent.terms_of_service == "accepted"` 时写

```go
type SubscriptionConsent struct {
    ID uint64; CreatedAt int64
    UserID uint64 `index`                      // client_reference_id = user UUID → id
    CheckoutSessionID string `uniqueIndex`
    ProviderSubscriptionID string; TextVersion string // "2026-10-v1"
    AcceptedAt int64                             // event.created
    Country string                               // customer_details.address.country
}
```

保留期：不自动删（≥3 年，加州 ARL）。用户找不到 → 告警并 `nil`（同意记录缺失不阻断入账）。

**部署前置（硬性）**：Stripe Dashboard → Settings → Public details 填 Terms of service URL，否则带 `consent_collection` 的 Checkout 创建直接报错，购买全断。顺序：先填 URL → 再部署 api。

## 5. 钱包对 overleap 关闭

- `BrandConfig.Wallet bool`（kaitu true / overleap false），导出进契约 `brands.<b>.wallet`；webapp 契约测试断言 `features.wallet === contract.brands[b].wallet`（web/ 只比品牌 id 集合，不受影响——已核对）。
- 中间件 `WalletRequired()`：`ReqUser(c) == nil` → `ErrorNotLogin`；`!Wallet` → `ErrorNotSupported`。7 个 `/api/wallet*` 路由都挂在 `AuthRequired()` 之后（读用户品牌：`AuthRequired` 对 admin 免品牌检查，请求品牌对 admin 不可信）。
- `ProcessOrderRefund` 锁用户行后拒绝无钱包品牌；`api_admin_refund_order` 预校验同样拒绝，返回中文说明（"overleap 订单：Stripe 用撤回工具；Apple 由 Apple 退款"），不建审批单。
- `addCashbackIncomeInTx`：收款人品牌无钱包 → 不入账，告警，返回 nil（返现侧本就非致命）。
- **不做（记为后续）**：邀请码跨品牌绑定（overleap 用户绑了 kaitu 分销商的码，返现进 kaitu 钱包）——品牌隔离问题，与退款无关，生产为 0。

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
> 7.6 **Other refunds**: Apart from Sections 7.2–7.5, payments are not refundable, including for partly used periods. If the Service is not provided as described or with reasonable care, you may be entitled to a remedy such as a price reduction under consumer law. Nothing in these Terms limits rights you have under the laws of your country.
>
> 7.7 **Chargebacks**: If a payment is fully refunded or disputed with your bank or card issuer, the subscription it paid for ends and your access stops. If a dispute is resolved in our favour, contact us and we will restore your access for the rest of the paid period.

- 删除 §8 "Wallet and Withdrawals"，后续节号前移（§9→§8 …）。已核对：全站只有 ToS 4.3→4.2、隐私 4.2→1.4 两处带节号的引用，都不受影响。
- 新 §8.1：
  > **User Termination**: You may terminate this Agreement at any time by deleting your account. Deleting your account does not cancel a renewing subscription — cancel it first on your account page (or in your App Store settings), or you will continue to be charged. Unused time is not refunded on deletion; if you are entitled to a refund under Section 7.2, request it before deleting your account.
- 新 §8.3 末尾加："If we terminate under Section 8.2(b) or 8.2(d), we will refund the unused part of any prepaid period."（执行：客服用 MCP 后台退款 + 截断会员，量极小，人工）。
- 文末加 "Model withdrawal form"（CCR Sch 3 模板）：
  > To Overleap LLC, support@overleap.io: I hereby give notice that I withdraw from my contract for the following service: Overleap subscription. Ordered on: ___ · Name: ___ · Account email: ___ · Date: ___

### 6.2 `delete-account.md`

- 更新日期。第 41 行 → "Unused subscription time is **not** refunded and cannot be restored afterwards. If you're within 14 days of your first payment for a subscription on our website, or of an annual renewal, email support@overleap.io before deleting your account to withdraw with a prorated refund (Terms, Section 7)."
- §5 加一条："Deleting your account does not cancel a renewing subscription. Cancel it first on your account page, or in your App Store settings for iPhone purchases."

### 6.3 站点 messages（20 个语言）

- `help.json` `billing.items.refund.answer`（英文基准）："You can withdraw and get a refund for the days you haven't used within 14 days of your first payment for a subscription on our website, or of an annual renewal. Email support@overleap.io. App Store purchases are refunded by Apple."
- `help.json` `billing.items.manage.answer` 末尾加一句："Cancelling stops renewal; it doesn't refund — see “Can I get a refund?”"
- `pricing.json` `faqSubtitle`："Payment, cancelling and what the plan covers."
- 翻译：邮箱、App Store、Apple 原样；de/fr 用法定词（Widerruf / rétractation）但范围必须是"首付 + 年付续费"；ar/fa 邮箱放在句末无尾随标点。

### 6.4 其他

- webapp `DeleteAccountDialog.tsx:148`：`deleteLoseWallet` 只在 `features.wallet` 时显示。
- `.agents/product-marketing-context.md:269` 英文必用词 "7-day refund" → "14-day withdrawal"（只描述 overleap；中文那行是开途的，不动）。
- kb 分支 `docs/customer-service/overleap/04`：退款段改成"有 14 天折算撤回（首付 / 年付续费）；机器人不判资格、不报金额；收集账号邮箱和付款日期 → 转人工"。另在 `docs/customer-service/README.md` 加客服撤回操作流程（§3.3）。

## 7. 测试（先红后绿；每条实现后做变异验证，变异用 scratchpad 备份还原）

| # | 测试 | 断言 | 变异 |
|---|---|---|---|
| 1 | 全额退款 | 到期≈now；sub revoked；`VipRefund` 历史且 Reason 是英文；cancel 被调 | 删 revoke 调用 |
| 2 | 部分退款 | 无任何变化；cancel 未调 | 全额判定改 `AmountRefunded > 0` |
| 3 | 赠送时长保护 | 到期 > 周期末 → 不动到期，sub revoked | 去掉 `<= CurrentPeriodEnd` |
| 4 | 取消失败后重投 | 第一次 500；第二次不重复写历史、cancel 再调 | 去掉 revoked 短路 |
| 5 | 远端已取消 | 不调 cancel | 去掉状态跳过 |
| 6 | cancel 返回 resource_missing | 200 | 把 resource_missing 当错误 |
| 7 | invoice payments 迭代器出错 | 500 | 忽略 `it.Err()` |
| 8 | 无 PI / 非订阅扣款 | 200，无写入 | — |
| 9 | 本地无订阅行 | 建墓碑行（revoked、正确 user）；cancel 被调 | 不建墓碑 |
| 10 | 墓碑后迟到 `invoice.paid` | 不加时长，告警 | 删 creditStripeInvoice 的 revoked 门 |
| 11 | 收回后迟到的续费 invoice | 同上 | 同上 |
| 12 | 对账 cover-through 遇 revoked | 到期不变 | 删 NOT EXISTS |
| 13 | 先全额退款后拒付 | 第二次不改数据、不调 cancel | — |
| 14 | 拒付 | 同 #1 | 删 dispute 分支 |
| 15 | 后台立即取消（`ended_at` < 周期末） | 到期截到 ended_at；期末自然结束的 deleted 不截 | 删截断 |
| 16 | quote 纯函数表驱动 | 首付 / 年付续费合格；月付续费、超 14 天、0 元、已撤回不合格；天数与金额边界（第 1 天、第 14 天、跨月、整除与取整） | 改 `+1`、改 `<=` |
| 17 | 执行器 | 退款金额与幂等键正确；cancel；revoke；记录 done；重复执行被唯一索引挡；pending 续跑不重算 | 去掉幂等键 / pending 续跑 |
| 18 | 结账参数 | `consent_collection`、`custom_text` 存在 | 删参数 |
| 19 | `checkout.session.completed` | 写 `SubscriptionConsent`；重投不重复 | — |
| 20 | 钱包路由守卫 | 用生产 `SetupRouter()` 枚举 `/api/wallet*`，数量 ≥7；overleap 全部 `ErrorNotSupported`；kaitu `GET /api/wallet` 不是；无用户 → 拒 | 去掉任一路由的门 |
| 21 | `ProcessOrderRefund` / `api_admin_refund_order` 拒 overleap | 无写入、无审批单；含"已有 pending 审批被执行"路径 | 删门 |
| 22 | 返现收款人无钱包 | 不入账 | 删门 |
| 23 | 契约 | golden 含 `wallet`；webapp 断言一致 | webapp overleap 改 `wallet:true` |
| 24 | webapp 删号对话框 | overleap 不显示钱包行，kaitu 显示 | — |

环境：worktree 已拷 `center/config.yml`；DB 走 devdb（当前 pve）；`-v` 下 0 SKIP；handler 测试单跑与全量各一次。webapp `yarn test`；`tools/kaitu-center` 的测试；`sites/overleap` 的 lint / build。

## 8. 文档

- `api/CLAUDE.md` 支付段：Stripe 收回规则、revoked 终态三道门、撤回执行器、"不要在 Stripe 后台手工部分退款"。
- 上位 spec §8 分期表：A 期内容同步为 v2。

## 9. 上线清单（顺序有依赖）

1. **Stripe Dashboard**（用户操作）：填 Terms of service URL；webhook 端点确认订阅了 `charge.refunded`、`charge.dispute.created`、`checkout.session.completed`、`customer.subscription.deleted`；Billing Portal 关闭"立即取消"（只留期末取消）；打开退款收据邮件。
2. 合并 → `make deploy-api`（center-deploy）。
3. `git push origin main:website`（overleap 站条款 / 帮助页）。
4. `webapp/x.y.z-overleap` tag（删号对话框）。
5. MCP 工具：`tools/kaitu-center` 按其 CLAUDE.md 发布。
6. kb 分支合并后按 `scripts/sync-kb.sh` 同步 overleap 知识库。

## 10. 请英国 / 欧盟律师确认（不阻塞 A 期上线，上线后尽快）

1. VPN 是"服务"还是"数字内容"（决定能否折算）。
2. CCR reg 16：确认邮件需含"要求立即开始"的确认（Stripe 收据不含）——可能需要 A 期后补一封我们自己的订阅确认邮件（B 期欢迎邮件里做）。
3. 欧盟撤回按钮（CRD Art 11a，2026-06-19 生效）：A 期只有邮件渠道是否可接受到 B 期上按钮。
4. DMCCA 续费冷静期的触发范围与折算基数。
5. 拒付期间即停止服务是否可接受。
6. 条款 12.3 适用法律未定义、10.2 责任上限。

## 11. 不做

- 用户自助撤回按钮、`DataSubscription` 撤回字段、试用、提醒邮件（B 期）。
- 拒付胜诉自动恢复。
- 邀请码跨品牌绑定。
- kaitu 品牌任何行为变化（钱包门对 kaitu 恒放行；kaitu 不走 Stripe）。
