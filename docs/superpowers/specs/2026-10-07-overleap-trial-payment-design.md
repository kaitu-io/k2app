# Overleap 支付：试用 + 法定退款（修订 10-01 spec）— 设计 spec

日期：2026-10-07 · 基线：`main` @ 3b73fb43
修订：`2026-10-01-overleap-pricing-purchase-funnel-design.md`。本 spec 改写它的决策 #1，以及 §3.1、§3.2、§3.3、§3.4 的退款部分、§4.2、§5 的 `refund_confirmation`、§6、§8、§12 的"网页免费试用"。其余部分不变。

## 0. 目标与决策

三个目标，排序即优先级冲突时的取舍顺序：**合法 > 保护我们 > 转化**。

| # | 决策 | 理由 |
|---|---|---|
| 1 | **两个渠道都做 7 天免费试用**（Apple Introductory Offer；Stripe Checkout 试用，先绑卡） | 降低首次付费门槛；VPN 行业在移动端普遍提供 7 天试用 |
| 2 | **试用只给年付**；月付不试用、立即扣款 | 引导到年付（更高 LTV、续费事件少一半）；iOS 本来就只有年付商品 |
| 3 | **不提供自愿退款**。只提供 14 天按天折算的撤回（§3），**全球统一**（2026-10-07 用户决定） | 用户要求"有试用就不退款"；英国 / 欧盟法律不允许完全不退，折算是法律允许的最小值；全球统一让规则和客服口径最简单 |
| 4 | **每人只有一次试用**：Apple 由 Apple ID 判定；Stripe 由账号 + 卡指纹判定（§2.3） | 网页试用是测卡和白嫖的重灾区 |
| 5 | **所有退款和拒付都收回会员**，后门（后台退款到钱包、钱包提现）对 overleap 关闭 | 现状：Stripe 后台退款后会员不收回；后台 `refund_order` 无品牌检查 |

## 1. 法律与规则底线（2026-10 调研，非法律意见，上线前请英国律师过一遍）

| 来源 | 要求 | 对我们的影响 |
|---|---|---|
| 英国 CCR 2013 | 网上签的服务合同，14 天冷静期。用户明确要求立即开始并确认知情后，取消只需按已用部分折算退款 | VPN 一般归为"服务"，不能做到"一开始就不能退"，只能折算 |
| 英国 DMCCA 订阅制度（**2027 年 1 月生效**，2026-08-10 宣布） | 试用结束前必须提醒；**试用转付费后再给 14 天冷静期**；一年及以上的订阅每次续费后也给 14 天；必须能在网上取消；最高罚全球营业额 10% | 试用转付费那一刻会触发一个可折算退款的窗口 |
| 欧盟 CRD + 指令 2023/2673（**2026-06-19 已生效**） | 14 天撤回权；网上签的合同必须提供"撤回合同"按钮，14 天内一直可用 | 我们按欧元收费，适用；目前没有这个按钮 |
| 美国加州 ARL（2025-07-01 修订）及各州同类法律 | 自动续费要**明确同意并留存记录**（3 年）；免费转付费纳入监管；网上开通就要能网上取消；年度提醒 | 结账要有同意动作；同意记录要存 |
| Visa / Mastercard 试用规则 | 签约时披露试用时长、之后的金额和扣款日；扣款前发通知并附取消链接；试用后的首次扣款账单描述要带 "trial" 字样 | Stripe 自带试用提醒邮件可以覆盖一部分 |
| Apple 3.1.2 + Apple 退款 | 付费墙必须写清试用后的价格和周期；Apple 订单的退款由 Apple 决定（英国 / 欧盟 14 天） | 我们拦不住 Apple 退款，只能在退款后收回会员（已实现） |

## 2. 用户能看到的方案（转化）

### 2.1 定价页与结账页

- 默认选中年付。年付卡片："**7 days free**, then £79/year (£6.58/month)"；月付卡片："£9.99/month"，不写试用。
- 主按钮：年付 "**Start 7-day free trial**"；月付 "Subscribe"。
- 时间线（年付才显示）：**今天** 免费开始 → **第 5 天** 邮件提醒 → **第 7 天** 扣款 £79，可随时在账户里取消。时间线是转化手段（降低对自动扣款的顾虑），也是卡组织要求的披露。
- 结账页摘要：试用结束日、首次扣款金额与日期、"Cancel anytime before {date} and you won't be charged."
- 去掉所有 "money-back" / "refund guarantee" 措辞。
- Apple Pay / Google Pay 保持开启（Stripe Checkout 自带）。

### 2.2 iOS 付费墙

- 有资格时主按钮 "Start 7-day free trial"，下面写 "then {displayPrice}/year · cancel anytime in Settings"（Apple 3.1.2）。资格由 StoreKit 的 `isEligibleForIntroOffer` 决定。
- 修掉开途遗留文案 `purchase.currentlyInTrial` 在 overleap 下的显示。
- 订阅状态 `trialing` 显示 "Free trial · ends {date}"。

