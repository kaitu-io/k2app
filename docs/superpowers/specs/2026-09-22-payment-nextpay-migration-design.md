# 开途支付迁移：WordGate 托管收银台 → NextPay 直连 Stripe

日期：2026-09-22 · 状态：Phase A 待实施 · 范围：`api/` + `web/` + `qtoolkit/nextpay`

## 1. 背景与结论

**症状**：用户在 `arbella.group/pay`（WordGate 的 Hugo 托管收银台）看到 `Load failed` +【返回订单列表】。

**已确认根因**（见工单排查记录，不再复述证据链）：

- 收银台页面对 `api.wordgate.52j.me` 做一次裸跨域 `fetch`，无重试、无备用入口，异常直接把浏览器原生错误串渲染出来。
- 报障用户 15 笔订单里 14 笔的收银台请求**根本没到东京**；WordGate 本身一个月零 5xx。
- 14 天内 WordGate 64 单 / 15 付 = **23%**，且用户普遍多次重试。

**决策**（用户已拍板）：

1. 全面迁移到 NextPay，**不要任何中间收银台页**——Center 直接返回 Stripe 的 `checkout.stripe.com` 链接。
2. 定价保持 **USD**（NextPay 只支持 usd；结算能力另议）。
3. 已装客户端的 OTA 现状不可依赖 → 迁移必须做到**零 OTA 依赖**（Phase A），渠道自选等增量放 Phase B 独立分支。

## 2. 为什么 Phase A 可以零 OTA

三条独立证据：

1. **客户端不解析 `payUrl`，只 `openExternal` 打开它**（`webapp/src/pages/Purchase.tsx:722-725`）。服务端把它从 `arbella.group/pay?order_no=…` 换成 `checkout.stripe.com/c/pay/…`，所有已装版本行为不变。
2. **官网不走 OTA**（Amplify 部署 `web/`）。iOS app 内购买走 Apple IAP（`Purchase.tsx:720` 注释），近 60 天 24 个只有 iOS 设备的下单用户都是在官网 Safari 里买的——报障用户就是其中之一。
3. **收不到 OTA 的 ~29% 老版本（<0.4.8）恰好只需要"打开一个链接"**，服务端改完即受益。

## 3. 范围

### Phase A（本 spec，主线）

| 层 | 改动 |
|---|---|
| `qtoolkit/nextpay` | 新增 `ConfirmPayment`（封装 `POST /api/checkout/:orderUuid/confirm`）；发 `nextpay/v1.5.37` |
| `api/` | 下单改走 NextPay（create + confirm `more`），返回 Stripe URL；新增 `/webhook/nextpay`；新增公开 302 端点 `GET /api/orders/:uuid/pay`（代付邮件用的**耐久链接**）；`Order` 加 `NextpayOrderID` 列 + `Channel=nextpay`；品牌渠道表加 `nextpay` |
| `web/` | 新增开途独有页 `/{locale}/pay-result/[uuid]`（Stripe 成功回跳落点，**零关键路径 fetch**） |
| 运维 | NextPay 建 kaitu 租户；Center 配置 `nextpay.*`；用 `more` 实测 Stripe 收银台是否列出支付宝/微信 |

### Phase B（独立分支 `feat/nextpay-channel-picker`，OTA 恢复后再合）

- app 内购买页自选渠道 → `POST /api/user/orders` 带 `paymentMethod` → confirm 指定渠道 → 少一次点击。
- 收不到 OTA 的老版本自动保持 Phase A 体验（合理降级）。
- 清理：kaitu 渠道表移除 `wordgate`、删 WordGate 下单代码与 SDK 依赖（保留 webhook handler 至过渡期结束）。

### 不在范围

- 入口池冗余（多 CloudFront 分发）、CloudFront 日志、`/health` + ALB matcher、`resolve-and-fetch` 失败上报——另一份 spec。
- Stripe CNY 结算能力核实——独立待办。
- web OTA 指针 `{brand}/web/latest.json` 缺失——独立事实，与本次无关。

## 4. 架构

