# Overleap 支付 A 期：堵漏 — 实施设计

日期：2026-10-07 · 分支 `feat/overleap-trial-payment` · 上位 spec：`docs/superpowers/specs/2026-10-07-overleap-trial-payment-design.md` §3.2、§8
目标：今天就存在的退款 / 钱包后门全部关掉，条款改成最终退款政策。不引入试用、不做自助撤回按钮（B 期）。

## 0. 现状（已核对代码与生产库，2026-10-07）

| 项 | 现状 | 证据 |
|---|---|---|
| Stripe `charge.refunded` | 只发 Slack，会员不收回、订阅不取消 | `api/logic_stripe.go` `recordStripeRefundAlert` |
| Stripe `charge.dispute.created` | 只发 Slack | `recordStripeDisputeAlert` |
| 后台 / MCP `refund_order` | 退款打进钱包，不看品牌；overleap 的 Apple 订单也能退 | `api_admin_order_refund.go` → approval → `ProcessOrderRefund`（唯一执行点，`logic_order.go:124`） |
| 钱包 `/api/wallet/*`（7 个路由） | 后端不看品牌 | `route.go:221-235` |
| webapp | overleap `features.wallet=false`，界面已隐藏 | `webapp/src/brands/overleap/index.ts:59` |
| 条款 | §7 "7 天内可退，退到钱包"、§8 加密货币提现 | `sites/overleap/public/legal/terms-of-service.md` |
| 帮助页 | "If {brand} doesn't work for you, email us and we'll sort it out."（22 种语言） | `sites/overleap/messages/*/help.json` `billing.items.refund` |
| 生产数据 | overleap 7 个用户；订单 0；钱包余额 0；提现账户 0；分销商 0；被邀请 0；订阅 1 条（apple, expired） | 2026-10-07 只读查询 |

结论：所有闸门对现有数据零影响，不需要迁移。

## 1. Stripe 全额退款 / 拒付 → 收回会员 + 取消订阅

### 1.1 规则

| 事件 | 条件 | 动作 |
|---|---|---|
| `charge.refunded` | **全额**：`ch.Refunded \|\| (ch.Amount > 0 && ch.AmountRefunded >= ch.Amount)` | 收回会员、本地订阅置 `revoked`、取消 Stripe 订阅、Slack 告警 |
| `charge.refunded` | 部分退款 | 只告警（运营补偿，保留服务）——现状不变 |
| `charge.dispute.created` | 任意 | 收回会员、置 `revoked`、取消 Stripe 订阅、Slack 告警 |
| `charge.dispute.closed` | — | 不处理（胜诉也不自动恢复，人工判断）——现状不变 |

"全额退款 = 终止合作"是运营规则：想补偿又保留服务就做部分退款。写进告警文案和 `docs/` 运营说明（见 §5）。

### 1.2 归属：charge → Stripe 订阅

basil 版本的 Charge 已经没有 `invoice` 字段（stripe-go v82 `Charge` 结构里没有），不能从 charge 直接拿到订阅。现有代码按 customer 取"最新一条订阅"只够告警用，不能用来收回会员：同一个 customer 取消后再订阅会有多条订阅，退的可能是旧的那条。

做法：用 `payment_intent` 查 Invoice Payments 精确定位。

```
stripeLookupSubscriptionByPaymentIntent(key, pi) (subID string, err error)
  invoicepayment.List{payment: {type: payment_intent, payment_intent: pi}, expand: [data.invoice]}
  → 第一条的 invoice.parent.subscription_details.subscription.id
  没有结果 / 没有 parent 订阅 → ("", nil)  // 不是订阅扣款
  API 错误 → ("", err)
```

- charge 的 PI：`ch.PaymentIntent.ID`；dispute 的 PI：`d.PaymentIntent.ID`（dispute 事件里 `charge` 只是 ID 字符串，但 `payment_intent` 也在，省一次取 charge）。
- 是包级 `var`（对标 `stripeNewCheckoutSession`），测试替换，不打真 Stripe。key 逐调用传入。

