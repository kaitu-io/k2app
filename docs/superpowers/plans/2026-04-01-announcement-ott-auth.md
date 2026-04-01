# Announcement OTT Auth Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace standalone SurveyBanner with Announcement system + OTT (one-time token) for seamless webapp-to-web auth handoff.

**Architecture:** API issues a one-time Redis-backed token. Webapp requests OTT before opening announcement links with `authMode=ott`. Web receives OTT via URL, exchanges it for a cookie session, then redirects to the target page. Announcement model gains `authMode` field to control this behavior.

**Tech Stack:** Go/Gin/GORM/Redis (API), React/MUI/TypeScript (Webapp), Next.js/shadcn (Web), Node.js/MCP SDK (Tools)

**Spec:** `docs/superpowers/specs/2026-04-01-announcement-ott-auth-design.md`

---

### Task 1: API — OTT Issue & Exchange Endpoints

**Files:**
- Create: `api/api_auth_ott.go`
- Modify: `api/route.go:54-74` (auth group) and `:114-156` (user group)

- [ ] **Step 1: Create `api/api_auth_ott.go` with OTT issue handler**

```go
package center

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"net/url"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/spf13/viper"
	db "github.com/wordgate/qtoolkit/db"
	"github.com/wordgate/qtoolkit/log"
	"github.com/wordgate/qtoolkit/redis"
)

const (
	ottPrefix = "ott:"
	ottTTL    = 300 // 5 minutes
)

type ottData struct {
	UserID   uint64 `json:"user_id"`
	Redirect string `json:"redirect"`
}

type DataOTTRequest struct {
	Redirect string `json:"redirect" binding:"required"`
}

type DataOTTResponse struct {
	URL string `json:"url"`
}

// isAllowedRedirect validates redirect URL: must be https, host must be kaitu.io or *.kaitu.io
func isAllowedRedirect(rawURL string) bool {
	u, err := url.Parse(rawURL)
	if err != nil {
		return false
	}
	if u.Scheme != "https" {
		return false
	}
	host := strings.ToLower(u.Hostname())
	return host == "kaitu.io" || strings.HasSuffix(host, ".kaitu.io")
}

// api_issue_ott issues a one-time token for webapp → web auth handoff
func api_issue_ott(c *gin.Context) {
	auth := getAuthContext(c)
	log.Infof(c, "user %d requesting OTT", auth.UserID)

	var req DataOTTRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		Error(c, ErrorInvalidArgument, "invalid request")
		return
	}

	if !isAllowedRedirect(req.Redirect) {
		Error(c, ErrorInvalidArgument, "redirect URL must be https on kaitu.io domain")
		return
	}

	// Generate 32-byte random token
	tokenBytes := make([]byte, 32)
	if _, err := rand.Read(tokenBytes); err != nil {
		log.Errorf(c, "failed to generate OTT: %v", err)
		Error(c, ErrorSystemError, "failed to generate token")
		return
	}
	token := hex.EncodeToString(tokenBytes)

	// Store in Redis
	data := ottData{UserID: auth.UserID, Redirect: req.Redirect}
	dataJSON, _ := json.Marshal(data)
	if err := redis.CacheSet(ottPrefix+token, string(dataJSON), ottTTL); err != nil {
		log.Errorf(c, "failed to store OTT in Redis: %v", err)
		Error(c, ErrorSystemError, "failed to store token")
		return
	}

	// Build exchange URL
	baseURL := viper.GetString("frontend_config.app_links.base_url")
	exchangeURL := baseURL + "/api/auth/ott/exchange?ott=" + token + "&redirect=" + url.QueryEscape(req.Redirect)

	log.Infof(c, "OTT issued for user %d, redirect: %s", auth.UserID, req.Redirect)
	Success(c, &DataOTTResponse{URL: exchangeURL})
}

// api_exchange_ott exchanges a one-time token for cookie session
func api_exchange_ott(c *gin.Context) {
	token := c.Query("ott")
	redirect := c.Query("redirect")

	if token == "" || redirect == "" {
		c.Redirect(302, "/auth/login?reason=invalid")
		return
	}

	// Get from Redis
	var dataJSON string
	exist, err := redis.CacheGet(ottPrefix+token, &dataJSON)
	if err != nil || !exist {
		log.Warnf(c, "OTT exchange failed: token not found or expired")
		c.Redirect(302, "/auth/login?reason=expired")
		return
	}

	// Delete immediately (one-time use)
	_ = redis.CacheDel(ottPrefix + token)

	// Parse stored data
	var data ottData
	if err := json.Unmarshal([]byte(dataJSON), &data); err != nil {
		log.Errorf(c, "OTT exchange failed: corrupt data: %v", err)
		c.Redirect(302, "/auth/login?reason=invalid")
		return
	}

	// Verify redirect matches stored value
	if data.Redirect != redirect {
		log.Warnf(c, "OTT exchange failed: redirect mismatch (stored=%s, got=%s)", data.Redirect, redirect)
		c.Redirect(302, "/auth/login?reason=invalid")
		return
	}

	// Look up user to get roles
	var user User
	if err := db.Get().First(&user, data.UserID).Error; err != nil {
		log.Errorf(c, "OTT exchange failed: user %d not found: %v", data.UserID, err)
		c.Redirect(302, "/auth/login?reason=expired")
		return
	}

	// Generate web cookie token (same as web login)
	authResult, _, err := generateWebCookieToken(c, user.ID, user.Roles)
	if err != nil {
		log.Errorf(c, "OTT exchange failed: token generation error: %v", err)
		c.Redirect(302, "/auth/login?reason=expired")
		return
	}

	// Set cookies and redirect
	setAuthCookies(c, authResult)
	log.Infof(c, "OTT exchange successful for user %d, redirecting to %s", user.ID, redirect)
	c.Redirect(302, redirect)
}
```

