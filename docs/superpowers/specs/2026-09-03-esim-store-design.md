# eSIM 商店设计（号码优先）

- 日期：2026-09-03（v2，按「网站只卖、app 只用、NextPay 订阅」修订）
- 状态：设计已认可，待写实施计划
- 范围：Center API（`api/`）、网站（`web/`）、webapp、K2Plugin 原生（Android/iOS）
- 品牌：后端与网站按双品牌参数化；**app 入口与网站商店 v1 只对开途开放**；NextPay 的 Go SDK 是每进程单 App，Overleap 接入前需 SDK 支持多客户端（见 §12）
- 调研原始材料：附录 C（本会话调研的压缩版；WebSearch 配额在第三轮前耗尽，未验证项已标注）

---

## 0. 决策摘要

| 决策 | 结论 |
|---|---|
| 产品定位 | **海外号码 eSIM 为主**（用户要的是「一个能收验证码的海外号码」），旅行流量 eSIM 为辅 |
| 职责切分 | **网站只管卖**（目录、结账、订阅管理、我的 eSIM 只读视图）；**app 只管用**（我的 eSIM、原生安装 SDK、用量与到期）。app 内不出现任何结账页 |
| 支付 | **NextPay**（`github.com/wordgate/qtoolkit/nextpay`）：号码线 = NextPay 订阅（卡自动续费，支付宝/微信由 NextPay 发 14/7/3/1 天提醒手动续费），流量线 = NextPay 一次性订单。VPN 的 WordGate / Stripe / IAP 轨道**不动** |
| 安装 | K2Plugin 新增 `esimSupported` / `esimInstall` 作为 webapp 直接调用的原生 SDK：iOS 内部走 17.4+ 通用链接，Android 走 `EuiccManager` 系统授权安装，GMS 通用链接兜底；桌面与其他设备出示二维码 + 手动码 |
| app 入口 | 发现页（网站 `/discovery`）加 eSIM 卡片 → 系统浏览器打开网站商店，**自有域名链接自动带 OTT 免登录**；账户页加「我的 eSIM」 |
| 流量供应商 | eSIM Access（唯一公开完整 API、自助开户、无 MOQ、含中国大陆套餐） |
| 号码供应商 | **待 pilot 决定**：候选 esim.tech（API-first、25+ 国真本地号、无最低）与 Telnyx（自助、价格透明）；用户列的五家中只有 eSIM Go 有号且仅英国 |
| v1 号码国家 | 美国 +1、英国 +44（无需实名、渠道最多），荷兰 +31 备选 |
| v2 | 日本（护照实名路径已验证，约 $115/年）、泰国/马来西亚（护照可办，渠道待找）、香港（监管收紧，仅 1GLOBAL） |
| 实名（KYC） | v1 不做；只上无需实名国家 |
| 数据模型 | 目录独立表 `esim_packages`（不复用 `Plan`）；订单复用 `Order`（`Meta` 放 Plan 形状快照）；每张卡一行 `esim_profiles`；NextPay 订阅 UUID 挂在 profile 上 |
| 退款 | NextPay 没有 App 侧退款 API；退款沿用本站 `ProcessOrderRefund` 退到用户钱包 |

---

## 1. 背景与目标

### 1.1 需求

1. 网站上可购买 eSIM 并管理订阅，一个用户可持有多个国家的多张卡。
2. 购买后在 app 内方便地把 eSIM 装到手机上：原生层做成 SDK，webapp 直接调用。
3. app 只管用，网站只管卖；功能以「扩展」形态从发现页进入，不是核心 VPN 流程的一部分。
4. **号码比流量重要**：用户要的是能收验证码的海外号码，成本要尽量低；流量多少是次要属性。
5. 支付用 qtoolkit 的 NextPay，以获得更精细的订阅支付方式（自动续费、手动续费提醒、暂停/取消）。
6. 实名可以接受，但必须是外国人凭护照能办的国家。
7. 开途用户的国行设备限制不作为设计约束（用户会用 SIM 转 eSIM 卡等方式解决），但安装页要把二维码/手动码作为一等路径。

### 1.2 非目标

- app 内不做任何购买、续费、订阅管理（只放「去网站」按钮）。
- 不做服务端读取短信（市场上没有供应商提供）。
- v1 不做护照采集与实名审核。
- v1 不做多供应商比价。
- 不改动 VPN 购买流程、WordGate / Stripe / IAP 轨道。

---

## 2. 调研结论摘要

### 2.1 流量供应商（用户指定的五家）

| 供应商 | 文档 | 开户 | 下单 | 交付 | 充值/退款 | Webhook | 中国大陆套餐 | 结论 |
|---|---|---|---|---|---|---|---|---|
| eSIM Access | 公开完整 | 自助、无 MOQ、预付余额、无沙箱 | 异步，`transactionId` 幂等，3–30 秒出档 | `ac` 完整 LPA 串 + QR 图 | 按 ICCID 充值 ≤9 次；未安装可取消退回余额 | 6 类事件，无签名，IP 白名单 | 有（联通 5G，香港出口） | **流量线首选** |
| eSIM Go | 公开完整 | 自助，首充 $1000 | 同步出档，bundle 生效 ≤10 分钟 | SM-DP+ 与 matchingId 分开，另给 Apple/Android 安装链接 | 叠 bundle；退款默认关闭 | HMAC 签名 | 未公开 | 第二适配器 / 英国号码 |
| MegaeSIM | 登录墙后 | 自助，$100，人工审核 | 同步 | QR + 激活码 + ICCID | 有 | 有签名 | 有 | 比价备货源 |
| TelecomsXChange | 仅博客与 MCP README，路径矛盾 | $599.99/年 | 同步 | `lpa` + `qr_base64` | 未公开 | 未公开 | 未公开 | 以后 |
| GloeSIM | 无 | API 需审批 | 未知 | 未知 | 不可退款 | 未知 | 不提 | 放弃 |

### 2.2 号码线

- 五家中只有 eSIM Go 有真号，且**仅英国**：号码只在语音/短信 bundle 有效期内存在，下游零售底价 £3.50/月，批发未公开，起充 $1000/月。eSIM Access、MegaeSIM 文档明写仅流量；TCXC 的 `sms`/`voice` 字段例子全为 0。
- 市场上「真 MSISDN 在 eSIM 上 + API」形态的供应商：**esim.tech**（US/UK/NL/DE/JP/KR/AU 等 25+ 国，一次调用返回 LPA 与号码，无最低无合同，价格页 404 需询价）、**Telnyx**（自助；SIM $1 + $2/月激活；SIM 附着手机号在新文档有、定价页无，需实测）、Airalo Partner（Discover+ 含美国 +1 号，零售 $9 起）、1GLOBAL（唯一香港号路径，企业报价）、WorldSIM（+44 真号白标，约 £15/年，无 API）。
- App 托管虚拟号（Yesim、Holafly、Roamless）常被 Google/Telegram 拒，排除。IoT SIM（Twilio Super SIM 等）无公网号，排除。

### 2.3 国家与监管

