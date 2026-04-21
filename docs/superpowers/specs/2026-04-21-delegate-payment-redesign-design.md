# 代付功能重设计 · Design Spec

**Date**: 2026-04-21
**Author**: David (via brainstorm)
**Status**: Draft — pending implementation plan

## 1. Summary

把"代付"从旧的**父账号主导**模型（父账号预建子成员并为其付款，见历史 `api_member_*` 端点）重构为**受益人主导**的邀请付款模型：用户在 `/purchase` 页面点 **[找人代付]**，进入独立引导页，选择 (a) 填写代付人邮箱由系统发送邀请邮件，或 (b) 复制支付链接自行分享。订单归属不变（`user_id` = 受益人），Wordgate 支付链是 Bearer token，任何打开链的人都能付款。

## 2. Goals / Non-goals

### Goals
- 受益人能一键把当前订单的支付责任"外包"给另一个人（父母、朋友、同学）
- 最小改动：订单模型 0 schema 改动、0 新状态
- 代付人零注册摩擦（点链接即付，不强制登录）
- 给填邮箱渠道创建 stub 用户，为未来"claim 账号"软转化留接口

### Non-goals（v1 不做）
- 代付人后台管理（"我代付过谁"）
- 受益人的"我发起的代付"管理列表（YAGNI — Wordgate 自己有订单过期，失败直接重新创建）
- 自定义代付过期/取消逻辑（全部依赖 Wordgate/Stripe checkout session 原生行为）
- 多语言邮件（v1 复用现有 `emailTo` 的中文硬编码模板，英文作为 follow-up）
- 订单侧审计字段（`delegate_invited_user_id` 等不加，要分析就走 `login_identifies.created_at` 交叉查）
- 清理旧 `/api/user/members` / `/api/user/delegate` 端点（已孤立但无害，单独 PR 处理）

## 3. UX 流程

```
Bob 登录 /purchase
  → 选 plan / 填活动码 → 点 [🚀 立即支付]
       （旧流程，不变）
  → 点主按钮下方次级链接 [🙋 自己不方便？找人代付]
       → 前端先调 api.createOrder（和自付同一条）拿 payUrl + order.uuid
       → 跳 /purchase/delegate?order=<uuid>

/purchase/delegate?order=<uuid>
  上下文卡片：显示 plan_name + amount + 受益人邮箱（= Bob）
  动作 A（邮件邀请，上）：输入框 + [发送邀请] → POST /api/orders/:uuid/delegate-invite {email}
    后端：
      · 校验 order.user_id === 当前用户 且未付款
      · email 小写 + hash
      · 若邮箱已存在 LoginIdentify → 不建 stub，直接进下一步
      · 否则创建 stub user（LoginIdentify cascade，无 DelegateID）
      · 发邮件（纯文本模板，包含 inviter_email + plan_name + amount + pay_url）
      · 返回 success
    前端：显示"已发送到 friend@example.com ✓" + 允许再邀请其他人
  动作 B（复制链接，下）：展示 payUrl + [复制] → 剪贴板 + toast "已复制"
  次要：[← 返回支付页]

Alice 打开邮件 / 微信链接
  → 直接到 Wordgate Stripe Checkout（就是 payUrl 本身）
  → 用任意支付方式付款（信用卡、Apple Pay 等）
  → Wordgate webhook → Kaitu 现有 webhook handler → order.user_id = Bob 加会员时长
  → Alice 看到 Stripe 自带的"支付成功"页，Kaitu 不插手

Bob 端
  → 支付完成通过现有机制（轮询 /api/orders/:uuid 或 Wordgate webhook 触发的推送）更新 UI
  → Bob 的会员时长到账
```

### 关键语义
- **Bearer URL**：payUrl 持票即付。泄露风险可接受——被陌生人付款 = Bob 仍然受益，无资金损失
- **一次性使用**：Stripe Checkout session 原生支持
- **过期**：Stripe checkout session 默认 24h，Wordgate 是否 wrap 视他们实现而定；如过期 Bob 只能重新创建订单（旧订单自然作废）
- **并发邀请**：Bob 可以连发给多人。若多人都付款，Bob 拿到多倍会员时长（商业正向，不做防重逻辑）

