package io.kaitu.k2plugin

/**
 * Pure decision core for the cold-start web-OTA boot check.
 *
 * WHY THIS FILE EXISTS — the "boot verified" ≠ "UI rendered" invariant
 * ===================================================================
 * Until 2026-08 the shell cleared `.boot-pending` inside `checkReady()`, which
 * the webapp calls from `injectCapacitorGlobals()` — i.e. BEFORE store init and
 * before the first React render. So a bundle that loaded its JS and then died
 * during either stage still reported "boot verified": the marker was gone, the
 * next cold start saw a clean bundle, and the user got the same white screen
 * forever. The desktop shell hit exactly this (2026-08-18, `/index.html`
 * matching no route → empty tree → `ui_boot_ok` fired anyway) and moved its
 * handshake after `ReactDOM.render`. Mobile kept the old shape, where it is
 * strictly worse: there is no `?ui=embedded` escape hatch and no hot-fix path
 * — a defeated rollback means a new store release.
 *
 * The handshake is now `confirmWebBootOk()`, called from main.tsx only after
 * the app has rendered. `checkReady()` no longer touches the marker.
 *
 * The second half is quarantine. A [Rollback] must carry the version it is
 * discarding, because rolling back deletes web-update/ — version.txt included.
 * Drop it here and the caller has nothing to quarantine, so the auto-update
 * check 3 seconds later re-downloads the same broken bundle (fetchManifest has
 * neither cache nor backoff) and every cold start repeats the cycle.
 *
 * Free of Android APIs on purpose — see [planAutoUpdate] for the same rationale.
 * The caller does the I/O; this file only decides.
 */
internal sealed class WebBootDecision {
    /** Serve the OTA bundle from disk. Caller arms `.boot-pending` first. */
    object ServeDisk : WebBootDecision()

    /**
     * Last boot never confirmed it rendered. Delete the bundle, fall back to
     * the bundled webapp, and quarantine [version] so it is not re-applied.
     * [version] is null when version.txt was missing or unreadable.
     */
    data class Rollback(val version: String?) : WebBootDecision()

    /** Bundle directory present but has no index.html — remove it. */
    object CleanCorrupt : WebBootDecision()

    /** Nothing usable on disk; serve the webapp bundled in the APK. */
    object ServeBundled : WebBootDecision()
}

/**
 * @param hasWebUpdateDir whether `filesDir/web-update/` exists.
 * @param hasBootPending  whether `web-update/.boot-pending` exists — set at the
 *   previous boot and cleared only by `confirmWebBootOk()` after render.
 * @param hasIndex        whether `web-update/index.html` exists.
 * @param diskVersion     contents of `web-update/version.txt`, or null.
 */
internal fun decideWebBoot(
    hasWebUpdateDir: Boolean,
    hasBootPending: Boolean,
    hasIndex: Boolean,
    diskVersion: String?,
): WebBootDecision = when {
    !hasWebUpdateDir -> WebBootDecision.ServeBundled
    // Checked before the corrupt case on purpose: an unconfirmed boot is a
    // FAILURE and must be quarantined, whereas CleanCorrupt merely tidies up.
    // Reordering these silently drops the quarantine for half the failures.
    hasBootPending -> WebBootDecision.Rollback(diskVersion)
    hasIndex -> WebBootDecision.ServeDisk
    else -> WebBootDecision.CleanCorrupt
}
