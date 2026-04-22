# Kaitu CMS MCP — Design

**Date:** 2026-04-22
**Status:** Approved, pending plan
**Scope:** MCP tools for Payload CMS management + Posts brand-visibility field

## Problem

Two gaps on Payload v3 CMS (Kaitu web, mounted at `/manager/cms`):

1. No programmatic interface for Claude Code to manage content (Posts, Categories, Tags, Media). Manual admin UI only.
2. `src/app/[locale]/blog/page.tsx` and `[slug]/page.tsx` read `post.brand` / `post.canonicalBrand` defensively, but `Posts.ts` collection has no such fields — defensive fallback treats all posts as visible on both brand hosts (kaitu.io + overleap.*).

## Non-Goals

- GraphQL API for CMS (REST only)
- Non-admin read tokens / public CMS reads (public pages already use Local API with `overrideAccess: true`)
- Third brand beyond Kaitu + Overleap (YAGNI)
- `canonicalBrand` field (locale-based fallback in existing frontend is sufficient until proven otherwise)
- Split `cms.read` / `cms.write` permission groups (team of 1–3, single `cms` group)

## Architecture

```
┌──────────────┐  stdio  ┌────────────────────┐
│  Claude Code │ ──────► │  kaitu-center      │
│              │         │  MCP (TS)          │
│              │         │                    │
│              │         │  CenterApiClient ──┼──► Center (Go)  /api/user/info
│              │         │  (target='center')│     /app/*  (60+ existing tools)
│              │         │                    │
│              │         │  CmsApiClient  ────┼──► Next.js + Payload
│              │         │  (target='cms')    │     /payload/api/*
│              │         └────────────────────┘            │
│              │                                           │ centerAuthStrategy
│              │                                           │ forwards X-Access-Key
│              │                                           ▼
│              │                                     Center /api/user/info
└──────────────┘                                           │
                                                           ▼
                                                  (isAdmin check, admins upsert)
```

**Auth chain per request**
1. MCP sends `X-Access-Key: ktu_xxx` to Payload REST
2. Payload runs `centerAuthStrategy` — reads X-Access-Key header, forwards to Center `/api/user/info`
3. Center's existing `X-Access-Key` middleware (`api/middleware.go:200`) validates key → returns user info
4. centerAuthStrategy does `isAdmin || (roles & 0xfffffffe) !== 0` check + upserts `admins` record
5. Payload's `access: isAdmin` (fixed in commit `5aa8f57`) admits the request
6. For Posts saves: existing `setAuthorFromRequest` / `setPublishedAt` / `autoTranslate` hooks fire

**Packaging decision: merge CMS tools into existing `tools/kaitu-center/`** rather than a separate `tools/kaitu-cms/` package. Shared infrastructure (`tool-factory`, `audit`, `config`, entrypoint) avoids duplication; `.mcp.json` stays at one MCP server; same `KAITU_ACCESS_KEY` covers all tools.

## Changes

### 1. Payload: auth bridge (`web/src/payload/auth/centerAuthStrategy.ts`)

Rename strategy `center-cookie` → `center-auth`. Extend to accept `X-Access-Key` header in addition to the existing `access_token` cookie. Priority: cookie first (preserves web admin login), then X-Access-Key.

- Read `x-access-key` (case-insensitive) from incoming `Headers`
- Forward chosen credential to Center `/api/user/info`:
  - cookie present → `Cookie: access_token=<token>`
  - X-Access-Key present → `X-Access-Key: <key>`
- All downstream logic (isAdmin check, admins upsert, race handling) unchanged
- Update one reference: `Admins.ts` `strategies: [centerAuthStrategy]` stays the same (import path unchanged; only the internal `name` field changes)

Tests: add X-Access-Key cases to `web/tests/payload/centerAuthStrategy.test.ts` — present/invalid/center-rejected/combined-with-cookie precedence/header case-insensitivity. Existing cookie tests untouched.

### 2. Payload: Posts visibility fields (`web/src/payload/collections/Posts.ts`)

Add two boolean fields after `status`:

```ts
{
  name: 'showOnKaitu',
  type: 'checkbox',
  defaultValue: true,
  required: true,
  custom: { translatorSkip: true },
  admin: { description: '显示在 Kaitu 品牌站（kaitu.io）' },
},
{
  name: 'showOnOverleap',
  type: 'checkbox',
  defaultValue: true,
  required: true,
  custom: { translatorSkip: true },
  admin: { description: '显示在 Overleap 品牌站（overleap.*）' },
},
```

