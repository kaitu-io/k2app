# Mobile — Capacitor 8 + gomobile

Capacitor 8 mobile app wrapping the k2 Go tunnel core via gomobile. K2Plugin bridges JS ↔ native VPN lifecycle.

**Long-form notes (not auto-loaded — read the one you need before changing that code):** `mobile/docs/build-reference-notes.md` (toolchain, version literals, code map, two-process / VpnService detail, logging, gomobile names, full gotcha wording) · `mobile/docs/brand-release-notes.md` (iOS build-number rationale, Android flavors / signing, plugin purity, iOS xcconfigs + derivation iron rule, release chain) · `mobile/docs/runtime-notes.md` (sign-in / push plugin config, manual-only selection, VpnService self-exemption, memory pause/wake, web-OTA boot / rollback / quarantine incidents).

## Toolchain baseline (Capacitor 8)

- **Node ≥ 22** everywhere the root workspace installs — `@capacitor/cli` 8 `engines`, and yarn 1 fails the whole root `yarn install` on a mismatch. All `.github/workflows` pin `'22'`.
- **JDK 21** for Android (`cap sync` regenerates `VERSION_21`). Local: `brew install openjdk@21`; root `Makefile` `ANDROID_JAVA_HOME` exports it only for `appext-android` / `build-android` / `dev-android` (`make check-jdk-21`). CI: `actions/setup-java@v4` `java-version: '21'`.
- Gradle 8.14.3 / AGP 8.13.0 / Kotlin 2.2.20 (JVM target in top-level `kotlin { compilerOptions { … } }`). **`versionCode` / `versionName` keep the space-separated Groovy syntax** — `scripts/sync-version.sh` and CI `check-versions` match them by sed pattern.
- **compileSdk / targetSdk 36** (`android/variables.gradle`, minSdk 24) is Play's floor — lowering it builds fine and only the Play Console refuses.
- **Xcode 26+** for App Store submissions; CI `runs-on: macos-26`, `xcode-version: '26.4'`. iOS real floor **16.0** (every target + `Podfile`; the pbxproj project-level 14.0 is overridden). CocoaPods, NOT SPM.

## Commands

```bash
make dev-android      # gomobile bind + cap sync + cap run android --flavor $(BRAND)
make dev-ios          # gomobile bind + cap sync, then scripts/deploy-ios-device.sh to the auto-detected
                      # physical iPhone (IOS_DEVICE=<udid> overrides); `cap run ios` (simulator) only
                      # when no device is detected — real devices only speak the CoreDevice tunnel
make build-android    # gomobile bind + decrypt-keystore + cap sync + :k2-plugin:testDebugUnitTest
                      # + assemble{Kaitu|Overleap}Release (per flavor — bare assembleRelease builds BOTH)
make build-ios        # gomobile bind + cap sync + xcodebuild archive
make publish-android VERSION=x.y.z BRAND=kaitu   # latest.json manifests → scripts/publish-mobile.sh
make publish-ios     VERSION=x.y.z BRAND=kaitu
make plugin-purity-check                          # k2-plugin must stay gomobile-free (see Gotchas)
make appext-android   # → k2/build/k2mobile.aar; build-android/dev-android copy it to mobile/android/app/libs/
make appext-ios       # → k2/build/K2Mobile.xcframework; build-ios/dev-ios copy it to mobile/ios/App/
```

After editing `plugins/k2-plugin/src/`: `make build-k2-plugin` (from the repo root: `npm run build` → committed `dist/`, then `rm -rf node_modules/k2-plugin && yarn install --force` to refresh the `file:` copy), then `cd mobile && npx cap sync` (`build-k2-plugin` does not sync; the `dev-*` / `build-*` mobile targets do).

CI entry points are `scripts/build-mobile-{ios,android}.sh`, not `make build-*` (same steps, same plugin-test gate). `build-mobile.yml` sets `K2_BUILD_LOG_LEVEL: info` and `EMBED_REQUIRE_FULL: "1"` (root CLAUDE.md); its `dry_run` builds + signs + runs the purity gate, skips ASC / S3 / Slack.

## Versioning (package.json → native literals)

`scripts/sync-version.sh` (run by `make pre-build` on every build path) rewrites, from root `package.json`:

