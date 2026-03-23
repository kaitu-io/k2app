# Admin RBAC Design

**Date:** 2026-03-23
**Status:** Approved
**Scope:** `api/` — Center API service

## Problem

All `/app/*` admin routes are protected by a single `AdminRequired()` middleware that checks `user.IsAdmin == true`. There is no way to grant a user (AI agent, employee) a subset of admin capabilities without giving them full superadmin access.

## Solution

Extend the existing `Roles uint64` bitmask field on `User` and in JWT claims — already present in the DB and codebase but unused for admin access control — with three new role constants. Add a single new middleware function `RoleRequired(role uint64)`. Move the target admin routes out of the `AdminRequired()` group into a new route group with per-route role checks.

No new dependencies. No DB migration.

## Role Constants (`type.go`)

Three constants appended after `RoleSuper`:

```go
RoleOpsViewer uint64 = 1 << 4 // 16 — read access: nodes, tunnels, cloud, users, device logs, tickets
RoleOpsEditor uint64 = 1 << 5 // 32 — write access: all OpsViewer routes + mutating ops endpoints
RoleSupport   uint64 = 1 << 6 // 64 — ticket resolve/close + device logs read
```

Added to `RoleNames` and new inverse `RoleByName` map:

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

## Access Matrix

| Route | Required role (any bit match) |
|-------|-------------------------------|
| GET /app/tunnels | `RoleOpsViewer \| RoleOpsEditor` |
| PUT /app/tunnels/:id | `RoleOpsEditor` |
| DELETE /app/tunnels/:id | `RoleOpsEditor` |
| GET /app/nodes | `RoleOpsViewer \| RoleOpsEditor` |
| PUT /app/nodes/:ipv4 | `RoleOpsEditor` |
| DELETE /app/nodes/:ipv4 | `RoleOpsEditor` |
| GET /app/cloud/instances | `RoleOpsViewer \| RoleOpsEditor` |
| GET /app/cloud/instances/:id | `RoleOpsViewer \| RoleOpsEditor` |
| GET /app/cloud/accounts | `RoleOpsViewer \| RoleOpsEditor` |
| GET /app/cloud/regions | `RoleOpsViewer \| RoleOpsEditor` |
| GET /app/cloud/plans | `RoleOpsViewer \| RoleOpsEditor` |
| GET /app/cloud/images | `RoleOpsViewer \| RoleOpsEditor` |
| POST /app/cloud/instances/sync | `RoleOpsEditor` |
| POST /app/cloud/instances/:id/change-ip | `RoleOpsEditor` |
| PUT /app/cloud/instances/:id/traffic-config | `RoleOpsEditor` |
| POST /app/cloud/instances | `RoleOpsEditor` |
| DELETE /app/cloud/instances/:id | `RoleOpsEditor` |
| GET /app/users | `RoleOpsViewer \| RoleOpsEditor` |
| GET /app/users/:uuid | `RoleOpsViewer \| RoleOpsEditor` |
| GET /app/users/:uuid/devices | `RoleOpsViewer \| RoleOpsEditor` |
| GET /app/device-logs | `RoleOpsViewer \| RoleOpsEditor \| RoleSupport` |
| GET /app/feedback-tickets | `RoleOpsViewer \| RoleOpsEditor \| RoleSupport` |
| PUT /app/feedback-tickets/:id/resolve | `RoleSupport` |
| PUT /app/feedback-tickets/:id/close | `RoleSupport` |

All other `/app/*` routes remain under the original `admin` group with `AdminRequired()` unchanged.

`IsAdmin=true` (superadmin) bypasses all role checks — this is enforced inside `RoleRequired()`.

## Middleware (`middleware.go`)

New function added alongside existing `AdminRequired()`:

