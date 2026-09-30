# Overleap 定价 → 购买 → 留存 + 双品牌转化统计 — 设计 spec

日期：2026-10-01 · 基线：`main` @ ab511103
前序：`2026-09-30-overleap-site-app-design.md`（本 spec = 其阶段 ② 定价 + 阶段 ③ 中 `/pricing`→结账部分）、`2026-07-22-overleap-stripe-web-design.md`（Stripe 后端既有资产）

## 0. 决策（2026-10-01 与 David）

| # | 决策 | 理由 |
|---|---|---|
| 1 | **风险逆转分渠道**：iOS = Apple 原生 7 天免费试用（Introductory Offer）；官网（Stripe）= 30 天无理由退款；Android 维持无购买入口 | IAP 退款权在 Apple，官网承诺退款在 iOS 上兑现不了；试用是 StoreKit 原生能力，iOS 上最丝滑。网页试用要绑卡、且是测卡重灾区，网页行业标准是 30 天退款（NordVPN 同型） |
| 2 | **转化统计 = 第一方 `sid` cookie + 1px 像素 → Center（Go）→ 现有 MySQL** | Next 不掺合后端逻辑；不引入第三方统计、不引入新数据库（DynamoDB 方案已否） |
| 3 | **统计 API 品牌无关，开途与 Overleap 共用** | 统计是平台能力，品牌只是数据维度；两站都经同源 `/api/*` 代理到 Center |
| 4 | 购买页在 `sites/overleap/` 新站重做，不在 `web/` 打补丁 | 沿用 09-30 spec 的拆站决策 |
| 5 | 价格数字不变：年 $79 / £79 / €89，月 $11.99 / £9.99 / €11.99 | 与 ASC、Stripe 既有价格一致 |

## 1. 现状问题（2026-10-01 调查，overleap.io 仍由 `web/` 承载）

- `/pricing` 与首页的套餐 CTA 一律链到 `/purchase`，不带所选套餐（`web/src/components/home-overleap/OverleapPricing.tsx:28`）。
- 付款前强制跳登录页；新用户看到 "Welcome Back" + 开途概念的邀请码框；登录页 "Get Started" 又回 `/purchase`。
- 购买页文案是开途遗留（"Purchase Pro Plan"、"Global Smart Nodes"），与 pricing 的 includes 不一致。
- 全漏斗无退款 / 保障承诺；`/terms` 写 "7 天退款到钱包余额、可提现加密货币"，overleap 没有钱包。
- 错误只有 405001 有专属文案；套餐加载失败显示成 "Plans are being prepared"。
- 成功页无收据、无上手步骤；`/install` 移动端 "Coming soon"。
- Center 不发欢迎 / 购买确认邮件；`templateSlugExists`（`api/worker_renewal_reminder.go:387`）不按品牌过滤而发送按品牌取 → overleap 生命周期邮件静默失败。
- 取消只能进 Stripe Portal，无原因采集、无恢复入口。
- 全站零埋点（overleap GA id 为空）。

## 2. 转化统计平台（品牌无关）

### 2.1 Cookie：`sid`

- 由 **Center** 下发，写法同 `setAuthCookies`（`api/api_auth.go:706`）：`Path=/`、Domain 留空（经同源代理，落在浏览器所见 host，即 overleap.io 或 kaitu.io）、`HttpOnly`、`Secure`（按 `X-Forwarded-Proto`）、`SameSite=Lax`、Max-Age 13 个月。
- 值：128 位随机数 base64url（22 字符）。值 `optout` 表示访客退出统计。
- 名字品牌中立（不含任一品牌词），两站同名。
- **Next 不读不写该 cookie**，只渲染像素。

### 2.2 上报：`GET /api/px`

