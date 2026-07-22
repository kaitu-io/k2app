# Overleap Stripe 支付系统性收口 — 设计 spec

日期：2026-07-22 · 基线：`overleap` 分支 @ bcbde599 · 分支：`feat/overleap-stripe-web`

## 背景与目标

品牌拆分后：kaitu 走 WordGate（不变），overleap 走 Stripe（订阅制）+ Apple IAP。Phase A 已交付 api 全套 Stripe 后端与 webapp（app 内）购买/管理面板；**缺口集中在 web 站（overleap.io）**：

1. `overleap.io/purchase`（`web/src/app/[locale]/purchase/PurchaseClient.tsx`，661 行）是纯 WordGate `createOrder` 流，无品牌分支——overleap 用户在网站上无法购买（后端返 405001）。
2. `overleap.io/account`（`web/src/app/[locale]/account/page.tsx`）无条件 redirect 到 `/purchase`。
3. 而 api 的 Stripe 跳转缺省 URL 恰指向这两页：success → `{overleap.BaseURL}/account?checkout=success`、cancel → `/purchase?checkout=cancelled`、portal return → `/account`（`api/logic_config.go configStripe`）。即 app 内购买能成，但回跳落地体验是坏的；网站本身不能买。

**目标**：overleap.io 完整网页购买流（欧美 VPN 主流转化路径：官网 → 网页付费 → 下载 app）+ 订阅管理闭环 + Stripe 侧资源建立 + ops 上线清单。kaitu 一切不变。

## 全局约束（每个任务隐含遵守）

- **Stripe key 永不入 git、永不出现在报告/commit message/测试代码里**。本地配置在 `center/config.yml`（gitignored；`center → api` 符号链接，实体是 `api/config.yml`，同样 gitignored）。
- **kaitu 零行为变化**：现有 `PurchaseClient.tsx`、`/account` 的 kaitu redirect、WordGate 流一行不改。
- **品牌纯度**：新组件/文案零 "kaitu" 字样（大小写均含）；overleap 页面无中文 locale（en-US/en-GB/en-AU/ja）。
- **错误码宪法**：客户端按 code 映射文案，绝不展示后端原始 message（web 既有 `api-errors.ts` 模式）。
- **测试判据**：api 真库 `go test ./...` 以 `-v` 下 0 SKIP 为准；web/webapp 用各自 vitest 套件。

## 既有资产（不重做，本 spec 只消费）

- `POST /api/user/stripe/checkout`（`api/api_stripe.go`）：品牌门（405001 ErrorPaymentChannelUnavailable）→ 品牌隔离 plan 解析（`getPlanByPID` + `StripePriceID` 非空）→ tier 校验 → 防双扣（`GetActiveSubscriptions` 非空即拒）→ Checkout Session（subscription metadata：user_uuid/plan_pid/brand；复用既有 Stripe Customer）。返回 `{url}`。
- `POST /api/user/stripe/portal`：Billing Portal 一次性 URL。
- `POST /webhook/stripe`：`invoice.paid` 自足入账（金额级幂等 UNIQUE(provider, transaction_id)）、`customer.subscription.updated/deleted`、`charge.refunded`、`charge.dispute.created`；`checkout.session.completed` 仅记录。
- 订阅读模型：`DataUser.subscriptions []DataSubscription{provider, tier, currentPeriodEnd, autoRenew, manage{kind,...}}`，随用户信息接口下发；`manage.kind == "stripe_portal"` 表示管理面在 Portal。
- webapp：overleap 品牌下 Purchase 页整页替换为 `StripePurchasePanel`（`webapp/src/components/stripe/StripePurchasePanel.tsx`），`useStripeCheckout` 外链打开 Checkout/Portal。
- web 鉴权：HttpOnly cookie（`credentials: 'include'`，`web/src/lib/api.ts`），web 前端可直接调上述用户端点。
- web 品牌机制：host → brand（`web/src/lib/brands.ts`、`brand-server.ts`、`BrandProvider`）。

## 决策记录（本次拍板）

