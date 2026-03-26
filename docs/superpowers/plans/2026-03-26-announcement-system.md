# Announcement System Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Migrate announcements from static config.yml to DB-backed storage with admin CRUD API, manager dashboard, webapp openMode support, and MCP tools.

**Architecture:** GORM model with soft delete, admin CRUD handlers under `/app/announcements`, single active announcement invariant enforced in activate/deactivate handlers. Frontend AnnouncementBanner gets `openMode` field support. Manager dashboard gets announcements list page. MCP tools use existing factory pattern.

**Tech Stack:** Go/Gin/GORM (API), React/MUI (webapp), Next.js/shadcn (manager), TypeScript/Zod (MCP)

**Spec:** `docs/superpowers/specs/2026-03-26-announcement-system-design.md`

---

## File Map

| Action | File | Responsibility |
|--------|------|---------------|
| Create | `api/api_admin_announcements.go` | Admin CRUD handlers (list, create, update, delete, activate, deactivate) |
| Modify | `api/model.go` | Add `Announcement` GORM model struct |
| Modify | `api/api_app_config.go` | Replace viper announcement read with DB query, add `OpenMode` field |
| Modify | `api/route.go` | Register `/app/announcements` routes |
| Modify | `api/migrate.go` | Add `&Announcement{}` to AutoMigrate |
| Modify | `api/api_admin_permissions.go` | Add `announcements` permission groups |
| Modify | `webapp/src/services/api-types.ts` | Add `openMode` to `Announcement` interface |
| Modify | `webapp/src/components/AnnouncementBanner.tsx` | Handle `openMode === 'webview'` in link click |
| Modify | `web/src/lib/api.ts` | Add announcement API types and methods |
| Modify | `web/src/components/manager-sidebar.tsx` | Add announcements sidebar entry |
| Create | `web/src/app/(manager)/manager/announcements/page.tsx` | Manager announcements list + CRUD page |
| Create | `tools/kaitu-center/src/tools/admin-announcements.ts` | MCP announcement tools |
| Modify | `tools/kaitu-center/src/index.ts` | Register announcement tools |

---

## Task 1: GORM Model + Migration

**Files:**
- Modify: `api/model.go` (append after Campaign model, ~line 764)
- Modify: `api/migrate.go` (add to AutoMigrate list)

- [ ] **Step 1: Add Announcement model to `api/model.go`**

Append after the `Campaign` model (around line 764):

```go
// Announcement 公告信息
type Announcement struct {
	ID        uint64         `gorm:"primarykey" json:"id"`
	CreatedAt time.Time      `json:"createdAt"`
	UpdatedAt time.Time      `json:"updatedAt"`
	DeletedAt gorm.DeletedAt `gorm:"index"`

	Message   string `gorm:"type:varchar(500);not null" json:"message"`              // 公告文字内容
	LinkURL   string `gorm:"type:varchar(1024);not null;default:''" json:"linkUrl"`   // 点击跳转链接
	LinkText  string `gorm:"type:varchar(100);not null;default:''" json:"linkText"`   // 链接文字
	OpenMode  string `gorm:"type:varchar(20);not null;default:'external'" json:"openMode"` // external | webview
	ExpiresAt int64  `gorm:"not null;default:0" json:"expiresAt"`                    // Unix秒，0=不过期
	IsActive  *bool  `gorm:"default:false;index:idx_announcement_active" json:"isActive"` // 同一时刻只有一条为true
}
```

- [ ] **Step 2: Add to AutoMigrate in `api/migrate.go`**

Add `&Announcement{}` at the end of the AutoMigrate list, before the closing parenthesis:

```go
		// Survey system
		&SurveyResponse{},
		// Announcement system
		&Announcement{},
	)
```

- [ ] **Step 3: Verify compilation**

Run: `cd api && go build ./...`
Expected: Build succeeds with no errors.

- [ ] **Step 4: Commit**

```bash
git add api/model.go api/migrate.go
git commit -m "feat(api): add Announcement GORM model and migration"
```

---

## Task 2: Admin CRUD Handlers

**Files:**
- Create: `api/api_admin_announcements.go`

- [ ] **Step 1: Create `api/api_admin_announcements.go`**