### 1.3 处理函数

```go
// revokeStripeForChargeLoss 是全额退款 / 拒付的共同落点。reason 进告警和 UserProHistory。
func revokeStripeForChargeLoss(ctx, cfg StripeConfig, pi, reason string) error
```

步骤：
1. `pi == ""` → 告警"无法归属，人工处理"，返回 nil（重投也不会变出 PI）。
2. `subID, err := stripeLookupSubscriptionByPaymentIntent(cfg.SecretKey, pi)`：`err` → 返回 err（500，Stripe 重投）；`subID == ""` → 告警"不是订阅扣款，人工处理"，返回 nil。
3. 查本地 `Subscription{provider=stripe, provider_subscription_id=subID}`：
   - 找到且 `status != revoked` → `revokeSubscription(ctx, &sub, "")`（`logic_apple_iap.go:512` 现成函数，provider 无关；`txnID=""` 跳过订单侧，Stripe 没有订单）。它的收回规则：只有用户到期落在 `(now, sub.CurrentPeriodEnd]` 里才砍到 now，叠加的赠送时长不误伤；写 `UserProHistory{VipRefund}`；订阅置 `revoked`。
   - 已是 `revoked` → 跳过（重投 / 先退款后拒付）。
   - 找不到（入账从没成功过）→ 不动本地，继续第 4 步。
   - DB 错误 → 返回 err。
4. `stripeCancelSubscription(cfg.SecretKey, subID, reason)`：先 Get，状态已是 `canceled` / `incomplete_expired` 就跳过；否则 `Cancel(prorate=false, invoice_now=false, cancellation_details.comment=reason)`。错误 → 返回 err（500 重投；第 3 步因 `revoked` 跳过，只重试取消）。
5. 告警：`[STRIPE-REFUND]` / `[STRIPE-DISPUTE]`，带 charge/dispute id、金额、user_id、sub、"membership revoked, subscription canceled"。

顺序理由：先收回会员（本地、事务内、立即生效），再调外部 API 取消。取消失败靠重投补，收回不会因外部故障而延迟。

后续事件的交互：取消后 Stripe 发 `customer.subscription.deleted` / `updated` → `markStripeSubscriptionDeleted` / `applyStripeSubscriptionUpdate` 都有 `revoked` 终态短路（已有），不会被改回 `expired` / `active`。之后若有 `invoice.paid`（理论上不会，已取消）→ `creditStripeInvoice` 用 `deriveVerifiedStatus`，revoked 不复活（已有）。

### 1.4 告警内容保留

`recordStripeRefundAlert` 的部分退款分支保持现状（文案改为 "partial refund — membership kept"）。全额分支和拒付分支在动作完成后告警，失败时告警写明哪一步失败。

## 2. 钱包对 overleap 关闭

### 2.1 品牌能力位

`BrandConfig` 加 `Wallet bool`（注释：钱包——订单退款入钱包、提现、分销返现结算）。kaitu `true`，overleap `false`。

导出进跨层契约 `contracts/api-contract.json`（`brands.<b>.wallet`），webapp 契约测试断言 `brand.features.wallet === contract.brands[b].wallet`——两层的开关从此不能漂移。重生成：`cd api && UPDATE_CONTRACT=1 go test -count=1 -run TestExportContract ./...`。web/ 契约测试只读它关心的字段，不受影响（实现时跑一遍确认）。

### 2.2 闸门

