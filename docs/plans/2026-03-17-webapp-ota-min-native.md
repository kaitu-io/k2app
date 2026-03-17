# Webapp OTA min_native + Boot Verification

> **For agentic workers:** REQUIRED: Use superpowers:subagent-driven-development (if subagents available) or superpowers:executing-plans to implement this plan. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Prevent webapp OTA from breaking the app when the native version is too old, and auto-rollback OTA webapps that crash at runtime.

**Architecture:** Two layers: (1) `min_native` field in web manifest checked before downloading OTA — prevents incompatible webapp from being applied. (2) Boot verification via `.boot-pending` marker file — if webapp fails to call `checkReady()` after OTA, next cold start rolls back to backup/bundled.

**Tech Stack:** Kotlin (Android K2Plugin), Swift (iOS K2Plugin), Bash (publish-mobile.sh), TypeScript (K2Plugin definitions)

---

## Design

### Layer 1: min_native compatibility check

```
Web manifest (latest.json):
{
  "version": "0.5.0",
  "url": "0.5.0/webapp.zip",
  "hash": "sha256:...",
  "min_native": "0.4.0",    ← NEW: minimum native app version required
  ...
}

Client flow:
  fetch manifest → read min_native → compare with app version
  → if app version < min_native → skip OTA (log warning, don't download)
  → if app version >= min_native → proceed with download
```

**Source of truth:** `webapp/package.json` field `"minNativeVersion": "0.4.0"`. Read by `publish-mobile.sh` at publish time.

### Layer 2: Boot verification (crash recovery)

```
OTA apply:
  extract zip → write version.txt → create .boot-pending marker

Cold start (load()):
  web-update/ exists?
    .boot-pending exists?
      YES → OTA crashed last time → DELETE web-update → fallback to bundled
      NO  → create .boot-pending → setServerBasePath(web-update/)

Webapp calls checkReady() (existing, already called on every launch):
  .boot-pending exists?
    YES → remove it (webapp loaded successfully, OTA verified)
    NO  → nothing (normal launch)
```

**Edge cases:**
- User force-kills before checkReady(): `.boot-pending` stays → next boot rolls back. Safe — falls back to bundled, next OTA re-applies.
- Native app upgrade with stale OTA dir: `.boot-pending` may exist → rollback → uses new bundled webapp. Correct.
- No OTA (fresh install / bundled only): No `web-update/` dir → no markers → clean path.

---

## Files

| File | Action | What changes |
|------|--------|-------------|
| `webapp/package.json` | Add field | `"minNativeVersion": "0.4.0"` |
| `scripts/publish-mobile.sh` | Modify | Read minNativeVersion, include as `min_native` in web manifest |
| `mobile/plugins/k2-plugin/android/.../K2Plugin.kt` | Modify | min_native check in checkWebUpdate/autoUpdate + boot verification in load/checkReady/applyWebUpdate |
| `mobile/plugins/k2-plugin/android/.../K2PluginUtils.kt` | Add method | `isCompatibleNativeVersion(minNative, appVersion)` |
| `mobile/plugins/k2-plugin/android/src/test/.../K2PluginUtilsTest.kt` | Add tests | Compatibility check tests |
| `mobile/plugins/k2-plugin/ios/Plugin/K2Plugin.swift` | Modify | Same as Android: min_native check + boot verification |

---

## Task 1: Add minNativeVersion to webapp/package.json + publish-mobile.sh

**Files:**
- Modify: `webapp/package.json` — add `minNativeVersion` field
- Modify: `scripts/publish-mobile.sh` — read and include in manifest

- [ ] **Step 1: Add minNativeVersion to webapp/package.json**

Add after the `"version"` field (line 4):
```json
  "minNativeVersion": "0.4.0",
```

This means: any webapp built from this codebase requires at least native app 0.4.0 to work.

- [ ] **Step 2: Update publish-mobile.sh to read minNativeVersion**

In `scripts/publish-mobile.sh`, after `APPSTORE_URL=` declaration, add:

```bash
# Read min native version from webapp package.json (optional field)
MIN_NATIVE=$(node -p "require('./webapp/package.json').minNativeVersion || ''" 2>/dev/null || echo "")
```

