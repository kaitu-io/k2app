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