### 2.3 一人一次（保护）

**Stripe**（服务端判定，结账前）：
- 资格：该 overleap 账号从未有过任何订阅或试用（Stripe 或 Apple，读 `subscriptions` / `subscription_credits`）。没资格就把年付按钮改成 "Subscribe"，Checkout 不带试用参数。
- 卡指纹：`checkout.session.completed` 时取支付方式的 `card.fingerprint`，写入新表 `TrialFingerprint{fingerprint unique, user_id, created_at}`。如果指纹已存在（同一张卡换邮箱再试用），**立即结束试用**（`trial_end=now`，马上按年付扣款），并发邮件说明"这张卡已用过试用"。用户仍可在冷静期内取消（§3）。
- Stripe Radar：对 0 元试用的绑卡请求，拦截预付卡（`card_funding = prepaid`），只在试用结账上生效。

**Apple**：Apple 按 Apple ID 和订阅组判定，我们不能也不需要干预。跨渠道（用过 Stripe 试用的人又在 iOS 试用）不拦，代价小。

### 2.4 提醒与取消（合规 + 降低拒付）

- **试用开始**：欢迎邮件，写清试用结束日、扣款金额、取消入口（10-01 spec §5 的 welcome 邮件加字段）。试用短于 7 天时，Stripe 的试用提醒邮件会在试用开始时发出，打开它（Dashboard 设置）。
- **试用第 5 天**：我们自己的提醒邮件（模板 `overleap-trial-ending`），附账户取消链接。由 `customer.subscription.trial_will_end`（Stripe 在结束前 3 天发）触发，不用新 cron。
- **年付续费前 14 天**：续费提醒（overleap 版 `renewal-*` 模板；修复 `templateSlugExists` 不分品牌的问题）。
- **取消**：账户页一键取消（10-01 spec §4.3）。试用期内取消：试用结束时订阅结束，不扣款，会员用到试用结束。Apple 用户指向系统订阅设置。
- **账单描述**：试用后首次扣款的 statement descriptor 带 "TRIAL"（Visa 规则；Stripe 能否按单张 invoice 设置待核实，见 §7）。

### 2.5 同意记录

Checkout 开启 `consent_collection.terms_of_service = required`，`custom_text.terms_of_service_acceptance` 写：

> I agree to the Terms. My subscription starts now and renews automatically at {price}/{period} until I cancel. I ask for the service to begin immediately, and understand that if I cancel within 14 days of a payment I'll receive a refund reduced for the days I've used.

`checkout.session.completed` 时把同意的时间、文案版本、IP 国家写入 `SubscriptionConsent{user_id, subscription_id, text_version, accepted_at, country}`，保存 3 年以上（加州）。

## 3. 退款：只做法律强制的那部分

### 3.1 规则（一个函数判定，前后端共用）

`statutoryRefund(sub) → (eligible, amount, until)`：

- **窗口**：每一笔实付款之后 14 天内。包括试用转付费的首次扣款、月付的首期、年付的每次续费（DMCCA）。月付的后续续费不在窗口内（DMCCA 只覆盖 12 个月及以上的续费；首期之外的月付续费不触发）。
- **适用地区**：全球统一，不按国家区分（2026-10-07 用户决定：方便处理、简化）。
- **金额**：`实付金额 × 未使用天数 / 本期总天数`，按天向下取整到最小货币单位。试用的免费天数不计入已用。
- **执行**：用户在账户页点 "Cancel and get a partial refund"（欧盟要求的"撤回合同"入口就是它，只在窗口内显示）→ 确认对话框写明退款金额和"会员立即结束" → `POST /api/user/stripe/withdraw` → Stripe 部分退款 + 立即取消订阅 + 收回会员 → 发确认邮件。
- 防重复：同一笔付款只能撤回一次（`StatutoryRefund{invoice_id unique, ...}` 唯一索引）。

取代 10-01 spec 的 `GuaranteeRefund` 和 30 天全额退款。

### 3.2 其他会发生退款的路径（保护）

| 路径 | 现状 | 改成 |
|---|---|---|
| Stripe 后台手动退款 → `charge.refunded` | 只发 Slack 告警，会员不收回 | 全额退款 → 收回会员并取消订阅；部分退款 → 只告警（人工判断） |
| Stripe 拒付 → `charge.dispute.created` | 只告警 | 收回会员、取消订阅、告警；拒付胜诉（`charge.dispute.closed` won）不自动恢复，人工处理 |
| Apple REFUND / REVOKE | 已自动收回会员 | 不变 |
| 后台 / MCP `refund_order`（退到钱包） | 不检查品牌，能对 overleap iOS 订单执行 | overleap 订单拒绝（overleap 没有钱包） |
| 钱包提现接口 `/api/wallet/*` | 后端不检查品牌 | overleap 用户拒绝 |

