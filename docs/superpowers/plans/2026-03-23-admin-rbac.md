# Admin RBAC Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add role-based access control to `/app/*` admin routes, enabling limited-permission users (AI agents, employees) to access specific admin endpoints without full superadmin privileges.

**Architecture:** Extend the existing `Roles uint64` bitmask on `User` with three new role constants (`RoleOpsViewer`, `RoleOpsEditor`, `RoleSupport`). Add a single `RoleRequired(role uint64)` middleware. Split the `/app` Gin group — 24 routes move from the `AdminRequired()` group to a new `AuthRequired()` group with per-route `RoleRequired()` checks.

**Tech Stack:** Go 1.24, Gin, GORM, Cobra (CLI). No new dependencies.

---

## File Map

| File | Change |
|------|--------|
| `api/type.go` | Add 3 role constants + extend `RoleNames` + add `RoleByName` map |
| `api/middleware.go` | Add `RoleRequired(role uint64)` function |
| `api/middleware_mock_test.go` | Add `RoleRequired` unit tests (no DB needed) |
| `api/route.go` | Remove 24 routes from `admin` group; add new `opsAdmin` group |
| `api/logic_user.go` | Add `SetUserRoles(ctx, email, roles)` function |
| `api/api_admin_user.go` | Add `PUT /app/users/:uuid/roles` handler |
| `api/cmd/user.go` | Add `user set-roles` CLI subcommand |

---

## Task 1: Add role constants to `type.go`

**Files:**
- Modify: `api/type.go`

- [ ] **Step 1: Add three role constants**

In `api/type.go`, replace the comment `// 预留 1<<4 到 1<<63 用于未来扩展` with the new constants:

```go
const (
	// RoleUser 普通用户（默认角色）
	RoleUser uint64 = 1 << 0 // 1

	// RoleCMSAdmin CMS 管理员（可以管理 Payload CMS 所有内容）
	RoleCMSAdmin uint64 = 1 << 1 // 2

	// RoleCMSEditor CMS 编辑（只能编辑自己的内容）
	RoleCMSEditor uint64 = 1 << 2 // 4

	// RoleSuper 超级管理员（拥有所有权限）
	RoleSuper uint64 = 1 << 3 // 8

	// RoleOpsViewer 运维只读（节点/隧道/云实例/用户/日志/工单 只读）
	RoleOpsViewer uint64 = 1 << 4 // 16

	// RoleOpsEditor 运维读写（含 OpsViewer 所有权限 + 节点/隧道/云实例变更）
	RoleOpsEditor uint64 = 1 << 5 // 32

	// RoleSupport 工单处理（工单状态变更 + 设备日志读）
	RoleSupport uint64 = 1 << 6 // 64
)
```

- [ ] **Step 2: Extend `RoleNames` map**

Replace the existing `RoleNames` var with:

```go
var RoleNames = map[uint64]string{
	RoleUser:      "user",
	RoleCMSAdmin:  "cms_admin",
	RoleCMSEditor: "cms_editor",
	RoleSuper:     "super",
	RoleOpsViewer: "ops_viewer",
	RoleOpsEditor: "ops_editor",
	RoleSupport:   "support",
}
```

- [ ] **Step 3: Add `RoleByName` inverse map (after `RoleNames`)**

```go
// RoleByName 角色名称到位掩码的反向映射（用于 CLI 和 API 赋权）
var RoleByName = map[string]uint64{
	"user":       RoleUser,
	"cms_admin":  RoleCMSAdmin,
	"cms_editor": RoleCMSEditor,
	"super":      RoleSuper,
	"ops_viewer": RoleOpsViewer,
	"ops_editor": RoleOpsEditor,
	"support":    RoleSupport,
}
```

- [ ] **Step 4: Verify it compiles**

```bash
cd api && go build ./...
```

Expected: no errors.

- [ ] **Step 5: Commit**

```bash
git add api/type.go
git commit -m "feat(rbac): add OpsViewer/OpsEditor/Support role constants and RoleByName map"
```

---

## Task 2: Add `RoleRequired` middleware with unit tests

**Files:**
- Modify: `api/middleware.go`
- Modify: `api/middleware_mock_test.go`

