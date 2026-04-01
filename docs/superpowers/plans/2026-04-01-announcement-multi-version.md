# Announcement Multi + Version Filtering Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Support multiple simultaneous active announcements with priority-based single display, and version-based filtering per announcement.

**Architecture:** Add `Priority`, `MinVersion`, `MaxVersion` fields to Announcement model. Remove mutual-exclusion from activate handler. API returns sorted array of active announcements filtered by client version from `X-K2-Client` header. Webapp displays highest-priority undismissed announcement, auto-shows next on dismiss.

**Tech Stack:** Go/Gin/GORM (API), React/MUI/TypeScript (Webapp), Next.js/shadcn (Web), Node.js/MCP SDK (Tools)

**Spec:** `docs/superpowers/specs/2026-04-01-announcement-multi-version-design.md`

---

### Task 1: API — Version Compare Utility

**Files:**
- Create: `api/logic_version.go`
- Create: `api/logic_version_test.go`

- [ ] **Step 1: Write test for `compareVersions`**

```go
// api/logic_version_test.go
package center

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestCompareVersions(t *testing.T) {
	tests := []struct {
		a, b string
		want int
	}{
		{"0.4.2", "0.4.1", 1},
		{"0.4.1", "0.4.2", -1},
		{"0.4.2", "0.4.2", 0},
		{"1.0.0", "0.9.9", 1},
		{"0.10.0", "0.9.0", 1},
		// Pre-release suffix ignored
		{"0.4.2-beta.1", "0.4.2", 0},
		{"0.4.2-beta.1", "0.4.1", 1},
		// Malformed input returns 0 (no filtering)
		{"", "0.4.2", 0},
		{"0.4.2", "", 0},
		{"invalid", "0.4.2", 0},
		{"0.4", "0.4.2", 0},
	}
	for _, tt := range tests {
		t.Run(tt.a+"_vs_"+tt.b, func(t *testing.T) {
			assert.Equal(t, tt.want, compareVersions(tt.a, tt.b))
		})
	}
}

func TestIsVersionInRange(t *testing.T) {
	tests := []struct {
		version, minV, maxV string
		want                bool
	}{
		{"0.4.2", "", "", true},           // no constraints
		{"0.4.2", "0.4.2", "", true},      // exact min
		{"0.4.2", "0.4.3", "", false},     // below min
		{"0.4.2", "", "0.4.2", true},      // exact max
		{"0.4.3", "", "0.4.2", false},     // above max
		{"0.4.2", "0.4.1", "0.4.3", true}, // in range
		{"", "0.4.1", "0.4.3", true},      // empty version = no filtering
		{"invalid", "0.4.1", "", true},     // malformed = no filtering
	}
	for _, tt := range tests {
		t.Run(tt.version+"_in_"+tt.minV+"_"+tt.maxV, func(t *testing.T) {
			assert.Equal(t, tt.want, isVersionInRange(tt.version, tt.minV, tt.maxV))
		})
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd api && go test -run TestCompareVersions -v ./...`
Expected: FAIL — functions not defined.

- [ ] **Step 3: Implement `logic_version.go`**

```go
// api/logic_version.go
package center

import (
	"strconv"
	"strings"
)

// compareVersions compares two semver strings (major.minor.patch).
// Pre-release suffixes are stripped. Malformed input returns 0 (treat as equal).
func compareVersions(a, b string) int {
	pa := parseVersionParts(a)
	pb := parseVersionParts(b)
	if pa == nil || pb == nil {
		return 0
	}
	for i := 0; i < 3; i++ {
		if pa[i] > pb[i] {
			return 1
		}
		if pa[i] < pb[i] {
			return -1
		}
	}
	return 0
}

// isVersionInRange checks if version is within [minVersion, maxVersion].
// Empty minVersion/maxVersion means no constraint. Empty/malformed version skips filtering (returns true).
func isVersionInRange(version, minVersion, maxVersion string) bool {
	if version == "" || parseVersionParts(version) == nil {
		return true // can't filter, show announcement
	}
	if minVersion != "" && compareVersions(version, minVersion) < 0 {
		return false
	}
	if maxVersion != "" && compareVersions(version, maxVersion) > 0 {
		return false
	}
	return true
}

// parseVersionParts parses "x.y.z" (ignoring pre-release suffix) into [major, minor, patch].
// Returns nil if malformed.
func parseVersionParts(v string) []int {
	if v == "" {
		return nil
	}
	// Strip pre-release suffix: "0.4.2-beta.1" -> "0.4.2"
	if idx := strings.IndexByte(v, '-'); idx >= 0 {
		v = v[:idx]
	}
	parts := strings.Split(v, ".")
	if len(parts) != 3 {
		return nil
	}
	result := make([]int, 3)
	for i, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil {
			return nil
		}
		result[i] = n
	}
	return result
}
```

