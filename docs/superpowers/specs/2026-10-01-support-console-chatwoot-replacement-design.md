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
- 每个会话在 Slack 里是一个独立频道，谁发的消息都按时序出现在里面；客服直接在频道里发言即回复访客。
- 从 Slack 点链接进管理后台，能看到这个人的身份、阶段、轨迹、工单、备注。
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

- **guest**：还没有账号的人。数据上只来自官网挂件（App 内不做即时沟通，见 §2.3 与 §9.2）。
- **user**：有账号的人（`users` 表有行）。注册未付费的人是 user，阶段标为"未付费"。

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

- 会话与消息存储、访客挂件、Slack 频道镜像与回复。
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
| `kind` | `cid`（聊天 cookie）、`sid`（漏斗 cookie）、`email` |
| `value` | |
| `strength` | `verified`（服务端从 cookie 读到）或 `claimed`（对方自报） |
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
| guest 的 `sid` 在 `funnel_identities` 里关联到了 user | 强 | **不复制**，读取时联查，单一事实源 |
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
| `slack_channel_id`、`slack_card_ts`、`slack_lobby_ts` | 该会话的专属 Slack 频道、频道内状态卡、总览频道那一行 |
| `closed_at` | |

会话在访客发出第一条消息时才创建，只点开挂件不建会话（避免 Slack 里出现空频道）。
实时频道按主体而不是按会话划分，所以挂件一打开就能连上。

一个主体在一个品牌下最多一个 `open` 会话。客服手动关闭，或无消息自动关闭（AI 接待 24 小时，客服介入过 72 小时）；
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
| `slack_ts` | 来自 Slack 的消息记其时间戳，用于去重；唯一索引 |
| `kind` | `text`、`image`、`options`、`option_reply`、`event` |
| `content`、`meta` | `meta` 为 JSON |

支持图片消息。现有欢迎语明确引导访客发截图，支付失败截图也是人工介入的主要依据。

实现（2026-10-04）：**不复用**日志桶 `kaitu-service-logs`——它对所有人公开读写，截图里可能有支付与邮箱信息。
新建私有桶 `kaitu-chat-images`（屏蔽公开访问，`chat/` 前缀 180 天过期），配置键 `chat.images.bucket`，
未配置时图片功能关闭（session 下发 `images: false`，挂件不画按钮）。
- 上传 `POST /api/chat/images`（multipart：`file` + `clientId`），经 Center 写桶；按内容嗅探只收
  png / jpeg / gif / webp，上限 5 MB，限流与发文字相同。消息 `kind=image`，`content` 存对象 key。
- 查看 `GET /api/chat/images/<令牌>`：令牌（用途 `chat-image-v1`，只绑消息 id 与过期）校验后 302 到 5 分钟的
  S3 下载地址。访客与后台拿站内相对路径（24 小时），Slack 频道拿挂在品牌官网域名下的绝对链接（30 天）。
- AI 直接拿 15 分钟的 S3 下载地址（OpenAI 自己下载），历史里的截图也带上。
- 客服从 Slack 发图给访客仍不支持。

### 4.3 备注与标签

`contact_notes`：`subject_kind`、`subject_id`、`content`、`follow_up_at`、`is_completed`、
`operator_id`、`assignee_id`、`slack_notified`，软删除。形状同 `RetailerNote`。

`contact_tags`：`subject_kind`、`subject_id`、`tag`、`created_by`。唯一索引 `(subject_kind, subject_id, tag)`。

不复用 `retailer_notes`：它的权限、待办查询和语义都绑定分销商，混用会把客服备注掺进分销待办。

不做销售管道、线索评分。零售量级用不上。

### 4.4 对现有表的改动

- `funnel_events` 增加索引 `(anon_id, occurred_at)`。

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
- 客服端不建 WebSocket：客服在 Slack 里收发（§8），实时性由 Slack 提供。

连接令牌：专用的 HMAC 签名短期令牌，有效期 5 分钟，只用于建立连接。
载荷含品牌、主体、会话 id。**不复用用户登录 JWT 的 `?token=` 通路**，
避免让 guest 进入用户鉴权逻辑。

**品牌以令牌为准，不看域名。** Center 按请求域名识别品牌，新子域名不在品牌注册表里会被默认成开途；
浏览器 WebSocket 又不能加自定义请求头。品牌写进令牌后，不需要改品牌注册表，也不需要重新生成跨层契约。

WebSocket 地址来自 Center 配置 `chat.ws_urls.<brand>`，由 `session` 接口下发。

### 6.2 广播

- 消息**先落库，再广播**。
- Redis 订阅做跨实例广播：每个会话一个频道。Slack 回调可能落在任一实例，靠它送达访客所在的实例。
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

