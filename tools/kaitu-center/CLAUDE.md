# kaitu-center — MCP Server + OpenClaw Plugin

TypeScript tools for DevOps, support, and marketing workflows over the Center API. Entry point: MCP server for Claude Code (`src/index.ts`). The OpenClaw plugin (`src/openclaw.ts`, manifest `openclaw.plugin.json`) is a zero-tool stub that only warns "pending migration to tool-factory".

## Commands

```bash
cd tools/kaitu-center && npm install
cd tools/kaitu-center && npm run build   # Compile TypeScript → dist/
cd tools/kaitu-center && npm test        # vitest
```

## Architecture

```
src/
├── index.ts          # MCP entry (@modelcontextprotocol/sdk, stdio). Registers tools by permission group
├── openclaw.ts       # OpenClaw plugin entry — stub, registers no tools (manifest: ../openclaw.plugin.json)
├── config.ts         # ~/.kaitu-center/config.toml + env overrides (see Gotchas)
├── ssh.ts            # Node SSH via ssh2
├── center-api.ts     # Shared HTTP client (Center + CMS targets), injects X-Access-Key
├── tool-factory.ts   # defineApiTool (Center envelope) + defineRestApiTool (raw REST) + fetchPermissions
├── audit.ts          # Fire-and-forget invocation log → ~/.kaitu-center/audit.log (500 KB rotation)
├── redact.ts         # Secret redaction for SSH command output
└── tools/
    ├── admin-*.ts    # Factory tools → Center Go API
    ├── cms-*.ts      # Factory tools → Payload CMS REST (/payload/api)
    └── list-nodes / exec-on-node / ping-node / delete-node / download-device-log /
        cms-upload-media / cms-post-helpers .ts   # 7 standalone hand-registered tools
skills/kaitu-content, skills/kaitu-marketing   # SKILL.md files shipped with the package
vitest.config.ts                               # aliases ./x.js → ./x.ts for tests
```

135 tools when every group is granted: 127 factory (109 admin + 18 cms) + 8 standalone. Registration is gated per tool by the groups returned from `GET /app/my-permissions` (`index.ts createServer`); there is no local role table.

## Conventions

- **NodeNext imports**: `tsconfig.json` uses `"module": "NodeNext"`. All relative imports in `.ts` source MUST use `.js` extension (`import { foo } from './bar.js'`). TypeScript compiles directly to ESM output — no bundler. Exception: `*.test.ts` import `./x.ts` directly — they are excluded from `tsc` (`tsconfig.json` `exclude`) and `vitest.config.ts` aliases `.js`→`.ts`.
- **Save-to-file for large outputs**: MCP tools that return large payloads save to temp dirs instead of streaming to the conversation:
  - `download_device_log` → `join(os.tmpdir(), 'kaitu-device-logs')` (returns path + metadata, not contents)
  - `exec_on_node` → stdout > 4000 chars saved to `join(os.tmpdir(), 'kaitu-exec-output')`, inline copy cut at 4000; stderr cut at 2000 and never saved
  - `os.tmpdir()` is `$TMPDIR` (`/var/folders/.../T/`) on macOS, not `/tmp`.
- **Independent package**: `tools/kaitu-center/` has its own `package-lock.json`, not part of the root yarn workspace. Use `npm` here, `yarn` everywhere else.

## Two factories — Center vs CMS

Tools target two different backends with different response envelopes:

- **`defineApiTool`** — Center Go API at `KAITU_CENTER_URL`. Always HTTP 200; errors signaled in `{code, message, data}` envelope. Factory unwraps `data` on `code===0`, returns `{error, code}` otherwise. Used for all `admin-*.ts` tools.
- **`defineRestApiTool`** — Payload REST at `KAITU_CMS_URL` (Next.js). Raw JSON body on 2xx; HTTP 4xx/5xx with `{errors:[{message}]}` envelope. Factory returns body verbatim; HTTP errors are thrown by `CenterApiClient` and surface as `{error}` text. Used for all `cms-*.ts` tools.

Both take `ApiClients` (`{center, cms}`); standalone multi-call tools (`upload_media`, `retranslate_post`) receive `clients.cms` directly at registration.

## CMS tools (group: `cms`)

The `cms` group is granted only to `IsAdmin` users — no role bitmask in `roleGroupMap` includes it (`api/api_admin_permissions.go`), so non-superadmin keys never see these tools. 21 tools total (18 factory + 3 standalone) targeting Payload CMS at `/payload/api/*`:

| Collection | Tools |
|-----------|-------|
| Posts | `list_posts`, `get_post`, `create_post`, `update_post`, `delete_post`, `publish_post`, `unpublish_post` |
| Post helpers (standalone) | `get_post_all_locales` (parallel 7-locale fetch), `retranslate_post` (GET+PATCH to re-fire autoTranslate hook) |
| Categories | `list_categories`, `create_category`, `update_category`, `delete_category` |
| Tags | `list_tags`, `create_tag`, `update_tag`, `delete_tag` |
| Media | `list_media`, `update_media_alt`, `delete_media`, `upload_media` (multipart standalone) |

Authentication: Payload uses the same `X-Access-Key` header as Center. `web/src/payload/auth/centerAuthStrategy.ts` calls back to Center `/api/user/info` to validate the key, then upserts a Payload admin record. Cookie auth still works in parallel for browser admin. Only accounts with `isAdmin` or a non-user role pass, and the web process must have `CENTER_API_URL` set or every key is rejected.

Spec: [`docs/superpowers/specs/2026-04-22-kaitu-cms-mcp-design.md`](../../docs/superpowers/specs/2026-04-22-kaitu-cms-mcp-design.md).

## Gotchas

- **Config source**: `~/.kaitu-center/config.toml` (`[center] url/access_key`, `[cms] url`, `[ssh] private_key_path/user/port`), each overridable by `KAITU_CENTER_URL` / `KAITU_ACCESS_KEY` / `KAITU_CMS_URL` / `KAITU_SSH_KEY` / `KAITU_SSH_USER` / `KAITU_SSH_PORT`. Defaults when neither is set: Center `https://k2.52j.me` (not api.kaitu.io), CMS `http://localhost:3000`, SSH `ubuntu:1022`, key `~/.ssh/id_rsa` then `id_ed25519`.
- **Permission-gated registration**: `createServer` calls `GET /app/my-permissions` and registers only tools whose `group` is in the returned list — a missing group means the tool silently does not exist. If the call fails, `fetchPermissions` returns `groups: []` and the server starts with zero tools and no error.
- **Audit log**: every invocation appends its params to `~/.kaitu-center/audit.log` (500 KB rotation); redaction (`redact.ts`) applies to SSH command output, not to audit entries.
- **`S3_BUCKET_URL`** is hardcoded to `kaitu-service-logs.s3.ap-northeast-1` in `download-device-log.ts`.
- **Node**: `engines.node >= 22`.

## Related Docs

- [Root Architecture](../../CLAUDE.md)
- [Center API](../../api/CLAUDE.md) — backend endpoints consumed by `center-api.ts`