```go
// RoleRequired grants access to users with IsAdmin=true (superadmin bypass)
// or users whose Roles bitmask has any bit in `role` set.
// Pass combined bits for OR semantics: RoleRequired(RoleOpsViewer | RoleOpsEditor).
//
// Authority source: reads user.Roles from the DB-loaded User struct via ReqUser(c),
// identical to how AdminRequired reads user.IsAdmin. Role changes take effect on the
// next request (no token re-issue required) because the user row is read fresh on
// every request — not from the JWT claims.
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

`HasRole` (existing, `type.go:32`) computes `(roles & role) != 0` — correct OR semantics when bits are combined.

`AdminRequired()` is not modified.

### Auth path coverage

`RoleRequired` works for all three auth paths because all three paths call `getAuthContext(c)` and populate `authContext.User` from the DB:

| Auth path | User loaded? | Roles field source |
|-----------|-------------|-------------------|
| JWT Bearer / Cookie | Yes (DB query by UserID) | `user.Roles` from DB row |
| X-Access-Key | Yes (DB query by AccessKey) | `user.Roles` from DB row |
| Device JWT | Yes (DB Preload via Device.User) | `user.Roles` from DB row |

AccessKey-authenticated requests (the primary path for AI agents using the kaitu-ops-mcp tools) are fully supported. The AI agent's user account must have the appropriate `Roles` bits set in the DB — same mechanism as any other user.

## Route Changes (`route.go`)

### Routes removed from `admin` group

The 24 routes listed in the access matrix above are deleted from the `admin` group.

### New `opsAdmin` group

Registered immediately after the `admin` group closes:

```go
opsAdmin := r.Group("/app")
opsAdmin.Use(log.MiddlewareRequestLog(true), MiddleRecovery(), CORSMiddleware(), AuthRequired())
{
    viewOrEdit  := RoleOpsViewer | RoleOpsEditor
    allOpsRoles := RoleOpsViewer | RoleOpsEditor | RoleSupport

    opsAdmin.GET("/tunnels",        RoleRequired(viewOrEdit),    api_admin_list_tunnels)
    opsAdmin.PUT("/tunnels/:id",    RoleRequired(RoleOpsEditor), api_admin_update_tunnel)
    opsAdmin.DELETE("/tunnels/:id", RoleRequired(RoleOpsEditor), api_admin_delete_tunnel)

    opsAdmin.GET("/nodes",          RoleRequired(viewOrEdit),    api_admin_list_nodes)
    opsAdmin.PUT("/nodes/:ipv4",    RoleRequired(RoleOpsEditor), api_admin_update_node)
    opsAdmin.DELETE("/nodes/:ipv4", RoleRequired(RoleOpsEditor), api_admin_delete_node)

    opsAdmin.GET("/cloud/instances",                      RoleRequired(viewOrEdit),    api_admin_list_cloud_instances)
    opsAdmin.GET("/cloud/instances/:id",                  RoleRequired(viewOrEdit),    api_admin_get_cloud_instance)
    opsAdmin.GET("/cloud/accounts",                       RoleRequired(viewOrEdit),    api_admin_list_cloud_accounts)
    opsAdmin.GET("/cloud/regions",                        RoleRequired(viewOrEdit),    api_admin_list_cloud_regions)
    opsAdmin.GET("/cloud/plans",                          RoleRequired(viewOrEdit),    api_admin_list_cloud_plans)
    opsAdmin.GET("/cloud/images",                         RoleRequired(viewOrEdit),    api_admin_list_cloud_images)
    opsAdmin.POST("/cloud/instances/sync",                RoleRequired(RoleOpsEditor), api_admin_sync_all_cloud_instances)
    opsAdmin.POST("/cloud/instances/:id/change-ip",       RoleRequired(RoleOpsEditor), api_admin_change_ip_cloud_instance)
    opsAdmin.PUT("/cloud/instances/:id/traffic-config",   RoleRequired(RoleOpsEditor), api_admin_update_traffic_config)
    opsAdmin.POST("/cloud/instances",                     RoleRequired(RoleOpsEditor), api_admin_create_cloud_instance)
    opsAdmin.DELETE("/cloud/instances/:id",               RoleRequired(RoleOpsEditor), api_admin_delete_cloud_instance)

    opsAdmin.GET("/users",               RoleRequired(viewOrEdit),   api_admin_list_users)
    opsAdmin.GET("/users/:uuid",         RoleRequired(viewOrEdit),   api_admin_get_user_detail)
    opsAdmin.GET("/users/:uuid/devices", RoleRequired(viewOrEdit),   api_admin_get_user_devices)

    opsAdmin.GET("/device-logs",                  RoleRequired(allOpsRoles), api_admin_list_device_logs)
    opsAdmin.GET("/feedback-tickets",             RoleRequired(allOpsRoles), api_admin_list_feedback_tickets)
    opsAdmin.PUT("/feedback-tickets/:id/resolve", RoleRequired(RoleSupport), api_admin_resolve_feedback_ticket)
    opsAdmin.PUT("/feedback-tickets/:id/close",   RoleRequired(RoleSupport), api_admin_close_feedback_ticket)
}
```

### Gin routing collision analysis

Routes staying in `admin` group that share path prefix with `opsAdmin`:

- `GET /app/users/statistics` (static) — takes priority over `GET /app/users/:uuid` (param). No collision.
- `GET /app/devices/statistics`, `GET /app/devices/active` — different prefix from `opsAdmin` routes. No collision.
- `GET /app/orders/statistics` — no overlap. No collision.

Gin panics at startup on duplicate method+path registration. Since routes are moved (not duplicated), no panic.

## Role Management

### CLI (`cmd/`)

New subcommand `user set-roles`:

```bash
./kaitu-center user set-roles \
  -e user@example.com \
  --roles ops_viewer,ops_editor,support \
  -c config.yml
```

Logic: parse `--roles` using `RoleByName`, OR bits together, preserve `RoleUser` bit, write via `db.Get().Model(&User{}).Update("roles", newRoles)`.

### Admin API (`api_admin_user.go`)

New endpoint registered in the `admin` group (superadmin only):

```
PUT /app/users/:uuid/roles
Body:     {"roles": ["ops_viewer", "ops_editor"]}
Response: {"roles": 48, "roleNames": ["ops_viewer", "ops_editor"]}
```

`RoleUser` bit is always preserved. Unknown role name strings return `ErrorInvalidParams`.

## Backward Compatibility

- Existing `IsAdmin=true` users: unaffected. All routes accessible.
- Existing `IsAdmin=false` users with `Roles=1` (RoleUser): cannot access any `/app/*` route (same behavior as before).
- No DB migration required. `users.roles` column exists with `default:1`.
- JWT `roles` field already populated on login. No auth flow changes.
- Frontend (`web/`): login response already returns `Roles uint64` in `DataAdminInfo`. Frontend reads roles to control menu visibility — no API changes required.

## Operational Notes

**Role change latency:** Because `RoleRequired` reads `user.Roles` from the DB-loaded user (not from JWT claims), role changes via `PUT /app/users/:uuid/roles` or the CLI `set-roles` command take effect immediately on the next request. No token invalidation or re-login required.

**JWT `roles` field:** The JWT already carries `roles` as a snapshot from login time. This snapshot is used by the frontend for UI rendering (menu visibility). It does NOT govern server-side access control — the DB value does. These can diverge until the user re-logs-in, which is acceptable: the worst case is the frontend shows a menu item that the server then rejects with 403, or hides a menu item the server would now allow. Neither is a security issue.
