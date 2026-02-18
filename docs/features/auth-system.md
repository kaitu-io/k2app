# Feature: Auth System

## Meta

| Field | Value |
|-------|-------|
| Feature | auth-system |
| Version | v1 |
| Status | implemented |
| Created | 2026-02-18 |
| Updated | 2026-02-18 |

## Overview

k2app 的认证系统，覆盖登录、token 管理、401 自动刷新、设备绑定和路由守卫。核心设计原则：

1. **全局弹窗模式** — 没有 `/login` 路由，所有登录流程通过 `LoginDialog` 全局弹窗触发
2. **Email OTP 为主** — 邮箱验证码登录是主要方式，密码登录是备选（仅在 EmailLoginForm 中可用）
3. **Token 自动管理** — cloudApi 拦截所有 auth 路径的响应自动存取 token，无需手动调用
4. **401 透明刷新** — 遇到 401 自动用 refreshToken 刷新，并发请求共享一个刷新 Promise

## Product Requirements

### 登录方式

| 方式 | 入口 | 说明 |
|------|------|------|
| 邮箱验证码登录 | LoginDialog / EmailLoginForm | 输入邮箱 -> 发送验证码 -> 输入验证码 -> 登录。无需提前注册，首次登录自动创建账号 |
| 密码登录 | EmailLoginForm (Purchase 页面) | Tab 切换到"密码登录"，输入邮箱 + 密码直接登录。仅在 EmailLoginForm 中可用 |
| 邀请码 | LoginDialog / EmailLoginForm | 未激活用户验证码步骤可选输入邀请码（8位大写字母），获得邀请奖励 |

### 登录触发场景

| 场景 | 触发方式 | 说明 |
|------|----------|------|
| 路由守卫 | `LoginRequiredGuard` | 包裹受保护页面，未登录时自动弹出 LoginDialog |
| Dashboard 连接按钮 | `openLoginDialog()` | 未登录点击连接时提示登录 |
| Tunnels 页面 | `openLoginDialog()` | 未登录访问线路列表时提示登录 |
| Account 会员卡 | `useLoginDialogStore.getState().open()` | 未登录时会员状态卡片显示登录按钮 |
| Purchase 页面 | EmailLoginForm 内嵌 | 未登录时直接在页面内显示登录表单（不用弹窗） |

### 登出流程

Account 页面"切换账号"按钮触发：
1. `window._k2.run('down')` 先断开 VPN
2. `cloudApi.post('/api/auth/logout')` 调用登出 API
3. cloudApi 自动清除 token 和缓存（`_handleAuthPath` 检测到 logout 路径）
4. `setIsAuthenticated(false)` 更新 UI 状态

## Technical Decisions

### 1. 全局弹窗而非路由

**决策**: 用 `LoginDialog` 组件 + `login-dialog.store` 管理登录弹窗，不设 `/login` 路由。

**原因**:
- 弹窗可以从任何页面触发而不丢失当前上下文
- `LoginRequiredGuard` 弹出登录窗口但不跳转，用户可以查看页面内容
- keep-alive 架构下路由跳转会引起不必要的组件卸载

**实现**:
- `useLoginDialogStore` 管理 `isOpen`、`trigger`（触发来源）、`redirectPath`、`message`
- `LoginDialog` 在 App.tsx 的 Layout 内全局挂载，始终可用
- 任何地方调用 `open({ trigger, message })` 即可打开弹窗

### 2. Token 存储与安全

**决策**: Token 存储在平台 secure storage 中，storage key 为 `k2.auth.token`（access）和 `k2.auth.refresh`（refresh）。

**存储实现按平台分**:

| 平台 | 实现 | 加密方式 |
|------|------|----------|
| Tauri (Desktop) | `tauri-plugin-store` | 内置 AES-256 |
| iOS | Swift 文件加密 | 系统级 |
| Android | `EncryptedSharedPreferences` | 系统级 |
| Web (standalone) | `webSecureStorage` | AES-256-GCM + 设备指纹派生密钥 |

Web 的 `webSecureStorage` 将 localStorage 中的数据用 AES-256-GCM 加密（设备指纹派生密钥）。实现细节详见 [secure-storage-cache.md](secure-storage-cache.md)。

### 3. cloudApi 自动 token 处理

**决策**: cloudApi 内部处理所有 auth 相关的 token 自动存取，调用方无需手动管理。

**auth 路径列表**:
```typescript
AUTH_TOKEN_PATHS = ['/api/auth/login', '/api/auth/register', '/api/auth/refresh']
AUTH_LOGOUT_PATH = '/api/auth/logout'
```

