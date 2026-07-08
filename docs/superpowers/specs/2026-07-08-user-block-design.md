# 用户封禁（黑名单）设计

## 背景

需要一套机制应对恶意/违规用户：管理员在后台标记某个账号为"封禁"，之后该账号的所有登录尝试与已登录会话都应被拒绝服务，直到管理员解封。

## 范围与非目标

- 只针对**已存在的账号**（按 `User` 行封禁），不做"预先拉黑一个从未注册过的邮箱"。这类邮箱本来就无法登录，无需处理。
- 不做审批流程（Maker-Checker）——管理员直接执行，生效即时。
- 不做批量封禁 / 批量解封。
- 不引入新的业务错误码，不改动 `webapp/src/utils/errorCode.ts`。
- 不记录封禁原因、操作人时间线等审计细节字段——复用现有的 `WriteAuditLog` 机制即可，不再加专门字段。

## 数据模型

在既有 `User` 表加一列（`api/model.go`），风格对齐 `IsActivated`/`IsAdmin`/`IsRetailer`：

```go
IsBlocked *bool `gorm:"default:false"`
```

不新增表。`AutoMigrate` 已包含 `&User{}`（`api/migrate.go`），加字段自动生效，无需额外迁移代码。

## 后端改动

### 1. 拦截点

在以下位置读取到 `User` 行后立即判断 `IsBlocked`，命中则 `Error(c, ErrorForbidden, "account blocked")` 并提前返回（不新增错误码，复用现有 403）：

| 位置 | 文件 | 说明 |
|------|------|------|
| `api_login` | `api_auth.go` | 设备验证码登录；在验证码校验前判断 |
| `api_web_auth` | `api_auth.go` | 网页验证码登录 |
| `api_password_login` | `api_auth.go` | 设备密码登录 |
| `api_web_password_login` | `api_auth.go` | 网页密码登录 |
| `api_send_auth_code` | `api_auth.go` | 仅当 `userExists == true` 时才判断（未注册邮箱谈不上封禁） |
| `AuthRequired()` | `middleware.go` | 覆盖已登录、token 尚未过期的老会话；`ctx.User` 已经加载好，直接判断字段，零额外查询 |
| `AuthOptional()` | `middleware.go` | 命中时不 abort，按匿名处理（不挂载 authContext），避免误伤允许匿名访问的接口 |

判断逻辑统一走一个小 helper（放 `logic_auth.go`）：

```go
func isUserBlocked(u *User) bool {
	return u != nil && u.IsBlocked != nil && *u.IsBlocked
}
```

### 2. 管理员 API（`api_admin_user.go`）

对齐现有 `api_admin_set_user_roles` / `api_admin_generate_access_key` 的风格（`:uuid` 路径参数，直接执行，`WriteAuditLog` 留痕）：

```go
// POST /app/users/:uuid/block
func api_admin_block_user(c *gin.Context)

// POST /app/users/:uuid/unblock
func api_admin_unblock_user(c *gin.Context)
```

路由注册（`route.go`，加在现有 `/users/:uuid/...` admin 分组里）：

```go
admin.POST("/users/:uuid/block", api_admin_block_user)
admin.POST("/users/:uuid/unblock", api_admin_unblock_user)
```

### 3. 响应字段

`DataUser`（`type.go`）加一个字段，跟 `IsRetailer`/`IsAdmin` 一样"仅管理员可见"：

```go
IsBlocked bool `json:"isBlocked,omitempty"`
```

只在 `api_admin_list_users` 和 `api_admin_get_user_detail` 里赋值（`user.IsBlocked != nil && *user.IsBlocked`），普通用户自查接口不填充此字段。

## 前端改动（`web/src/app/(manager)/manager/users/`）

- **位置**：`detail/components/MoreActionsMenu.tsx` 下拉菜单里，"重置密码"和"硬删除用户"之间插入一个菜单项。样式低调（普通样式，不用 destructive 红色、不用醒目图标），跟"重置密码"一致。
- **文案**：根据当前 `isBlocked` 状态显示"封禁用户"或"解封用户"。
- **交互**：单次确认弹窗（不需要像硬删除那样二次确认，封禁是可逆操作）。调用对应接口成功后 `toast` 提示 + `router.refresh()` 刷新数据，菜单文案随之切换。
- **不做**：列表页批量操作、列表页状态标记（除非后续需要）。

### 数据链路

- `web/src/lib/api.ts`：`DataUser` TS 类型加 `isBlocked?: boolean`；新增 `blockUser(uuid)` / `unblockUser(uuid)` 方法，分别调 `POST /app/users/:uuid/block` / `/unblock`
- `detail/page.tsx`：把 `userDetail.isBlocked` 传给 `MoreActionsMenu`，新增 prop `isBlocked: boolean`

## 测试

- 后端：mock DB 测试覆盖 4 个登录入口 + `api_send_auth_code` 在 `IsBlocked=true` 时返回 `ErrorForbidden`；`AuthRequired()` 中间件测试覆盖已登录用户被封禁后下一次请求 403；`AuthOptional()` 测试覆盖降级为匿名而非 abort。
- 前端：不强制要求新增 E2E，人工过一遍详情页封禁/解封交互即可（低风险 UI 改动）。
