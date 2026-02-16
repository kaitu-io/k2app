# Feature: Mobile Update System

## Meta

| Field      | Value                                                     |
|------------|-----------------------------------------------------------|
| Feature    | mobile-updater                                            |
| Version    | v1                                                        |
| Status     | draft                                                     |
| Created    | 2026-02-14                                                |
| Updated    | 2026-02-14                                                |
| Depends on | mobile-webapp-bridge, mobile-vpn-ios, mobile-vpn-android  |

## Version History

| Version | Date       | Summary                                                              |
|---------|------------|----------------------------------------------------------------------|
| v1      | 2026-02-14 | Initial: three-channel update system (Web OTA, Android APK, iOS App Store) + CI/CD pipeline |

## Overview

Three-channel update system for k2app mobile clients:

1. **Web OTA** — Hot update webapp assets (JS/CSS/HTML) without app store review
2. **Android APK self-update** — Silent download + system installer, no browser redirect
3. **iOS App Store** — CI auto-upload to App Store Connect, manual release trigger

Plus CI/CD pipeline to automate build + publish on `v*` tag push.

## Context

- Desktop already has Tauri updater (CloudFront + S3 `latest.json` -> download -> install)
- Mobile has no update mechanism — `build-mobile.yml` is manual dispatch only
- Capacitor hybrid architecture enables web-layer-only OTA (web assets are separate files on device)
- Apple Developer account + App Store listing already exist (app ID: `6448744655`)
- S3 bucket `d.all7.cc` and CloudFront already in use for desktop releases

## Product Requirements

- PR1: Web OTA hot-updates webapp assets (JS/CSS/HTML) without app store review
- PR2: Android APK self-update downloads + installs silently, no browser redirect
- PR3: iOS updates via App Store with CI auto-upload
- PR4: Update check on every app launch (priority: native > web)
- PR5: CI/CD pipeline automates build + publish on v* tag push
- PR6: S3 (d.all7.cc) hosts all update manifests and artifacts

## Technical Decisions

### TD1: Update Channel Architecture

Three independent channels, each with its own manifest and delivery mechanism.

```
                    ┌─────────────────────────────┐
                    │     S3 (d.all7.cc/kaitu/)    │
                    │                              │
                    │  web/latest.json             │  ← Web OTA manifest
                    │  web/{ver}/webapp.zip        │  ← Web asset bundles
                    │  android/latest.json         │  ← APK update manifest
                    │  android/{ver}/Kaitu-*.apk   │  ← APK files
                    │  desktop/...                 │  ← Existing, unchanged
                    └──────────┬───────────────────┘
                               │
          ┌────────────────────┼────────────────────┐
          │                    │                    │
    ┌─────▼─────┐      ┌──────▼──────┐     ┌───────▼───────┐
    │  Web OTA  │      │ Android APK │     │  iOS          │
    │  Both     │      │ Self-update │     │  App Store    │
    │  platforms│      │ via S3      │     │  + TestFlight │
    └───────────┘      └─────────────┘     └───────────────┘
```

| Channel | Trigger | Speed | User experience |
|---------|---------|-------|-----------------|
| Web OTA | Webapp code change only | Seconds, next launch | Invisible |
| Android APK | Native/Go code change | Minutes, one-tap install | System install dialog |
| iOS App Store | Any change | Hours-days (Apple review) | App Store update |

#### Update Check Priority

```
App startup
  → checkNativeUpdate()      ← Check native version first
     → New version? Prompt full update (APK / App Store)
     → No new version? Continue ↓
  → checkWebUpdate()          ← Then check web version
     → New version? Silent download, apply on next launch
```

Native update takes priority because a new native version may contain incompatible web changes.

#### S3 Layout

```
d.all7.cc/kaitu/
├── web/                          ← Shared (mobile + future platforms)
│   ├── latest.json
│   └── {version}/
│       └── webapp.zip
├── android/
│   ├── latest.json
│   └── {version}/
│       └── Kaitu-{version}.apk
├── ios/
│   └── latest.json               ← Version + App Store URL only
├── desktop/                       ← Existing, unchanged
│   ├── cloudfront.latest.json
│   ├── d0.latest.json
│   └── {version}/...
```

Web OTA assets at `kaitu/web/` (not `kaitu/mobile/web/`) because the webapp is shared across platforms.

