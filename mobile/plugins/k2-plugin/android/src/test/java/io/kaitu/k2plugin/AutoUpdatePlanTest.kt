package io.kaitu.k2plugin

import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Test

/**
 * Regression suite for the native/web auto-update decoupling.
 *
 * The load-bearing test is [both_updates_available_plans_both_lanes]: before
 * 2026-08 the shell returned right after emitting `nativeUpdateAvailable`, so
 * any device one release behind lost web OTA — the only hour-scale remediation
 * channel the mobile shell has. Re-introducing that `return` (or any early exit
 * between the lanes) must turn this file red.
 *
 * Deliberately free of Android APIs and org.json: this module has no
 * Robolectric, so `JSONObject` is a throwing stub in local unit tests. All
 * parsing happens in the K2Plugin I/O shell; everything asserted here is the
 * decision logic.
 */
class AutoUpdatePlanTest {

    private val nativeUp = NativeManifestInfo(
        version = "0.4.9",
        downloadUrl = "https://cdn/0.4.9/Kaitu-0.4.9.apk",
        minAndroid = 26,
    )

    private fun web(
        version: String = "0.4.8.20000000",
        minNative: String = "0.4.8",
        minBridge: Int = 1,
    ) = WebManifestInfo(
        version = version,
        downloadUrl = "https://cdn/web/$version/web.zip",
        minNative = minNative,
        minBridge = minBridge,
        hash = "sha256:deadbeef",
        sig = "",
    )

    private fun plan(
        native: () -> NativeManifestInfo? = { nativeUp },
        webM: () -> WebManifestInfo? = { web() },
        appVersion: String = "0.4.8",
        localWebVersion: String = "0.4.8",
        sdkInt: Int = 34,
        bridgeVersion: Int = 2,
        forceDowngrade: Boolean = false,
        quarantinedWebVersion: String? = null,
    ) = planAutoUpdate(
        nativeManifest = native,
        webManifest = webM,
        appVersion = appVersion,
        localWebVersion = localWebVersion,
        sdkInt = sdkInt,
        bridgeVersion = bridgeVersion,
        forceDowngrade = forceDowngrade,
        quarantinedWebVersion = quarantinedWebVersion,
    )

    private fun List<AutoUpdateStep>.notifiesNative() =
        any { it is AutoUpdateStep.NotifyNative }

    private fun List<AutoUpdateStep>.appliesWeb() =
        any { it is AutoUpdateStep.ApplyWeb }

    private fun List<AutoUpdateStep>.skipReason(lane: String) =
        filterIsInstance<AutoUpdateStep.Skip>().firstOrNull { it.lane == lane }?.reason

    // ==================== THE decoupling invariant ====================

    /**
     * Field-confirmed failure (Redmi K40 Pro / Android 14, app 0.4.6, manifest
     * 0.4.7): the native banner appeared and web/latest.json was never fetched.
     * Both lanes must now be planned in the same run.
     */
    @Test
    fun both_updates_available_plans_both_lanes() {
        val steps = plan()
        assertTrue("native update must still be announced", steps.notifiesNative())
        assertTrue("web OTA must NOT be short-circuited by the native update", steps.appliesWeb())
    }

    /** The web lane must be planned after the native one — the banner should
     *  not wait behind a multi-MB bundle download. */
    @Test
    fun native_lane_is_planned_before_web_lane() {
        val steps = plan()
        val nativeIdx = steps.indexOfFirst { it is AutoUpdateStep.NotifyNative }
        val webIdx = steps.indexOfFirst { it is AutoUpdateStep.ApplyWeb }
        assertTrue(nativeIdx in 0 until webIdx)
    }

    /** Exactly one outcome per lane, always — the plan is total. */
    @Test
    fun plan_always_reports_on_both_lanes() {
        for (n in listOf<() -> NativeManifestInfo?>({ nativeUp }, { null }, { error("boom") })) {
            for (w in listOf<() -> WebManifestInfo?>({ web() }, { null }, { error("boom") })) {
                val steps = plan(native = n, webM = w)
                assertEquals("one step per lane: $steps", 2, steps.size)
            }
        }
    }

    // ==================== matrix: native x web ====================

    @Test
    fun native_update_no_web_update() {
        val steps = plan(webM = { web(version = "0.4.8") }, localWebVersion = "0.4.8")
        assertTrue(steps.notifiesNative())
        assertTrue(!steps.appliesWeb())
        assertTrue(steps.skipReason("web")!!.contains("no newer version"))
    }

    @Test
    fun no_native_update_web_update() {
        val steps = plan(native = { nativeUp.copy(version = "0.4.8") })
        assertTrue(!steps.notifiesNative())
        assertTrue(steps.appliesWeb())
    }