- Android `versionCode = MAJOR*1000000 + MINOR*10000 + PATCH*100 + REV`; `REV` = `ANDROID_BUILD_REV` (0-99), set **only** to re-upload an already-uploaded version to Play (it burns every accepted versionCode), then clear. CI `check-versions` reads only `versionName`.
- `K2Helpers.swift` `let k2AppVersion` — **compiled in on purpose**: it is what the web-OTA `min_native` gate compares against, and a gate must never fall back to a different quantity (e.g. Info.plist). Bumping `package.json` without `pre-build` ships a stale iOS gate.
- `project.pbxproj` `MARKETING_VERSION` + `CURRENT_PROJECT_VERSION` come from `scripts/build-mobile-ios.sh --print-build-number` — never a second copy of the formula.

**iOS build number** (`scripts/build-mobile-ios.sh`):
- Marketing version is **per brand**: kaitu `0.x.y` → `4.x.y` (must stay > the predecessor's 3.x for Apple's downgrade check); overleap ships the real semver.
- `CFBundleVersion = 4000000 + MINOR*100000 + PATCH*1000 + SLOT*10 + REV`, brand-independent; SLOT = N for `-beta.N` (1..98), 99 for final. REV = repo variable `IOS_BUILD_REV` (0..9), set **only** to re-upload an already-uploaded (version, slot) after an ASC rejection, then clear — never a run number. Aborts on MAJOR ≠ 0 or MINOR/PATCH > 99.
- `scripts/test-ios-build-number.sh` (ci.yml) drives the real script — don't restate the arithmetic in a test.

**Bridge API version**: `K2Helpers.swift` `k2BridgeApiVersion` = `K2PluginUtils.kt` `BRIDGE_API_VERSION` = webapp `BRIDGE_API_VERSION` (and desktop `DESKTOP_BRIDGE_VERSION`). `bridge-contract.test.ts` snapshots `contracts/bridge-api.json` — a new `CAPPluginMethod` or bumped literal without the regen turns webapp vitest red. Procedure: `webapp/CLAUDE.md` 兼容模型 "契约门".

## Brand (kaitu / overleap)

Same `K2_BRAND` build-time contract as desktop/webapp (root `Makefile` `BRAND ?= kaitu` → `export K2_BRAND`; a script calling `make` must pass `BRAND=$BRAND` — see root CLAUDE.md Cross-Layer). Invariants (mechanism + incidents: `mobile/docs/brand-release-notes.md`):

- `scripts/build-mobile-{ios,android}.sh` **re-export `K2_BRAND` themselves** — they call `npx cap sync` outside make's env; dropping it shipped an overleap APK with `"appId":"io.kaitu"`. `capacitor.config.ts` `appId`/`appName` are inert but kept brand-conditional.

### Android