客服（`/app/chat`，`RoleSupport`，只读为主，供管理后台查看历史）：

| 接口 | 作用 |
|---|---|
| `GET /conversations` | 列表，可按状态、品牌、处理方过滤 |
| `GET /conversations/:uuid`、`/messages?after=` | 详情与消息 |
| `PUT /conversations/:uuid/close` | 关闭 |
| `POST /conversations/:uuid/to-ticket` | 转工单 |

Slack 回调（公开，签名校验）：`POST /webhook/slack/events`。

联系人（`/app/contacts`，`RoleSupport`）：

| 接口 | 作用 |
|---|---|
| `GET /:kind/:id` | 面板数据：标识、关联、"可能是同一人"提示、系统阶段 |
| `GET /:kind/:id/timeline` | 单人事件时间线 |
| `POST /guests/:id/merge`、`POST /guest-merges/:id/undo` | 合并、撤销 |
| `/:kind/:id/notes`、`/:kind/:id/tags` | 备注与标签的增删改查 |
| `GET /todos` | 到期待跟进列表 |

## 7. 客服工作台

### 7.1 会话页（新页面 `/manager/conversations`）

实时收发在 Slack 里完成（§8），管理后台不做实时收件箱。这个页面的职责是**看全一个人**：

- 左：会话列表（历史可查，Slack 免费版只保留 90 天，数据库才是事实源）。
- 中：对话记录，只读。
- 右：这个人的面板。

会话频道的状态卡带链接 `{manager.base_url}/manager/conversations?c=<uuid>`，一点直达。

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
- 侧边栏"客户支持"组增加"会话记录"。

## 8. Slack：会话的主界面

客服的收发都在 Slack 里完成，不需要打开后台。选 Slack 而不是邮件或新工具的理由：
团队已经在用；手机推送现成；Center 已有 bot 令牌的调用先例（审批通知）。

工作方式是**人全程监督 AI**：AI 先答，客服实时看着，答得不够就直接接手。
所以每一条消息、不论谁发的，都必须按真实时序出现在 Slack 里。

### 8.1 一次会话一个 Slack 频道

- 访客发出第一条消息时，bot 新建一个私有频道（如 `chat-1001-3fa9c2`），把客服全部拉进来。
- 这次会话的**每一条消息都作为普通消息发进这个频道**，顺序与真实对话一致：访客的、AI 的、系统事件。
  不去抖、不合并。
- 频道主题写品牌、邮箱、入口页面。频道内置顶一张状态卡。
- 会话关闭时归档频道。

客服名单 = **总览频道的成员**。总览频道由配置 `slack.chat_lobby_channel_id` 指定，
每个新会话在里面留一行带链接的记录，归档后也能从这里找回。两个品牌共用，记录里标明品牌。
不用现有的 `customer` 频道，那里留给工单提醒。

### 8.2 什么时候响

频道里的普通消息只让频道显示未读，不强提醒。AI 主动转人工时，bot 在频道里 @所有人，这是唯一的强提醒。

状态卡显示：AI 接待中 / **等待人工** / 已回复待访客 / 已关闭。入口是支付结果页的会话加醒目标记。

### 8.3 在 Slack 里回复

- 客服在会话频道里直接发言，Slack 事件回调到 `POST /webhook/slack/events`。
- Center 按频道找到会话，把内容写成一条客服消息，经 Redis 广播推给访客。
- 会话的 `handler` 同时置为 `human`，AI 不再接话。此刻若 AI 的回复还在生成，那条回复被丢弃。
- 客服身份：用 Slack 账号的邮箱匹配管理员账号，要求是超管或带 `support` 角色。
  匹配不上的发言不转发，并在频道里提示。
- 以 `!` 开头的消息永不发给访客：`!ai` 把会话交还 AI，`!close` 关闭会话，其余是内部备注。
  不用 `/` 前缀，Slack 输入框会把它当斜杠命令拦下。
- 频道内的**线程回复不发给访客**，留给客服之间讨论。

回调处理的硬要求：

- 校验 Slack 签名与时间戳，拒绝超过 5 分钟的请求。
- 3 秒内应答，处理放到后台。
- 按 `slack_ts` 去重（Slack 会重试）。
- 忽略 bot 自己发的消息，否则镜像会自激成环。

### 8.4 发送侧

- 每个会话的消息串行发送以保证顺序；遇到限流按 Slack 返回的等待时间重试。
- 建频道分几步（建、拉人、设主题、发状态卡），任一步失败都能从断点续做，不会重复建频道。
- **Slack 不可用不影响聊天**：消息已落库并已推给访客，镜像失败只记日志并重试，访客无感。