- 匿名路由（不过 auth 中间件），品牌取请求品牌（Host → `X-K2-Brand` → kaitu，既有 BrandResolver）。
- 参数：`e` 事件名（必填，白名单）、`p` 套餐 pid（可选）、`r` 外部来源 host（可选，仅首个 page_view 由前端从 `document.referrer` 取 host 传入，前端丢弃路径与查询串）。
- 页面路径与 `utm_source/medium/campaign` 从 `Referer` 头解析（同源请求带完整 URL）；只存 path，不存查询串中 utm 以外的参数。
- 响应：43 字节透明 GIF，`Cache-Control: no-store`。无 `sid` 时同时 `Set-Cookie`。
- 不记录：`sid == optout`；UA 命中爬虫特征（bot / crawler / spider / headless）；超限流（每 IP 120 次/分钟，超出静默返回 GIF）。
- `Sec-GPC: 1`：不下发 `sid`，事件照记但 `sid` 为空（只进计数，不进路径）。
- 写入：进程内有界队列（容量 10k），每 100 条或 2 秒批量 `INSERT`；队列满丢弃并每分钟汇总一行 warn 日志。像素请求不等写库。
- `GET /api/px/optout`：下发 `sid=optout` 并 302 回 `Referer` 同源路径（非同源一律回 `/`）。

事件白名单（两品牌共用，未知事件 400 不记）：

| 事件 | 触发 |
|---|---|
| `page_view` | 每个公开页 SSR 内嵌 `<img>` |
| `plan_select` | 点套餐卡 CTA（带 `p`） |
| `checkout_view` | 进入结账页（带 `p`） |
| `auth_code_sent` | 结账内联登录发送验证码 |
| `auth_done` | 结账内联登录成功 |
| `checkout_start` | 支付表单挂载 / 发起支付（带 `p`） |
| `checkout_cancelled` | 从支付回退 |
| `purchase_seen` | 付款后落地页确认激活 |
| `install_click` | 点下载按钮（`p` 复用为平台：`windows` / `macos` / `ios` / `android` / `linux`） |
| `refund_request` / `cancel_submit` / `resume_submit` | 账户页操作 |

前端上报两种形态，都只是图片请求：SSR 页面里 `<img src="/api/px?e=page_view…" width="1" height="1" alt="" aria-hidden>`；交互事件 `new Image().src = '/api/px?e=…'`。

### 2.3 存储（Center MySQL，GORM AutoMigrate）

```go
// WebEvent：访客行为事件。90 天后由 worker 删除。
type WebEvent struct {
    ID          uint64    `gorm:"primarykey"`
    CreatedAt   time.Time `gorm:"index:idx_brand_event_time,priority:3;index:idx_sid_time,priority:2"`
    Sid         string    `gorm:"type:varchar(32);index:idx_sid_time,priority:1"`   // 空 = GPC
    Brand       string    `gorm:"type:varchar(16);not null;index:idx_brand_event_time,priority:1"`
    Event       string    `gorm:"type:varchar(32);not null;index:idx_brand_event_time,priority:2"`
    Path        string    `gorm:"type:varchar(255)"`
    Plan        string    `gorm:"type:varchar(64)"`
    RefHost     string    `gorm:"type:varchar(128)"`
    UtmSource   string    `gorm:"type:varchar(64)"`
    UtmMedium   string    `gorm:"type:varchar(64)"`
    UtmCampaign string    `gorm:"type:varchar(64)"`
    Country     string    `gorm:"type:varchar(2)"`
    Device      string    `gorm:"type:varchar(16)"` // desktop|mobile|tablet
    OS          string    `gorm:"type:varchar(16)"` // windows|macos|ios|android|linux|other
}

// WebSidUser：sid ↔ 用户关联。一个 sid 可对多个用户（共用设备），一个用户可有多个 sid（多设备）。
type WebSidUser struct {
    ID        uint64    `gorm:"primarykey"`
    CreatedAt time.Time
    Sid       string    `gorm:"type:varchar(32);not null;uniqueIndex:uniq_sid_user"`
    UserID    uint64    `gorm:"not null;uniqueIndex:uniq_sid_user;index"`
    Brand     string    `gorm:"type:varchar(16);not null"`
}
```