Add a `beforeValidate` collection hook that blocks published-with-both-false:

```ts
// Block the footgun of "published + invisible on every brand"
({ data }) => {
  if (data?.status === 'published' && !data.showOnKaitu && !data.showOnOverleap) {
    throw new ValidationError({
      errors: [{ path: 'showOnOverleap',
        message: '发布状态下至少勾选一个品牌站点；如需全部隐藏请改为 draft' }],
    })
  }
  return data
}
```

**Field decisions:**
- `type: 'checkbox'` (booleans), not a multi-select — simpler query shape, cleaner mental model
- `defaultValue: true` + `required: true` — existing posts backfill to `(true, true)` on migration, preserving current "visible everywhere" behavior
- `translatorSkip` + non-localized — visibility is cross-locale metadata, not content
- Schema forbids `(false, false)` only when `status='published'` — leaves draft state flexible

### 3. Payload: migration (`web/src/migrations/<ts>_add_post_brand_visibility.ts`)

Generated via `cd web && yarn payload migrate:create add_post_brand_visibility`. Expected SQL:

```sql
ALTER TABLE posts ADD COLUMN show_on_kaitu boolean DEFAULT true NOT NULL;
ALTER TABLE posts ADD COLUMN show_on_overleap boolean DEFAULT true NOT NULL;
ALTER TABLE _posts_v ADD COLUMN version_show_on_kaitu boolean DEFAULT true NOT NULL;
ALTER TABLE _posts_v ADD COLUMN version_show_on_overleap boolean DEFAULT true NOT NULL;
```

Deploy order: run migration against prod → then deploy new code. Reverse order breaks existing queries.

**Prerequisite check:** `yarn payload migrate:create` must succeed despite the `@payload-enchants/translator` ESM dir-import bug. Smoke-test first (generate empty migration, discard) before implementation. Fallback: hand-write the migration.

### 4. Payload: blog frontend (`web/src/app/[locale]/blog/page.tsx`, `[slug]/page.tsx`)

Replace `brand` / `canonicalBrand` defensive fallback logic with direct queries on the new fields.

**List page (`page.tsx`)** — filter at DB level:
```ts
const field = currentBrand.id === 'kaitu' ? 'showOnKaitu' : 'showOnOverleap'
const { docs } = await payload.find({
  collection: 'posts',
  where: {
    and: [
      { status: { equals: 'published' } },
      { [field]: { equals: true } },
    ],
  },
  // ... rest unchanged
})
```

**Detail page (`[slug]/page.tsx`)** — boolean check:
```ts
const visible = currentBrand.id === 'kaitu' ? post.showOnKaitu : post.showOnOverleap
if (!visible) notFound()
```

Canonical URL resolution continues using locale fallback (`resolveCanonicalBrand`) — `canonicalBrand` field is explicitly not added.

### 5. kaitu-center: factory target routing (`tools/kaitu-center/src/tool-factory.ts`)

Extend `ApiToolDef`:
```ts
target?: 'center' | 'cms'   // default 'center'
```

Change the `register()` signature to receive a `clients` object:
```ts
register: (server, clients: { center: CenterApiClient; cms: CenterApiClient }) => void
```

Inside registration, select the client: `const client = clients[def.target ?? 'center']`.

All 60+ existing tools omit `target` → continue using center client. Zero semantic change.

### 6. kaitu-center: CMS client + config

- `src/config.ts` — add `cms` section with `url` field; read `KAITU_CMS_URL` env (default `http://localhost:3000` for dev)
- `src/center-api.ts` — either parameterize baseUrl on construction or export a `createCmsClient(config)` helper; same `X-Access-Key` header injection
- `src/index.ts` — instantiate both clients, pass `{ center, cms }` to tool registration

### 7. kaitu-center: CMS tool files

Five new files under `tools/kaitu-center/src/tools/`:

**`cms-posts.ts`** — 8 `defineApiTool` declarations + 1 standalone (`get_post_all_locales`):

