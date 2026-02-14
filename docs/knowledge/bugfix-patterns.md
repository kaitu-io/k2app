# Bugfix Patterns

Issues discovered during implementation and their fixes.

---

## TypeScript Strict Array Access Requires Non-Null Assertion (2026-02-14, k2app-rewrite)

**Problem**: `noUncheckedIndexedAccess` (implied by strict mode) causes `TS2532: Object is possibly 'undefined'` on array element access.

**Symptom**:
```typescript
const servers = [{ id: 'a' }, { id: 'b' }];
set({ selectedServerId: servers[0].id });  // TS2532
```

**Fix**: Non-null assertion operator `!` when the index is guaranteed valid:
```typescript
if (servers.length > 0) {
  set({ selectedServerId: servers[0]!.id });  // OK
}
```

**When to use `!`**:
- Array access after length check (`if (arr.length > 0) arr[0]!`)
- `getAllByRole()` returns in test assertions (length validated by `getAllByRole`)
- Never on user input or API response arrays without guards

**Files fixed**:
- `webapp/src/stores/servers.store.ts` — `servers[0]!.id` after length check
- `webapp/src/components/__tests__/ServerList.test.tsx` — `buttons[1]!.className` in test assertions

**Validation**: `npx tsc --noEmit` passes after fix.

---

## Missing Peer Dependencies in Yarn Workspaces (2026-02-14, k2app-rewrite)

**Problem**: `@testing-library/react` requires `@testing-library/dom` as peer dependency, but yarn doesn't always auto-install peers.

**Symptom**:
```
Error: Cannot find module '@testing-library/dom'
```
4 test suites failed simultaneously.

**Fix**: Run `yarn install` from workspace root to resolve peer dependencies.

**Why it happened**: Parallel agents installed dependencies in isolated worktrees. When merged, the combined `node_modules` was stale. Re-running `yarn install` resolved the peer dependency tree.

**Rule**: After merging branches that modified `package.json`, always run `yarn install` from root before running tests.

**Validation**: All 95 tests pass after `yarn install`.

---

## Unused Import Causes TS6133 After Merge (2026-02-14, k2app-rewrite)

**Problem**: Branch W1 imported `vi` from vitest for spy functionality. After merging W3 which refactored the tests, `vi` became unused.

**Symptom**: `TS6133: 'vi' is declared but its value is never read`

**Fix**: Remove the unused import.

**Prevention**: Run `tsc --noEmit` after every merge as part of the delivery gate.

**Validation**: `npx tsc --noEmit` passes.

---

## Go→JS JSON Key Mismatch Causes Silent Data Loss (2026-02-14, mobile-rewrite)

**Problem**: Go `json.Marshal` outputs `connected_at`, but TypeScript type declares `connectedAt`. The value exists but is inaccessible under the expected key.

**Symptom**: `status.connectedAt === undefined` even though the JSON contains the data under `status.connected_at`. No runtime error — just missing data in the UI.

**Discovery**: Code review found the mismatch by comparing Go's `Engine.StatusJSON()` output keys against `K2PluginInterface` TypeScript definitions.

**Fix**: Added `remapStatusKeys()` method in both K2Plugin.swift and K2Plugin.kt to transform snake_case keys to camelCase at the native bridge boundary.

**Files fixed**:
- `mobile/plugins/k2-plugin/ios/Plugin/K2Plugin.swift` — `remapStatusKeys(_:)`
- `mobile/plugins/k2-plugin/android/src/main/java/io/kaitu/k2plugin/K2Plugin.kt` — `remapStatusKeys(obj)`

**Prevention**: Added AGENT.md convention: "Go→JS JSON key convention: Go `json.Marshal` outputs snake_case. JS/TS expects camelCase. Native bridge layers must remap keys at the boundary."

---

## Overbroad .gitignore Silently Hides Source Files (2026-02-14, mobile-rewrite)

**Problem**: `.gitignore` patterns `mobile/ios/` and `mobile/android/` ignored all files including source files that should be tracked.

**Symptom**: Source files created by agents were invisible to git. `git status` showed no untracked files. No error messages — completely silent.

**Discovery**: Code review checked `git check-ignore` and found source files were being ignored.

**Fix**: Replaced overbroad directory patterns with targeted build artifact patterns (Pods/, build/, .gradle/, etc.).

**Prevention**: Added AGENT.md convention: "Never ignore entire source directories. Only ignore build artifacts."

**Verification**: `git check-ignore <path>` on source files should return nothing.

---

## Missing Info.plist Causes Runtime Failures in iOS App (2026-02-14, mobile-rewrite)

**Problem**: iOS K2Plugin.swift calls `Bundle.main.infoDictionary?["CFBundleShortVersionString"]` in `checkReady()` and `getVersion()`. Without `Info.plist`, these return `nil` → version shows as "unknown".

**Root cause**: T1 (iOS) agent didn't create `Info.plist` for the main app target. The PacketTunnelExtension's `Info.plist` was created but not the app's.

**Fix**: Created `mobile/ios/App/App/Info.plist` with all required keys.

**Prevention**: When creating iOS targets, always verify both the main app and extension targets have their own Info.plist files.

---
