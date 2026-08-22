package io.kaitu.k2plugin

/**
 * Pure decision core for the cold-start auto-update check.
 *
 * WHY THIS FILE EXISTS — the native/web decoupling invariant
 * =========================================================
 * Until 2026-08, `performAutoUpdateCheck` ran the two update lanes as one
 * straight line and `return`ed as soon as the native (APK) lane found an
 * update. Field-confirmed on a Redmi K40 Pro / Android 14 running 0.4.6 while
 * `android/latest.json` served 0.4.7: the "v0.4.7 已准备好安装" banner appeared
 * and `web/latest.json` was never requested even once.
 *
 * That coupling is wrong because the two lanes mean different things:
 *   - native lane = "we SUGGEST you install a new APK" — user-refusable
 *     ("Later"), asynchronous, needs a manual install.
 *   - web  lane  = "silently repair the webapp you are running right now" —
 *     invisible, immediate, effective at the next launch.
 * A user who declines the APK is exactly the user who most needs the hot fix.
 * With the short-circuit, every device one release behind lost the web OTA
 * channel permanently — and web OTA is the only hour-scale remediation path
 * the mobile shell has (the native layer has none: a bad APK means a new
 * store/CDN release).
 *
 * The gate that legitimately expresses "this webapp needs a newer shell" is
 * `min_native` / `min_bridge` on the web manifest, NOT the presence of a native
 * update. Both come from contracts/webapp-support-floor.json. Both gates are
 * MONOTONE in the shell's version: raising the native version (or the bridge
 * version, which only ever moves with it) can only satisfy more floors, never
 * fewer. Therefore a web bundle accepted before a native upgrade stays
 * acceptable after it — applying both in the same run cannot contradict
 * itself. Conversely a webapp that genuinely requires a newer bridge is
 * rejected by `min_bridge`, and the native banner is then the correct — and,
 * thanks to this decoupling, simultaneously visible — way out.
 *
 * Kept free of Android APIs and of org.json on purpose: `JSONObject` is a
 * throwing stub in local unit tests (no Robolectric in this module), so the
 * decision logic is only reachable by tests if it speaks plain data. The
 * caller parses manifests and performs I/O; this file only decides.
 */

/** Native (APK) manifest, already parsed by the I/O shell. */
internal data class NativeManifestInfo(
    val version: String,
    val downloadUrl: String,
    val minAndroid: Int,
)

/**
 * Web OTA manifest, already parsed by the I/O shell. [hash] and [sig] are
 * carried through so the apply step runs the SAME verification core as the
 * user-triggered path — the checks must never fork between the two.
 */
internal data class WebManifestInfo(
    val version: String,
    val downloadUrl: String,
    val minNative: String,
    val minBridge: Int,
    val hash: String,
    val sig: String,
)

/**
 * One executable outcome. The plan is an ordered list; the shell runs the
 * steps in order and must isolate each step's failure from the next.
 */
internal sealed class AutoUpdateStep {
    /** Emit `nativeUpdateAvailable` to the webapp. Never blocks the web lane. */
    data class NotifyNative(val version: String, val url: String) : AutoUpdateStep()

    /** Download + verify + install the web bundle. */
    data class ApplyWeb(val manifest: WebManifestInfo) : AutoUpdateStep()

    /** Nothing to do on [lane] ("native" / "web"). Carries a loggable reason. */
    data class Skip(val lane: String, val reason: String) : AutoUpdateStep()
}

/**
 * Build the auto-update plan. Both lanes ALWAYS run: the native lane never
 * short-circuits the web lane, and an exception in either lane is confined to
 * that lane (a throwing manifest supplier yields a Skip, not a lost web OTA).
 *
 * @param nativeManifest supplier for the parsed android manifest; null = unreachable/unusable.
 * @param webManifest    supplier for the parsed web manifest; null = unreachable/unusable.
 * @param appVersion     installed APK versionName.
 * @param localWebVersion currently effective webapp version (web-update/version.txt, else appVersion).
 * @param sdkInt         Build.VERSION.SDK_INT.
 * @param bridgeVersion  compile-time bridge API version of this shell.
 * @param forceDowngrade beta→stable channel switch: allow a non-newer native version.
 * @param quarantinedWebVersion web version that already failed to boot once and
 *   was rolled back; null when nothing is quarantined.
 */