- [ ] **Step 2: Register routes in `route.go`**

In the `/api/auth` group (public, no auth middleware), add:
```go
auth.GET("/ott/exchange", api_exchange_ott)
```

In the `/api/user` group (after `AuthRequired()`), add:
```go
user.POST("/ott", api_issue_ott)
```

Note: Issue endpoint goes under `/api/user` (auth required), exchange under `/api/auth` (public). The issue URL becomes `POST /api/user/ott`, and the OTT response URL uses `/api/auth/ott/exchange`.

- [ ] **Step 3: Run tests**

Run: `cd api && go build ./...`
Expected: Compiles without errors.

- [ ] **Step 4: Commit**

```bash
git add api/api_auth_ott.go api/route.go
git commit -m "feat(api): add OTT issue and exchange endpoints for webapp→web auth handoff"
```

---

### Task 2: API — Announcement Model + Admin AuthMode

**Files:**
- Modify: `api/model.go:774-786`
- Modify: `api/api_admin_announcements.go:14-21,24-48,95-102,118-125,170-177,182-188,305-322`
- Modify: `api/api_app_config.go:24-31`

- [ ] **Step 1: Add `AuthMode` to Announcement model in `model.go`**

After the `OpenMode` line (783), add:
```go
AuthMode  string `gorm:"type:varchar(20);not null;default:'none'" json:"authMode"` // none | ott
```

- [ ] **Step 2: Add `AuthMode` to `AnnouncementRequest` in `api_admin_announcements.go`**

Add to the struct (after line 18):
```go
AuthMode  string `json:"authMode"`  // none | ott, default none
```

- [ ] **Step 3: Add `AuthMode` to `AnnouncementResponse`**

Add to the struct (after line 31):
```go
AuthMode  string `json:"authMode"`
```

- [ ] **Step 4: Add `AuthMode` mapping to `convertAnnouncementToResponse()`**

Add after `OpenMode` line (44):
```go
AuthMode:  a.AuthMode,
```

- [ ] **Step 5: Add `authMode` validation in create handler**

After the `openMode` validation block (lines 95-102), add:
```go
authMode := req.AuthMode
if authMode == "" {
	authMode = "none"
}
if authMode != "none" && authMode != "ott" {
	Error(c, ErrorInvalidArgument, "authMode must be 'none' or 'ott'")
	return
}
```

Set in the `Announcement{}` creation struct (after `OpenMode: openMode,`):
```go
AuthMode: authMode,
```

- [ ] **Step 6: Add `authMode` validation in update handler**

After the `openMode` validation block (lines 170-177), add same validation. Add to updates map:
```go
"auth_mode": authMode,
```

- [ ] **Step 7: Add `AuthMode` to `DataAnnouncement` in `api_app_config.go`**

Add to the struct (after line 29):
```go
AuthMode  string `json:"authMode,omitempty" example:"none"` // none | ott
```

- [ ] **Step 8: Add `AuthMode` mapping in `getActiveAnnouncement()`**

