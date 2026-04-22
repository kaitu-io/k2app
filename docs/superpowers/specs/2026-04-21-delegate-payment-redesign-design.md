# 代付功能重设计 · Design Spec

**Date**: 2026-04-21
**Author**: David (via brainstorm)
**Status**: Draft — pending implementation plan
**Revision**: v2（把"每单临时指定"改为"账户级预设"）

## 1. Summary

把"代付"从旧的**父账号主导**（`api_member_add` 父账号为子账号付款）重构为**受益人主导的账户级预设**：用户在账户里设置一个代付人邮箱（直接覆盖 `user.delegate_id`），之后每次购买都可以一键请该代付人付款。后端创建 stub 用户（如果邮箱还不存在），前端通过邮件把订单支付链（Wordgate Stripe Checkout）推送给代付人，代付人点击即付。订单归属不变（`user_id = 受益人`），订单模型零 schema 改动。

### 与 v1（已废弃）的差别
- v1：每单临时输入邮箱、独立 `/purchase/delegate` 页面、两种分享方式（邮件 + 复制链接）
- v2：代付人作为账户级预设、两个触发点（`/purchase` 内联空态 + `/account/delegate` 独立页）、仅邮件触达

## 2. Goals / Non-goals

### Goals
- 受益人能**一次设置长期生效**，避免每单重填
- `user.delegate_id` 做字段的**直接覆盖**（按用户指示）
- 空态用户在 `/purchase` 内联输入即可完成"保存 + 请求"，不强制跳账户页
- 已设置用户在 `/purchase` 看到双 CTA：[请 alice 代付] / [或自己支付]
- 代付人零注册摩擦（点邮件链接即付）
- 订单模型零 schema 改动

### Non-goals（v1 不做）
- 代付人后台管理（"我代付过谁"）
- 复制支付链分享（仅邮件触达）
- 独立 `/purchase/delegate` 二选一页面（整页已简化掉）
- 设置代付人时给代付人发"你被设为代付人"通知邮件（代付人第一次接触就是支付请求邮件本身）
- 多语言邮件（v1 复用现有 `emailTo` 中文硬编码，英文作 follow-up）
- 订单侧审计字段
- 清理旧 `/api/user/members` 孤立端点（单独 PR 处理）
- 企业批量授权同步机制（Novus Academy 那种）

## 3. UX 流程

### 3.1 空态用户（未设代付人）

```
Bob 登录 /purchase
  → 选 plan / 填活动码（不变）
  → Step 3 看到：
     [🚀 立即支付]  ← 自付主路径
     ---
     🙋 或请朋友代付
     [ email input: friend@example.com ]
     [ 发送代付请求 ]  ← secondary button
     （小字：我们会把支付链接发给 TA，同时保存为你的代付人，下次付款一键使用）
  → 填 alice@example.com → 点 [发送代付请求]
  → 前端连锁调用：
     1. PUT /api/user/delegate {email: "alice@example.com"}
        → 后端：查 stub，没有就建，设 user.delegate_id
     2. POST /api/user/orders（常规订单创建）
     3. POST /api/user/orders/:uuid/notify-delegate
        → 后端：给 delegate 发 pay-request 邮件
  → 显示确认状态："已请求 alice@example.com 代付（等待对方付款）"
```

### 3.2 已设用户（delegate_id 非空）

```
Bob 登录 /purchase
  → 选 plan / 填活动码（不变）
  → Step 3 顶部显示 chip：
     🙋 代付人：alice@example.com  [更改]
  → Step 3 底部看到：
     [🙋 请 alice 代付]  ← 主 CTA（indigo）
     [或自己支付]        ← secondary（灰色）
  → 点 [请 alice 代付]
  → 前端：
     1. POST /api/user/orders
     2. POST /api/user/orders/:uuid/notify-delegate
  → 显示确认状态
```

### 3.3 账户预设管理 `/account/delegate`

