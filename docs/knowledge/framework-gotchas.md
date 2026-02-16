# Framework Gotchas

Platform-specific issues and workarounds discovered during implementation.

---

## WebKit Mixed Content Blocking on macOS (2026-02-14, k2app-rewrite)

**Problem**: macOS WebKit blocks `https://` → `http://` requests even for loopback. Tauri default origin `https://tauri.localhost` cannot call daemon at `http://127.0.0.1:1777`.

**Root cause**: WebKit enforces mixed content strictly (no loopback exception). Chromium (Windows) allows it.

**Solution**: `tauri-plugin-localhost` serves webapp from `http://localhost:14580`. HTTP→HTTP calls are allowed. See `desktop/src-tauri/src/main.rs` — `Builder::new(14580).build()`.

**Security**: localhost port accessible to local processes, but daemon already exposes :1777 (CORS protected). Attack surface unchanged.

**Applies to**: macOS + Linux WebKitGTK. Not Windows (Chromium-based).

**Validation**: fetch `/ping` from webview succeeds; no console mixed content errors.

---

## Go json.Marshal snake_case vs JavaScript camelCase (2026-02-14, mobile-rewrite)

**Problem**: Go `json.Marshal` outputs `connected_at`, TypeScript expects `connectedAt`. Raw Go JSON passed through native bridges causes silent `undefined` values — no runtime error, just missing data.

**Discovery**: Code review caught mismatch between Go `Engine.StatusJSON()` output keys and `K2PluginInterface` TypeScript definitions.

**Solution**: `remapStatusKeys()` in both K2Plugin.swift and K2Plugin.kt transforms keys at the native bridge boundary. Convention added to CLAUDE.md.

**Prevention rule**: When passing Go JSON to JavaScript, always remap keys at the bridge.

**Cross-reference**: See Bugfix Patterns → "Go→JS JSON Key Mismatch" for discovery details. See Architecture Decisions → "Go→JS JSON Key Remapping" for the decision rationale.

---

## .gitignore Overbroad Patterns Hide Source Files (2026-02-14, mobile-rewrite)

**Problem**: `.gitignore` patterns like `mobile/ios/` ignore ALL files including source files.

**Symptom**: Source files created by agents invisible to git. `git status` shows nothing. Completely silent.

**Solution**: Replace directory patterns with targeted build artifact patterns (`Pods/`, `build/`, `.gradle/`, `libs/`). Convention added to CLAUDE.md.

**Verification**: `git check-ignore <source-file>` should return nothing.

**Cross-reference**: See Bugfix Patterns → "Overbroad .gitignore" for the original discovery.

---

## Capacitor Plugin Dynamic Import to Avoid Desktop Bundle Bloat (2026-02-14, mobile-rewrite)

**Problem**: Module-level `import { K2Plugin } from 'k2-plugin'` causes Vite to bundle Capacitor deps into desktop build.

**Solution**: Dynamic import with variable indirection + `@vite-ignore` comment in `webapp/src/vpn-client/index.ts`. Vite skips static analysis for this import. `k2-plugin` only available at runtime on native platforms.

**Trade-off**: Lose static analysis for this import. Type safety maintained via local `K2PluginType` interface in `native-client.ts`.

---

## Vite Dev Proxy vs Production baseUrl (2026-02-14, k2app-rewrite)

**Problem**: HttpVpnClient needs different baseUrl in dev (Vite proxy, relative URLs) vs production (no proxy, absolute `http://127.0.0.1:1777`).

**Solution**: `import.meta.env.DEV` compile-time switch. Dev: empty baseUrl (relative, proxied via `vite.config.ts`). Prod: absolute daemon URL. See `webapp/src/vpn-client/http-client.ts:16`.

**Why**: Vite proxy is dev-only. Relative URLs avoid CORS preflight. Absolute URLs required in prod (Tauri serves static files, no proxy).

---

## Tauri Version Reference from Parent package.json (2026-02-14, k2app-rewrite)

**Problem**: Tauri `version` must match root `package.json` to prevent drift.

**Solution**: `"version": "../../package.json"` in `desktop/src-tauri/tauri.conf.json`. Tauri CLI resolves paths ending in `.json` and reads the `version` field.

**Gotcha**: Path is relative from `desktop/src-tauri/` to root — hence `../../` not `../`.

---

## Zustand Store Initialization with Async VpnClient (2026-02-14, k2app-rewrite)

**Problem**: Zustand stores are synchronous — `await` not allowed in `create()` callback.

**Solution**: Separate `init()` async action called from React `useEffect`. Store created with `ready: null` initial state; `init()` calls async VpnClient methods and updates via `set()`.

**Pattern**: Used by all stores — `useVpnStore.init()`, `useAuthStore.restoreSession()`, `useServersStore.fetchServers()`. Getter actions use `get()` closure for current state without subscription.

**Validating tests**: `webapp/src/stores/__tests__/vpn.store.test.ts`

---

## Git Submodule in Monorepo Workspace (2026-02-14, k2app-rewrite)

**Problem**: k2 is a Git submodule (Go), but yarn workspaces expects package.json in each workspace.

**Solution**: Only include actual yarn packages in workspaces: `["webapp", "desktop", "mobile"]` — NOT `"k2"`. k2 is built via Makefile (`cd k2 && go build`), initialized via `git submodule update --init`.

**CI gotcha**: Private submodule requires SSH agent setup in GitHub Actions workflows.

---

## Service Readiness Retry on Startup (2026-02-14, k2app-rewrite)

**Problem**: k2 daemon takes variable time to start after Tauri app launches. Immediate readiness check fails.

**Solution**: `ServiceReadiness.tsx` retries up to 20 times at 500ms intervals (10s total). Shows "Starting service..." during retry, "Service not available" with manual retry button on timeout.

**State machine**: Loading → Retrying (20×500ms) → Ready | Failed → (manual retry) → Retrying.

**Why not just wait longer**: 10s covers 99% of cases. Manual retry button handles edge cases without blocking all users with a long spinner.

**Validating tests**: `webapp/src/components/__tests__/ServiceReadiness.test.tsx` (if exists), manual dev testing.

---