| 国家 | 护照可办 | 年保号（消费级基准） | 大陆漫游收短信 | API 渠道 | v1/v2 |
|---|---|---|---|---|---|
| 美国 +1 | 无需实名 | $36（Ultra PayGo） | 是 | Telnyx / esim.tech / Airalo | v1 |
| 英国 +44 | 无需实名 | £0.6（giffgaff）～ £42 | 是 | eSIM Go / esim.tech / eSIM.net / WorldSIM | v1 |
| 荷兰 +31 | 无需实名 | 约 $22（Lebara 每 90 天充 €5） | 需特定套餐 | esim.tech | v1 备选 |
| 日本 +81 | **是（已验证，Mobal）** | 约 $115 | 是，收短信免费 | esim.tech | v2 |
| 泰国 / 马来西亚 | 是（未验证） | $10–30（未验证），马来每人 5 张上限 | 待验 | 未找到 | v2 |
| 香港 +852 | 是，但 2025 起收紧（每人每商 3 张、CMHK 禁大陆激活） | HK$118 | 是 | 1GLOBAL | 观望 |
| 韩国 / 越南 / 菲律宾 / 印尼 / 台湾 / 新加坡 | 出境封号 / 绑签证 / 要本地地址 / 收短信收费 / 双证件 / 非居民 30 天 | | | | 排除 |

### 2.4 平台能力与商店政策

- iOS：`CTCellularPlanProvisioning` 需 Apple 只发给运营商的权限，不可用。iOS 17.4+ 从原生 `UIApplication.open` 打开 `https://esimsetup.apple.com/esim_qrcode_provisioning?carddata=LPA:1$…`（`$` 不编码）直接进系统安装向导；不能在 WKWebView 内打开；首次点击偶发 DNS 失败需重试。17.4 以下只能二维码（需另一设备）或手动输入。
- Android：非运营商 app 调 `EuiccManager.downloadSubscription` 会返回 `RESOLVABLE_ERROR`，`startResolutionActivity` 弹系统授权即可安装（API 28+，PendingIntent 需 `FLAG_MUTABLE`）。兜底 `https://esimsetup.android.com/esim_qrcode_provisioning?carddata=…`（GMS ≥ 25.14.34），再兜底剪贴板 + `ACTION_MANAGE_EMBEDDED_SUBSCRIPTIONS`。
- 中国设备：国行 iPhone 仅 17e / Air 有 eSIM，且 Apple 明文「非大陆运营商 eSIM 在大陆境内无法安装」；国行安卓少数机型且无 GMS。安装页需有「出境后安装」提示。
- Apple 3.1.3(e)：eSIM 属 app 外消费的服务，**必须**外部支付，IAP 会被拒（2025-12 先例）。本设计 app 内根本没有结账，天然合规；app 内「去网站购买」按钮只打开系统浏览器。Google Play 未明文，Airalo/Holafly 无 IAP 标签。
- 隐私：ICCID/EID/激活码属新数据采集，需更新 `PrivacyInfo.xcprivacy` 与 ASC 隐私标签。

### 2.5 NextPay 能力（`qtoolkit/nextpay` v1.5.35 + `wordgate/nextpay` 服务）

- 托管结账：`CreateOrder`（一次性）/ `CreateSubscription`（订阅，`Code` 指 NextPay plan，`Period` monthly|quarterly|yearly|lifetime，可传首单折扣 `Amount`、`ObjectID`、success/cancel URL）都返回 `PaymentURL`，我们只需重定向。支付方式：信用卡（自动续费）、支付宝、微信（手动续费）。**币种仅 USD**。
- 订阅是 NextPay 自管的循环账单（不是 Stripe Subscription）：卡订阅到期前 24 小时内 off-session 扣款；无卡订阅由 NextPay 在 14/7/3/1 天前发提醒邮件，邮件里的「Renew now」调 `RenewSubscription` 拿 `PaymentURL`；**到期即 `expired`，无宽限**。状态 `active` / `cancelled`（用户关自动续费 = 期末取消）/ `expired`（续费失败）/ `paused`。`SetAutoRenew` / `CancelSubscription` / `Pause` / `Resume` / `RenewSubscription` 均有 SDK。
- Plan：`CreatePlan` 按 `(app, code)` 幂等 upsert（`Amount` cents、`IntervalType` month|quarter|year|lifetime、`TrialDays`）；`UpdatePlan` 改价/停用。
- Webhook：`X-NextPay-Signature: t=,v1=` HMAC-SHA256；**至少一次投递**（重试 15 次 + 扫描器），必须按 `evt.ID`（`evt_<uuid>`）去重；事件 `order.paid/expired/failed`、`subscription.created/renewed/cancelled/expired/paused/resumed`（`past_due`/`trialing` 保留不产生）。`WebhookOrderData` 带 `ObjectID`（我们传的订单 UUID）；`WebhookSubscriptionData` 带 `SubscriptionID`、`ObjectID`、`CurrentPeriodEnd`、`AutoRenew`。
- 限制：SDK 是**每进程单 App**（`nextpay.access_key` 单例，`Client` 方法未导出）；**无 App 侧退款 API**；无 `order.refunded` 事件。
- 配置：`nextpay.access_key`、`nextpay.endpoint`（默认 `https://pay.arbella.group`）、`nextpay.timeout`；webhook secret 在 NextPay 后台按 App 配。

---

## 3. 产品定义

### 3.1 两条产品线

| 属性 | 号码 eSIM（`line=number`） | 流量 eSIM（`line=data`） |
|---|---|---|
| 主卖点 | 一个可收验证码的海外号码 | 目的地上网 |
| 目录组织 | 按号码国家 | 按目的地/区域 |
| 关键属性 | `number_country`、`sms_in`、`voice_in`、`voice_out`、含流量、账期、是否需护照 | 覆盖、流量、天数、激活方式、是否可充值 |
| 计费 | **NextPay 订阅**：月付 / 季付 / 年付（`billing=subscription`） | **NextPay 一次性订单**（`billing=one_time`） |
| 续费 | NextPay 驱动：卡自动扣款；支付宝/微信收 NextPay 提醒邮件手动续费；`subscription.renewed` → 我们调供应商 `Renew` | 「加流量」= 新的一次性订单，履约调供应商 `TopUp` |
| 停用 | 网站「关闭自动续费」= NextPay 期末取消；到期 NextPay 发 `subscription.expired` → 号码按供应商规则失效 | 到期/用尽自然结束 |
| v1 国家 | 美国、英国（荷兰备选） | eSIM Access 全目录，admin 挑选上架 |

### 3.2 订阅与号码生命周期的对应

- 一张号码 eSIM ↔ 一个 NextPay 订阅（`esim_profiles.nextpay_subscription_uuid`）。订阅 UUID 终生不变，续费只推进账期。
- `number_valid_until` = NextPay `currentPeriodEnd` 与供应商侧号码有效期两者取小；UI 以此显示「保号到期」。
- 号码线的提醒邮件由 NextPay 发（14/7/3/1 天），我们**不再**自建号码到期提醒；流量线的「流量不足 / 到期」提醒仍由我们做（v1.1）。
- NextPay 到期即 `expired` 无宽限，所以供应商侧续期必须在 `subscription.renewed` 到达后立刻执行；供应商续期失败 → Slack + admin 队列，并在 profile 上标 `renew_failed`，不回滚 NextPay 扣款（人工处理）。

### 3.3 定价规则

- 售价存美元 cents，与现有 `Plan.Price` 一致，NextPay 也只收 USD；展示币种沿用各品牌现状。
- `sell_cents = roundTo99(wholesale_cents × esim.markup_multiplier + esim.fixed_fee_cents)`，默认系数 1.5、固定费 0；单条 `price_override_cents` 优先。
- 号码线：供应商月租折算成每账期批发成本后套规则；月付 / 季付 / 年付各一个 NextPay plan（code = `{slug}-{month|quarter|year}`），年付给折扣系数（默认 0.85）。
- 主推 SKU：美国号年付，目标零售 $39–49（等供应商报价后定）。

