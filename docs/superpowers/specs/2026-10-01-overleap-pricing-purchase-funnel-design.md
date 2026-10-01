# Overleap 定价 → 购买 → 留存 + 转化统计平台（双品牌 · 官网 + App）— 设计 spec

日期：2026-10-01 · 基线：`main` @ ab511103
前序：`2026-09-30-overleap-site-app-design.md`（本 spec = 其阶段 ② 定价 + 阶段 ③ 中 `/pricing`→结账部分）、`2026-07-22-overleap-stripe-web-design.md`（Stripe 后端既有资产）

## 0. 决策（2026-10-01 与 David）

| # | 决策 | 理由 |
|---|---|---|
| 1 | **风险逆转分渠道**：iOS = Apple 原生 7 天免费试用（Introductory Offer）；官网（Stripe）= 30 天无理由退款；Android 维持无购买入口 | IAP 退款权在 Apple，官网承诺退款在 iOS 上兑现不了；试用是 StoreKit 原生能力，iOS 上最丝滑。网页试用要绑卡、且是测卡重灾区，网页行业标准是 30 天退款（NordVPN 同型） |
| 2 | **转化统计全部落 Center（Go）+ 现有 MySQL**；官网用第一方 `sid` cookie + 1px 像素 | Next 不掺合后端逻辑；不引入第三方统计、不引入新数据库（DynamoDB 方案已否） |
| 3 | **统计是品牌无关的平台能力，覆盖官网 + webapp（App 内）+ 服务端事实**，一条事件流、一组命名转化路径、一个后台看板页 | 目的是转化：漏斗的一半在 App 里（激活、付费墙、试用）；品牌只是数据维度 |
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

目的只有一个：**量出每条转化路径在哪一步丢人**。

### 2.0 架构原则（本节每一处设计都从这里推出）

1. **行为与事实分开。** 客户端（官网、App）只上报**行为**——看见了、点了、发起了支付。钱与账户的**事实**（注册、付款、续费、试用、退款、取消、恢复、发信）**不进事件表**，查询时从拥有它的事实表投影出来（§2.4）。事实只有一个源，不复制、不丢、历史即刻可用，支付路径零改动。
2. **每个事件只有一个生产面。** 注册表为每个事件声明允许的 `surface`，写入侧强制；面不匹配的事件直接丢弃。
3. **注册表是唯一定义处。** 事件（名字、生产面、`kind`）与路径（步骤、窗口）都在 Go 注册表里；TS 侧类型由契约文件锁一致。事件语义不可变：含义变了就起新名字。
4. **原样存，查询时归并。** 事件存原始匿名身份与当时的登录用户；"谁是同一个人"在查询时由 `FunnelIdentity` 解析，从不回写历史行。
5. **投递语义写明。** 官网像素 = 至多一次（不重试，丢了就丢了）；App 事件 = 至少一次 + `eid` 去重；事实 = 不丢（读表）。
6. **统计永远不拖累产品。** 上报请求不等写库；队列满就丢；任何统计故障不影响登录、支付、连接。
7. **最小化。** 不存 IP、不存 UA 原文、不含任何连接 / 流量 / 目的地信息。
8. **品牌是维度，不是结构。** 表、事件、路径、接口都没有按品牌分叉的东西。

### 2.1 行为事件：两个上报面，一张表

| 面 `surface` | 谁上报 | 通道 | 匿名身份 | 投递 |
|---|---|---|---|---|
| `web` | 官网（`sites/overleap`、`web/`） | 1px 像素 `GET /api/px` | `sid` cookie | 至多一次 |
| `app` | webapp（桌面 / iOS / Android） | 既有 `POST /api/stats/events` 增一个 `funnel` 数组 | `did` = 既有 `device_hash` | 至少一次 + 去重 |

