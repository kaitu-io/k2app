# Review Lessons

Insights from code reviews and merge processes.

---

## Code Review Catches Silent Data Bugs (2026-02-14, mobile-rewrite)

**Lesson**: The Go→JS JSON key mismatch (`connected_at` vs `connectedAt`) was caught by code review, not by tests or runtime errors. The value existed under the wrong key — `status.connectedAt === undefined` silently.

**Takeaway**: When bridging between languages with different naming conventions, code review should explicitly compare output keys against consumer type definitions. Type-safe languages don't help when the JSON wire format bypasses the type system.

**Cross-reference**: See Bugfix Patterns → "Go→JS JSON Key Mismatch Causes Silent Data Loss"

---

## git check-ignore as Review Tool (2026-02-14, mobile-rewrite)

**Lesson**: `.gitignore` overbroad patterns were caught by running `git check-ignore <path>` during code review. Without this check, source files were silently invisible to git.

**Takeaway**: When reviewing changes that add `.gitignore` patterns, verify with `git check-ignore` that source files aren't accidentally excluded. Silent git invisibility has no error message.

**Cross-reference**: See Bugfix Patterns → "Overbroad .gitignore Silently Hides Source Files"

---

## Delivery Gate: tsc + vitest After Merge (2026-02-14, k2app-rewrite)

**Lesson**: Parallel worktree branches that compile independently can break after merge — unused imports, duplicate declarations, incompatible type changes. Running `tsc --noEmit && yarn test` as delivery gate caught all post-merge issues.

**Concrete catches**: Unused `vi` import (TS6133), missing peer dependency (`@testing-library/dom`).

**Takeaway**: Never skip the delivery gate, even when all individual branches pass their tests.

**Cross-reference**: See Testing Strategies → "Delivery Gate: tsc + vitest After Every Merge"

---

## Swift/Kotlin Parity Review (2026-02-14, mobile-rewrite)

**Lesson**: K2Plugin.swift and K2Plugin.kt must have identical `remapStatusKeys()` key maps. Code review verified both files side-by-side. A missing key in one platform would cause silent data loss only on that platform — extremely hard to debug.

**Takeaway**: When native bridge code exists on multiple platforms, review them in parallel. Same function, same key map, same state mappings. Any divergence is a bug.

---

## Info.plist Completeness Check (2026-02-14, mobile-rewrite)

**Lesson**: iOS main app target was missing Info.plist (only the extension had one). `Bundle.main.infoDictionary` returned nil, causing version to show as "unknown".

**Takeaway**: When reviewing iOS PRs, verify that every target (main app + extensions) has its own Info.plist with required keys.

---

## Real Device Install Catches Issues Simulator Misses (2026-02-16, android-aar-fix)

**Lesson**: `xcodebuild` succeeded but `devicectl device install` failed twice — first for missing `CFBundleExecutable`, then for missing `CFBundleVersion` in extension Info.plist. Xcode builds don't validate these keys; only real device install does.

**Takeaway**: Always test on real device (not just build). `xcodebuild build` validates compilation and code signing but NOT installability. Missing plist keys are a device-side validation. `BUILD SUCCEEDED` does not mean `INSTALL SUCCEEDED`.

---

## Scrum Debate Reveals Simpler Architecture (2026-02-16, android-aar-fix)

**Lesson**: Adversarial debate (Plan A: keep wrapper vs Plan B: flatDir everywhere) produced Plan C (remove wrapper entirely) because a challenger identified that k2-plugin doesn't actually need AAR access — VpnServiceBridge already decouples it. This reduced the problem from "multi-module AAR sharing" to "single module AAR reference".

**Takeaway**: When facing integration complexity, question whether all consumers actually need the dependency. Decoupling interfaces may already exist.

---

## Architecture Review of Two-Process iOS VPN Finds 10 Issues (2026-02-16, ios-vpn-fixes)

**Lesson**: Reviewing PacketTunnelProvider.swift (98 lines) and K2Plugin.swift (418 lines) from the perspective of "iOS PacketTunnel architecture engineer" uncovered 10 issues across P0/P1/P2 severity — despite both files compiling and having no obvious bugs. The issues were all architectural: missing error propagation paths, incorrect lifecycle ordering, missing network settings, orphaned state writes.

**Key pattern**: Two-process architecture (NE + App) multiplies failure modes. Every communication path between processes must be explicitly designed. Silent failures are the default — no crash, no error, just missing data.

**Issues found and grouped**:
- P0 (3): Error events lost, engine failure unreported to system, orphaned state writes
- P1 (4): No old engine cleanup, fd before settings, missing IPv6, race in getStatus
- P2 (3): loadVPNManager race, hardcoded constants, simulator entitlements

**Scrum debate produced clear action items**: 8 of 10 issues fixed in single session. 2 deferred (KVC long-term replacement, simulator entitlements — not needed).

**Takeaway**: For multi-process architectures, review every IPC boundary as a potential data loss point. "It compiles and runs" is insufficient — error paths and lifecycle ordering are where bugs hide.

---

## CLAUDE.md Staleness After Major Features (2026-02-16, kaitu-feature-migration)

**Lesson**: After merging 11 feature branches (150 new tests, 16 pages, 8 stores), `webapp/CLAUDE.md` was grade D (45/100). It listed 4 pages (actual: 19), 3 stores (actual: 8), 169 tests (actual: 279), wrong BottomNav tabs. AI agents reading this file would make wrong assumptions about the codebase.

**Root cause**: No task in the execution plan was scoped to update documentation. The `w9f-baseline` agent updated root `CLAUDE.md` (test count, structure tree) but didn't rewrite `webapp/CLAUDE.md`.

**Concrete damage**: Phantom `nav` i18n namespace listed in root CLAUDE.md (referenced in spec but never created). BottomNav described as "Dashboard, Servers, Settings" — actual was "Dashboard, Purchase, Invite, Account". App.tsx described as "3 routes + AuthGuard" — no longer true.

**Prevention**: After major feature execution, run CLAUDE.md audit (`/claude-md-improver`). Alternatively, add a "docs update" step to w9f-baseline that rewrites all CLAUDE.md files, not just root.

**Takeaway**: Stale documentation is worse than no documentation — it actively misleads AI agents into wrong decisions.

---

## TypeScript Delivery Gate Catches Compound Merge Issues (2026-02-16, kaitu-feature-migration)

**Lesson**: After merging 11 parallel branches, `npx tsc --noEmit` caught 24 errors across 12 files. Each branch compiled independently. Errors were all cross-branch — unused imports from removed code, missing interface fields expected by new consumers, type narrowing failures from combined patterns.

**Specific patterns**:
- `AppConfig` missing `announcement`/`minClientVersion` fields (added by global component tests, consumed by components in different branch)
- `VpnStore` missing `daemonReachable` (added by ServiceAlert, consumed by test in different branch)
- Unused imports from refactored code (Purchase.tsx had 6 unused imports after merge)
- Test file unused variables (different branches added/removed test utilities)

**Takeaway**: The delivery gate (`tsc --noEmit` + `vitest run`) is not optional after parallel merges. Budget 15-30 minutes for TypeScript cleanup proportional to branch count. For 11 branches: 24 errors, 12 files, ~20 minutes to fix.

---