## 4. Backend

### 4.1 新端点
```
POST /api/orders/:uuid/delegate-invite
AuthRequired
Body: { "email": "friend@example.com" }
Success: { "stubCreated": bool }
Errors:
  404 ErrorNotFound        — 订单不存在
  403 ErrorForbidden       — 订单非当前用户所有
  422 ErrorInvalidArgument — 订单已付款 / email 格式非法 / email == 当前用户自己
  500 ErrorSystemError     — 邮件发送失败（stub 若已创建不回滚）
```

实现位置：`api/api_order.go`（新增 handler `api_delegate_invite`）或独立 `api/api_delegate_pay.go`（若我们预期代付功能后续扩展则独立更清晰）。推荐**独立文件**。

### 4.2 实现细节
```go
// api_delegate_pay.go
func api_delegate_invite(c *gin.Context) {
    orderUUID := c.Param("uuid")
    var req struct { Email string `json:"email" binding:"required,email"` }
    // bind + validate
    
    user := ReqUser(c)
    var order Order
    // find by uuid, check ownership, check !IsPaid
    
    email := strings.ToLower(req.Email)
    // reject if email == current user's email（避免自己邀自己）
    
    indexID := secretHashIt(c, []byte(email))
    stubCreated := false
    var existing LoginIdentify
    err := db.Get().Where("type = ? AND index_id = ?", "email", indexID).First(&existing).Error
    if err == gorm.ErrRecordNotFound {
        // 建 stub（不设 DelegateID —— 这是新模型，不挂父子关系）
        encEmail, _ := secretEncryptString(c, email)
        li := LoginIdentify{
            Type: "email", IndexID: indexID, EncryptedValue: encEmail,
            User: &User{ UUID: generateId("user"), ExpiredAt: 0 },
        }
        if e := db.Get().Create(&li).Error; e != nil { /* handle */ }
        stubCreated = true
    } else if err != nil { /* sys error */ }
    
    // 发送邮件（同步或 goroutine，看 emailTo 语义；现有 member_add 用 goroutine）
    go emailTo(c, email, delegatePayInviteTemplate, DelegatePayInviteMeta{
        InviterEmail: <bob's email>,
        PlanName:     order.GetPlan().Label,
        Amount:       formatMoney(order.PayAmount),
        PayUrl:       order.Meta.PayUrl, // 注：需确认 order 模型存了 payUrl；若没存则需先取
    })
    
    Success(c, gin.H{"stubCreated": stubCreated})
}
```

**注**：`payUrl` 是否持久化在 `orders` 表需要确认。若没有，需要在 `create_order` 时存进 `meta` JSON，或在本 handler 里重新向 Wordgate 请求（后者浪费，前者更好）。这是实现期要解决的 one-liner。

### 4.3 路由注册
在 `api/route.go` 的 `/api/orders/*` 分组加：
```go
orders.POST("/:uuid/delegate-invite", api_delegate_invite)
```

### 4.4 邮件模板（`api/logic_email.go`）
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

### 4.5 测试
- `api_delegate_invite_test.go`
  - 正常路径：未存在邮箱 → 建 stub + 发邮件 → 返回 stubCreated=true
  - 已存在邮箱 → 不建 stub，仍发邮件 → stubCreated=false
  - 订单非当前用户 → 403
  - 订单已付款 → 422
  - 自己给自己发 → 422
  - 邮箱格式非法 → 422
- 走 `SetupMockDB(t)` + testify assert，遵照 CLAUDE.md 的 Test Convention

## 5. Frontend (`web/`)

### 5.1 `/purchase` 页改动
在 `web/src/components/PurchaseStep3.tsx` 的 [立即支付] 按钮**下方**加一个次级链接：

```tsx
<Button variant="destructive" ...>🚀 立即支付</Button>

<div className="text-center mt-2">
  <Link 
    href={`/purchase/delegate?order=${orderData?.uuid}`}
    className="text-sm text-primary hover:underline inline-flex items-center gap-1"
  >
    🙋 {t('purchase.delegatePay.entryLink')}
  </Link>
</div>
```

