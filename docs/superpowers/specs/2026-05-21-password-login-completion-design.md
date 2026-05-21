# 密码登录补完 — 设计文档

**日期**：2026-05-21
**作者**：David + Claude
**状态**：Draft

---

## 1. 背景

之前的迭代里我们已经把"邮箱+密码"作为第二条登录路径开发到一半，目前散落在仓库各处：

- 后端 `api_password_login`（`api/api_auth.go:721`）+ `api_set_password`（`api/api_user.go:370`）+ `logic_password.go` 已全部就位
- webapp `EmailLoginForm` 已带验证码/密码 Tabs（用于 Purchase）
- webapp `PasswordDialog`（"设置密码"形态）写完后于 commit `038f5fd0`（2026-03-08）"hide password entry (not ready for release)" 被一键删除入口
- webapp 全局 `LoginDialog` 只支持验证码
- web 站点 `EmailLogin` 只支持验证码；没有密码登录端点；没有设置/修改密码 UI

这次目标：把"密码登录 + 设置/修改密码"补完到全平台一致，复用现有组件而不是重写。

## 2. 范围

| # | 改动 |
|---|---|
| 1 | 后端新增 `POST /api/auth/web-login/password`（web cookie 版密码登录，无 UDID） |
| 2 | 后端 `api_set_password` 改密成功后发提醒邮件 |
| 3 | 后端 `DataUser` 暴露 `hasPassword: bool` |
| 4 | webapp 抽 `PasswordAuthFields` 共用，`EmailLoginForm` 改用之 |
| 5 | webapp `LoginDialog` 加密码 Tab，复用 `PasswordAuthFields` |
| 6 | webapp `Account.tsx` 恢复密码入口（文案随 `hasPassword` 切换） |
| 7 | web `EmailLogin` 加密码 Tab（自动覆盖 Purchase / 兑换码 / Survey 三处） |
| 8 | web `/account` 加修改密码入口 + 新 `ChangePasswordDialog` |
| 9 | web `api.ts` 加 `passwordLogin()` + `setPassword()` |
| 10 | webapp/web 错误码模块：识别强度校验 message（`password_too_short` 等）→ i18n |

**不在范围**：
- 忘记密码独立流程（决议：让用户走"验证码登录"重置）
- 旧密码校验（决议：信任 cookie/session，但发改密提醒邮件）
- 独立 `ErrorPasswordNotSet` 错误码（决议：未设密码用户走密码登录返 `ErrorInvalidCredentials`，防止邮箱枚举）

## 3. 后端设计（`api/`）

### 3.1 新增 `POST /api/auth/web-login/password`

新 handler `api_web_password_login`（`api/api_auth.go`，紧贴 `api_web_auth` 之后）：

```go
type WebPasswordLoginRequest struct {
    Email    string `json:"email" binding:"required,email"`
    Password string `json:"password" binding:"required"`
    Language string `json:"language"`
    // 邀请码可选 — 与 web-login 一致
    InviteCode string `json:"inviteCode"`
}
```

实现要点（与 `api_password_login` 共享密码校验段、与 `api_web_auth` 共享 cookie/邀请段）：

1. `cleanEmailField` → 查 `LoginIdentify` → 验 `IsAccountLocked` / `HasPasswordSet` / `UserPasswordVerify`
2. 失败 → `RecordFailedPasswordAttempt` → 返 `ErrorInvalidCredentials`（**统一**：未设密码、密码错、用户不存在都返同一个码）
3. 成功 → `ResetFailedPasswordAttempts`
4. 复用 `api_web_auth` 的事务（语言、邀请码、激活），生成 `generateWebCookieToken`，`setAuthCookies(c, authResult)`
5. 发 `webLoginTemplate` 邮件（与验证码版 web 登录一致 — 用户视角"登录提醒"）
6. 返 `DataWebLoginResponse{User, AccessToken}` — 与现有 `api_web_auth` 同形

`api/route.go` 加：
```go
auth.POST("/web-login/password", api_web_password_login)
```

### 3.2 `api_set_password` 发改密邮件

`api/api_user.go:370` 末尾保存成功后追加（在返 `SuccessEmpty` 前）：