```go
// FunnelEvent：行为事件。只追加。
type FunnelEvent struct {
    ID          uint64    `gorm:"primarykey"`
    OccurredAt  time.Time `gorm:"index:idx_brand_event_time,priority:3;index:idx_anon_time,priority:2;index:idx_user_time,priority:2"`
    ReceivedAt  time.Time `gorm:"autoCreateTime;index"` // 保留期按它删
    Eid         *string   `gorm:"type:varchar(36);uniqueIndex"` // app 面客户端生成；web 面为 NULL
    Brand       string    `gorm:"type:varchar(16);not null;index:idx_brand_event_time,priority:1"`
    Surface     string    `gorm:"type:varchar(8);not null"` // web|app
    Event       string    `gorm:"type:varchar(32);not null;index:idx_brand_event_time,priority:2"`
    AnonID      string    `gorm:"type:varchar(64);index:idx_anon_time,priority:1"` // sid 或 did；GPC 访客为空
    UserID      uint64    `gorm:"index:idx_user_time,priority:1"`                  // 上报当时的登录用户，0 = 未登录
    Plan        string    `gorm:"type:varchar(64)"`
    Source      string    `gorm:"type:varchar(32)"` // 入口标签（付费墙来源等）
    Channel     string    `gorm:"type:varchar(16)"` // stripe|apple|wordgate|nextpay
    Path        string    `gorm:"type:varchar(255)"` // web：去掉 locale 前缀的路径
    RefHost     string    `gorm:"type:varchar(128)"`
    UtmSource   string    `gorm:"type:varchar(64)"`
    UtmMedium   string    `gorm:"type:varchar(64)"`
    UtmCampaign string    `gorm:"type:varchar(64)"`
    Country     string    `gorm:"type:varchar(2)"`
    Device      string    `gorm:"type:varchar(16)"` // desktop|mobile|tablet
    OS          string    `gorm:"type:varchar(16)"` // windows|macos|ios|android|linux|other
    AppVersion  string    `gorm:"type:varchar(32)"`
}

// FunnelIdentity：匿名身份 ↔ 用户。一个匿名身份可对多个用户（共用设备），反之亦然。
type FunnelIdentity struct {
    ID        uint64    `gorm:"primarykey"`
    CreatedAt time.Time
    Kind      string    `gorm:"type:varchar(4);not null;uniqueIndex:uniq_identity,priority:1"` // sid|did
    AnonID    string    `gorm:"type:varchar(64);not null;uniqueIndex:uniq_identity,priority:2"`
    UserID    uint64    `gorm:"not null;uniqueIndex:uniq_identity,priority:3;index"`
    Brand     string    `gorm:"type:varchar(16);not null"`
}
```

- `UserID` 由 Center 从登录态写入（cookie / Bearer），**上报体里没有 user 字段**。
- **时间**：`web` 面 `OccurredAt` = 服务器时间。`app` 面取客户端 `created_at`（离线排队后补发），但若它在未来、或比 `ReceivedAt` 早 7 天以上，则改用 `ReceivedAt`——不让坏时钟打乱步骤顺序。
- **写入**：进程内有界队列（10k），每 100 条或 2 秒批量 `INSERT IGNORE`（`Eid` 冲突即重复，忽略）；满则丢弃并每分钟汇总一行 warn。
- **保留**：`ReceivedAt` 起 120 天（看板区间上限 90 天 + 最长行为窗口 14 天，留余量），沿用 `worker_stats_retention.go` 模式每日分批删除。`FunnelIdentity` 不过期。超过 120 天的问题由事实表回答（§2.7）。
- **关联写入点**（`FunnelIdentity` upsert，不产生事件）：web 登录成功 → `sid`；带登录态的 `/api/stats/events` → `did`；浏览器打开 `GET /api/orders/:uuid/pay`（App 内发起支付的外部跳转，浏览器直达 Center）→ 把 `sid` 关联到订单用户——App 用户与其网页身份在此连通。

### 2.2 `web` 面：`sid` + 像素

**Cookie `sid`**：由 Center 下发，写法同 `setAuthCookies`（`api/api_auth.go:706`）——`Path=/`、Domain 留空（经同源代理落在浏览器所见 host）、`HttpOnly`、`Secure`、`SameSite=Lax`、Max-Age 13 个月；值 128 位随机 base64url；值 `optout` = 访客退出。名字品牌中立，两站同名。**Next 不读不写它**。

**`GET /api/px`**（匿名，品牌走既有 BrandResolver）：