- [ ] **Step 4: Run tests**

Run: `cd api && go test -run "TestCompareVersions|TestIsVersionInRange" -v ./...`
Expected: All PASS.

- [ ] **Step 5: Commit**

```bash
git add api/logic_version.go api/logic_version_test.go
git commit -m "feat(api): add version compare utility for announcement filtering"
```

---

### Task 2: API — Model + Admin API Fields

**Files:**
- Modify: `api/model.go:784-786`
- Modify: `api/api_admin_announcements.go:14-51,107-138,192-211`
- Modify: `api/api_app_config.go:24-41`

- [ ] **Step 1: Add fields to `Announcement` model in `model.go`**

After `AuthMode` line (784), before `ExpiresAt`:
```go
Priority   int    `gorm:"not null;default:0" json:"priority"`                            // 数字越大越优先
MinVersion string `gorm:"type:varchar(20);not null;default:''" json:"minVersion"`         // 最低版本（含），空=不限
MaxVersion string `gorm:"type:varchar(20);not null;default:''" json:"maxVersion"`         // 最高版本（含），空=不限
```

Update `IsActive` field comment from "同一时刻只有一条为true" to "可多条同时为true".

Also update `api_admin_activate_announcement` function docstring (line 252) from "激活公告（同时 deactivate 其他所有）" to "激活公告".

Also update the update handler comment (lines 201-203) — remove "requires mutual-exclusion logic" rationale, keep note that `IsActive` uses dedicated endpoints.

- [ ] **Step 2: Add fields to `AnnouncementRequest`**

After `AuthMode` (line 19), add:
```go
Priority   int    `json:"priority"`              // 优先级，默认 0
MinVersion string `json:"minVersion"`            // 最低版本，空=不限
MaxVersion string `json:"maxVersion"`            // 最高版本，空=不限
```

- [ ] **Step 3: Add fields to `AnnouncementResponse`**

After `AuthMode` (line 33), add:
```go
Priority   int    `json:"priority"`
MinVersion string `json:"minVersion"`
MaxVersion string `json:"maxVersion"`
```

- [ ] **Step 4: Add fields to `convertAnnouncementToResponse`**

After `AuthMode` mapping (line 47), add:
```go
Priority:   a.Priority,
MinVersion: a.MinVersion,
MaxVersion: a.MaxVersion,
```

- [ ] **Step 5: Add fields to `DataAnnouncement` in `api_app_config.go`**

After `AuthMode` (line 30), add:
```go
Priority   int    `json:"priority"`
MinVersion string `json:"minVersion,omitempty"`
MaxVersion string `json:"maxVersion,omitempty"`
```

- [ ] **Step 6: Add version validation to create handler**

After `authMode` validation block (lines 107-114), add:
```go
if req.MinVersion != "" && parseVersionParts(req.MinVersion) == nil {
	Error(c, ErrorInvalidArgument, "minVersion must be in x.y.z format")
	return
}
if req.MaxVersion != "" && parseVersionParts(req.MaxVersion) == nil {
	Error(c, ErrorInvalidArgument, "maxVersion must be in x.y.z format")
	return
}
```