**前置条件**：链接要等 `orderData?.uuid` 就绪再显示（避免用户点了跳空页）。现有 `createOrderPreview` 流程已经会在 Step3 拿到 preview order，若 preview 不带 uuid（推测只带金额），需要判断 `orderData?.uuid` 存在才启用链接；否则 disable。

**额外策略**（需前端架构师确认）：是否进入 delegate 页前"确认创建正式订单"（非 preview）？Wordgate checkout session 是在 preview 阶段还是 confirm 阶段生成，会影响这里。

### 5.2 新页 `web/src/app/[locale]/purchase/delegate/page.tsx`
- Server Component 导出 `generateMetadata`（遵循 web/CLAUDE.md SEO 规则，虽然此页应为 `noindex`——代付页面不应被搜索引擎索引）
- 内部 `<DelegatePayClient />` 是 client component，读 URL search param `?order=<uuid>`
- 通过 `api.getOrder(uuid)` 拿订单详情，渲染上下文
- 若订单不属于当前用户 → 显示错误"此订单不属于你"
- 若订单已付款 → 显示"订单已支付，无需代付"+ 跳转账户
- 若当前用户未登录 → 跳到 `/login?next=/purchase/delegate?order=...`

**布局**（L1 · 纵向堆叠）：
```
← 返回支付页

标题：找人代付

上下文 Card：
  请求为 {planName} · {amount} 付款
  受益人：{currentUserEmail}

Card A · 📧 邮件邀请
  Input[邮箱] + Button[发送邀请]
  成功态：显示"✓ 已发送到 x@y.com（可再邀请他人）"
  失败态：显示错误（getApiErrorMessage）

Card B · 🔗 复制支付链接
  payUrl（truncate 显示）+ Button[复制]
  成功态：toast "已复制"
```

### 5.3 API 客户端（`web/src/lib/api.ts`）
新增：
```ts
async inviteDelegatePay(orderUuid: string, email: string): Promise<{ stubCreated: boolean }> {
  return this.post(`/api/orders/${orderUuid}/delegate-invite`, { email });
}
```

### 5.4 i18n keys（7 种语言 × 一份 JSON 文件）
在 `web/messages/*/purchase.json` 新增 `delegatePay` 命名空间：
```json
{
  "delegatePay": {
    "entryLink": "自己不方便？找人代付",
    "pageTitle": "找人代付",
    "contextLine": "请求为 {planName} · {amount} 付款",
    "beneficiary": "受益人：{email}",
    "backToPay": "返回支付页",
    "sectionEmail": "邮件邀请",
    "sectionEmailHint": "填写对方邮箱，我们发送支付链接",
    "emailPlaceholder": "friend@example.com",
    "sendInvite": "发送邀请",
    "inviteSent": "已发送到 {email}",
    "sectionCopy": "复制支付链接",
    "sectionCopyHint": "通过微信、短信或任何方式分享给对方",
    "copy": "复制",
    "copied": "已复制"
  }
}
```
zh-CN 为源，其它 6 种语言同步翻译。**不要只加 zh-CN**——web/CLAUDE.md 明确要求每个 key 必须在 7 个 locale 都存在。

### 5.5 前端测试
- `tests/delegate-pay.spec.ts`（Playwright E2E）：登录 → 建订单 → 点 entry link → 跳 delegate 页 → 填邮箱 → 发送 → 看到成功态
- `delegate/page.test.tsx`（vitest）：非登录用户看到重定向、订单 已付 状态显示正确等
- 至少一个 snapshot 测试或 DOM 断言覆盖布局

## 6. 边界情况 & 错误处理

