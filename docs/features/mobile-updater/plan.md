# Plan: Mobile Update System

## Meta

| Field | Value |
|-------|-------|
| Feature | mobile-updater |
| Spec | docs/features/mobile-updater/spec.md |
| Date | 2026-02-14 |
| Complexity | Complex (>15 files, 3 platforms, CI + native + webapp) |

## AC Mapping

| AC | Test / Verification | Task |
|----|---------------------|------|
| ci.yml runs K2 plugin type-check | CI green on push | T1 |
| build-mobile.yml triggers on v* tag | CI triggers on tag push | T2 |
| iOS exports IPA + uploads App Store Connect | CI job completes, TestFlight available | T3 |
| TestFlight available within minutes | Manual verify after tag push | T3 |
| App Store submitted with manual release | App Store Connect shows "Pending Developer Release" | T3 |
| Android APK uploaded to S3 | `curl android/latest.json` returns correct version | T4 |
| android/latest.json updated | S3 manifest has version, URL, hash, size | T4 |
| Web OTA bundle uploaded to S3 | `curl web/latest.json` returns correct version | T4 |
| web/latest.json updated | S3 manifest has version, URL, hash, size | T4 |
| ios/latest.json updated | S3 manifest has version and appstore_url | T4 |
| App checks native update on startup | `native-client.test.ts`: checkNativeUpdate | T5, T7, T8 |
| App checks web update on startup | `native-client.test.ts`: checkWebUpdate | T5, T7, T8 |
| Web OTA downloads + verifies + extracts | Swift/Kotlin unit test: OTA flow | T7, T8 |
| Web OTA takes effect on next launch | Manual verify on device | T7, T8 |
| Web OTA fallback on failure | Swift/Kotlin unit test: fallback | T7, T8 |
| Android APK silent download | Kotlin test: background download | T8 |
| Android in-app dialog when ready | `UpdatePrompt.test.tsx` | T9 |
| Android one-tap system installer | Manual verify on device | T8 |
| iOS dialog opens App Store | `UpdatePrompt.test.tsx` | T9 |
| Update UI shows version + size | `UpdatePrompt.test.tsx` | T9 |

## Phase 1: CI/CD Base

### T1: Add K2 Plugin type-check to CI

**Scope**: Add `npx tsc --noEmit` step for K2 plugin in `ci.yml`
**Files**:
- `.github/workflows/ci.yml`
**Depends on**: none
**TDD**:
- RED: K2 plugin TS errors not caught by CI (current state)
- GREEN: Add tsc --noEmit step after webapp type-check
- REFACTOR: N/A (single step addition)
**Acceptance**: CI runs K2 plugin type-check on push/PR; local `cd mobile/plugins/k2-plugin && npx tsc --noEmit` passes

### T2: Add v* tag trigger to build-mobile.yml

**Scope**: Enable automatic mobile builds on version tags, add concurrency + timeout
**Files**:
- `.github/workflows/build-mobile.yml`
**Depends on**: none
**TDD**:
- RED: `v*` tag push doesn't trigger mobile build (current state)
- GREEN: Add `push.tags: ['v*']` trigger, update `if` conditions for tag vs dispatch, add `concurrency` + `timeout-minutes: 60`
- REFACTOR: N/A
**Acceptance**: `v*` tag push triggers both iOS and Android jobs; manual dispatch still works with platform selection

---

## Phase 2: Release Pipeline

### T3: iOS IPA export + App Store Connect upload

**Scope**: Extend iOS build job to export IPA and upload to App Store Connect for TestFlight + App Store review
**Files**:
- `.github/workflows/build-mobile.yml` (iOS job steps)
- `scripts/build-mobile-ios.sh` (add `-exportArchive` step)
- `mobile/ios/ExportOptions.plist` (new)
**Depends on**: [T2]
**TDD**:
- RED: iOS job produces .xcarchive but no IPA, no TestFlight upload
- GREEN: Add ExportOptions.plist, add `xcodebuild -exportArchive` to build script, add `xcrun altool --upload-app` step in CI (or App Store Connect API via `altool`)
- REFACTOR: Consider using App Store Connect API (more modern) instead of altool if altool is deprecated
**Acceptance**: Tag push → iOS job uploads IPA → TestFlight internal testers can install; App Store review submitted with "manually release"
**Prerequisites**: CI secrets configured (APP_STORE_CONNECT_API_KEY_BASE64, KEY_ID, ISSUER_ID, IOS_DISTRIBUTION_PROFILE_BASE64)

