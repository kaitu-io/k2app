# iOS 推送通知（APNs）设计

日期：2026-11-03 · 状态：草案，待 David 批准 · 范围：开途 + Overleap iOS

## 1. 意图

为了提升转化，给 iOS 用户一个能在 App 之外触达他们的渠道：付款失败、会员即将到期这类**账户提醒**，以及召回、优惠这类**营销消息**。APNs 在大陆不用翻墙也能直接送达，是目前唯一能稳定触达国内移动用户的推送渠道；国内安卓的 FCM 基本不通。

成功标准：
- 两个品牌的 iOS 用户都能收到推送，点开后直接跳到对应页面（例如 `/purchase`）。
- 服务端能按 user_id、按用户列表、按 EDM 同款筛选条件发送；发不出去时**明确报告**，不能假装成功。
- 顺利通过 App Store 审核（规则 4.5.4）。

## 2. 目标 / 非目标 / 反目标

**目标**
- 客户端：申请通知权限、注册 token、处理点击跳转、营销同意开关。
- 服务端：真正实现 APNs 发送，区分交易类和营销类，处理失效 token，发送记录可查，同一触发不重复发。
- 自动触发的账户提醒（第 6 节列出的几条）。
- 后台手动群发：API + kaitu-center MCP 工具。

**非目标**
- 本地通知（选了方案 A，见第 3 节）。
- Android 推送（FCM/JPush 以后再说，数据模型已经预留了）。
- 可配置的规则引擎；自动触发写成代码里的固定触发点。
- 后台 web 管理页面（先只做 API + MCP，和 EDM 的用法一致）。
- 「试用结束未转化」的召回，由并行会话 `fix-apple-webhook-hardening-0a` 负责，spec 是 `docs/superpowers/specs/2026-11-03-ios-trial-recall-design.md`。本设计只给它提供发送通道。

**反目标**（技术上能跑通，但算失败）
- 给**开着自动续费的试用用户**发「明天就要扣费了」。这等于提醒他们去取消，直接伤害转化。Apple 不要求做这个提醒，所以不发。
- 用户没同意营销推送，却收到了营销推送（违反审核规则 4.5.4）。
- 发送失败时返回成功。现在的 `sendAPNsPush` 就是这样，必须消灭。
- 推送里提到另一个品牌，或用错了品牌的 topic。
- 推送内容里带敏感信息（邮箱、订单号、金额明细）。

## 3. 方案取舍

| 方案 | 做法 | 代价 | 结论 |
|---|---|---|---|
| **A. 全部走服务端 APNs** | 官方 `@capacitor/push-notifications` 插件；所有提醒都由服务端判断并发送 | 没网或 APNs 延迟时提醒会晚到 | **David 选定** |
| B. A + 本地通知做试用提醒 | 客户端根据 StoreKit 的到期时间再排一条本地通知 | 和推送共用同一个系统权限，多覆盖的情况很少；两条提醒要去重，用户状态变了还要撤销 | 放弃 |
| C. 在 K2Plugin 里原生实现 | 自己写 Swift 处理权限、token、点击 | 要多写原生代码，bridge 契约要加一组方法；官方插件已经够用 | 放弃 |

选 A 的理由：服务端握有准确状态（付费、取消、续费失败、到期），不会出现「已经买了还提醒」的情况；只维护一套逻辑；以后 Android 接 FCM 时能复用同一个插件。

## 4. 约束

- **两个品牌同属一个 Apple 开发者团队** `NJT954Q3RH`，一个 APNs Auth Key（.p8）就能覆盖两个 App。topic 就是 bundle id：开途是 `com.allnationconnect.anc.wgios`，Overleap 是 `io.overleap`（见 `mobile/ios/App/App/Config/brand-*.xcconfig`）。
- **原生部分必须发新版 App，并经过两个品牌各自的 App Store 审核。** webapp 部分走 web OTA（`webapp/x.y.z` tag），会推到还没装新原生的老版本上，所以 webapp 必须先检测插件是否存在（`Capacitor.isPluginAvailable('PushNotifications')`，`capacitor-social-signin.ts:48` 用的就是这个写法）。没有插件时整个功能静默隐藏，**不能报错**。
- 品牌字面量只能写在 `webapp/src/brands/<brand>/index.ts`（根目录 CLAUDE.md 有说明）。推送文案里的品牌名同样遵守：开途用户面对的中文文案一律写「开途」，不写 Kaitu。
- Apple webhook 会重试投递，reconcile 也会重放事件，所以自动触发的推送必须做幂等。
- 注册推送需要登录。试用用户用的是设备账号，也算登录状态（`api/api_push.go` 从 JWT 里取 user 和 UDID）。