| 决策 | 结论 | 理由 |
|---|---|---|
| 网站购买定位 | 完整网页购买流 | 用户确认；欧美主流转化路径 |
| 套餐结构 | 年付 €89 + 月付 €11.99（随时取消） | 用户拍板；月付做低门槛入口 + 年付锚点（折合月价贵 60%+）；Stripe 无 Apple 1.5× 限制 |
| 品牌分支方式 | 页面级分流（方案 A） | kaitu 的 661 行 WordGate 流零改动；与 host 品牌机制同构 |
| 月付→年付升级 | v1 不做即时升级 | 防双扣守卫拒绝二次 checkout；路径 = Portal 取消 → 到期后购年付；proration 复杂度 v1 不背 |
| Billing Portal 能力 | 只开取消/换卡/账单，**禁 plan switching** | plan switching 绕过后端 validatePurchase（既有决策，沿用） |
| Stripe 账号 | 已有（Overleap LLC），用户提供 sk_test | 端到端可在测试模式验证 |

## 1. Stripe 侧资源（测试模式先建；live 切换时照此重建）

- Product：**Overleap Basic**。
- Price 年付：recurring interval=year，**EUR 89.00** 为主币种，`currency_options`：USD 79.00、GBP 79.00（Checkout 按客户属地自动选币，与 ASC 定价 $79/€89/£79 对齐）。
- Price 月付：recurring interval=month，**EUR 11.99**，`currency_options`：USD 11.99、GBP 9.99。
- Billing Portal configuration：开启 subscription cancel（at period end）、payment method update、invoice history；**关闭** subscription update/plan switching。
- 建法：`scripts/stripe-setup-overleap.sh`（curl 调 Stripe REST API，key 从 `STRIPE_SECRET_KEY` 环境变量读，脚本本身零密钥）。**幂等**：Price 带 `lookup_key`（`overleap_basic_1y` / `overleap_basic_1m`），先按 lookup_key 查、存在即输出既有 id 不重建；Product 按 `metadata.slug=overleap-basic` 查。live 切换时对 live key 重跑同一脚本。脚本输出 price id 供回填 Plan 行。

## 2. Plan 行（admin 数据，dev 库先建，生产上线时照建）

| PID | brand | 周期 | stripe_price_id | apple_product_id |
|---|---|---|---|---|
| `overleap-basic-1y` | overleap | 年 | 〈年付 price id〉 | `io.overleap.sub.basic.1y` |
| `overleap-basic-1m` | overleap | 月 | 〈月付 price id〉 | （空） |

- 两行同 tier（basic）——tier 校验允许月/年互购（但防双扣挡住并行订阅）。
- 月付无 `apple_product_id` → iOS 面板按品牌 `iapProductIds` 过滤后自动不展示月付：**月付是网页/桌面专属**，不新增 IAP 商品（ASC 侧的"月度付费"是年付商品的分期选项，另一回事）。
- 字段名以 `api/model.go` 为准：`stripe_price_id`（json `stripePriceId`）、`apple_product_id`（json `appleProductId`）。

## 3. web `/purchase` overleap 分支

- `purchase/page.tsx`（server component）读取品牌（既有 `brand-server.ts` 机制）：kaitu → 渲染现有 `PurchaseClient`（不动）；overleap → 渲染新组件 `OverleapPurchaseClient.tsx`（同目录）。
- `OverleapPurchaseClient` 行为：
  - 套餐区：年付主推卡（含"折合 €7.42/月，省 38%"式对比）+ 月付卡；数据来自既有 plans 接口（按品牌下发，只展示 `stripePriceId` 非空的 plan）。
  - 未登录：点购买 → 复用既有 `redirectToLogin()`（`web/src/lib/auth.ts`，`/login?next=<path>` 机制），next 指回 `/purchase?plan=<pid>`；登录回来后该套餐高亮，用户再点一次购买——**不自动触发 checkout**（避免登录后意外直跳付款页）。
  - 已登录：点购买 → `POST /api/user/stripe/checkout {plan: <pid>}` → `window.location.href = data.url`（同窗口跳转，非外链——网页场景没有"打开外部浏览器"问题）。
  - 已有活跃订阅（`user.subscriptions` 非空）：隐藏购买按钮，显示"已订阅"卡 + 链接到 `/account`。
  - `?checkout=cancelled`：页顶温和提示"支付未完成，可随时重试"，其余照常。
  - 错误处理：405001 → 品牌渠道不可用提示；其余按 web 既有错误码映射，不显示原始 message。
- i18n：新增独立 namespace（按 `web/messages/namespaces.ts` 既有机制注册）。overleap 站 locale 为 en-US/en-GB/en-AU/ja，四 locale 全量翻译；zh-* 三个目录放英文同文案文件（overleap 页面在中文 locale 不可达，仅保构建/加载完整性，不构成品牌泄漏面）。

## 4. web `/account` overleap 分支