```go
meta := PasswordChangedMeta{
    ChangeTime: time.Now().Format("2006-01-02 15:04:05"),
    ClientIP:   c.ClientIP(),
}
if err := emailToUser(c, int64(userID), passwordChangedTemplate, meta); err != nil {
    log.Errorf(c, "failed to send password changed email to user %d: %v", userID, err)
}
```

**新模板**：
- `api/templates/email/password_changed.{zh-CN,en-US,zh-TW,zh-HK,en-AU,en-GB,ja}.html`
- 模板内容参考现有 `web_login.zh-CN.html` —— 改成"您的密码已被修改 / If this wasn't you, contact support immediately"
- `api/logic_email.go`（或 template 注册处）加 `passwordChangedTemplate` 常量

**新 meta 类型**（合适的 model 或 type 文件）：
```go
type PasswordChangedMeta struct {
    ChangeTime string
    ClientIP   string
}
```

### 3.3 `DataUser.HasPassword`

`api/type.go` `DataUser` 加字段：
```go
HasPassword bool `json:"hasPassword"`
```

`api/api_user.go:454` `buildDataUserWithDevice` 构造时填：
```go
HasPassword: HasPasswordSet(user),
```

### 3.4 测试新增

- `api/api_web_password_login_test.go`（新）— SetupMockDB + 覆盖：
  - 成功登录 → 200 + cookie 设置 + `accessToken` 在 body
  - 密码错 → `ErrorInvalidCredentials` + `PasswordFailedAttempts` 自增
  - 未设密码 → `ErrorInvalidCredentials`（与密码错同码，确认无邮箱枚举差异）
  - 锁定状态 → `ErrorTooManyRequests`
- `api/api_user_password_test.go`（新或补到现有 `api_user_test.go`）—
  - `api_set_password` 成功后调 `emailToUser` 一次（mock 拦截）
  - 强度错误返 `ErrorInvalidArgument` + message 是 `password_too_short` 等

## 4. Webapp 设计（`webapp/`）

### 4.1 新组件 `PasswordAuthFields.tsx`

`webapp/src/components/PasswordAuthFields.tsx`（新）—— 纯展示，无 fetch / 无状态机：

```tsx
interface PasswordAuthFieldsProps {
  email: string;
  password: string;
  onEmailChange: (v: string) => void;
  onPasswordChange: (v: string) => void;
  onSubmit: () => void;
  isSubmitting: boolean;
  emailSuggestion: string | null;
  onAcceptSuggestion: () => void;
  onEmailBlur: () => void;
}
```

输出：MUI `TextField` 邮箱 + 密码 + `EmailSuggestion` + 提交按钮。
- autocomplete=`current-password`
- Enter 触发 `onSubmit`
- 与 `EmailLoginForm` 现有密码段视觉一致，仅抽离出来

### 4.2 改造 `EmailLoginForm.tsx`

- 删除密码 tab 中重复的邮箱/密码输入框代码
- 改为 `<PasswordAuthFields {...props}>`
- `handlePasswordLogin` 保持不变（已有，调 `/api/auth/login/password`）

### 4.3 改造 `LoginDialog.tsx`

- 加 MUI `Tabs`（`code` | `password`），与 `EmailLoginForm` 同结构
- 验证码段保持原样
- 密码段嵌 `<PasswordAuthFields>`
- `handlePasswordLogin`：调用 `/api/auth/login/password`（device-bound，带 UDID）
- `transformStatus` / cacheStore 清空逻辑沿用验证码登录后处理

### 4.4 恢复 `Account.tsx` 密码入口

恢复 commit `038f5fd0` 删除的：
- `LockIcon` import
- `PasswordDialog` import
- `showPasswordDialog` state
- ListItem（含 `<LockIcon>` + 文案）
- `<PasswordDialog>` 渲染

**新增**：文案随 `user.hasPassword` 切换：
```tsx
<ListItemText primary={user?.hasPassword
  ? t('account:password.changePassword')
  : t('account:password.setPassword')} />
```

`PasswordDialog` 标题同理切：
```tsx
<DialogTitle>{hasPassword
  ? t('account:password.changePassword')
  : t('account:password.setPassword')}</DialogTitle>
```

加 prop `hasPassword?: boolean` 给 `PasswordDialog`。

成功后调 `fetchUser()` 刷新 `hasPassword`。