## 5. 客户端（webapp + iOS 原生）

### 5.1 原生
- 两个品牌的 `App.entitlements` 都加上 `aps-environment`（release 用 production，dev 用 development），并重新生成 provisioning profile。只有主 App 需要，Network Extension 不需要。
- 安装 `@capacitor/push-notifications`，在 `AppDelegate.swift` 里按插件文档接上 `didRegisterForRemoteNotificationsWithDeviceToken` / `didFailToRegister...`。
- TestFlight 和 App Store 版本用 production 环境；只有 Xcode 直接装的 dev 构建用 sandbox。
- 本次不做通知扩展（Notification Service Extension），也不做富媒体推送。

### 5.2 webapp 推送服务（新文件，例如 `webapp/src/services/push-service.ts`）
对外接口：`isSupported()`、`getPermission()`、`requestPermissionAndRegister()`、`onTap(handler)`。
- **注册**：拿到 token 后调用 `POST /api/push/token`，参数为 `platform=ios`、`provider=apns`、`app_flavor` 沿用现有枚举、`topic=<当前 bundle id>`、`sandbox=<是否 dev 构建>`。bundle id 由原生返回，或者从品牌配置读取，不能在 webapp 里写死。
- **每次启动都重新注册一次**（App 在前台、已登录、已授权时）。token 可能会变；接口本身是 upsert。
- **登出时**调用 `DELETE /api/push/token`。**切换账号时**以最新的注册为准（服务端按 `device_udid + provider` 做 upsert，**会改写 user_id**；构建者需要确认现有代码确实会更新 user_id，没有的话就补上）。
- **点击推送**：读 `data.route`，**只允许跳白名单里的路由**（`/purchase`、`/account`、`/pro-histories`，以及并行会话需要的路由）。不在白名单里的直接忽略，只打日志。如果冷启动时路由还没准备好，先把目标存起来，等首页渲染完成后再跳。

### 5.3 什么时候申请权限
- **时机**：用户**第一次成功连接 VPN 之后**（连接状态第一次变成 connected）申请一次。只弹一次，用持久化标记记住。不在冷启动时弹。
- **形式**：先弹一个 MUI Dialog（不能用 `window.confirm`，Capacitor 的 WebView 会把它吞掉），说明能收到什么：「到期和付款提醒」。弹窗里放一个**默认不勾选**的复选框：「同时接收优惠和活动通知」。用户点「开启」后再触发系统权限弹窗；点「以后再说」就不弹系统弹窗，过 7 天后在下一次连接成功时最多再问一次，之后不再主动问。
  - 理由：系统弹窗一辈子只能弹一次，被拒后只能让用户自己去系统设置里打开。先用自己的弹窗把「会收到什么」讲清楚，能提高同意率；同时把营销同意也放在这个弹窗里，一次满足规则 4.5.4 的「明确同意」，不用再多弹一个窗。
- **设置页**：新增一个「通知」入口。显示系统权限状态，未开启时提供跳转到 iOS 设置的按钮；另有一个营销通知开关，读写服务端字段。

## 6. 服务端

### 6.1 发送通道（`api/logic_push.go`）
- 用 token-based 方式（.p8）实现 `sendAPNsPush`，推荐用 `github.com/sideshow/apns2`。按 token 的 `Sandbox` 字段选 development 还是 production 环境。
- 配置写在 `center/config.yml` 新增的 `apns:` 段：`team_id`、`key_id`、`p8` 文件路径或内容。**配置缺失时**：启动时打 Error 日志，每次调用返回 `ErrPushChannelUnavailable`。**不能**返回 nil。
- **校验 topic**：服务端以用户品牌的 `Brand.Config().Social.AppleBundleID` 作为 topic 写入 token。客户端上报的 topic 不为空且与之不符时，直接拒绝注册（400）。
- **自动识别 sandbox 还是 production（实现时的决定）**：先按 production 发送；如果 APNs 返回 `BadDeviceToken`，再用 sandbox 发一次。sandbox 成功就把 token 的 `sandbox` 改为 true，失败才把 token 标为 inactive。这样 webapp 不需要判断自己是不是 dev 构建（各种签名方式下根本判断不准）。
- **按设备归属过滤（实现时的决定）**：发送时只选 `push_tokens.user_id = devices.user_id AND push_tokens.device_udid = devices.udid` 的 token。设备被踢出、被删除，或已换绑到别的账号后，自动不再收到原账号的推送，不需要在每个删设备的地方单独去停用 token。登出时还会显式停用（设备账号登出时保留了设备行，靠上面的过滤挡不住）。
- **处理 APNs 响应**：`410 Unregistered`、`400 BadDeviceToken`、`DeviceTokenNotForTopic` 这几种，把 token 标记为 `inactive`，不重试。5xx 和超时交给 asynq 重试。`403` 这类证书或 key 错误要**打 Error 并走现有的静默失败告警**（参考 Apple webhook 加固时的告警做法）。
- 消息体：`alert.title` / `alert.body`、`sound=default`；`data.route` 和 `data.push_id` 放在自定义字段里。

