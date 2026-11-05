# 扣费失败用户的挽回（billing issue recovery）

日期：2026-11-05 · 分支 `feat/billing-issue`

## 为什么做

试用转化很低（近 30 天 499 个试用，已结算的 27 条转化为 0）。愿意付费、只是**扣款失败**的人数量很少，但他们是最接近付费的那批。如果这一步卡住，最后这批人也会流失。目标是让这批人顺利完成付款。

## 现状（2026-11-05 核实）

- 订阅状态 `grace` / `billing_retry` 由 `computeRenewalState`（`api/logic_apple_iap.go`）根据 Apple 的 `IsInBillingRetryPeriod` / `GracePeriodExpiresDate` 计算出来。Stripe 只会映射成 `billing_retry`（`stripeSubStatus`，`logic_stripe.go:21-33`），唯一写入点是 `applyStripeSubscriptionUpdate`；Stripe 进入扣费失败时**没有推送**。
- `isSubscriptionLive` 把 `grace` / `billing_retry` 视为活跃，所以 App 的 affordance 是 `manage`，不允许再购买（防止重复扣款）。
- **缺口 1**：`DataSubscription`（`api/type.go`）没有状态字段，App 看不出付款失败，会员页仍显示「试用中」或「续订中」。
- **缺口 2**：试用链第一笔扣款失败时**不享受宽限**（spec 2026-10-19 §3.5），会员在试用结束时就停了。用户连不上，App 里的会员卡片还写着试用，首页按钮只泛泛提示「续费」。
- **缺口 3**：进入扣费失败时只推送一次（`notifyBillingRetryIfEntered`）。没开推送的人收不到，没有邮件，也没有后续提醒。
- iOS 16.4+ 的 StoreKit 会在 App 回到前台时自动弹出 Apple 的「付款问题」弹窗。我们没有监听 `Message.messages`，所以这个弹窗是生效的，不需要我们实现。

## 目标 / 非目标

目标：
1. App 能识别「付款未成功」，在会员页和首页给出明确说明，并提供一键去更新付款方式的入口。
2. 连不上时告诉用户原因是付款失败，而不是会员到期。
3. 进入扣费失败后补发提醒：第 0 / 3 / 7 天，推送加邮件（有邮箱的用户）。付款恢复或订阅终结后自动停止。

非目标：
- **不给试用链加宽限**（David 选 A：立刻停服，靠讲清原因来挽回）。§3.5 不变。
- 不改防重复售卖逻辑：扣费失败期间仍然不允许重新购买，修好付款方式后 Apple 会对原订阅扣款。
- 不做 `Message.messages` 拦截或自定义弹窗。
- 不做营销优惠。这是交易类提醒，不带优惠码，不进召回对照组。
- **Stripe 只做 App 内卡片，不发补发提醒。** Stripe 账号与其他业务共用，它自带的催款邮件（Smart Retries）由账号级设置决定，我们再发一封可能重复；Stripe 用户主要在网页或桌面，本来就收不到 APNs 推送。

反目标：提醒不能在用户已恢复付款、或订阅已终结之后还继续发出。

## 做法（方案选择）

- **采用：服务端下发布尔值 `billingIssue` + 客户端按它渲染 + 每日 worker 补发提醒。**
- 不采用：把 `status` 原样下发。这会把内部的五态字符串变成跨层契约，客户端实际只需要「是否付款失败」这一个判断。
- 不采用：只靠 Apple 系统弹窗。弹窗关掉之后就找不到了，也和我们界面上的「试用中」互相矛盾。
- 不采用：试用链加宽限（方案 B，David 已否决）。

## 设计

### 1. 服务端

- `DataSubscription` 增加 `BillingIssue bool \`json:"billingIssue"\``：`status ∈ {grace, billing_retry}` 时为 true。在 `logic_apple_iap.go` 组装 DTO 的地方填写，Stripe 分支同理。
- `Subscription` 新增列 `billing_issue_since`（int64 unix 秒，默认 0）：从非问题状态进入 `grace` / `billing_retry` 的那次写入时，记为当前时间。**不需要清零**，因为 worker 只扫描当前状态仍属于问题状态的行；下一次再进入时会重新写入。只有 Apple 写入（Stripe 不做提醒，见非目标）。
  - 写入点：`applyRenewalInfo` 把 `billing_issue_since` 放进同一个 `updates` map，所以它和 status 一起受 `status<>'revoked'` 与 signedDate 守卫保护。「是否为进入」的判断用事务外快照 `sub.Status`（与 `notifyBillingRetryIfEntered` 同一判据），不进 SQL。我的判断：快照过期最坏的后果是 since 被晚写一次，或者重入时没有重置，只会让提醒日期偏一天，不会误发给已恢复的人，因为 worker 按当前 status 过滤。为此把判断写进 SQL 不值得。reconcile 也走这条路径，自动覆盖。
  - **存量回填**：migrate 时把当前 `status IN (grace, billing_retry) AND billing_issue_since = 0` 的 Apple 行设为部署时刻（GORM 结构化更新，幂等）。这样存量用户会在部署后第 3 / 7 天收到提醒，但收不到第 0 天那封邮件，因为第 0 天由事件触发。