### 3.4 范围分期

- **v1**：流量线全目录 + 号码线（US/UK）；网站目录、结账、我的 eSIM、订阅管理（自动续费开关、手动续费、取消）、加流量、未安装前取消退款、交付邮件；app 我的 eSIM + 原生安装 SDK；admin 套餐/档案/供应商页。
- **v1.1**：流量线用量不足与到期提醒（webhook → 邮件 + push）。
- **v2**：护照实名模块 + 日本/泰国/马来西亚/香港号码线；第二流量供应商；Overleap 接入（需 NextPay SDK 多客户端）；「来华 eSIM」专题页。

---

## 4. 架构与数据流

```
供应商 A (eSIM Access, 流量)  ─┐
供应商 B (esim.tech | Telnyx, 号码) ─┴── api/esimprovider（Provider 接口 + 工厂 + 每家一个适配器）
        ↑ 供应商 webhook（IP 白名单 / HMAC）     ↑
NextPay ── /webhook/nextpay（HMAC，evt.ID 去重）── Center API ──┴── asynq: esim:catalog_sync / nextpay_plan_sync / provision / provision_timeout / sync_profile / usage_sync / topup / renew
        ↑ paymentUrl 重定向                     │
   网站 /esim 商店 + /account/esim ─────────────┤        app /esim（我的 eSIM · 原生安装）── K2Plugin esimInstall
   admin /manager/esim/* ───────────────────────┘        发现页卡片 → 系统浏览器（OTT 免登录）→ 网站商店
```

### 4.1 首购生命周期

1. 网站选套餐 → `POST /api/user/esim/orders {slug, period?}`。
2. 后端建 `Order`（`Meta.plan` 快照 `Product="esim"`，`Meta.esim` 放 package、line、kind、period），按 `billing` 调 `nextpay.CreateSubscription`（`Code`=NextPay plan code，`ObjectID`=订单 UUID，`UserID`=用户 UUID，`Email`）或 `nextpay.CreateOrder`（`ObjectID`=订单 UUID，`Amount`=sell_cents），把 `PaymentURL` 写进 `Meta.payUrl`，返回。
3. 网站 `window.location.assign(paymentUrl)`，用户在 NextPay 托管页付款，回跳 `successUrl`（`/account/esim?checkout=success&order=<uuid>`）。
4. NextPay webhook `order.paid`（`ObjectID`=订单 UUID）→ 去重 → `SELECT … FOR UPDATE` → 品牌哨兵 → `MarkOrderAsPaid` → `applyOrderToBuyer` 的 `esim` 分支建 `esim_profiles`（`pending`），ID 收进 post-commit 队列；**不在支付事务内入队**。
5. 事务提交后入队 `esim:provision {profileID}`。
6. worker 调 `Provider.Order`（`ClientRef`=订单 UUID，幂等）→ `provisioning` → 轮询 `QueryProfile` 至拿到 LPA（同任务内 ≤60 秒，超出留给 `sync_profile` 与供应商 webhook）→ 加密落库 → `ready` → 入队交付邮件。
7. `subscription.created`（号码线）→ 按 `ObjectID` 找 profile，写 `nextpay_subscription_uuid`、`current_period_end`、`auto_renew`。事件顺序不保证，两个处理器都要能先到。
8. app「我的 eSIM」轮询到 `ready`，展示安装页；网站账户页同步显示 QR 与手动码。

### 4.2 续费、加流量、停用

- `subscription.renewed` → 建一条已支付的续费 `Order`（`Channel=nextpay`，金额取事件 plan amount，`Meta.esim.kind=renew`）用于对账 → `esim_topups(kind=renew)` → 入队 `esim:renew` → `Provider.Renew` → 成功推进 `number_valid_until`。
- 加流量：网站「加流量」→ `POST /api/user/esim/orders {slug, kind:"topup", profileUUID}` → NextPay 一次性订单 → `order.paid` → `esim_topups(kind=data)` → `esim:topup` → `Provider.TopUp`。
- `subscription.cancelled`（期末取消）→ profile `auto_renew=false`，UI 显示「将于 X 到期」。`subscription.expired` → profile `status=expired`（号码按供应商规则失效）。`paused/resumed` 同步标记。
- 网站订阅管理：`POST /api/user/esim/profiles/:uuid/subscription/{auto-renew|cancel|renew}` 分别代理 `SetAutoRenew` / `CancelSubscription` / `RenewSubscription`（后者返回 `PaymentURL` 供重定向）。

### 4.3 状态机（`esim_profiles.status`）

```
pending → provisioning → ready → installed → active → depleted
                │            │                    └→ expired
                │            └→ cancelled（仅 ready 且供应商允许取消）
                └→ failed（超时扫描 30 分钟 / 最后一次重试失败；Slack；admin 重试或退款）
```

- `ready/installed/active` 由供应商状态映射：eSIM Access `smdpStatus=RELEASED` → ready，`ENABLED/DOWNLOAD/INSTALLATION` → installed，`esimStatus=IN_USE` → active，`USED_UP` → depleted，`expiredTime < now` → expired。号码适配器按各自语义映射，映射表写在适配器内并有单测。
- 状态推进只发生在 worker（`sync_profile` / `usage_sync` / `renew`）里；供应商 webhook 只触发一次 `sync_profile`，**不直接信 payload**；NextPay webhook 只改订阅字段与触发履约。
- 迟到的异步响应写库前必须重读当前状态（`cancelled`/`failed`/`expired` 不可被 `ready` 覆盖）。

---

## 5. 后端设计（`api/`）

### 5.1 数据模型（GORM AutoMigrate，`api/migrate.go` 注册）

**`esim_packages`**（品牌中立目录，每个供应商包一行）

| 列 | 类型 | 说明 |
|---|---|---|
| id / created_at / updated_at / deleted_at | | |
| provider | varchar(30) idx | `esimaccess` / `esimtech` / `telnyx` |
| provider_package_code | varchar(100) | 供应商包 ID；`UNIQUE(provider, provider_package_code)` |
| slug | varchar(120) uniqueIndex | 我们的稳定 ID，`{provider}:{provider_slug}` |
| line | varchar(10) idx | `number` / `data` |
| billing | varchar(15) | `one_time` / `subscription` |
| name / description | varchar(255) / text | |
| number_country | char(2) idx, nullable | 号码线必填 |
| coverage | JSON | `[{code,name}]` |
| operators | JSON | `[{country, name, networkType}]` |
| data_bytes | bigint | 0 = 无流量 |
| duration_days | int | 流量有效期（天） |
| data_type | varchar(10) | `fixed` / `daily` |
| active_type | tinyint | 1 安装时 / 2 首次联网 / 3 购买即激活（不上架） |
| sms_in / sms_out / voice_in / voice_out | bool | |
| supports_topup | bool | |
| topup_group | varchar(60) | 同组可互相充值 |
| kyc_required | bool | v1 恒 false |
| kyc_type | varchar(20) | `none` / `passport` |
| wholesale_cents | uint64 | USD；号码线为每月批发成本 |
| retail_hint_cents | uint64 | 供应商建议零售价 |
| sell_cents | uint64 | 规则算出；订阅线为月价 |
| price_override_cents | *uint64 | admin 覆盖 |
| nextpay_plan_codes | JSON | 订阅线：`{month, quarter, year}` → NextPay plan code；`nextpay_plan_sync` 维护 |
| nextpay_synced_at | datetime | |
| is_active / is_listed / featured / sort_weight | | 供应商侧可售 / admin 上架 / 置顶 / 排序 |
| last_synced_at | datetime | |