- 不存 IP、不存 UA 原文。`Country` 复用 Center 既有的 IP→国家推导；若真实客户端 IP 经 CloudFront → Next SSR → rewrite 链后到不了 Center，则留空（实现时实测，见 §10）。
- 关联写入点：web 登录成功（`/api/auth/web-login`、`/api/auth/web-login/password`）与发起结账时，请求带合法 `sid` 就 `INSERT IGNORE` 一行。
- 保留：`WebEvent` 90 天（沿用 `worker_stats_retention.go` 模式，每日按 `created_at` 分批删除）；`WebSidUser` 不过期（它是归因关系，量级 = 用户数）。

### 2.4 归因与报表

- "付费"按用户判定、与支付渠道无关：Overleap = 首条 `SubscriptionCredit{kind=purchase}`（Stripe / Apple）；开途 = 首个 `Order{is_paid}`。漏斗通过 `WebSidUser` 把 sid 连到用户，再连到付费事实——**不改任何支付路径的入账代码**。
- Stripe 结账额外把 `sid` 写进 Checkout Session metadata（排障用，不作为归因依据）。
- 首触来源 = 该用户所有关联 sid 中最早一条带 `RefHost` 或 utm 的 `page_view`。
- 报表接口 `GET /app/stats/web-funnel?brand=&from=&to=&groupBy=source|utm_campaign|country|device`（admin，`RoleMarketing`）：返回各事件去重 sid 数、相邻步转化率、付费用户数、30 天内退款数、取消数。
- kaitu-center MCP 新增工具 `web_funnel`，调用上述接口。

### 2.5 合规

- 隐私政策（两品牌各自）写明：`sid` 的用途（第一方聚合统计与购买归因）、保留期、不与第三方共享、退出方式。
- 页脚加 "Do not count my visits" / 「不参与访问统计」链接 → `/api/px/optout`。
- 不设 cookie 横幅。英国 Data (Use and Access) Act 2025 的统计类 cookie 豁免生效日期、欧盟访客适用口径 → **上线前法务确认项**（§10）。
- `sites/overleap/CLAUDE.md` 的 "no analytics / tracking cookie" 规则改为：**唯一允许的非必要 cookie 是 Center 下发的第一方 `sid`；禁止任何第三方统计脚本；Stripe.js 只允许出现在 `/checkout`**。

## 3. Overleap 站页面（`sites/overleap/`）

链路：`/pricing` → `/checkout?plan=<pid>` → 内嵌支付 → `/welcome` → `/install`；管理在 `/account`。

### 3.1 `/pricing`（全站唯一比价页）

- 两张套餐卡，年付主推：大字折合月价（"£6.58/mo"），下方 "£79 billed yearly · save 34%"（按显示币种实时计算 `1 - yearly / (monthly × 12)`，币种不同百分比不同，如实显示）；月付卡 "Cancel anytime"。
- 价格单一事实源：SSR 读 `/api/plans` 的 `currencyPrices`，`revalidate = 3600`；删除静态价格表。读失败时页面仍渲染，套餐区显示重试态（不显示价格，不伪造）。显示币种：en-GB → GBP，其余 → USD，附 "You're charged in your local currency where available."
- 信任区：30-day money-back guarantee 徽标；"Cancel in two clicks"；支付方式标识（Visa / Mastercard / Amex / Apple Pay / Google Pay）；5 devices · unlimited data · all locations。
- FAQ（付费相关）：refund、iOS 7-day trial（"Subscribing in the iOS app? You get a 7-day free trial; App Store refunds are handled by Apple."）、cancel、devices、payment methods、receipts & VAT。
- JSON-LD：Product + Offer（按显示币种）+ FAQPage，`url` 指向 `/checkout?plan=`。
- CTA：`/checkout?plan=<pid>`，点击时上报 `plan_select`。

### 3.2 `/checkout`

