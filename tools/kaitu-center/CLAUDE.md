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
├── center-api.ts     # Center API HTTP client
└── tools/            # Tool implementations per role
```

## Conventions

- **NodeNext imports**: `tsconfig.json` uses `"module": "NodeNext"`. All relative imports in `.ts` source MUST use `.js` extension (`import { foo } from './bar.js'`). TypeScript compiles directly to ESM output — no bundler.
- **Save-to-file for large outputs**: MCP tools that return large payloads save to temp dirs instead of streaming to the conversation:
  - `download_device_log` → `/tmp/kaitu-device-logs/` (returns path + metadata, not contents)
  - `exec_on_node` → stdout > 4 kB saved to `/tmp/kaitu-exec-output/`
  - Callers use the `Read` tool to inspect files.
- **Independent package**: `tools/kaitu-center/` has its own `package-lock.json`, not part of the root yarn workspace. Use `npm` here, `yarn` everywhere else.

## Related Docs

- [Root Architecture](../../CLAUDE.md)
- [Center API](../../api/CLAUDE.md) — backend endpoints consumed by `center-api.ts`
