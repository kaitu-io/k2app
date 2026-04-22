# kaitu-center — MCP Server + OpenClaw Plugin

TypeScript tools for DevOps, support, and marketing workflows over the Center API. Two entry points: MCP server for Claude Code, OpenClaw plugin for ops.

## Commands

```bash
cd tools/kaitu-center && npm install
cd tools/kaitu-center && npm run build   # Compile TypeScript → dist/
cd tools/kaitu-center && npm test        # vitest
```

## Architecture

```
src/
├── index.ts          # MCP entry (@modelcontextprotocol/sdk, stdio transport)
├── openclaw.ts       # OpenClaw plugin entry
├── roles.ts          # Role definitions (DevOps / Support / Marketing)
├── config.ts         # Environment + credential loading
├── ssh.ts            # Node SSH via ssh2
├── center-api.ts     # Shared HTTP client (Center + CMS targets)
├── tool-factory.ts   # defineApiTool (Center envelope) + defineRestApiTool (raw REST)
└── tools/            # Tool implementations per role
    ├── admin-*.ts    # Center-API tools (Go backend)
    └── cms-*.ts      # Payload CMS tools (Next.js REST via /payload/api)
```

## Conventions

- **NodeNext imports**: `tsconfig.json` uses `"module": "NodeNext"`. All relative imports in `.ts` source MUST use `.js` extension (`import { foo } from './bar.js'`). TypeScript compiles directly to ESM output — no bundler.
- **Save-to-file for large outputs**: MCP tools that return large payloads save to temp dirs instead of streaming to the conversation:
  - `download_device_log` → `/tmp/kaitu-device-logs/` (returns path + metadata, not contents)
  - `exec_on_node` → stdout > 4 kB saved to `/tmp/kaitu-exec-output/`
  - Callers use the `Read` tool to inspect files.
- **Independent package**: `tools/kaitu-center/` has its own `package-lock.json`, not part of the root yarn workspace. Use `npm` here, `yarn` everywhere else.

## Two factories — Center vs CMS

Tools target two different backends with different response envelopes:

- **`defineApiTool`** — Center Go API at `KAITU_CENTER_URL`. Always HTTP 200; errors signaled in `{code, message, data}` envelope. Factory unwraps `data` on `code===0`, returns `{error, code}` otherwise. Used for all `admin-*.ts` tools.
- **`defineRestApiTool`** — Payload REST at `KAITU_CMS_URL` (Next.js). Raw JSON body on 2xx; HTTP 4xx/5xx with `{errors:[{message}]}` envelope. Factory returns body verbatim; HTTP errors are thrown by `CenterApiClient` and surface as `{error}` text. Used for all `cms-*.ts` tools.

Both factories take an `ApiClients` param (`{center, cms}`) so one MCP server can serve both targets. Standalone tools that compose multiple calls (e.g., `upload_media`, `retranslate_post`) import the raw `CenterApiClient` type and pick the right client from `clients.cms` at registration time.

## CMS tools (group: `cms`)

Requires the access key to have the `cms` permission group in Center's registry. 21 tools total targeting Payload CMS at `/payload/api/*`:

| Collection | Tools |
|-----------|-------|
| Posts | `list_posts`, `get_post`, `create_post`, `update_post`, `delete_post`, `publish_post`, `unpublish_post` |
| Post helpers (standalone) | `get_post_all_locales` (parallel 7-locale fetch), `retranslate_post` (GET+PATCH to re-fire autoTranslate hook) |
| Categories | `list_categories`, `create_category`, `update_category`, `delete_category` |
| Tags | `list_tags`, `create_tag`, `update_tag`, `delete_tag` |
| Media | `list_media`, `update_media_alt`, `delete_media`, `upload_media` (multipart standalone) |

Authentication: Payload uses the same `X-Access-Key` header as Center. `web/src/payload/auth/centerAuthStrategy.ts` calls back to Center `/api/user/info` to validate the key, then upserts a Payload admin record. Cookie auth still works in parallel for browser admin.

Spec: [`docs/superpowers/specs/2026-04-22-kaitu-cms-mcp-design.md`](../../docs/superpowers/specs/2026-04-22-kaitu-cms-mcp-design.md).

## Related Docs

- [Root Architecture](../../CLAUDE.md)
- [Center API](../../api/CLAUDE.md) — backend endpoints consumed by `center-api.ts`
