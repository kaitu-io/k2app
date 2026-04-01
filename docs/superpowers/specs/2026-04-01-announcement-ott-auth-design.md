# Announcement OTT Auth — 废弃 Survey Banner，统一为 Announcement + 一次性令牌登录

**Date**: 2026-04-01
**Status**: Draft
**Scope**: API, Webapp, Web, MCP Tools

## Background

当前 webapp 有独立的 `SurveyBanner` 组件，硬编码问卷配置，通过 `openExternal` 打开浏览器。用户需要在浏览器中重新登录才能填写问卷。

同时已有完整的 Announcement 系统（数据库驱动、manager 后台 CRUD、支持 `openMode: external | webview`），但不支持携带 auth 信息。

## Goals

1. 废弃独立的 Survey Banner，问卷推送统一通过 Announcement 系统管理
2. Announcement 新增 `authMode` 字段，支持打开链接时自动传递登录态
3. 设计通用的 OTT（One-Time Token）机制，webapp auth → web cookie session，适用于所有需要 auth 的 webview 场景

## Non-Goals

- 不改造 Survey 页面本身（已有 cookie-based auth 判断，天然兼容）
- 不改造 Survey API（问卷提交逻辑保留）

---

## Design

### 1. OTT 签发与交换（API 层）

#### `POST /api/auth/ott` — 签发（需要 auth）

**Request:**
```json
{
  "redirect": "https://kaitu.io/survey/active_2026q1"
}
```

**Validation:**
- `redirect` URL scheme 必须是 `https`，host 必须精确匹配 `kaitu.io` 或以 `.kaitu.io` 结尾（防止 `evil-kaitu.io` 绕过）
- 用户必须已认证（现有 auth middleware）

**Route registration:** 注册在需要 auth 的路由组中（与 `/api/user/*` 同组），确保 Bearer token 验证。

**Logic:**
1. 生成 32 字节随机 token（hex 编码，256 bit entropy）
2. 存入 Redis: `ott:{token}` → `{ "user_id": <int>, "redirect": "<url>" }`，TTL 300 秒（5 分钟）
3. 拼接完整 exchange URL

**Response:**
```json
{
  "url": "https://kaitu.io/api/auth/ott/exchange?ott=<token>&redirect=<encoded_url>"
}
```

后端拼好完整 URL，webapp 不需要自己拼。

#### `GET /api/auth/ott/exchange` — 交换（无需 auth）

**Route registration:** 注册为公共路由（无 auth middleware），与 `/api/auth/login` 同组。

**Query params:** `ott`, `redirect`

**Logic:**
1. Redis `GET ott:{ott}` → 取出 `user_id` + 预期 `redirect`
2. 校验 `redirect` 参数与 Redis 存储一致（防篡改）
3. Redis `DEL ott:{ott}`（一次性，用后即废）
4. 根据 `user_id` 查询用户（含 `Roles`），调用现有 `generateWebCookieToken(c, userID, roles)` 生成 `DataAuthResult`（与 web 登录 `api_web_auth` 共用同一 cookie token 生成逻辑）
5. 调用 `setAuthCookies(c, authResult)` 设置 HttpOnly `access_token` + `csrf_token` cookie
6. HTTP 302 重定向到 `redirect` URL

**Failure cases:**
- OTT 无效 / 过期 / 已使用 → 302 到 `/auth/login?reason=expired`
- redirect 不匹配 → 302 到 `/auth/login?reason=invalid`

#### Redis key 设计

```
Key:    ott:<hex_token>
Value:  {"user_id": 123, "redirect": "https://kaitu.io/survey/active_2026q1"}
TTL:    300s
```

### 2. Announcement 数据模型扩展

#### 数据库

`announcements` 表新增字段（GORM AutoMigrate）：

```go
AuthMode string `gorm:"type:varchar(20);not null;default:'none'"` // "none" | "ott"
```

- `none` — 默认，打开链接不带 auth（现有行为）
- `ott` — 打开前先请求 OTT，拼入 URL 后再打开

#### API 验证

创建 / 更新时验证 `authMode` 只能是 `none` 或 `ott`，与现有 `openMode` 验证方式一致。`AnnouncementRequest` 结构体新增 `AuthMode` 字段。

#### Admin 响应

`AnnouncementResponse` 结构体和 `convertAnnouncementToResponse()` 函数新增 `AuthMode` 映射。

#### 公共下发

`/api/app/config` 响应的 `DataAnnouncement` 新增 `authMode` 字段。`getActiveAnnouncement()` 函数新增 `AuthMode: announcement.AuthMode` 映射行：

