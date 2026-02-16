# Plan: Android AAR Integration Fix (方案 C)

## Meta

| Field | Value |
|-------|-------|
| Feature | android-aar-fix |
| Spec | docs/features/mobile-rewrite.md (Decision 4: Android) |
| Date | 2026-02-16 |
| Complexity | simple |
| Origin | Scrum verdict — 方案 C (derived from Mobile Lead challenge) |

## Context

### Problem
`k2-mobile` wrapper module uses `api files('libs/k2mobile.aar')` which treats
AAR as a jar — JNI `.so` files won't be bundled into APK → runtime
`UnsatisfiedLinkError`. The wrapper exists to share AAR across modules, but
K2Plugin (in k2-plugin module) already doesn't import `mobile.*` — only
`K2VpnService` (in app module) needs the AAR.

### Current State
- K2Plugin: already decoupled via `VpnServiceBridge` (commit 1b821f5) — zero `mobile.*` imports
- K2VpnService: only consumer of `mobile.Mobile`, `mobile.Engine`, `mobile.EventHandler`
- `k2-mobile` module: wrapper with broken `files()` AAR reference
- `app/build.gradle`: has uncommitted partial fix (flatDir + direct AAR, but still includes `k2-mobile` in settings.gradle)

### Decision (from Scrum)
Delete `k2-mobile` wrapper module. `app` references AAR directly via flatDir.
Single consumer = no wrapper needed.

## AC Mapping

| AC | Verification | Task |
|----|-------------|------|
| AC1: app module compiles with gomobile AAR | `./gradlew :app:compileDebugKotlin` succeeds | T1 |
| AC2: k2-plugin compiles without AAR dep | `./gradlew :k2-plugin:compileDebugKotlin` succeeds | T1 |
| AC3: APK contains libgojni.so | `unzip -l app.apk \| grep libgojni` shows arm64/armv7 entries | T1 |
| AC4: No k2-mobile module references remain | `grep -r 'k2-mobile' mobile/android/` returns nothing | T1 |

## Feature Tasks

### T1: Remove k2-mobile module, fix app AAR reference

**Scope**: Delete k2-mobile wrapper module. Configure app/build.gradle to
correctly consume k2mobile.aar via flatDir. Verify JNI .so bundling.

**Files**:
- DELETE `mobile/android/k2-mobile/` (entire directory)
- MODIFY `mobile/android/settings.gradle` (remove `include ':k2-mobile'`)
- MODIFY `mobile/android/app/build.gradle` (flatDir + AAR reference)
- MODIFY `scripts/build-mobile-android.sh` (update AAR copy destination if needed)

**Depends on**: none

**TDD**:
- RED: No unit tests needed — this is a build config change. Verification is
  build success + APK inspection.
  - Verify: `cd mobile/android && ./gradlew :app:compileDebugKotlin` currently
    fails or produces broken output with `files()` AAR
- GREEN:
  1. Delete `mobile/android/k2-mobile/` directory
  2. Remove `include ':k2-mobile'` from `settings.gradle`
  3. Update `app/build.gradle`:
     - Remove `implementation project(':k2-mobile')` (already done in uncommitted)
     - Keep `flatDir { dirs '../k2-mobile/libs' }` → change to `'libs'`
     - Keep `implementation(name: 'k2mobile', ext: 'aar')`
  4. AAR location decision: copy to `app/libs/k2mobile.aar`
     (update build scripts accordingly)
  5. Verify: `./gradlew :app:compileDebugKotlin` passes
  6. Verify: `./gradlew assembleDebug` produces APK with `libgojni.so`
- REFACTOR:
  - [SHOULD] Update `docs/features/mobile-rewrite.md` project structure section
    to reflect k2-mobile removal

**Acceptance**:
- `./gradlew :app:compileDebugKotlin` succeeds
- `./gradlew :k2-plugin:compileDebugKotlin` succeeds (no AAR dep)
- `grep -r 'k2-mobile' mobile/android/` returns empty (except build outputs)
- APK contains `lib/arm64-v8a/libgojni.so` (verifiable when AAR is present)

**Notes**:
- AAR file (`k2mobile.aar`) is a build artifact from `gomobile bind`, not checked
  into git. The `app/libs/` directory is gitignored. Build scripts copy it there.
- If flatDir doesn't correctly bundle JNI .so from AAR, fallback to local Maven
  publish: `./gradlew publishToMavenLocal` from a temp module. But flatDir should
  work for AAR consumption — AGP extracts .so from AAR during merge.