独立页面，两种状态渲染：

**空态**
```
标题：代付人设置
说明：设置后，付款时可一键邀请 TA 代付。只需填邮箱，付款发生时我们会发邮件给 TA。
[ email input ]
[ 保存 ]
小字：保存后不会立刻通知 TA；TA 收到的第一封邮件是你实际付款请求时的支付邀请。
```

**已设态**
```
标题：代付人设置
当前代付人：alice@example.com
  于 2026-04-18 设置
[ 修改 ]  [ 移除代付人 ]
小字：付款时可选择「请 alice@example.com 代付」，我们会发邮件给 TA。
```

### 3.4 Alice 侧体验

```
Alice 收邮件（纯文本）
  Subject: bob@example.com 请你帮忙代付一下 Kaitu 会员
  Body:
    你好，
    bob@example.com 想请你帮忙代付一下 Kaitu 会员，希望你愿意 🙏
    订单：1 年 Pro
    金额：$49.90
    付款链接：
    https://pay.wordgate.com/c/cs_...
    链接是 Stripe 安全支付页。付完以后 bob@example.com 会立刻收到会员时长。
    如果你不认识 bob@example.com，忽略这封邮件就好，不会扣任何费用。
    谢谢。
    —— Kaitu

Alice 点链接 → Stripe Checkout → 付款
  → Wordgate webhook → Kaitu 现有 handler 给 Bob 加会员时长
  → Stripe 自带成功页（Kaitu 不插手）
```

### 3.5 关键语义
- **`user.delegate_id` 可随时被覆盖**——用户 PUT 不同邮箱时，直接指向新的 stub，不保留历史
- **设置 delegate 不发通知邮件**——简化系统，Alice 第一次接触就是支付请求（含上下文）
- **自付退路永远保留**——delegate 只是预设，不是强制。`/purchase` 已设状态保留 [或自己支付] 按钮
- **Bearer URL**：payUrl 持票即付，Stripe Checkout session 原生处理过期/单次性/并发

## 4. Backend

### 4.1 端点清单

| 方法 | 路径 | 用途 | 用户角度 |
|---|---|---|---|
| `GET` | `/api/user/delegate` | 查询当前用户的代付人 | "谁在代付我" |
| `PUT` | `/api/user/delegate` | 设置或覆盖代付人 | "把 X 设为我的代付人" |
| `DELETE` | `/api/user/delegate` | 移除代付人 | "我不要代付人了" |
| `POST` | `/api/user/orders/:uuid/notify-delegate` | 给已设代付人发支付请求邮件 | "请 X 现在代付" |

### 4.2 `GET /api/user/delegate`

**Auth**: Required
**Response**:
```json
{
  "data": {
    "email": "alice@example.com",
    "setAt": 1745236789
  }
}
```
若未设置：`data: null`

**实现**：
- 读 `user.delegate_id`
- 若为 NULL → 返回 `null`
- 否则 JOIN `login_identifies` where `user_id = delegate_id AND type = "email"`, 解密 `encrypted_value` 作为 email
- `setAt` 取 `user.updated_at`（近似；若需精确可加独立字段，但 YAGNI）

### 4.3 `PUT /api/user/delegate`

**Auth**: Required
**Body**:
```json
{ "email": "alice@example.com" }
```

**逻辑**：
1. 校验 email 格式（gin binding `email` tag）
2. 拒绝 email 等于当前用户自己邮箱 → 422
3. email 小写化，计算 indexID
4. 查 `login_identifies` where type=email AND index_id=indexID
   - 命中 → 拿到对应 user.id（设 `delegateUserID`）
   - 未命中 → 创建 stub：`LoginIdentify{Type: email, IndexID, EncryptedValue, User: &User{UUID, ExpiredAt:0}}` via GORM cascade；`delegateUserID` = 新建 user.id