### 4.5 错误码强度提示

`webapp/src/utils/errorCode.ts`：在 `getErrorMessage(code, message, t)` 内对 `ErrorInvalidArgument` (`422`) 检查 message：

```ts
if (code === ERROR_CODES.INVALID_ARGUMENT) {
  if (message === 'password_too_short') return t('account:password.tooShort');
  if (message === 'password_needs_letter') return t('account:password.needsLetter');
  if (message === 'password_needs_number') return t('account:password.needsNumber');
}
```

这是 **唯一**允许根据 message 字符串路由的位置 —— message 是后端强度 enum 而非自由文本，与"展示 backend message"宪法不冲突。

### 4.6 类型同步

`webapp/src/services/api-types.ts` `DataUser` 加：
```ts
hasPassword: boolean;
```

### 4.7 Webapp 测试

- `EmailLoginForm.test.tsx`：加 password tab 测试（点 tab、填邮箱+密码、提交、断言调 `/api/auth/login/password`）
- `LoginDialog.test.tsx`：tab 切换 + 密码登录提交断言
- `Account.test.tsx`：
  - `hasPassword=true` → ListItem 文案为"修改密码"
  - `hasPassword=false` → 文案为"设置密码"
  - 点击打开 `PasswordDialog`
- `PasswordDialog.test.tsx`：保持现有 + 加 `hasPassword=true` 时标题文案

## 5. Web 设计（`web/`）

### 5.1 `web/src/lib/api.ts`

新增类型 + 方法：

```ts
interface PasswordLoginRequest {
  email: string;
  password: string;
  language?: string;
  inviteCode?: string;
}

async passwordLogin(
  data: PasswordLoginRequest,
  options?: Pick<ApiRequestOptions, 'autoRedirectToAuth'>
): Promise<WebLoginResponse> {
  return this.request<WebLoginResponse>('/api/auth/web-login/password', {
    method: 'POST',
    body: JSON.stringify(data),
    ...options,
  });
},

async setPassword(data: { password: string; confirmPassword: string }): Promise<void> {
  return this.request<void>('/api/user/password', {
    method: 'POST',
    body: JSON.stringify(data),
  });
},
```

`UserProfile` 类型加：
```ts
hasPassword: boolean;
```

### 5.2 改造 `web/src/components/EmailLogin.tsx`

- 加 shadcn `Tabs`：`code` | `password`
- 密码 tab：邮箱（已有的 typo suggestion 逻辑保留）+ 密码 + 提交
- `handlePasswordLogin` → 调 `api.passwordLogin` → `useAuth().login(user, accessToken)` → `onLoginSuccess?.()`
- 错误处理与 webapp 同：识别 message-based 强度错误，其它走 `getApiErrorMessage`
- `mode='bind'` 时 password tab 仍可用（兑换码场景下，已有密码用户直接密码登录也合理）

### 5.3 新 `web/src/components/ChangePasswordDialog.tsx`

shadcn `Dialog` + 两个 `Input type="password"` + 校验：
- 至少 8 位 + 字母 + 数字（前端预校验，后端最终校验）
- 两次密码一致
- 调 `api.setPassword`
- 成功 toast → 关 dialog

Props：`{ open, onOpenChange, hasPassword: boolean, onSuccess?: () => void }`。

`hasPassword` 控制标题/按钮文案。

### 5.4 `web/src/app/[locale]/account/page.tsx`

- 加"安全"分区
- 一个按钮 → 打开 `ChangePasswordDialog`
- 按钮文案根据 `user.hasPassword` 切换
- 关闭后 `mutate()` 刷新用户

### 5.5 i18n

`web/messages/{locale}/auth.json` 加：
```json
"login": {
  "passwordLogin": "...",
  "codeLogin": "...",
  "password": "...",
  "passwordPlaceholder": "..."
}
```
7 locale 全部加。

`web/messages/{locale}/account.json`（如无则参考 namespaces 注册）：
```json
"password": {
  "setPassword": "...",
  "changePassword": "...",
  ...
}
```

`web/src/lib/api-errors.ts` `getApiErrorMessage(code, t, message?)` 加 message 路由（同 webapp）。

### 5.6 Web 测试