- [ ] **Step 1: Write the failing tests**

Add the following test block to the end of `api/middleware_mock_test.go`:

```go
// ===================== RoleRequired 中间件测试（不需要数据库） =====================

// createRoleTestRouter 创建带角色检查的测试路由器
// 直接注入 authContext，绕过数据库查询
func createRoleTestRouter(user *User, requiredRole uint64) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(gin.Recovery())

	// 直接注入认证上下文（模拟已登录用户）
	r.Use(func(c *gin.Context) {
		if user != nil {
			c.Set("authContext", &authContext{UserID: user.ID, User: user})
		}
		c.Next()
	})

	r.GET("/test", RoleRequired(requiredRole), func(c *gin.Context) {
		c.JSON(200, gin.H{"code": 0})
	})
	return r
}

// assertForbidden 断言权限不足（业务错误码 403）
func assertForbidden(t *testing.T, w *httptest.ResponseRecorder) {
	t.Helper()
	assert.Equal(t, 200, w.Code, "HTTP status should be 200 (API convention)")
	var resp struct{ Code int `json:"code"` }
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	assert.Equal(t, int(ErrorForbidden), resp.Code, "Business code should be 403 (forbidden)")
}

// TestRoleRequired_NoUser 未登录用户被拒绝
func TestRoleRequired_NoUser(t *testing.T) {
	testInitConfig()
	r := createRoleTestRouter(nil, RoleOpsViewer)
	req, _ := http.NewRequest("GET", "/test", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	assertAuthFailed(t, w)
}

// TestRoleRequired_ExactRole 拥有精确角色的用户通过
func TestRoleRequired_ExactRole(t *testing.T) {
	testInitConfig()
	user := &User{ID: 1, Roles: RoleOpsViewer}
	r := createRoleTestRouter(user, RoleOpsViewer)
	req, _ := http.NewRequest("GET", "/test", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	assertAuthSuccess(t, w)
}

// TestRoleRequired_WrongRole 拥有不同角色的用户被拒绝
func TestRoleRequired_WrongRole(t *testing.T) {
	testInitConfig()
	user := &User{ID: 2, Roles: RoleSupport} // 有 Support，但需要 OpsViewer
	r := createRoleTestRouter(user, RoleOpsViewer)
	req, _ := http.NewRequest("GET", "/test", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	assertForbidden(t, w)
}

// TestRoleRequired_RoleUser_Denied 普通用户（RoleUser）被拒绝
func TestRoleRequired_RoleUser_Denied(t *testing.T) {
	testInitConfig()
	user := &User{ID: 3, Roles: RoleUser}
	r := createRoleTestRouter(user, RoleOpsViewer)
	req, _ := http.NewRequest("GET", "/test", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	assertForbidden(t, w)
}

// TestRoleRequired_IsAdmin_Bypass 超级管理员（IsAdmin=true）直接通过，不检查 Roles
func TestRoleRequired_IsAdmin_Bypass(t *testing.T) {
	testInitConfig()
	isAdmin := true
	user := &User{ID: 4, Roles: RoleUser, IsAdmin: &isAdmin} // Roles 没有 OpsViewer，但 IsAdmin=true
	r := createRoleTestRouter(user, RoleOpsViewer)
	req, _ := http.NewRequest("GET", "/test", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	assertAuthSuccess(t, w)
}

// TestRoleRequired_CombinedRole_EitherSuffices 组合角色检查（任一满足即通过）
func TestRoleRequired_CombinedRole_EitherSuffices(t *testing.T) {
	testInitConfig()
	viewOrEdit := RoleOpsViewer | RoleOpsEditor

	// 只有 OpsViewer：应通过
	t.Run("viewer passes viewOrEdit check", func(t *testing.T) {
		user := &User{ID: 5, Roles: RoleOpsViewer}
		r := createRoleTestRouter(user, viewOrEdit)
		req, _ := http.NewRequest("GET", "/test", nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		assertAuthSuccess(t, w)
	})

	// 只有 OpsEditor：应通过
	t.Run("editor passes viewOrEdit check", func(t *testing.T) {
		user := &User{ID: 6, Roles: RoleOpsEditor}
		r := createRoleTestRouter(user, viewOrEdit)
		req, _ := http.NewRequest("GET", "/test", nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		assertAuthSuccess(t, w)
	})

	// 只有 Support（不在组合内）：应被拒绝
	t.Run("support denied for viewOrEdit check", func(t *testing.T) {
		user := &User{ID: 7, Roles: RoleSupport}
		r := createRoleTestRouter(user, viewOrEdit)
		req, _ := http.NewRequest("GET", "/test", nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		assertForbidden(t, w)
	})
}

// TestRoleRequired_MultipleRoles 用户拥有多个角色时，任一满足即通过
func TestRoleRequired_MultipleRoles(t *testing.T) {
	testInitConfig()
	// 用户同时有 OpsViewer + Support
	user := &User{ID: 8, Roles: RoleOpsViewer | RoleSupport}

	// 检查需要 OpsEditor：应失败（没有 OpsEditor bit）
	t.Run("missing editor role denied", func(t *testing.T) {
		r := createRoleTestRouter(user, RoleOpsEditor)
		req, _ := http.NewRequest("GET", "/test", nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		assertForbidden(t, w)
	})

	// 检查需要 Support：应通过
	t.Run("has support role passes", func(t *testing.T) {
		r := createRoleTestRouter(user, RoleSupport)
		req, _ := http.NewRequest("GET", "/test", nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		assertAuthSuccess(t, w)
	})
}
```

