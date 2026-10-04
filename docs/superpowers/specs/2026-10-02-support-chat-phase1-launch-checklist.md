# 自建客服会话 第 1 期：上线清单与已知限制

配套设计：`2026-10-01-support-console-chatwoot-replacement-design.md`。本文记录第 1 期（分支 `feat/support-chat-p1`）合并后、打开开关前必须做的事，以及验证没有覆盖到的部分。

## 验证状态

- api 全量测试、web 全量测试、类型检查：通过。
- 真实浏览器链路（Playwright + 本地 Center + MySQL）：访客发消息、AI 应答、实时推送、断线补齐、轮询降级、后台列表与关闭、邮件回链，均通过。
- **未对真实 Slack 联调**：Slack 一侧（建频道、镜像、事件回调、归档）只经假服务验证。
- **未验证**：两个定时任务在真实 worker 里的运行；生产 Amplify 下服务端 Sentry 擦除是否生效；Cookie 横幅与挂件在手机视口的布局；AI 是否遵守"纯文本、不写英文品牌名"的提示。

真实 Slack 联调通过前，不要把 `chat.enabled` 打开。

## 配置

| 键 | 默认 | 说明 |
|---|---|---|
| `chat.enabled` | `false` | 总开关 |
| `chat.preview_enabled` | `false` | 总开关关闭时是否放行 `?chat=preview` 与邮件回链。两者都为 false 即硬关 |
| `chat.brands` | 空 | 开放的品牌列表。空 = 全部关闭，必须显式配 `[kaitu]` |
| `chat.ws_urls.<brand>` | 空 | 访客 WebSocket 地址，须为 `wss://`，主机在该品牌 Hosts 内。空或配错 = 静默降级轮询 |
| `slack.bot_token` / `slack.signing_secret` | — | 缺 signing_secret 时客服在频道里的回复到不了访客 |
| `slack.chat_lobby_channel_id` | 空 | 总览频道。空 = Slack 镜像整体关闭 |
| `manager.base_url` | 空 | 状态卡里的后台链接；不配则不渲染 |

开关或预览打开而上述 Slack 键缺失、或 `chat.brands` 为空时，启动日志有一条 Error（不阻止启动）。

## Slack 应用

- Bot 权限：建私有频道、邀请成员、发消息、置顶、归档、`users:read.email`。
- 事件订阅：**只订阅 `message.groups`**，回调地址 `/webhook/slack/events`。订阅了 `channel` 字段为对象的事件类型会被回 400 并反复重投。
- 客服身份：Slack 账号邮箱必须等于带 `RoleSupport` 或管理员的后台账号邮箱，否则发言被拒并在频道提示。
- 频道内用法：主时间线发言 = 回复访客；线程内 = 内部备注；`!ai` 交还 AI；`!close` 关闭。编辑、删除不同步给访客。

## 基础设施

- 每品牌一个 WebSocket 子域名（CloudFront + ACM + ALB），空闲超时不低于 60 秒。
- 数据库：6 张新表由 AutoMigrate 创建。已经建过这些表的开发库需手工 `DROP INDEX idx_conversation_messages_conversation_id`、`idx_conversation_messages_slack_ts`（生产是新建表，无需执行）。

## 正式放量前必须做

1. ~~**入口探测**~~ 已完成（2026-10-04）：`GET /api/chat/enabled` 只读开关、不建访客；挂件按探测结果先画入口，访客第一次展开面板才建会话（`gate.ts` 的 `probeChatEnabled` / `shouldStartSession`，`ChatWidget` 的 `deferStart`）。kaitu.io 的挂件随之改为全站挂载，替换 Chatwoot。
2. **重估限流**：新建会话每实例 30/分钟、新建访客 600/分钟等为常量。每个无 cookie 的 session 请求都会落一行访客记录（入口探测上线后只在访客展开面板时才发 session）。
3. **真实浏览器看一眼** Cookie 横幅与挂件的相对位置（桌面与手机）。
4. **服务端 Sentry**：本期只擦除了 `/api/chat/` 路由。`web/src/instrumentation.ts` 的 `sendDefaultPii` 加全量采样对其它 `/api/*` 代理请求的请求体与 cookie 仍然生效，属既有配置，需单独评估。

## 已知限制

- 生产双实例下，客服在同一频道连发两条，若事件落在不同实例，访客侧可能乱序。
- Slack 故障超过 24 小时的消息不再补发到频道（后台仍可见）。
- 发版时在途的 AI 回复会丢，没有"AI 未答"看门狗；访客再发一条即恢复。
- 欢迎语、自动关闭文案、AI 提示词只有简体中文的开途版本；Overleap 进白名单前要品牌化。
- 游客登录后若会话被关闭且不刷新页面，回复要等重连才出现。
- 访客侧超过 200 条消息的会话，中间消息需刷新才补齐。
- 图片消息、转工单、挂件内登录、删除 Chatwoot 不在本期。