### T4: S3 upload + manifest generation (Android + Web OTA + iOS)

**Scope**: Upload Android APK, Web OTA bundle, and iOS manifest to S3; generate all `latest.json` files
**Files**:
- `.github/workflows/build-mobile.yml` (new upload steps in both jobs)
- `scripts/ci/upload-mobile-s3.sh` (new — S3 upload + manifest generation)
**Depends on**: [T2]
**TDD**:
- RED: Build artifacts exist only as GitHub Actions artifacts, no S3 upload
- GREEN: Create `upload-mobile-s3.sh` script that:
  1. Zips `webapp/dist/` → `webapp.zip`
  2. Computes sha256 hash of all artifacts
  3. Uploads APK to `s3://d.all7.cc/kaitu/android/{version}/`
  4. Uploads webapp.zip to `s3://d.all7.cc/kaitu/web/{version}/`
  5. Generates and uploads `android/latest.json`, `web/latest.json`, `ios/latest.json`
  Add CI steps to call this script after build
- REFACTOR: Extract shared S3 upload logic if duplicated with desktop workflow
**Acceptance**: After tag push, all three `latest.json` files exist on S3 with correct version, URLs, hashes

---

## Phase 3: Client Updater

### T5: K2Plugin update interface definitions

**Scope**: Add update-related method definitions to K2Plugin TypeScript interface
**Files**:
- `mobile/plugins/k2-plugin/src/definitions.ts`
- `mobile/plugins/k2-plugin/src/index.ts`
- `mobile/plugins/k2-plugin/src/web.ts` (stub for web fallback)
**Depends on**: [T4] (needs manifest format finalized)
**TDD**:
- RED: Write type tests — `K2PluginInterface` missing `checkWebUpdate`, `checkNativeUpdate`, `applyWebUpdate`, `downloadNativeUpdate`, `installNativeUpdate`
- GREEN: Add interface methods:
  ```typescript
  checkWebUpdate(): Promise<{ available: boolean; version?: string; size?: number }>;
  checkNativeUpdate(): Promise<{ available: boolean; version?: string; size?: number; url?: string }>;
  applyWebUpdate(): Promise<void>;
  downloadNativeUpdate(): Promise<{ path: string }>;
  installNativeUpdate(options: { path: string }): Promise<void>;
  ```
  Add event: `updateDownloadProgress` with `{ percent: number }`
- REFACTOR: Ensure naming consistency with existing plugin methods
**Acceptance**: TypeScript definitions compile; web.ts stubs throw "not implemented on web"

### T6: NativeVpnClient update integration

**Scope**: Add update checking to NativeVpnClient, integrate with app startup flow
**Files**:
- `webapp/src/vpn-client/native-client.ts`
- `webapp/src/vpn-client/types.ts` (add update types if needed)
- `webapp/src/vpn-client/__tests__/native-client.test.ts`
**Depends on**: [T5]
**TDD**:
- RED: Write tests for `NativeVpnClient.checkForUpdates()` — calls checkNativeUpdate first, then checkWebUpdate if no native update
- GREEN: Implement priority-based update check in NativeVpnClient
- REFACTOR: Ensure MockVpnClient also supports update methods for testing
**Acceptance**: Tests pass; native update check priority is native > web
**Knowledge**: docs/knowledge/architecture-decisions.md (VpnClient Abstraction Pattern)

### T7: iOS update checker (K2Plugin.swift)

**Scope**: Implement update checking in iOS K2Plugin — Web OTA download/extract/swap + App Store redirect for native
**Files**:
- `mobile/plugins/k2-plugin/ios/Plugin/K2Plugin.swift`
**Depends on**: [T5]
**TDD**:
- RED: Call `checkWebUpdate()` → method not found
- GREEN: Implement:
  - `checkWebUpdate()`: fetch `web/latest.json`, compare version vs bundled version
  - `checkNativeUpdate()`: fetch `ios/latest.json`, compare version vs `Bundle.main.version`
  - `applyWebUpdate()`: download zip → verify sha256 → extract to Documents/web-update/ → swap with active
  - `installNativeUpdate()`: open App Store URL (`https://apps.apple.com/app/id6759199298`)
  - Backup mechanism: keep previous web version in Documents/web-backup/
  - On launch: check if Documents/web-update/ exists → use as webDir, else use bundled
- REFACTOR: Extract URL fetching + hash verification into shared utility
**Acceptance**: Web OTA downloads, verifies, extracts; next launch loads new assets; App Store redirect works
**Knowledge**: docs/knowledge/framework-gotchas.md (Capacitor webDir configuration)