- `/purchase` → 301 `/checkout`，保留 `plan` 参数（旧站 URL 与 Stripe 旧 cancel URL 兼容）。无 `plan` 或 pid 无效 → 默认年付（highlight 套餐）。
- 桌面两栏 / 移动单栏（摘要折叠在顶部）：左侧步骤，右侧订单摘要（套餐、价格、计费周期、"Renews on {date}"、"Full refund until {date}"）。套餐可在摘要内切换（年 / 月）。
- **步骤 1 · 邮箱（内联）**：输入邮箱 → `POST /api/auth/code` → 同页 6 位验证码输入（`inputmode=numeric`、`autocomplete=one-time-code`、支持粘贴、满 6 位自动提交）→ `POST /api/auth/web-login` → 登录态建立。无邀请码框、无跳转。已登录：显示 "Signed in as a***@example.com · Not you?"（Not you → 登出并回到邮箱步骤）。
- **步骤 2 · 支付（Stripe Embedded Checkout）**：`POST /api/user/stripe/checkout {plan, uiMode: "embedded", locale}` → `{clientSecret}` → `@stripe/react-stripe-js` 的 `EmbeddedCheckout` 挂载在页面内。完成后 Stripe 跳 `return_url`。
- **已有订阅**：`user.subscriptions` 非空时不出支付步骤；Stripe → "You're already subscribed" + `/account`；Apple → "You subscribed in the App Store — manage it in Settings on your iPhone."
- **错误**：每个错误码有专属文案（渠道不可用 / 已有订阅 / 套餐无效 / 验证码错误或过期 / 发送过于频繁 / 系统错误）；套餐加载失败 → 重试按钮；Stripe.js 加载失败 → "Payment form didn't load" + 重试。

### 3.3 `/welcome`

- `return_url = {BaseURL}/{locale}/welcome?session_id={CHECKOUT_SESSION_ID}`（带 locale，修正旧回跳丢 locale）。
- 轮询 `/api/user/info`（3s × 10）直到出现活跃 Stripe 订阅；超时兜底 "Payment received — your plan will be active within a few minutes. We've emailed your receipt."
- 激活后：收据摘要（套餐、金额与币种、下次扣款日、账单邮箱、"Full refund available until {date}"）+ 三步上手：① 下载（按 UA 推荐平台 + 其他平台折叠）② 用同一邮箱登录 ③ 连接。上报 `purchase_seen`。
- 未登录访问 → 登录后回到本页（`?next=`）。

### 3.4 `/account`

- 订阅卡：套餐、价格、下次扣款日 / 到期日、续订状态、来源（Stripe / App Store）。
- **退款**：`refundEligibleUntil > now` 时显示 "Request a refund"，确认对话框（shadcn `Dialog`，不用 `window.confirm`）写明 "Full refund of {amount}. Your plan ends immediately." → `POST /api/user/stripe/refund`。
- **取消**：站内流程 → 单选原因（too expensive / not using it enough / speed / connection problems / missing a feature / switching to another service / only needed it temporarily / other + 可选文本）→ `POST /api/user/stripe/cancel` → 显示 "Your plan stays active until {date}" + "Resume subscription"。
- **恢复**：`cancelAtPeriodEnd` 时显示 "Resume" → `POST /api/user/stripe/resume`。
- Stripe Portal 只保留 "Update payment method" 与 "Invoices"。
- Apple 订阅：管理入口指向 App Store 订阅页，不显示退款 / 取消按钮（由 Apple 处理）。

### 3.5 `/install`（最小版）

- 按 UA 推荐平台，其余平台列表展示。桌面：CDN 直链（`Overleap_{VERSION}_{ARCH}.{EXT}`，版本读 CDN latest.json，同 `web/` 现有 `OverleapInstall` 的取法，复制不 import）。iOS / Android：商店链接写在 `lib/site.ts`，为空时显示 "Coming soon"（不写邮件订阅承诺）。
- 点击上报 `install_click`（`p` = 平台）。