| 场景 | 行为 |
|---|---|
| 邀请邮箱已是 Kaitu 用户 | 不建 stub，仍发邮件。收件人看到 pay_url，可以登录后付款（若愿）或免登录直接付 Wordgate |
| 邀请邮箱已是**代付人专用 stub**（之前别人也邀请过） | 幂等：不建新 stub，发邮件 |
| 重复邀请同一邮箱（同一订单） | 允许，不去重——每次都会新发邮件；Alice 收到 2 封不影响付款（仍然是同一 payUrl） |
| 邀请邮箱是当前用户自己 | 422 "不能邀请自己" |
| 订单已付款 | 422 "订单已支付，无需代付邀请" |
| 订单不属于当前用户 | 403 |
| 订单 uuid 格式不存在 | 404 |
| 邮件发送失败（SMTP 异常） | Stub 创建不回滚（已生效的业务），但接口返回 500，前端提示"邀请邮件发送失败，请稍后重试或直接复制链接发送" |
| Wordgate payUrl 已过期 | 前端检测（或后端），提示"订单已过期，请回到支付页重新创建" |

## 7. 对现存代码的影响

### 保持不动
- `api_member_add` / `api_member_list` / `api_member_remove`（旧父账号模式 CRUD）
- `api_get_delegate` / `api_reject_delegate`（旧"拒绝被代付"端点）
- `user.delegate_id` 字段（数据库中仍有数据，只是不在新流程中产生）
- `/account/delegate` 旧管理页（继续服务已有父账号用户）

### 新代码不复用旧 `delegate_id`
旧 `delegate_id` 语义 = "我被谁代付"，是父子绑定。新代付模式下 Alice 付了 Bob 的某单 ≠ Alice 要代 Bob 未来所有订单——不应绑定。所以新 stub 用户的 `delegate_id` 保持 NULL。

### 后续清理（不在本 spec 内）
- 整理 `/api/user/members` 等孤立端点，考虑标记 deprecated 或迁移到新模型
- 清理 21 个已删除的"壳代付关系"（之前 anc-000 的 182 个已清理）
- 评估 Novus Academy 集团账号的特殊同步机制是否保留

## 8. 开放问题（实现期确认）

1. **Wordgate payUrl 是否已持久化在 order 上**？若未持久化需在 create_order 时存进 `meta`
2. **Wordgate Checkout Session 的实际过期时间**？（影响 7.5 条边界提示）
3. **`emailTo` 是否支持按收件人语言切换模板**？若不支持则 v1 接受中文硬编码
4. **delegate 页是否应设置 `robots: noindex`**？（几乎肯定 yes，遵循 web/CLAUDE.md "No public pages without SEO metadata"——但该页是交易页不是公开页）
5. **前端创建订单的时机**：next-intl 下 purchase preview → confirm 的具体时机是否在点"找人代付"时触发 `api.createOrder`（非 preview）以拿到 payUrl？需对照 PurchaseClient.tsx 现有 onPurchase 分支梳理

## 9. Out of Scope（本次不做，单独 ticket）

- 英文/其他 6 语言邮件模板
- "我发起的代付"管理 UI
- 代付人激活账号的软转化（邮件里加"创建账号领 3 天会员"）
- 代付数据大盘（转化率 / 成功率）
- 清理旧 delegate_id 孤儿关系
- 企业批量授权（Novus Academy 那种 9 人同步过期的机制）

## 10. 变更清单（用于 PR 拆分参考）

后端：
- `api/api_delegate_pay.go`（新）
- `api/logic_email.go`（+ 模板 `delegatePayInviteTemplate` 与 `DelegatePayInviteMeta`）
- `api/route.go`（+ 1 行路由）
- `api/api_delegate_pay_test.go`（新）

前端：
- `web/src/components/PurchaseStep3.tsx`（+ 次级链接）
- `web/src/app/[locale]/purchase/delegate/page.tsx`（新 Server Component）
- `web/src/app/[locale]/purchase/delegate/DelegatePayClient.tsx`（新 Client Component）
- `web/src/lib/api.ts`（+ `inviteDelegatePay`）
- `web/src/lib/api-errors.ts`（若引入新 error code 需映射）
- `web/messages/{7 locales}/purchase.json`（+ `delegatePay` 命名空间）
- `web/tests/delegate-pay.spec.ts`（Playwright E2E）
- `web/src/app/[locale]/purchase/delegate/page.test.tsx`（vitest）

其它：
- 无 schema 变更
- 无 migration
- 无 config / env 新增
