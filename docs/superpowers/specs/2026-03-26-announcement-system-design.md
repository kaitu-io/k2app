# Announcement System — DB + Admin API

**Date**: 2026-03-26
**Status**: Approved

## Summary

Migrate the announcement feature from static config.yml to DB-backed storage with admin CRUD API, manager dashboard page, and MCP tools. Add `open_mode` field to control whether links open in external browser or webview. Frontend webapp already has `AnnouncementBanner` component fully wired — only needs `openMode` field support added.

## DB Model

Table: `announcements` (GORM soft delete)

| Column | Type | Default | Description |
|--------|------|---------|-------------|
| id | BIGINT UNSIGNED AUTO_INCREMENT | PK | Also used as frontend dismiss tracking ID (stringified) |
| message | VARCHAR(500) NOT NULL | — | Announcement text |
| link_url | VARCHAR(1024) NOT NULL | '' | Optional: click target URL |
| link_text | VARCHAR(100) NOT NULL | '' | Optional: link display text |
| open_mode | VARCHAR(20) NOT NULL | 'external' | 'external' (system browser) or 'webview' (in-app) |
| expires_at | BIGINT NOT NULL | 0 | Unix seconds, 0 = never expires |
| is_active | TINYINT(1) NOT NULL | 0 | Only one row can be active at a time |
| created_at | DATETIME(3) | NULL | GORM auto |
| updated_at | DATETIME(3) | NULL | GORM auto |
| deleted_at | DATETIME(3) | NULL | GORM soft delete |

Index: `idx_active (is_active, deleted_at)` for fast active announcement lookup.

**Invariant**: At most one row has `is_active=1` at any time. Activating a new announcement deactivates all others in the same transaction.

## Backend API

### Admin Endpoints (`/app/announcements`, AdminRequired middleware)

| Method | Path | Description |
|--------|------|-------------|
| GET | `/app/announcements` | List all (paginated, includes inactive/expired history) |
| POST | `/app/announcements` | Create announcement. If `is_active: true`, deactivate others in same tx |
| PUT | `/app/announcements/:id` | Update fields (message, link_url, link_text, open_mode, expires_at) |
| DELETE | `/app/announcements/:id` | Soft delete |
| POST | `/app/announcements/:id/activate` | Set active (deactivate all others in same tx) |
| POST | `/app/announcements/:id/deactivate` | Set inactive |

### Public Endpoint Change

`GET /api/app/config` — change `announcement` field source from viper config to DB query:

```
SELECT * FROM announcements
WHERE is_active = 1 AND deleted_at IS NULL
  AND (expires_at = 0 OR expires_at > UNIX_TIMESTAMP())
LIMIT 1
```

Response shape unchanged. Add `openMode` field:

```json
{
  "announcement": {
    "id": "42",
    "message": "...",
    "linkUrl": "https://...",
    "linkText": "查看详情",
    "openMode": "external",
    "expiresAt": 0
  }
}
```

`openMode` uses `omitempty` — absent means external (backward compatible).

### Files to Create/Modify

- **Create**: `api/api_admin_announcement.go` — admin CRUD handlers
- **Modify**: `api/model.go` — add `Announcement` GORM model
- **Modify**: `api/api_app_config.go` — replace viper read with DB query, add `OpenMode` to `DataAnnouncement`
- **Modify**: `api/route.go` — register `/app/announcements` routes

## Frontend Webapp Changes

### Type Addition

`webapp/src/services/api-types.ts` — add `openMode` to `Announcement` interface:

```typescript
export interface Announcement {
  id: string;
  message: string;
  linkUrl?: string;
  linkText?: string;
  openMode?: 'external' | 'webview';  // new field
  expiresAt?: number;
}
```

### AnnouncementBanner Change

`webapp/src/components/AnnouncementBanner.tsx` — modify `handleLinkClick`:

- `openMode === 'webview'` or absent/undefined `_platform`: use `window.open(url, '_blank')`
- Default (undefined / 'external'): use `window._platform.openExternal(url)` (current behavior)

This is the only frontend change needed. No layout, styling, or dismissal logic changes.

## Manager Dashboard

New page: `web/src/app/(manager)/manager/announcements/page.tsx`

Add sidebar entry in manager layout navigation.

### Page Features

- **Table**: columns — message (truncated), status badge (Active/Inactive/Expired), open_mode, created_at
- **Create button** → dialog form: message (textarea), linkUrl, linkText, openMode (select), expiresAt (date picker)
- **Row actions**: Edit (dialog), Activate/Deactivate (confirm dialog for activate: "will deactivate current active announcement"), Delete (confirm)
- Follow existing manager page patterns (campaigns page as reference)

## MCP Tools

Add to `tools/kaitu-center/`:

| Tool | Description |
|------|-------------|
| `list_announcements` | List all announcements (paginated) |
| `create_announcement` | Create new announcement |
| `update_announcement` | Update announcement fields |
| `delete_announcement` | Soft delete |
| `activate_announcement` | Activate (deactivates others) |

Follow existing factory declaration pattern in the MCP server.

## Backward Compatibility

- Frontend `AnnouncementBanner` already handles `announcement: undefined` (no-op) and `announcement.linkUrl: undefined` (no link shown)
- `openMode` absent → defaults to external browser → identical to current behavior
- Config.yml `frontend_config.announcement.*` keys become dead config after migration — can be removed from config files at any time without impact