| 位置 | 改动 |
|---|---|
| 新中间件 `WalletRequired()` | 读 `ReqUser(c)`，`!Brand(u.Brand).Config().Wallet` → `Error(c, ErrorNotSupported, "wallet is not available for this brand")` + Abort |
| `route.go` 7 个 `/api/wallet/*` 路由 | 每个在 `AuthRequired()` 之后加 `WalletRequired()`（必须在鉴权后：要读用户品牌，不读请求品牌——跨品牌 staff 免检时请求品牌不可信） |
| `ProcessOrderRefund`（唯一执行点） | 锁用户行后：`!Brand(user.Brand).Config().Wallet` → 返回错误"该品牌没有钱包，不能退款到钱包"。审批执行与任何未来调用方都过这道门 |
| `api_admin_refund_order` | 预校验加同一判断（Preload User），返回 `ErrorNotSupported` + 中文说明"overleap 的 Stripe 订单请在 Stripe 后台退款（全额退款会自动收回会员），Apple 订单由 Apple 处理"。不建审批单 |

后台提现审批（`/admin/wallet/withdraws/*`）不加门：overleap 用户建不出提现单，审批端没有可审的对象。

### 2.3 结构守卫

`TestWalletRoutes_AllGated`：启动真实 `SetupRouter`（或现有 brand_isolation 测试的路由构造方式），遍历 `r.Routes()` 中前缀 `/api/wallet` 的全部路由，用 overleap 用户逐个请求，断言都返回 `ErrorNotSupported`。以后新增钱包路由忘了加门，这个测试直接红。kaitu 用户对 `GET /api/wallet` 断言不返回 `ErrorNotSupported`（正向对照，防止门把所有人都拦了还是绿）。

## 3. 条款与帮助页（只改 overleap 站）

### 3.1 `terms-of-service.md`

- `Last updated: 2026-10`。
- §7 改名 "Cancellation and Refunds"，全文：

> 7.1 **Cancelling**: You can cancel automatic renewal at any time. You keep access until the end of the period you've paid for, and you won't be charged again.
>
> 7.2 **14-day cancellation right**: You can cancel within 14 days after any payment for a subscription bought on our website — the first payment, and each renewal of an annual plan — and receive a refund. Because you ask us to start the Service as soon as you subscribe, the refund is reduced in proportion to the days you have already used. Your access ends when the refund is issued.
>
> 7.3 **How to request it**: Email support@overleap.io from your account's email address. We'll confirm the amount and refund it to your original payment method within 5 business days.
>
> 7.4 **App Store purchases**: Subscriptions bought through Apple's App Store are billed by Apple. Refunds are handled by Apple under its own policy; we can't issue them. If Apple refunds a purchase, the access it paid for ends.
>
> 7.5 **No other refunds**: Apart from Sections 7.2 and 7.4, payments are non-refundable, including for partially used periods. Nothing in these Terms limits rights you have under the laws of your country.
>
> 7.6 **Chargebacks**: If a payment is fully refunded or disputed with your bank or card issuer, the subscription it paid for ends and your access stops immediately.

- 删除 §8 "Wallet and Withdrawals"，后面各节编号前移（§9→§8 …）；全文 grep "Section"/"§" 交叉引用一并改。
- 原 §9.1（新 §8.1）："After account deletion, your subscription will immediately terminate without refund." → "Deleting your account ends your subscription immediately. If you're entitled to a refund under Section 7.2, request it before deleting your account."
- 原 §9.3(c) 保持（"unless otherwise required by law"），追加 "or provided in Section 7"。
- §4.2 "without refund" 保持（违规终止）。

措辞说明：7.2 写成"网站购买"统一规则，不提国家（全球统一）；"each renewal of an annual plan" 对应 DMCCA 的 12 个月以上续费；月付续费不在内（spec §3.1）。

### 3.2 `delete-account.md:41`

→ "Unused subscription time is **not** refunded and cannot be restored afterwards. If you're within 14 days of a payment on our website, email support@overleap.io before deleting your account to request a prorated refund (see our Terms, Section 7)."

### 3.3 帮助页 `billing.items.refund.answer`（22 种语言）

英文：
> "Within 14 days of a payment on our website you can cancel and get a refund for the days you haven't used — email support@overleap.io. Purchases made in the App Store are refunded by Apple."