- 参数：`e` 事件名（白名单）、`p` 套餐 pid、`s` 来源标签、`r` 外部来源 host（仅访客首个事件，由前端从 `document.referrer` 取 host，丢弃路径）。
- 路径与 `utm_*` 从 `Referer` 头解析（同源才记 path；去掉 locale 前缀归一化，如 `/en-GB/pricing` → `/pricing`）。
- 响应 43 字节透明 GIF + `no-store`；无 `sid` 时 `Set-Cookie`。
- 不记：`sid == optout`、爬虫 UA、超限流（每 IP 120 次/分钟，静默返回 GIF）。`Sec-GPC: 1`：不下发 `sid`，事件照记但 `AnonID` 为空。
- `GET /api/px/optout`：下发 `sid=optout`，302 回同源 Referer（否则 `/`）。
- 前端两种形态都只是图片请求：SSR 内嵌 `<img src="/api/px?e=…" width="1" height="1" alt="" aria-hidden>`；交互 `new Image().src = …`。

### 2.3 `app` 面：复用 `statsService`

- `POST /api/stats/events` 请求体新增 `funnel: [{eid, event, plan, source, channel, created_at}]`（`eid` = 入队时生成的 UUID，重试带同一个），与 `app_opens` / `connections` 共用既有持久化队列、批量 flush、失败留队重试（`webapp/src/services/stats.ts`）；`device_hash` / `os` / `app_version` 沿用既有字段。旧 Center 忽略未知字段，新旧互兼。
- 品牌取 `X-K2-Brand`；带有效登录态时 Center 写 `UserID` 并 upsert `FunnelIdentity{did}`。
- "只发一次"的事件（`app_first_open`、`first_connect_ok`）由 webapp 在 `_platform.storage` 记标志位。
- webapp 新增 `funnel.track(event, {plan, source, channel})` 一个入口，事件名是 TS 联合类型，与 Go 白名单由契约测试锁一致（进 `contracts/api-contract.json` 的 `funnelEvents`）。

### 2.4 事实：从事实表投影，不写事件

路径引擎通过**事实适配器**在查询时把事实表的行读成与 `FunnelEvent` 同形的记录（带 `UserID`、时间、`plan`、`channel`），与行为事件合并后一起算。每个事实一个小查询函数，集中在 `api/logic_funnel_facts.go`：

| 事实 | 来源 |
|---|---|
| `signup` | `users.created_at` |
| `purchase` / `renewal` | 一个用户的全部付款按时间排序，第一笔 = `purchase`，其后 = `renewal`。付款 = `orders`（`is_paid`，**排除 Apple 渠道**）∪ `subscription_credits`（`kind ∈ {purchase, renewal}`，Stripe 与 Apple）。Apple 同时写订单与入账行，只取入账行，保证每笔付款只计一次；Stripe 入账不建订单 |
| `trial_start` | `subscription_credits{kind=trial}`（§6） |
| `refund` | `GuaranteeRefund`（§4.2）∪ `orders.is_refunded` ∪ 订阅 `status=revoked` |
| `cancel_request` / `resume` | `SubscriptionIntent`（§4.3，只追加） |
| `email_sent` | `LifecycleEmailLog`（§5），`source` = 邮件 kind |

- 事实永远带 `UserID`，没有匿名身份；与行为事件靠 `FunnelIdentity` 在"人"的层面对上。
- 行为侧的"发起支付" `checkout_start` 是客户端事件（用户点了去支付），不是事实。

### 2.5 事件注册表

`api/logic_funnel_events.go`：每个事件声明 `name`、允许的 `surface`、`kind`（`view` 页面 / 屏幕浏览，`action` 操作）。写入侧校验：未注册的事件、或从未声明的面发来的事件 → 丢弃（像素仍返回 GIF）。事实名（§2.4）也在注册表里、`surface` 为空集——所以任何客户端发 `purchase` 都会被丢。导出进 `contracts/api-contract.json` 的 `funnelEvents`。