```go
package center

import (
	"fmt"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	db "github.com/wordgate/qtoolkit/db"
	"github.com/wordgate/qtoolkit/log"
)

// AnnouncementRequest 创建/更新公告请求
type AnnouncementRequest struct {
	Message   string `json:"message" binding:"required"`
	LinkURL   string `json:"linkUrl"`
	LinkText  string `json:"linkText"`
	OpenMode  string `json:"openMode"`  // external | webview，默认 external
	ExpiresAt int64  `json:"expiresAt"` // Unix秒，0=不过期
	IsActive  *bool  `json:"isActive"`
}

// AnnouncementResponse 公告响应
type AnnouncementResponse struct {
	ID        uint64 `json:"id"`
	CreatedAt int64  `json:"createdAt"`
	UpdatedAt int64  `json:"updatedAt"`
	Message   string `json:"message"`
	LinkURL   string `json:"linkUrl"`
	LinkText  string `json:"linkText"`
	OpenMode  string `json:"openMode"`
	ExpiresAt int64  `json:"expiresAt"`
	IsActive  bool   `json:"isActive"`
}

func convertAnnouncementToResponse(a Announcement) AnnouncementResponse {
	return AnnouncementResponse{
		ID:        a.ID,
		CreatedAt: a.CreatedAt.Unix(),
		UpdatedAt: a.UpdatedAt.Unix(),
		Message:   a.Message,
		LinkURL:   a.LinkURL,
		LinkText:  a.LinkText,
		OpenMode:  a.OpenMode,
		ExpiresAt: a.ExpiresAt,
		IsActive:  a.IsActive != nil && *a.IsActive,
	}
}

// api_admin_list_announcements 列出所有公告
func api_admin_list_announcements(c *gin.Context) {
	log.Infof(c, "admin request to list announcements")

	pagination := PaginationFromRequest(c)
	query := db.Get().Model(&Announcement{})

	if err := query.Count(&pagination.Total).Error; err != nil {
		log.Errorf(c, "failed to count announcements: %v", err)
		Error(c, ErrorSystemError, "failed to count announcements")
		return
	}

	var announcements []Announcement
	if err := query.Offset(pagination.Offset()).Limit(pagination.PageSize).Order("created_at DESC").Find(&announcements).Error; err != nil {
		log.Errorf(c, "failed to query announcements: %v", err)
		Error(c, ErrorSystemError, "failed to query announcements")
		return
	}

	items := make([]AnnouncementResponse, len(announcements))
	for i, a := range announcements {
		items[i] = convertAnnouncementToResponse(a)
	}

	log.Infof(c, "successfully retrieved %d announcements", len(items))
	ListWithData(c, items, pagination)
}

// api_admin_create_announcement 创建公告
func api_admin_create_announcement(c *gin.Context) {
	log.Infof(c, "admin request to create announcement")

	var req AnnouncementRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		log.Warnf(c, "invalid request: %v", err)
		Error(c, ErrorInvalidArgument, err.Error())
		return
	}

	if len(req.Message) > 500 {
		Error(c, ErrorInvalidArgument, "message too long (max 500)")
		return
	}

	openMode := req.OpenMode
	if openMode == "" {
		openMode = "external"
	}
	if openMode != "external" && openMode != "webview" {
		Error(c, ErrorInvalidArgument, "openMode must be 'external' or 'webview'")
		return
	}

	isActive := req.IsActive != nil && *req.IsActive

	tx := db.Get().Begin()

	// 如果要激活，先 deactivate 其他所有
	if isActive {
		if err := tx.Model(&Announcement{}).Where("is_active = ?", true).Update("is_active", false).Error; err != nil {
			tx.Rollback()
			log.Errorf(c, "failed to deactivate existing announcements: %v", err)
			Error(c, ErrorSystemError, "failed to create announcement")
			return
		}
	}

	announcement := Announcement{
		Message:   req.Message,
		LinkURL:   req.LinkURL,
		LinkText:  req.LinkText,
		OpenMode:  openMode,
		ExpiresAt: req.ExpiresAt,
		IsActive:  BoolPtr(isActive),
	}

	if err := tx.Create(&announcement).Error; err != nil {
		tx.Rollback()
		log.Errorf(c, "failed to create announcement: %v", err)
		Error(c, ErrorSystemError, "failed to create announcement")
		return
	}

	tx.Commit()

	response := convertAnnouncementToResponse(announcement)
	log.Infof(c, "successfully created announcement: %d", announcement.ID)
	Success(c, &response)
}

// api_admin_update_announcement 更新公告
func api_admin_update_announcement(c *gin.Context) {
	log.Infof(c, "admin request to update announcement")

	idStr := c.Param("id")
	id, err := strconv.ParseUint(idStr, 10, 64)
	if err != nil {
		Error(c, ErrorInvalidArgument, "invalid announcement id")
		return
	}

	var req AnnouncementRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		log.Warnf(c, "invalid request: %v", err)
		Error(c, ErrorInvalidArgument, err.Error())
		return
	}

	if len(req.Message) > 500 {
		Error(c, ErrorInvalidArgument, "message too long (max 500)")
		return
	}

	var announcement Announcement
	if err := db.Get().First(&announcement, id).Error; err != nil {
		Error(c, ErrorNotFound, "announcement not found")
		return
	}

	openMode := req.OpenMode
	if openMode == "" {
		openMode = "external"
	}
	if openMode != "external" && openMode != "webview" {
		Error(c, ErrorInvalidArgument, "openMode must be 'external' or 'webview'")
		return
	}

	updates := map[string]interface{}{
		"message":    req.Message,
		"link_url":   req.LinkURL,
		"link_text":  req.LinkText,
		"open_mode":  openMode,
		"expires_at": req.ExpiresAt,
	}

	if err := db.Get().Model(&announcement).Updates(updates).Error; err != nil {
		log.Errorf(c, "failed to update announcement: %v", err)
		Error(c, ErrorSystemError, "failed to update announcement")
		return
	}

	// 重新读取以获取更新后的值
	db.Get().First(&announcement, id)
	response := convertAnnouncementToResponse(announcement)
	log.Infof(c, "successfully updated announcement: %d", id)
	Success(c, &response)
}

// api_admin_delete_announcement 删除公告
func api_admin_delete_announcement(c *gin.Context) {
	log.Infof(c, "admin request to delete announcement")

	idStr := c.Param("id")
	id, err := strconv.ParseUint(idStr, 10, 64)
	if err != nil {
		Error(c, ErrorInvalidArgument, "invalid announcement id")
		return
	}

	var announcement Announcement
	if err := db.Get().First(&announcement, id).Error; err != nil {
		Error(c, ErrorNotFound, "announcement not found")
		return
	}

	if err := db.Get().Delete(&announcement).Error; err != nil {
		log.Errorf(c, "failed to delete announcement: %v", err)
		Error(c, ErrorSystemError, "failed to delete announcement")
		return
	}

	log.Infof(c, "successfully deleted announcement: %d", id)
	SuccessEmpty(c)
}

// api_admin_activate_announcement 激活公告（同时 deactivate 其他所有）
func api_admin_activate_announcement(c *gin.Context) {
	log.Infof(c, "admin request to activate announcement")

	idStr := c.Param("id")
	id, err := strconv.ParseUint(idStr, 10, 64)
	if err != nil {
		Error(c, ErrorInvalidArgument, "invalid announcement id")
		return
	}

	var announcement Announcement
	if err := db.Get().First(&announcement, id).Error; err != nil {
		Error(c, ErrorNotFound, "announcement not found")
		return
	}

	// 检查是否已过期
	if announcement.ExpiresAt > 0 && time.Now().Unix() > announcement.ExpiresAt {
		Error(c, ErrorInvalidOperation, "cannot activate expired announcement")
		return
	}

	tx := db.Get().Begin()

	// Deactivate all others
	if err := tx.Model(&Announcement{}).Where("is_active = ?", true).Update("is_active", false).Error; err != nil {
		tx.Rollback()
		log.Errorf(c, "failed to deactivate announcements: %v", err)
		Error(c, ErrorSystemError, "failed to activate announcement")
		return
	}

	// Activate this one
	if err := tx.Model(&announcement).Update("is_active", true).Error; err != nil {
		tx.Rollback()
		log.Errorf(c, "failed to activate announcement: %v", err)
		Error(c, ErrorSystemError, "failed to activate announcement")
		return
	}

	tx.Commit()

	db.Get().First(&announcement, id)
	response := convertAnnouncementToResponse(announcement)
	log.Infof(c, "successfully activated announcement: %d (deactivated all others)", id)
	Success(c, &response)
}

// api_admin_deactivate_announcement 停用公告
func api_admin_deactivate_announcement(c *gin.Context) {
	log.Infof(c, "admin request to deactivate announcement")

	idStr := c.Param("id")
	id, err := strconv.ParseUint(idStr, 10, 64)
	if err != nil {
		Error(c, ErrorInvalidArgument, "invalid announcement id")
		return
	}

	var announcement Announcement
	if err := db.Get().First(&announcement, id).Error; err != nil {
		Error(c, ErrorNotFound, "announcement not found")
		return
	}

	if err := db.Get().Model(&announcement).Update("is_active", false).Error; err != nil {
		log.Errorf(c, "failed to deactivate announcement: %v", err)
		Error(c, ErrorSystemError, "failed to deactivate announcement")
		return
	}

	db.Get().First(&announcement, id)
	response := convertAnnouncementToResponse(announcement)
	log.Infof(c, "successfully deactivated announcement: %d", id)
	Success(c, &response)
}

// getActiveAnnouncement 获取当前活跃且未过期的公告（供 /api/app/config 使用）
func getActiveAnnouncement() *DataAnnouncement {
	var announcement Announcement
	err := db.Get().
		Where("is_active = ? AND (expires_at = 0 OR expires_at > ?)", true, time.Now().Unix()).
		First(&announcement).Error
	if err != nil {
		return nil
	}

	return &DataAnnouncement{
		ID:        fmt.Sprintf("%d", announcement.ID),
		Message:   announcement.Message,
		LinkURL:   announcement.LinkURL,
		LinkText:  announcement.LinkText,
		OpenMode:  announcement.OpenMode,
		ExpiresAt: announcement.ExpiresAt,
	}
}
```