**`esim_profiles`**（一张卡一行）

| 列 | 类型 | 说明 |
|---|---|---|
| id / created_at / updated_at / deleted_at | | |
| uuid | varchar(30) uniqueIndex | `esp-<xid>`，对外寻址 |
| user_id | idx | |
| brand | varchar(20) idx | |
| order_id | uniqueIndex | 首购订单；履约幂等 |
| package_id | idx | |
| line / provider / billing | | 快照 |
| provider_ref | varchar(100) idx | eSIM Access `esimTranNo`（ICCID 会复用，不能当主键） |
| provider_order_ref | varchar(100) | |
| iccid | varchar(32) idx | |
| eid | varchar(40) | 安装后回填 |
| msisdn | varchar(20) idx | 号码线 |
| number_country | char(2) | |
| activation_code_enc | text | LPA 串，`secretEncryptString` |
| smdp_address / matching_id | varchar(255) | 从 LPA 拆出 |
| status | varchar(20) idx | 见 4.3 |
| provider_status | varchar(60) | 原始状态串 |
| nextpay_subscription_uuid | varchar(64) idx | 号码线 |
| subscription_status | varchar(20) | NextPay 状态镜像 |
| auto_renew | bool | |
| current_period_end | datetime | NextPay 账期末 |
| renew_failed_at | datetime | 供应商续期失败标记 |
| label | varchar(60) | 用户备注名 |
| coverage_snapshot | JSON | |
| data_total_bytes / data_used_bytes | bigint | |
| activated_at / data_expires_at | datetime | 流量有效期 |
| number_valid_until | datetime idx | 保号到期 = min(账期末, 供应商有效期) |
| usage_synced_at / provider_synced_at | datetime | |
| provision_attempts / last_provision_error | | |

**`esim_topups`**：id, profile_id idx, order_id uniqueIndex, package_id, kind（`data` / `renew`）, status（`pending` / `provisioning` / `done` / `failed`）, provider_ref, added_bytes, added_days, error。

**`nextpay_webhook_events`**：event_id uniqueIndex, type, object_id, subscription_uuid, payload JSON, received_at, processed_at。镜像 `stripe_webhook_events`。

**`esim_provider_events`**（供应商 webhook 审计）：id, provider, event_type, payload JSON, remote_ip, received_at, processed_at, profile_id nullable。保留 90 天。

不给 `Order` 加列；admin 的 eSIM 订单视图通过 `esim_profiles.order_id` / `esim_topups.order_id` 关联。

### 5.2 `api/esimprovider/` 包

```go
type Provider interface {
    Name() string
    Ready() bool                                       // 凭证缺失 → false，渠道静默不可用
    ListPackages(ctx) ([]Package, error)               // 全量目录（含 line、号码属性、批发价）
    ListTopups(ctx, ref ProfileRef) ([]Package, error)  // 可对该档案充值的包
    Order(ctx, req OrderReq) (OrderRef, error)          // 幂等于 req.ClientRef；供应商不支持幂等时适配器先查后建
    QueryProfile(ctx, ref ProfileRef) (Profile, error)  // LPA、ICCID、msisdn、状态、用量、有效期
    QueryOrder(ctx, ref OrderRef) ([]Profile, error)
    Usage(ctx, refs []ProfileRef) ([]Usage, error)      // 批量
    TopUp(ctx, req TopUpReq) (TopUpResult, error)       // 幂等于 ClientRef
    Renew(ctx, req RenewReq) (RenewResult, error)       // 号码保号续期；流量适配器返回 ErrUnsupported
    Cancel(ctx, ref ProfileRef) (CancelResult, error)   // 未安装退款；不支持返回 ErrUnsupported
    Balance(ctx) (int64, error)                         // cents
    VerifyWebhook(r *http.Request) (Event, error)       // IP 白名单 / HMAC；返回归一化事件
}
func NewProvider(cfg ProviderConfig) (Provider, error)  // switch 工厂，镜像 cloudprovider/factory.go
```

- 适配器：`esimaccess/`（v1）、`esimtech/` 或 `telnyx/`（pilot 后定其一）。每个适配器有自己的状态映射表 + 单测（httptest 回放真实响应 fixture）。
- eSIM Access 细节：`RT-AccessCode` + 可选 HMAC（`RT-Timestamp`/`RT-RequestID`/`RT-Signature`）；8 rps 令牌桶；价格 ÷10000 → cents；`activeType=3` 的包不上架；用 `slug` 做稳定 ID；webhook 白名单 IP 见附录 C。
- 配置（`api/config.yml` 模板，真值在部署配置）：

```yaml
esim:
  markup_multiplier: 1.5
  fixed_fee_cents: 0
  yearly_discount: 0.85
  catalog_sync_interval: 6h
  low_balance_alert_cents: 20000
  providers:
    - name: esimaccess
      access_code: ""
      secret_key: ""
      base_url: https://api.esimaccess.com/api/v1/open
      webhook_ips: [3.1.131.226, 54.254.74.88, 18.136.190.97, 18.136.60.197, 18.136.19.137]
    - name: esimtech
      api_key: ""
      base_url: ""
nextpay:
  access_key: ""
  webhook_secret: ""
  # endpoint 默认 https://pay.arbella.group
```

- `viper.UnmarshalKey("esim.providers", &cfgs)`，镜像 `cloud_instance.accounts[]`。凭证缺失 ⇒ `Ready()=false`，目录同步跳过该家，下单返回 eSIM 专用错误码。

### 5.3 NextPay 接线

- 新支付渠道常量 `PayChannelNextPay = "nextpay"`（`brand.go`），加入开途的 `PaymentChannels`；这是 **brand registry 变更 → 必须重生成 `contracts/api-contract.json`**（`-count=1`）。Overleap 在 SDK 支持多客户端前不加入。
- `logic_nextpay.go`：`configNextPay(ctx)` 读 `nextpay.access_key` / `webhook_secret`，缺失 ⇒ 渠道不可用（`Ready()` 语义）；测试缝 `var nextpayCreateOrder = nextpay.CreateOrder` 等包级变量，镜像 `stripeNewCheckoutSession`。
- Plan 同步 `esim:nextpay_plan_sync`：对 `billing=subscription && is_listed` 的包，按 `{month, quarter, year}` 各 `CreatePlan` upsert（code `{slug}-{interval}`，`Amount` = 月价 × 月数 × 折扣，`IntervalType`），写回 `nextpay_plan_codes`；下架 → `UpdatePlan{IsActive:false}`；改价 → `UpdatePlan{Amount}`。目录同步后自动触发。
- Webhook `POST /webhook/nextpay`：读原始 body → `nextpay.ParseWebhook(body, header, secret)`（签名错 → 400，NextPay 停止重试）→ `nextpay_webhook_events` 按 `evt.ID` 去重（已处理 → 200）→ 分派：
  - `order.paid` → 按 `ObjectID` 取订单 → `withDeadlockRetry` + `FOR UPDATE` → 品牌哨兵（`alertPaymentBrandMismatch`）→ `MarkOrderAsPaid` → post-commit 入队。
  - `subscription.created` → 按 `ObjectID` 找 profile，写订阅字段（profile 可能尚未由 `order.paid` 建出：先落 `nextpay_webhook_events` 并标 `pending_link`，`order.paid` 处理完后回补）。
  - `subscription.renewed` → 建续费 `Order`（已支付）+ `esim_topups(kind=renew)` → 入队 `esim:renew`。
  - `subscription.cancelled/expired/paused/resumed` → 更新 profile 订阅字段与 `status`。
  - `order.expired/failed` → 未支付订单标记，走现有弃单提醒。
  - 处理失败返回 5xx 让 NextPay 重试；handler 内**不调供应商、不发邮件**。