## 4. Center API 变更

### 4.1 结账：`POST /api/user/stripe/checkout`

- 请求新增可选字段 `uiMode`（`"hosted"` 默认 | `"embedded"`）与 `locale`（必须 ∈ 请求品牌的站点 locale 白名单，否则忽略）。
- `embedded`：Session `ui_mode=embedded`、`return_url` 如 §3.3；响应 `{clientSecret}`。`hosted` 行为完全不变（webapp 内购买面板继续用）。
- metadata 增 `sid`（仅当 cookie 带合法 sid）。结账时写 `WebSidUser`。
- 品牌门、防双扣、tier 校验等既有守卫顺序不变。

### 4.2 退款：`POST /api/user/stripe/refund`

- 资格（同时以 `refundEligibleUntil` 下发给前端，前后端同一函数判定）：用户有 Stripe 订阅；其**第一条** Stripe `SubscriptionCredit{kind=purchase}` 创建于 30 天内；该用户从未用过保障退款。
- 新模型 `GuaranteeRefund{ID, CreatedAt, UserID uniqueIndex, SubscriptionID, InvoiceID, StripeRefundID, Amount, Currency}` —— `UserID` 唯一索引硬保证"每用户一次"。
- 执行（单事务）：锁用户 → 插入 `GuaranteeRefund`（唯一冲突 → 409）→ Stripe Refund（该首张 invoice 的 PaymentIntent 全额，idempotency key `guarantee-{userUUID}`）→ Stripe 立即取消订阅（不 prorate）→ 复用 `revokeSubscription` 的回收逻辑（`api/logic_apple_iap.go`，截断权益 + `UserProHistory{VipRefund}` + 状态 `revoked`）→ 提交。Stripe 已退款但提交失败 → 告警（`alertStripeCredit` 同通道），重试靠 idempotency key 收敛。
- `charge.refunded` webhook：匹配到 `GuaranteeRefund.StripeRefundID` 时只记日志不告警；其余退款维持告警。
- 月付同样适用（退首期）。

### 4.3 取消 / 恢复

- `POST /api/user/stripe/cancel {reason, note}`：`reason` 枚举（`too_expensive` / `not_using` / `speed` / `connection` / `missing_feature` / `switching` / `temporary` / `other`），`note` ≤ 500 字符。Stripe `cancel_at_period_end=true`；`Subscription` 增列 `cancel_reason`、`cancel_note`、`cancel_requested_at`。`auto_renew` 仍由 `customer.subscription.updated` 同步（单一写入者不变）。
- `POST /api/user/stripe/resume`：`cancel_at_period_end=false`，清空取消三列。
- 均需登录 + 请求品牌允许 Stripe，否则 405001。

### 4.4 `DataSubscription` 下发字段

新增：`planPid`、`amount`（最小货币单位）、`currency`、`interval`（`month` / `year`）、`cancelAtPeriodEnd`、`refundEligibleUntil`（unix 秒，0 = 不可退）、`status`（含 `trialing`）。Stripe 的 amount / currency 取自最近一张已入账 invoice 的实收：`stripeInvoiceFacts` 带出金额与币种，入账时写入 `Subscription` 新增两列 `last_amount` / `last_currency`（不从 plan 反查——Adaptive Pricing 下实收币种可能不是标价币种）。Apple 订阅这两列留空，前端显示商店价。

## 5. 留存（Center）

邮件模板以代码形式放 `api/email_templates_overleap.go`（en / ja，按用户语言），每封按 `(user_id, kind, period_key)` 在新表 `LifecycleEmailLog` 唯一索引去重，webhook 重试不重复发。全部经 asynq 异步发送。

