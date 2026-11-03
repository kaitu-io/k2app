# 开途 iOS：试用未转化召回（邮件 + 推送）— 设计 spec

日期：2026-11-03 · 基线：`main` @ 69fcfe430 · 决策人：David
相关：`2026-10-19-kaitu-ios-apple-trial-design.md`（7 天试用）、`2026-10-20-ios-device-account-design.md`（零身份设备账号）、`2026-11-03-ios-push-design.md`（推送通道，并行会话负责）

## 1. 意图

开途 iOS 自 10-27 起提供 Apple 7 天免费试用。截至 11-03，403 人开了试用，324 人（80%）已关闭自动续费；第一批今天到期，此后每天一批。关了续费的人到期不会被扣费，按现状几乎全部流失。

David 的判断：开过试用 = 有真实使用意愿；绑了邮箱 = 意愿更强、且能在 App 外联系到。召回的目标是把「试用未转化」的人转成付费。

iOS 的限制：iOS 用户在 App 内只能走 Apple 内购（一个年付订阅 `io.kaitu.sub.basic.1y`），授权码 / 活动码在 App 内无处可用。所以：

- **邮件**（App 外）→ 引导到**官网**购买，带活动码打折。苹果允许在 App 外联系用户告知其他购买方式；官网走现有 NextPay，多档套餐 + 活动码都是现成的，且没有苹果分成。官网购买的会员在 iOS App 内同样生效（会员挂在账号上）。
- **推送**（App 内渠道）→ 跳 App 内 `/purchase`，**原价**订阅。推送里不提官网、不提折扣（推送属 App 内沟通，提外部购买方式会触犯 3.1.1）。

## 2. 目标 / 非目标 / 反目标

**目标**
- 每天自动圈出「试用未转化」的开途用户，在两个时间点各触达一次：试用结束前约 1 天、结束后约 3 天。
- 有邮箱的人收邮件；已授权营销推送的人收推送（推送通道就绪后）。
- 能量化：发了多少、多少人用活动码在官网下单、多少人在 App 内订阅。

**非目标**
- Apple 优惠码 / 推广优惠 / 赢回优惠（要三端改造 + 过审，本次不做）。
- Overleap（官网支付与活动码只有开途有）。
- 推送通道本身（APNs、权限弹窗、营销同意）——归 `2026-11-03-ios-push-design.md`。
- 试用结束后第一笔扣款失败（billing_retry）的「更新付款方式」提醒——归推送 spec §6.4。
- 已付过费用户的续费提醒 / 过期召回——现有 `worker_renewal_reminder.go` 已覆盖。

**反目标（做了也算失败）**
- 给已经付费、或试用续费开着、即将被自动扣费的人发「快来买」——这是在劝人多付一次钱，或打扰本来就会转化的人。
- 同一个人同一阶段收到多封。
- 推送内容里出现官网、外部价格、活动码（App 审核风险）。
- 把开途的活动码 / 价格发给 Overleap 用户。

## 3. 约束（代码现状）

