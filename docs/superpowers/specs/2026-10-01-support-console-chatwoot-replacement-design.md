# 客服工作台：替换 Chatwoot 的自建即时沟通 + 用户跟进体系

- 日期：2026-10-01
- 状态：设计待审
- 范围：`api/`（Center）、`web/`（官网 + 管理后台）、`sites/overleap/`、`webapp/`（仅清理与一处工单修正）

## 1. 目的

下掉 Chatwoot，把即时沟通收进 Center 和现有管理后台，并让客服在一处看全一个人：
浏览轨迹与所处阶段、工单、用户资料、跟进备注。

Chatwoot 现状的三个问题：

1. 它是跑在 Center 那两台共用 EC2 上的一组容器，挤占 Center 自己的内存和 CPU。
2. 挂件完全匿名，全仓没有任何向它传递用户、设备或 sid 的代码，客服不知道对面是谁。
3. 它与工单、用户资料、转化漏斗互不相通。

成功标准：

- 仓库内 Chatwoot 零引用，容器停止，ALB 规则与 DNS 清除，旧令牌作废。
- 客服收到 Slack 提醒，点链接直达会话，右侧能看到这个人的身份、阶段、轨迹、工单、备注。
- 新方案不新增常驻进程。

## 2. 核心概念

### 2.1 两根互相独立的轴

**通道按"这笔钱付出去之前还是之后"分：**

| | 付款前（转化） | 付款后（售后） |
|---|---|---|
| 话题 | 买不买、选哪个、怎么付、付不了、续不续 | 连不上、慢、退款、账号问题 |
| 谁会来 | guest 首购，**以及 user 复购** | 只有 user |
| 通道 | 即时沟通（会话） | 工单 |

**身份按"账号是否成立"分：**

- **guest**：没有通过过验证码、也没有买过。官网未登录访客、App 已装未登录的人都是 guest。
- **user**：验证码通过或已购买。注册未付费的人是 user，阶段标为"未付费"。

身份只决定客服能看到什么（guest 只有轨迹，user 有完整资料），不决定走哪条通道。
复购的 user 在购买页发起的是会话，不是工单。

### 2.2 人工只用在三类情况

本业务是单用户零售订阅，客单价撑不起人工逐个说服。AI 层接住绝大部分对话，人工留给：

- 付不出去：支付失败、支付方式不支持、卡在结账页。
- 高客单价：路由器、专属线路。
- 复购流失：到期未续的老用户回到购买页。

### 2.3 App 内不做即时沟通

- iPhone 购买走 Apple 一键支付，试用是比客服更强的杠杆（试用为漏斗 spec 第三期，尚未上线）。
- 非 iPhone 的 App 购买会打开浏览器进官网支付页，已被官网入口覆盖。
- App 内保持工单。

## 3. 现状（2026-10-01 摸底）

已有，复用：

- 工单：`FeedbackTicket` / `TicketReply`（`api/model.go:1350-1381`），用户侧与管理侧接口，
  后台页 `web/src/app/(manager)/manager/tickets/page.kaitu.tsx`，回复邮件通知。
- 用户详情页：`web/src/app/(manager)/manager/users/detail/page.kaitu.tsx`。
- `RoleSupport` 角色（`api/type.go:22`）。
- Slack：`slack.Send("customer", ...)` 单向 webhook。
- 漏斗：`funnel_events`、`funnel_identities`，纯函数 `funnelWalk`、`funnelPersonKey`。
- AI 应答：`api/api_chatwoot.go`（欢迎语、选项、知识库作答、`[TRANSFER_HUMAN]`）。
- 分销商跟进备注：`RetailerNote` + 到期 Slack 提醒，形状可照搬。

没有，新建：

- 会话与消息存储、访客挂件、客服收件箱。
- 实时通道：api 内无 WebSocket、无 SSE、无 Redis 订阅。
- 按人存取的漏斗视图：现在只有聚合看板，`funnel_events.anon_id` 无索引。
- 针对用户的备注、标签、跟进日志。

部署事实：

- 两个官网在 Amplify 上，各有 Amplify 托管的 CloudFront（`docs/ops/web-amplify.md`）。
  官网 `/api` 经 SSR Lambda 转发到 Center，Lambda 上限 30 秒，长连接不能走这条路。
