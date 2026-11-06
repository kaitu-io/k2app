package io.kaitu.k2plugin

import org.junit.Assert.assertEquals
import org.junit.Test

/**
 * Cold-start decision for the web-OTA bundle on disk.
 *
 * Extracted from K2Plugin.load() for the same reason AutoUpdatePlan was: this
 * module has no Robolectric, so anything touching File/Context is untestable.
 * The decision is what carries the safety properties — the shell only does the
 * I/O it is told to do.
 *
 * The property this file exists to pin: a rollback must NAME the version it is
 * rolling back, so the caller can quarantine it. Rollback deletes web-update/
 * along with version.txt, so if the version isn't captured in the decision it
 * is gone forever and the same bad bundle gets re-downloaded minutes later.
 */
class WebBootDecisionTest {

    @Test
    fun unconfirmed_boot_rolls_back_and_names_the_version_to_quarantine() {
        val d = decideWebBoot(
            hasWebUpdateDir = true,
            hasBootPending = true,
            hasIndex = true,
            diskVersion = "0.4.8.20000000",
            appVersion = "0.4.8",
        )
        assertEquals(WebBootDecision.Rollback("0.4.8.20000000"), d)
    }

    /** A rollback with no readable version.txt still rolls back — it just has
     *  nothing to quarantine. Must not throw, must not serve the bundle. */
    @Test
    fun rollback_without_a_readable_version_still_rolls_back() {
        val d = decideWebBoot(
            hasWebUpdateDir = true,
            hasBootPending = true,
            hasIndex = true,
            diskVersion = null,
            appVersion = "0.4.8",
        )
        assertEquals(WebBootDecision.Rollback(null), d)
    }

    /**
     * A bundle whose index.html is gone AND whose boot was never confirmed is
     * still a rollback, not a plain cleanup: it failed, so it must be
     * quarantined. Ordering the corrupt-check first would silently drop the
     * quarantine and let the bad version return.
     */
    @Test
    fun unconfirmed_boot_wins_over_the_corrupt_check() {
        val d = decideWebBoot(
            hasWebUpdateDir = true,
            hasBootPending = true,
            hasIndex = false,
            diskVersion = "0.4.8.20000000",
            appVersion = "0.4.8",
        )
        assertEquals(WebBootDecision.Rollback("0.4.8.20000000"), d)
    }

    @Test
    fun confirmed_bundle_is_served_from_disk() {
        val d = decideWebBoot(
            hasWebUpdateDir = true,
            hasBootPending = false,
            hasIndex = true,
            diskVersion = "0.4.8.20000000",
            appVersion = "0.4.8",
        )
        assertEquals(WebBootDecision.ServeDisk, d)
    }

    @Test
    fun directory_without_index_is_cleaned_up() {
        val d = decideWebBoot(
            hasWebUpdateDir = true,
            hasBootPending = false,
            hasIndex = false,
            diskVersion = null,
            appVersion = "0.4.8",
        )
        assertEquals(WebBootDecision.CleanCorrupt, d)
    }

    @Test
    fun no_bundle_on_disk_serves_the_bundled_webapp() {
        val d = decideWebBoot(
            hasWebUpdateDir = false,
            hasBootPending = false,
            hasIndex = false,
            diskVersion = null,
            appVersion = "0.4.8",
        )
        assertEquals(WebBootDecision.ServeBundled, d)
    }

    /** A stray marker with no bundle directory is not a rollback — there is
     *  nothing to roll back to or quarantine. */
    @Test
    fun stray_marker_without_a_bundle_serves_the_bundled_webapp() {
        val d = decideWebBoot(
            hasWebUpdateDir = false,
            hasBootPending = true,
            hasIndex = false,
            diskVersion = null,
            appVersion = "0.4.8",
        )
        assertEquals(WebBootDecision.ServeBundled, d)
    }

    /**
     * An APK upgrade must not keep serving an older cached OTA bundle: 0.4.16
     * shell + 0.4.15.x disk bundle → discard and serve the bundled webapp
     * (the bundled one carries the newer bridge/engine wiring, e.g. fallbacks).
     */
    @Test
    fun shell_newer_than_disk_bundle_discards_it() {
        val d = decideWebBoot(
            hasWebUpdateDir = true,
            hasBootPending = false,
            hasIndex = true,
            diskVersion = "0.4.15.26499202",
            appVersion = "0.4.16",
        )
        assertEquals(WebBootDecision.DiscardStale("0.4.15.26499202"), d)
    }

    /** An OTA built from the same release (x.y.z.<build>) is newer than the shell — serve it. */
    @Test
    fun ota_of_the_same_release_is_served() {
        val d = decideWebBoot(
            hasWebUpdateDir = true,
            hasBootPending = false,
            hasIndex = true,
            diskVersion = "0.4.16.26600000",
            appVersion = "0.4.16",
        )
        assertEquals(WebBootDecision.ServeDisk, d)
    }

    /** Unconfirmed boot still wins: a stale bundle that failed must be quarantined. */
    @Test
    fun unconfirmed_boot_wins_over_stale_discard() {
        val d = decideWebBoot(
            hasWebUpdateDir = true,
            hasBootPending = true,
            hasIndex = true,
            diskVersion = "0.4.15.26499202",
            appVersion = "0.4.16",
        )
        assertEquals(WebBootDecision.Rollback("0.4.15.26499202"), d)
    }

    /** Never discard what we cannot date: unreadable version.txt or app version. */
    @Test
    fun undatable_bundle_or_unknown_app_version_is_not_discarded() {
        assertEquals(WebBootDecision.ServeDisk, decideWebBoot(true, false, true, null, "0.4.16"))
        assertEquals(WebBootDecision.ServeDisk, decideWebBoot(true, false, true, "0.4.15.1", null))
    }
}