Then update the `generate_manifest` function's heredoc. Change:

```bash
    cat > "$manifest" <<MANIFEST_EOF
{
  "version": "${VERSION}",
  "url": "${rel_url}",
  "hash": "${hash}",
  "size": ${size},
  "released_at": "${RELEASED_AT}"${extra_fields}
}
MANIFEST_EOF
```

to:

```bash
    # Build optional fields
    local min_native_field=""
    if [ -n "$MIN_NATIVE" ]; then
        min_native_field=",
  \"min_native\": \"${MIN_NATIVE}\""
    fi

    cat > "$manifest" <<MANIFEST_EOF
{
  "version": "${VERSION}",
  "url": "${rel_url}",
  "hash": "${hash}",
  "size": ${size},
  "released_at": "${RELEASED_AT}"${min_native_field}${extra_fields}
}
MANIFEST_EOF
```

- [ ] **Step 3: Test with local mock**

```bash
MOCK_DIR=$(mktemp -d)
mkdir -p "$MOCK_DIR/android/0.4.0-beta.6" "$MOCK_DIR/web/0.4.0-beta.6"
echo "fake" > "$MOCK_DIR/android/0.4.0-beta.6/Kaitu-0.4.0-beta.6.apk"
echo "fake" > "$MOCK_DIR/web/0.4.0-beta.6/webapp.zip"
scripts/publish-mobile.sh 0.4.0-beta.6 --s3-base="$MOCK_DIR"
echo "--- web manifest ---"
cat "$MOCK_DIR/web/beta/latest.json"
# Expected: has "min_native": "0.4.0"
rm -rf "$MOCK_DIR"
```

- [ ] **Step 4: Commit**

```bash
git add webapp/package.json scripts/publish-mobile.sh
git commit -m "feat: add min_native to web OTA manifest

webapp/package.json defines minNativeVersion (minimum native app
version required). publish-mobile.sh reads it and includes as
min_native field in web/latest.json manifest. Clients will check
this before applying OTA to prevent incompatible webapp updates."
```

---

## Task 2: Android K2Plugin — min_native check + boot verification

**Files:**
- Modify: `mobile/plugins/k2-plugin/android/src/main/java/io/kaitu/k2plugin/K2PluginUtils.kt`
- Modify: `mobile/plugins/k2-plugin/android/src/main/java/io/kaitu/k2plugin/K2Plugin.kt`
- Modify: `mobile/plugins/k2-plugin/android/src/test/java/io/kaitu/k2plugin/K2PluginUtilsTest.kt`

- [ ] **Step 1: Add isCompatibleNativeVersion to K2PluginUtils.kt**

Add after the `isNewerVersion` function:

```kotlin
    /**
     * Check if the current native app version meets the minimum required by a webapp.
     * Returns true if appVersion >= minNative (using semantic version comparison).
     * Returns true if minNative is null or empty (backwards compat — old manifests without field).
     */
    fun isCompatibleNativeVersion(minNative: String?, appVersion: String): Boolean {
        if (minNative.isNullOrBlank()) return true
        // appVersion >= minNative means minNative is NOT newer than appVersion
        return !isNewerVersion(minNative, appVersion)
    }
```

- [ ] **Step 2: Add unit tests**

Add to `K2PluginUtilsTest.kt`:

```kotlin
    // --- isCompatibleNativeVersion ---

    @Test
    fun compatibleNativeVersion_nullMinNative() {
        assertTrue(K2PluginUtils.isCompatibleNativeVersion(null, "0.4.0"))
    }

    @Test
    fun compatibleNativeVersion_emptyMinNative() {
        assertTrue(K2PluginUtils.isCompatibleNativeVersion("", "0.4.0"))
    }

    @Test
    fun compatibleNativeVersion_exact() {
        assertTrue(K2PluginUtils.isCompatibleNativeVersion("0.4.0", "0.4.0"))
    }

    @Test
    fun compatibleNativeVersion_newer() {
        assertTrue(K2PluginUtils.isCompatibleNativeVersion("0.4.0", "0.5.0"))
    }

    @Test
    fun compatibleNativeVersion_older() {
        assertFalse(K2PluginUtils.isCompatibleNativeVersion("0.5.0", "0.4.0"))
    }

    @Test
    fun compatibleNativeVersion_betaApp() {
        assertTrue(K2PluginUtils.isCompatibleNativeVersion("0.4.0", "0.4.0-beta.6"))
        assertFalse(K2PluginUtils.isCompatibleNativeVersion("0.5.0", "0.4.0-beta.6"))
    }
```