**自动行为**:
- 请求成功（`code === 0`）且路径匹配 `AUTH_TOKEN_PATHS` -> 自动从 `response.data` 提取 `accessToken`/`token` + `refreshToken` 并存入 secure storage
- 请求路径匹配 `AUTH_LOGOUT_PATH` -> 自动清除 token + 清除 cacheStore

### 4. 401 自动刷新与并发锁

**决策**: cloudApi 拦截 401 响应，用 refresh token 自动刷新，并发请求共享一个 refresh promise。

**流程**:
```
请求返回 401 (HTTP status 或 JSON code)
  -> _handle401()
    -> 检查有无 refreshToken，无则直接清除 token + 设置未认证
    -> 有 refreshToken:
      -> 检查 _refreshPromise 是否已存在
        -> 已存在: 等待同一个 promise（并发去重）
        -> 不存在: 创建新的 _doRefresh() promise
      -> _doRefresh() 成功: 保存新 token，返回 true
      -> _doRefresh() 失败: 清除 token，设置未认证
    -> 刷新成功后: 重试原始请求（request() 自动注入新 token）
```

**并发锁实现**: 模块级变量 `_refreshPromise: Promise<boolean> | null`，`finally` 中清空。多个并发 401 请求只触发一次 refresh 调用。

### 5. Auth Header 注入

每个 cloudApi 请求自动注入 Bearer token:
```typescript
const token = await authService.getToken();
if (token) {
  headers['Authorization'] = `Bearer ${token}`;
}
```

Token 来自 secure storage 的 `k2.auth.token` key。无 token 时不发送 Authorization header（匿名请求）。

### 6. 设备 UDID

**格式**: `{48 random hex}-{8 fingerprint hash}`，共 57 字符。

**生成与获取按平台分**:

| 平台 | 方法 | 说明 |
|------|------|------|
| Tauri | `invoke('get_udid')` -> Rust -> daemon `/api/device/udid` | 由 daemon 生成并持久化 |
| Capacitor | `K2Plugin.getUdid()` -> native | 由 native 层生成并持久化 |
| Standalone | `fetch('/api/device/udid')` -> daemon | 同 Tauri，走 daemon |

UDID 用于：
- 设备注册和管理
- Tunnel URL 鉴权: `k2v4://udid:token@domain?...`
- 后端设备列表展示

### 7. Auth Store 设计

`auth.store.ts` 是极简设计:

```typescript
interface AuthState {
  isAuthenticated: boolean;  // 用户是否已认证
  isAuthChecking: boolean;   // 认证状态是否正在检查中
  setIsAuthenticated: (value: boolean) => void;
  setIsAuthChecking: (value: boolean) => void;
  syncAuthStatus: () => Promise<void>;
}
```

**关键设计**: `syncAuthStatus()` 直接设置 `isAuthenticated: true`，假设已登录。如果实际未登录，第一个 API 请求会收到 401，cloudApi 会尝试 refresh，失败后设置 `isAuthenticated: false`。这是乐观策略 — 避免启动时额外的认证检查请求。

### 8. LoginRequiredGuard 行为

Guard 不阻止渲染 — 未登录时仍然渲染子组件（`return <>{children}</>`），只是额外弹出登录弹窗。

**keep-alive 场景处理**: Guard 检查 `location.pathname !== pagePath` 避免多个缓存页面的 Guard 同时响应。只有当前活跃页面的 Guard 才触发弹窗。

**受保护的页面**: `/devices`, `/pro-histories`, `/invite-codes`, `/member-management`, `/issues`, `/issues/:number`

### 9. Email OTP 流程细节

**Step 1 — 发送验证码**:
```
POST /api/auth/code { email, language }
Response: { userExists, isActivated, isFirstOrderDone }
```
- `isActivated` 决定是否在验证码步骤显示邀请码输入框
- 发送后启动 60 秒倒计时，倒计时结束前禁止重发

**Step 2 — 验证登录**:
```
POST /api/auth/login { email, verificationCode, remark, inviteCode?, language }
Response: { accessToken, refreshToken, expiredAt }
```
- `remark` 为设备备注（`t("startup:startup.newDevice")`）
- token 由 cloudApi 自动保存
- 登录成功后清除 cacheStore 确保数据刷新

**密码登录**（仅 EmailLoginForm）:
```
POST /api/auth/login/password { email, password, remark, deviceName, platform, language }
```

### 10. 邀请码流程

- `EmailLoginForm` 启动时检查 `kaitu_invite_code` cookie，有则预填邀请码
- 仅在 `isActivated === false`（未激活用户）时显示邀请码输入框
- 邀请码为可选项，8 位大写字母格式
- 通过 `inviteCode` 字段随登录请求一起提交