Add to `Announcement{}` struct creation (after `AuthMode: authMode`):
```go
Priority:   req.Priority,
MinVersion: req.MinVersion,
MaxVersion: req.MaxVersion,
```

- [ ] **Step 7: Add version validation + fields to update handler**

After `authMode` validation (around line 199), add same validation.

Add to updates map:
```go
"priority":    req.Priority,
"min_version": req.MinVersion,
"max_version": req.MaxVersion,
```

- [ ] **Step 8: Verify build**

Run: `cd api && go build ./...`
Expected: Clean compile.

- [ ] **Step 9: Commit**

```bash
git add api/model.go api/api_admin_announcements.go api/api_app_config.go
git commit -m "feat(api): add priority, minVersion, maxVersion to Announcement model and admin API"
```

---

### Task 3: API — Remove Mutual Exclusion + Multi-Announcement Config

**Files:**
- Modify: `api/api_admin_announcements.go:116-128,252-296`
- Modify: `api/api_app_config.go:34-105`

- [ ] **Step 1: Simplify create handler — remove deactivate-all**

Replace lines 116-128 (the `isActive` + transaction + deactivate-all block) with:
```go
isActive := req.IsActive != nil && *req.IsActive
```

Replace lines 130-147 (tx.Create + tx.Commit) with simple create:
```go
announcement := Announcement{
	Message:    req.Message,
	LinkURL:    req.LinkURL,
	LinkText:   req.LinkText,
	OpenMode:   openMode,
	AuthMode:   authMode,
	Priority:   req.Priority,
	MinVersion: req.MinVersion,
	MaxVersion: req.MaxVersion,
	ExpiresAt:  req.ExpiresAt,
	IsActive:   BoolPtr(isActive),
}

if err := db.Get().Create(&announcement).Error; err != nil {
	log.Errorf(c, "failed to create announcement: %v", err)
	Error(c, ErrorSystemError, "failed to create announcement")
	return
}
```

- [ ] **Step 2: Simplify activate handler — remove deactivate-all**

Replace the entire transaction block in `api_admin_activate_announcement` (lines 274-290) with:
```go
if err := db.Get().Model(&announcement).Update("is_active", true).Error; err != nil {
	log.Errorf(c, "failed to activate announcement: %v", err)
	Error(c, ErrorSystemError, "failed to activate announcement")
	return
}
```

Update log message (line 294) from "deactivated all others" to just "activated".

- [ ] **Step 3: Replace `getActiveAnnouncement` with `getActiveAnnouncements`**

Replace the function (lines 327-346) with:
```go
// getActiveAnnouncements returns all active, unexpired announcements filtered by client version.
// Sorted by priority DESC, id DESC. clientVersion="" skips version filtering.
func getActiveAnnouncements(clientVersion string) []DataAnnouncement {
	var announcements []Announcement
	err := db.Get().
		Where("is_active = ? AND (expires_at = 0 OR expires_at > ?)", true, time.Now().Unix()).
		Order("priority DESC, id DESC").
		Find(&announcements).Error
	if err != nil {
		return nil
	}

	var result []DataAnnouncement
	for _, a := range announcements {
		if !isVersionInRange(clientVersion, a.MinVersion, a.MaxVersion) {
			continue
		}
		result = append(result, DataAnnouncement{
			ID:         fmt.Sprintf("%d", a.ID),
			Message:    a.Message,
			LinkURL:    a.LinkURL,
			LinkText:   a.LinkText,
			OpenMode:   a.OpenMode,
			AuthMode:   a.AuthMode,
			Priority:   a.Priority,
			MinVersion: a.MinVersion,
			MaxVersion: a.MaxVersion,
			ExpiresAt:  a.ExpiresAt,
		})
	}
	return result
}
```

- [ ] **Step 4: Update `DataAppConfig` and `api_get_app_config` handler**

Add to `DataAppConfig` struct (after `Announcement` field):
```go
Announcements []DataAnnouncement `json:"announcements,omitempty"` // 新增：全部活跃公告
```