    @Test
    fun neither_update() {
        val steps = plan(
            native = { nativeUp.copy(version = "0.4.8") },
            webM = { web(version = "0.4.8") },
        )
        assertTrue(!steps.notifiesNative())
        assertTrue(!steps.appliesWeb())
    }

    @Test
    fun native_manifest_unreachable_does_not_block_web() {
        val steps = plan(native = { null })
        assertEquals("manifest unavailable", steps.skipReason("native"))
        assertTrue(steps.appliesWeb())
    }

    @Test
    fun web_manifest_unreachable_does_not_block_native() {
        val steps = plan(webM = { null })
        assertTrue(steps.notifiesNative())
        assertEquals("manifest unavailable", steps.skipReason("web"))
    }

    // ==================== failure isolation ====================

    /** A throwing native lane (malformed manifest, missing key, network stack
     *  blowing up mid-parse) previously aborted the whole function through the
     *  single shared try/catch. */
    @Test
    fun native_lane_exception_does_not_block_web() {
        val steps = plan(native = { throw IllegalStateException("bad json") })
        assertTrue(steps.skipReason("native")!!.contains("check failed"))
        assertTrue("web OTA must survive a native-lane throw", steps.appliesWeb())
    }

    @Test
    fun web_lane_exception_does_not_block_native() {
        val steps = plan(webM = { throw IllegalStateException("bad json") })
        assertTrue(steps.notifiesNative())
        assertTrue(steps.skipReason("web")!!.contains("check failed"))
    }

    @Test
    fun both_lanes_throwing_yields_two_skips() {
        val steps = plan(native = { error("a") }, webM = { error("b") })
        assertEquals(2, steps.filterIsInstance<AutoUpdateStep.Skip>().size)
    }

    // ==================== min_native gate ====================

    @Test
    fun min_native_gate_blocks_web_but_keeps_native_banner() {
        // The device is too old for the newest webapp — the APK update IS the
        // way out, and it must still be offered.
        val steps = plan(webM = { web(minNative = "0.5.0") }, appVersion = "0.4.8")
        assertTrue(steps.notifiesNative())
        assertTrue(!steps.appliesWeb())
        assertTrue(steps.skipReason("web")!!.contains("min_native=0.5.0"))
    }

    @Test
    fun min_native_equal_passes() {
        assertTrue(plan(webM = { web(minNative = "0.4.8") }, appVersion = "0.4.8").appliesWeb())
    }

    @Test
    fun min_native_absent_passes() {
        assertTrue(plan(webM = { web(minNative = "") }).appliesWeb())
    }

    // ==================== min_bridge gate ====================

    /**
     * The gate that legitimately expresses "this webapp needs a newer shell".
     * It — not the presence of a native update — is what must hold back a web
     * bundle running ahead of its bridge, and it must not suppress the native
     * banner that resolves the situation.
     */
    @Test
    fun min_bridge_gate_blocks_web_but_keeps_native_banner() {
        val steps = plan(webM = { web(minBridge = 3) }, bridgeVersion = 2)
        assertTrue(steps.notifiesNative())
        assertTrue(!steps.appliesWeb())
        assertTrue(steps.skipReason("web")!!.contains("min_bridge=3"))
    }

    @Test
    fun min_bridge_equal_passes() {
        assertTrue(plan(webM = { web(minBridge = 2) }, bridgeVersion = 2).appliesWeb())
    }

    @Test
    fun min_bridge_absent_passes() {
        // Pre-bridge-era manifests carry no min_bridge; shell parses it as 0.
        assertTrue(plan(webM = { web(minBridge = 0) }, bridgeVersion = 2).appliesWeb())
    }

    /**
     * Monotonicity — the safety argument for applying both updates in one run.
     * A bundle accepted at the current native/bridge version stays accepted
     * after the user installs the newer APK, so the two lanes cannot produce a
     * self-contradictory end state.
     */
    @Test
    fun gates_are_monotone_in_shell_version() {
        val m = web(minNative = "0.4.8", minBridge = 2)
        assertTrue(plan(webM = { m }, appVersion = "0.4.8", bridgeVersion = 2).appliesWeb())
        // After installing the 0.4.9 APK (bridge can only go up, never down):
        assertTrue(plan(webM = { m }, appVersion = "0.4.9", bridgeVersion = 2).appliesWeb())
        assertTrue(plan(webM = { m }, appVersion = "0.4.9", bridgeVersion = 3).appliesWeb())
    }

    // ==================== min_android gate ====================

    @Test
    fun min_android_gate_blocks_native_but_keeps_web() {
        val steps = plan(native = { nativeUp.copy(minAndroid = 35) }, sdkInt = 34)
        assertTrue(!steps.notifiesNative())
        assertTrue(steps.skipReason("native")!!.contains("min_android=35"))
        assertTrue("an unusable APK must never cost the device its web OTA", steps.appliesWeb())
    }