- Center 是 CloudFront → ALB（按域名分流）→ 两台 EC2，域名 `k2.52j.me`。
- Chatwoot 在同一 ALB 上，目标组 `tokyo-alb-tg-chatwoot`，端口 3000，域名 `chat.anc.52j.me`。

## 4. 数据模型

全部为 Center MySQL 新表，在 `api/migrate.go` 注册。

### 4.1 身份

**设计原则：只存观察到的事实；强证据自动合并，弱证据只提示；合并只改指针，可撤销。**

guest 留的邮箱按定义永远未验证（验证过就是 user 了），任何人都能填任何邮箱，
所以邮箱相同不能作为自动合并的依据。

`guests`

| 字段 | 说明 |
|---|---|
| `id` | 主键 |
| `brand` | 品牌，出生属性 |
| `merged_into_id` | 空表示自己是根；非空指向根。合并时压平，深度恒为 1 |
| `locale`、`country` | 首次出现时记录 |
| `first_seen_at`、`last_seen_at` | 留存清理依据 `last_seen_at` |

`guest_identities`

| 字段 | 说明 |
|---|---|
| `guest_id` | 指向**原始** guest，合并时不改写 |
| `brand` | |
| `kind` | `cid`（聊天 cookie）、`sid`（漏斗 cookie）、`did`（设备哈希）、`email` |
| `value` | |
| `strength` | `verified`（服务端从 cookie 或设备读到）或 `claimed`（对方自报） |
| `first_seen_at`、`last_seen_at` | |

唯一索引 `(guest_id, kind, value)`；查找索引 `(brand, kind, value)`。
**不对 `(kind, value)` 做唯一约束**：同一邮箱可以挂在多个 guest 上。

`guest_merges`（只追加）

| 字段 | 说明 |
|---|---|
| `from_guest_id`、`into_guest_id` | |
| `reason` | `same_cid`、`same_sid`、`manual` |
| `evidence_identity_id` | 依据的那条标识 |
| `actor_id` | 空表示系统 |
| `undone_at`、`undone_by` | 撤销记录 |

`guest_user_links`

| 字段 | 说明 |
|---|---|
| `guest_id`、`user_id`、`brand` | 多对多 |
| `evidence` | `session_login`、`widget_verify`、`manual` |
| `created_by`、`created_at` | |

唯一索引 `(guest_id, user_id, evidence)`。

**合并规则：**

| 观察 | 处理 |
|---|---|
| 同一 `cid` | 就是同一个 guest |
| 请求里的 `sid` 已挂在同品牌另一个根 guest 上 | 自动合并（同一浏览器） |
| 两个 guest 自报了同一邮箱 | 不合并。面板提示"可能是同一人"，客服可一键合并 |
| 客服手动合并 | 记 `manual`，可撤销 |

弱证据不传递：A 与 B 同邮箱、B 与 C 同浏览器，不会自动把 A 与 C 连成一人。

读取一个人的全部数据时，先取根，再取 `merged_into_id = 根` 的全部 guest。
撤销合并只需还原指针，会话与备注始终指向原始 guest，不丢数据。

**转化关联（guest → user）的来源：**

| 来源 | 强度 | 存储 |
|---|---|---|
| 建会话时请求已带登录态 | 强 | 写 `guest_user_links`，`session_login` |
| 挂件内完成验证码登录 | 强 | 写 `guest_user_links`，`widget_verify` |
| guest 的 `sid` / `did` 在 `funnel_identities` 里关联到了 user | 强 | **不复制**，读取时联查，单一事实源 |
| guest 自报邮箱等于某 user 的邮箱 | 弱 | 不存，读取时计算，面板标"访客自报，未验证" |
| 客服手动关联 | 强 | 写 `guest_user_links`，`manual` |

### 4.2 会话

`conversations`

| 字段 | 说明 |
|---|---|
| `uuid` | 对外标识 |
| `brand` | |
| `subject_kind`、`subject_id` | `guest` 或 `user` |
| `status` | `open`、`closed` |
| `handler` | `ai`、`human` |
| `assignee_id` | 指派客服，可空 |
| `entry_path` | 发起时所在页面 |
| `ticket_id` | 转成工单后填写 |
| `last_message_at`、`last_message_by` | |
| `staff_unread`、`visitor_unread` | |
| `slack_notified_at` | Slack 去抖 |
| `closed_at` | |

