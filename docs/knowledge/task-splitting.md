# Task Splitting

Lessons from decomposing features into parallel worktree tasks.

---

## Entry-Point Files Are Merge Conflict Hotspots (2026-02-14, k2app-rewrite)

**Observation**: `main.rs`, `App.tsx`, `mod.rs`, `index.ts` — every feature registers itself in these files. Parallel tasks touching the same entry point always conflict.

**k2app-rewrite conflicts**: 6 total — D1+D2 on `main.rs`, D3 on top, W1+W5 on `App.tsx`, W2+W3 on `App.tsx`.

**Mitigation**: Accept that entry-point conflicts are inevitable. Keep feature code in separate files; only imports/registration in entry points. Merge simpler task first. All 6 conflicts were resolved successfully.

---

## Desktop Tasks: Parallel Modules, Sequential main.rs Wiring (2026-02-14, k2app-rewrite)

**Pattern**: Each Tauri task owns one module file (service.rs, tray.rs, updater.rs) with clear public API. Only `main.rs` overlaps — conflict is mechanical (combining registrations). Module files have zero overlap.

---

## Webapp: Shared Infrastructure Must Complete Before Feature Tasks (2026-02-14, k2app-rewrite)

**Rule**: If a task imports from another task's output (store, component, utility), it must wait. W2 (auth), W3 (dashboard), W4 (servers) all depended on W1 (VpnClient) and W5 (i18n/layout).

**Safe parallelism**: Tasks that don't import from each other (W2 and W3) can run in parallel.

---

## Yarn Lock Files: Never Manually Merge (2026-02-14, k2app-rewrite)

**Rule**: When parallel worktrees produce conflicting `yarn.lock`, take either side and run `yarn install` from root to regenerate. Never manually resolve lock file conflicts.

---

## Platform Boundaries = Zero Merge Conflicts (2026-02-14, mobile-rewrite)

**Observation**: Mobile-rewrite T1 (iOS), T2 (Android), T3 (NativeVpnClient) merged with **zero conflicts** because file system boundaries align with platform boundaries — iOS and Android directories are completely separate.

**Contrast with desktop**: Desktop features all register in `main.rs` (6 conflicts). Mobile has no shared entry-point file. K2Plugin TypeScript definitions were created in T0 (foundation), so T1/T2/T3 only read them.

**Lesson**: Plan tasks along platform boundaries when possible. Shared definitions go in a foundation task that completes first.

---

## Mobile Build Pipeline: Order Matters (2026-02-14, mobile-rewrite)

**Pipeline**: `gomobile bind` → copy library into native project → `npx cap sync` → native build (xcodebuild/gradlew).

**Why order matters**: `gomobile bind` produces the native library (xcframework/AAR). `cap sync` generates native config from web config. iOS needs `pod install` after sync. Reversing any step breaks the build.

**Gotcha**: `k2/build/` must exist before `gomobile bind` — Makefile creates it.

---

## Simple Features: Sequential Tasks, No Parallelism Benefit (2026-02-16, mobile-debug)

**Observation**: mobile-debug had 2 tasks (T1: debug.html + vite config, T2: Settings entry). T2 depended on T1. No parallelism possible. Worktree overhead (create, install deps, merge, cleanup) exceeded the implementation time.

**Lesson**: For simple features (<5 files, sequential deps), worktrees add overhead without benefit. The fire protocol's worktree model pays off at moderate+ complexity where parallel execution is possible.

**Rule of thumb**: If `depends_on` graph is a straight line (T1→T2→T3), consider working directly on a single branch instead of per-task worktrees.

---

## Foundation-Then-Features Task Dependency Pattern (2026-02-16, kaitu-feature-migration)

**Pattern**: Large migrations split into Foundation tasks (F1–F4) then parallel Feature tasks (T5–T11). Foundation tasks establish interfaces and shared infrastructure. Feature tasks implement pages/flows using those foundations.

**kaitu-feature-migration dependency graph**:
```
F1 (API) ──────┐
               ├── F3 (Stores) ──┐
F2 (Platform) ─┤                 ├── F4 (Nav+Layout+Global) ──┬── T5 (Purchase)
               └─────────────────┘                            ├── T6 (Invite)
                                                              ├── T7 (Account)
                                                              ├── T8 (Dashboard)
                                                              ├── T9 (Device/Member/History)
                                                              ├── T10 (FAQ/Issues/Tickets)
                                                              └── T11 (Email/Install/Changelog/Discover)
```

**Why this ordering**:
- F1 (API layer) and F2 (Platform abstraction) are fully parallel (no shared code)
- F3 (Stores) depends on F1 (API imports)
- F4 (Layout + global components) depends on F2 + F3 (uses platform APIs, imports stores)
- T5–T11 all depend on F4 (shared layout, guards, design tokens) but are mutually independent

**Merge order**: F1 ‖ F2 → F3 → F4 → (T5 ‖ T6 ‖ T7 ‖ T8 ‖ T9 ‖ T10 ‖ T11). Critical path length: 4 (shortest with parallelism).

**Conflicts**: 0 conflicts in F1/F2 merge (different directories). F4 established app.css and App.tsx routes — all feature tasks read, never modify entry points.

**Validating artifact**: Plan dependency graph and execution summary in `.word9f/kaitu-feature-migration/plan.md` lines 520–533, 960–979

---

## Design Token First, Components Second (2026-02-16, kaitu-feature-migration)

**Pattern**: Define ALL color/spacing/typography tokens in `app.css` before writing any components. Components reference tokens via `bg-[--color-*]` — zero hardcoded colors in component files.

**Execution order**:
1. F4 (foundation): Establish full token palette in `app.css` (97 lines of CSS variables)
2. F4: Create component UI pattern reference in plan (card, button, input, list item, etc.)
3. T5–T11 (features): Implement pages using tokens only