```
app / 官网 ──POST /api/user/orders──▶ Center
                                       │ 1. nextpay.CreateOrder   (http://127.0.0.1:5900, 同机)
                                       │ 2. nextpay.ConfirmPayment(orderId, "more")
                                       ▼
                                  { payUrl: https://checkout.stripe.com/c/pay/cs_… }
app: openExternal(payUrl) / 官网: location.href = payUrl
                                       │
                          用户在 Stripe 收银台付款（支付宝 / 微信 / 卡，由 Stripe 列出）
                                       │
        ┌──────────────────────────────┴───────────────────────────────┐
        ▼                                                              ▼
Stripe → NextPay webhook → NextPay outbox → POST /webhook/nextpay      浏览器 302 → www.kaitu.io/{locale}/pay-result/{uuid}
        (HMAC, 15 次重试 + cron 补投)      ↓                           （静态成功页，不发请求）
                                   MarkOrderAsPaid（既有）
```

**链路对比**：旧 = app → Center → WordGate → 托管页 → 裸 fetch WordGate → Stripe；新 = app → Center →（同机）NextPay → Stripe。用户浏览器**只接触 `checkout.stripe.com` 与 `www.kaitu.io`**，不再碰任何 52j.me 域。

**代付邮件的耐久链接**：Stripe Checkout Session 24h 过期、NextPay 订单 30 分钟后拒绝 confirm。直接把 Stripe URL 写进邮件，代付人隔天打开就是死链。所以 `Order.Meta.payUrl`（代付邮件唯一读取者）存的是 `{BaseURL}/api/orders/{uuid}/pay`：Center 收到 GET 时，若已付 → 302 到 pay-result；若缓存的 Stripe URL 未满 23h → 302 复用；否则新建 NextPay 订单 + confirm → 存回 Meta → 302。**app / 官网的即时流程不经过这个端点**（直接拿 Stripe URL），它只服务邮件链接。

## 5. 组件设计

### 5.1 `qtoolkit/nextpay`：`ConfirmPayment`

```go
// ConfirmPaymentRequest picks the rail for a pending one-time order.
// PaymentMethod: card | alipay | wechat_pay | crypto | more（more = Stripe 按账号
// 设置自动列出支付方式）。
type ConfirmPaymentRequest struct { PaymentMethod string `json:"paymentMethod"` }
type ConfirmPaymentResult  struct { CheckoutURL string `json:"checkoutUrl"`; RedirectURL string `json:"redirectUrl,omitempty"` }

func ConfirmPayment(ctx context.Context, orderUUID string, req *ConfirmPaymentRequest) (*ConfirmPaymentResult, error)
// → POST /api/checkout/{orderUUID}/confirm，走既有 doRequest / decodeData
```

测试沿用 `nextpay_test.go` 的 `mock()` 模式：断言路径、方法、body 的 `paymentMethod`，解析 `checkoutUrl`。README 加一段。提交后打 tag `nextpay/v1.5.37` 并 push（qtoolkit 是公开仓，Center 直接 `go get`）。

### 5.2 Center

**配置**（`logic_config.go`）：

```yaml
nextpay:
  endpoint: "http://127.0.0.1:5900"   # 同机直连；备选 https://pay.arbella.group
  access_key: "<kaitu 租户 access key>"
  webhook_secret: "<kaitu 租户 webhook secret>"
  payment_method: "more"               # confirm 时传给 NextPay 的渠道；smoke 不通过时改 alipay 即可，无需重部署 NextPay
  timeout: 15
```

`nextpay.endpoint/access_key/timeout` 由 qtoolkit 自己从 viper 读；Center 新增 `configNextpay(ctx)` 读 `webhook_secret` 与 `payment_method`（缺省 `more`），并提供 `Ready()`（`access_key` 与 `webhook_secret` 缺一即渠道不可用：下单返 405001，webhook 返 503，绝不 panic——对齐 `StripeConfig.Ready()`）。

**品牌渠道**（`brand.go`）：新增 `PayChannelNextpay = "nextpay"`；kaitu `PaymentChannels: [nextpay, wordgate, apple_iap]`。**过渡期保留 `wordgate`**：部署瞬间仍有未付的 WordGate 订单，其 webhook 的品牌哨兵会检查 `AllowsPayment(wordgate)`，此时拿掉会把在途付款拒掉。Phase B 清理时移除。重生成 `contracts/api-contract.json`，并同步 `webapp` / `web` 的跨层契约测试。