- [ ] **Step 2: Run tests to verify they fail**

```bash
cd api && go test -run TestRoleRequired ./...
```

Expected: compilation fails with "undefined: RoleRequired".

- [ ] **Step 3: Implement `RoleRequired` in `middleware.go`**

Add after the `AdminRequired()` function:

```go
// RoleRequired 细粒度权限检查：IsAdmin=true 直接通过；否则检查 user.Roles 是否包含指定角色。
// role 参数支持位或组合：RoleRequired(RoleOpsViewer | RoleOpsEditor) 表示任一满足即通过。
// 权限来源：从 DB 加载的 User 结构体（通过 ReqUser(c)），与 AdminRequired() 读取 IsAdmin 一致。
// 角色变更立即生效（下次请求），无需重新签发 token。
func RoleRequired(role uint64) gin.HandlerFunc {
	return func(c *gin.Context) {
		user := ReqUser(c)
		if user == nil {
			Error(c, ErrorNotLogin, "authentication failed")
			c.Abort()
			return
		}
		if user.IsAdmin != nil && *user.IsAdmin {
			c.Next()
			return
		}
		if !HasRole(user.Roles, role) {
			log.Warnf(c, "role check failed: need=%d user=%d roles=%d path=%s",
				role, user.ID, user.Roles, c.Request.URL.Path)
			Error(c, ErrorForbidden, "permission denied")
			c.Abort()
			return
		}
		c.Next()
	}
}
```

- [ ] **Step 4: Run tests to verify they pass**

```bash
cd api && go test -run TestRoleRequired ./...
```

Expected: all 8 test functions pass (PASS).

- [ ] **Step 5: Run full test suite to confirm no regressions**

```bash
cd api && go test ./...
```