- 试用状态在 `subscriptions`：`provider='apple'`、`environment='Production'`、`is_trial`、`trial_ends_at`、`auto_renew`、`status`（active|grace|billing_retry|expired|revoked）。试用不建订单、不算首单（`users.is_first_order_done` 保持 false）。
- 邮箱只在 `login_identifies`（`type='email'`），取法 `getUserEmailFromIdentifies`。**试用只认设备、不要求邮箱**（10-20 决策），所以相当一部分试用用户是零身份设备账号，邮件够不着——**邮件覆盖率未测**（§8 第一项）。
- 现有生命周期邮件 worker（`worker_renewal_reminder.go`，cron `30 2 * * *` UTC）刻意排除从没付过费的人（`lifecycleEmailCandidates` 要求 `is_first_order_done`，`TestRenewalReminder_ExcludesNeverPaid` 守着）。试用召回是**另一类人**，不能改那个查询，要新开一条。
- 邮件发送用 `SendTemplatedEmails`（模板 slug、按收件人品牌与语言渲染、`email_send_logs` 记日志）；模板缺失时现有做法是 Slack 告警 + 跳过（照抄）。系统内**没有退订机制**（见 §10）。
- 活动码只作用于官网 WordGate/NextPay 下单（`api_order.go`）；`first_order` matcher = 从没付过费的新客，试用用户正好符合。现有 winback 用 `campaignVarsForBrand` 生成 `CampaignCode` / `SavingsText` / `ValidDays` 模板变量——复用。
- 官网购买页 `web/src/app/[locale]/purchase/PurchaseClient.tsx` 的活动码**只能手输**，URL 不能预填。
- 推送接口（推送 spec §6.2，由并行会话先合并）：`PushToUser(ctx, userID, PushRequest{Category, DedupKey, Title, Body, Route}) error`；同步错误 `ErrPushChannelUnavailable` / `ErrPushNoActiveToken` / `ErrPushNotConsented` / `ErrPushDuplicate`；nil 只代表已入队。Title/Body 由调用方按 `users.language` 本地化。路由白名单含 `/purchase`。

## 4. 方案取舍

| 方案 | 做法 | 取舍 |
|---|---|---|
| **A. Center 每日自动召回任务（已选）** | 新 cron 任务按规则圈人，发邮件 + 推送，按人按阶段只发一次 | 全自动、可量化、推送就绪后只加一处调用；要写代码、过部署，上线前到期的几批靠首跑补发覆盖 |
| B. 每天手工跑名单、手工群发 | 现有 EDM + 审批 | 今天就能做，但每天要人值守、接不了推送 |
| C. 先 B 后 A | 手工过渡 + 开发 A | 不漏人，但多一套临时流程 |

David 选 **A**（2026-11-03）。上线前到期的人用 §5.4 的首跑补发接住。

付款路径 David 已确认（2026-11-03）：邮件 → 官网 + 活动码；推送 → App 内原价。

## 5. 设计

### 5.1 谁会被召回（候选判定）

对象：开途品牌（`users.brand='kaitu'`）、Production 环境的 Apple 试用订阅链，且用户**从没付过费**（`is_first_order_done` 为 false/NULL）。

| 阶段 | 时间窗（按 `trial_ends_at`） | 额外条件 | 意图 |
|---|---|---|---|
| **S1 到期前** | 落在「运行时刻 + 0 ～ +48 小时」 | `is_trial` 且 `auto_renew = false`；`users.expired_at ≤ trial_ends_at + 1 天` | 「试用在 X 日结束」 |
| **S2 到期后** | 落在「运行时刻 −10 天 ～ −2 天」 | `users.expired_at ≤ now`；订阅 `status ≠ billing_retry` | 「试用已结束，限时优惠回来」 |

说明：
- **试用期间 `users.expired_at` 就等于试用结束时刻**（试用交易会把会员时长加到试用结束，`logic_apple_iap.go` 试用分支），所以 S1 不能用「`expired_at > now` 就排除」——那会排除所有人。S1 只排除会员时长**超出试用结束 1 天以上**的人（已经在官网买了、或拿了邀请奖励）。
- **S2 以 `users.expired_at ≤ now` 为准**，不看订阅 `status` 是否已从 active 改成 expired：关了续费的试用到期后，Apple 不会扣费，但 `status` 可能要等 webhook / 对账才更新，用它判定会漏人。唯一看 `status` 的地方是排除 billing_retry（推送 spec 负责「更新付款方式」）。
- 两个阶段都排除已付过费（`is_first_order_done`）。S1 排除续费开着的人（会被自动扣费；Apple 自己会发续费通知）。

**每人每阶段终身只发一次**（不是每天一次，也不是每条订阅链一次）：幂等键 = `trial-recall:<stage>:<user_id>`。同一用户有多条试用链（换 Apple ID）时只发一次。邮件侧只有 `sent` 状态算「已发过」，`failed` 的下一次运行会重试；推送用同一个字符串当 `DedupKey`。窗口刻意比一天宽（S1 48 小时、S2 8 天），这样 cron 漏跑一天、或者 Apple 状态晚一天同步都不会漏人；重复由幂等键挡住。S1 窗口宽到 48 小时，所以文案用具体日期（`TrialEndDate`），不写「明天」。

