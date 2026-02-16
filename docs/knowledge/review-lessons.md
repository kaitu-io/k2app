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