5. **直接覆盖** `user.delegate_id = delegateUserID`（即便已有值也覆盖）
6. 返回与 GET 相同 shape

**Response**: `{ data: { email, setAt } }`

**Error codes**:
- 422 ErrorInvalidArgument — email 格式非法 / email 等于自己 / stub 创建 DB 错误

### 4.4 `DELETE /api/user/delegate`

**Auth**: Required

**逻辑**：
- `user.delegate_id = NULL`
- Stub 用户不删除（可能别人也指向它，且 stub 本身是低开销记录）
- 返回 success

**注意**：与旧 `api_reject_delegate` 语义一致（都是"我清除我的代付关系"），可以复用现有 handler 或新写。推荐新写一个干净的 `api_delete_delegate` 并废弃旧的（旧的未来随 `/api/user/members` 一起清理）。

### 4.5 `POST /api/user/orders/:uuid/notify-delegate`

**Auth**: Required

**前置校验**：
1. 订单存在、`user_id = 当前用户`、`is_paid != true`
2. 当前用户 `delegate_id != NULL`

**逻辑**：
1. 查订单 + 拿 plan + payUrl（参考 4.7 开放问题）
2. 查 delegate_id 对应的 email（通过 login_identifies 解密）
3. 查当前用户自己的 email 作为 InviterEmail
4. 异步发送邮件（goroutine，模板见 4.6）
5. 同步返回 success

**Response**: `{ data: { delegateEmail: "alice@example.com" } }`

**Error codes**:
- 404 ErrorNotFound — 订单不存在
- 403 ErrorForbidden — 订单非当前用户
- 422 ErrorInvalidArgument — 订单已付款
- 422 ErrorInvalidArgument — 当前用户未设代付人
- 500 ErrorSystemError — 邮件发送失败

**注意**：邮件发送用 goroutine 时，如果失败无法反馈到 HTTP response。方案：
- 方案 A：同步发送，响应时间 < 2s（SMTP 快），失败返回 500
- 方案 B：goroutine 异步，HTTP 永远返回 success；失败用日志 + Slack 告警
- 推荐 A（用户点按钮就是在等反馈，明确成功/失败更好）

### 4.6 邮件模板

新增到 `api/logic_email.go`（继续使用 `EmailTemplate[T]` 模式，与现有 `memberAddedTemplate` 同构）：

```go
var delegatePayInviteTemplate = EmailTemplate[DelegatePayInviteMeta]{
    Subject: "{{.InviterEmail}} 请你帮忙代付一下 Kaitu 会员",
    Body: `你好，

{{.InviterEmail}} 想请你帮忙代付一下 Kaitu 会员，希望你愿意 🙏

订单：{{.PlanName}}
金额：{{.Amount}}

付款链接：
{{.PayUrl}}

链接是 Stripe 安全支付页。付完以后 {{.InviterEmail}} 会立刻收到会员时长。

如果你不认识 {{.InviterEmail}}，忽略这封邮件就好，不会扣任何费用。

谢谢。

—— Kaitu`,
}

type DelegatePayInviteMeta struct {
    InviterEmail string
    PlanName     string
    Amount       string // formatted, e.g. "$49.90"
    PayUrl       string
}
```

纯文本，无 HTML。deliverability 优先。

### 4.7 开放实现问题

1. **Wordgate payUrl 持久化**：`orders` 表的 `meta` JSON 里是否已存 payUrl？若无，需在 `create_order` 时存进去，`notify-delegate` 才能读到。实现期第一件事要查。
2. **`emailTo` 是否支持按收件人偏好切换语言模板**：当前看代码是硬编码 zh-CN；v1 接受。
3. **并发覆盖 delegate_id 的竞态**：不太可能（单用户自己设置），不加锁。
4. **stub 邮箱被真实用户注册后**：已有 `login_identifies` 唯一约束保护；stub 升级为真实用户的路径走现有登录流程（用户首次 OTP 登录该邮箱即认领 stub）。

