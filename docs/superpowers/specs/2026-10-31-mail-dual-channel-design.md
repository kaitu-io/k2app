# 系统邮件双通道与失败报警

## 为什么做

2026-10-30 18:16（北京时间）起，阿里云 DirectMail 账号余额为负，所有系统邮件报 551，登录验证码接口返回 500。线上事故没有任何报警：发送失败只写日志。

单通道有两类盲区：账号级故障（欠费、封禁）让全部发信失败；单封邮件“已接收但没送达”时，发送方不报错。

## 目标

1. 任一通道发送失败，立即用另一通道重发，并发 Slack 报警。
2. 验证码第一次走通道一，重发走通道二。
3. 发件人按品牌隔离：开途用户永远收到开途身份的邮件，Overleap 同理，任何回落都不跨品牌。

## 非目标

- 不改 qtoolkit：`mail.Config(prefix)` 已支持多配置。
- EDM 不切通道二：免费额度每天 200 封，且群发会污染事务通道信誉。EDM 只加失败报警。
- 不做投递回执或退信分析。

## 反目标（做了等于失败）

- Overleap 用户收到开途或 allnationconnect 身份的邮件。
- 一次故障刷出大量 Slack 消息。
- 收件地址无效这类单封错误触发报警。

## 现状与发现

- 系统邮件入口：`MailSendWith`（`api/logic_email.go`），`sendSystemEmailAs` 和工单回复都走它。
- 验证码入口：`api_auth.go:155`、`api_user.go:245` 的 `emailTo`。
- **线上没有 `mail_overleap` 配置**，Overleap 验证码目前从 `joshua@mail.allnationconnect.com` 发出，本身就违反品牌隔离。

## 方案

### 考虑过的方式

| 方式 | 取舍 | 结论 |
|---|---|---|
| A. 第二个阿里云国际账号 | 独立账户，欠费互不影响；同一服务商，平台级故障时一起坏 | **采用，账号已开通** |
| B. AWS SES | 服务商独立，但全区沙盒，转正未获批 | 放弃 |
| C. 第三方 SMTP | 服务商独立，需新注册和域名配置 | 以后可替换通道二，配置兼容 |

### 通道配置

| 品牌 | 通道一 | 通道二 |
|---|---|---|
| 开途 | `mail`：`开途 <noreply@notify.kaitu.io>`（账号 5158；旧地址 `joshua@mail.allnationconnect.com` 先并行，切换时间由 David 确认） | `mail_backup`：`开途 <noreply@kaitu.io>`（账号 anc-intl） |
| Overleap | `mail_overleap`：`Overleap <noreply@notify.overleap.io>`（账号 5158，**新增线上配置**） | `mail_backup_overleap`：`Overleap <noreply@overleap.io>`（账号 anc-intl） |

EDM 独立域名：`开途 <hello@service.kaitu.io>`、`Overleap <hello@service.overleap.io>`，账号 5158，地址类型 trigger。不用 `news` 这类营销命名。真实模板内容实测（2026-10-31，Gmail）trigger 地址进收件箱主标签，含优惠码的一封也是；同内容的 batch 地址没有进收件箱。EDM 同样设 `Reply-To`，因为模板普遍让用户直接回复。`edm_overleap.send_from` 为空时 Overleap EDM 拒发并报警。

通道配置按 viper 前缀读取，`send_from` 缺失视为通道未配置。

### 发送逻辑

`sendMailWithFailover(ctx, brand, msg)` 是系统邮件的唯一出口，替代原 `MailSend` / `MailSendWith`：

1. 默认通道一在前；`mailContextForCode` 写入 context 后通道二在前。
2. 第一个通道成功：结束。
3. 失败：记日志，用第二个通道重发，并报警。
4. 两个都失败：返回错误，发更高级别的报警（内容注明“两个通道都失败”）。
5. 某品牌某通道未配置：跳过该通道并报警，**绝不借用其他品牌的通道**。删除现有的“Overleap 回落全局发件人”逻辑。