- [ ] **Step 3: Run tests**

```bash
cd mobile/plugins/k2-plugin && ./gradlew test 2>&1 | tail -10
```
Expected: all pass including new compatibility tests.

- [ ] **Step 4: Add min_native check to K2Plugin.kt**

In `checkWebUpdate()` (around line 300, after extracting manifest fields), add:

```kotlin
                val minNative = manifest.optString("min_native", "")
                val appVersion = context.packageManager
                    .getPackageInfo(context.packageName, 0).versionName ?: "0.0.0"

                if (!K2PluginUtils.isCompatibleNativeVersion(minNative, appVersion)) {
                    Log.w(TAG, "Web OTA skipped: min_native=$minNative > app=$appVersion")
                    val ret = JSObject()
                    ret.put("available", false)
                    ret.put("reason", "native_too_old")
                    call.resolve(ret)
                    return@Thread
                }
```

In `performAutoUpdateCheck()` web OTA section (around line 840, after extracting manifest), add the same check:

```kotlin
                val minNative = manifest.optString("min_native", "")
                val appVersionForCompat = context.packageManager
                    .getPackageInfo(context.packageName, 0).versionName ?: "0.0.0"
                if (!K2PluginUtils.isCompatibleNativeVersion(minNative, appVersionForCompat)) {
                    Log.w(TAG, "Auto web OTA skipped: min_native=$minNative > app=$appVersionForCompat")
                    return
                }
```

- [ ] **Step 5: Add boot verification to K2Plugin.kt**

In `load()`, replace the web-update detection block (lines ~52-64) with:

```kotlin
        // Check for OTA web update with boot verification
        val webUpdateDir = File(context.filesDir, "web-update")
        val bootPending = File(webUpdateDir, ".boot-pending")
        val indexFile = File(webUpdateDir, "index.html")

        if (webUpdateDir.exists() && bootPending.exists()) {
            // OTA webapp failed to call checkReady() last time — rollback
            Log.w(TAG, "load: OTA boot verification failed — rolling back to bundled webapp")
            val webBackupDir = File(context.filesDir, "web-backup")
            webUpdateDir.deleteRecursively()
            webBackupDir.deleteRecursively()
            // Fall through to use bundled webapp
        } else if (webUpdateDir.exists() && indexFile.exists()) {
            Log.d(TAG, "load: OTA web update found, setting server base path")
            bootPending.createNewFile()  // Mark pending — removed by checkReady()
            bridge.setServerBasePath(webUpdateDir.absolutePath)
        } else if (webUpdateDir.exists()) {
            Log.w(TAG, "load: corrupt OTA web dir (no index.html) — removing")
            webUpdateDir.deleteRecursively()
        }
```

In `checkReady()` (around line 87), add boot-pending removal:

```kotlin
    @PluginMethod
    fun checkReady(call: PluginCall) {
        // Clear OTA boot-pending marker (webapp loaded successfully)
        val bootPending = File(context.filesDir, "web-update/.boot-pending")
        if (bootPending.exists()) {
            bootPending.delete()
            Log.d(TAG, "checkReady: OTA boot verified — cleared .boot-pending")
        }

        val version = context.packageManager.getPackageInfo(context.packageName, 0).versionName ?: "unknown"
        val ret = JSObject()
        ret.put("ready", true)
        ret.put("version", version)
        call.resolve(ret)
    }
```

In `applyWebUpdate()` (around line 427, after writing version.txt), add marker creation:

```kotlin
            // Mark boot-pending for verification on next cold start
            File(webUpdateDir, ".boot-pending").createNewFile()
```