一个主体在一个品牌下最多一个 `open` 会话。客服手动关闭，或 72 小时无消息自动关闭；
关闭后的新消息开新会话。

guest 在会话中途通过验证码成为 user 时，会话主体不改写，只增加一条关联；
面板经关联展示 user 资料。

`conversation_messages`

| 字段 | 说明 |
|---|---|
| `id` | 自增，兼作拉取游标 |
| `conversation_id` | |
| `sender_type` | `visitor`、`ai`、`staff`、`system` |
| `sender_id` | 客服时为管理员 id |
| `kind` | `text`、`image`、`options`、`option_reply`、`event` |
| `content`、`meta` | `meta` 为 JSON |

支持图片消息。现有欢迎语明确引导访客发截图，支付失败截图也是人工介入的主要依据。
上传复用现有 S3 上传路径，具体接口在实现计划中确定。

### 4.3 备注与标签

`contact_notes`：`subject_kind`、`subject_id`、`content`、`follow_up_at`、`is_completed`、
`operator_id`、`assignee_id`、`slack_notified`，软删除。形状同 `RetailerNote`。

`contact_tags`：`subject_kind`、`subject_id`、`tag`、`created_by`。唯一索引 `(subject_kind, subject_id, tag)`。

不复用 `retailer_notes`：它的权限、待办查询和语义都绑定分销商，混用会把客服备注掺进分销待办。

不做销售管道、线索评分。零售量级用不上。

### 4.4 对现有表的改动

- `funnel_events` 增加索引 `(anon_id, occurred_at)`。
- `feedback_tickets` 增加可空列 `guest_id`（见 §9.2）。

## 5. 访客侧

### 5.1 入口

两个官网的定价页、购买页、支付结果页、支持页。不做全站悬浮。

### 5.2 流程

1. 访客点开挂件，发同源请求 `POST /api/chat/session`，带当前页面路径。
2. Center 从 cookie 读 `cid`、`sid` 和登录态：
   - 已登录 → 主体是 user。
   - 未登录 → 按 `cid` 找或新建 guest；有 `sid` 就挂上并按 §4.1 判断合并。
   - 没有 `cid` 则种一个：一方、HttpOnly、`SameSite=Lax`、400 天，与 `sid` 同样的属性。
3. 返回会话状态、历史消息、一枚短期连接令牌和 WebSocket 地址。
4. 挂件连 WebSocket；连不上则用 HTTP 拉取运行，功能不受影响。

`cid` 只在访客主动点开挂件时才种，属于访客请求的功能本身，不受统计退出和 GPC 影响。
退出统计或开了 GPC 的访客没有 `sid`，聊天正常，只是面板里没有轨迹。

### 5.3 AI 层

把 `api/api_chatwoot.go` 的逻辑迁到新会话上，改动只有输入输出两端：

- 输入从 Chatwoot webhook 改为进程内的新消息事件。
- 输出从 `chatwoot.Reply` 改为写 `conversation_messages`。
- 保留 `filesearch.Ask(ctx, "crm", ...)`、`api/data/system_prompt.md`、`[TRANSFER_HUMAN]` 约定。

现有四个开场选项为：安装问题、购买/续费、使用问题、与客服视频。迁移后：

- 前三项保留，其中"使用问题"在对方是 user 时导向工单（§5.5）。
- **"与客服视频"不迁移**。视频通话是 Chatwoot 自带能力，自建不做。这是功能减少，需产品知悉。

单会话 AI 调用上限 20 次，超过后直接转人工。

### 5.4 客服不在线

转人工后若一段时间无客服接入，挂件请访客留邮箱。邮箱记为 `claimed` 标识。
客服回复时，若访客已离线，发一封带回链的邮件。回链打开官网并恢复该会话。

### 5.5 转成工单

触发：话题属于付款后，由 AI 判定或客服手动触发。

- 对方已登录：建 `FeedbackTicket`（内容为会话摘要，`meta` 记来源与会话 uuid），
  会话写入 `ticket_id` 并关闭。挂件界面不变，后续消息走现有工单回复接口。