**模型**（`model.go`）：

```go
NextpayOrderID string `gorm:"type:varchar(36);index"` // 最近一次 NextPay 订单 uuid（耐久链接重建后会更新）
```
`Channel` 常量增加 `OrderChannelNextpay = "nextpay"`，下单时写入；修正 `OrderChannelWordgate` 注释里"网页订单 Channel 恒空"的口径为"**历史** WordGate 订单为空，2026-09-22 起网页/app 订单为 nextpay"。AutoMigrate 自动加列（additive）。

**Meta**：`payUrl` 语义改为**耐久链接**；新增 `checkoutUrl` + `checkoutAt`（unix）缓存最近一次 Stripe URL。`SetOrderPayUrl` 泛化为 `SetOrderCheckout(payUrl, checkoutUrl string, at int64)`；`GetPayUrl` 不变（代付邮件读它）。

**下单**（`api_order.go`，替换第 274–352 行的 WordGate 事务）：

```go
checkout, err := createNextpayCheckoutFn(c, user, order, plan)   // var 形态，测试可替换
// createNextpayCheckout:
//   email := getUserEmail(user.ID)  — 空则返错（NextPay 要求合法 email；kaitu 全员邮箱注册）
//   res  := nextpay.CreateOrder(ctx, &nextpay.OrderRequest{
//             UserID: user.UUID, Email: email,
//             ProductName: order.Title, ProductDescription: plan.Label,
//             Amount: order.PayAmount, Currency: "usd",
//             ObjectID: order.UUID,
//             SuccessURL: payResultURL(user, order.UUID),   // {BaseURL}/{locale}/pay-result/{uuid}（无 query——NextPay 会拼 ?session_id=）
//             CancelURL:  {BaseURL}/{locale}/purchase,
//             Metadata:   {"brand":"kaitu","plan":plan.PID} })
//   conf := nextpay.ConfirmPayment(ctx, res.OrderID, &{PaymentMethod: cfg.PaymentMethod})   // 缺省 "more"
//   → order.NextpayOrderID = res.OrderID; order.Channel = nextpay
//   → order.SetOrderCheckout(BaseURL+"/api/orders/"+uuid+"/pay", conf.CheckoutURL, now); tx.Save
// 响应 payUrl = conf.CheckoutURL（直达 Stripe）
```
品牌门从 `AllowsPayment(PayChannelWordgate)` 改为 `AllowsPayment(PayChannelNextpay)`。locale 由 `user.Language` 映射：`zh-TW`/`zh-HK` 原样，其余 `zh-CN`（开途站只有这三个）。

**耐久 302**（新文件 `api_order_pay_redirect.go`，路由 `api.GET("/orders/:uuid/pay")`，无认证，uuid 即能力凭证——与 WordGate 公开 `order_no` 同等暴露面）：

```
order := by uuid（找不到 → 302 {BaseURL}/{locale}/purchase）
已付      → 302 pay-result
checkoutAt 距今 < 23h → 302 缓存 checkoutUrl
否则      → createNextpayCheckoutFn 重建（新 NextPay 订单 + confirm）→ 存 Meta + NextpayOrderID → 302
```
不加限流：命中需要知道 uuid，且重建至多每 23h 一次。

**Webhook**（新文件 `api_nextpay_webhook.go`，路由 `r.POST("/webhook/nextpay", …BrandResolver(), api_nextpay_webhook)`，HTTP 状态语义同 wordgate handler）：