Same in `performAutoUpdateCheck()` web OTA section, after writing version.txt:

```kotlin
                File(webUpdateDir, ".boot-pending").createNewFile()
```

- [ ] **Step 6: Verify build**

```bash
cd mobile/plugins/k2-plugin && ./gradlew build 2>&1 | tail -5
```

- [ ] **Step 7: Commit**

```bash
git add mobile/plugins/k2-plugin/android/
git commit -m "feat(android): add min_native check + boot verification for web OTA

- min_native: skip OTA if app version < manifest min_native field
- Boot verification: .boot-pending marker created on OTA apply,
  cleared by checkReady(). If still present on next cold start,
  OTA webapp crashed → rollback to bundled webapp.
- New K2PluginUtils.isCompatibleNativeVersion() with 6 unit tests."
```

---

## Task 3: iOS K2Plugin — min_native check + boot verification

**Files:**
- Modify: `mobile/plugins/k2-plugin/ios/Plugin/K2Plugin.swift`

The logic is identical to Android. Changes are in: `load()`, `checkReady()`, `checkWebUpdate()`, `performAutoUpdateCheck()`, `applyWebUpdateInternal()`.

- [ ] **Step 1: Add min_native check to checkWebUpdate()**

In `checkWebUpdate()`, after extracting manifest fields, add:

```swift
        let minNative = json["min_native"] as? String ?? ""
        if !minNative.isEmpty && isNewerVersion(minNative, than: appVersion) {
            logger.info("Web OTA skipped: min_native=\(minNative) > app=\(appVersion)")
            await MainActor.run { call.resolve(["available": false, "reason": "native_too_old"]) }
            return
        }
```

Same check in `performAutoUpdateCheck()` web OTA section.

- [ ] **Step 2: Add boot verification to load()**

Replace the web-update detection block in `load()` with:

```swift
        let documentsPath = FileManager.default.urls(for: .documentDirectory, in: .userDomainMask).first!
        let webUpdatePath = documentsPath.appendingPathComponent("web-update")
        let bootPending = webUpdatePath.appendingPathComponent(".boot-pending")
        let indexPath = webUpdatePath.appendingPathComponent("index.html")

        if FileManager.default.fileExists(atPath: webUpdatePath.path) && FileManager.default.fileExists(atPath: bootPending.path) {
            // OTA webapp failed to call checkReady() last time — rollback
            logger.warning("load: OTA boot verification failed — rolling back to bundled webapp")
            let webBackupPath = documentsPath.appendingPathComponent("web-backup")
            try? FileManager.default.removeItem(at: webUpdatePath)
            try? FileManager.default.removeItem(at: webBackupPath)
        } else if FileManager.default.fileExists(atPath: webUpdatePath.path) {
            if FileManager.default.fileExists(atPath: indexPath.path) {
                FileManager.default.createFile(atPath: bootPending.path, contents: nil)
                bridge?.setServerBasePath(webUpdatePath.path)
            } else {
                try? FileManager.default.removeItem(at: webUpdatePath)
                logger.info("Removed corrupt OTA dir")
            }
        }
```

- [ ] **Step 3: Add boot-pending removal to checkReady()**

```swift
    @objc func checkReady(_ call: CAPPluginCall) {
        // Clear OTA boot-pending marker (webapp loaded successfully)
        let documentsPath = FileManager.default.urls(for: .documentDirectory, in: .userDomainMask).first!
        let bootPending = documentsPath.appendingPathComponent("web-update/.boot-pending")
        if FileManager.default.fileExists(atPath: bootPending.path) {
            try? FileManager.default.removeItem(at: bootPending)
            logger.info("checkReady: OTA boot verified — cleared .boot-pending")
        }

        call.resolve(["ready": true, "version": appVersion])
    }
```

- [ ] **Step 4: Add .boot-pending creation in applyWebUpdateInternal()**

After writing version.txt, add:

```swift
        // Mark boot-pending for verification on next cold start
        FileManager.default.createFile(
            atPath: webUpdatePath.appendingPathComponent(".boot-pending").path,
            contents: nil)
```