- 首次（第 0 天）的提醒：保留现有的进入时推送（事件触发、立即）；第 0 天的**邮件由每日 worker 发**（`billing_issue_since` 落在过去 24 小时内），不在 webhook 里同步发信，因为 `SendTemplatedEmails` 是同步 SMTP，会拖慢 Apple webhook（计划阶段的决定）。
- 每日 worker `processBillingIssueReminders`，挂在现有每日生命周期任务 `handleRenewalReminderTask` 上：对 d ∈ {0, 3, 7}：查询 `provider=apple AND environment=Production AND status IN (grace, billing_retry) AND billing_issue_since ∈ (now−(d+1)·24h, now−d·24h]` 的订阅，给每个订阅的用户发一封邮件（如有邮箱）；d ∈ {3, 7} 时再推送一次（d=0 的推送已由事件触发）。每日任务每天只跑一次（`Unique 25h`），所以每个窗口只命中一次。
  - 推送 DedupKey：第 3 / 7 天为 `billing_issue_<d>:<sub_id>:<billing_issue_since>`；第 0 天保留现有的 `billing_retry:<sub_id>:<current_period_end>`（`logic_push_triggers.go:52`），不改。
  - 邮件 batch id 为 `billing-issue:<d>d:<YYYY-MM-DD>`（与续费提醒同一形状），走 `SendTemplatedEmails`，由它保证每个 batch 内对每个用户幂等。
  - 推送文案沿用 `pushCopyBillingRetry`，路由 `/purchase`。
- 邮件通道：**数据库模板 + `SendTemplatedEmails`**，与续费提醒同一套（不用代码里写死的 `brandedEmailTemplate`）。slug 统一为 `billing-issue`，模板内用一个变量区分第 0 / 3 / 7 天，或者三天共用同一份文案（builder 定）。模板由后台按 `(slug, brand)` 为两个品牌各建一份（部署清单项）。模板不存在时按 `templateSlugExists` 的做法报 Slack 告警并跳过。
- 邮件属于**服务通知**：把 `billing-issue` 加进 `edmOptOutExemptSlugs`（`logic_email_send.go`），不受 `edm_opt_out` 影响；不走 `recallMailer`，所以不进召回对照组。

### 2. 客户端（webapp）

- `api-types.ts` 中 `DataSubscription` 增加 `billingIssue?: boolean`（可选：老服务端不返回时等同 false）。
- 新组件 `BillingIssueCard`（`components/ios/`）：标题「付款未成功」，正文「Apple 没能扣款，更新付款方式后会员会自动恢复」（试用链未转付费时改为「试用已结束，但首期扣款未成功……」）。按钮「更新付款方式」：
  - `manage.kind === 'apple_settings'`：在 iOS 上打开 `itms-apps://apps.apple.com/account/billing`（需真机验证；不可用时回退到现有的 `APPLE_SUBS_URL`）。非 iOS 平台不显示按钮，正文提示「请在 iPhone 的 设置 → Apple ID → 付款与配送 中更新」。
  - `stripe_portal` → `openPortal()`；`url` → 打开该 URL（与 `openManage` 的分派一致）。
- `IosMembershipPanel`：`activeSub.billingIssue` 为 true 时，用 `BillingIssueCard` 取代 Trial / Renewal 卡片。
- `SubscriptionManagePanel`：这是 Stripe 用户和非 iOS 平台上的 Apple 订阅用户看到的会员页（`Purchase.tsx` 的 manage 分支，以及 `StripePurchasePanel`）。在其顶部同样插入 `BillingIssueCard`，否则卡片里的 Stripe 分支和非 iOS 指引永远显示不出来。
- 首页（`Dashboard`，所有平台）：任一订阅 `billingIssue` 为 true 时，在 `HomeTrialCard` 的位置显示同一张卡片（紧凑版即可），优先级高于试用卡片。`upsellHint` 在付款失败时改为「付款未成功，更新付款方式后即可连接」，点击进入 `/purchase`。
- 漏斗埋点：按钮点击复用现有 `manage_click` 事件，带 `source=billing_issue`（`PAYWALL_SOURCES` 增加这一项，它只在 webapp 侧定义，不跨层）。不新增事件，所以不需要改 api 的事件登记表（计划阶段的决定，比两层同时登记省事）。
- i18n：新 key 必须写进**全部** locale（`locale-coverage.test.ts` 要求每个 locale 的 key 与 en-US 完全一致）；中文 / 英文 / 日文人工写，其余语言机翻。品牌字面量不进页面（文案里不写品牌名）。

### 3. 发布

- api：`make deploy-api`，含一次 migrate（新列 + 存量回填）。部署前在后台为两个品牌创建 `billing-issue` 邮件模板。
- webapp：推 `webapp/x.y.z` tag 才会到达客户端。老客户端忽略新字段，不受影响。

## 约束

- Apple 通知顺序不保证：`billing_issue_since` 和 status 写在同一个条件 UPDATE 里，受同样的 signedDate / revoked 守卫保护。
- 推送和邮件失败只记日志，不能让 webhook 或 worker 失败。
- 推送和邮件一律不写品牌名、金额或订单号。

## 留给 builder

- 首页紧凑卡片的具体样式（与 `HomeTrialCard` 保持一致）。

## 我做的决定（David 可改）

- 提醒节奏为第 0 / 3 / 7 天，第 7 天之后不再提醒（Apple 重试最长 60 天，提醒太多会让人反感）。
- 只下发布尔值 `billingIssue`，不下发 status 字符串。
- 非 iOS 平台不提供跳转 Apple 付款页的按钮，只显示文字指引。
- Stripe 只做 App 内卡片，不做第 0 / 3 / 7 天提醒。
- 邮件走数据库模板，与续费提醒同一通道，并加入退订豁免名单。
- 存量的扣费失败订阅回填为部署时刻，部署后第 3 / 7 天补发提醒。
- 「进入」判断沿用事务外快照，不写进 SQL（理由见 §1）。