```go
type DataAnnouncement struct {
    ID        string `json:"id"`
    Message   string `json:"message"`
    LinkURL   string `json:"linkUrl,omitempty"`
    LinkText  string `json:"linkText,omitempty"`
    OpenMode  string `json:"openMode,omitempty"`  // "external" | "webview"
    AuthMode  string `json:"authMode,omitempty"`  // "none" | "ott"
    ExpiresAt int64  `json:"expiresAt,omitempty"`
}
```

### 3. Webapp — AnnouncementBanner 链接打开逻辑

#### `handleLinkClick` 改造

```
if authMode === 'ott' && 用户已登录:
    1. POST /api/auth/ott { redirect: linkUrl }
    2. 获得完整 URL（含 ott）
    3. 用 openMode 决定打开方式（external / webview）
else:
    现有行为（直接打开 linkUrl）
```

#### 错误处理

- OTT 请求失败 → fallback 直接打开原始 URL（用户到目标页自行登录）
- 未登录用户点击 `authMode=ott` 的公告 → 直接打开原始 URL

#### 类型变更

`webapp/src/services/api-types.ts` 的 `Announcement` 接口新增：

```typescript
authMode?: 'none' | 'ott';
```

### 4. Survey Banner 废弃

删除以下文件 / 代码：

| 位置 | 变更 |
|------|------|
| `webapp/src/components/SurveyBanner.tsx` | 删除文件 |
| `webapp/src/components/Layout.tsx` | 移除 `<SurveyBanner />` 引用 |
| `webapp/src/stores/vpn-machine.store.ts` | 移除连接计数逻辑（`k2_connect_success_count`） |
| `webapp/src/i18n/locales/*/common.json` (7 locales) | 移除 `survey.banner_*` key |

问卷推送改为：通过 manager 后台创建 announcement，设置 `authMode=ott`、`openMode` 按需选择、`linkUrl` 指向问卷页面。

### 5. Web — Manager 后台

`web/src/app/(manager)/manager/announcements/page.tsx` 创建/编辑表单新增：

- `authMode` 下拉：`"不需要登录"` (`none`) / `"自动登录"` (`ott`)

`web/src/lib/api.ts` 的 `AnnouncementRequest` / `AnnouncementResponse` 新增 `authMode` 字段。

### 6. MCP 工具

`tools/kaitu-center/src/tools/admin-announcements.ts`：

- `create_announcement` 新增 `auth_mode` 参数（`"none"` | `"ott"`，默认 `"none"`）
- `update_announcement` 新增 `auth_mode` 参数

---

## Security

| 约束 | 实现 |
|------|------|
| 一次性 | Redis DEL 在 exchange 时立即执行 |
| 短时效 | Redis TTL 300s，过期自动清理 |
| 防篡改 redirect | exchange 时校验 URL 参数与 Redis 存储一致 |
| 防 open redirect | 签发时校验 redirect: scheme 必须 `https`，host 精确匹配 `kaitu.io` 或以 `.kaitu.io` 结尾 |
| 防枚举 | 32 字节随机 token（256 bit entropy） |
| 限频 | 复用现有 auth middleware rate limit |
| 无新 session 类型 | exchange 后设置的 cookie 与现有 web 登录完全一致 |

OTT 不携带权限信息，只是 userId 的临时引用。目标页面的权限检查（如 survey 的会员检查）仍由各页面自行负责。

---

## Change Summary

### API (`api/`)
- **新增** `api_auth_ott.go`：OTT 签发 + 交换两个 handler
- **改动** `model.go`：Announcement 新增 `AuthMode` 字段
- **改动** `api_admin_announcements.go`：创建/更新时 `authMode` 验证
- **改动** `api_app_config.go`：`DataAnnouncement` 新增 `AuthMode` 下发
- **改动** `route.go`：注册 OTT 路由

### Webapp (`webapp/`)
- **改动** `AnnouncementBanner.tsx`：`handleLinkClick` 增加 OTT 逻辑
- **改动** `api-types.ts`：`Announcement` 新增 `authMode`
- **删除** `SurveyBanner.tsx`
- **改动** `Layout.tsx`：移除 `<SurveyBanner />`
- **改动** `vpn-machine.store.ts`：移除连接计数
- **改动** 7 locale `common.json`：移除 `survey.banner_*`

### Web (`web/`)
- **改动** `manager/announcements/page.tsx`：表单新增 `authMode`
- **改动** `api.ts`：类型新增 `authMode`

### MCP Tools (`tools/kaitu-center/`)
- **改动** `admin-announcements.ts`：两个工具新增 `auth_mode` 参数

### 不变
- Survey 页面（`web/src/app/[locale]/survey/`）无需改动
- Survey API（`api/api_survey.go`）保留