- [ ] **Step 2: Verify compilation**

Run: `cd api && go build ./...`
Expected: Build succeeds. (Note: `getActiveAnnouncement` and routes will be wired in subsequent tasks.)

- [ ] **Step 3: Commit**

```bash
git add api/api_admin_announcements.go
git commit -m "feat(api): add announcement admin CRUD handlers"
```

---

## Task 3: Update DataAnnouncement + Public Config Endpoint

**Files:**
- Modify: `api/api_app_config.go`

- [ ] **Step 1: Add `OpenMode` field to `DataAnnouncement` struct**

In `api/api_app_config.go`, find the `DataAnnouncement` struct and add the `OpenMode` field:

```go
// DataAnnouncement 公告信息
//
type DataAnnouncement struct {
	ID        string `json:"id" example:"announcement-2024-01"`                             // 公告唯一ID，用于客户端跟踪关闭状态
	Message   string `json:"message" example:"系统维护公告：1月1日凌晨进行系统升级"`                         // 公告文字内容
	LinkURL   string `json:"linkUrl,omitempty" example:"https://kaitu.io/news/maintenance"` // 可选：点击跳转链接
	LinkText  string `json:"linkText,omitempty" example:"查看详情"`                             // 可选：链接文字
	OpenMode  string `json:"openMode,omitempty" example:"external"`                         // 可选：external（默认）或 webview
	ExpiresAt int64  `json:"expiresAt,omitempty" example:"1704067200"`                      // 可选：公告过期时间戳（Unix秒），为0表示不过期
}
```

- [ ] **Step 2: Replace viper announcement read with DB query in `api_get_app_config`**

In the `api_get_app_config` function, replace the viper-based announcement block (lines 91-101):

