# Announcement 增强 — 多条公告 + 版本过滤

**Date**: 2026-04-01
**Status**: Draft
**Scope**: API, Webapp, Web, MCP Tools
**Depends on**: announcement-ott-auth（已合入 main）

## Background

当前 Announcement 系统限制同一时间只有 1 条活跃公告（activate 时 deactivate-all）。实际需要"长期促销 + 临时维护通知"并存。同时，不同版本的 app 支持不同功能（如 v0.4.2+ 才有 OTT），公告需要按版本过滤。

## Goals

1. 支持多条公告同时活跃，按 priority 排序，客户端显示最高优先级未 dismiss 的一条
2. 公告按客户端版本过滤（minVersion / maxVersion）
3. 向后兼容旧 webapp（保留 `announcement` 单数字段）

## Non-Goals

- 不做客户端多条 banner 堆叠展示（始终单条）
- 不废弃 activate/deactivate 端点（保留，但 activate 不再 deactivate-all）

---

## Design

### 1. 数据库模型变更

`Announcement` 新增 3 个字段（GORM AutoMigrate）：

```go
Priority   int    `gorm:"not null;default:0"`                          // 数字越大越优先
MinVersion string `gorm:"type:varchar(20);not null;default:''"` // 最低版本要求（含），空=不限
MaxVersion string `gorm:"type:varchar(20);not null;default:''"` // 最高版本要求（含），空=不限
```

现有数据：`Priority=0`，`MinVersion=""`，`MaxVersion=""`，行为不变。

注意：现有 `IsActive` 字段注释 "同一时刻只有一条为true" 需更新为 "可多条同时为true"。`idx_announcement_active` 索引保留（仍用于查询优化）。

### 2. activate handler 改造

**当前行为**：activate 时在事务中先 `UPDATE SET is_active=false WHERE is_active=true`，再激活目标。

**新行为**：activate 只设置目标 `is_active=true`，**不 deactivate 其他**。deactivate 端点不变。

**影响**：多条可同时 active。运营通过 deactivate 手动关闭不需要的公告。

### 3. 版本比较（Go 侧新增）

新增 `logic_version.go`，一个简单的 semver 比较函数：

```go
// compareVersions compares two semver strings (major.minor.patch).
// Returns -1, 0, or 1. Pre-release suffixes are ignored for announcement filtering.
func compareVersions(a, b string) int
```

只需处理 `x.y.z` 格式（忽略 `-beta.1` 后缀），因为公告版本过滤不需要 pre-release 精度。畸形输入（空字符串、非数字、段数不足）返回 0（视为相等 / 不过滤），避免意外隐藏公告。

### 4. API — getActiveAnnouncements()

**新函数**替代 `getActiveAnnouncement()`：

```go
func getActiveAnnouncements(clientVersion string) []DataAnnouncement
```

逻辑：
1. 查询 `WHERE is_active = true AND (expires_at = 0 OR expires_at > time.Now().Unix())`
2. `ORDER BY priority DESC, id DESC`
3. 过滤版本：如果 `clientVersion` 非空，排除 `minVersion > clientVersion` 或 `maxVersion < clientVersion` 的记录
4. 返回 `[]DataAnnouncement`

**`clientVersion` 来源**：从 `X-K2-Client` header 解析（已有 `parseClientHeader()` → `AppInfo.Version`）。`/api/app/config` 是公开端点（无 auth middleware），客户端不一定发送此 header。`clientVersion = ""` 时跳过版本过滤，返回全部活跃公告。版本过滤是 best-effort 设计。

### 5. API — /api/app/config 响应变更

`DataAppConfig` 新增 `Announcements` 数组字段，保留 `Announcement` 单数字段向后兼容：

```go
type DataAppConfig struct {
    AppLinks         DataAppLinks      `json:"appLinks"`
    InviteReward     InviteConfig      `json:"inviteReward"`
    MinClientVersion string            `json:"minClientVersion,omitempty"`
    Announcement     *DataAnnouncement `json:"announcement,omitempty"`     // 向后兼容：第一条
    Announcements    []DataAnnouncement `json:"announcements,omitempty"`   // 新增：全部
}
```

`DataAnnouncement` 新增字段：

```go
Priority   int    `json:"priority"`
MinVersion string `json:"minVersion,omitempty"`
MaxVersion string `json:"maxVersion,omitempty"`
```