    // ==================== web version baseline ====================

    /** Web OTA versions are 4-segment ({pkg}.{epoch-seconds}); a fresh install
     *  compares the remote against the APK version. */
    @Test
    fun four_segment_web_version_beats_bare_app_version() {
        assertTrue(plan(webM = { web(version = "0.4.8.20000000") }, localWebVersion = "0.4.8").appliesWeb())
    }

    @Test
    fun older_build_number_is_not_applied() {
        val steps = plan(
            webM = { web(version = "0.4.8.19999999") },
            localWebVersion = "0.4.8.20000000",
        )
        assertTrue(!steps.appliesWeb())
    }

    // ==================== forceDowngrade (beta -> stable) ====================

    @Test
    fun force_downgrade_offers_differing_stable_to_beta_build() {
        val steps = plan(
            native = { nativeUp.copy(version = "0.4.8") },
            appVersion = "0.4.8-beta.3",
            forceDowngrade = true,
        )
        assertTrue(steps.notifiesNative())
    }

    @Test
    fun force_downgrade_is_noop_when_versions_match() {
        val steps = plan(
            native = { nativeUp.copy(version = "0.4.8-beta.3") },
            appVersion = "0.4.8-beta.3",
            forceDowngrade = true,
        )
        assertTrue(!steps.notifiesNative())
    }

    @Test
    fun force_downgrade_on_stable_build_uses_normal_rule() {
        // Not a beta build: a lower remote version must not be offered.
        val steps = plan(
            native = { nativeUp.copy(version = "0.4.7") },
            appVersion = "0.4.8",
            forceDowngrade = true,
        )
        assertTrue(!steps.notifiesNative())
    }

    /** forceDowngrade drives the native lane only; the web lane keeps its
     *  strictly-newer rule (unchanged from before the decoupling). */
    @Test
    fun force_downgrade_does_not_downgrade_web_bundle() {
        val steps = plan(
            webM = { web(version = "0.4.8.10000000") },
            localWebVersion = "0.4.8.20000000",
            appVersion = "0.4.8-beta.3",
            forceDowngrade = true,
        )
        assertTrue(!steps.appliesWeb())
    }

    // ==================== payload plumbing ====================

    @Test
    fun notify_native_carries_version_and_resolved_url() {
        val step = plan().filterIsInstance<AutoUpdateStep.NotifyNative>().single()
        assertEquals("0.4.9", step.version)
        assertEquals("https://cdn/0.4.9/Kaitu-0.4.9.apk", step.url)
    }

    @Test
    fun apply_web_carries_hash_and_sig_for_verification() {
        val m = web().copy(hash = "sha256:abc", sig = "SIGBASE64")
        val step = plan(webM = { m }).filterIsInstance<AutoUpdateStep.ApplyWeb>().single()
        assertEquals("sha256:abc", step.manifest.hash)
        assertEquals("SIGBASE64", step.manifest.sig)
    }

    // ==================== quarantine: a bad bundle must not come back ====

    /**
     * Without this gate the shell is a reinstall treadmill. Rollback (K2Plugin
     * .load()) deletes web-update/ including version.txt, so localWebVersion
     * falls back to appVersion and the SAME bad bundle is "newer" again;
     * fetchManifest has no cache or backoff, so the 3s post-boot check
     * re-downloads it within seconds. Every cold start: white screen, roll
     * back, re-download. The desktop shell blocks this with
     * quarantined-version.txt (web_ota.rs evaluate_manifest); this is the
     * mobile half of the same invariant.
     */
    @Test
    fun quarantined_web_version_is_not_reapplied() {
        val steps = plan(
            webM = { web(version = "0.4.8.20000000") },
            quarantinedWebVersion = "0.4.8.20000000",
        )
        assertTrue("a version that already failed to boot must not be re-applied", !steps.appliesWeb())
        assertEquals("version quarantined", steps.skipReason("web"))
    }

    /** The block is version-scoped, not permanent: publishing a newer bundle
     *  is how the CDN clears it (desktop F2 semantics). */
    @Test
    fun newer_web_version_clears_the_quarantine() {
        val steps = plan(
            webM = { web(version = "0.4.8.20000001") },
            quarantinedWebVersion = "0.4.8.20000000",
        )
        assertTrue("a newer bundle must still be applied", steps.appliesWeb())
    }

    /** Quarantine is a web-lane concept — the APK banner is unaffected. */
    @Test
    fun quarantine_does_not_suppress_the_native_lane() {
        val steps = plan(quarantinedWebVersion = "0.4.8.20000000")
        assertTrue(steps.notifiesNative())
    }
}