### T8: Android update checker + APK installer (K2Plugin.kt)

**Scope**: Implement update checking in Android K2Plugin — Web OTA + APK silent download + system installer
**Files**:
- `mobile/plugins/k2-plugin/android/src/main/java/io/kaitu/k2plugin/K2Plugin.kt`
- `mobile/android/app/src/main/AndroidManifest.xml` (add REQUEST_INSTALL_PACKAGES)
- `mobile/android/app/src/main/res/xml/file_paths.xml` (new — FileProvider config)
**Depends on**: [T5]
**TDD**:
- RED: Call `checkWebUpdate()` → method not found
- GREEN: Implement:
  - `checkWebUpdate()`: fetch `web/latest.json`, compare version
  - `checkNativeUpdate()`: fetch `android/latest.json`, compare version vs BuildConfig.VERSION_NAME
  - `applyWebUpdate()`: download zip → verify sha256 → extract to app files/web-update/ → swap
  - `downloadNativeUpdate()`: background download APK to cache dir, emit progress events
  - `installNativeUpdate()`: FileProvider URI + ACTION_INSTALL_PACKAGE intent → system installer
  - Backup mechanism: keep previous web version in files/web-backup/
  - Add `REQUEST_INSTALL_PACKAGES` to AndroidManifest.xml
  - Add FileProvider config for sharing APK with installer
- REFACTOR: Extract download + hash logic shared between web OTA and APK download
**Acceptance**: Web OTA works; APK downloads silently; system installer opens with one tap; no browser redirect

### T9: Update Prompt UI (webapp)

**Scope**: In-app update dialog component for native/APK updates
**Files**:
- `webapp/src/components/UpdatePrompt.tsx` (new)
- `webapp/src/components/__tests__/UpdatePrompt.test.tsx` (new)
- `webapp/src/App.tsx` (mount UpdatePrompt)
**Depends on**: [T6]
**TDD**:
- RED: Write tests:
  - Shows nothing when no update available
  - Shows dialog with version + size when native update ready
  - Shows download progress during APK download
  - Calls installNativeUpdate on "Update" tap (Android)
  - Calls installNativeUpdate (opens App Store) on "Update" tap (iOS)
  - Dismiss option available
- GREEN: Implement UpdatePrompt component:
  - Uses `useEffect` on mount to call `checkForUpdates()`
  - Renders modal dialog with update info
  - Download progress bar for Android APK
  - Platform-aware button text ("Update" / "Go to App Store")
- REFACTOR: i18n for update prompt strings (zh-CN, en-US)
**Acceptance**: All tests pass; update prompt appears when update available; correct behavior per platform

---

## Execution Notes

### Phase Dependencies

```
T1 ──────────────────────────────────────→ (independent, can ship anytime)
T2 ──────────────┬──→ T3 (iOS release)
                 └──→ T4 (S3 upload)
                           └──→ T5 (TS defs) ──┬──→ T6 (NativeVpnClient) ──→ T9 (UI)
                                                ├──→ T7 (iOS native)
                                                └──→ T8 (Android native)
```

### Parallel Opportunities

- T1 and T2 are fully independent — can execute in parallel
- T3 and T4 are independent (iOS upload vs S3 upload) — can execute in parallel
- T7 and T8 are independent (iOS vs Android native) — can execute in parallel
- T9 depends on T6 (needs NativeVpnClient update interface)

### Prerequisites Before Phase 2

- [ ] Configure CI secrets: APP_STORE_CONNECT_API_KEY_BASE64, KEY_ID, ISSUER_ID
- [ ] Create iOS distribution provisioning profile and add as CI secret
- [ ] Verify AWS credentials in CI have write access to `s3://d.all7.cc/kaitu/`

### Prerequisites Before Phase 3

- [ ] Phase 2 complete — manifests on S3 to test against
- [ ] Test devices available (iOS physical device for TestFlight, Android for APK install)

### Web OTA Integration with Capacitor

The Capacitor `webDir` config normally points to `../webapp/dist/`. For OTA:
- On fresh install: Capacitor loads from bundled assets (app bundle)
- After OTA: native code checks for `Documents/web-update/` at launch
- If `web-update/` exists and is valid: set webview URL to that directory
- If `web-update/` is corrupt: delete it, fall back to bundled assets
- This requires configuring Capacitor's server path at runtime in native code (both iOS and Android support this via `ServerPath` or custom `WebViewClient`)