internal fun planAutoUpdate(
    nativeManifest: () -> NativeManifestInfo?,
    webManifest: () -> WebManifestInfo?,
    appVersion: String,
    localWebVersion: String,
    sdkInt: Int,
    bridgeVersion: Int = K2PluginUtils.BRIDGE_API_VERSION,
    forceDowngrade: Boolean = false,
    quarantinedWebVersion: String? = null,
): List<AutoUpdateStep> {
    val steps = mutableListOf<AutoUpdateStep>()

    // ── Lane 1: native APK ──────────────────────────────────────────
    // Produces at most a notification. Deliberately does NOT return.
    steps += try {
        val m = nativeManifest()
        when {
            m == null -> AutoUpdateStep.Skip("native", "manifest unavailable")
            !nativeShouldUpdate(m.version, appVersion, forceDowngrade) ->
                AutoUpdateStep.Skip("native", "no newer version (remote=${m.version} local=$appVersion)")
            sdkInt < m.minAndroid ->
                AutoUpdateStep.Skip("native", "min_android=${m.minAndroid} > sdk=$sdkInt")
            else -> AutoUpdateStep.NotifyNative(m.version, m.downloadUrl)
        }
    } catch (e: Exception) {
        AutoUpdateStep.Skip("native", "check failed: ${e.message}")
    }

    // ── Lane 2: web OTA ─────────────────────────────────────────────
    // Runs regardless of lane 1's outcome — that is the whole point of this
    // file. Gate order matches the pre-decoupling code: min_native, then
    // min_bridge, then version.
    steps += try {
        val m = webManifest()
        when {
            m == null -> AutoUpdateStep.Skip("web", "manifest unavailable")
            !K2PluginUtils.isCompatibleNativeVersion(m.minNative, appVersion) ->
                AutoUpdateStep.Skip("web", "min_native=${m.minNative} > app=$appVersion")
            !K2PluginUtils.isCompatibleBridgeVersion(m.minBridge, bridgeVersion) ->
                AutoUpdateStep.Skip("web", "min_bridge=${m.minBridge} > bridge=$bridgeVersion")
            !K2PluginUtils.isNewerVersion(m.version, localWebVersion) ->
                AutoUpdateStep.Skip("web", "no newer version (remote=${m.version} local=$localWebVersion)")
            // Already failed to boot once and was rolled back. Without this the
            // shell is a reinstall treadmill: rollback deletes version.txt, so
            // localWebVersion falls back to appVersion and this same bundle
            // reads as "newer" on the very next check. Version-scoped, so
            // publishing a newer bundle clears it (desktop F2 semantics).
            quarantinedWebVersion == m.version ->
                AutoUpdateStep.Skip("web", "version quarantined")
            else -> AutoUpdateStep.ApplyWeb(m)
        }
    } catch (e: Exception) {
        AutoUpdateStep.Skip("web", "check failed: ${e.message}")
    }

    return steps
}

/**
 * beta→stable downgrade: a beta build accepts ANY differing stable version so
 * the user can drop off the beta train. Otherwise the normal "strictly newer"
 * rule applies. Extracted verbatim from the pre-decoupling implementation.
 */
internal fun nativeShouldUpdate(
    remoteVersion: String,
    localVersion: String,
    forceDowngrade: Boolean,
): Boolean = if (forceDowngrade && localVersion.contains("-beta")) {
    remoteVersion != localVersion
} else {
    K2PluginUtils.isNewerVersion(remoteVersion, localVersion)
}