Same in `performAutoUpdateCheck()` after writing version.txt.

- [ ] **Step 5: Verify Xcode build**

```bash
cd mobile/ios/App && xcodebuild -scheme App -sdk iphonesimulator -destination 'platform=iOS Simulator,name=iPhone 16' build 2>&1 | tail -10
```
Or simpler: open Xcode, build for simulator.

- [ ] **Step 6: Commit**

```bash
git add mobile/plugins/k2-plugin/ios/
git commit -m "feat(ios): add min_native check + boot verification for web OTA

Same as Android: min_native compatibility check prevents applying
incompatible webapp. .boot-pending marker enables automatic rollback
if OTA webapp fails to load (cleared by checkReady on successful mount)."
```

---

## Task 4: Rebuild K2Plugin dist/ + documentation

K2Plugin `dist/` must be committed — webapp `tsc` depends on `dist/definitions.d.ts`.

**Files:**
- Rebuild: `mobile/plugins/k2-plugin/dist/`
- Modify: `mobile/CLAUDE.md`

- [ ] **Step 1: Rebuild plugin dist**

```bash
cd mobile/plugins/k2-plugin && npm run build
```

Note: definitions.ts didn't change (no new methods added — `checkReady()` already exists). But rebuild to ensure dist/ is in sync.

- [ ] **Step 2: Update mobile/CLAUDE.md**

Add to Gotchas section:

```
- **Web OTA min_native**: Manifest `min_native` field prevents applying webapp that requires a newer native app. Source: `webapp/package.json` → `minNativeVersion`. Bump this when webapp adds new native bridge dependencies.
- **Web OTA boot verification**: `.boot-pending` marker in `web-update/` dir. Created on OTA apply, cleared by `checkReady()`. If present on cold start → OTA crashed → rollback to bundled webapp.
```

- [ ] **Step 3: Commit**

```bash
git add mobile/plugins/k2-plugin/dist/ mobile/CLAUDE.md
git commit -m "docs: update mobile CLAUDE.md with OTA min_native and boot verification"
```

---

## Verification

### Local mock test (publish-mobile.sh)

```bash
MOCK_DIR=$(mktemp -d)
mkdir -p "$MOCK_DIR/android/0.4.0-beta.6" "$MOCK_DIR/web/0.4.0-beta.6"
echo "fake" > "$MOCK_DIR/android/0.4.0-beta.6/Kaitu-0.4.0-beta.6.apk"
echo "fake" > "$MOCK_DIR/web/0.4.0-beta.6/webapp.zip"

scripts/publish-mobile.sh 0.4.0-beta.6 --s3-base="$MOCK_DIR"

# Verify min_native in web manifest
grep '"min_native": "0.4.0"' "$MOCK_DIR/web/beta/latest.json" && echo "✓ min_native present"
rm -rf "$MOCK_DIR"
```

### Android unit tests

```bash
cd mobile/plugins/k2-plugin && ./gradlew test
```

### Build verification

```bash
# Android
cd mobile/plugins/k2-plugin && ./gradlew build

# iOS (simulator)
cd mobile/ios/App && xcodebuild -scheme App -sdk iphonesimulator -destination 'platform=iOS Simulator,name=iPhone 16' build
```

## Edge Cases

| Scenario | Behavior | Why correct |
|----------|----------|-------------|
| Old manifest without min_native | `isCompatibleNativeVersion(null, appVer)` → true | Backwards compatible |
| App version == min_native | Compatible (>= check) | Exact match is OK |
| App version < min_native | OTA skipped, logged as warning | Prevents crash |
| OTA webapp loads successfully | checkReady() removes .boot-pending | Verified, next boot normal |
| OTA webapp crashes (JS error) | .boot-pending stays → next boot rollback | Auto-recovery |
| User force-kills after OTA before checkReady | .boot-pending stays → rollback | Safe — uses bundled |
| No OTA dir (fresh install) | No web-update/ → no markers | Clean path |
| Native upgrade with stale OTA | .boot-pending may exist → rollback → new bundled | Correct |
| Two consecutive OTA applies | Each creates .boot-pending → verified independently | Correct |