### 6.2 对外接口：`PushToUser` / `PushToUsers`
签名改为带分类参数、带返回结果（并行会话的硬性要求）：

```go
type PushCategory string // "transactional" | "marketing"

type PushRequest struct {
    Category  PushCategory
    DedupKey  string            // 同一个 DedupKey 只发一次；手动群发用 batch_id 派生
    Title, Body string          // 调用方已按用户语言本地化
    Route     string            // 可选，必须在白名单内
}

// 同步完成资格判断后入队，真正投递是异步的。
// 返回 ErrPushChannelUnavailable / ErrPushNoActiveToken / ErrPushNotConsented / ErrPushDuplicate，
// 或者 nil，表示「已入队，至少有 1 个有效 token」。
func PushToUser(ctx context.Context, userID uint64, req PushRequest) error
```

- **必须如实说明**：nil 只保证「资格判断通过、已入队」，不保证送达。异步阶段 token 全部失效时，调用方同步拿不到这个结果，只能从发送记录里查。调用方需要的「回退到邮件」只能基于这几种同步错误来做。这一点要写进函数注释。
- **跟并行会话的分工（工程层面的约定，2026-11-03）：** 本设计负责改 `PushToUser` 的签名、错误类型和路由白名单，并且先合并。召回会话只调用 `PushToUser`，固定传 `category=marketing`，`route` 用 `/purchase`；遇到同步返回的错误就改发邮件。召回的判定逻辑、文案和合并都由对方负责。对方的 spec 还没写，接口以本节为准。
- `PushToUsers` 返回每个用户的结果（例如 `map[userID]error` 或结果切片）。
- 营销类：用户的 `push_marketing_opt_in` 必须为 true，否则返回 `ErrPushNotConsented`。

### 6.3 数据
- `users` 表新增 `push_marketing_opt_in bool default false`、`push_marketing_opt_in_at`。新增接口 `GET/PUT /api/push/preferences`。开关**按用户**存，不按设备。
- 新增 `push_send_logs` 表：`dedup_key` 唯一、`user_id`、`category`、`route`、`status`（queued/sent/failed/no_token）、`sent_count`、`fail_count`、`batch_id`、时间戳。用途有两个：一是幂等（Apple webhook 重放时靠唯一约束挡掉重复），二是事后能查「发给谁了、成功没有」。这一点推翻了 `logic_push.go` 开头注释里「不记录推送日志」的原则，原因是幂等和可观测性都需要它。改完同步更新那段注释。

### 6.4 自动触发的账户提醒（交易类，本设计负责）
| 触发 | 来源 | 对象 | 跳转 |
|---|---|---|---|
| 续费扣款失败（**包括**试用结束后的第一笔扣款失败） | Apple webhook `DID_FAIL_TO_RENEW` | 该订阅的用户 | `/purchase`（或者 `/account`，由构建者看哪个页面能处理更新付款） |
| 会员 3 天后到期 | 现有 `worker_renewal_reminder.go`，`daysBefore=3` 分支 | 和同一天的提醒邮件**完全相同的候选人**（`lifecycleEmailCandidates` 并去掉 `usersWithLiveAutoRenew`） | `/purchase` |
| 会员已过期 1 天 | 同上，`-1` 分支 | 同上 | `/purchase` |