**Benefits**:
- Visual consistency enforced by token reference
- Easy theme changes (edit tokens, all components update)
- No "magic hex values" scattered across codebase
- Tailwind v4 `@theme` integrates CSS variables natively

**Trade-off**: More upfront design work. But kaitu-feature-migration had a source of truth (old kaitu webapp dark palette) — direct replication.

**Validating tests**: All component tests implicitly validate (they render without errors). `dark-theme.test.ts` explicitly checks CSS variable application.

---

## Entry-Point Wiring Must Be an Explicit Task (2026-02-16, kaitu-feature-migration)

**Problem**: 11 parallel tasks built 16 pages, 8 stores, 25 components, 279 tests — but App.tsx was never updated. All pages were orphaned (files exist, no routes). Gap analysis showed 94% routing gap despite 100% component completion.

**Root cause**: No task was scoped to rewrite App.tsx. Each feature task built its own page file + test. The plan assumed Layout + BottomNav (F4) covered routing, but Layout only handles keep-alive rendering — App.tsx defines the actual `<Routes>` tree.

**Fix**: Added explicit route wiring step after all feature tasks merged. One commit: 16 routes + 7 global components + guards + app config init.

**Prevention rule**: When splitting a feature migration, always include an explicit "wire entry point" task that:
1. Rewrites the router (`App.tsx`, `main.rs`, `mod.rs`) to import all new modules
2. Integrates global components (error boundaries, modals, alerts)
3. Depends on ALL feature tasks (runs last)

**Lesson**: Building components without wiring them is 90% done but 0% functional.

---

## i18n Index File is a Merge Conflict Hotspot (2026-02-16, kaitu-feature-migration)

**Problem**: `webapp/src/i18n/index.ts` had 3 merge conflicts across T6/T7/T10 merges. Each parallel feature task added its own namespace import (purchase, invite, account, feedback).

**Pattern**: Same structure as `main.rs` entry-point conflicts — every feature registers itself in the same file (namespace imports + resources object).

**Resolution**: Mechanical — combine all namespace imports and resource entries. Takes 30 seconds per conflict. But multiplicative: N parallel tasks = up to N-1 conflicts.

**Mitigation options**:
1. Accept conflicts (current approach) — fast to resolve, low risk
2. Foundation task pre-registers all namespaces with empty JSON files — eliminates conflicts but requires foreknowledge
3. Dynamic namespace loading (`i18next-http-backend`) — eliminates file entirely but adds runtime complexity

**Chosen**: Option 1 (accept conflicts). Mechanical resolution is reliable and fast.

---

## Interface Change + Consumer Update Must Be Atomic Without Worktrees (2026-02-18, platform-interface-cleanup)

**Observation**: Plan separated T1 (change IPlatform interface) and T2 (update 11 consumer files) as distinct tasks. But without worktrees, changing the interface immediately breaks all consumers — `tsc --noEmit` fails with 35 errors.

**Execution**: T1 and T2 were effectively merged into a single pass. Changed interface → fixed all consumers → verified with tsc. No intermediate commit possible between T1 and T2.

**Why this differs from worktree execution**: In worktree mode, T1 branch has the interface change and the T1 tests pass in isolation. T2 branch would be based on T1 and fix consumers. Without worktrees, both must happen atomically.

**Lesson**: When planning interface-breaking changes for non-worktree execution, merge the "change interface" and "update consumers" tasks into one. The dependency isn't "T2 depends on T1" — it's "T1 and T2 are inseparable without worktrees".

**Contrast**: T3 (Tauri bridge) and T4 (Capacitor bridge) remained independent — different files, no cross-dependency. Parallel execution worked for these.

---

## Cross-Repo Worktree for Submodule Changes (2026-02-16, unified-engine)

**Observation**: unified-engine modified files in `k2/` submodule (engine/ package, daemon/, mobile/) and k2app repo (iOS/Android native plugins, webapp TypeScript). The plan called for separate k2 worktrees (F1, T2, T3) and k2app worktrees (T4, T5, T6).

**Execution reality**: All tasks worked in single k2app branch with submodule changes committed inline. No cross-repo merge. Simpler than planned.

**Why planned complexity wasn't needed**: Tasks F1 (engine), T2 (daemon), T3 (mobile wrapper) were actually sequential (dependency chain), not parallel. Foundation F1 completed first; T2 and T3 both depended on it. So worktree parallelism didn't apply.

**Pattern**: When submodule changes are foundational and have sequential dependencies, working in a single branch is simpler than cross-repo worktree merging.

**When to use cross-repo worktrees**: Only when tasks in k2/ and k2app/ are truly parallel (no dependencies). Example: k2 daemon feature + k2app UI feature touching disjoint files.

---

## Port-From-Source Features: Read Target Files Before Planning (2026-02-20, desktop-window-management)

**Observation**: Plan listed 6 file changes to port window management from kaitu/client. But 3 of the 6 were already implemented in k2app (window.rs existed, main.rs had all handlers, tray.rs had show/hide/quit). Only 3 webapp-side changes (tauri-k2.ts, main.tsx, index.html) were actually needed.

**Why this happened**: The plan was written based on the source project's diff against an empty baseline. It didn't diff against k2app's current state.

**Prevention**: When porting features from another project, always:
1. Read source files to understand the complete feature
2. Read **target** files to understand what's already present
3. Diff the gap — only plan changes for missing pieces
4. Mark already-implemented steps as "SKIP" in the plan

**Cost of not checking**: Low in this case (discovered at execution time, 3 no-op steps). But if the plan had estimated time or assigned parallel agents, half the work allocation would be wasted.

---