Old code to replace:
```go
	// 读取公告配置
	var announcement *DataAnnouncement
	announcementID := viper.GetString("frontend_config.announcement.id")
	if announcementID != "" {
		announcement = &DataAnnouncement{
			ID:        announcementID,
			Message:   viper.GetString("frontend_config.announcement.message"),
			LinkURL:   viper.GetString("frontend_config.announcement.link_url"),
			LinkText:  viper.GetString("frontend_config.announcement.link_text"),
			ExpiresAt: viper.GetInt64("frontend_config.announcement.expires_at"),
		}
	}
```

New code:
```go
	// 从数据库读取活跃公告
	announcement := getActiveAnnouncement()
```

- [ ] **Step 3: Verify compilation**

Run: `cd api && go build ./...`
Expected: Build succeeds.

- [ ] **Step 4: Commit**

```bash
git add api/api_app_config.go
git commit -m "feat(api): read announcement from DB instead of config.yml, add openMode field"
```

---

## Task 4: Route Registration + Permission Groups

**Files:**
- Modify: `api/route.go`
- Modify: `api/api_admin_permissions.go`

- [ ] **Step 1: Register announcement routes in `api/route.go`**

In the `admin` group (the `/app` group with `AdminRequired()` middleware, around line 244), add the announcement routes. Place them after the campaigns block (after line 307):

```go
		// 公告管理
		admin.GET("/announcements", api_admin_list_announcements)
		admin.POST("/announcements", api_admin_create_announcement)
		admin.PUT("/announcements/:id", api_admin_update_announcement)
		admin.DELETE("/announcements/:id", api_admin_delete_announcement)
		admin.POST("/announcements/:id/activate", api_admin_activate_announcement)
		admin.POST("/announcements/:id/deactivate", api_admin_deactivate_announcement)
```

- [ ] **Step 2: Add permission groups in `api/api_admin_permissions.go`**

Add `"announcements", "announcements.write"` to the `allGroups` slice:

```go
var allGroups = []string{
	"nodes", "nodes.write",
	"tunnels", "tunnels.write",
	"cloud", "cloud.write",
	"users", "users.write",
	"orders",
	"campaigns", "campaigns.write",
	"license_keys", "license_keys.write",
	"plans", "plans.write",
	"announcements", "announcements.write",
	"stats",
	"device_logs",
	"feedback_tickets", "feedback_tickets.write",
	"retailers", "retailers.write",
	"edm",
	"approvals", "approvals.write",
	"wallet", "wallet.write",
	"strategy", "strategy.write",
	"surveys",
	"admins",
}
```

Add `"announcements", "announcements.write"` to the `RoleMarketing` role in `roleGroupMap`:

```go
RoleMarketing: {"users", "orders", "retailers", "retailers.write", "edm", "campaigns", "campaigns.write", "license_keys", "license_keys.write", "stats", "surveys", "announcements", "announcements.write"},
```

- [ ] **Step 3: Verify compilation**

Run: `cd api && go build ./...`
Expected: Build succeeds.

- [ ] **Step 4: Commit**

```bash
git add api/route.go api/api_admin_permissions.go
git commit -m "feat(api): register announcement routes and permission groups"
```

---

## Task 5: Webapp — Add `openMode` Support to AnnouncementBanner

**Files:**
- Modify: `webapp/src/services/api-types.ts`
- Modify: `webapp/src/components/AnnouncementBanner.tsx`

- [ ] **Step 1: Add `openMode` to `Announcement` interface in `webapp/src/services/api-types.ts`**

Find the `Announcement` interface (around line 537) and add the field:

```typescript
// 公告信息
export interface Announcement {
  id: string; // 公告唯一ID，用于跟踪关闭状态
  message: string; // 公告文字内容
  linkUrl?: string; // 可选：点击跳转链接
  linkText?: string; // 可选：链接文字
  openMode?: 'external' | 'webview'; // 可选：打开方式，默认 external
  expiresAt?: number; // 可选：公告过期时间戳（Unix秒），为0表示不过期
}
```

- [ ] **Step 2: Update `handleLinkClick` in `webapp/src/components/AnnouncementBanner.tsx`**

Replace the existing `handleLinkClick` function (lines 111-121):

Old code:
```typescript
  const handleLinkClick = async (e: React.MouseEvent) => {
    e.preventDefault();
    if (!announcement?.linkUrl) return;

    try {
      await window._platform!.openExternal(announcement.linkUrl);
    } catch (error) {
      console.error('Failed to open link:', error);
      window.open(announcement.linkUrl, '_blank', 'noopener,noreferrer');
    }
  };
```

New code:
```typescript
  const handleLinkClick = async (e: React.MouseEvent) => {
    e.preventDefault();
    if (!announcement?.linkUrl) return;

    if (announcement.openMode === 'webview') {
      window.open(announcement.linkUrl, '_blank', 'noopener,noreferrer');
      return;
    }

    try {
      await window._platform!.openExternal(announcement.linkUrl);
    } catch (error) {
      console.error('Failed to open link:', error);
      window.open(announcement.linkUrl, '_blank', 'noopener,noreferrer');
    }
  };
```

- [ ] **Step 3: Verify compilation**

Run: `cd webapp && npx tsc --noEmit`
Expected: No type errors.

- [ ] **Step 4: Commit**

```bash
git add webapp/src/services/api-types.ts webapp/src/components/AnnouncementBanner.tsx
git commit -m "feat(webapp): add openMode support to AnnouncementBanner"
```

---

## Task 6: Manager Dashboard — API Client Methods

**Files:**
- Modify: `web/src/lib/api.ts`