In the return block (after `OpenMode` line 319), add:
```go
AuthMode:  announcement.AuthMode,
```

- [ ] **Step 9: Verify build**

Run: `cd api && go build ./...`
Expected: Compiles without errors.

- [ ] **Step 10: Commit**

```bash
git add api/model.go api/api_admin_announcements.go api/api_app_config.go
git commit -m "feat(api): add authMode field to Announcement model and admin API"
```

---

### Task 3: Webapp — AnnouncementBanner OTT Logic

**Files:**
- Modify: `webapp/src/services/api-types.ts:537-544`
- Modify: `webapp/src/components/AnnouncementBanner.tsx:111-126`

- [ ] **Step 1: Add `authMode` to `Announcement` interface in `api-types.ts`**

After line 542 (`openMode`), add:
```typescript
authMode?: 'none' | 'ott'; // 可选：认证模式，默认 none
```

- [ ] **Step 2: Update `handleLinkClick` in `AnnouncementBanner.tsx`**

Add import for `cloudApi`:
```typescript
import { cloudApi } from '../services/cloud-api';
import { useAuthStore } from '../stores';
```

Replace `handleLinkClick` (lines 111-126) with:
```typescript
const handleLinkClick = async (e: React.MouseEvent) => {
  e.preventDefault();
  if (!announcement?.linkUrl) return;

  let targetUrl = announcement.linkUrl;

  // If authMode is 'ott' and user is authenticated, request OTT first
  if (announcement.authMode === 'ott' && useAuthStore.getState().isAuthenticated) {
    try {
      const { code, data } = await cloudApi.post<{ url: string }>('/api/user/ott', {
        redirect: announcement.linkUrl,
      });
      if (code === 0 && data?.url) {
        targetUrl = data.url;
      }
    } catch (error) {
      console.error('[AnnouncementBanner] OTT request failed, falling back to direct URL:', error);
      // Fallback: open original URL without auth
    }
  }

  if (announcement.openMode === 'webview') {
    window.open(targetUrl, '_blank', 'noopener,noreferrer');
    return;
  }

  try {
    await window._platform!.openExternal(targetUrl);
  } catch (error) {
    console.error('Failed to open link:', error);
    window.open(targetUrl, '_blank', 'noopener,noreferrer');
  }
};
```

- [ ] **Step 3: Verify TypeScript**

Run: `cd webapp && npx tsc --noEmit`
Expected: No type errors.

- [ ] **Step 4: Commit**

```bash
git add webapp/src/services/api-types.ts webapp/src/components/AnnouncementBanner.tsx
git commit -m "feat(webapp): add OTT auth support to AnnouncementBanner link handling"
```

---

### Task 4: Webapp — Remove SurveyBanner

**Files:**
- Delete: `webapp/src/components/SurveyBanner.tsx`
- Modify: `webapp/src/components/Layout.tsx:8,121`
- Modify: `webapp/src/stores/vpn-machine.store.ts:197-202`
- Modify: `webapp/src/i18n/locales/*/common.json` (7 locales)

- [ ] **Step 1: Remove SurveyBanner import and usage from `Layout.tsx`**

Remove line 8:
```typescript
import SurveyBanner from "./SurveyBanner";
```

Remove line 121:
```typescript
        <SurveyBanner />
```

- [ ] **Step 2: Remove connection counter from `vpn-machine.store.ts`**

Remove lines 197-202 (the `// Increment survey connection counter` block):
```typescript
  // Increment survey connection counter
  if (nextState === 'connected') {
    const key = 'k2_connect_success_count';
    const count = parseInt(localStorage.getItem(key) || '0', 10);
    localStorage.setItem(key, String(count + 1));
  }
```

- [ ] **Step 3: Remove `survey.banner_*` i18n keys from all 7 locales**

Remove the `"survey"` section from `common.json` in:
- `webapp/src/i18n/locales/zh-CN/common.json` (lines 172-175)
- `webapp/src/i18n/locales/en-US/common.json`
- `webapp/src/i18n/locales/ja/common.json`
- `webapp/src/i18n/locales/zh-TW/common.json`
- `webapp/src/i18n/locales/zh-HK/common.json`
- `webapp/src/i18n/locales/en-AU/common.json`
- `webapp/src/i18n/locales/en-GB/common.json`

- [ ] **Step 4: Delete `SurveyBanner.tsx`**

```bash
rm webapp/src/components/SurveyBanner.tsx
```

- [ ] **Step 5: Verify TypeScript + test**