- 续费失败对账：`subscription.renewed` 已到但 `Provider.Renew` 最终失败 → `renew_failed_at` + Slack + admin 队列；不主动退款（NextPay 无退款 API），人工处理。

### 5.4 订单接线

- 新 handler `api_create_esim_order`（`POST /api/user/esim/orders`），**不改** `api_create_order`：
  1. `AuthRequired()` + `EnforceDeviceClass()`；品牌支付渠道门 `AllowsPayment(PayChannelNextPay)`。
  2. 读 `esim_packages`：`is_active && is_listed`；`kind` ∈ `new` / `topup`；`topup` 校验 `profile.user_id` 归属、状态允许、包属于 `topup_group`；号码线 `new` 必带 `period`。
  3. `Order.Title`「eSIM · 美国号码 · 年付」；`Channel="nextpay"`；`Meta.plan` = `Plan{PID:"esim:"+slug, Label, Price, Product:"esim", Brand}`；`Meta.esim = {packageId, slug, line, billing, kind, period, profileUUID?}`。`SetOrderMeta`/`GetPlan` 改用同一个具名 `orderMeta` 结构体，加 `Esim *OrderEsimMeta` 字段。
  4. 调 NextPay 拿 `PaymentURL`，写 `Meta.payUrl`；`preview=true` 短路。
- `applyOrderToBuyer`（`logic_member.go`）：`plan.Product == ProductEsim` → 按 `Meta.esim.kind` 建 `esim_profiles` 或 `esim_topups`，ID 收进 `provisionIDs`，**不加 VPN 天数、不触发邀请奖励**（显式豁免，见开放问题），分销返现照旧。
- 退款：`ProcessOrderRefund` 加 `esim` 分支：先 `Provider.Cancel`，成功才记退款并退用户钱包（现有）。号码线在 `ready` 之后不可退（号码已分配）；`failed` 自动退。NextPay 侧无退款 API，订阅由用户在网站取消。

### 5.5 Worker 与供应商 webhook（`api/worker_esim.go`）

| 任务 | 触发 | 行为 |
|---|---|---|
| `esim:catalog_sync` | cron 每 6h，`Unique(7h)` | 每家 `ListPackages` → upsert；供应商侧消失的置 `is_active=false`；按规则重算 `sell_cents`；批发价变动 >20% 或新包 → Slack；结束后入队 `nextpay_plan_sync` |
| `esim:nextpay_plan_sync` | catalog_sync 后 / admin 改价上下架后 | 见 5.3 |
| `esim:provision` | `order.paid` post-commit | 孤儿 → `SkipRetry`；`Order` 幂等（`ClientRef`=订单 UUID）；同任务内轮询 ≤60s；成功 → `ready` + 入队 `esim:deliver_email`；`isLastAttempt` → `failed` + Slack |
| `esim:provision_timeout` | cron 每 10min | `provisioning` 超 30min → 先 `QueryOrder` 一次，否则 `failed` + Slack |
| `esim:sync_profile` | 供应商 webhook / 用户 refresh | `QueryProfile` → 映射状态 → 重读当前状态后写库 |
| `esim:usage_sync` | cron 每 1h | 取 `status ∈ (ready, installed, active)` 且未过期、按 `usage_synced_at` 升序分批；推进 `depleted/expired` |
| `esim:topup` | `order.paid`（kind=topup）post-commit | `TopUp`；成功累加 `data_total_bytes`/`data_expires_at` |
| `esim:renew` | `subscription.renewed` | `Renew`；成功推进 `number_valid_until`；失败 → `renew_failed_at` + Slack |
| `esim:deliver_email` | provision 成功 | `templateSlugExists("esim-delivered")` 守卫；`EnqueueTemplatedEmailTask` |
| `esim:data_reminder` | cron 每日（v1.1） | 流量线 `data_expires_at` 3d/0d、用量 ≥90% → 邮件 + `push:send`；每窗口幂等 |
| `esim:balance_check` | cron 每 1h | 供应商 `Balance()` 低于阈值 → Slack |
| `esim:events_cleanup` | cron 每日 | 清理 90 天前的 `esim_provider_events` / `nextpay_webhook_events` |

- 供应商 webhook 路由：`POST /webhook/esim/:provider` → `Provider.VerifyWebhook` → 写 `esim_provider_events` → 找 profile → 入队 `esim:sync_profile`。鉴权失败 401；其余 200。**从不同步调供应商、从不直发邮件。**

### 5.6 用户 API

| 方法 | 路径 | 说明 |
|---|---|---|
| GET | `/api/esim/countries?line=` | 公开；聚合：国家 → 最低价、包数 |
| GET | `/api/esim/packages?line=&country=` | 公开；`is_active && is_listed`；订阅线返回月/季/年三档价 |
| GET | `/api/esim/qr/:uuid.png?exp=&sig=` | 公开；HMAC 签名 URL（`exp` ≤ 30 天）；服务端生成 PNG（`github.com/skip2/go-qrcode`，MIT） |
| POST | `/api/user/esim/orders` | `{slug, kind, period?, profileUUID?, preview?}` → `{order, payUrl}` |
| GET | `/api/user/esim/profiles` | 列表；**不含激活码**；含 `msisdn`、状态、用量、有效期、`numberValidUntil`、`autoRenew`、`subscriptionStatus` |
| GET | `/api/user/esim/profiles/:uuid` | 详情；含 `lpa`、`smdpAddress`、`matchingId`、`appleInstallUrl`、`androidInstallUrl`、签名 QR URL；限频 30/min |
| POST | `/api/user/esim/profiles/:uuid/refresh` | 入队 `sync_profile`；限频 1/min/profile |
| POST | `/api/user/esim/profiles/:uuid/cancel` | 仅 `ready` 且供应商支持；调 `ProcessOrderRefund`；号码线同时 `CancelSubscription` |
| POST | `/api/user/esim/profiles/:uuid/subscription/auto-renew` | `{enabled}` → `SetAutoRenew` |
| POST | `/api/user/esim/profiles/:uuid/subscription/cancel` | → `CancelSubscription`（期末取消） |
| POST | `/api/user/esim/profiles/:uuid/subscription/renew` | → `RenewSubscription` → `{payUrl}`（仅无卡订阅） |
| POST | `/api/user/esim/profiles/:uuid/resend-email` | 限频 3/h |
| PATCH | `/api/user/esim/profiles/:uuid` | `{label}` |
| GET | `/api/user/esim/profiles/:uuid/topups` | 可用加流量包（`ListTopups` 与本地目录取交集，缓存 10min） |

DTO 用 camelCase；供应商域名不外泄（QR 用我们的端点）。app 只用 `profiles` 列表/详情/refresh/resend/label；订阅与购买端点只有网站调。

### 5.7 Admin API（`/app/esim/*`，`RoleRequired`）

- 套餐：`GET /app/esim/packages`（筛 provider/line/country/listed）、`PUT /app/esim/packages/:id`（`is_listed`/`featured`/`sort_weight`/`price_override_cents`，触发 `nextpay_plan_sync`）、`POST /app/esim/packages/sync`。
- 档案：`GET /app/esim/profiles`（筛 user/iccid/msisdn/status/provider/subscription_status）、`GET /app/esim/profiles/:uuid`（含解密 LPA，**写审计日志**；含 NextPay 订阅状态）、`POST …/reprovision`、`POST …/renew`（人工重跑供应商续期）、`POST …/cancel`、`POST …/sync`。
- 供应商：`GET /app/esim/providers`（每家 `Ready`、余额、最近同步、最近 webhook）；NextPay 连通性与最近事件。
- 改价/上架不走 maker-checker（v1），记入开放问题。