### 4.8 文件改动

| 文件 | 改动 |
|---|---|
| `api/api_user_delegate.go`（新） | 3 个 handler：GET/PUT/DELETE 的 `api_get_delegate`/`api_put_delegate`/`api_delete_delegate` |
| `api/api_order_notify_delegate.go`（新） | `api_order_notify_delegate` handler |
| `api/logic_email.go` | + `delegatePayInviteTemplate` + `DelegatePayInviteMeta` |
| `api/route.go` | + 路由注册（3 delegate 路由 + 1 notify 路由）|
| `api/api_user_delegate_test.go`（新） | 覆盖正常/异常/覆盖语义 |
| `api/api_order_notify_delegate_test.go`（新） | 覆盖正常/缺 delegate/订单不属于/已付款等 |

**旧代码保持不动**（本 spec 不处理）：
- `api_get_delegate` / `api_reject_delegate`（旧名，若路径冲突则重命名新 handler）
- `api_member_add` / `api_member_list` / `api_member_remove`
- `/account/delegate` 旧前端（将被新版覆盖）

**路由冲突处理**：旧 `GET /api/user/delegate` 和 `DELETE /api/user/delegate` 在 `route.go` 已注册。新 PUT 是纯新增无冲突。GET 和 DELETE 需要决定：
- A. 把旧 handler 改写（直接替换函数体）
- B. 新写 handler，替换 route.go 中的函数名指向

推荐 **A**（就地重写），路径不变、语义升级。旧 handler 内部逻辑整体替换。

## 5. Frontend (`web/`)

### 5.1 `/purchase` 页改动（`PurchaseStep3.tsx`）

**页面加载时**：调 `GET /api/user/delegate` 拿当前状态，设到本地 state 中。

**空态渲染**：
```
主 CTA: [🚀 立即支付]（保持不变）
---（dashed divider）
🙋 或请朋友代付
[ email input ]
[ 发送代付请求 ] (outline button, indigo)
小字说明
```

点 [发送代付请求] 行为：
```ts
const onDelegatePayEmpty = async (email: string) => {
  setLoading(true);
  try {
    await api.setDelegate({ email });          // PUT
    const { order } = await api.createOrder({ plan, campaignCode });
    await api.notifyDelegate(order.uuid);
    setDelegateEmail(email);                   // 局部状态，立即变成已设态
    showConfirmation(email);                   // 跳确认视图或 in-place 提示
  } catch (e) { showError(e); }
  finally { setLoading(false); }
};
```

**已设状态渲染**：
```
顶部 chip: 🙋 代付人：alice@example.com  [更改]
--
主 CTA: [🙋 请 alice 代付]（indigo primary）
Secondary: [或自己支付]（灰色 outline）
```

点 [请 alice 代付] 行为：
```ts
const onDelegatePayExisting = async () => {
  setLoading(true);
  try {
    const { order } = await api.createOrder({ plan, campaignCode });
    await api.notifyDelegate(order.uuid);
    showConfirmation(delegateEmail);
  } catch (e) { showError(e); }
  finally { setLoading(false); }
};
```

点 [或自己支付] = 原有 onPurchase 逻辑，不变。
点 [更改] = 跳 `/account/delegate`。

### 5.2 确认状态（in-page）

替换整个 step3 为：
```
📨 已请求 alice@example.com 代付
我们已发邮件给 alice@example.com，附带支付链接。
TA 完成付款后你会立刻收到会员时长。

提醒：若 TA 收不到邮件请检查垃圾邮件箱。

[ 重新发送邮件 ]  [ 返回首页 ]
```

[重新发送邮件] = 再调一次 `notifyDelegate(order.uuid)`（相同订单，允许多次）。

### 5.3 `/account/delegate` 页改动

现有的旧版页（"拒绝代付"）整页替换。新结构：