| 事件 | 面 | kind |
|---|---|---|
| `page_view` · `pricing_view` · `checkout_view` · `install_view` · `welcome_view` | web | view |
| `install_click`（`p` = 平台）· `checkout_cancelled` · `refund_click` · `cancel_click` | web | action |
| `app_first_open` · `first_connect_attempt` · `first_connect_ok` · `manage_click` | app | action |
| `login_view` · `paywall_view`（`source` = 入口） | app | view |
| `plan_select` · `auth_code_sent` · `auth_done` · `checkout_start`（`channel`） | web, app | action |

`paywall_view.source` 取值（对应 webapp 现有全部购买入口）：`tunnel_locked`（`CloudTunnelList`）· `membership_guard` · `login_dialog` · `account` · `account_expired` · `direct`。

### 2.6 转化路径（命名漏斗）

定义在 Go 注册表 `api/logic_funnel_paths.go`（代码而非 DB：路径变更走 review + 测试）。每条路径 = 有序步骤，每步 = 一个或多个事件名（任一命中，表中以 `/` 分隔）+ 可选过滤（`surface` / `source` / `channel` / `os`）；归因窗口（步骤 1 起算）。

**路径用品牌无关的语义步骤定义，品牌只是查询时的筛选条件**——没有按品牌命名的路径，也没有"适用品牌"字段；某品牌不产生某条路径的事件（如开途没有 iOS 试用），看板显示为空即可。主路径只放**每个人都必须经过**的步骤；条件性步骤（登录：已登录的人不会再触发）单列为诊断路径，否则回头客会被算成流失。

| key | 回答的问题 | 步骤 | 窗口 |
|---|---|---|---|
| `web_purchase` | 官网访客变付费 | `page_view`(首访) → `pricing_view` → `plan_select` → `checkout_start` → `purchase` | 14 天 |
| `web_checkout_auth` | 结账里的登录卡不卡人 | `auth_code_sent`(surface=web) → `auth_done`(surface=web) → `checkout_start` | 1 天 |
| `web_install` | 官网访客去下载 | `page_view` → `install_view` → `install_click` | 7 天 |
| `app_activation` | 装了的人连上没有 | `app_first_open` → `auth_done`(surface=app) → `first_connect_attempt` → `first_connect_ok` | 7 天 |
| `app_purchase` | App 内付费墙变付费 | `paywall_view` → `plan_select`(surface=app) → `checkout_start` → `purchase` | 7 天 |
| `trial` | 试用变付费 | `paywall_view` → `checkout_start`(channel=apple) → `trial_start` → `purchase` | 14 天 |
| `post_purchase_activation` | 付了钱的人用上没有 | `purchase` → `first_connect_ok` | 7 天 |
| `email_return` | 生命周期邮件拉回 | `email_sent` → `page_view`(utm_source=email) → `checkout_start` → `purchase`/`renewal` | 14 天 |
| `cancel_save` | 取消的人回来没有 | `cancel_request` → `resume` | 至账期末（上限 370 天） |

`pricing_view` 在两站各指本品牌的选套餐页（Overleap `/pricing`，开途 `/purchase`）；`checkout_view` / `welcome_view` / `login_view` 仍作为页面浏览记录、可在明细里看，但不进任何路径的必经步骤。

**计算口径**（一个纯函数，喂事件切片，单测覆盖）：

- **人**的归并（查询时，不回写）：记录带 `UserID` → 人 = 该用户；否则查 `FunnelIdentity(kind, AnonID)`，有关联取最早关联的用户，无则人 = 匿名身份本身。所以登录前的浏览、登录后的点击、事实表里的付款算同一个人。
- 输入 = 区间内的行为事件 ∪ 同区间（加窗口）的事实投影；计算是一个不碰数据库的纯函数。
- 进入路径 = 在所选时间段内发生步骤 1；后续步骤须按顺序、时间不早于前一步、且在窗口内。每步去重计人。
- 输出：每步人数、相邻步转化率、总转化率、相邻步耗时中位数。
- 分组维度：`source`（首触 `RefHost` / `utm_source`）· `utm_campaign` · `country` · `os` · `device` · `app_version` · `paywall_source` · `plan` · `channel`。维度取自该人在步骤 1 的事件（`plan` / `channel` 取自末步）。
- `web → app` 的跨端（官网点下载 → App 首开）**不强行拼接**：安装包带不了 `sid`，两端只在同一用户两边都登录后才连通。`web_install` 与 `app_activation` 因此是两条独立路径，看板并排显示而不伪造一条连续漏斗。