### 5.8 邮件与通知

- 模板 `email_templates_esim.go`：`esim-delivered`（号码大字 + 账户页链接 + 签名 QR 图 + 手动码 + 「在 app 中一键安装」说明）、`esim-renew-failed`（供应商续期失败，用户版 + 内部版）、`esim-provision-failed`。号码到期提醒由 NextPay 发，不重复。Overleap 英文变体放 `email_templates_overleap.go` 惯例位置（v2）。
- 全部走 `EnqueueTemplatedEmailTask`，`templateSlugExists` 守卫，缺模板只 warn。
- Slack：`sendCloudSlackNotification` 用于 provision 失败、续期失败、余额低、目录价格跳变、NextPay 签名失败突增。

### 5.9 错误码、契约、迁移

- 新错误码在 `api/response.go` 以显式 `ErrorCode` 类型声明：`ErrorEsimPackageUnavailable`、`ErrorEsimProviderUnavailable`、`ErrorEsimProfileNotCancellable`、`ErrorEsimTopupNotAllowed`、`ErrorEsimSubscriptionInvalidOp`、`ErrorEsimKycRequired`（v2 预留）；镜像到 `webapp/src/utils/errorCode.ts` 与 `web/src/lib/api-errors.ts`。
- 品牌注册表新增支付渠道 + 新错误码 → `cd api && UPDATE_CONTRACT=1 go test -count=1 -run TestExportContract ./...` 重生成并提交契约。
- 迁移：AutoMigrate 加五表；测试库需手动 `migrate` 一次。
- `go.mod` 加 `github.com/wordgate/qtoolkit/nextpay v1.5.35` 与 `github.com/skip2/go-qrcode`。
- `api/CLAUDE.md` 增补「eSIM / NextPay」段。

---

## 6. 网站设计（`web/`）

- 品牌门：`web/src/lib/brands.ts` 加 `features.esim`（kaitu `true`，overleap `false`）；页面 `notFound()`、导航、`sitemap.ts` 三处同时门控。
- 页面：
  - `/[locale]/esim`：两个分区「海外号码」「旅行流量」，国家网格 + 搜索；ISR `revalidate=3600`。
  - `/[locale]/esim/[line]/[country]`：套餐列表 + 详情抽屉（号码国家、收短信、接电话、含流量、账期与月/季/年价、需护照）。「购买」需登录；`api.createEsimOrder` → `window.location.assign(payUrl)`。回跳 `/account/esim?checkout=success&order=` 显示「出档中」并轮询。
  - `/[locale]/account/esim` 与 `/[locale]/account/esim/[uuid]`：我的 eSIM 列表与详情（号码、状态、用量、有效期、自动续费开关、手动续费按钮、取消订阅、加流量、二维码、手动码、「用 app 一键安装」提示）。
  - `/discovery`：顶部加 eSIM 卡片，链接 `/esim`（嵌入模式下由现有拦截器扔到系统浏览器，app 侧给自有域名补 OTT）。
- **不改** `embed-interceptor.js`，**不加** postMessage 发送端。
- `api.ts` 新增：`getEsimCountries`、`getEsimPackages`、`createEsimOrder`、`getEsimProfiles`、`getEsimProfile`、`esimSubscriptionAutoRenew/Cancel/Renew`、`esimCancel`、`esimTopups`、`esimResendEmail`、`updateEsimLabel`；admin 对应方法。
- Admin：`/manager/esim/packages`、`/manager/esim/profiles`、`/manager/esim/providers`；`manager-sidebar.tsx` 在「用户与订单」加「eSIM 档案」，「运营配置」加「eSIM 套餐 / 供应商」。
- i18n：新 namespace `esim` × 7 locale，`web/messages/namespaces.ts` 手动注册；中文面**禁用 "Kaitu" 裸词**。

---

## 7. Webapp 设计（只管用）

### 7.1 品牌门与路由

- `brands/types.ts` `BrandFeatures` 加 `esim: boolean`（kaitu `true`，overleap `false`）；`config/apps.ts` 镜像；测试按 `brandConfig.*` 断言。
- `App.tsx` 路由**无条件**注册：`/esim`、`/esim/:uuid`（均 `LoginRequiredGuard`）；入口（账户页条目）按 `features.esim` 门控。
- `useAppLinks` 加 `esimStoreUrl`（`appLinks.esimPath || '/esim'`）与 `esimAccountUrl`（`/account/esim`）；后端 `AppLinks` 结构体加两个字段。

### 7.2 页面

- `pages/esim/EsimList.tsx`（`/esim`）：我的 eSIM 列表（号码、国家、状态徽标、保号到期、流量条）；顶部「去购买」按钮 → `openWebsite(esimStoreUrl)`；空态引导购买。轮询按 `location.pathname.startsWith('/esim')` 门控。
- `pages/esim/EsimDetail.tsx`（`/esim/:uuid`）：号码大字 + 复制；状态卡；用量条（标「更新于」）；保号到期与自动续费状态（只读，「管理订阅」→ `openWebsite(esimAccountUrl/<uuid>)`）；主按钮「一键安装」按 `esimInstallMode()` 分派；次级面板：二维码（`qrcode` 已有，`DeviceInstall.tsx` 模式）、手动码（SM-DP+ / 激活码各带复制）、发到邮箱；重命名。文案含「短信会出现在手机自带短信 app 里」「出境后安装」「iOS 首次打开失败请重试」。
- `pages/Account.tsx` 加「我的 eSIM」条目（`features.esim`）。
- `Discover.tsx` 的 `external-link` 处理：目标 host 属品牌根域且已登录时，先 `POST /api/user/ott {redirect}` 换免登录 URL 再 `openExternal`（`AnnouncementBanner.tsx` 现有模式），失败则原 URL 打开。抽成 `services/open-website.ts` 的 `openWebsite(url)`，账户页与 eSIM 页共用。

### 7.3 `services/capabilities.ts`（新建，仓库规则要求的单一供给点）

```ts
export type EsimInstallMode = 'native' | 'ios-link' | 'android-link' | 'qr-only';
export function esimInstallMode(): EsimInstallMode;   // 桥有 esim.install → native；iOS ≥17.4 无桥 → ios-link；Android 无桥 → android-link；其余 qr-only
export function isDesktopPlatform(): boolean;         // 收编 DeviceInstall / AndroidInstall 里重复的 isDesktop 字面量
export function hasAndroidAppList(): boolean;         // 收编 capacitor-k2.ts 的 getPlatform()==='android' 门
```

- 能力判断只看**存在性**（`typeof fn === 'function'`），不比版本号；iOS 版本从 UA 解析仅用于无桥老版本兜底。
- CI grep 守卫（`scripts/check-capabilities-single-supplier.sh`）：本次新增或修改的 `pages/`、`components/` 文件不得直接比较 `window._platform?.os` / `platformType`，一律经 `capabilities.ts`；存量 20 处不在本次范围。

### 7.4 Store / Service

- `services/esim-service.ts`：纯 async 函数，返回解包数据并给稳定兜底，绝不向上泄漏 `SResponse`。
- `stores/esim.store.ts`：`profiles`、`phase`；迟到响应写入前重读 store。
- 安装动作：`native` → `window._platform.esim.install({activationCode})`（iOS 内部打开通用链接、Android 原生安装）；`ios-link`/`android-link` → `openExternal(url)`；返回值驱动提示，最终以服务端状态为准。