### TD2: Web OTA Mechanism

#### Manifest Format

`web/latest.json`:
```json
{
  "version": "0.5.0",
  "url": "https://d0.all7.cc/kaitu/web/0.5.0/webapp.zip",
  "hash": "sha256:abc123...",
  "size": 1523456,
  "released_at": "2026-02-14T10:00:00Z"
}
```

#### Client Flow (K2Plugin native layer)

```
App startup
  → K2Plugin.checkWebUpdate()
  → fetch web/latest.json
  → Compare version vs current bundle version
  → If new version:
      1. Background download webapp.zip
      2. Verify sha256 hash
      3. Unzip to app sandbox updates/ directory
      4. Atomic swap: updates/ → active/, old version → backup/
      5. Takes effect on next app launch (webview reload)
  → If download/verify fails:
      Fall back to backup/ version
```

#### Key Design Points

- Keep **1 backup** version for rollback
- zip content = `webapp/dist/` output (Vite build)
- Capacitor webDir path reconfigured to point to sandbox instead of app bundle
- sha256 hash verification prevents corruption/tampering
- No real-time webview reload — simpler, takes effect on next launch

### TD3: Android APK Self-Update

#### Manifest Format

`android/latest.json`:
```json
{
  "version": "0.5.0",
  "url": "https://d0.all7.cc/kaitu/android/0.5.0/Kaitu-0.5.0.apk",
  "hash": "sha256:def456...",
  "size": 45678901,
  "released_at": "2026-02-14T10:00:00Z",
  "min_android": 26
}
```

#### Client Flow (K2Plugin Kotlin layer)

```
App startup (or manual check)
  → K2Plugin.checkNativeUpdate()
  → fetch android/latest.json
  → Compare version vs BuildConfig.VERSION_NAME
  → If new version:
      1. Silent background download APK to app cache
      2. Verify sha256 hash
      3. Notify webapp: show in-app update dialog (version + size)
      4. User taps "Update"
      5. FileProvider + ACTION_INSTALL_PACKAGE → system installer
      6. User taps "Install" in system dialog → done
```

#### Key Design Points

- **No browser redirect** — entire flow stays in-app
- APK downloaded silently in background (user unaware until ready)
- Requires `REQUEST_INSTALL_PACKAGES` permission in AndroidManifest.xml
- Uses `FileProvider` for secure APK sharing with system installer (Android 7+)
- User sees only one system dialog ("Install") — minimal friction

### TD4: iOS Update Path

iOS cannot self-update native code (Apple restriction). Two mechanisms:

#### App Store / TestFlight Redirect

`ios/latest.json`:
```json
{
  "version": "0.5.0",
  "appstore_url": "https://apps.apple.com/app/id6759199298",
  "released_at": "2026-02-14T10:00:00Z"
}
```

Client flow:
```
checkNativeUpdate() → fetch ios/latest.json
  → New version? Show in-app dialog → open App Store URL
```

#### Web OTA (shared with Android)

iOS uses the same `web/latest.json` mechanism as Android. Web-only updates bypass App Store entirely.

### TD5: CI/CD Pipeline

#### Trigger Rules

All `v*` tags trigger the full pipeline. No prerelease distinction (2-person team, simplicity > staged rollout).

```
v* tag push
  ├──→ release-desktop.yml (existing, unchanged)
  │      macOS + Windows → S3
  │
  └──→ build-mobile.yml (enhanced)
         ├── iOS: build → IPA → upload App Store Connect
         │     ├── TestFlight: immediate (internal, no review)
         │     └── App Store: auto-submit, "manual release" mode
         ├── Android: build APK → S3 + update android/latest.json
         └── Web OTA: zip webapp/dist → S3 + update web/latest.json
```

#### Rationale for No Prerelease Tags

1. 2-person team doesn't need staged rollout
2. TestFlight internal testing needs no review — instant after upload
3. App Store "Pending Developer Release" is the safety gate
4. One rule (`v*` = everything) reduces cognitive overhead

#### CI Secrets (new)

| Secret | Purpose |
|--------|---------|
| `APP_STORE_CONNECT_API_KEY_BASE64` | .p8 API key for upload |
| `APP_STORE_CONNECT_KEY_ID` | API key identifier |
| `APP_STORE_CONNECT_ISSUER_ID` | Team issuer ID |
| `IOS_DISTRIBUTION_PROFILE_BASE64` | Distribution provisioning profile |