其余 21 种语言翻译同义，保留 `{brand}` 以外的占位符约定（本句无占位符）。en-GB / en-AU 用同一英文。

### 3.4 客服知识库（kb 分支 `feat/support-kb-brands`）

`docs/customer-service/overleap/02`、`04` 里的退款段落改成 §3.1 的口径（机器人仍不承诺退款，引导写邮件 / 转人工）。在 kb 工作树里改并提交，随 kb 分支合并。

## 4. 测试（每条先写、先红、再实现；实现后做变异验证）

| # | 测试 | 断言 | 变异（必须让它红） |
|---|---|---|---|
| 1 | 全额退款 → 收回 + 取消 | 用户到期≈now；sub `revoked`；有 `VipRefund` 历史；cancel 被调且参数为该 subID | 删掉 revoke 调用 |
| 2 | 部分退款 | 用户到期不变；sub 状态不变；cancel 未调 | 把全额判定改成 `AmountRefunded > 0` |
| 3 | 赠送时长保护 | 用户到期 > 周期末 → 到期不变，sub `revoked`，cancel 被调 | — （验证复用函数的既有规则） |
| 4 | 取消失败后重投 | 第一次 cancel 返错 → handler 返错（500）；第二次：不重复写历史，cancel 再次被调 | 去掉 `revoked` 跳过 |
| 5 | 无 PI / 非订阅扣款 | 返回 nil（200），无任何写入，cancel 未调 | — |
| 6 | 本地无订阅行 | cancel 仍被调；无本地写入 | 本地找不到时提前 return |
| 7 | 查询 API 出错 | handler 返错（500） | 把错误吞掉返回 nil |
| 8 | 拒付 | 同 #1 | 删 dispute 分支调用 |
| 9 | 钱包路由全覆盖 | overleap 用户访问全部 `/api/wallet*` → `ErrorNotSupported`；kaitu `GET /api/wallet` 不是 | 去掉任意一个路由的门 |
| 10 | `ProcessOrderRefund` 拒 overleap | 返回错误；订单 / 用户 / 钱包均未改 | 删门 |
| 11 | `api_admin_refund_order` 拒 overleap | `ErrorNotSupported`；`approvals` 表无新行 | 删预校验 |
| 12 | 契约 | golden 含 `wallet`；webapp 断言 features.wallet 与契约一致 | webapp overleap 改 `wallet:true` |

既有测试 `ChargeRefunded_PassiveAlert_200`（全额、无 PI）按新规则仍是 200 且无写入（落在 #5），保留并改名。

运行：api 新 worktree 先 `cp ../k2app/center/config.yml center/`（否则 DB 测试静默 SKIP，判据 `-v` 下 0 SKIP）；DB 走 devdb 闸门。handler 测试单跑和全量各跑一次。webapp `yarn test` 契约测试；`sites/overleap` 的 lint / test（看它的 CLAUDE.md）。

## 5. 文档

- `api/CLAUDE.md` 支付段：Stripe 全额退款 / 拒付自动收回 + 取消；部分退款只告警；"全额退款 = 终止合作"。
- 上位 spec §3.2 表格的"改成"列已覆盖，不重复。

## 6. 上线清单

1. 合并 → `make deploy-api`（center-deploy）。
2. Stripe Dashboard（用户操作）：确认 webhook 端点订阅了 `charge.refunded`、`charge.dispute.created`（新代码依赖它们；没订阅的话收回不会触发）；Billing Portal 关闭"立即取消并按比例退款"（只留期末取消）。
3. overleap 站点按 `sites/overleap/CLAUDE.md` 的发布方式发布（条款 + 帮助页）。
4. webapp 只有契约测试改动，不需要 `webapp/*` tag。

## 7. 不做

- 自助撤回按钮、`StatutoryRefund` 表、试用（B 期）。
- 拒付胜诉自动恢复会员。
- kaitu 品牌的任何行为变化（kaitu 不走 Stripe；钱包门对 kaitu 恒放行）。