### 7.5 i18n

- 新 namespace `esim` × 7 locale；`node scripts/i18n/split-namespaces.js webapp` 重生成 `namespaces.ts`。用户可见文案不出现 "VPN"。

---

## 8. 原生安装 SDK（K2Plugin，v1 必做）

- **只加方法，不建新插件**。`definitions.ts` 加：

```ts
esimSupported(): Promise<{ supported: boolean; mode: 'native' | 'link' | 'none'; reason?: string }>;
esimInstall(options: { activationCode: string }): Promise<{ result: 'installed' | 'launched' | 'cancelled' | 'failed'; message?: string }>;
```

- Android（`K2Plugin.kt`）：`Build.VERSION.SDK_INT >= 28` + `hasSystemFeature(FEATURE_TELEPHONY_EUICC)` + `EuiccManager.isEnabled` → `downloadSubscription(DownloadableSubscription.forActivationCode(code), switchAfterDownload=true, PendingIntent(FLAG_MUTABLE))` → BroadcastReceiver 收 `RESOLVABLE_ERROR` → `startResolutionActivity` → 第二个 PendingIntent 拿最终结果（`installed`/`cancelled`/`failed`）。eUICC 不可用时返回 `mode:'link'`，由 webapp 走 `esimsetup.android.com`。复用 `startActivityForResult` + `@ActivityCallback` 模式。不需要新 manifest 权限。
- iOS（`K2Plugin.swift`）：`esimSupported` 返回 `{supported: iOS >= 17.4, mode: 'link'}`；`esimInstall` 用 `UIApplication.shared.open` 打开通用链接并返回 `launched`。`pluginMethods` 表与 `@objc func` 两处都加。
- `web.ts` 加 `unavailable` 桩；`npm run build` 并提交 `dist/`。
- 桥：`IPlatform.esim?: IEsim`，`capacitor-k2.ts` 按平台构造（镜像 `iap:` / `appList:`），桌面/web 为 `undefined`。
- `BRIDGE_API_VERSION` 3 → 4（`bridge-version.ts`、`K2Helpers.swift`、`K2PluginUtils.kt`），`UPDATE_BRIDGE_CONTRACT=1` 重生成 `contracts/bridge-api.json`；**不动** `webapp-support-floor.json`。
- `PrivacyInfo.xcprivacy` 与 ASC 隐私标签补 ICCID/EID/激活码；审核备注模板补 eSIM 用途说明。
- 老版本 app（无桥）通过 `capabilities.ts` 的 `ios-link` / `android-link` 兜底，webapp 可先于原生发版通过 web OTA 上线。

---

## 9. 安全与合规

- 激活码加密存储；列表接口不返回；详情接口限频；admin 解密读取写审计。
- NextPay webhook 先验签再解析、按 `evt.ID` 去重、5xx 触发重试、400 终止重试；供应商 webhook 只信白名单 IP / HMAC，且只做「触发同步」。
- OTT 免登录链接只对品牌根域生效（`isAllowedRedirect`），300 秒单次。
- 用户条款加「禁止批量注册、禁止作为短信网关转售」（供应商 AUP 明令禁止 SIM gateway）。
- Apple：app 内无结账，「去购买」仅打开系统浏览器；不引用 VPN 购买页。
- KYC：v1 不采集任何证件；v2 设计单独 spec。
- 品牌纯度：品牌字面量只进 `brands/` 注册表；`check-brand-purity.sh` 照跑。

---

## 10. 测试与验证

- Go：适配器用 httptest 回放真实响应 fixture；NextPay 用包级函数变量注入假客户端，webhook 测试覆盖签名错/重复事件/乱序（`subscription.created` 先于 `order.paid`）；worker 测试覆盖孤儿 SkipRetry、幂等重放、超时扫描、迟到响应不覆盖 cancelled、续期失败标记；`applyOrderToBuyer` 与 `ProcessOrderRefund` 的 esim 分支做**变异验证**；handler 测试单跑与全量各跑一次。
- webapp：`capabilities.ts` 各分支；store 轮询门控；detail 页四条安装路径按 mode 渲染；`openWebsite` 的 OTT 成功/失败两路；品牌门测试按 `brandConfig.*`；`K2_BRAND=overleap npx vitest run` 也绿。
- web：品牌守卫与 SSR 泄漏测试覆盖新页；订阅管理三个动作的错误码映射；真实浏览器过一遍 i18n 嵌套。
- 原生：Kotlin 状态映射单测；真机 smoke 至少 Pixel、Samsung、iPhone（iOS 17.4+）各一台，另加一张 SIM 转 eSIM 卡扫码验证。
- NextPay 端到端：测试 App + Stripe 测试卡 `4242…` 跑首购、自动续费（把账期缩到最短）、手动续费、取消、过期五条链路，确认每个 webhook 都落库且幂等。
- 上线前：用**已知会失败**的对照（`activeType=3` 包、错误签名的 NextPay webhook、错误 IP 的供应商 webhook）确认门真的关。

---

## 11. 分期与依赖

| 阶段 | 内容 | 依赖 |
|---|---|---|
| P0 pilot | 附录 A 三卡测试 + esim.tech 询价；NextPay 后台建开途 App（access key、webhook secret、success/cancel URL） | 用户开账号/付款 |
| P1 后端 | 5.1–5.9 全部；号码适配器按 pilot 结果实现其一 | P0 决定号码供应商 |
| P2 网站 | 商店、结账、账户页与订阅管理、发现页卡片、admin 三页、i18n | P1 |
| P3 webapp | 品牌门、两页、`openWebsite`、`capabilities.ts`、store/service、i18n | P1；可 web OTA 发布 |
| P4 原生 SDK | K2Plugin 两个方法、桥版本 4、契约、隐私标签 | 与 P3 并行；随下一次原生发版；v1 完整体验需要它 |
| v1.1 | 流量线提醒 + push | P1 |
| v2 | KYC 模块 + 日本/泰马/香港线、第二流量供应商、NextPay SDK 多客户端 + Overleap 接入、来华 eSIM 专题 | 单独 spec |

---

## 12. 开放问题

1. 号码供应商：esim.tech 报价与 KYC 流程；Telnyx SIM 附着号码是否真能收第三方短信。**P0 解决。**
2. NextPay SDK 每进程单 App：Overleap 接入需要 qtoolkit 导出多客户端 API（`NewClient(cfg)` + 导出方法），或后端按品牌起两个进程。v2 前解决。
3. 邀请奖励是否对 eSIM 订单生效：默认**不生效**，需业务确认。
4. 分品牌加价系数：默认全局一个。
5. 号码线退款政策：`ready` 后不可退；供应商若支持未安装取消则放开。
6. admin 改价是否走 maker-checker：v1 不走。
7. `capabilities.ts` 存量 20 处平台字面量的迁移节奏。
8. 二维码 PNG 依赖（`skip2/go-qrcode` MIT）需过依赖审查。
9. NextPay 到期无宽限：供应商续期慢于账期推进时的用户体验（号码短暂不可用）是否可接受；可选方案是我们侧给 24 小时容忍窗。

---

## 附录 A：供应商 pilot 协议

**目标**：在写号码适配器之前，用真卡回答三件事：号码段是否被主流服务判为手机号；人在大陆漫游时能否收码；闲置后号码是否保留。

**样本**（总成本约 $50）：

