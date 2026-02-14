# Task Splitting

Lessons from decomposing features into parallel worktree tasks.

---

## Parallel Worktree File Overlap Causes Merge Conflicts (2026-02-14, k2app-rewrite)

**Observation**: Parallel tasks that touch the same file always produce merge conflicts.

**Conflicts encountered**:
- D1 (service) + D2 (tray) both modified `desktop/src-tauri/src/main.rs` → manual merge
- D3 (updater) on top of D1+D2 → another `main.rs` merge (3-way combine)
- W1 (VpnClient) + W5 (layout) both modified `webapp/src/App.tsx` → manual merge
- W2 (auth) + W3 (dashboard) both modified `webapp/src/App.tsx` → manual merge

**Pattern**: Entry-point files (`main.rs`, `App.tsx`, `mod.rs`, `index.ts`) are natural conflict hotspots because every feature registers itself there.

**Mitigation strategies**:
1. Accept that entry-point conflicts are inevitable in parallel execution
2. Keep feature-specific code in separate files; only imports/registration in entry points
3. When splitting, note which tasks will touch the same entry point and plan merge order
4. Merge the simpler task first, then resolve the more complex one on top

**What did NOT work**: Trying to avoid all file overlap. Some files (route config, plugin registration, store initialization) are architectural chokepoints that every feature must touch.

**Validation**: All 6 merge conflicts were resolved successfully during k2app-rewrite fire execution.

---

## Desktop Tasks (D1/D2/D3) Are Naturally Parallel Despite main.rs Overlap (2026-02-14, k2app-rewrite)

**Observation**: Service manager, tray, and updater each have their own module file (`service.rs`, `tray.rs`, `updater.rs`) with well-defined interfaces. The only overlap is `main.rs` registration.

**Splitting pattern for Tauri desktop tasks**:
- Each task owns one `mod.rs` module with clear public API
- `main.rs` only imports and wires: `.plugin()`, `.invoke_handler()`, `.setup()`
- Merge order: wire one at a time into setup closure

**Why this works**:
- Module files have zero overlap (different files)
- `main.rs` conflict is mechanical (just combining registrations)
- No runtime interaction between modules during initial implementation

---

## Webapp Features Should Depend on Shared Infrastructure Tasks (2026-02-14, k2app-rewrite)

**Observation**: W2 (auth), W3 (dashboard), W4 (servers) all depended on W1 (VpnClient) and W5 (i18n/layout). This dependency graph was correct — attempting to parallelize W2/W3 with W1 would have caused broken imports.

**Dependency rule**: If a task imports from another task's output (store, component, utility), it must wait for that task to complete.

**Safe parallelism**: W2 and W3 could run in parallel because they don't import from each other. W4 depended on both (server selection triggers VPN connection from W3).

---

## Yarn Workspace Lock File Conflicts (2026-02-14, k2app-rewrite)

**Problem**: S1 (webapp) and S2 (desktop) both created `yarn.lock` in their worktrees. Merging produced a conflict.

**Solution**: Take either branch's `yarn.lock`, then run `yarn install` from root to regenerate.

**Rule**: Never manually merge `yarn.lock`. Accept one side and regenerate.

**Validation**: `yarn install` after merge produced clean lock file, all dependencies resolved.

---