| Tool | Method | Path | Notes |
|---|---|---|---|
| `list_posts` | GET | `/payload/api/posts` | passes `locale`, `where`, `page`, `limit`, `sort` |
| `get_post` | GET | `/payload/api/posts/:id` | passes `locale`, `draft` |
| `create_post` | POST | `/payload/api/posts?locale=zh-CN` | enforces zh-CN source |
| `update_post` | PATCH | `/payload/api/posts/:id` | partial body |
| `delete_post` | DELETE | `/payload/api/posts/:id` | |
| `publish_post` | PATCH | `/payload/api/posts/:id` | body `{status: 'published'}`, `setPublishedAt` hook auto-fills |
| `unpublish_post` | PATCH | `/payload/api/posts/:id` | body `{status: 'draft'}` |
| `retranslate_post` | PATCH | `/payload/api/posts/:id?locale=zh-CN` | body `{}` — empty PATCH still fires afterChange → `autoTranslate` |
| `get_post_all_locales` | — | standalone | MCP-side parallel fetch of 7 locales, aggregated |

**`cms-categories.ts`** — `list_categories` / `create_category` / `update_category` / `delete_category`

**`cms-tags.ts`** — `list_tags` / `create_tag` / `update_tag` / `delete_tag`

**`cms-media.ts`** — `list_media` / `update_media_alt` / `delete_media`

**`cms-upload-media.ts`** — standalone implementation of `upload_media` (multipart/form-data to `/payload/api/media`). Not `defineApiTool` because factory handles only JSON bodies.

All tools declare `group: 'cms'`.

### 8. kaitu-center: index wiring (`src/index.ts`)

Add imports and register the 4 declarative files into `allFactoryTools`; register `upload_media` and `get_post_all_locales` via standalone registrars.

### 9. Center (Go) — ops-only step

**No code change.** Requires one operations action: in Center admin UI, grant `cms` permission group to the `ktu_cfba...` access key used by kaitu-center MCP. Without this, MCP's `fetchPermissions` won't include `cms` → CMS tools won't register.

Enforcement note: the `group: 'cms'` label controls MCP-side tool **visibility**, not Payload-side **authorization**. Payload still uses `access: isAdmin`. Anyone with an admin key reaching Payload REST directly bypasses the group check. For current team size this is acceptable; tightening to per-resource roles is a future concern.

## Test Strategy

| Layer | Test | File |
|---|---|---|
| Payload auth | X-Access-Key 5 cases: present / invalid / Center-rejected / combined-with-cookie precedence / header case-insensitivity | `web/tests/payload/centerAuthStrategy.test.ts` (extend) |
| Posts fields | `showOnKaitu`/`showOnOverleap` exist; beforeValidate blocks `published + (false,false)` and allows `draft + (false,false)` | `web/tests/payload/posts-brand-visibility.test.ts` (new) |
| Blog frontend | Visibility filter returns expected subset per brand | `web/tests/payload/blog-brand-filter.test.ts` (new) |
| MCP factory | `target: 'cms'` routes to cms client; default routes to center client | `tools/kaitu-center/src/tool-factory.test.ts` (extend) |
| MCP CMS tools | `list_posts` / `create_post` / `publish_post` / `upload_media`: happy path + 1 error path each, mocked HTTP | `tools/kaitu-center/src/tools/cms-*.test.ts` (new) |

## Implementation Order

Each step has a clear checkpoint and can be committed independently.

1. **Smoke `payload migrate:create`** (30 min) — verify `@payload-enchants/translator` ESM bug doesn't block CLI. Generate empty migration, discard. Fall back to hand-written migration if blocked.
2. **Posts visibility fields + migration + tests** (1 h) — independent PR, safe to deploy.
3. **centerAuthStrategy X-Access-Key + rename + tests** (30 min) — independent PR.
4. **Blog frontend switches to new fields** (15 min) — depends on #2.
5. **Deploy #2 + #3 + #4 to prod**: migrate DB → Amplify deploy.
6. **kaitu-center factory + config + client changes** (1–2 h) — no behavior change for existing tools; adds target routing.
7. **CMS tool files (5 files)** (2–3 h).
8. **Center ops: grant `cms` group to access key** (5 min).
9. **Smoke: `/mcp` reconnect + run list_posts, create_post, publish_post in Claude Code.**

Steps 1–5 are backend; can ship without touching the MCP. Steps 6–9 are MCP-side; can ship after backend is live.

## Known Risks

- `yarn payload migrate:create` may hit the translator ESM bug. Mitigation: step #1 smoke-test before committing.
- Empty-body PATCH firing afterChange (for `retranslate_post`) relies on Payload's behavior that all saves trigger hooks regardless of body contents. Mitigation: the MCP tool test covers this.
- `access: isAdmin` check in Payload is coarse — any admin token gets full CMS access regardless of MCP-side `cms` group membership. Accepted trade-off for current team size; future tightening via per-resource roles is out of scope.