- 对方未登录：在挂件内完成验证码登录（复用现有网页登录接口）。通过后同上，
  并写一条 `widget_verify` 关联。这一步同时消除了"自报邮箱不可信"的问题。

访客始终看到同一个对话气泡界面，区别只在后端存成什么。

### 5.6 两份挂件

- `web/`：一个组件，按品牌注册表取文案。
- `sites/overleap/`：独立实现，不共享代码。

两份都不写品牌字面量，也不写 WebSocket 域名（由 `session` 接口返回）。
用契约测试锁住两份对同一套接口的用法。

注：`docs/ops/web-amplify.md` 记录 overleap.io 目前由 `web/` 的品牌构建提供服务。
第 4 期动手前先确认当时哪个代码库在服务 overleap.io，再决定写哪一份或两份都写。

## 6. 实时通道

### 6.1 连接

- 访客端：`wss://<品牌子域名>/api/chat/ws?token=...`
- 客服端：`wss://<品牌子域名>/app/chat/ws?token=...`（管理后台同样经过 SSR Lambda，也不能同源）

连接令牌：专用的 HMAC 签名短期令牌，有效期 5 分钟，只用于建立连接。
载荷含品牌、角色（访客或客服）、会话或管理员 id。**不复用用户登录 JWT 的 `?token=` 通路**，
避免让 guest 进入用户鉴权逻辑。

**品牌以令牌为准，不看域名。** Center 按请求域名识别品牌，新子域名不在品牌注册表里会被默认成开途；
浏览器 WebSocket 又不能加自定义请求头。品牌写进令牌后，不需要改品牌注册表，也不需要重新生成跨层契约。

WebSocket 地址来自 Center 配置 `chat.ws_hosts.<brand>`，由 `session` 接口下发。

### 6.2 广播

- 消息**先落库，再广播**。
- Redis 订阅做跨实例广播：每个会话一个频道，外加一个客服收件箱频道。
- 断线重连后按消息 id 游标调用 HTTP 拉取接口补齐。广播丢失不丢消息。
- 心跳 25 秒，短于 ALB 与 CloudFront 的空闲超时。

`gorilla/websocket` 目前是间接依赖，改为直接依赖。

### 6.3 接口一览

访客（`/api/chat`，可选鉴权，同源）：

| 接口 | 作用 |
|---|---|
| `POST /session` | 建立或恢复会话，下发令牌 |
| `GET /messages?after=` | 按游标拉取 |
| `POST /messages` | 发消息 |
| `POST /email` | 留邮箱 |

客服（`/app/chat`，`RoleSupport`）：

| 接口 | 作用 |
|---|---|
| `GET /conversations` | 列表，可按状态、品牌、处理方、指派人过滤 |
| `GET /conversations/:uuid`、`/messages?after=` | 详情与消息 |
| `POST /conversations/:uuid/messages` | 回复，同时把 `handler` 置为 `human` |
| `PUT /conversations/:uuid/assign`、`/close` | 指派、关闭 |
| `POST /conversations/:uuid/to-ticket` | 转工单 |
| `GET /ws-token` | 客服连接令牌 |

联系人（`/app/contacts`，`RoleSupport`）：

| 接口 | 作用 |
|---|---|
| `GET /:kind/:id` | 面板数据：标识、关联、"可能是同一人"提示、系统阶段 |
| `GET /:kind/:id/timeline` | 单人事件时间线 |
| `POST /guests/:id/merge`、`POST /guest-merges/:id/undo` | 合并、撤销 |
| `/:kind/:id/notes`、`/:kind/:id/tags` | 备注与标签的增删改查 |
| `GET /todos` | 到期待跟进列表 |

## 7. 客服工作台

### 7.1 收件箱（新页面 `/manager/inbox`）

- 左：会话列表，高优先在前。
- 中：对话。
- 右：这个人的面板。

右侧面板内容：

- **身份与关联**：标识列表、关联到的 user、"可能是同一人"提示，带合并与撤销。
- **系统阶段**：见 §7.2。
- **轨迹**：单人事件时间线。
- **工单**：该 user 的工单列表，可跳转。
- **备注与标签**：时间线、手动标签、跟进日期。

### 7.2 系统阶段