| 邮件 | 触发 | 内容 |
|---|---|---|
| welcome | 首次 Stripe `invoice.paid` 入账提交后；Apple 首笔交易（含试用）入账后 | 套餐、金额、下次扣款日、退款截止日（Stripe）/ 试用结束日（Apple）、三步上手 + 下载链接 |
| activation_nudge | 付款 48h 后（asynq 延时任务），且该用户无任何设备完成过认证 | "Need a hand getting connected?" 安装指引 + 支持邮箱 |
| renewal_notice | 每日 worker：Stripe 年付、`auto_renew`、`current_period_end` ∈ [now+7d, now+8d) | 金额、日期、如何取消（链接 `/account`） |
| payment_failed | 新增 webhook case `invoice.payment_failed` | 更新支付方式链接（`/account` → Portal） |
| refund_confirmation | 保障退款成功后 | 金额、预计到账时间（5–10 个工作日） |
| cancel_confirmation | 站内取消成功后 | 可用至日期 + Resume 链接 |

- 修复 `templateSlugExists` 按品牌过滤（与发送侧 `logic_email_send.go:209` 同口径）：模板不存在 = 静默跳过而非逐条失败。
- overleap 的 `winback-7d` 用 DB 模板（经 `create_edm_template` 建），v1 **不含折扣码**（`BACK90` / `BACK85` 是本地 Campaign，Stripe 结账不认）。
- Stripe Dashboard ops：开启 Smart Retries；webhook 端点新增订阅 `invoice.payment_failed`。

## 6. iOS 7 天试用

- **ASC**：`io.overleap.sub.basic.1y` 配 Introductory Offer — Free Trial、1 week、全部地区（控制台手动）。
- **K2Plugin（Swift）**：`iapGetProducts` 每个商品新增 `introOffer: {period: "P1W", paymentMode: "freeTrial"} | null` 与 `introEligible: bool`（`product.subscription?.isEligibleForIntroOffer`）。`IapHelpers.swift` 同步字段；`definitions.ts` 类型可选。
- **webapp `IosSubscribePanel`**：`introEligible && introOffer` → 主按钮 "Start 7-day free trial"，按钮下 "then {displayPrice}/year · cancel anytime in Settings"（Guideline 3.1.2 披露）；否则维持现状。**特性探测**：字段缺失（旧原生壳 + 新 OTA webapp）→ 旧 UI。订阅状态 `trialing` 显示 "Free trial · ends {date}"。
- **api `creditAppleTransaction`**：`info.OfferType == 1`（introductory）且 `OfferDiscountType == FREE_TRIAL` → 权益延至 `ExpiresDate`、订阅 `status=trialing`、写 `SubscriptionCredit{kind=trial}`、**不调用 `createAppleIAPOrderInTx`**（不建付费单、不返佣）。首次付费续订（`DID_RENEW`）走既有路径建单，自然成为首单。
- 漏斗口径：Overleap "付费" 判定排除 `kind=trial`；试用 → 付费转化率单列。
- 原生改动随下一次 iOS 版本送审，web OTA 带不到。

## 7. 开途侧接入（统计平台的第二个消费者）

- `web/` 根 layout 加 `page_view` 像素（全站），开途购买流（`PurchaseClient.tsx` 的 WordGate / NextPay 下单）加 `plan_select`、`checkout_start`；页脚加退出统计链接；开途隐私政策加 `sid` 条款（中文）。
- `web/` 其余行为不变；GA 去留不在本 spec。

## 8. 法务与文案

- `sites/overleap/public/legal/terms-of-service.md` 退款节重写：30 天保障（首次订阅、每用户一次、原路退回、Stripe 购买适用）、App Store 购买由 Apple 处理、续费后未使用时间不按比例退。
- `privacy-policy.md`：`sid`（§2.5）、Stripe.js 在结账页设置的反欺诈 cookie（严格必要）。
- 全部新文案四个 locale（en-GB 母版 / en-US / en-AU / ja），新 namespace 按 `sites/overleap/CLAUDE.md` 的注册流程。

## 9. 测试