### 2.7 留存（群组）

不走事件表，直接读事实表（行为事件只留 120 天，事实表永久）：

- **付费留存**：按首次付费月份分群，M1 / M3 / M6 / M12 仍有有效权益（`users.expired_at` 未过期且非退款）的比例；按品牌 / 首购套餐 / 渠道切分。
- **30 天退款率**：`GuaranteeRefund` 与开途退款单 / 首购数。
- **活跃留存**：既有 `StatAppOpen`，按 `device_hash` 首次出现日分群的 D1 / D7 / D30。
- **取消原因分布**：`SubscriptionIntent`（§4.3）。

### 2.8 接口与看板页

- `GET /app/stats/funnels` — 路径注册表（key、标题、步骤、窗口）。
- `GET /app/stats/funnels/:key?brand=&from=&to=&groupBy=` — 单条路径结果（区间上限 90 天）。
- `GET /app/stats/retention?brand=&cohort=month&metric=paid|active|refund|cancel_reasons`。
- 规模边界：路径在查询时对原始事件计算（当前量级每天万级事件，90 天区间内存里算）。单次查询事件数超过 200 万时接口拒绝并提示缩小区间——到那一步再加按天预聚合，现在不建。
- 均为 admin + `RoleMarketing`；`brand` 走既有 `parseBrandFilter`。
- kaitu-center MCP 新增工具 `funnel`（列路径 / 查单条）与 `retention`。
- **看板页 `web/src/app/(manager)/manager/funnels/page.kaitu.tsx`**（管理后台只存在于开途构建，管两个品牌的数据，品牌用筛选器切）：
  - 顶部：品牌 · 时间段 · 路径选择 · 分组维度。
  - **漏斗图**：横向条，每步人数 + 相对上一步的转化率 + 流失人数；分组时并排小图。
  - **趋势**：总转化率按天的折线。
  - **明细表**：分组维度 × 各步人数，可排序（找出哪个来源 / 入口 / 版本最差）。
  - **留存**页签：群组表格（行 = 首购月，列 = M1…M12）+ 退款率 + 取消原因条形图。
  - 图表按 `dataviz` skill 的规范实现；沿用后台既有的 shadcn + 图表库。

### 2.9 合规

- 两品牌隐私政策各自写明：`sid`（官网）与 App 内产品流程事件（含登录后与账户关联）的用途、保留期、不与第三方共享、不含连接或流量内容；官网退出方式。
- 官网页脚 "Do not count my visits" / 「不参与访问统计」→ `/api/px/optout`。不设 cookie 横幅；英国 DUAA 2025 统计 cookie 豁免生效日与欧盟访客口径 → **上线前法务确认项**（§10）。
- App 面此前的统计刻意不带用户（`StatAppOpen` 只有设备哈希）；本设计让流程事件在登录后关联到用户——这是隐私口径的一次**有意变更**，须在隐私政策落字后才能发版。既有 `app_opens` / `connections` 两类事件维持不带用户。
- `sites/overleap/CLAUDE.md` 的规则改为：唯一允许的非必要 cookie 是 Center 下发的第一方 `sid`；禁止任何第三方统计脚本；Stripe.js 只允许出现在 `/checkout`。

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
- 激活后：收据摘要（套餐、金额与币种、下次扣款日、账单邮箱、"Full refund available until {date}"）+ 三步上手：① 下载（按 UA 推荐平台 + 其他平台折叠）② 用同一邮箱登录 ③ 连接。页面像素 `welcome_view`。
- 未登录访问 → 登录后回到本页（`?next=`）。

### 3.4 `/account`

- 订阅卡：套餐、价格、下次扣款日 / 到期日、续订状态、来源（Stripe / App Store）。
- **退款**：`refundEligibleUntil > now` 时显示 "Request a refund"，确认对话框（shadcn `Dialog`，不用 `window.confirm`）写明 "Full refund of {amount}. Your plan ends immediately." → `POST /api/user/stripe/refund`。点击上报 `refund_click`。
- **取消**：站内流程 → 单选原因（too expensive / not using it enough / speed / connection problems / missing a feature / switching to another service / only needed it temporarily / other + 可选文本）→ `POST /api/user/stripe/cancel` → 显示 "Your plan stays active until {date}" + "Resume subscription"。进入流程上报 `cancel_click`。
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
- metadata 增 `sid`（仅当 cookie 带合法 sid，排障用，不作归因依据）。结账时 upsert `FunnelIdentity{sid}`。
- 品牌门、防双扣、tier 校验等既有守卫顺序不变。