**不落库，读取时现算。**复用 `funnelWalk` 与 `funnelPersonKey`，事实部分从用户、订单表读。

| 主体 | 阶段 |
|---|---|
| guest | 访客 → 看过定价 → 进入结账 |
| user | 未付费 → 付费中 → 已到期 → 已退款 |

漏斗事件保留 120 天，超出窗口的 guest 只有"访客"。

客服手动标签是另一层，存在 `contact_tags`，与系统阶段并列展示。

### 7.3 现有页面的补充

- 用户详情页增加三块：工单、备注与标签、售前记录（关联 guest 的会话）。
- 工单页详情里的用户 ID 改为可点链接。
- 侧边栏"客户支持"组增加"收件箱"，带未读角标。

## 8. Slack

频道沿用 `customer`。

触发：

- 会话转人工时。
- 人工会话有访客新消息，**且没有客服正连着这个会话**时。
- 同一会话 5 分钟内不重复发，除非优先级升高。

内容：品牌、系统阶段、入口页面、首句摘要、直达链接 `{manager.base_url}/manager/inbox?c=<uuid>`。

高优先（消息单独标注）：

- 24 小时内有进入结账的事件但无购买。
- 入口是支付结果页。
- user 已到期。

后台地址收成一个配置项 `manager.base_url`，替换现有两处不一致的硬编码
（`api/logic_approval.go:434`、`api/worker_retailer_followup.go:159`）。

顺带修补工单通知：

- 新工单的 Slack 消息带直达链接。现在发 Slack 在落库之前，拿不到工单号，需调整顺序。
- 用户回复工单时发 Slack。现在完全没有通知。

跟进到期提醒：照 `api/worker_retailer_followup.go` 写一个针对 `contact_notes` 的 worker。

## 9. 权限、隐私、数据生命周期

### 9.1 权限

`support` 权限组新增 `chat`、`contacts`。单人时间线对 `support` 开放；
聚合漏斗看板仍只对 `marketing` 开放。用户写操作（加时长、改邮箱、封禁）仍归超管。

### 9.2 未登录提交工单不再生成 user

现状：`api/api_ticket.go:69` 对未登录提交调用 `FindOrCreateUserByEmail`，
填一个邮箱就凭空生成一个 user，与本设计的 user 定义冲突。

改为：记成 guest（标识为 `did` 与自报邮箱），工单写 `guest_id`，`user_id` 留空。

用户的工单列表查询改为：`user_id` 是自己，**或** 工单邮箱等于自己已验证的邮箱且同品牌。
这样该邮箱日后注册，仍能看到之前的工单。这是安全的：工单回复本来就发往那个邮箱。

**风险：**这是给工单增加了一种"无 user"的状态。实现前必须审计 `FeedbackTicket.UserID`
的全部读取点，以及 `FindOrCreateUserByEmail` 的其他调用方。

### 9.3 隐私

两个品牌的隐私政策各加一段：聊天记录的收集与留存；客服可见浏览轨迹。
现政策对漏斗数据的用途表述是"聚合的转化统计"，单人轨迹展示给客服超出了这个表述，
上线第 3 期前必须落字。

两个文件：`web/public/legal/privacy-policy.md`、`sites/overleap/public/legal/privacy-policy.md`。
它们在 `public/` 下，品牌守卫扫不到，改完要人工确认各自只讲自己的品牌。

### 9.4 留存与删除

- 未关联到任何 user 的 guest：`last_seen_at` 起 180 天后，连同标识、会话、消息、备注一并清除。
- 账号删除：该 user 的会话、备注、标签，以及关联 guest 的全部数据一并删除。
  接入现有两处删除点（`api/api_user.go:286`、`api/logic_approval_callbacks.go:288`）。

### 9.5 防滥用

- 建会话、发消息按 IP 与 `cid` 限流。
- 消息限长，图片限大小与类型。
- 单会话 AI 调用上限（§5.3）。
- 离线邮件只在客服回复时发送，每会话有频率上限，防止被用来向他人邮箱发信。

## 10. 分期

每期一份独立的实现计划。

