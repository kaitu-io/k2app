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
├── center-api.ts     # Shared HTTP client for the Center API, injects X-Access-Key
├── tool-factory.ts   # defineApiTool (Center envelope) + defineRestApiTool (raw REST) + fetchPermissions
├── audit.ts          # Fire-and-forget invocation log → ~/.kaitu-center/audit.log (500 KB rotation)
├── redact.ts         # Secret redaction for SSH command output
└── tools/
    ├── admin-*.ts    # Factory tools → Center Go API
    └── list-nodes / exec-on-node / ping-node / delete-node / download-device-log /
        # standalone hand-registered tools
skills/kaitu-content, skills/kaitu-marketing   # SKILL.md files shipped with the package
vitest.config.ts                               # aliases ./x.js → ./x.ts for tests
```

114 tools when every group is granted: 109 factory (admin) + 5 standalone. (The 21 `cms` tools were removed 2026-09 with Payload CMS — content is now Velite markdown in `web/content/`.) Registration is gated per tool by the groups returned from `GET /app/my-permissions` (`index.ts createServer`); there is no local role table.

## Conventions

- **NodeNext imports**: `tsconfig.json` uses `"module": "NodeNext"`. All relative imports in `.ts` source MUST use `.js` extension (`import { foo } from './bar.js'`). TypeScript compiles directly to ESM output — no bundler. Exception: `*.test.ts` import `./x.ts` directly — they are excluded from `tsc` (`tsconfig.json` `exclude`) and `vitest.config.ts` aliases `.js`→`.ts`.
- **Save-to-file for large outputs**: MCP tools that return large payloads save to temp dirs instead of streaming to the conversation:
  - `download_device_log` → `join(os.tmpdir(), 'kaitu-device-logs')` (returns path + metadata, not contents)
  - `exec_on_node` → stdout > 4000 chars saved to `join(os.tmpdir(), 'kaitu-exec-output')`, inline copy cut at 4000; stderr cut at 2000 and never saved
  - `os.tmpdir()` is `$TMPDIR` (`/var/folders/.../T/`) on macOS, not `/tmp`.
- **Independent package**: `tools/kaitu-center/` has its own `package-lock.json`, not part of the root yarn workspace. Use `npm` here, `yarn` everywhere else.

## Tool factory

Tools target two different backends with different response envelopes:

- **`defineApiTool`** — Center Go API at `KAITU_CENTER_URL`. Always HTTP 200; errors signaled in `{code, message, data}` envelope. Factory unwraps `data` on `code===0`, returns `{error, code}` otherwise. Used for all `admin-*.ts` tools.



## Gotchas

- **Config source**: `~/.kaitu-center/config.toml` (`[center] url/access_key`, `[ssh] private_key_path/user/port`), each overridable by `KAITU_CENTER_URL` / `KAITU_ACCESS_KEY` / `KAITU_SSH_KEY` / `KAITU_SSH_USER` / `KAITU_SSH_PORT`. Defaults when neither is set: Center `https://k2.52j.me` (not api.kaitu.io), SSH `ubuntu:1022`, key `~/.ssh/id_rsa` then `id_ed25519`.
- **Permission-gated registration**: `createServer` calls `GET /app/my-permissions` and registers only tools whose `group` is in the returned list — a missing group means the tool silently does not exist. If the call fails, `fetchPermissions` returns `groups: []` and the server starts with zero tools and no error.
- **Audit log**: every invocation appends its params to `~/.kaitu-center/audit.log` (500 KB rotation); redaction (`redact.ts`) applies to SSH command output, not to audit entries.
- **`S3_BUCKET_URL`** is hardcoded to `kaitu-service-logs.s3.ap-northeast-1` in `download-device-log.ts`.
- **Node**: `engines.node >= 22`.

## Related Docs

- [Root Architecture](../../CLAUDE.md)
- [Center API](../../api/CLAUDE.md) — backend endpoints consumed by `center-api.ts`