Update handler to parse header and fill both fields. Replace lines 92-101 with:
```go
// Parse client version from X-K2-Client header
clientVersion := ""
if clientHeader := c.GetHeader("X-K2-Client"); clientHeader != "" {
	if appInfo := parseClientHeader(clientHeader); appInfo != nil {
		clientVersion = appInfo.Version
	}
}

// Get active announcements filtered by client version
announcements := getActiveAnnouncements(clientVersion)

// Build response
var singleAnnouncement *DataAnnouncement
if len(announcements) > 0 {
	singleAnnouncement = &announcements[0]
}

data := DataAppConfig{
	AppLinks:         appLinks,
	InviteReward:     inviteReward,
	MinClientVersion: minClientVersion,
	Announcement:     singleAnnouncement,
	Announcements:    announcements,
}
```

- [ ] **Step 5: Verify build + tests**

Run: `cd api && go build ./... && go test ./...`
Expected: Clean compile, all tests pass.

- [ ] **Step 6: Commit**

```bash
git add api/api_admin_announcements.go api/api_app_config.go
git commit -m "feat(api): multi-announcement support — remove mutual exclusion, add version filtering"
```

---

### Task 4: Webapp — Multi-Announcement Display

**Files:**
- Modify: `webapp/src/services/api-types.ts:537-553`
- Modify: `webapp/src/components/AnnouncementBanner.tsx:71-104`

- [ ] **Step 1: Update `Announcement` and `AppConfig` types in `api-types.ts`**

Add to `Announcement` (after `authMode`):
```typescript
priority?: number; // 优先级，数字越大越优先
minVersion?: string; // 最低版本要求
maxVersion?: string; // 最高版本要求
```

Add to `AppConfig` (after `announcement`):
```typescript
announcements?: Announcement[]; // 全部活跃公告（按 priority DESC 排序）
```

- [ ] **Step 2: Rewrite AnnouncementBanner useEffect to handle array**

Replace the `useEffect` block (lines 78-104) with:
```typescript
// Check and display the highest-priority undismissed announcement
useEffect(() => {
  // Prefer announcements array, fallback to singular for backward compat
  const list = appConfig?.announcements ?? (appConfig?.announcement ? [appConfig.announcement] : []);

  // Find first non-expired, non-dismissed announcement
  const active = list.find(ann =>
    !isAnnouncementExpired(ann.expiresAt) && !isAnnouncementDismissed(ann.id)
  );

  if (active) {
    setAnnouncement(active);
    setVisible(true);
  } else {
    setAnnouncement(null);
    setVisible(false);
  }
}, [appConfig?.announcements, appConfig?.announcement]);
```

- [ ] **Step 3: Update handleDismiss to re-evaluate next announcement**

Replace `handleDismiss` (lines 106-111) with:
```typescript
const handleDismiss = () => {
  if (announcement) {
    dismissAnnouncement(announcement.id);
  }
  // Re-evaluate: find next undismissed announcement
  const list = appConfig?.announcements ?? (appConfig?.announcement ? [appConfig.announcement] : []);
  const next = list.find(ann =>
    !isAnnouncementExpired(ann.expiresAt) && !isAnnouncementDismissed(ann.id) && ann.id !== announcement?.id
  );
  if (next) {
    setAnnouncement(next);
  } else {
    setVisible(false);
  }
};
```

- [ ] **Step 4: Verify TypeScript**

Run: `cd webapp && npx tsc --noEmit`
Expected: No errors.

- [ ] **Step 5: Run tests**

Run: `cd webapp && npx vitest run`
Expected: All pass.

- [ ] **Step 6: Commit**

```bash
git add webapp/src/services/api-types.ts webapp/src/components/AnnouncementBanner.tsx
git commit -m "feat(webapp): multi-announcement display — priority-based, dismiss reveals next"
```

---

### Task 5: Web — Manager + API Types

**Files:**
- Modify: `web/src/lib/api.ts:780-801`
- Modify: `web/src/app/(manager)/manager/announcements/page.tsx`