Expected: PASS (tests requiring `config.yml` will be skipped — that's correct).

- [ ] **Step 6: Commit**

```bash
git add api/middleware.go api/middleware_mock_test.go
git commit -m "feat(rbac): add RoleRequired middleware with unit tests"
```

---

## Task 3: Restructure routes in `route.go`

**Files:**
- Modify: `api/route.go`

**Critical:** The 24 routes below must be **deleted from the `admin` group** before being added to `opsAdmin`. Double-registering the same method+path causes a Gin startup panic.

Routes to move (delete from `admin`, add to `opsAdmin`):

| Lines in current `route.go` | Route |
|-----------------------------|-------|
| 238 | `admin.GET("/tunnels", ...)` |
| 239 | `admin.PUT("/tunnels/:id", ...)` |
| 240 | `admin.DELETE("/tunnels/:id", ...)` |
| 243 | `admin.GET("/nodes", ...)` |
| 244 | `admin.PUT("/nodes/:ipv4", ...)` |
| 245 | `admin.DELETE("/nodes/:ipv4", ...)` |
| 255 | `admin.GET("/users", ...)` |
| 256 | `admin.GET("/users/:uuid", ...)` |
| 272 | `admin.GET("/users/:uuid/devices", ...)` |
| 340 | `admin.GET("/cloud/instances", ...)` |
| 341 | `admin.POST("/cloud/instances/sync", ...)` |
| 342 | `admin.GET("/cloud/instances/:id", ...)` |
| 343 | `admin.POST("/cloud/instances/:id/change-ip", ...)` |
| 344 | `admin.PUT("/cloud/instances/:id/traffic-config", ...)` |
| 345 | `admin.POST("/cloud/instances", ...)` |
| 346 | `admin.DELETE("/cloud/instances/:id", ...)` |
| 347 | `admin.GET("/cloud/accounts", ...)` |
| 348 | `admin.GET("/cloud/regions", ...)` |
| 349 | `admin.GET("/cloud/plans", ...)` |
| 350 | `admin.GET("/cloud/images", ...)` |
| 353 | `admin.GET("/device-logs", ...)` |
| 354 | `admin.GET("/feedback-tickets", ...)` |
| 355 | `admin.PUT("/feedback-tickets/:id/resolve", ...)` |
| 356 | `admin.PUT("/feedback-tickets/:id/close", ...)` |

- [ ] **Step 1: Delete the 24 routes from the `admin` group**

In `route.go`, inside the `admin` group block, delete:
- The entire "隧道管理" section (lines 237-240 including comment)
- The entire "物理节点管理" section (lines 242-245 including comment)
- The three user-read lines: `admin.GET("/users", ...)`, `admin.GET("/users/:uuid", ...)`, `admin.GET("/users/:uuid/devices", ...)`
- The entire "Cloud instance management" section (lines 339-350 including comment)
- The entire "Device logs & feedback tickets" section (lines 352-356 including comment)

After deletion, the `admin` group should retain: plans, user-write routes, device statistics, user/order statistics, retailers, admins list, wallet, orders, campaigns, EDM, stats/overview, strategy.

- [ ] **Step 2: Add the `opsAdmin` group after the `admin` group closing brace**

Insert immediately after the `}` that closes the `admin` group block (before the GitHub Issues comment):

```go
// opsAdmin 运维权限路由组：不需要超级管理员，通过角色位掩码控制访问
// 超级管理员（IsAdmin=true）经由 RoleRequired 内部 bypass 直接通过
opsAdmin := r.Group("/app")
log.Debugf(ctx, "registering /app opsAdmin group")
opsAdmin.Use(log.MiddlewareRequestLog(true), MiddleRecovery(), CORSMiddleware(), AuthRequired())
{
	viewOrEdit  := RoleOpsViewer | RoleOpsEditor
	allOpsRoles := RoleOpsViewer | RoleOpsEditor | RoleSupport

	// 隧道管理
	opsAdmin.GET("/tunnels",        RoleRequired(viewOrEdit),    api_admin_list_tunnels)
	opsAdmin.PUT("/tunnels/:id",    RoleRequired(RoleOpsEditor), api_admin_update_tunnel)
	opsAdmin.DELETE("/tunnels/:id", RoleRequired(RoleOpsEditor), api_admin_delete_tunnel)

	// 物理节点管理
	opsAdmin.GET("/nodes",          RoleRequired(viewOrEdit),    api_admin_list_nodes)
	opsAdmin.PUT("/nodes/:ipv4",    RoleRequired(RoleOpsEditor), api_admin_update_node)
	opsAdmin.DELETE("/nodes/:ipv4", RoleRequired(RoleOpsEditor), api_admin_delete_node)

	// 云实例（只读）
	opsAdmin.GET("/cloud/instances",     RoleRequired(viewOrEdit), api_admin_list_cloud_instances)
	opsAdmin.GET("/cloud/instances/:id", RoleRequired(viewOrEdit), api_admin_get_cloud_instance)
	opsAdmin.GET("/cloud/accounts",      RoleRequired(viewOrEdit), api_admin_list_cloud_accounts)
	opsAdmin.GET("/cloud/regions",       RoleRequired(viewOrEdit), api_admin_list_cloud_regions)
	opsAdmin.GET("/cloud/plans",         RoleRequired(viewOrEdit), api_admin_list_cloud_plans)
	opsAdmin.GET("/cloud/images",        RoleRequired(viewOrEdit), api_admin_list_cloud_images)

	// 云实例（读写）
	opsAdmin.POST("/cloud/instances/sync",                RoleRequired(RoleOpsEditor), api_admin_sync_all_cloud_instances)
	opsAdmin.POST("/cloud/instances/:id/change-ip",       RoleRequired(RoleOpsEditor), api_admin_change_ip_cloud_instance)
	opsAdmin.PUT("/cloud/instances/:id/traffic-config",   RoleRequired(RoleOpsEditor), api_admin_update_traffic_config)
	opsAdmin.POST("/cloud/instances",                     RoleRequired(RoleOpsEditor), api_admin_create_cloud_instance)
	opsAdmin.DELETE("/cloud/instances/:id",               RoleRequired(RoleOpsEditor), api_admin_delete_cloud_instance)

	// 用户查看（只读）
	opsAdmin.GET("/users",               RoleRequired(viewOrEdit), api_admin_list_users)
	opsAdmin.GET("/users/:uuid",         RoleRequired(viewOrEdit), api_admin_get_user_detail)
	opsAdmin.GET("/users/:uuid/devices", RoleRequired(viewOrEdit), api_admin_get_user_devices)

	// 设备日志 + 工单
	opsAdmin.GET("/device-logs",                  RoleRequired(allOpsRoles), api_admin_list_device_logs)
	opsAdmin.GET("/feedback-tickets",             RoleRequired(allOpsRoles), api_admin_list_feedback_tickets)
	opsAdmin.PUT("/feedback-tickets/:id/resolve", RoleRequired(RoleSupport), api_admin_resolve_feedback_ticket)
	opsAdmin.PUT("/feedback-tickets/:id/close",   RoleRequired(RoleSupport), api_admin_close_feedback_ticket)
}
```

- [ ] **Step 3: Verify it compiles and starts without panic**

```bash
cd api && go build ./...
```

Expected: compiles with no errors. If you see `panic: ... wildcard route conflicts`, a route was double-registered — check that all 24 routes were removed from `admin`.

- [ ] **Step 4: Verify tests still pass**

```bash
cd api && go test ./...
```

Expected: same pass/skip results as before this task.

- [ ] **Step 5: Commit**

```bash
git add api/route.go
git commit -m "feat(rbac): split admin routes — move 24 routes to opsAdmin group with RoleRequired"
```

---

## Task 4: Add `SetUserRoles` logic function

**Files:**
- Modify: `api/logic_user.go`

This function is shared by both the CLI command (Task 6) and the admin API handler (Task 5).

- [ ] **Step 1: Add `SetUserRoles` to `logic_user.go`**

Find the `SetUserAdminStatus` function in `logic_user.go` and add the following immediately after it:

```go
// SetUserRoles 设置用户角色位掩码（replace-all 语义）
// roleNames 是角色名称列表，对应 RoleByName 中的键（如 "ops_viewer", "ops_editor", "support"）
// RoleUser bit 始终保留，不可被清除
// 返回写入后的角色值（含 RoleUser）
func SetUserRoles(ctx context.Context, email string, roleNames []string) (uint64, error) {
	identify, err := GetLoginIdentifyByEmail(ctx, email)
	if err != nil {
		log.Errorf(ctx, "failed to find user by email %s: %v", email, err)
		return 0, err
	}

	// 解析角色名称，未知名称报错
	var newRoles uint64 = RoleUser // 始终保留 RoleUser
	for _, name := range roleNames {
		bit, ok := RoleByName[name]
		if !ok {
			return 0, fmt.Errorf("unknown role name: %q (valid: %v)", name, validRoleNames())
		}
		newRoles |= bit
	}

	if err := db.Get().Model(&User{}).Where("id = ?", identify.UserID).Update("roles", newRoles).Error; err != nil {
		log.Errorf(ctx, "failed to update roles for user %d: %v", identify.UserID, err)
		return 0, err
	}

	log.Infof(ctx, "roles updated for user %d: %d (%v)", identify.UserID, newRoles, GetRoleNames(newRoles))
	return newRoles, nil
}

// validRoleNames 返回所有合法角色名称列表（用于错误提示）
func validRoleNames() []string {
	names := make([]string, 0, len(RoleByName))
	for name := range RoleByName {
		names = append(names, name)
	}
	return names
}
```

Check what imports are already present in `logic_user.go`. The function needs `context`, `fmt`, and the `db` package — these should already be imported. If `fmt` is missing, add it to the import block.

- [ ] **Step 2: Compile**

```bash
cd api && go build ./...
```

Expected: no errors.

- [ ] **Step 3: Commit**

```bash
git add api/logic_user.go
git commit -m "feat(rbac): add SetUserRoles logic function with replace-all semantics"
```

---

## Task 5: Add `PUT /app/users/:uuid/roles` admin API endpoint

**Files:**
- Modify: `api/api_admin_user.go`
- Modify: `api/route.go`

- [ ] **Step 1: Add request/response types and handler to `api_admin_user.go`**

Open `api_admin_user.go`. Find the end of the file and add:

```go
// reqAdminSetUserRoles PUT /app/users/:uuid/roles 请求体
type reqAdminSetUserRoles struct {
	Roles []string `json:"roles" binding:"required"`
}

// respAdminSetUserRoles PUT /app/users/:uuid/roles 响应体
type respAdminSetUserRoles struct {
	Roles     uint64   `json:"roles"`
	RoleNames []string `json:"roleNames"`
}

// api_admin_set_user_roles 设置用户角色（超级管理员专用，replace-all 语义）
func api_admin_set_user_roles(c *gin.Context) {
	uuid := c.Param("uuid")

	var req reqAdminSetUserRoles
	if err := c.ShouldBindJSON(&req); err != nil {
		Error(c, ErrorInvalidParams, "invalid request body")
		return
	}

	// 查找用户
	var user User
	if err := db.Get().Where("uuid = ?", uuid).First(&user).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			Error(c, ErrorNotFound, "user not found")
			return
		}
		log.Errorf(c, "failed to find user %s: %v", uuid, err)
		Error(c, ErrorInternal, "database error")
		return
	}

	// 查找 email 用于 SetUserRoles（SetUserRoles 通过 email 查找）
	// 直接用 user.ID 更新，避免二次查找
	var newRoles uint64 = RoleUser
	for _, name := range req.Roles {
		bit, ok := RoleByName[name]
		if !ok {
			Error(c, ErrorInvalidParams, fmt.Sprintf("unknown role: %q", name))
			return
		}
		newRoles |= bit
	}

	if err := db.Get().Model(&user).Update("roles", newRoles).Error; err != nil {
		log.Errorf(c, "failed to update roles for user %s: %v", uuid, err)
		Error(c, ErrorInternal, "failed to update roles")
		return
	}

	log.Infof(c, "admin set roles for user %s (id=%d): %d (%v)",
		uuid, user.ID, newRoles, GetRoleNames(newRoles))

	Success(c, &respAdminSetUserRoles{
		Roles:     newRoles,
		RoleNames: GetRoleNames(newRoles),
	})
}
```

Check the import block in `api_admin_user.go`. Add `"fmt"` to the import list — it is not currently imported. `errors` and `gorm.io/gorm` are already present.

- [ ] **Step 2: Register the route in `route.go`**

In `route.go`, inside the `admin` group block, find the user management section (around the `PUT /users/:uuid/email` line) and add:

```go
// 用户角色管理（仅超级管理员）
admin.PUT("/users/:uuid/roles", api_admin_set_user_roles)
```

Add it after the `PUT /users/:uuid/email` line.

- [ ] **Step 3: Compile**

```bash
cd api && go build ./...
```

Expected: no errors. If `ErrorNotFound` is undefined, check `response.go` for the correct constant name (look for 404 — it might be `ErrorNotFound` or similar).

- [ ] **Step 4: Commit**

```bash
git add api/api_admin_user.go api/route.go
git commit -m "feat(rbac): add PUT /app/users/:uuid/roles endpoint for superadmin role management"
```

---

## Task 6: Add `user set-roles` CLI subcommand

**Files:**
- Modify: `api/cmd/user.go`

- [ ] **Step 1: Add the `userSetRolesCmd` command to `cmd/user.go`**

Find `var userDelRetailerCmd` in `cmd/user.go` and add the new command after it:

```go
var userSetRolesCmd = &cobra.Command{
	Use:   "set-roles",
	Short: "Set roles for a user (replace-all semantics, RoleUser always preserved)",
	Long: `Set the role bitmask for a user identified by email.

Semantics: replace-all. The --roles list becomes the complete new role set.
RoleUser bit is always preserved regardless of input.
To add a single role without removing others, include all current roles in --roles.

Valid role names: user, cms_admin, cms_editor, super, ops_viewer, ops_editor, support

Examples:
  # Grant ops viewer + support roles
  center user set-roles --email ai@example.com --roles ops_viewer,support -c config.yml

  # Grant full ops roles (viewer + editor + support)
  center user set-roles --email employee@example.com --roles ops_viewer,ops_editor,support -c config.yml

  # Reset to plain user (no admin roles)
  center user set-roles --email user@example.com --roles user -c config.yml
`,
	Run: func(cmd *cobra.Command, args []string) {
		email, _ := cmd.Flags().GetString("email")
		rolesFlag, _ := cmd.Flags().GetString("roles")

		if rolesFlag == "" {
			fmt.Println("Error: --roles is required (e.g. --roles ops_viewer,ops_editor)")
			return
		}

		roleNames := strings.Split(rolesFlag, ",")
		// 去除空格
		for i, name := range roleNames {
			roleNames[i] = strings.TrimSpace(name)
		}

		newRoles, err := center.SetUserRoles(context.Background(), email, roleNames)
		if err != nil {
			fmt.Printf("Error setting roles: %v\n", err)
			return
		}
		fmt.Printf("Roles updated for user %s\n", email)
		fmt.Printf("  Bitmask: %d\n", newRoles)
		fmt.Printf("  Names:   %v\n", center.GetRoleNames(newRoles))
	},
}
```

- [ ] **Step 2: Register the command in `init()`**

In the `init()` function in `cmd/user.go`, find where `userSendEmailCmd` flags are set and add before the `userCmd.AddCommand(...)` call:

```go
// Set-roles subcommand
userSetRolesCmd.Flags().String("email", "", "User's email address")
userSetRolesCmd.Flags().String("roles", "", "Comma-separated role names (e.g. ops_viewer,ops_editor,support)")
userSetRolesCmd.MarkFlagRequired("email")
userSetRolesCmd.MarkFlagRequired("roles")
```

Then add `userSetRolesCmd` to the `userCmd.AddCommand(...)` call:

```go
userCmd.AddCommand(
    userAddCmd,
    userSetAdminCmd,
    userDelAdminCmd,
    userSetRetailerCmd,
    userDelRetailerCmd,
    userSendEmailCmd,
    userSetRolesCmd,   // new
)
```

- [ ] **Step 3: Compile the CLI binary**

```bash
cd api/cmd && go build -o kaitu-center .
```

Expected: no errors.

- [ ] **Step 4: Verify help output**

```bash
./kaitu-center user set-roles --help
```

Expected: shows usage with `--email`, `--roles`, `-c` flags and the role examples.

- [ ] **Step 5: Commit**

```bash
git add api/cmd/user.go
git commit -m "feat(rbac): add user set-roles CLI subcommand"
```

---

## Task 7: Final verification

- [ ] **Step 1: Run all tests**

```bash
cd api && go test ./...
```

Expected: all tests PASS or SKIP (skipped = requires `config.yml`, not a failure).

- [ ] **Step 2: Build the full binary**

```bash
cd api/cmd && go build -o kaitu-center . && echo "BUILD OK"
```

Expected: `BUILD OK`.

- [ ] **Step 3: Smoke-test CLI help**

```bash
./kaitu-center user --help
```

Expected: lists `add`, `set-admin`, `del-admin`, `set-retailer`, `del-retailer`, `send-email`, `set-roles` subcommands.

- [ ] **Step 4: Final commit**

```bash
git add -p  # review any unstaged changes
git commit -m "feat(rbac): admin RBAC — OpsViewer/OpsEditor/Support roles complete"
```