- [ ] **Step 1: Add announcement types to `web/src/lib/api.ts`**

Find the campaign types section (around line 726) and add announcement types nearby:

```typescript
// Announcement-related interfaces
export interface AnnouncementRequest {
  message: string;
  linkUrl?: string;
  linkText?: string;
  openMode?: string; // 'external' | 'webview'
  expiresAt?: number;
  isActive?: boolean;
}

export interface AnnouncementResponse {
  id: number;
  createdAt: number;
  updatedAt: number;
  message: string;
  linkUrl: string;
  linkText: string;
  openMode: string;
  expiresAt: number;
  isActive: boolean;
}

export interface AnnouncementListResponse {
  items: AnnouncementResponse[];
  pagination?: Pagination | null;
}

export interface AnnouncementListParams {
  page?: number;
  pageSize?: number;
}
```

- [ ] **Step 2: Add API methods to the `ApiClient` class**

Find the campaign management methods section (around line 1279) and add announcement methods nearby:

```typescript
  // Announcement management APIs
  async getAnnouncements(params: AnnouncementListParams = {}): Promise<AnnouncementListResponse> {
    const query = new URLSearchParams();
    if (params.page) query.set('page', params.page.toString());
    if (params.pageSize) query.set('pageSize', params.pageSize.toString());
    return this.request<AnnouncementListResponse>(`/app/announcements${query.toString() ? '?' + query.toString() : ''}`);
  }

  async createAnnouncement(data: AnnouncementRequest): Promise<AnnouncementResponse> {
    return this.request<AnnouncementResponse>('/app/announcements', {
      method: 'POST',
      body: JSON.stringify(data),
    });
  }

  async updateAnnouncement(id: number, data: AnnouncementRequest): Promise<AnnouncementResponse> {
    return this.request<AnnouncementResponse>(`/app/announcements/${id}`, {
      method: 'PUT',
      body: JSON.stringify(data),
    });
  }

  async deleteAnnouncement(id: number): Promise<void> {
    return this.request<void>(`/app/announcements/${id}`, {
      method: 'DELETE',
    });
  }

  async activateAnnouncement(id: number): Promise<AnnouncementResponse> {
    return this.request<AnnouncementResponse>(`/app/announcements/${id}/activate`, {
      method: 'POST',
    });
  }

  async deactivateAnnouncement(id: number): Promise<AnnouncementResponse> {
    return this.request<AnnouncementResponse>(`/app/announcements/${id}/deactivate`, {
      method: 'POST',
    });
  }
```

- [ ] **Step 3: Verify compilation**

Run: `cd web && npx tsc --noEmit`
Expected: No type errors.

- [ ] **Step 4: Commit**

```bash
git add web/src/lib/api.ts
git commit -m "feat(web): add announcement API client methods"
```

---

## Task 7: Manager Dashboard — Sidebar Entry

**Files:**
- Modify: `web/src/components/manager-sidebar.tsx`

- [ ] **Step 1: Add Megaphone icon import**

In the import line (line 8), add `Megaphone` to the lucide-react imports:

```typescript
import { Package, Users, Server, Receipt, Mail, Tag, Wallet, FileText, Activity, LogOut, Gauge, UserCircle, ClipboardList, Cloud, BarChart3, Key, MessageSquare, ShieldCheck, Megaphone } from "lucide-react";
```

- [ ] **Step 2: Add announcements menu item to the "运营配置" group**

Find the "运营配置" group (line 41) and add the announcement entry:

```typescript
  {
    title: "运营配置",
    items: [
      { href: "/manager/plans", icon: Package, label: "套餐管理" },
      { href: "/manager/campaigns", icon: Tag, label: "优惠活动" },
      { href: "/manager/license-keys", icon: Key, label: "授权码" },
      { href: "/manager/announcements", icon: Megaphone, label: "公告管理" },
    ]
  },
```

- [ ] **Step 3: Commit**

```bash
git add web/src/components/manager-sidebar.tsx
git commit -m "feat(web): add announcements entry to manager sidebar"
```

---

## Task 8: Manager Dashboard — Announcements Page

**Files:**
- Create: `web/src/app/(manager)/manager/announcements/page.tsx`

- [ ] **Step 1: Create the announcements page**

