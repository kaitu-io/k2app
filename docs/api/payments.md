# Payments — NextPay / Stripe / Apple IAP / Apple 退款

> 内容移自 `api/CLAUDE.md` 的 Brand 段子节（2026-10 精简）。改任一支付渠道的下单 / 入账 / 退款 / 收回 / 对账、Stripe 撤回执行器、钱包品牌门前读。"违反了会出钱的事故"的硬不变量同时留在 `api/CLAUDE.md` "Payments" 段。

## NextPay (kaitu 一次性购买渠道, 2026-09-22 起)

- **Spec**: `docs/superpowers/specs/2026-09-22-payment-nextpay-migration-design.md`。取代 WordGate 托管收银台：`POST /api/user/orders` 经 `ensureOrderCheckout`（`logic_nextpay_checkout.go`）在 NextPay 建单 + confirm，`payUrl` **直接是 `checkout.stripe.com` 链接**——客户端只 `openExternal` 不解析，所以此迁移零 OTA 依赖。
- **Config**: viper `nextpay.endpoint`（同机 `http://127.0.0.1:5900`）/ `access_key` / `timeout` 由 `qtoolkit/nextpay` 读；`nextpay.webhook_secret` / `nextpay.payment_method`（缺省 `more`）由 `configNextpay` 读。缺 key/secret 时渠道不可用（下单 405001 / webhook 503 / 302 端点回 purchase），不 panic。`Ready()` 检查放在 preview 之后——预览只算价，不依赖支付渠道。
- **Single credit point**: `/webhook/nextpay` 的 `order.paid` → 既有 `MarkOrderAsPaid`。关联键 `ObjectID == orders.uuid`；`orders.nextpay_order_id` 只做对账/双付判定。金额/币种错配与品牌错配都 fail-loud（Slack `[PAYMENT-AMOUNT-MISMATCH]` / `[PAYMENT-BRAND-MISMATCH]` + 5xx 让 NextPay 重投）；已付订单被另一 NextPay 订单再付 → `[DOUBLE-PAY]` 告警 + ack，人工退款。
- **耐久链接**: `Order.Meta.payUrl` 是 `{BaseURL}/api/orders/{uuid}/pay`（代付邮件唯一读者），不是 Stripe URL——Stripe session 24h 过期、NextPay 订单 30 分钟后拒 confirm。`Meta.checkoutUrl/checkoutAt` 缓存最近 session，复用窗口按 `Meta.checkoutMode` 区分（`reuseWindowFor`：Stripe Checkout 23h / 支付页 25min；无该字段的老订单按 Stripe Checkout），超出则整单重建。
- **两条 checkout 路径（灰度）**: viper `nextpay.payment_page_percent`（0–100，缺省 0）按用户 UUID 哈希固定分组（`nextpayPaymentPageBucket`，放量单调）。命中者 `createNextpayCheckout` **只建单不 confirm**，`payUrl` 是 NextPay 自有支付页 `pay.arbella.group/pay/<nextpay订单>`（用户在页上选支付宝 / 卡；支付宝回跳先回支付页，只有 paid 才转发 successUrl `?order_id=`）；其余仍 confirm(more) 拿 `checkout.stripe.com`。两路都由同一个 `order.paid` 入账，webhook 不区分。对比两组付款率读 `orders.meta` 的 `checkoutMode` + `is_paid`；`[PayFunnel]` 的 checkout 行也带 `mode=`。
- **WordGate 现状**: 只切断了下单路径；`/webhook/wordgate`、`configWordgate`、SDK 依赖保留到在途订单清零（Phase B 清理）。`Order.Channel` 空串 = 历史 WordGate 订单，`nextpay` = 新订单。

## Stripe (overleap 官网支付渠道, Phase 6)