### 8.5 索引与历史

- **正在进行的会话**：Slack 侧栏。有未读的频道就是要看的，归档即处理完。
- **回头客**：状态卡里有"此前会话"一行，列出这个人之前的会话数和最近一次的链接。
- **全量历史**：管理后台的会话列表，可按状态、品牌、邮箱、时间筛选，每行带"在 Slack 打开"。
  Slack 免费版只保留 90 天，数据库才是事实源。

已知代价：

- 每个发过消息的访客都产生一个频道，归档的频道用普通权限删不掉，会持续累积。
- 为了不让侧栏堆积，AI 接待且无人介入的会话 24 小时无消息自动关闭；客服介入过的 72 小时。

### 8.6 配置与顺带修补

后台地址收成一个配置项 `manager.base_url`，替换现有两处不一致的硬编码
（`api/logic_approval.go:434`、`api/worker_retailer_followup.go:159`）。

工单通知（仍走 `customer` 频道）：

- 新工单的 Slack 消息带直达链接。现在发 Slack 在落库之前，拿不到工单号，需调整顺序。
- 用户回复工单时发 Slack。现在完全没有通知。

跟进到期提醒：照 `api/worker_retailer_followup.go` 写一个针对 `contact_notes` 的 worker。

## 9. 权限、隐私、数据生命周期

### 9.1 权限

`support` 权限组新增 `chat`、`contacts`。单人时间线对 `support` 开放；
聚合漏斗看板仍只对 `marketing` 开放。用户写操作（加时长、改邮箱、封禁）仍归超管。

### 9.2 App 内未登录提交工单：保持现状

已核对：`webapp/src/pages/SubmitTicket.tsx:158-168` 未登录提交时邮箱必填并校验格式，
后端 `api/api_ticket.go:69` 按该邮箱找到或创建 user。

这与现有账号模型一致：发送验证码时（`api/api_auth.go:119`）就已经按邮箱建 user 行，
并不等到验证通过。所以"留了邮箱的工单提交者是 user"不是特例，本设计不改它。

结论：App 内不产生 guest 记录。guest 只来自官网挂件。

### 9.3 隐私

两个品牌的隐私政策各加一段：聊天记录的收集与留存；客服可见浏览轨迹；聊天内容会经由 Slack 处理。
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
| 1 | 身份与会话表、访客接口、访客实时通道、AI 迁移、每会话一个 Slack 频道（全量镜像与频道内回复）、离线邮件、后台会话列表（只读）、开途官网挂件（文字消息） | 开途官网新旧并行可用，客服全程在 Slack 里接待 |
| 1b | 图片消息（需先定存储桶） | 访客可发截图，AI 与 Slack 频道可见 |
| 2 | 切换并清理 Chatwoot | 仓库零引用；容器停止；ALB 规则与 DNS 清除；旧令牌作废 |
| 3 | 转工单与挂件内验证、会话页的右侧面板、系统阶段、备注标签、跟进提醒、用户页补块、隐私条款 | 客服能在一处看全一个人 |
| 4 | overleap 官网挂件与隐私条款 | 两个品牌都上线 |

第 2 期不等第 4 期：overleap 现在本来就没有即时沟通。
第 2 期要等 1b：现有欢迎语引导访客发截图，没有图片能力就切换是功能倒退。

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

#### 2026-10-04 实际执行情况

- 代码清理全部完成（`d04e559d`），生产配置的 `chatwoot:` 段已删。挂件改为探测后对所有访客显示（R26）。
- **运维清理不执行**：Chatwoot 实例 `chat.anc.52j.me` 不是开途独占，还服务 ANCBank、x.ancbank.com、
  开路者（waymkr.app）、ANC 四合会议（allnationconnect.com）与一个邮件渠道；ALB `tokyo-alb` 80 端口的
  默认规则也指向它的目标组。停容器、删目标组 / DNS、作废令牌都会影响这些业务。开途在 Chatwoot 里的
  收件箱是 5「K2」和 10「K2 Support」，停用与否由运营决定。

## 11. 测试

- **身份合并**：弱证据不传递；合并后会话与备注可读；撤销后恢复原状；链式合并深度恒为 1。
- **双实例广播**：消息从一个实例写入，另一个实例上的连接收到。
- **断线补齐**：广播丢失时，按游标拉取能取回全部消息。
- **品牌隔离**：令牌里的品牌决定连接归属，子域名不影响；一个品牌的令牌读不到另一品牌的会话。
- **转工单**：已登录与挂件内验证两条路径；转后消息进入工单回复。
- **Slack 镜像**：每条消息都进会话频道且顺序正确；并发下不重复建频道；建频道中途失败可续做；Slack 失败不影响访客收发。
- **Slack 回复**：签名错误拒绝；重复事件只落一条；bot 自己的消息被忽略；非客服账号的回复不转发；`!` 开头的不发给访客。
- **两份挂件的接口契约一致。**
- **留存与删除**：到期清理与账号删除都覆盖新表。