```tsx
"use client";

export const dynamic = "force-dynamic";

import { useState, useEffect, useCallback } from "react";
import {
  useReactTable,
  getCoreRowModel,
  flexRender,
  type ColumnDef,
} from "@tanstack/react-table";
import { api, type AnnouncementResponse, type AnnouncementRequest } from "@/lib/api";
import { Button } from "@/components/ui/button";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
  DialogFooter,
} from "@/components/ui/dialog";
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from "@/components/ui/alert-dialog";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Textarea } from "@/components/ui/textarea";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { Badge } from "@/components/ui/badge";
import { toast } from "sonner";
import { Plus, Pencil, Trash2, Power, PowerOff } from "lucide-react";

function formatDate(ts: number): string {
  if (!ts) return "-";
  return new Date(ts * 1000).toLocaleString("zh-CN");
}

function getStatusBadge(item: AnnouncementResponse) {
  if (item.isActive) {
    // Check if expired
    if (item.expiresAt > 0 && Date.now() / 1000 > item.expiresAt) {
      return <Badge variant="destructive">已过期</Badge>;
    }
    return <Badge className="bg-green-600">活跃</Badge>;
  }
  return <Badge variant="secondary">停用</Badge>;
}

const initialForm: AnnouncementRequest = {
  message: "",
  linkUrl: "",
  linkText: "",
  openMode: "external",
  expiresAt: 0,
  isActive: false,
};

export default function AnnouncementsPage() {
  const [announcements, setAnnouncements] = useState<AnnouncementResponse[]>([]);
  const [loading, setLoading] = useState(true);
  const [pagination, setPagination] = useState({ page: 1, pageSize: 20, total: 0 });

  // Dialog state
  const [createDialogOpen, setCreateDialogOpen] = useState(false);
  const [editDialogOpen, setEditDialogOpen] = useState(false);
  const [editingAnnouncement, setEditingAnnouncement] = useState<AnnouncementResponse | null>(null);
  const [form, setForm] = useState<AnnouncementRequest>({ ...initialForm });
  const [submitting, setSubmitting] = useState(false);

  // Confirm dialogs
  const [activateTarget, setActivateTarget] = useState<AnnouncementResponse | null>(null);
  const [deleteTarget, setDeleteTarget] = useState<AnnouncementResponse | null>(null);

  const fetchAnnouncements = useCallback(async () => {
    setLoading(true);
    try {
      const res = await api.getAnnouncements({ page: pagination.page, pageSize: pagination.pageSize });
      setAnnouncements(res.items ?? []);
      if (res.pagination) {
        setPagination(prev => ({ ...prev, total: res.pagination!.total }));
      }
    } catch {
      toast.error("加载公告列表失败");
    } finally {
      setLoading(false);
    }
  }, [pagination.page, pagination.pageSize]);

  useEffect(() => {
    fetchAnnouncements();
  }, [fetchAnnouncements]);

  const resetForm = () => setForm({ ...initialForm });

  const handleCreate = async () => {
    if (!form.message.trim()) {
      toast.error("公告内容不能为空");
      return;
    }
    setSubmitting(true);
    try {
      await api.createAnnouncement(form);
      toast.success("公告创建成功");
      setCreateDialogOpen(false);
      resetForm();
      fetchAnnouncements();
    } catch {
      toast.error("创建失败");
    } finally {
      setSubmitting(false);
    }
  };

  const handleUpdate = async () => {
    if (!editingAnnouncement || !form.message.trim()) return;
    setSubmitting(true);
    try {
      await api.updateAnnouncement(editingAnnouncement.id, form);
      toast.success("公告更新成功");
      setEditDialogOpen(false);
      setEditingAnnouncement(null);
      resetForm();
      fetchAnnouncements();
    } catch {
      toast.error("更新失败");
    } finally {
      setSubmitting(false);
    }
  };

  const handleDelete = async () => {
    if (!deleteTarget) return;
    try {
      await api.deleteAnnouncement(deleteTarget.id);
      toast.success("公告已删除");
      setDeleteTarget(null);
      fetchAnnouncements();
    } catch {
      toast.error("删除失败");
    }
  };

  const handleActivate = async () => {
    if (!activateTarget) return;
    try {
      await api.activateAnnouncement(activateTarget.id);
      toast.success("公告已激活");
      setActivateTarget(null);
      fetchAnnouncements();
    } catch {
      toast.error("激活失败");
    }
  };

  const handleDeactivate = async (item: AnnouncementResponse) => {
    try {
      await api.deactivateAnnouncement(item.id);
      toast.success("公告已停用");
      fetchAnnouncements();
    } catch {
      toast.error("停用失败");
    }
  };

  const openEditDialog = (item: AnnouncementResponse) => {
    setEditingAnnouncement(item);
    setForm({
      message: item.message,
      linkUrl: item.linkUrl,
      linkText: item.linkText,
      openMode: item.openMode,
      expiresAt: item.expiresAt,
    });
    setEditDialogOpen(true);
  };

  const columns: ColumnDef<AnnouncementResponse>[] = [
    {
      accessorKey: "id",
      header: "ID",
      size: 60,
    },
    {
      accessorKey: "message",
      header: "公告内容",
      cell: ({ row }) => (
        <div className="max-w-[300px] truncate" title={row.original.message}>
          {row.original.message}
        </div>
      ),
    },
    {
      accessorKey: "openMode",
      header: "打开方式",
      size: 100,
      cell: ({ row }) => (
        <span>{row.original.openMode === "webview" ? "内部" : "外部"}</span>
      ),
    },
    {
      id: "status",
      header: "状态",
      size: 80,
      cell: ({ row }) => getStatusBadge(row.original),
    },
    {
      accessorKey: "createdAt",
      header: "创建时间",
      size: 160,
      cell: ({ row }) => formatDate(row.original.createdAt),
    },
    {
      accessorKey: "expiresAt",
      header: "过期时间",
      size: 160,
      cell: ({ row }) => row.original.expiresAt ? formatDate(row.original.expiresAt) : "不过期",
    },
    {
      id: "actions",
      header: "操作",
      size: 200,
      cell: ({ row }) => {
        const item = row.original;
        return (
          <div className="flex gap-1">
            <Button variant="ghost" size="sm" onClick={() => openEditDialog(item)}>
              <Pencil className="h-4 w-4" />
            </Button>
            {item.isActive ? (
              <Button variant="ghost" size="sm" onClick={() => handleDeactivate(item)}>
                <PowerOff className="h-4 w-4" />
              </Button>
            ) : (
              <Button variant="ghost" size="sm" onClick={() => setActivateTarget(item)}>
                <Power className="h-4 w-4" />
              </Button>
            )}
            <Button variant="ghost" size="sm" onClick={() => setDeleteTarget(item)}>
              <Trash2 className="h-4 w-4 text-red-500" />
            </Button>
          </div>
        );
      },
    },
  ];

  const table = useReactTable({
    data: announcements,
    columns,
    getCoreRowModel: getCoreRowModel(),
  });

  const renderForm = () => (
    <div className="grid gap-4 py-4">
      <div className="grid gap-2">
        <Label>公告内容 *</Label>
        <Textarea
          value={form.message}
          onChange={(e) => setForm({ ...form, message: e.target.value })}
          placeholder="输入公告文字内容（最多500字）"
          maxLength={500}
          rows={3}
        />
      </div>
      <div className="grid gap-2">
        <Label>链接地址</Label>
        <Input
          value={form.linkUrl ?? ""}
          onChange={(e) => setForm({ ...form, linkUrl: e.target.value })}
          placeholder="https://..."
        />
      </div>
      <div className="grid gap-2">
        <Label>链接文字</Label>
        <Input
          value={form.linkText ?? ""}
          onChange={(e) => setForm({ ...form, linkText: e.target.value })}
          placeholder="查看详情"
        />
      </div>
      <div className="grid gap-2">
        <Label>打开方式</Label>
        <Select
          value={form.openMode ?? "external"}
          onValueChange={(v) => setForm({ ...form, openMode: v })}
        >
          <SelectTrigger>
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            <SelectItem value="external">外部浏览器</SelectItem>
            <SelectItem value="webview">应用内打开</SelectItem>
          </SelectContent>
        </Select>
      </div>
      <div className="grid gap-2">
        <Label>过期时间</Label>
        <Input
          type="datetime-local"
          value={form.expiresAt ? new Date(form.expiresAt * 1000).toISOString().slice(0, 16) : ""}
          onChange={(e) => {
            const ts = e.target.value ? Math.floor(new Date(e.target.value).getTime() / 1000) : 0;
            setForm({ ...form, expiresAt: ts });
          }}
        />
        <p className="text-xs text-muted-foreground">留空表示不过期</p>
      </div>
    </div>
  );

  return (
    <div className="p-6">
      <div className="flex items-center justify-between mb-6">
        <h1 className="text-2xl font-bold">公告管理</h1>
        <Button onClick={() => { resetForm(); setCreateDialogOpen(true); }}>
          <Plus className="h-4 w-4 mr-2" />
          创建公告
        </Button>
      </div>

      <div className="rounded-md border">
        <Table>
          <TableHeader>
            {table.getHeaderGroups().map((headerGroup) => (
              <TableRow key={headerGroup.id}>
                {headerGroup.headers.map((header) => (
                  <TableHead key={header.id}>
                    {header.isPlaceholder ? null : flexRender(header.column.columnDef.header, header.getContext())}
                  </TableHead>
                ))}
              </TableRow>
            ))}
          </TableHeader>
          <TableBody>
            {loading ? (
              <TableRow>
                <TableCell colSpan={columns.length} className="text-center py-8">
                  加载中...
                </TableCell>
              </TableRow>
            ) : table.getRowModel().rows.length === 0 ? (
              <TableRow>
                <TableCell colSpan={columns.length} className="text-center py-8 text-muted-foreground">
                  暂无公告
                </TableCell>
              </TableRow>
            ) : (
              table.getRowModel().rows.map((row) => (
                <TableRow key={row.id}>
                  {row.getVisibleCells().map((cell) => (
                    <TableCell key={cell.id}>
                      {flexRender(cell.column.columnDef.cell, cell.getContext())}
                    </TableCell>
                  ))}
                </TableRow>
              ))
            )}
          </TableBody>
        </Table>
      </div>

      {/* Pagination */}
      {pagination.total > pagination.pageSize && (
        <div className="flex items-center justify-between mt-4">
          <p className="text-sm text-muted-foreground">
            共 {pagination.total} 条
          </p>
          <div className="flex gap-2">
            <Button
              variant="outline"
              size="sm"
              disabled={pagination.page <= 1}
              onClick={() => setPagination(prev => ({ ...prev, page: prev.page - 1 }))}
            >
              上一页
            </Button>
            <Button
              variant="outline"
              size="sm"
              disabled={pagination.page * pagination.pageSize >= pagination.total}
              onClick={() => setPagination(prev => ({ ...prev, page: prev.page + 1 }))}
            >
              下一页
            </Button>
          </div>
        </div>
      )}

      {/* Create Dialog */}
      <Dialog open={createDialogOpen} onOpenChange={setCreateDialogOpen}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>创建公告</DialogTitle>
          </DialogHeader>
          {renderForm()}
          <DialogFooter>
            <Button variant="outline" onClick={() => setCreateDialogOpen(false)}>取消</Button>
            <Button onClick={handleCreate} disabled={submitting}>
              {submitting ? "创建中..." : "创建"}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      {/* Edit Dialog */}
      <Dialog open={editDialogOpen} onOpenChange={setEditDialogOpen}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>编辑公告</DialogTitle>
          </DialogHeader>
          {renderForm()}
          <DialogFooter>
            <Button variant="outline" onClick={() => setEditDialogOpen(false)}>取消</Button>
            <Button onClick={handleUpdate} disabled={submitting}>
              {submitting ? "保存中..." : "保存"}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      {/* Activate Confirm */}
      <AlertDialog open={!!activateTarget} onOpenChange={(open) => !open && setActivateTarget(null)}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>确认激活</AlertDialogTitle>
            <AlertDialogDescription>
              激活此公告将自动停用当前活跃的公告。确认继续？
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>取消</AlertDialogCancel>
            <AlertDialogAction onClick={handleActivate}>确认激活</AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>

      {/* Delete Confirm */}
      <AlertDialog open={!!deleteTarget} onOpenChange={(open) => !open && setDeleteTarget(null)}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>确认删除</AlertDialogTitle>
            <AlertDialogDescription>
              确定要删除此公告吗？此操作不可恢复。
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>取消</AlertDialogCancel>
            <AlertDialogAction onClick={handleDelete} className="bg-red-600 hover:bg-red-700">确认删除</AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </div>
  );
}
```