- `web/src/components/__tests__/EmailLogin.test.tsx`：tab 切换、密码登录调 `/api/auth/web-login/password`
- `web/src/components/__tests__/ChangePasswordDialog.test.tsx`：校验、成功调 setPassword

## 6. 数据流

```
┌──── Webapp（桌面/移动） ────────────────────┐
│ EmailLoginForm / LoginDialog               │
│   Tab=code:     POST /api/auth/login       │
│                 (UDID + verificationCode)  │
│   Tab=password: POST /api/auth/login/      │
│                 password (UDID + password) │
│                                            │
│ Account                                    │
│   PasswordDialog → POST /api/user/password │
│   服务端发改密邮件                          │
│   onSuccess → fetchUser() 刷新 hasPassword │
└────────────────────────────────────────────┘

┌──── Web 站点（cookie） ────────────────────┐
│ EmailLogin (Purchase/兑换码/Survey 共用)    │
│   Tab=code:     POST /api/auth/web-login   │
│   Tab=password: POST /api/auth/web-login/  │
│                 password                   │
│                 → Set-Cookie access_token  │
│                                            │
│ /account                                   │
│   ChangePasswordDialog →                   │
│   POST /api/user/password                  │
└────────────────────────────────────────────┘
```

## 7. 错误处理矩阵

| 场景 | 后端响应 | 前端处理 |
|---|---|---|
| 密码错 | `ErrorInvalidCredentials` (400) | i18n "邮箱或密码错" |
| 未设密码（密码登录） | `ErrorInvalidCredentials` (400) | 同上（防邮箱枚举） |
| 账号锁定（5 次失败） | `ErrorTooManyRequests` (429) | i18n "失败次数过多，请稍后再试" |
| 密码太短 | `ErrorInvalidArgument` + message `password_too_short` | i18n `account.password.tooShort` |
| 缺字母 | `ErrorInvalidArgument` + `password_needs_letter` | `needsLetter` |
| 缺数字 | `ErrorInvalidArgument` + `password_needs_number` | `needsNumber` |
| 两次不一致 | `ErrorInvalidArgument` + `passwords do not match` | 前端先拦截，不发请求 |
| Cookie 写入失败（iOS WeChat） | 200 + accessToken in body | `localStorage` Bearer fallback（已有逻辑） |

## 8. 实现顺序

1. **后端** — 新增端点 + 改 set_password 发邮件 + 模板 + DataUser.hasPassword + 测试
2. **webapp** — 抽 PasswordAuthFields → 改 EmailLoginForm → 加 LoginDialog 密码 tab → 恢复 Account 入口 → 错误码 message 路由 → 测试
3. **web** — api.ts 加方法 → EmailLogin 加 tab → ChangePasswordDialog → /account 入口 → i18n → 错误码 → 测试
4. **回归**：vitest + go test ./… + 手动跑 Purchase / 兑换码 / Survey / Account 三平台冒烟

## 9. 风险与回退

| 风险 | 缓解 |
|---|---|
| 改密后邮件发不出阻塞响应 | 邮件失败仅 `log.Errorf`，不返错（与现有 `webLoginTemplate` 一致） |
| 邮箱枚举（未设密码 vs 不存在用户） | 统一返 `ErrorInvalidCredentials`，不在响应里区分 |
| 旧版 webapp（已发布版本 ≤ 0.4.4）调老路径 | 不删任何老端点；新功能纯加 |
| `PasswordDialog` 旧测试（仅 set 形态） | 加 `hasPassword` prop 默认值 `false` 保持向后兼容 |

## 10. 验收清单

- [ ] 后端测试全过：`cd api && go test ./...`
- [ ] webapp 测试全过：`cd webapp && yarn test`
- [ ] web 测试全过：`cd web && yarn test`
- [ ] webapp 类型：`cd webapp && npx tsc --noEmit`
- [ ] web 类型：`cd web && yarn build`
- [ ] 手测 webapp：Purchase 密码登录、LoginDialog 密码登录、Account 设/改密码 + 收到邮件
- [ ] 手测 web：Purchase / 兑换码 / Survey 三处密码登录、/account 改密码
- [ ] 检查 ErrorCode 宪法：所有 message-based 路由集中在 `errorCode.ts` / `api-errors.ts`