### 4.2 退款：`POST /api/user/stripe/refund`

- 资格（同时以 `refundEligibleUntil` 下发给前端，前后端同一函数判定）：用户有 Stripe 订阅；其**第一条** Stripe `SubscriptionCredit{kind=purchase}` 创建于 30 天内；该用户从未用过保障退款。
- 新模型 `GuaranteeRefund{ID, CreatedAt, UserID uniqueIndex, SubscriptionID, InvoiceID, StripeRefundID, Amount, Currency}` —— `UserID` 唯一索引硬保证"每用户一次"。
- 执行（单事务）：锁用户 → 插入 `GuaranteeRefund`（唯一冲突 → 409）→ Stripe Refund（该首张 invoice 的 PaymentIntent 全额，idempotency key `guarantee-{userUUID}`）→ Stripe 立即取消订阅（不 prorate）→ 复用 `revokeSubscription` 的回收逻辑（`api/logic_apple_iap.go`，截断权益 + `UserProHistory{VipRefund}` + 状态 `revoked`）→ 提交。Stripe 已退款但提交失败 → 告警（`alertStripeCredit` 同通道），重试靠 idempotency key 收敛。
- `charge.refunded` webhook：匹配到 `GuaranteeRefund.StripeRefundID` 时只记日志不告警；其余退款维持告警。
- 月付同样适用（退首期）。

### 4.3 取消 / 恢复

- `POST /api/user/stripe/cancel {reason, note}`：`reason` 枚举（`too_expensive` / `not_using` / `speed` / `connection` / `missing_feature` / `switching` / `temporary` / `other`），`note` ≤ 500 字符。Stripe `cancel_at_period_end=true`；追加一行 `SubscriptionIntent{ID, CreatedAt, SubscriptionID index, UserID index, Kind(cancel|resume), Reason, Note, Origin(site|apple)}`（**只追加**：取消史与原因永久保留，不随恢复被清掉）。`auto_renew` 仍由 `customer.subscription.updated` 同步（单一写入者不变）。
- `POST /api/user/stripe/resume`：`cancel_at_period_end=false`，追加一行 `Kind=resume`。Apple 的 `DID_CHANGE_RENEWAL_STATUS` 同样各追加一行（`Origin=apple`，无原因）。
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
- 漏斗口径：试用入账行是 `kind=trial`（投影为 `trial_start`，不算付款）；首次付费续订是该用户第一笔付款（投影为 `purchase`）。试用 → 付费即路径 `trial`。面板调 `iapPurchase` 前上报 `checkout_start`（`channel=apple`）。`trial_start` / `purchase` 是事实，由 §2.4 从入账行投影。
- 原生改动随下一次 iOS 版本送审，web OTA 带不到。

## 7. 埋点接入清单（统计平台的消费者）

**`sites/overleap/`**：随 §3 各页面实现，事件见 §2.5 web 行。一个 `<Pixel event=… />` 服务端组件 + 一个 `track()` 客户端函数（各 ≤ 20 行，只拼 URL）。

**`web/`（开途官网）**：根 layout 加 `page_view`；购买页 `pricing_view`、`plan_select`、页内登录的 `auth_code_sent` / `auth_done`；`/install` 加 `install_view` / `install_click`；页脚加退出统计链接；隐私政策加条款（中文）。点「去支付」上报 `checkout_start`（真实下单才报，预览试算不报）；付款是事实，不上报。GA 去留不在本 spec。

**`webapp/`（两品牌、全平台）**：

