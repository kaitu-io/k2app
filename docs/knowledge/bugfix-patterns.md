# Bugfix Patterns

Issues discovered during implementation and their fixes.

---

## TypeScript Strict Array Access Requires Non-Null Assertion (2026-02-14, k2app-rewrite)

**Problem**: `noUncheckedIndexedAccess` causes `TS2532: Object is possibly 'undefined'` on array element access.

**Fix**: Non-null assertion `!` after length check: `if (arr.length > 0) arr[0]!.id`. Never use on unvalidated user input or API response arrays.

**Files fixed**: `webapp/src/stores/servers.store.ts`, `webapp/src/components/__tests__/ServerList.test.tsx`

**Validation**: `npx tsc --noEmit` passes.

---

## Missing Peer Dependencies in Yarn Workspaces (2026-02-14, k2app-rewrite)

**Problem**: `@testing-library/react` requires `@testing-library/dom` as peer. Parallel agents installing in isolated worktrees left combined `node_modules` stale after merge.

**Fix**: `yarn install` from workspace root after merging branches that modify `package.json`.

**Rule**: Always run `yarn install` from root after merge.

---

## Unused Import Causes TS6133 After Merge (2026-02-14, k2app-rewrite)

**Problem**: Branch W1 imported `vi` for spy functionality. Branch W3 refactored tests, making `vi` unused. Merge created unused import.

**Fix**: Remove unused import.

**Prevention**: Run `tsc --noEmit` after every merge as delivery gate.

---

## Go→JS JSON Key Mismatch Causes Silent Data Loss (2026-02-14, mobile-rewrite)

**Problem**: Go `json.Marshal` outputs `connected_at`, TypeScript type declares `connectedAt`. Value exists under wrong key — `status.connectedAt === undefined` with no runtime error.

**Discovery**: Code review compared Go `Engine.StatusJSON()` output keys against `K2PluginInterface` TypeScript definitions.

**Fix**: Added `remapStatusKeys()` in K2Plugin.swift and K2Plugin.kt to transform snake_case → camelCase at native bridge.

**Prevention**: CLAUDE.md convention — always remap Go JSON keys at native bridge boundary.

**Cross-reference**: See Architecture Decisions → "Go→JS JSON Key Remapping" for the architectural decision.

---

## Overbroad .gitignore Silently Hides Source Files (2026-02-14, mobile-rewrite)

**Problem**: `.gitignore` patterns `mobile/ios/` and `mobile/android/` ignored all files, including source. Agent-created Swift/Kotlin files invisible to git — completely silent failure.

**Discovery**: Code review ran `git check-ignore` on source files.

**Fix**: Replaced directory patterns with targeted build artifact patterns (Pods/, build/, .gradle/).

**Prevention**: CLAUDE.md convention — never ignore entire source directories.

---

## Missing Info.plist Causes Runtime Failures in iOS App (2026-02-14, mobile-rewrite)

**Problem**: K2Plugin.swift calls `Bundle.main.infoDictionary?["CFBundleShortVersionString"]` — returns `nil` without Info.plist, version shows as "unknown".

**Root cause**: iOS agent created PacketTunnelExtension's Info.plist but not the main app target's.

**Fix**: Created `mobile/ios/App/App/Info.plist` with all required keys.

**Prevention**: When creating iOS targets, verify both main app and extension targets have their own Info.plist files.

---

## gomobile bind Fails Without Pre-Created Output Directory (2026-02-14, mobile-rewrite)

**Problem**: `gomobile bind` fails if the output directory (`k2/build/`) doesn't exist.

**Symptom**: `mkdir: no such file or directory` during mobile build.

**Fix**: Makefile creates `k2/build/` before running `gomobile bind`. See Makefile targets `build-mobile-ios` and `build-mobile-android`.

**Prevention**: Always create output directories before tool invocations that expect them.

---