（我的判断）S2 选「结束后约 2～10 天」而不是精确第 3 天：真正的去重靠幂等键，窗口宽一点等于自带补发。

### 5.2 发什么

**邮件**（有邮箱的人）
- 模板 slug：`trial-recall-before`（S1）、`trial-recall-after`（S2），品牌 kaitu，多语言沿用 EDM 模板翻译机制。模板内容由运营在后台建，缺失时 Slack 告警并跳过该阶段（照抄 winback）。
- 活动码缺失或已失效（`campaignVarsForBrand` 返回空）时，**整个邮件阶段跳过并 Slack 告警**，不发不带码的邮件（我的判断：没码的召回邮件等于原价广告，还会消耗掉这个人「只发一次」的名额）。`SavingsText` 沿用 winback 的现有格式（开途套餐以美分定价，现有 winback 邮件已在用）。
- 变量：`CampaignCode`、`SavingsText`、`ValidDays`（`campaignVarsForBrand`）、`PurchaseURL`（官网购买页，带活动码与 utm，见 §5.3）、`TrialEndDate`（S1 用）。
- 文案方向：S1 =「你的 7 天试用将在 X 结束，用这个码在官网购买享 8 折」；S2 =「试用已结束，限时 8 折回来」。中文面一律「开途」，不出现 Kaitu 裸词。

**推送**（已授权营销推送的人，推送通道就绪后）
- `PushRequest{Category: marketing, DedupKey: 同上, Route: "/purchase", Title/Body: 按 users.language 本地化}`。
- 文案只讲「试用即将结束 / 已结束，继续使用请订阅」，**不提官网、折扣、价格**。
- 文案表写在 Go 代码里（与推送 spec §6.4 的交易类文案同一种做法），覆盖 zh-CN / zh-TW / en-US / ja，缺语言按品牌默认语言兜底。

**两条渠道独立发**（我的判断）：有邮箱就发邮件，有营销同意就发推送，两者都满足就两条都发。理由：两条内容不同（一个带官网折扣、一个是 App 内原价），且推送同意率预计很低，没必要用推送去「替代」邮件。推送同步返回的四种错误只记日志计数，不触发额外动作。

### 5.3 官网落地

- 官网购买页支持 URL 预填活动码：`/{locale}/purchase?campaign=<CODE>`。打开时自动填入并展开活动码区域、触发预览价格；码无效时与手输无效同样提示。未登录用户先走现有的邮箱验证码登录，登录后保留该参数。
- 邮件里的链接带 `utm_source=email&utm_medium=lifecycle&utm_campaign=trial-recall-<stage>`。

### 5.4 运行方式

- 新 Asynq cron 任务（例如 `TaskTypeTrialRecall`，独立文件 `worker_trial_recall.go`），每天跑一次，时刻跟随现有生命周期任务（`30 2 * * *` UTC，北京时间 10:30），在 `worker_integration.go` 里接线。
- **首跑补发**：S2 的 −10 天下界让上线当天自动覆盖 10-27 以来已经到期的人；S1 漏掉的人（上线前已到期）只收 S2。
- 每次运行打一行汇总日志：各阶段候选数、排除原因计数、邮件 sent/skipped/failed、推送各错误计数。

### 5.5 活动码

- 新建一个共享码（建议 `TRIAL80`），`discount`、`first_order`、8 折，品牌 kaitu，有效期先设到 2026-12-31。通过后台建（走审批）。
- 共享码**做不到「每人 72 小时有效」**（我的判断：不为此做一人一码）。邮件里的 `ValidDays` 写成「本周内有效」这类软期限；真正的截止是活动码的全局到期日。码外传的风险可接受：`first_order` 只给从没付过费的人用。

### 5.6 量化