- `account/page.tsx`：kaitu → 维持现状（redirect `/purchase`）；overleap → 渲染新组件 `OverleapAccountClient.tsx`。
- 未登录 → 引导登录（复用既有机制）。
- 订阅状态卡：套餐名、`currentPeriodEnd` 到期日、`autoRenew` 续订状态、provider 标识（stripe / apple——IAP 用户也可能访问此页）。
- 管理入口按 `manage.kind` 分派：
  - `stripe_portal` → `POST /api/user/stripe/portal` → 同窗口跳转 Portal；
  - `url` → 直接跳该 URL（IAP 用户 → App Store 订阅页）。
- `?checkout=success` 落地：webhook 入账异步 → 显示"正在激活订阅…"，轮询用户信息（间隔 3s，最多 10 次）；出现活跃订阅即切换为订阅卡 + **下载引导区**（各平台下载入口，网页付费→装 app 的转化收尾）；超时兜底："支付已完成，权益将在几分钟内到账，刷新本页查看。"
- 无订阅且非 success 回跳：显示"暂无订阅" + 引导 `/purchase`。
- 导航入口：确认 overleap 站 header 登录态用户菜单有 Account 入口（没有则补，kaitu 侧导航不动）。

## 5. webapp 顺手必修（同属 Stripe 收口）

`webapp/src/components/ios/IosMembershipPanel.tsx:58`：`manageUrl` 只特判 `kind==='url'`，`stripe_portal` 塌缩到 App Store 订阅页。修复：与 `StripePurchasePanel` 一致按 kind 分派（`stripe_portal` → `useStripeCheckout.openPortal()`）。同时给 `StripePurchasePanel` 的 `itms-apps://` 加非 Apple 平台的 https `apps.apple.com` fallback（Phase A 终审记录的同 phase 顺手项）。

## 6. 配置与 ops

- 本地/staging：`center/config.yml` 增 `stripe.secret_key` 与 `stripe.webhook_secret`——**已完成（2026-07-22）**，测试 key 取自用户指定的 `wordgate/nextpay/api/config.dev.yml`（同一 Stripe 账号测试模式；`config.yml` 里那对是占位符，勿取）。注意 nextpay 的 whsec 绑定其自身端点：本地 E2E 跑 `stripe listen` 时以 CLI 生成的 whsec **临时覆盖**再还原。跳转 URL 三项不配，走代码内 overleap.io 缺省。
- 上线清单（并入 runbook 记忆 `project_overleap_ios_asc_release_checklist.md` B 节）：
  1. live `sk_live_` + live webhook secret 切换（Dashboard 注册 `POST /webhook/stripe`，订阅 invoice.paid / customer.subscription.updated / customer.subscription.deleted / charge.refunded / charge.dispute.created 五事件）；
  2. live 侧重跑资源脚本建 Product/Price，Portal configuration 同参；
  3. 生产 admin 建两条 Plan 行（上表）；
  4. 真卡小额验证 + Slack `alert` 频道确认（三道支付哨兵依赖）；
  5. Stripe Tax 策略（商务侧待定，不阻塞代码）。

## 7. 测试与验证

- **单测**（web vitest）：品牌分流渲染（kaitu 出 WordGate 组件 / overleap 出 Stripe 组件）；checkout 调用与跳转；已订阅态隐藏购买；`?checkout=cancelled` 提示；success 轮询（出现订阅停轮询 / 超时兜底）；manage.kind 双分派。
- **回归**：api `go test ./...`（真库，0 SKIP 判据）；webapp 双品牌套件；web 既有契约测试。
- **品牌下发验证**：确认 `/api/plans` 经 BrandResolver 按请求品牌过滤（overleap.io 来源请求只见 overleap plans、`stripePriceId` 字段随 DataPlan 下发）——plan 阶段核实字段存在性，缺则属 api 侧小改。
- **端到端（测试模式，拿到 sk_test 后）**：Stripe CLI `listen --forward-to localhost:<port>/webhook/stripe` → 测试卡 4242 完成年付 checkout → 验 invoice.paid 入账、`user.subscriptions` 出现、success 落地页轮询成功 → Portal 取消 → `customer.subscription.deleted` 权益处理；月付同流程走一遍购买。
- 交付时截图过一遍购买/管理全流程。

## 明确不做（YAGNI）

- 月付→年付即时升级/proration；Portal plan switching。
- 新 IAP 月付商品（`io.overleap.sub.basic.1m` 仍是未来可选项，见 runbook）。
- kaitu 侧任何改动；WordGate 流重构。
- web 营销页内容改版（legal 文案、releases 404 是另案）。