- [ ] **Step 1: Add fields to web API types**

Add to `AnnouncementRequest` (after `authMode`):
```typescript
priority?: number;
minVersion?: string;
maxVersion?: string;
```

Add to `AnnouncementResponse` (after `authMode`):
```typescript
priority: number;
minVersion: string;
maxVersion: string;
```

- [ ] **Step 2: Update `initialForm` in announcements page**

Add after `authMode: "none"`:
```typescript
priority: 0,
minVersion: "",
maxVersion: "",
```

- [ ] **Step 3: Add `priority` to edit form population**

In `openEditDialog`, add to setForm:
```typescript
priority: item.priority,
minVersion: item.minVersion,
maxVersion: item.maxVersion,
```

- [ ] **Step 4: Add `priority` column to table**

After the `authMode` column, add:
```typescript
{
  accessorKey: "priority",
  header: "优先级",
  size: 80,
},
{
  id: "version",
  header: "版本范围",
  size: 120,
  cell: ({ row }) => {
    const { minVersion, maxVersion } = row.original;
    if (!minVersion && !maxVersion) return <span className="text-muted-foreground">全部</span>;
    return <span>{minVersion || "*"} ~ {maxVersion || "*"}</span>;
  },
},
```

- [ ] **Step 5: Add form fields**

After the `authMode` Select block, add:
```tsx
<div className="grid gap-2">
  <Label>优先级</Label>
  <Input
    type="number"
    value={form.priority ?? 0}
    onChange={(e) => setForm({ ...form, priority: parseInt(e.target.value) || 0 })}
    placeholder="0"
  />
  <p className="text-xs text-muted-foreground">数字越大越优先显示</p>
</div>
<div className="grid grid-cols-2 gap-4">
  <div className="grid gap-2">
    <Label>最低版本</Label>
    <Input
      value={form.minVersion ?? ""}
      onChange={(e) => setForm({ ...form, minVersion: e.target.value })}
      placeholder="0.4.2"
    />
  </div>
  <div className="grid gap-2">
    <Label>最高版本</Label>
    <Input
      value={form.maxVersion ?? ""}
      onChange={(e) => setForm({ ...form, maxVersion: e.target.value })}
      placeholder="0.4.3"
    />
  </div>
</div>
```

- [ ] **Step 6: Update MCP tool descriptions**

In `tools/kaitu-center/src/tools/admin-announcements.ts`:
- `activate_announcement` description: `'Activate an announcement (deactivates all others).'` → `'Activate an announcement.'`
- `create_announcement` `is_active` param description: `'Activate immediately (deactivates current active)'` → `'Activate immediately'`

- [ ] **Step 7: Add params to MCP create/update tools**

In `create_announcement` params (after `auth_mode`):
```typescript
priority: z.number().optional().describe('Display priority (higher = shown first, default 0)'),
min_version: z.string().optional().describe('Minimum app version (inclusive, e.g. "0.4.2")'),
max_version: z.string().optional().describe('Maximum app version (inclusive, e.g. "0.4.3")'),
```

In `create_announcement` mapBody (after `authMode`):
```typescript
priority: p.priority,
minVersion: p.min_version,
maxVersion: p.max_version,
```

Same additions for `update_announcement`.

- [ ] **Step 8: Commit**

```bash
git add web/src/lib/api.ts web/src/app/\(manager\)/manager/announcements/page.tsx tools/kaitu-center/src/tools/admin-announcements.ts
git commit -m "feat(web+mcp): add priority, version fields to manager and MCP tools"
```

---

### Task 6: Integration Verification

- [ ] **Step 1: Go build + test**

Run: `cd api && go build ./... && go test ./...`
Expected: Clean compile, all pass.

- [ ] **Step 2: Webapp type check + test**

Run: `cd webapp && npx tsc --noEmit && npx vitest run`
Expected: No errors, all pass.

- [ ] **Step 3: Final git log**

Run: `git log --oneline feature/announcement-multi-version ^main`
Expected: 5 clean commits.