### 3.3 拒付率

目标：拒付率保持在 0.5% 以下（Visa 的监控线是 0.9%）。手段：试用提醒、明确的账单描述、一键取消、冷静期内自助退款（用户能自己退就不会去找银行）。看板上加拒付率。

## 4. 后端改动（Center）

| 位置 | 改动 |
|---|---|
| `api/api_stripe.go` 结账 | 年付且有资格：`subscription_data.trial_period_days=7`，`payment_method_collection=always`，`trial_settings.end_behavior.missing_payment_method=cancel`；所有结账：`consent_collection`、`custom_text` |
| `api/logic_stripe.go` webhook | `checkout.session.completed`：写 `SubscriptionConsent`、卡指纹去重（§2.3）；`customer.subscription.trial_will_end`：发提醒邮件；`charge.refunded` / `charge.dispute.created`：收回会员（§3.2）；试用入账写 `SubscriptionCredit{kind=trial}`（不算付款，漏斗投影为 `trial_start`） |
| 新接口 | `GET` 资格（试用资格 + `statutoryRefund` 结果，并入 `DataSubscription`：`trialEligible`、`trialEndsAt`、`withdrawEligibleUntil`、`withdrawAmount`）；`POST /api/user/stripe/withdraw` |
| `api/logic_apple_iap.go` | 识别 Introductory Offer 的 FREE_TRIAL：只发会员到试用结束、`status=trialing`、写 `kind=trial`、不建付费单、不返佣（即 10-01 spec §6，原样保留） |
| 退款与钱包 | `refund_order` 与钱包提现按品牌拒绝 overleap |
| 新表 | `TrialFingerprint`、`SubscriptionConsent`、`StatutoryRefund`（替代 `GuaranteeRefund`） |
| 邮件模板（overleap） | `overleap-trial-ending`、`overleap-trial-card-used`、`overleap-withdraw-confirmation`、年付续费提醒；修复 `templateSlugExists` 按品牌过滤 |

## 5. 文案与法务（全部只改 overleap）

- 服务条款：重写 §7 为 "Free trial, cancellation and refunds"（试用规则、一人一次、自动续费与取消、法定撤回与折算、其他情况不退、Apple 购买由 Apple 处理）；删除 §8 钱包与提现；§9.3(c) 对齐。
- 官网：定价页 FAQ 增加试用和取消，删 refund guarantee；帮助页 `billing.items.refund`（22 种语言）改成"取消与法定撤回"说明；`delete-account.md:41`。
- 客服知识库 `overleap/02`、`04`：写试用、取消、法定撤回；机器人不承诺退款，法定撤回引导到账户页按钮。
- `.agents/product-marketing-context.md`："7-day refund" 只留给开途，overleap 的必用词改成 "7-day free trial"。

## 6. 衡量

漏斗路径 `trial` 扩展为两个渠道（`channel=apple|stripe`）。看板新增：试用开始率（定价页 → 试用）、试用转付费率、冷静期撤回率、拒付率、试用内取消原因。上线后 4 周对比 10-01 基线（无试用时的定价页 → 付款）。

## 7. 待核实 / 待决定

1. **请英国律师确认**：VPN 是"服务"还是"数字内容"；§2.5 的同意文案。
2. Stripe 能否给单张 invoice（试用后首扣）设置带 "TRIAL" 的账单描述；不能的话用账户级 descriptor 后缀。
3. Stripe Dashboard：打开试用提醒邮件、Smart Retries、Billing Portal 只允许期末取消、不按比例退。
4. ASC：在 `io.overleap.sub.basic.1y` 上配置 1 周免费的 Introductory Offer（该商品还在审核中，过审后配置）。
5. ~~卡指纹重复时的处理~~ 已定（2026-10-07）：立即结束试用并扣款，邮件说明，冷静期内仍可撤回。

## 8. 分期

| 期 | 内容 | 依赖 |
|---|---|---|
| A 堵漏（独立，先做） | §3.2 五条后门 + 条款改成最终退款政策（14 天折算撤回，全球统一；B 期按钮上线前经客服邮件办理），去掉"退到钱包 / 可提现" | 无。详细设计：`docs/superpowers/specs/2026-10-07-overleap-payment-phase-a-design.md` |
| B 网页试用 | §2.1、§2.3、§2.4、§2.5、§3.1、§4 的 Stripe 部分、§5 | 10-01 spec ① 期新站购买页；律师意见 |
| C iOS 试用 | §2.2、§4 的 Apple 部分 | iOS 商品过审 + ASC 配置 + 下一个 iOS 原生版本 + `webapp/*` tag |

## 9. 不做

- 月付试用；无卡试用；挽回折扣。
- Google Play 内购与试用（overleap Android 维持无购买入口）。