`api_get_app_config` handler 中：解析 `X-K2-Client` header 获取 `clientVersion`，调用 `getActiveAnnouncements(clientVersion)`，填充两个字段。

### 6. Admin API 变更

`AnnouncementRequest` / `AnnouncementResponse` / `convertAnnouncementToResponse()` 新增 `Priority`、`MinVersion`、`MaxVersion` 字段。

创建/更新 handler 中：
- `Priority` 无需验证（任意 int，默认 0）
- `MinVersion` / `MaxVersion` 如非空，验证格式为 `x.y.z`

### 7. Webapp — AnnouncementBanner 改造

`AppConfig` 类型新增：

```typescript
announcements?: Announcement[];
```

`Announcement` 类型新增：

```typescript
priority?: number;
minVersion?: string;
maxVersion?: string;
```

`AnnouncementBanner` 组件改造：
1. 优先读 `appConfig.announcements`（数组），fallback 到 `appConfig.announcement`（单数，兼容旧 API）
2. 遍历数组，过滤掉已过期 + 已 dismiss 的
3. 显示第一条（已按 priority DESC 排序）
4. dismiss 逻辑不变（按 `announcement_dismissed_{id}` 记录）
5. 用户关闭当前公告 → 组件 re-render → 自动显示下一条未 dismiss 的

### 8. Web — Manager 后台

表单新增 3 个字段：
- `priority`：数字输入框（默认 0）
- `minVersion`：文本输入框（placeholder `0.4.2`，可留空）
- `maxVersion`：文本输入框（placeholder `0.4.3`，可留空）

表格新增 `priority` 列。`minVersion`/`maxVersion` 显示在详情或 tooltip 中（避免表格过宽）。

`AnnouncementRequest` / `AnnouncementResponse` 新增对应字段。

### 9. MCP 工具

`create_announcement` / `update_announcement` 新增参数：
- `priority`: number（默认 0）
- `min_version`: string（可选）
- `max_version`: string（可选）

`list_announcements` 响应自然包含新字段（透传 `AnnouncementResponse`），无需改动。

### 10. activate handler 事务简化

**当前 `api_admin_activate_announcement`**：

```go
tx := db.Get().Begin()
tx.Model(&Announcement{}).Where("is_active = ?", true).Update("is_active", false) // 去掉
tx.Model(&announcement).Update("is_active", true)
tx.Commit()
```

**改为**：

```go
db.Get().Model(&announcement).Update("is_active", true) // 单条 update，不再需要事务
```

同样，**create handler** 中创建时如果 `isActive=true`，不再 deactivate-all。

---

## 向后兼容

| 客户端版本 | API 响应 | 行为 |
|-----------|---------|------|
| v0.4.1 及以下 | 收到 `announcement`（单数）+ `announcements`（数组），但 webapp 无 AnnouncementBanner 后端数据（v0.4.1 无公告系统后端） | 无公告 |
| v0.4.2（当前 main，OTT 版本） | 读 `announcement`（单数），忽略 `announcements` | 看到最高优先级公告 |
| v0.4.3+（本次改造后） | 读 `announcements`（数组） | 多条公告，dismiss 后自动露出下一条 |

## Change Summary

### API (`api/`)
- **新增** `logic_version.go`：`compareVersions()` 函数
- **改动** `model.go`：Announcement 新增 `Priority`、`MinVersion`、`MaxVersion`
- **改动** `api_admin_announcements.go`：request/response/convert 新增字段；activate 去掉 deactivate-all；create 去掉 deactivate-all；create/update 新增版本格式验证
- **改动** `api_app_config.go`：`DataAppConfig` 新增 `Announcements`；`DataAnnouncement` 新增字段；新函数 `getActiveAnnouncements(clientVersion)`；handler 解析 header

### Webapp (`webapp/`)
- **改动** `api-types.ts`：`AppConfig` 新增 `announcements`；`Announcement` 新增字段
- **改动** `AnnouncementBanner.tsx`：读数组 → 过滤 → 显示首条未 dismiss

### Web (`web/`)
- **改动** `api.ts`：`AnnouncementRequest`/`AnnouncementResponse` 新增字段
- **改动** `manager/announcements/page.tsx`：表单 + 表格新增字段

### MCP Tools (`tools/kaitu-center/`)
- **改动** `admin-announcements.ts`：create/update 新增参数