- [ ] **Step 2: Verify compilation**

Run: `cd web && npx tsc --noEmit`
Expected: No type errors.

- [ ] **Step 3: Commit**

```bash
git add web/src/app/\(manager\)/manager/announcements/page.tsx
git commit -m "feat(web): add announcements manager page"
```

---

## Task 9: MCP Tools

**Files:**
- Create: `tools/kaitu-center/src/tools/admin-announcements.ts`
- Modify: `tools/kaitu-center/src/index.ts`

- [ ] **Step 1: Create `tools/kaitu-center/src/tools/admin-announcements.ts`**

```typescript
/**
 * Admin announcement management tools.
 */

import { z } from 'zod'
import { defineApiTool, type ToolRegistration } from '../tool-factory.js'

export const announcementTools: ToolRegistration[] = [
  defineApiTool({
    name: 'list_announcements',
    description: 'List all announcements (paginated, includes inactive/expired).',
    group: 'announcements',
    path: '/app/announcements',
  }),

  defineApiTool({
    name: 'create_announcement',
    description: 'Create a new announcement.',
    group: 'announcements.write',
    method: 'POST',
    params: {
      message: z.string().describe('Announcement text (max 500 chars)'),
      link_url: z.string().optional().describe('Optional click target URL'),
      link_text: z.string().optional().describe('Optional link display text'),
      open_mode: z.enum(['external', 'webview']).optional().describe('Link open mode: external (default) or webview'),
      expires_at: z.number().optional().describe('Expiry Unix timestamp (0 = never)'),
      is_active: z.boolean().optional().describe('Activate immediately (deactivates current active)'),
    },
    path: '/app/announcements',
    mapBody: (p) => ({
      message: p.message,
      linkUrl: p.link_url,
      linkText: p.link_text,
      openMode: p.open_mode,
      expiresAt: p.expires_at,
      isActive: p.is_active,
    }),
  }),

  defineApiTool({
    name: 'update_announcement',
    description: 'Update an existing announcement.',
    group: 'announcements.write',
    method: 'PUT',
    params: {
      id: z.number().describe('Announcement ID'),
      message: z.string().describe('Announcement text (max 500 chars)'),
      link_url: z.string().optional().describe('Click target URL'),
      link_text: z.string().optional().describe('Link display text'),
      open_mode: z.enum(['external', 'webview']).optional().describe('Link open mode'),
      expires_at: z.number().optional().describe('Expiry Unix timestamp (0 = never)'),
    },
    path: (p) => `/app/announcements/${p.id}`,
    mapBody: (p) => ({
      message: p.message,
      linkUrl: p.link_url,
      linkText: p.link_text,
      openMode: p.open_mode,
      expiresAt: p.expires_at,
    }),
  }),

  defineApiTool({
    name: 'delete_announcement',
    description: 'Soft-delete an announcement by ID.',
    group: 'announcements.write',
    method: 'DELETE',
    params: {
      id: z.number().describe('Announcement ID'),
    },
    path: (p) => `/app/announcements/${p.id}`,
  }),

  defineApiTool({
    name: 'activate_announcement',
    description: 'Activate an announcement (deactivates all others).',
    group: 'announcements.write',
    method: 'POST',
    params: {
      id: z.number().describe('Announcement ID'),
    },
    path: (p) => `/app/announcements/${p.id}/activate`,
  }),
]
```