`dev_mode` 行为不变：只打印日志，不触碰任何通道，也不碰 Redis。

单次发送上限 20 秒：gomail 连上后没有读写超时，挂起的通道靠这个上限才能切走。收件地址语法错误在发送前拦下，不切换、不报警。错域名和不存在的邮箱阿里云都会接受，异步退信，不走这里。

### 重发换通道

验证码发送前对 Redis 键 `mail:code_send:<brand>:<邮箱 SHA-256>` 执行 INCR，TTL 等于验证码有效期：奇数次通道一在前，偶数次通道二在前。Redis 出错时走默认顺序，不阻断发信。Key 里不存明文邮箱。

其他通知没有“重发”，一律通道一在前。

### 报警

- 发到 Slack `alert` 频道（`slack.Send("alert", ...)`）。
- 内容：品牌、失败通道、错误摘要、是否已由另一通道发出；不含收件人明文和邮件正文。
- 限频：同一品牌、同一通道 10 分钟一条（Redis SETNX）。限频键写失败时仍发送报警，宁可多发。
- Slack 发送失败只写日志，不影响发信。
- 收件地址语法错误、明确的单收件人拒收不报警，也不切换；账号级错误（包括 551）必须切换并报警。无法判断的错误按账号级处理。
- EDM（`sendEmail`）失败时走同一套报警，不切换通道。

### 送达率

测试结果（2026-10-31，Gmail）：两个通道二地址的 SPF、DKIM、DMARC 都 pass。无显示名的测试文案有一封进了垃圾箱，带显示名、真实文案和 `Reply-To` 的两封都进收件箱。

- `send_from` 必须带品牌显示名。**中文显示名必须写成 RFC 2047 形式**：`"=?UTF-8?B?5byA6YCU?= <noreply@notify.kaitu.io>"`（「开途」）。写成 `开途 <...>` 时 gomail 把地址一起编码，所有邮件报 `invalid address`（2026-10-31 实测）。代码把这种配置判为通道不可用并报警。
- 系统邮件在邮件头设 `Reply-To`（取 `BrandConfig` 的客服邮箱）。不能配在阿里云发件地址上，那里未验证会导致发件报错。
- 阿里云会补 `Message-ID`，qtoolkit 不用处理。
- DKIM 为 1024 位，阿里云 CLI 没有提供修改接口，保持现状。
- DMARC 暂保持 `p=none`，观察一段时间后再收紧。

### 域名归属

`kaitu.io`、`overleap.io` 和账号 5158 归 k2app；`allnationconnect.com`（含 `mail2`）归 ANC / Meet。开途的 `mail` 和 `edm` 都要迁出 `mail.allnationconnect.com`，分开事务和群发域名，避免群发拖累验证码信誉。

新域名没有发信信誉，所以旧地址先并行运行，确认送达率后再切。切换完成后通知 Meet 会话，由它清理 `mail.allnationconnect.com`。

## 部署顺序

1. 两台 Center 的 `config.yml` 写入 `mail_overleap`、`mail_backup`、`mail_backup_overleap`、`edm_overleap`，`send_from` 带显示名，确认 Slack `alert` 可用。
2. 部署 API。**必须先配置再部署**：回落逻辑删除后，缺配置的 Overleap 邮件会发不出去。
3. 线上验证：两个品牌各发一次验证码、重发一次，确认发件人和通道；用错误密码验证切换和报警；实发一封真实 EDM，确认 `service.*` 进收件箱且回复能到客服信箱。
4. David 确认后，把开途 `mail` 从 `joshua@mail.allnationconnect.com` 切到 `noreply@notify.kaitu.io`。

## 留给实现者

- 错误分类的具体判断（gomail 错误会包一层，需以实际错误文本测试）。
- 函数和测试接缝的命名。
- 测试需覆盖：品牌绝不交叉、切换、双失败、限频、奇偶换序、Redis 故障回退。
