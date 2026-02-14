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