| 事件 | 埋点位置 |
|---|---|
| `app_first_open` | `main.tsx` 启动处，`statsService.trackAppOpen` 旁；storage 标志位保证只发一次 |
| `login_view` · `auth_code_sent` · `auth_done` | `LoginDialog.tsx` |
| `first_connect_attempt` · `first_connect_ok` | `vpn-machine.store.ts` 的连接发起与进入 connected；storage 标志位 |
| `paywall_view`（带 `source`） | `Purchase.tsx` 挂载；`source` 由五个入口经路由 state 传入（`CloudTunnelList` / `MembershipGuard` / `LoginDialog` / `Account` ×2），缺省 `direct` |
| `plan_select` | `Purchase.tsx`（开途）、`StripePurchasePanel`、`IosSubscribePanel` |
| `checkout_start`（带 `channel`） | 用户点去支付时：`Purchase.tsx`（开途真实下单，预览不报）、`StripePurchasePanel`、`IosSubscribePanel` 调 `iapPurchase` 前 |
| `manage_click` | `IosMembershipPanel` / `StripePurchasePanel` 的管理入口 |

- Overleap Android 无购买入口 → 不出 `paywall_view`（既有 `purchase-surface.android` 守卫不变）。
- webapp 改动经 `webapp/x.y.z` tag 发 web OTA 才到客户端（两品牌）。

## 8. 法务与文案

- `sites/overleap/public/legal/terms-of-service.md` 退款节重写：30 天保障（首次订阅、每用户一次、原路退回、Stripe 购买适用）、App Store 购买由 Apple 处理、续费后未使用时间不按比例退。
- `privacy-policy.md`：`sid` 与 App 内流程事件（§2.9）、Stripe.js 在结账页设置的反欺诈 cookie（严格必要）。
- 全部新文案四个 locale（en-GB 母版 / en-US / en-AU / ja），新 namespace 按 `sites/overleap/CLAUDE.md` 的注册流程。

## 9. 测试

- **api**（真库，`-v` 下 0 SKIP）：
  - `/api/px`：白名单、爬虫过滤、GPC 不下发、optout 不记、限流、Referer 解析（path 去 locale + utm，外站 Referer 不记 path）、`Set-Cookie` 属性、队列满丢弃不阻塞。
  - `/api/stats/events` 的 `funnel` 数组：未知事件丢弃、登录态写 `UserID` 与 `FunnelIdentity{did}`、请求体里伪造的 user 字段被忽略、旧请求体（无 `funnel`）行为不变。
  - 事件注册表强制：未注册事件丢弃；面不匹配丢弃（app 发 `page_view`、任何客户端发 `purchase`）。
  - 幂等与时间：同一 `eid` 重发只落一行；客户端时间在未来 / 早于 7 天 → 取接收时间。
  - 事实适配器：首笔 = `purchase` 其后 = `renewal`；Apple 一笔付款只计一次（订单与入账行不重复）；Stripe 无订单也能投影；试用不算付款；退款三种来源。
  - **路径计算纯函数**（表驱动）：匿名→登录归并为同一人、乱序事件不计、超窗口不计、同一人重复事件只计一次、多用户共用一个 sid、分组维度取步骤 1。
  - 路径注册表守卫：每条路径的每个步骤都是已注册的事件或事实、步骤数 ≥ 2、key 不含品牌词；`FunnelEvent` 等模型与路径定义里没有按品牌分叉的字段。
  - 留存：群组边界（月初 / 月末）、退款用户不计入留存。
  - 跨端接缝：`/api/orders/:uuid/pay` 把 sid 关联到订单用户。
  - 退款：资格边界（第 30 天内 / 外、非首次、已用过、Apple 订阅不可）、唯一约束并发（两个并发请求只成功一个）、Stripe 失败回滚、回收权益与 `UserProHistory`。
  - 取消 / 恢复：Stripe 调用参数、`SubscriptionIntent` 只追加（恢复后取消行仍在）、品牌门。
  - 结账：`embedded` 返回 `clientSecret`、`return_url` 带合法 locale、非法 locale 忽略、`hosted` 不变。
  - Apple 试用：不建单、不返佣、`kind=trial`、首次续订建单。
  - 生命周期邮件：去重、`templateSlugExists` 品牌过滤、activation_nudge 在已认证设备时不发。
  - 以上 handler 测试单跑与全量各跑一次（包级全局状态坑）。