| 编号 | 卡 | 来源 | 目的 |
|---|---|---|---|
| T1 | Telnyx eSIM + enable_voice 号码 | portal.telnyx.com 自助，SIM $1 + $2/月 | 候选供应商 |
| T2 | esim.tech US 或 UK 号码 eSIM | 询价后拿测试凭证 | 候选供应商 |
| C1 | Ultra Mobile PayGo eSIM（$3/月） | ultramobile.com | 对照组：已知 OTP 友好的 T-Mobile 号段 |
| C2 | giffgaff eSIM | giffgaff.com | 对照组：英国 |

**测试矩阵**（每张卡各跑一遍，记录「通过/失败/未送达/被判非手机号」）：

| 服务 | 海外位置 | 大陆漫游 |
|---|---|---|
| Google 注册 / 二次验证 | | |
| Telegram 注册 | | |
| WhatsApp 注册 | | |
| 微信（海外手机号注册） | | |
| 支付宝国际版 / X / ChatGPT / Binance（任选 2） | | |
| 任一银行或 Stripe 的验证短信 | | |

**保号**：闲置 30 天后复测一次收码；记录供应商后台的状态与扣费。

**安装**：每张卡分别在 iPhone（iOS 17.4+）用通用链接、在 Pixel 用系统安装、在桌面用二维码各装一次，记录失败点。

**判定**：候选供应商（T1/T2）在「Google + Telegram + WhatsApp」三项海外位置全部通过、大陆漫游至少两项通过，才进入适配器开发；否则换下一家。

## 附录 B：esim.tech 询价邮件（英文，直接可发）

```
Subject: Reseller API inquiry — local eSIMs with phone numbers (US/UK/NL, later JP/TH)

Hi esim.tech team,

We run a consumer app with an existing paying user base and are adding an eSIM store. Our primary product is an overseas phone number for receiving SMS verification codes (OTP); data volume is secondary. We would integrate via your REST API and white-label the eSIM.

Could you share:
1. Your current country list for "local eSIM with a real phone number", and for each: inbound SMS, inbound/outbound voice, and whether the number is a native MNO mobile range.
2. Wholesale pricing per number: one-time provisioning fee, monthly or annual keep-alive, included data, and volume tiers. We expect to start at a few hundred numbers and grow.
3. Keep-alive rules: what happens when a package lapses, how long the number is retained, and whether it can be renewed via API.
4. Roaming behaviour: does the number receive inbound SMS while the device is attached to networks in mainland China, and at what cost?
5. KYC: for US/UK/NL, is any end-user identity collection required? For JP/TH, what documents do you accept (passport only?) and is the KYC step exposed through the API?
6. Sandbox or test credentials, idempotency support on order creation, and webhook events (delivery, SMS, expiry).
7. Whether inbound SMS can be read via API, or only on the device.

Company: [公司名]   Website: [网址]   Contact: [邮箱]
Expected volume: [数字] numbers in the first 6 months.

Thanks,
[署名]
```

## 附录 C：调研摘要（压缩版）

### C.1 eSIM Access API 要点

- Base `https://api.esimaccess.com/api/v1/open`，全 POST；`RT-AccessCode`；可选 HMAC `RT-Timestamp`/`RT-RequestID`/`RT-Signature = hmac_sha256(ts + reqId + accessCode + body)`；8 rps。
- `/package/list {locationCode, type BASE|TOPUP, iccid, dataType}` → `slug`（稳定）、`packageCode`、`price`（USD×10000）、`volume`（bytes）、`duration`、`activeType`（1 安装时 / 2 首次联网 / 3 购买即激活不可退）、`retailPrice`、`supportTopUpType`、`fupPolicy`、`locationNetworkList`。
- `/esim/order {transactionId ≤50, packageInfoList[{slug, count}]}` → `{orderNo}`，异步 ≤30s；`/esim/query {orderNo|iccid|esimTranNo}` → `esimTranNo`（主键）、`iccid`、`ac`（LPA）、`qrCodeUrl`、`smdpStatus`、`esimStatus`、`expiredTime`、`totalVolume`、`orderUsage`、`eid`。
- `/esim/usage/query {esimTranNoList ≤10}`（2–3h 延迟）；`/esim/topup`（≤9 次，过期后不可）；`/esim/cancel`（仅 GOT_RESOURCE + RELEASED，退余额）；`/balance/query`。
- Webhook：`ORDER_STATUS` / `ESIM_STATUS` / `SMDP_EVENT` / `DATA_USAGE` / `VALIDITY_USAGE` / `CHECK_HEALTH`；无签名；白名单 IP `3.1.131.226, 54.254.74.88, 18.136.190.97, 18.136.60.197, 18.136.19.137`。
- 无沙箱；参考价日本 1GB/7d $0.91、欧洲 5GB/30d $15.60、美国 1GB/7d $1.04、中国大陆 1GB/7d $0.91。

### C.2 eSIM Go 要点（第二适配器 / 英国号）

- `X-API-Key`，`https://api.esim-go.com/v2.5/`，10 rps；模型 = eSIM 档案（`iccid`, `matchingId`, `smdpAddress`）+ 可排队 bundle；`POST /orders {type:transaction, assign:true, order:[{type:bundle, quantity, item, iccids?}]}` 同步返回档案，bundle 生效 ≤10min；`GET /esims/{iccid}` 含 `appleInstallUrl`/`androidInstallUrl`/`msisdn`；`GET /esims/{iccid}/bundles` 用量；`DELETE …/bundles/{name}` 未开始可退（默认关闭）；Webhook V3 HMAC-SHA256；`MSISDN Enabled/Disabled` 事件；首充 $1000；无沙箱。

### C.3 号码线供应商要点

- esim.tech：`POST /v1/esims/local` 同步返回 LPA + 号码；25+ 国；无最低无合同；价格页 404。
- Telnyx：eSIM $0.70 一次性；$2/月激活、$0.20/月待机；中国本地数据 $0.0325/MB；`POST /sim_cards/{id}/actions/enable_voice` 给 +E.164 号；旧 FAQ 仍写 data-only，需实测。
- Airalo Partner：Discover+ 含美国 +1 号，收短信免费，覆盖含中国，零售 $9 起；号码续存未知。
- WorldSIM：+44 真号，US +1 加 £10/年，每年一次充值保号，Reseller/White-label 有、API 无。
- 1GLOBAL：10 国含香港，企业报价。

### C.4 中国设备与政策

- 国行 iPhone 仅 17e / Air 有 eSIM；Apple 123879：「非大陆运营商 eSIM 在大陆境内无法安装」。
- 国行安卓：2025-10 起华为 Mate 80 RS / Pura 90 Pro Max、三星 S26、OPPO Find X9 卫星版等少数机型；小米/vivo 全无；无 GMS。
- Apple 3.1.3(e) 拒审先例（2025-12）：eSIM 服务用 IAP 被拒，改外部支付后通过。Airalo / Nomad / Holafly 商店页均无 IAP 标签。

### C.5 未验证项清单（写适配器前需确认）

- `supportsEmbeddedSIM` 无权限时是否可靠；通用链接在 SFSafariViewController 内的行为；`%24` 编码是否被接受。
- Pixel 的 LPA 是否接受非运营商 `ACTION_START_EUICC_ACTIVATION`；`ACTION_VIEW LPA:` 是否有系统处理器。
- Telnyx SIM 附着号码的短信落地；esim.tech 全部商务条款；Airalo Discover+ 号码续存。
- 泰国 / 马来西亚运营商保号规则与漫游收码；日本 Mobal 漫游激活费与是否须在日本首次激活。