Run: `cd webapp && npx tsc --noEmit`
Expected: No type errors, no missing imports.

- [ ] **Step 6: Commit**

```bash
git add -u webapp/
git commit -m "refactor(webapp): remove SurveyBanner — surveys now via Announcements"
```

---

### Task 5: Web — Manager Announcements + API Types

**Files:**
- Modify: `web/src/lib/api.ts:780-799`
- Modify: `web/src/app/(manager)/manager/announcements/page.tsx:68-74,187,209-213,297-311`

- [ ] **Step 1: Add `authMode` to API types in `web/src/lib/api.ts`**

Add to `AnnouncementRequest` (after line 784):
```typescript
authMode?: string; // 'none' | 'ott'
```

Add to `AnnouncementResponse` (after line 796):
```typescript
authMode: string;
```

- [ ] **Step 2: Add `authMode` to `initialForm` in announcements page**

After `openMode: "external"` (line 72), add:
```typescript
authMode: "none",
```

- [ ] **Step 3: Add `authMode` column to the table**

After the `openMode` column definition (around line 209-213), add a new column:
```typescript
{
  accessorKey: "authMode",
  header: "认证",
  cell: ({ row }) => (
    <span>{row.original.authMode === "ott" ? "自动登录" : "无"}</span>
  ),
},
```

- [ ] **Step 4: Add `authMode` to edit dialog form population**

In the `openEdit` handler where form is set from `item` (around line 187), add:
```typescript
authMode: item.authMode,
```

- [ ] **Step 5: Add `authMode` Select to the form component**

After the `openMode` Select block (around line 297-311), add:
```tsx
<div className="grid gap-2">
  <Label>认证模式</Label>
  <Select
    value={form.authMode ?? "none"}
    onValueChange={(v) => setForm({ ...form, authMode: v })}
  >
    <SelectTrigger>
      <SelectValue />
    </SelectTrigger>
    <SelectContent>
      <SelectItem value="none">不需要登录</SelectItem>
      <SelectItem value="ott">自动登录</SelectItem>
    </SelectContent>
  </Select>
</div>
```

- [ ] **Step 6: Verify build**

Run: `cd web && npx tsc --noEmit`
Expected: No type errors.

- [ ] **Step 7: Commit**

```bash
git add web/src/lib/api.ts web/src/app/\(manager\)/manager/announcements/page.tsx
git commit -m "feat(web): add authMode field to announcements manager and API types"
```

---

### Task 6: MCP Tools — Add auth_mode Parameter

**Files:**
- Modify: `tools/kaitu-center/src/tools/admin-announcements.ts:21-37,45-60`

- [ ] **Step 1: Add `auth_mode` to `create_announcement`**

In `params` (after `open_mode`, line 25), add:
```typescript
auth_mode: z.enum(['none', 'ott']).optional().describe('Auth mode: none (default) or ott (auto-login via one-time token)'),
```

In `mapBody` (after `openMode`, line 34), add:
```typescript
authMode: p.auth_mode,
```

- [ ] **Step 2: Add `auth_mode` to `update_announcement`**

In `params` (after `open_mode`, line 50), add:
```typescript
auth_mode: z.enum(['none', 'ott']).optional().describe('Auth mode: none or ott'),
```

In `mapBody` (after `openMode`, line 59), add:
```typescript
authMode: p.auth_mode,
```

- [ ] **Step 3: Build MCP tools**

Run: `cd tools/kaitu-center && npm run build`
Expected: Compiles without errors.

- [ ] **Step 4: Commit**

```bash
git add tools/kaitu-center/src/tools/admin-announcements.ts
git commit -m "feat(mcp): add auth_mode parameter to announcement tools"
```

---

### Task 7: Integration Verification

- [ ] **Step 1: Full API build**

Run: `cd api && go build ./...`
Expected: Clean compile.

- [ ] **Step 2: Webapp type check**

Run: `cd webapp && npx tsc --noEmit`
Expected: No errors.

- [ ] **Step 3: Web type check**

Run: `cd web && npx tsc --noEmit`
Expected: No errors.

- [ ] **Step 4: MCP tools build**

Run: `cd tools/kaitu-center && npm run build`
Expected: No errors.

- [ ] **Step 5: Run existing tests**

Run: `cd webapp && yarn test` and `cd api && go test ./...`
Expected: All pass.

- [ ] **Step 6: Final commit (if any remaining changes)**

```bash
git status  # Should be clean
```