- **sites/overleap**（vitest，真实 next-intl 渲染）：pricing 价格渲染与失败态、checkout 内联登录状态机、已订阅分支（Stripe / Apple）、每个错误码文案、welcome 轮询与超时、account 退款 / 取消 / 恢复按钮可见性、像素 `<img>` 存在于每个公开页、`/purchase` 301。
- **守卫**：`source-guards` 增 "Stripe.js 只出现在 checkout 路由"、"无第三方统计域名"；每个新守卫做变异验证。
- **webapp**：`IosSubscribePanel` 有资格 / 无资格 / 字段缺失三态；`funnel.track` 入队与批量 flush、一次性事件的标志位、五个购买入口各自的 `paywall_view.source`；双品牌套件都跑。
- **契约**：`contracts/api-contract.json` 增 `funnelEvents`，webapp 与两个站的事件类型与 Go 注册表（名字 + 允许的面）由契约测试锁一致（`UPDATE_CONTRACT=1 go test -count=1 -run TestExportContract ./...` 重生成）。
- **看板页**：真实接口数据渲染（空数据 / 单步 / 分组三态），浏览器实测截图。
- **端到端（Stripe 测试模式）**：本地 Center + `stripe listen` → 新站 `/pricing` 选月付 → 内联邮箱登录 → 内嵌支付 4242 → `/welcome` 激活 → `/account` 取消 → 恢复 → 退款 → 权益回收；全程在浏览器里走，并核对 `funnel_events` / `funnel_identities` 行，以及看板页 `web_purchase`、`web_checkout_auth`、`cancel_save` 三条路径的数字与手工步骤一致。截图交付。
- **iOS**：StoreKit 沙盒真机试用购买 → Center 无订单、`trialing` → 沙盒加速续订 → 建单。

## 10. 待确认 / ops 项

1. 法务：DUAA 2025 统计 cookie 豁免生效日与欧盟访客口径（决定是否需要横幅）。
2. 真实客户端 IP 能否经 Amplify SSR rewrite 链到达 Center（决定 `Country` 是否可用）。
3. Stripe publishable key（test / live）进 Amplify 环境变量，并加入 `amplify.yml` 的烘焙白名单。
4. Stripe Dashboard：Smart Retries、webhook 新增 `invoice.payment_failed`、确认 Portal 仍禁 plan switching。
5. ASC 配置 Introductory Offer；Overleap iOS / Android 商店链接上线后填 `lib/site.ts`。
6. 隐私政策 "Overleap LLC registered under U.S. law" 表述（09-30 spec 遗留）仍待法务。

## 11. 分期

统计平台先行：后面每一期的页面从第一天起就带埋点，同时先拿到开途现有漏斗的基线。

| 期 | 内容 | 发布 |
|---|---|---|
| ⓪ 统计平台 | §2 全部（事件表、像素、`stats/events` 扩展、事实适配器、路径引擎、留存、接口、MCP 工具、看板页）+ §7 的 `web/` 与 `webapp/` 埋点 + 两品牌隐私政策条款 | `make deploy-api` → `git push origin main:website` → `webapp/*` tag |
| ① Overleap 购买 | §3 新站全部页面（含埋点）、§4 Center API、§8 法务文案 | `make deploy-api` → 新站预览域名验证（切换随 09-30 spec 阶段 ④） |
| ② 留存 | §5 邮件与 worker 修复 | `make deploy-api` |
| ③ iOS 试用 | §6 | ASC 配置 + 下一个 iOS 原生版本 + `webapp/*` tag |

每期单独出 implementation plan。⓪ 的隐私政策条款落字是其发版前置（§2.9）。

## 12. 不做（YAGNI）

- 网页免费试用；月付→年付即时升级 / proration；Portal plan switching。
- 挽回折扣、推荐 / 邀请奖励。
- 首页营销改版、`/support`（09-30 spec 阶段 ③ 其余部分）。
- Android 购买入口（Play 版维持无购买）。
- 第三方统计、A/B 测试框架（有了漏斗数据后另议）。
- 可在后台自定义的漏斗编辑器（路径在代码注册表里）；官网 → App 的跨端强行拼接；按渠道定制的落地页。