```
/account/delegate/page.tsx (Server Component)
/account/delegate/DelegateClient.tsx (Client Component)

useEffect → api.getDelegate()
- 若 null → 渲染空态 form（email input + [保存]）
- 若 {email, setAt} → 渲染已设态（展示 + 修改/移除）

修改 = 显示 input 覆盖展示（或跳 modal）
移除 = DELETE /api/user/delegate → 回到空态
保存 = PUT /api/user/delegate → 回到已设态
```

**? `?returnTo=/purchase` 支持**：若 URL 带 returnTo，保存成功后自动跳该 URL（便于从 purchase "更改"跳来后无缝回流）。

### 5.4 API 客户端（`web/src/lib/api.ts`）

新增：
```ts
async getDelegate(): Promise<{ email: string; setAt: number } | null>
async setDelegate(req: { email: string }): Promise<{ email: string; setAt: number }>
async removeDelegate(): Promise<void>
async notifyDelegate(orderUuid: string): Promise<{ delegateEmail: string }>
```

### 5.5 i18n keys（`web/messages/*/purchase.json` + 可能新建 `account.json`）

加到 `purchase` namespace 的 `delegatePay` 命名空间：
```json
{
  "delegatePay": {
    "inlineTitle": "或请朋友代付",
    "inlineHint": "我们会把支付链接发给 TA，同时保存为你的代付人，下次付款一键使用。",
    "emailPlaceholder": "代付人邮箱 friend@example.com",
    "sendInviteButton": "发送代付请求",
    "chipLabel": "代付人：{email}",
    "chipChange": "更改",
    "primaryCtaWithDelegate": "请 {email} 代付",
    "secondaryCtaSelfPay": "或自己支付",
    "confirmationTitle": "已请求 {email} 代付",
    "confirmationBody": "我们已发邮件给 {email}，附带支付链接。TA 完成付款后你会立刻收到会员时长。",
    "confirmationSpamHint": "若 TA 收不到邮件请检查垃圾邮件箱。",
    "confirmationResend": "重新发送邮件",
    "confirmationResentToast": "已重新发送",
    "confirmationBackHome": "返回首页",
    "errorSelfInvite": "不能把自己设为代付人",
    "errorOrderAlreadyPaid": "订单已支付，无需代付"
  }
}
```

`account.json` namespace（新命名空间，需在 `namespaces.ts` 注册）：
```json
{
  "delegate": {
    "pageTitle": "代付人设置",
    "emptyTitle": "设置代付人",
    "emptyDescription": "设置后，付款时可一键邀请 TA 代付。",
    "emptyHint": "保存后不会立刻通知 TA；TA 收到的第一封邮件是你实际付款请求时的支付邀请。",
    "emailPlaceholder": "friend@example.com",
    "saveButton": "保存",
    "currentTitle": "当前代付人",
    "setAtLabel": "于 {date} 设置",
    "modifyButton": "修改",
    "removeButton": "移除代付人",
    "removeConfirm": "确认移除？你的付款将不再有预设代付人。",
    "currentHint": "付款时可选择「请 {email} 代付」，我们会发邮件给 TA。"
  }
}
```

7 个 locale 全加。zh-CN 源，其它 AI 翻译。

### 5.6 前端测试

- `PurchaseStep3.test.tsx`（vitest）：
  - 无 delegate → 渲染内联 input + [发送代付请求]
  - 有 delegate → 渲染 chip + 主次 CTA
  - 点 [发送代付请求] → 调用 setDelegate + createOrder + notifyDelegate 三个 API
- `account/delegate/DelegateClient.test.tsx`：
  - 空态 form 正确渲染 + 保存
  - 已设态展示 + 移除
- Playwright `tests/delegate-pay.spec.ts`：端到端
  - 登录 → purchase 无 delegate → 内联填邮箱 → 发送 → 看到确认态
  - 登录 → /account/delegate → 设置 → 跳 purchase → 看到 chip 和请 X 代付按钮