| 期 | 内容 | 完成标志 |
|---|---|---|
| 1 | 身份与会话表、访客与客服接口、实时通道、AI 迁移、收件箱、Slack、开途官网挂件 | 开途官网新旧并行可用 |
| 2 | 切换并清理 Chatwoot | 仓库零引用；容器停止；ALB 规则与 DNS 清除；旧令牌作废 |
| 3 | 右侧面板全量、系统阶段、备注标签、跟进提醒、用户页补块、工单修正（§9.2）、隐私条款 | 客服能在一处看全一个人 |
| 4 | overleap 官网挂件与隐私条款 | 两个品牌都上线 |

第 2 期不等第 4 期：overleap 现在本来就没有即时沟通。

### 第 2 期清理清单

代码：

- `web/src/components/ChatwootWidget.tsx` 及 `web/src/app/[locale]/layout.tsx` 的挂载。
- `web/src/app/[locale]/support/SupportClient.tsx` 的 `openChat()`。
- `web/src/lib/brands.ts` 的 `chatwootToken` 字段与测试。
- Sentry 过滤：`web/src/lib/sentry-filters.ts`、`web/src/instrumentation-client.ts`、
  `sites/overleap/src/lib/sentry-filters.ts` 及测试。
- `api/api_chatwoot.go`、`api/api_chatwoot_test.go`、`api/route.go` 的 webhook 挂载、
  `qtoolkit/chatwoot` 依赖。
- `webapp/` 的 `features.chatwoot` 开关（无人读取）。webapp 改动需推 `webapp/*` tag 才到达客户端；
  该开关无运行时效果，可随下一次常规发布带出，不单独发。

品牌数据变动（`chatwootToken` 删除）后按规则重新生成跨层契约。

文案与文档：`web/messages/*/guide-parents.json` 的在线客服文案、
`docs/customer-service/14-contact-and-support.md`、`web/CLAUDE.md:279`。

配置与运维：

- 线上与本地的 `chatwoot.*` 配置项。`openai.filesearch.*` **保留**，AI 层仍在用。
- 作废 Chatwoot API 令牌。
- 停止容器组，删除目标组 `tokyo-alb-tg-chatwoot`、对应监听规则、`chat.anc.52j.me` 的 DNS，
  清理其数据库与缓存。
- 历史会话不导出。

## 11. 测试

- **身份合并**：弱证据不传递；合并后会话与备注可读；撤销后恢复原状；链式合并深度恒为 1。
- **双实例广播**：消息从一个实例写入，另一个实例上的连接收到。
- **断线补齐**：广播丢失时，按游标拉取能取回全部消息。
- **品牌隔离**：令牌里的品牌决定连接归属，子域名不影响；一个品牌的令牌读不到另一品牌的会话。
- **转工单**：已登录与挂件内验证两条路径；转后消息进入工单回复。
- **工单修正**：未登录提交不生成 user；该邮箱日后注册能看到工单。
- **两份挂件的接口契约一致。**
- **留存与删除**：到期清理与账号删除都覆盖新表。

api 测试注意：新 worktree 里先确认 `-v` 下 0 SKIP；handler 测试单跑与全量各跑一次。

## 12. 运维前置与待验证项

需要运维操作：

1. 每个品牌一个指向 Center 的子域名，挂到 Center 的 CloudFront 上，ALB 加按域名转发到
   `tokyo-alb-tg-kaitu` 的规则。在此之前挂件靠 HTTP 拉取运行。
2. 线上配置：`chat.ws_hosts.<brand>`、`manager.base_url`。

写实现计划前需验证：

1. Center 的 CloudFront 分发是否透传 WebSocket 握手头。仓库里看不到其源请求策略，需在 AWS 控制台核对。
2. `qtoolkit/redis` 是否暴露订阅接口；没有则直接使用底层客户端。
3. 大陆访客到品牌子域名的长连接稳定性。没有数据，HTTP 拉取是兜底。

需要产品或法务确认：

1. "与客服视频"不再提供（§5.3）。
2. 未关联 guest 的 180 天留存期（§9.4）。
3. 隐私政策的新增表述（§9.3）。

## 13. 不做

- App 内即时沟通。
- 视频通话。
- Chatwoot 历史会话迁移。
- 销售管道、线索评分、自动化营销触达。
- Slack 内直接回复（Slack 只做提醒，回复在工作台）。
- 官网与 App 的匿名身份跨端强行拼接。
- 多客服排班、会话自动分配规则。