#### CI Files (new/modified)

| File | Change |
|------|--------|
| `.github/workflows/ci.yml` | + K2 plugin tsc --noEmit |
| `.github/workflows/build-mobile.yml` | + v* tag trigger, TestFlight upload, S3 upload, manifest generation |
| `mobile/ios/ExportOptions.plist` | New — IPA export config (team ID, profile, method=app-store) |
| `scripts/ci/upload-mobile-s3.sh` | New — S3 upload + manifest generation for Android + Web OTA |

### TD6: Engineer Workflow

```
1. Develop on branches, merge to main

2. Ready to release → push v* tag
   $ git tag v0.5.0 && git push --tags
   → Desktop + Mobile CI triggers automatically

3. CI does everything:
   → Desktop: build + sign + upload S3
   → iOS: build + upload App Store Connect (TestFlight immediate)
   → Android: build + upload APK to S3
   → Web OTA: zip + upload to S3
   → All latest.json manifests updated

4. Test on TestFlight (immediate) + Android APK (from S3)

5. App Store review passes → "Pending Developer Release"
   → Go to App Store Connect → click "Release" (only manual step)
```

## Acceptance Criteria

### Web OTA

- AC1: Web OTA bundle uploaded to `d.all7.cc/kaitu/web/{version}/`
- AC2: `web/latest.json` updated with version, URL, hash, size
- AC3: App checks for web update on startup (after native check)
- AC4: Downloads zip, verifies sha256 hash, extracts to sandbox
- AC5: New web assets take effect on next launch
- AC6: Fallback to backup on download/verify failure

### Android APK

- AC7: Android APK uploaded to `d.all7.cc/kaitu/android/{version}/`
- AC8: `android/latest.json` updated with version, URL, hash, size
- AC9: APK downloaded silently in background
- AC10: In-app dialog shows when download ready (version + size)
- AC11: One-tap triggers system installer (no browser redirect)

### iOS

- AC12: `ios/latest.json` updated with version and appstore_url
- AC13: In-app dialog with "Update" opens App Store page

### CI/CD

- AC14: `ci.yml` runs K2 plugin type-check on push/PR
- AC15: `build-mobile.yml` triggers automatically on `v*` tag push
- AC16: iOS job exports IPA and uploads to App Store Connect
- AC17: TestFlight internal testers can install within minutes of tag push
- AC18: App Store review is submitted with "manually release" option

### Update Check

- AC19: App checks for native update on startup (priority over web)
- AC20: Update UI shows version number and download size

## Testing Strategy

### Automated

- **CI type-check**: K2 plugin TypeScript definitions compile (`tsc --noEmit`)
- **Webapp unit tests**: Mock `checkWebUpdate()` / `checkNativeUpdate()` responses, verify update dialog state transitions
- **S3 manifest validation**: CI script verifies `latest.json` schema after upload (version, url, hash, size fields present)

### Manual Integration

- **Web OTA end-to-end**: Build webapp with visible version string, upload bundle, verify next app launch picks up new version
- **Web OTA rollback**: Corrupt the downloaded zip, verify app falls back to backup version
- **Android APK flow**: Push v* tag, verify APK appears on S3, install on device, verify system installer dialog
- **iOS App Store flow**: Push v* tag, verify IPA uploads to App Store Connect, verify TestFlight availability
- **Update priority**: Deploy both native + web update simultaneously, verify native update prompt appears first
- **No-update path**: Verify app launches normally when already on latest version (no spurious dialogs)
- **Offline resilience**: Verify app launches normally when manifest fetch fails (no crash, no blocking UI)

## Deployment & CI/CD

Build + publish is fully automated on `v*` tag push:

```
v* tag push
  → build-mobile.yml triggers
  → iOS: gomobile bind → xcframework → cap sync → xcodebuild archive → IPA → App Store Connect
  → Android: gomobile bind → AAR → cap sync → assembleRelease → APK → S3
  → Web OTA: yarn build → zip dist/ → S3
  → Manifests: generate + upload latest.json for web/android/ios
```

CI secrets required: see TD5 (CI Secrets table).

New CI files:
- `mobile/ios/ExportOptions.plist` — IPA export config
- `scripts/ci/upload-mobile-s3.sh` — S3 upload + manifest generation