- Gradle product flavors `kaitu`/`overleap` on dimension `brand` (`android/app/build.gradle`): `applicationId` forks (`io.kaitu`/`io.overleap`) but `namespace "io.kaitu"` stays **shared** — that's why `io.kaitu.K2VpnService` / `io.kaitu.k2plugin` class names work unchanged for both flavors (namespace ≠ applicationId).
- Per-flavor `app/src/{kaitu,overleap}/res/values/brand.xml`: `k2_cdn_primary` / `k2_cdn_fallback` / `k2_vpn_display_name`, bool `k2_apk_updates` (overleap `false`: a Play app must never self-install APKs — native lane off, web OTA untouched).
- Launcher / splash artwork is **generated** by `webapp/brand-assets/<brand>/generate.sh` — never hand-edit a PNG (guard: `scripts/check-mobile-brand-assets.sh <brand>`). Release builds also emit `release/{VERSION}/{Kaitu|Overleap}-{VERSION}.aab` (Play's format).
- Keystores `app/{kaitu,overleap}-release.jks.enc` (AES-256-CBC; `signingConfigs.release` / `signingConfigs.overleap`), passwords `KAITU_ANDROID_STORE_PASSWORD` / `OVERLEAP_ANDROID_STORE_PASSWORD`; `make decrypt-keystore BRAND=<brand>`; plaintext `.jks` gitignored.
- `signingConfigs` are assigned **unconditionally** at the flavor level (a missing password must fail `assemble*Release` loudly, never produce an unsigned APK); debug gets a *separate* conditional override in `androidComponents.onVariants(...withBuildType("debug"))`. Don't collapse the two halves into one conditional.

### Plugin brand purity

- `plugins/k2-plugin` has no flavors and no brand CDN literals: `K2PluginUtils.brandString()` resolves `k2_cdn_primary` / `k2_cdn_fallback` from the **host app**'s merged resources.
- Bare `kaitu` internal tokens are exempt on both brands (`kaitu-icon://`, `io.kaitu.k2plugin`, `kaitu-service-logs`, `io.kaitu.K2VpnService`, `package_name`); `scripts/check-mobile-brand-purity.sh <brand> <apk-or-xcarchive>` forbids `kaitu\.io|开途|開途|/kaitu/(android|ios|web)/` on overleap, not the bare word.

### iOS

- `brand-{kaitu,overleap}.xcconfig` (under `ios/App/App/Config/`) define the `K2_*` brand vars. `scripts/apply-ios-brand.sh <brand>` copies one to `brand-active.xcconfig` (committed content is always the kaitu fallback), stages `InfoPlist.strings` and swaps `Assets.xcassets/{AppIcon.appiconset,Splash.imageset}` from `App/brand/<brand>/`. **These diffs are never committed** — run `scripts/apply-ios-brand.sh kaitu` before committing anything else under `mobile/ios/`.
- Wrapper configs (`App-Base-{Debug,Release}.xcconfig`, `PacketTunnelExtension-Base-*.xcconfig`) must `#include` both the CocoaPods xcconfig *and* `brand-active.xcconfig` — that is what keeps `pod install` working.
- `K2_APP_STORE_URL` is empty for overleap; `K2Plugin.swift` treats empty as absent at both call sites. Distinct from `OVERLEAP_APPSTORE_URL` (publish-time override for `scripts/publish-mobile.sh`).

### iOS derivation iron rule

- Swift reads brand values from `Info.plist` keys (`K2AppGroup` / `K2CDNPrimary` / `K2CDNFallback` / `K2VpnDisplayName` / `K2AppStoreURL`). The NE bundle id is derived (`+ ".ThePacketTunnel"`) **only in the main app process**; `PacketTunnelProvider.swift` reads its own bundle id.
- **Never edit the `?? ` fallback literals** in `K2Plugin.swift` / `AppDelegate.swift` / `PacketTunnelProvider.swift` — they are the pre-split kaitu values, and `loadVPNManager()` deletes any NE config that drifts from them (wipes live users' VPN configs). `K2Tests/BrandDerivationTests.swift` asserts them but **nothing runs `K2Tests`** — reference, not enforcement.
- **kaitu's real iOS bundle id is the legacy ANC one** (`com.allnationconnect.anc.wgios`), immutable (migrating zeros ratings and orphans subscriptions); kaitu IAP lives under ASC app `6448744655`. Overleap is a separate ASC record (`6759199298`, `io.overleap`).

### Release chain

- `scripts/publish-mobile.sh VERSION --platform=android|ios --brand=kaitu|overleap`; overleap's manifest `appstore_url` defaults to `https://apps.apple.com/app/id6759199298` (`OVERLEAP_APPSTORE_URL` only overrides it). **Overleap Android is Play-only**: that combination prints `Play-only` and exits 0 without touching S3.
- `build-mobile.yml` takes brands/platforms from `scripts/ci/release-plan.mjs` (unknown suffix → every job skips); `workflow_dispatch` brand **defaults to both**. ASC upload runs for both brands; Android S3 upload is kaitu-only (overleap AAB goes to Play by hand). Purity gates run on the **brand-exact artifact path, never a glob** (`813bf3f5`). It does **not** publish web OTA (root CLAUDE.md "Web OTA").
- **Overleap builds with every bare tag** (uploads to ASC, no submission), so `IOS_BUILD_REV` / `ANDROID_BUILD_REV` bump both brands together. Overleap's App Store link is built from the ASC app id (webapp `storeUrls.ios` and `publish-mobile.sh` already carry it); only `K2_APP_STORE_URL` in `brand-overleap.xcconfig` is still empty.

## Code map (non-obvious locations — full list in notes)

- `plugins/k2-plugin/` — **gomobile-free**, `dist/` committed. JVM-tested Kotlin cores `K2PluginUtils.kt` / `AutoUpdatePlan.kt` / `WebBootDecision.kt`; iOS `K2Helpers.swift`, StoreKit 2 in `K2Plugin+Iap.swift` / `StoreKitManager.swift`, `K2RelayBridge.swift` handler slots.
- gomobile calls live in the app shells: `android/app/src/main/java/io/kaitu/` (`K2VpnService.kt`, `MainApplication.kt`) and `ios/App/App/AppDelegate.swift`. **NetEvent** (`Engine.NotifyNetEvent`) is wired there, in `K2VpnService.kt` / `PacketTunnelProvider.swift` (Swift: `engine?.notify(event)`) — not in `mobile/plugins/`.
- `ios/App/PacketTunnelExtension/` has its own `NativeLogger.swift` (50 MB vs the plugin's 20 MB copy — fix both when touching either). `ios/App/K2Tests/` is **not run in CI**. `k2mobile.aar` / `K2Mobile.xcframework` copies are gitignored.

## iOS Two-Process Architecture

App process (`K2Plugin.swift`) ↔ NE process (`PacketTunnelProvider`, gomobile Engine), sharing state through the App Group.

- **State source of truth**: `NEVPNStatusDidChange` notification ONLY. NE errors: `vpnError` in App Group → `cancelTunnelWithError()` → K2Plugin reads it on `.disconnected`.
- `configJSON` via `startVPNTunnel(options:)`, fallback `providerConfiguration`. TUN fd: KVC `socket.fileDescriptor`, then `findTunnelFileDescriptor()`.
- The **App target also links `K2Mobile.xcframework`** (relay fetch + rule prefetch, VPN-independent); the K2Plugin pod does not.

## Social Sign-In / Push (plugin config — details in `mobile/docs/runtime-notes.md`)

- Social: `@capgo/capacitor-social-login`. **`capacitor.config.ts` `SocialLogin.providers` is load-bearing** — `facebook: false` keeps the Facebook SDK out (`Podfile.lock` must not contain `FBSDK`). iOS Google URL scheme `K2_GOOGLE_REVERSED_CLIENT_ID` is locked by `webapp/src/brands/__tests__/ios-google-url-scheme.test.ts`. Android: Google only; never pass custom Google scopes.
- Push (iOS only): `@capacitor/push-notifications`. **Android never gets it**: `capacitor.config.ts` `android.includePlugins` = every dependency minus `IOS_ONLY_PLUGINS` (after `cap sync`, `android/capacitor.settings.gradle` must not mention `push-notifications`). Both App IDs need the Push capability or signing fails; `AppDelegate` must forward the APNs token callbacks or `register()` never resolves.

## Server Selection — Manual only on mobile

Mobile has **no smart-mode / k2subs resolution**: the webapp passes the user's single `k2v5://` URL as `_k2.run('up', {routes:[{via:'k2v5://...'}]})`. The only legitimate `via` on mobile is `k2v5://` or `direct`; a raw `k2subs://` reaching appext → code 570 "no k2v5 outbound configured" — always a webapp bug. No probe / RTT display on mobile. Why: `mobile/docs/runtime-notes.md`.

## Router LAN Bridge

`routerRequest` = `CapacitorHttp` + `disableRedirects: true` behind the TS SSRF gate `assertRouterUrlAllowed` (`capacitor-k2.ts`, private/loopback IPv4 literal only); iOS needs `NSLocalNetworkUsageDescription` + `NSAllowsLocalNetworking`. `getDefaultGateway()` is unconsumed. Flow: `webapp/CLAUDE.md` "Router Tab".

## Android VpnService

- `VpnService.prepare(activity)` needs an **Activity** context. Pass the `ParcelFileDescriptor` `fd`, **not `detachFd()`** (Go dups it; Kotlin closes it). All gomobile JNI calls on the single-thread `engineExecutor` (ANR). `FOREGROUND_SERVICE_TYPE_SPECIAL_USE` on Android 14+.
- **Self-UID exemption**: `Builder.addDisallowedApplication(packageName)` is mandatory — otherwise K2Plugin's own S3 log uploads / cloudApi / OTA downloads route through the tunnel they debug. Regression symptom: Android tickets with `vpnState=connected` show `logCount=0`.
- **`vpnInterface.close()` on teardown is mandatory** — without it routing stays active, every request hangs until reboot.
- **`VpnService.protect()`** must cover wire transport (QUIC UDP, TCP-WS TCP), direct DNS and direct tunnel connections; gomobile needs an `int32` fd.

## Crash Diagnostics & Memory (appext) — details in `mobile/docs/runtime-notes.md`

- `k2/appext/appext.go`: `debug.SetTraceback("crash")` + `recover()` wrappers on every exported Engine method (`Start()` / `Stop()` → error, `StatusJSON()` → `{"state":"disconnected"}`, `Pause()` / `Wake()` / `NotifyNetEvent()` → log only). Go memory strategy: `k2/appext/CLAUDE.md`.
- **Android `onTrimMemory()`** pauses only at `TRIM_MEMORY_RUNNING_CRITICAL` — not `RUNNING_LOW` (ticket #3169: tore down the tunnel while the UI read "connected"). `engine.pause()` + `Appext.freeMemory()`, `AtomicBoolean(enginePaused)` guards double-pause; wake on `onAvailable()` or the 60 s `pendingPauseTimeout` safety net (a stable network never fires `onAvailable()`; the timeout is cancelled by a real `onAvailable()` and by `stopVpn()`, which also resets `enginePaused`).
- `StatusJSON()` must report `"paused"` while paused, or the webapp's 15 s safety-net poll overwrites the paused UI back to "connected".
- **iOS** `sleep()` → stop memory monitor, `engine.pause()`, `AppextFreeMemory()`; `wake()` → `engine.wake()`.

## Logging

- Files under LogDir: `k2.log` (Go), `native.log` (`NativeLogger`), `webapp.log` (`K2Plugin.appendLogs`). iOS LogDir `{AppGroup}/logs/` (shared App ↔ NE); Android `{filesDir}/logs/`.
- `K2Plugin.uploadLogs()` → S3 `mobile/{version}/{udid}/{date}/logs-{ts}-{id}.zip`, feedback only (no beta auto-upload); token / password / `Bearer` / `X-K2-Token` redacted first. Legacy prefixes (`service-logs/` / `feedback-logs/`) are still recognised by the notify Lambda (`scripts/lambda/s3-log-notify/index.mjs` `LEGACY_PREFIXES`).
- Log level: Android `K2Plugin.setLogLevel()` applies immediately (same process); **iOS only logs** — the NE takes `configJSON.log.level` on the next `startVPNTunnel`.

## Gotchas

- **Plugin is gomobile-free by rule**: `make plugin-purity-check` (prerequisite of `appext-ios` / `appext-android`) fails on any `appext.` / `Appext*` under `plugins/k2-plugin/`. The iOS plugin reaches relay only through `K2RelayBridge` handler slots (nil → relay unsupported); Android through `VpnServiceBridge.relayFetch`.
- **K2Plugin `dist/` must be committed** — webapp `tsc` reads `dist/definitions.d.ts`.
- **`file:` plugin sync**: copied (not symlinked) to `node_modules/`; `make build-k2-plugin` after edits. **Gradle compiles the copy** (main and test), so `./gradlew :k2-plugin:testDebugUnitTest` before re-syncing silently tests the STALE copy and reports green. Both release paths run that gate after `cap sync`.
- **Web OTA minisign pubkey is duplicated** — `K2Plugin.swift` `webOtaMinisignPublicKey` and `K2PluginUtils.kt` `WEB_OTA_MINISIGN_PUBKEY` (same pair as desktop); rotating without both silently breaks mobile OTA. Verification is mandatory when the manifest carries `sig`.
- **IAP is iOS-only** (StoreKit 2); `K2Plugin.kt` has no IAP surface.
- gomobile Swift methods `throws` (no NSError out-param). iOS Debug must use `App/App.entitlements` (NE entitlement) — else "not entitled to establish IPC with plugins". The extension `Info.plist` needs explicit `CFBundleExecutable` + `CFBundleVersion`.
- Android JVM tests of pure utils need `testImplementation "org.json:json:20231013"`.
- `AppBridgeViewController` overrides `router()` with `FixedCapacitorRouter`; Main.storyboard must reference this subclass.
- **Android 15 edge-to-edge**: `@capawesome/capacitor-android-edge-to-edge-support` pads the WebView; `capacitor.config.ts` `SystemBars.insetsHandling: 'disable'` is required (remove it and insets apply twice). Do not hand-roll CSS variables or MainActivity WindowInsets listeners.
- `fetchManifest(endpoints)` tries CloudFront first, S3 fallback.
- **Cold-start auto-update: the native lane and the web-OTA lane are independent and both always run** — a pending APK prompt must never suppress web OTA. Pinned by `AutoUpdatePlanTest` (Android); iOS `performAutoUpdateCheck` has two independent `do` blocks. Only `min_native` / `min_bridge` hold back a web bundle; `k2_apk_updates=false` → no native lane, web OTA still runs (`AutoUpdatePlanTest.apk_updates_disabled_yields_no_native_action_but_web_ota_proceeds`).
- VPN display name is brand-parameterized (`K2VpnDisplayName` / `k2_vpn_display_name`: `"kaitu.io"` / `"Overleap"`).
- **iOS stale VPN config cleanup**: `loadVPNManager()` removes NE configs with wrong `providerBundleIdentifier` or `localizedDescription` on every load — why the iron-rule fallbacks above must never drift.
- **Web OTA min_native is CI-derived from `contracts/webapp-support-floor.json`** (the support floor), never hand-written (the 2026-03 incident) and never a per-feature bump; there is no `minNativeVersion` field. Older apps are handled by runtime capability detection (`webapp/CLAUDE.md` 兼容模型, `docs/superpowers/specs/2026-08-14-web-ota-design.md` §4). Comparison uses BASE version only: `0.4.0-beta.6` satisfies `min_native=0.4.0`.
- **Web OTA boot verification**: `.boot-pending` (in `web-update/`) is cleared by **`confirmWebBootOk()`** after `ReactDOM.render` — **never by `checkReady()`** (runs before first render; clearing there let a white-screen bundle serve itself for months). Pure decision logic: `WebBootDecision.kt` (`WebBootDecisionTest`) / `K2Helpers.swift` `decideWebBoot`.
- **Android `K2Plugin.load()` runs inside the Capacitor `Bridge` constructor, before `localServer` exists**: anything touching `bridge.localServer` / the WebView must be posted to the main looper (`scheduleWebBoot()`), and **any exception from `load()` silently drops the whole plugin** (every JS call → `"K2Plugin" plugin is not implemented on android`) — that broke Android web OTA 2026-02 → 2026-09-13 (#3713 / #3735). Every step stays wrapped in `guarded()`. Verify `load()` changes on a real APK: cold start ×3 with `adb logcat -s K2Plugin:*`, must reach `bindToService`.
- **Stale disk bundle after a store/APK upgrade**: both shells' `decideWebBoot` discard `web-update/` (+ `web-backup/`) when the shell version is newer than `version.txt` (iOS `.discardStale`, Android `DiscardStale`). An unconfirmed boot rolls back first; an undatable bundle is never discarded; `x.y.z.<build>` OTAs outrank the `x.y.z` shell.
- **Web OTA quarantine**: rollback records the failed version in `web-quarantined-version.txt` **outside `web-update/`** (rollback deletes that dir); `planAutoUpdate` (Android) / `webBundleSkipReason` (iOS) refuse that exact version until a newer one is published — without it the shell reinstalls the same bad bundle forever. Mirrors desktop `evaluate_manifest` and Linux `shouldApply`.
- **`.gitignore` for native platforms**: Never ignore entire source directories (`mobile/ios/`, `mobile/android/`). Only ignore build artifacts.

## App Store Optimization

ASO / AEO constitutional rules for App Store Connect submissions (keyword system, no "VPN" in user-visible text, review-notes template, etc.) live in [`docs/marketing/aso-constitutional-rules.md`](../docs/marketing/aso-constitutional-rules.md).

## Android S3 CDN Structure

`d13jc1jqzlg4yt.cloudfront.net/{kaitu,overleap}/android/` (brand-parameterized path, `--brand` flag on `scripts/publish-mobile.sh`; S3 origin `s3://d0.all7.cc/{brand}/`):

- `{VERSION}/Kaitu-{VERSION}.apk` — CI uploads here (`scripts/ci/upload-release.sh --android`). **Nothing is ever published under `overleap/android/`** — overleap Android is Play-only; its `Overleap-{VERSION}.aab` is a workflow artifact uploaded to the Play Console by hand.
- `latest.json` — stable APK manifest; `beta/latest.json` — beta channel
- `tools/tools.json` — adb binaries (`scripts/sync-adb-tools.sh`; kaitu path only)

`publish-mobile.sh` copies the artifact into `beta/{VERSION}/` and then: a **stable** version updates both `latest.json` and `beta/latest.json` (beta is a superset of stable); a **`-beta` version updates only `beta/latest.json`** — it never touches the stable manifest the Android install flow reads. iOS manifests (`ios/latest.json`, `ios/beta/latest.json`) follow the same rule.