## 6. 边界情况

| 场景 | 行为 |
|---|---|
| 设置邮箱等于自己 | 422 "不能把自己设为代付人" |
| 设置邮箱格式非法 | 422（gin binding 自带）|
| 设置邮箱已是 Kaitu 用户 | 不建 stub，指向已有 user.id |
| 设置邮箱已是别人的 delegate stub | 共享同一 stub 记录，两个用户指向同一 delegate_id 正常（该 stub 是"收邮件的占位") |
| 覆盖 delegate_id | 直接改，旧 delegate_id 不清理（stub 可能仍被别人引用）|
| 移除后立刻再设置 | 正常工作 |
| 订单已付款时调 notify-delegate | 422 |
| 订单属于别人 | 403 |
| 未设 delegate 时调 notify-delegate | 422 "未设代付人" |
| Stub 邮箱后来注册真实账号 | 自然融合（login_identifies 唯一约束，首次 OTP 登录即认领）|
| 同订单多次调用 notify-delegate | 允许（即支持前端"重新发送邮件"）|

## 7. 对现存代码的影响

### 替换
- `GET /api/user/delegate` 和 `DELETE /api/user/delegate`：handler 整体重写为新语义（就地替换）
- `/account/delegate` 前端页整体替换

### 保持不动（单独 ticket）
- `api_member_add` / `api_member_list` / `api_member_remove`
- `/api/user/members` 系列端点
- `user.delegate_id` 字段上已有的历史数据（Novus Academy 等集团账号）
- 旧的 `memberAddedTemplate` 邮件模板

### 新代码的字段使用
- 新模型复用 `user.delegate_id` 字段（就是覆盖它）
- 不引入新字段、新表
- Stub 用户仍然是 `User{UUID, ExpiredAt:0}` + `LoginIdentify{Type,IndexID,EncryptedValue}`，与 member_add 同构

## 8. Out of Scope（单独 ticket）

- 英文及其它 5 语言邮件模板
- 清理旧 `api_member_*` 端点和 `/api/user/members` 路由
- 清理 21 个历史"壳代付关系"（之前 5056 的 182 已清）
- 企业批量授权同步机制（Novus Academy）
- 代付人"我代付过谁"的个人后台
- 代付数据大盘（转化率 / 成功率）
- 设置代付人时的通知邮件（决定不做；若后续产品侧要求可加）
- 一次性代付（不覆盖 delegate_id，只本次使用）——如果发现有需求再做

## 9. 变更清单（PR 拆分参考）

后端（1 PR）：
- `api/api_user_delegate.go`（新）
- `api/api_order_notify_delegate.go`（新）
- `api/logic_email.go`（+ 模板）
- `api/route.go`（+ 路由）
- `api/api_user_delegate_test.go`（新）
- `api/api_order_notify_delegate_test.go`（新）

前端（1 PR）：
- `web/src/components/PurchaseStep3.tsx`（改）
- `web/src/app/[locale]/purchase/PurchaseClient.tsx`（改，加载 delegate 状态 + 新流程）
- `web/src/app/[locale]/account/delegate/page.tsx`（覆盖旧）
- `web/src/app/[locale]/account/delegate/DelegateClient.tsx`（新 / 覆盖旧）
- `web/src/lib/api.ts`（+ 4 个方法）
- `web/messages/{7 locales}/purchase.json`（+ delegatePay）
- `web/messages/{7 locales}/account.json`（新 namespace）
- `web/messages/namespaces.ts`（注册 account namespace）
- `web/src/components/PurchaseStep3.test.tsx`（vitest 新增覆盖）
- `web/src/app/[locale]/account/delegate/DelegateClient.test.tsx`（vitest 新增）
- `web/tests/delegate-pay.spec.ts`（Playwright E2E）

其它：
- 无 schema 迁移
- 无 config / env 新增
- 无审批流程新增
