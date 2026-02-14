# Plan: T4 Completion — Build System + CI/CD

## Meta

| Field | Value |
|-------|-------|
| Parent | docs/features/mobile-rewrite/plan.md (T4) |
| Date | 2026-02-14 |
| Complexity | Simple (<5 files, no refactoring) |
| Prerequisites | T0-T3 merged, fixes 1-4 applied |

## Current State

T4 has 3 created files + Makefile targets, but needs alignment fixes before commit.

| File | Status | Notes |
|------|--------|-------|
| `scripts/build-mobile-ios.sh` | Created ✅ | Full pipeline: gomobile → cap sync → pod install → xcodebuild → codesign |
| `scripts/build-mobile-android.sh` | Created ✅ | Full pipeline: gomobile → cap sync → gradlew → APK collect |
| `.github/workflows/build-mobile.yml` | Created ✅ | iOS + Android CI, manual dispatch, artifact upload |
| `Makefile` mobile targets | Created ✅ | Has redundancy issue (see below) |
| `scripts/build-macos.sh` | Created ✅ | Untracked — must be committed alongside mobile scripts |

## Issues to Fix

### Issue 1: Makefile/Script Redundancy

**Problem**: `build-mobile-ios` and `build-mobile-android` Makefile targets have inline commands that duplicate what the scripts do. The scripts already call `make pre-build`, `make build-webapp`, and `make mobile-ios/android` internally. Having these as Makefile prerequisites causes double execution.

**Current** (wrong):
```makefile
build-mobile-ios: pre-build build-webapp mobile-ios
	cp -r k2/build/K2Mobile.xcframework mobile/ios/App/
	cd mobile && npx cap sync ios
	cd mobile/ios/App && xcodebuild ...
```

**Fix**: Delegate to scripts (matching `build-macos` pattern):
```makefile
build-mobile-ios:
	bash scripts/build-mobile-ios.sh

build-mobile-android:
	bash scripts/build-mobile-android.sh
```

**Rationale**: `build-macos` already uses this pattern. Scripts are the full pipeline (pre-build, webapp, gomobile, cap sync, native build, verification). Makefile targets are thin wrappers. `mobile-ios` and `mobile-android` remain as standalone gomobile-only targets.

### Issue 2: Missing Mobile Clean Targets

**Problem**: `clean:` target doesn't remove mobile build artifacts.

**Fix**: Add mobile paths to clean target:
```makefile
clean:
	rm -rf webapp/dist desktop/src-tauri/target desktop/src-tauri/binaries/k2-* \
		mobile/ios/App/build mobile/android/app/build release/
```

### Issue 3: `build-macos.sh` Untracked

**Problem**: `scripts/build-macos.sh` was created for the `build-macos` Makefile target but is untracked in git.

**Fix**: Include in the T4 commit.

## Tasks

### Step 1: Fix Makefile alignment
- Replace `build-mobile-ios` and `build-mobile-android` inline commands with script delegation
- Keep `mobile-ios` and `mobile-android` as standalone gomobile-only targets (useful for dev)
- Add mobile paths to `clean:` target

**Files**: `Makefile`

### Step 2: Commit all uncommitted work

Single commit covering:
- **Fixes 1-4**: .gitignore, remapStatusKeys (Swift/Kotlin), Info.plist, definitions.ts
- **AGENT.md**: conventions
- **T4 files**: build scripts (iOS, Android, macOS), CI workflow, Makefile changes
- **Knowledge + Baseline**: 5 knowledge files, baseline update

**Commit message**: `feat(mobile): T4 build system + CI/CD + post-review fixes`

## AC Coverage

| AC | Covered By | Status |
|----|-----------|--------|
| `make build-mobile-ios` → xcarchive | `scripts/build-mobile-ios.sh` → xcodebuild archive | ✅ Script exists |
| `make build-mobile-android` → signed APK | `scripts/build-mobile-android.sh` → gradlew assembleRelease | ✅ Script exists |
| CI workflow succeeds | `.github/workflows/build-mobile.yml` | ✅ Workflow exists (verify on push) |
| Codesign valid | `build-mobile-ios.sh` line 76: `codesign --verify --deep --strict` | ✅ In script |

## Execution Estimate

2 steps, no worktree needed (direct on main). ~5 minutes.