- **试用结束后第一笔扣款失败归本设计（我的判断）。** 理由：这类用户本来就想转付费，只是付款失败；「请更新付款方式」是交易类提醒，不需要营销同意。并行会话的召回需要排除处于 billing_retry 状态的订阅，避免同一个用户收到两条推送（已经同步给对方）。
- **到期提醒挂在现有的邮件 worker 上，推送是额外加的一条，不替代邮件（我的判断）。** 每天 02:30 UTC 跑，也就是北京时间 10:30。候选人和邮件共用同一套筛选，从没付过费的试用到期用户本来就不在里面，那部分归并行会话。只在 `3` 和 `-1` 这两个节点额外发推送；`30/14/7` 离到期太远，`-7/-30` 是带折扣码的 winback，属于营销类，本次不加推送。
- DedupKey 示例：`billing_retry:<sub_id>:<period_end>`、`renewal_3:<user_id>:<expired_at>`。
- 文案：按品牌和 `users.language` 写在 Go 代码的模板表里，覆盖 zh-CN / zh-TW / en-US / ja，缺语言时按品牌的默认语言兜底。不带金额、邮箱、订单号。

### 6.5 后台手动群发（营销类）
- `POST /app/admin/push/send`：参数为显式的 `userIds` 列表（**更正**：`api/type.go` 里的 `UserFilter` 是没有任何调用方的死类型，EDM 实际做法是调用方给出明确的收件人列表，这里照搬 EDM），外加 `brand`、按语言给出的 `{title, body}`、`route`、`batch_id`（用来做幂等）、`dry_run`。**接口没有 `category` 参数，固定按 marketing 发（我的判断）。** 这样管理员没法把营销内容伪装成交易类，绕过用户的营销同意。交易类推送只能由代码里的触发点发出。
  - `dry_run=true` 时返回：命中多少用户、其中有有效 token 的多少、其中已同意营销的多少。真正发送时逐个调用 `PushToUser`，按结果汇总。
- 真正发送要走现有的审批流 `SubmitApproval(c, "push_send", ...)`，和 EDM 的 `edm_send` 一样：超管直接执行，其他角色进审批。`dry_run` 不需要审批。`userIds` 里每个用户都必须属于 `brand`，不属于的计入 skipped。
- `GET /app/admin/push/batches/:batch_id`：返回 `push_send_logs` 的汇总。
- kaitu-center MCP 新增工具 `send_push`（先 dry run，确认后再真正发送）和 `get_push_batch_stats`，写法参考现有的 EDM 工具。

## 7. 上线顺序
1. David 在 Apple Developer 创建 APNs Auth Key，给两个 App ID 开启 Push Notifications 能力，把 key 放进两台 center 的 config。
2. 部署服务端（向后兼容，没有客户端注册时什么都不会发）。
3. 发布带 entitlement 和插件的原生新版本（两个品牌各自提审）。审核备注里说明：推送用于账户提醒，营销推送需要用户在 App 内单独同意，并且可以在设置里关闭。
4. 发 webapp OTA。老版本原生检测不到插件，功能自动隐藏。
5. 真机验证：用 labtest 租一台 iPhone，dev 构建走 sandbox，跑通注册 → 后台 `send_push` 给自己 → 点击跳转 `/purchase` 的全链路。生产环境上线后，再用 David 自己的账号做一次 production 推送验证。

## 8. 测试要求（关键的几条）
- 用 `PushToUser` 分别构造无 token、没同意营销、通道未配置、DedupKey 重复四种情况，断言**各自返回对应的错误**。还要做一次变异验证：把 `sendAPNsPush` 改回 `return nil`，相关测试必须变红。
- APNs 客户端抽成接口；用假实现模拟 410 / BadDeviceToken / 5xx，断言 token 状态变化和是否重试。
- topic 和品牌不匹配时，注册被拒绝。
- webapp：插件不可用时整个功能隐藏、不报错；不在白名单里的路由被忽略；冷启动时点击的跳转在路由就绪后才执行。
- 修改 api 之后，按 api/CLAUDE.md 的说明重新生成 `contracts/api-contract.json`。

## 9. 留给构建者
- 续费失败跳 `/purchase` 还是 `/account`（看哪个页面能引导用户去 Apple 更新付款方式）。
- `push_send_logs` 的保留期和清理方式。
- `app_flavor` 在 iOS 上填什么值（现有枚举只有 china / google_play，iOS 的校验并不看它）。
- 文案的具体措辞（交给 David review）。

## 10. 可选项（不在本次范围内，由 David 决定要不要加）
- 营销推送频率上限（例如每个用户 3 天最多 1 条）。手动群发和并行会话的召回可能会叠加在同一个用户身上。
- 后台 web 页面版的推送群发界面。