- **Routes**: `POST /api/user/stripe/checkout` (Checkout Session, `mode=subscription`, returns `{url}`), `POST /api/user/stripe/portal` (Billing Portal `{url}`), `POST /webhook/stripe`. Files: `api_stripe.go` / `api_stripe_webhook.go` / `logic_stripe.go` (spec 提到的 `payment_stripe.go` 按本仓库命名铁律拆分).
- **Config**: viper `stripe.secret_key` / `stripe.webhook_secret` / `stripe.success_url` / `stripe.cancel_url` / `stripe.portal_return_url` — overleap 专用；URL 缺省回退 `BrandConfig(overleap).BaseURL`。缺 key/secret 时渠道自动不可用（handler 405001 / webhook 503），不 panic。
- **Single credit point**: `creditStripeInvoice` (`logic_stripe.go`) — `invoice.paid` 是唯一 bind+credit 事件；`subscription_data.metadata`(`user_uuid`/`plan_pid`/`brand`) 随每张 invoice 回传，事件自足。INV1 幂等键 `SubscriptionCredit(provider="stripe", transaction_id=invoice id)`；INV9 绑定键 = Stripe subscription id；INV3 叠加走 `applyGiftCredit`/`applyRenewalCredit`。入账 plan 查找用 `planByPIDForCredit`（不过滤 `is_active`——下架不停续费入账）。
- **Event-level idempotency**: `stripe_webhook_events` 表按 event id 去重（check → process → record）。
- **SDK-shape adapter**: `extractStripeInvoiceFacts` 是 stripe-go(v82/basil) invoice 形态的唯一适配点（`invoice.parent.subscription_details.*`、period 取 invoice line）。升 SDK 只改它。
- **Manage surface**: `DataSubscription.Manage.Kind == "stripe_portal"` → 客户端调 portal 端点换 URL 再跳转。
- **多币种展示价**: `DataPlan.CurrencyPrices`（`logic_stripe_price.go`）—— Stripe 套餐的 `/api/plans` 附带 `{币种→最小单位}`（主币 + `currency_options` 全部币种），从 Stripe Price 取（`expand=currency_options`）、进程内缓存 1h、失败只记日志字段省略（客户端回落 `Price` 美元）。Price 主币 **USD**（账号结算币；只有主币=结算币 Adaptive Pricing 才生效），GBP/EUR 为固定本币价，Checkout 按属地自动选币——建价只走 `scripts/stripe-setup-overleap.sh`（spec `2026-09-04-overleap-site-decoupling-and-uk-positioning-design.md` §4）。
- **Reminders**: `processRenewalReminders` 跳过 `usersWithLiveAutoRenew`（apple/stripe 活跃自动续订用户不收"手动续费"邮件）。
- **收回（spec `2026-10-07-overleap-payment-phase-a-design.md`）**: `charge.refunded` **全额**与 `charge.dispute.created`（任一张 invoice，含询问）→ `revokeStripeForChargeLoss`：收回会员 + 取消 Stripe 订阅 + 告警（"全额退款 = 终止合作"；想补偿又保留服务就做部分退款，部分退款只告警）。归属靠 PI → Invoice Payments → 订阅；本地无行建 **revoked 墓碑**。**Stripe 账户（Wordgate LLC）与 NextPay / WordGate 等业务共用**，本端点会收到别家的 checkout / 退款 / 拒付事件：`revokeStripeSubscription` 返回 `ours`（本地有行或墓碑建成），非 ours 的订阅原样放过、**绝不取消**；非订阅收款只记日志。账户级设置（公开详情的 ToS URL、Portal 默认配置、收据邮件）同样波及别家，改前先想清楚。`subscription.deleted` 提前结束（`ended_at` 早于事件自身周期末 >1h）按差值截断。
- **扣减量只看 `Subscription.PaidThrough`**（已发放的付费时长覆盖到的时刻，入账 / 对账 cover-through 按实际发放累加，扣减后回收）：**不要**用 `CurrentPeriodEnd`（对账会推到未付的下一期 → 扣掉赠送时长）（Apple 同口径，见下方 Apple 退款段）。PaidThrough 为 0 的老行退回 `CurrentPeriodEnd`（fail-closed）。
- **revoked 是终态，四道门**：`creditStripeInvoice` 不加时长、对账 cover-through 锁内跳过、`subscription.updated` 写带 `status<>revoked`、`subscription.deleted` 短路。新增任何改 Stripe 订阅 / 用户到期的路径都要过这四道门的同款检查。
- **14 天撤回执行器**（`logic_stripe_withdrawal.go`；预览 `GET /app/users/:uuid/stripe-withdrawal` 在 staff 组，客服可报价；执行 `POST` / 作废只给超管；MCP `quote_stripe_withdrawal` / `withdraw_stripe_subscription`）：一次请求 = primary（`notice_at` 前最近一笔付款，按天折算，含付款当天；首付或周期 ≥360 天的续费才合格，按 `billing_reason` 判）+ 通知后扣的款（全额，从 Stripe 已付 invoice 枚举）。顺序 退款 → 收回 → 取消 → 完成；以 `StatutoryRefund` pending 行续跑（不重判资格），退款先按 metadata 查自家退款（幂等键只留 24h）；primary 行 `LockedUntil` 是请求占位锁；`ActiveInvoiceID` 唯一（作废置 NULL 释放）。**客服不要在 Stripe 后台手工部分退款**——会员不会被收回。
- **结账同意**：Checkout 带 `consent_collection` + `custom_text`（`stripeConsentMessage`，改文案必改 `consentTextVersion`），`checkout.session.completed` 落 `SubscriptionConsent`。无同意记录的订阅撤回时全额退（没有折算依据）。**部署前置**：Stripe Dashboard 必须先填 Terms of service URL，否则 Checkout 创建直接报错。
- **钱包**：`BrandConfig.Wallet`（overleap false，进契约）；`WalletRequired()` 挂全部 `/api/wallet*`（`AuthRequired` 之后，读用户品牌），`ProcessOrderRefund` / `api_admin_refund_order` / `addCashbackIncomeInTx` 按品牌拒绝。新钱包路由忘挂门，`TestWalletRoutes_AllGatedForBrandsWithoutWallet`（枚举生产 `SetupRouter`）会红。