- 邮件：`email_send_logs` 按 slug 统计 sent。
- 官网转化：使用 `TRIAL80` 且已付款的订单数（现有 `campaign_stats` / `campaign_orders`）。
- App 内转化：被召回的人（有发送记录）在发送后 14 天内产生首笔 Apple 付款。
- 推送：`push_send_logs` 按 DedupKey 前缀 `trial-recall:` 统计。

## 6. 上线顺序

1. 运营建邮件模板 `trial-recall-before` / `trial-recall-after`（kaitu），建活动码 `TRIAL80`。
2. 部署官网（URL 预填活动码）——`git push origin main:website`。
3. 部署 Center（召回任务）。模板或活动码缺失时任务只告警不发，所以 1 和 3 的先后不会出事故，但 1 不做就什么都不会发。
4. 推送：等推送 spec 的签名合入 main 后再接上推送调用；在那之前召回任务只发邮件。不需要等推送的原生版本和用户授权——没有 token 的人 `PushToUser` 同步返回 `ErrPushNoActiveToken`，只记计数。

## 7. 测试要求（关键的几条）

- 候选判定逐条有测试：S1 续费开着的不选、**S1 `expired_at` 恰好等于 `trial_ends_at` 的试用用户要选上**（正例，防止 §5.1 那个排除写反后测试全绿却谁都不选）、S1 会员超出试用 1 天以上的不选、S2 billing_retry 不选、S2 `status` 仍是 active 但 `expired_at ≤ now` 的要选上、已付过费不选、Overleap 不选、Sandbox 不选。
- 幂等：同一用户连续两天都在窗口内只发一次；同一用户两条试用链只发一次；`failed` 的下次重试（邮件与推送各测）。
- 活动码缺失 → 该阶段不发邮件、有告警。
- `TestRenewalReminder_ExcludesNeverPaid` 继续绿（不碰现有 worker 的候选查询）。
- 推送文案表断言：任何语言的 Title/Body 都不含「官网」「折」「%」「$」「kaitu.io」等字样。
- 官网预填：`?campaign=X` 自动填入并触发预览；无效码显示无效提示。
- 新 handler 要在 `worker_integration.go` 接线（漏接不会报错）。

## 8. 留给构建者

1. **先测邮件覆盖率**：按 S1/S2 状态统计有邮箱 / 只有 Apple 登录 / 零身份的人数（SQL 草稿在会话 scratchpad `trial-email.sql`）。如果有邮箱的比例极低，要回报 David，不影响本设计，但影响预期。
2. 只有 Apple 登录（无 email identity）的用户能否拿到可用邮箱（Apple 中继邮箱）——查 `login_identifies` 里 apple 行存了什么；能拿到就作为邮件地址，拿不到就只走推送。
3. 「每人每阶段只发一次」在邮件侧的具体实现：现有 `IsIdempotencyKeyExists` 按 `(batch_id, template_id, user_id)` 判，不能跨批次，需要另写按 `(template, user, status=sent)` 的查询或自建小表。
4. 官网未登录时，登录流程跳回购买页要保留 `?campaign=`（确认现有登录跳转是否保留 query，不保留就补）。
5. 推送文案的具体措辞。

## 9. 已做的判断（David 可推翻）

- S1/S2 两个时间点、S2 宽窗口 + 按用户幂等去重（§5.1）。
- S2 以会员是否已过期为准，不依赖订阅 status 是否及时更新（§5.1）。
- 活动码失效时整个邮件阶段停发（§5.2）。
- 邮件与推送独立发，不做互相回退（§5.2）。
- 共享活动码 `TRIAL80` 8 折、不做一人一码（§5.5）。
- 只做开途。

## 10. 可选项（不在本次范围，David 决定）

- **邮件退订链接**：系统目前没有退订机制；营销邮件按多数地区规范应带退订。可以加一个最简退订（标记用户不再收生命周期营销邮件）。
- 第三次触达（例如结束后 14 天、更深折扣）。
- Apple 赢回优惠（Win-back Offer）：让推送渠道也能打折，需要三端改造 + 过审。