```
body := io.ReadAll；evt := nextpay.ParseWebhook(body, X-NextPay-Signature, cfg.WebhookSecret)
  签名失败 → 400；配置未 Ready → 503
switch evt.Type:
  order.paid:
    d := evt.OrderData()
    withDeadlockRetry(3):
      order := FOR UPDATE + Preload(User) WHERE uuid = d.ObjectID      // ObjectID = Center 订单 uuid
      找不到 → 记 error，返回 nil（ack，避免 15 次重试打空）
      品牌哨兵：!AllowsPayment(nextpay) → alertPaymentBrandMismatch + error（fail-loud，同 wordgate）
      金额哨兵：d.Amount != order.PayAmount || d.Currency != "usd" → Slack [PAYMENT-AMOUNT-MISMATCH] + error
      已付：
        d.OrderID == order.NextpayOrderID → debug，nil（重投）
        否则 → Slack [DOUBLE-PAY] order=… nextpay=…（同一 Center 订单两个 session 都被付了，需人工退一笔），nil
      未付 → MarkOrderAsPaid(c, tx, &order, &provisionSubIDs)；成功后 order.NextpayOrderID = d.OrderID
    事务后：enqueueProvision + onPrivateNodeOrderOnboarding（照抄 wordgate handler）
  order.expired / order.failed: 只记 info，nil
  其余: warn，nil
```

**双付窗口说明**：耐久链接重建时旧 Stripe session 可能仍有最多 1h 可付。概率极低（要求买家和代付人在同一小时内各付一次），用 `[DOUBLE-PAY]` 告警兜底、人工退款，不做自动退款（与 IAP `[DOUBLE-REFUND]` 同哲学）。

**WordGate 代码**：Phase A **只切断下单路径**，`/webhook/wordgate`、`configWordgate`、SDK 依赖全部保留（在途订单需要）。Phase B 清理。

### 5.3 `web/`：`/{locale}/pay-result/[uuid]`

- 文件：`src/app/[locale]/pay-result/[uuid]/page.kaitu.tsx`（Server Component，`generateMetadata` 带 `robots: noindex`）+ 一个小 client 组件。**开途独有**（`.kaitu.tsx`）；`tests/brand-page-tree.test.ts` 的 `KAITU_ONLY_DIRS` 加 `pay-result`；`web/CLAUDE.md` 保留路径清单加 `pay-result`。
- 内容：✅「支付已提交」/ 「权益通常在几秒内到账，可能因支付方式不同延迟几分钟」/ 订单号（来自路径参数）/ 两个按钮：【查看账号】→ `/account`，【打开开途】→ `/install`。**不发任何请求**——代付人未登录也能正常看到；买家点【查看账号】由 `/account` 现有逻辑展示到期时间。
- i18n：新 kaitu-only namespace `payResult`（`messages/zh-CN|zh-TW|zh-HK/payResult.json`），注册进 `namespaces.ts` 的 `namespaces` 与 `BRAND_NAMESPACES.kaitu`。
- `PurchaseClient.tsx`：`handleOrder` 不变（`location.href = payUrl`）；`PayResultDialog` 在跳走前一闪即逝，Phase A 不动它（Phase B 顺手删）。

### 5.4 NextPay 侧

**不改代码**。运维建租户：

```
POST {nextpay}/admin/apps   (admin 魔法链接登录，admin.allowed_emails)
{ "name": "kaitu",
  "webhookUrl": "http://127.0.0.1:5800/webhook/nextpay",   // 同机直投；备选 https://k2.52j.me/webhook/nextpay
  "successUrl": "https://www.kaitu.io/zh-CN/account",
  "cancelUrl":  "https://www.kaitu.io/zh-CN/purchase" }
→ 响应里的 accessKey；webhook secret 经 GET /api/apps/webhook-secret（X-Access-Key）取得
```

## 6. 错误处理与不变量

| 场景 | 处理 |
|---|---|
| NextPay create/confirm 失败 | 订单已落库但无 payUrl → 返 `ErrorSystemError`（与 WordGate 失败同形）；订单可由耐久链接/重试再建 checkout |
| 用户无邮箱 | 返 `ErrorSystemError` + error 日志（kaitu 全员邮箱注册，出现即数据异常） |
| webhook 签名错 | 400，不重试 |
| webhook 找不到订单 | ack + error 日志（永久异常，避免重试风暴） |
| 品牌/金额错配 | Slack 告警 + 5xx（fail-loud，持续告警是设计取舍） |
| 同一订单第二次 `order.paid`（不同 NextPay 订单） | Slack `[DOUBLE-PAY]`，ack |
| `nextpay.*` 缺配置 | 下单 405001、webhook 503、302 端点 302 回 purchase |