api 测试注意：新 worktree 里先确认 `-v` 下 0 SKIP；handler 测试单跑与全量各跑一次。

## 12. 运维前置与已核实项

### 12.1 已核实（2026-10-01）

- **CloudFront → ALB 的 WebSocket 可用。** 对 `chat.anc.52j.me/cable` 发握手请求，经 CloudFront 返回
  `101 Switching Protocols`。Chatwoot 的分发（`E27IOJJHN8ZT8C`）与 Center 的分发（`E3R9YV4KNF3Q5D`）
  转发配置完全一致：同一 ALB 源站、回源 `http-only`、透传 `Host` / `Authorization` / `Accept`、
  cookie 与查询串全透传、读超时 60 秒。新通道照这个配置建即可。
- **Redis 支持订阅**（ElastiCache Redis 7.1）。
- **长连接可用性**：Chatwoot 现在就是经这条链路用 WebSocket 服务同一批访客，不另做验证。
  HTTP 游标拉取仍保留，用于断线重连补齐。

由配置得出的两条约束：

- 分发**不透传 `Origin` 头**，Center 不能靠 `Origin` 校验 WebSocket 来源。连接合法性只靠令牌（§6.1）。
- 分发**透传 `Host`**，所以 Center 会看到新子域名；品牌以令牌为准这条规则是必需的。

### 12.2 需要运维操作

1. 每个品牌一个子域名：新建 CloudFront 分发（配置照抄 `E3R9YV4KNF3Q5D`），us-east-1 的 ACM 证书，
   Route 53 记录，ALB 增加按该域名转发到 `tokyo-alb-tg-kaitu` 的规则。
   在此之前挂件靠 HTTP 拉取运行。
2. 线上配置：`chat.ws_urls.<brand>`、`manager.base_url`、`slack.bot_token`、`slack.signing_secret`、`slack.chat_lobby_channel_id`。
3. Slack 应用：开启事件订阅（私有频道消息）并把回调地址指向 Center；权限含发消息、读私有频道消息、
   建与归档私有频道、拉人进频道、列频道成员、置顶、按用户查邮箱；新建总览频道，把 bot 和全部客服拉进去。生产环境是否已配 bot 令牌未核实（本地配置里没有）。

### 12.3 封装库 qtoolkit 的前置改动

仓库在 `~/projects/wordgate/qtoolkit`，独立发版。第 1 期开工前先改两个模块并打 tag，再在 `api/go.mod` 升级。

`redis` 模块的 `Broadcast`（Redis 订阅加 WebSocket 推送，现成可用，但有三处缺陷）：

- 向订阅者投递用无缓冲通道，**一个连接写得慢会卡住全部频道的广播**。改为每个订阅者带缓冲队列，
  满了就关闭该连接，由客户端重连后按游标补齐。
- Redis 的发布订阅不分库，而它的订阅键固定是 `broadcast`。同一个 Redis 上的其他服务（`meet` 在用）
  会和 Center 互相收到对方的消息。增加带命名空间的构造函数，旧构造函数行为不变。
- 不读取客户端帧，探测不到对端关闭。补一个读循环。

接口保持向后兼容，`meet` 不升级就不受影响，升级后同样受益。

`slack` 模块目前只有 webhook 和私信。新增通用能力：发消息并返回时间戳（支持线程与广播到频道）、
改写消息、取永久链接、置顶、校验事件签名、按 Slack 用户 id 取邮箱；
以及频道管理：建频道、拉人、归档、列成员、设主题。

会话逻辑、连接令牌、游标补齐是业务代码，留在 Center。

### 12.4 需要产品或法务确认

1. "与客服视频"不再提供（§5.3）。
2. 未关联 guest 的 180 天留存期（§9.4）。
3. 隐私政策的新增表述（§9.3）。

## 13. 不做

- App 内即时沟通。
- 视频通话。
- Chatwoot 历史会话迁移。
- 销售管道、线索评分、自动化营销触达。
- 管理后台内的实时收件箱（实时收发在 Slack 里）。
- 客服从 Slack 发图片给访客（第 1 期只支持文字回复）。
- 官网与 App 的匿名身份跨端强行拼接。
- 多客服排班、会话自动分配规则。