- **api**（真库，`-v` 下 0 SKIP）：
  - `/api/px`：白名单、爬虫过滤、GPC 不下发、optout 不记、限流、Referer 解析（path + utm，外站 Referer 不记 path）、`Set-Cookie` 属性、队列满丢弃不阻塞。
  - 退款：资格边界（第 30 天内 / 外、非首次、已用过、Apple 订阅不可）、唯一约束并发（两个并发请求只成功一个）、Stripe 失败回滚、回收权益与 `UserProHistory`。
  - 取消 / 恢复：Stripe 调用参数、三列写入与清空、品牌门。
  - 结账：`embedded` 返回 `clientSecret`、`return_url` 带合法 locale、非法 locale 忽略、`hosted` 不变。
  - Apple 试用：不建单、不返佣、`kind=trial`、首次续订建单。
  - 生命周期邮件：去重、`templateSlugExists` 品牌过滤、activation_nudge 在已认证设备时不发。
  - 以上 handler 测试单跑与全量各跑一次（包级全局状态坑）。
- **sites/overleap**（vitest，真实 next-intl 渲染）：pricing 价格渲染与失败态、checkout 内联登录状态机、已订阅分支（Stripe / Apple）、每个错误码文案、welcome 轮询与超时、account 退款 / 取消 / 恢复按钮可见性、像素 `<img>` 存在于每个公开页、`/purchase` 301。
- **守卫**：`source-guards` 增 "Stripe.js 只出现在 checkout 路由"、"无第三方统计域名"；每个新守卫做变异验证。
- **webapp**：`IosSubscribePanel` 有资格 / 无资格 / 字段缺失三态。
- **端到端（Stripe 测试模式）**：本地 Center + `stripe listen` → 新站 `/pricing` 选月付 → 内联邮箱登录 → 内嵌支付 4242 → `/welcome` 激活 → `/account` 取消 → 恢复 → 退款 → 权益回收；全程在浏览器里走，并核对 `web_events` / `web_sid_users` 行与 `web-funnel` 报表数字。截图交付。
- **iOS**：StoreKit 沙盒真机试用购买 → Center 无订单、`trialing` → 沙盒加速续订 → 建单。

## 10. 待确认 / ops 项

1. 法务：DUAA 2025 统计 cookie 豁免生效日与欧盟访客口径（决定是否需要横幅）。
2. 真实客户端 IP 能否经 Amplify SSR rewrite 链到达 Center（决定 `Country` 是否可用）。
3. Stripe publishable key（test / live）进 Amplify 环境变量，并加入 `amplify.yml` 的烘焙白名单。
4. Stripe Dashboard：Smart Retries、webhook 新增 `invoice.payment_failed`、确认 Portal 仍禁 plan switching。
5. ASC 配置 Introductory Offer；Overleap iOS / Android 商店链接上线后填 `lib/site.ts`。
6. 隐私政策 "Overleap LLC registered under U.S. law" 表述（09-30 spec 遗留）仍待法务。

## 11. 分期

| 期 | 内容 | 发布 |
|---|---|---|
| ① | §2 统计平台（Center）、§3 Overleap 站全部页面、§4 Center API、§8 法务文案 | `make deploy-api` → 新站预览域名验证（切换随 09-30 spec 阶段 ④） |
| ①b | §7 开途 `web/` 接入 | `git push origin main:website` |
| ② | §5 留存邮件与 worker 修复 | `make deploy-api` |
| ③ | §6 iOS 试用 | ASC 配置 + 下一个 iOS 原生版本 + web OTA（`webapp/*` tag） |

每期单独出 implementation plan。

## 12. 不做（YAGNI）

- 网页免费试用；月付→年付即时升级 / proration；Portal plan switching。
- 挽回折扣、推荐 / 邀请奖励。
- 首页营销改版、`/support`（09-30 spec 阶段 ③ 其余部分）。
- Android 购买入口（Play 版维持无购买）。
- 第三方统计、A/B 测试框架（有了漏斗数据后另议）。