不变量：`MarkOrderAsPaid` 仍是唯一入账点；`IsPaid` + `FOR UPDATE` 保证幂等；`ObjectID == order.UUID` 是 webhook ↔ 订单的唯一关联键（`NextpayOrderID` 只做对账/双付判定）。

## 7. 测试

- **qtoolkit**：`TestConfirmPayment_Success` / `_APIError`（mock 服务器）。
- **Center 单测**（mock DB / 手工路由，对标 `api_stripe_webhook_test.go`）：
  - webhook：合法签名 `order.paid` → 调用 `MarkOrderAsPaid`；坏签名 → 400；金额错配 → 5xx + 告警 seam 被调用；已付重投 → 200 且不再入账；已付但 NextPay 订单不同 → `[DOUBLE-PAY]` seam。
  - 下单：`createNextpayCheckoutFn` seam 返回固定 URL → 响应 `payUrl` 等于它、`Channel=nextpay`、Meta 含耐久链接；seam 返错 → `ErrorSystemError`。
  - 302 端点：未付且缓存新鲜 → Location = 缓存；已付 → pay-result；过期 → seam 被调用一次。
  - 品牌：`brand_test.go` / `brand_payment_test.go` 更新为 kaitu 允许 nextpay（且过渡期仍允许 wordgate）。
  - 契约：`UPDATE_CONTRACT=1 go test -count=1 -run TestExportContract`，webapp/web 契约测试同步绿。
- **web**：`brand-page-tree` / `messages-parity` / `site-config-keys` 全绿；`yarn build` 与 `yarn build:overleap` 都过（Overleap 构建里 `pay-result` 不存在）。
- **线上 smoke（部署清单第 5 步）**：用测试账号下一单，打开返回的 Stripe URL，**肉眼确认列出了支付宝与微信**；用最小金额付一笔，确认 webhook 入账 + pay-result 落地。

## 8. 部署清单（按序）

1. qtoolkit：合并 + tag `nextpay/v1.5.37` + push。
2. NextPay：建 kaitu 租户（§5.4），拿 access key / webhook secret。
3. Center 配置：两台 `/data/app-configs/kaitu/config.yml` 加 `nextpay:` 段（center-deploy skill）。
4. `make deploy-api`（AutoMigrate 加 `nextpay_order_id` 列）。
5. **Smoke `more`**：若 Stripe 收银台没列出支付宝/微信 → 回退方案：配置 `nextpay.payment_method: alipay`（只改 Center 配置，不重新部署 NextPay），同时在 Stripe Dashboard 打开自动支付方式后再切回 `more`。
6. 验证 `curl -I https://www.kaitu.io/api/orders/<uuid>/pay` 返回 302（Amplify rewrite 透传上游 3xx）；若不透传，`Meta.payUrl` 改用 `https://k2.52j.me/api/orders/…`（改 `payRedirectBase` 一处）。
7. `git push origin main:website` 发布 pay-result 页。
8. 观察 7 天：Slack 告警频道零 `[PAYMENT-*]` / `[DOUBLE-PAY]`；`orders.channel='nextpay'` 的付费率对比 23% 基线。
9. 7 天后 Phase B 清理分支合入：kaitu 渠道表移除 `wordgate`。

## 9. 置信度声明

| 断言 | 置信 | 依据 |
|---|---|---|
| 客户端只打开 payUrl 不解析 | 10/10 | `Purchase.tsx:722-725`、`PurchaseClient.tsx:304` |
| NextPay confirm 返回 Stripe 原生 URL | 10/10 | `logic_checkout.go:487-560`、`:1414-1470` |
| NextPay 在两台 center 本机 `:5900` | 10/10 | `ss -tlnp` 两台均命中 |
| NextPay 的 Stripe 账号支持 USD 下 alipay/wechat_pay/card | 9/10 | 60 天内三种 session 都创建成功（expired 记录证明 Stripe 接受了该 type），1 笔支付宝实付 |
| `more` 会列出支付宝/微信 | **未验证** | 线上从未用过 `more`；取决于 Stripe Dashboard 设置 → 部署清单第 5 步实测，有回退 |
| Amplify rewrite 透传 302 | 8/10 | Next.js 外部 rewrite 是反代不跟随跳转；部署清单第 6 步实测，有回退 |