- [ ] **Step 2: Register in `tools/kaitu-center/src/index.ts`**

Add the import after the existing factory tool imports (around line 37):

```typescript
import { announcementTools } from './tools/admin-announcements.js'
```

Add to the `allFactoryTools` array (around line 55):

```typescript
  ...strategyTools,
  ...announcementTools,
```

- [ ] **Step 3: Verify compilation**

Run: `cd tools/kaitu-center && npm run build`
Expected: Build succeeds.

- [ ] **Step 4: Commit**

```bash
git add tools/kaitu-center/src/tools/admin-announcements.ts tools/kaitu-center/src/index.ts
git commit -m "feat(mcp): add announcement management tools"
```

---

## Task 10: Verification

- [ ] **Step 1: Build all affected packages**

```bash
cd api && go build ./...
cd webapp && npx tsc --noEmit
cd web && npx tsc --noEmit
cd tools/kaitu-center && npm run build
```

Expected: All builds succeed.

- [ ] **Step 2: Run existing tests**

```bash
cd api && go test ./...
cd webapp && npx vitest run
cd tools/kaitu-center && npm test
```

Expected: All existing tests pass (no regressions).

- [ ] **Step 3: Verify unused viper config keys are harmless**

The old config keys (`frontend_config.announcement.*`) in config.yml are now dead code. They won't cause errors — viper simply won't read them since the code no longer references them. No config file changes needed.