## Key Files

| 文件 | 职责 |
|------|------|
| `webapp/src/components/LoginDialog.tsx` | 全局登录弹窗。Email OTP 两步流程（输入邮箱 -> 输入验证码）。从 `login-dialog.store` 读取显示状态 |
| `webapp/src/components/EmailLoginForm.tsx` | 内嵌式登录表单（用于 Purchase 页面）。支持验证码登录和密码登录两种 Tab。读取 cookie 邀请码 |
| `webapp/src/components/EmailTextField.tsx` | 可复用的邮箱输入组件。onBlur 时 trim + lowercase + 格式校验 |
| `webapp/src/components/LoginRequiredGuard.tsx` | 路由守卫。未登录时弹出 LoginDialog 但不阻止页面渲染。支持 keep-alive 场景的 pagePath 判断 |
| `webapp/src/services/auth-service.ts` | Token 管理。`getToken()`, `getRefreshToken()`, `setTokens()`, `clearTokens()`, `getUdid()`, `getCredentials()`, `hasToken()` |
| `webapp/src/services/cloud-api.ts` | HTTP 客户端。Auth header 注入、401 自动刷新、并发锁、auth 路径自动 token 存取 |
| `webapp/src/stores/auth.store.ts` | 认证状态。`isAuthenticated`, `isAuthChecking`, `syncAuthStatus()`。乐观策略 |
| `webapp/src/stores/login-dialog.store.ts` | 弹窗状态。`isOpen`, `trigger`, `redirectPath`, `message`, `open()`, `close()` |
| `webapp/src/services/secure-storage.ts` | Web 安全存储。AES-256-GCM 加密 localStorage，设备指纹派生密钥 |
| `webapp/src/types/kaitu-core.ts` | 核心类型定义。`ISecureStorage`, `IPlatform.getUdid()`, `IPlatform.storage` |
| `webapp/src/services/api-types.ts` | API 类型。`AuthResult`, `SendCodeResponse`, `LoginRequest` |
| `webapp/src/pages/Account.tsx` | 登出入口。`handleLogout()`: VPN down -> logout API -> setIsAuthenticated(false) |
| `webapp/src/App.tsx` | 路由配置。`LoginRequiredGuard` 包裹受保护页面，`LoginDialog` 全局挂载 |

## Acceptance Criteria

### 登录流程

- **AC1**: 邮箱验证码登录 — 输入有效邮箱后点击发送，收到验证码，输入后成功登录。Token 自动存入 secure storage
- **AC2**: 密码登录 — EmailLoginForm 中切换到密码 Tab，输入邮箱和密码后成功登录
- **AC3**: 首次登录自动注册 — 新邮箱发送验证码返回 `isActivated: false`，显示邀请码输入框
- **AC4**: 验证码倒计时 — 发送后 60 秒内"重发"按钮禁用，显示倒计时数字
- **AC5**: 邀请码从 cookie 预填 — `kaitu_invite_code` cookie 存在时自动填入邀请码字段

### Token 管理

- **AC6**: Auth header 自动注入 — 已登录状态下所有 cloudApi 请求携带 `Authorization: Bearer <token>`
- **AC7**: 401 自动刷新 — 请求返回 401 时自动用 refreshToken 刷新，成功后重试原始请求
- **AC8**: 并发 401 去重 — 多个请求同时收到 401 时只触发一次 refresh 调用
- **AC9**: Refresh 失败登出 — refresh 失败时清除所有 token 并设置 `isAuthenticated: false`
- **AC10**: Auth 路径自动存取 — login/register/refresh 成功响应自动保存 token；logout 成功自动清除

### 路由守卫

- **AC11**: Guard 弹窗不阻塞 — `LoginRequiredGuard` 弹出登录弹窗但页面内容仍然渲染
- **AC12**: Keep-alive 隔离 — 只有当前活跃页面的 Guard 触发弹窗，缓存页面不触发
- **AC13**: 受保护页面列表 — `/devices`, `/pro-histories`, `/invite-codes`, `/member-management`, `/issues` 需要登录

### 登出

- **AC14**: 登出完整流程 — VPN 先断开，再调用 logout API，再清除本地状态
- **AC15**: 登出后缓存清除 — cloudApi 检测到 logout 路径成功后自动清除 cacheStore

### 安全存储

- **AC16**: Token 加密存储 — 所有平台的 token 存储均经过加密，不以明文形式暴露
- **AC17**: UDID 跨平台可用 — Tauri/Capacitor/Standalone 三种平台均能正确获取设备 UDID