## Apple IAP brand split (Phase A) + remaining seams

Phase A opened `apple_iap` for overleap with bundle-level isolation:

- **`planByAppleProductID`** (`logic_apple_iap.go`) is **brand-scoped** — `ScopeBrand` filter applied, preventing cross-brand product ID collisions.
- **`appstore.bundleIds.<brand>`** (viper): per-brand Apple bundle id for IAP verify. kaitu keeps legacy `appstore.bundleId`; other brands read `appstore.bundleIds.<brand>` via `appleBundleIDForBrand` — empty = fail-loud (verify refuses, no silent fallback to kaitu's bundle). `verifyAndGrantTransaction` loads the user's brand and sends that brand's bundle id to Apple, so a kaitu-app transaction can never credit an overleap account (e2e #09 pins this).

Invariants from the 2026-08-10 sandbox-order incident (narrative: `docs/incidents/2026-08-10-apple-iap-sandbox-order.md`):

- **Sandbox transactions grant entitlement but never create an `Order`** — `creditAppleTransaction` returns before order creation when `info.Environment == appstore.Environment_Sandbox`. Orders are financial entities: `PayAmount` feeds cashback, revenue stats, and wallet refunds. `iapOrderFixture` defaults to `Environment_Production` for the same reason — flipping it tests a path the gate cuts off.
- **App Store orders never go through the admin wallet refund** — `ProcessOrderRefund` / `api_admin_refund_order` / the approval callback reject `channel=apple_iap` (`appleIAPWalletRefundMsg`): Apple took the money and only Apple refunds it; a withdrawable wallet refund plus a later Apple refund is a double refund. Legacy wallet-refunded IAP orders are still recognised by `alreadyWalletRefunded`（`logic_apple_iap.go`；调用方在事务提交后发告警） (`[DOUBLE-REFUND]`) and skip the entitlement cut.

## Apple 退款收回（spec `2026-10-08-apple-refund-design.md`，两品牌同一规则）

- **规则**：收回被退交易自身尚未消耗的付费时长；不碰赠送；不追讨已消耗的旧期；退款被撤销就还回去。入口 `applyAppleRefund`（webhook `REFUND`/`REVOKE`、对账发现 Revoked）/ `reverseAppleRefund`（`REFUND_REVERSED`、对账探测）——`logic_apple_refund.go`。**不写品牌分支**：开途特有风险挂在开途独有功能上（邀请、钱包、分销），Overleap 这三项关闭。
- **`PaidThrough` 现在 Apple 也维护**（`creditAppleTransaction` 入账后单独 `Update`）。扣减量 = `min(PaidThrough−now, 到期−now, 本期长度)`；更新的一期已入账（`CurrentPeriodEnd > tExp`：Apple 提前 24h 扣续费 / 迟到处理）时只收 `tExp−now`。宽限期 cover-through 不动 `PaidThrough`。
- **退款不改 `current_period_end`，只置 revoked**；退款之后的新付款（同 otx）按**首购口径**入账（`revival`：整期、从 `max(到期, 购买时刻)` 起叠、覆盖周期末），离开 revoked 只走 `deriveActiveOrExpired`——`deriveVerifiedStatus` 对 revoked 恒返回 revoked。被退交易从未入账则**永不入账**（`errAppleTxnRevoked`：verify 报"已退款"，webhook/对账视为已处理）。revoked 守卫：`applyRenewalInfo` 与 `setSubStatus` 的 SQL 带 `status<>'revoked'`。
- **先后按 Apple 签名时刻（毫秒）判定，不按到达顺序**：`AppleRefund`（每笔交易一行）的 `RefundSignedAt` / `ReversedSignedAt` / `Active`。撤销先到建占位行；已在退款状态时只用 Apple 给出的时刻推后证据（now 兜底不推后——否则撤销探测冷却永不到期）；再次退款封顶 `CutSeconds − (now − RestoredAt)`，仅当上一轮确实收过也还过。
- **webhook 信任边界**：通知与交易的 bundleId 必须是该订阅品牌的 bundle（Apple 根证书签所有 app 的通知）；`REFUND_REVERSED` 恢复前经 `fetchAppleTransaction` 复核（授予动作都过第二层）。`appleRootCAPEM` 是 var 仅供端到端测试注入。
- **对账**（`worker_subscription_reconcile.go`）：Apple 生产订阅按 `id % 7` 每天巡检一桶（有效 / 近 120 天过期 / 带生效退款的任意状态），与 48h 临期扫描去重；先判 Apple Revoked → 只走收回（不做宽限延长）；revoked 行只允许复活入账与撤销探测（跳过续期与 Expired 分支）；撤销探测 48h 冷却、交易号与生产环境一致、证据时刻 = `RefundSignedAt+1ms`；Apple 仍报退款而本地已撤销 → `[APPLE-REFUND-CONFLICT]` 一次，不自动处理。
- **退款后续共用 `onPaidOrderRefundedInTx`**（`logic_purchase_refund.go`，Apple 与后台网页退款都调）：被邀请首购奖励按 `InviteRewardGrant`（被邀请人唯一 = 一生一次；记触发购买 `apple_txn`/`order`）精确撤回双方，被邀请人仍持有合格购买（实付 >0、月数达门槛）则改锚不撤；后台退款可 `keepInviteRewards`（审批 params 留痕）。分销付费人数按 `Order.RetailerCountedID` 扣回或转移到买家其它有效单；L2 自动升级后跌破门槛只告警 `[RETAILER-COUNT-DROP]`，不自动降级。
- **邀请奖励只发生产交易**（沙盒不发、不消耗首单资格；沙盒权益与 Tier 照发）。IAP 返现冻结 90 天（覆盖 Apple 退款窗口），网页 30 天；返现撤回致负余额 `[CASHBACK-NEGATIVE]`。
- **告警**走 `alertBilling`（var，测试替换）：`[APPLE-REFUND]`（含该用户累计退款次数）、`[APPLE-REFUND-REVERSED]`（列手工步骤：订单退款标记、返现、分销计数不自动反转）、`[APPLE-REFUND-PCT]`、`[APPLE-REFUND-RATE]`（开途 30 天 ≥3 次）。
- **Consumption 应答**（`logic_apple_consumption.go`）：webhook `CONSUMPTION_REQUEST` → 仅当 `users.apple_consumption_consent = true`（`PUT /api/user/apple-consumption-consent`）才 PUT v2 `/inApps/v2/transactions/consumption/{txn}`；两品牌一律 `GRANT_PRORATED`、不发 `consumptionPercentage`（自动续订带它 400）。发送失败 500 且不记 UUID，让 Apple 重投。

Still open before overleap Play Billing / campaign sends:
- **Overleap winback campaign codes**: `winbackCampaigns` (`worker_renewal_reminder.go`) has no overleap-scoped codes — `campaignVarsForBrand` returns an empty vars map for overleap recipients (silently no-op; verify intent once overleap runs campaigns).
- **`SavingsText` is hardcoded Chinese** (`worker_renewal_reminder.go`): leaks Chinese copy into English winback email the moment overleap gets a campaign code in `winbackCampaigns` — needs an English variant gated on `brand` before that happens.
- **Overleap lifecycle EDM templates** (`renewal-Nd` / `winback-Nd` slugs, brand=overleap) don't exist yet — reminder/winback sends to overleap users skip with a Slack alert until ops creates them.
