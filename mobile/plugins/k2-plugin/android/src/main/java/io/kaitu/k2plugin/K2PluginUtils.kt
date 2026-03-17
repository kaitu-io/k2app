package io.kaitu.k2plugin

import java.security.MessageDigest

internal object K2PluginUtils {

    private const val CDN_PRIMARY = "https://d13jc1jqzlg4yt.cloudfront.net/kaitu"
    private const val CDN_FALLBACK = "https://d0.all7.cc/kaitu"

    fun isNewerVersion(remote: String, local: String): Boolean {
        val (rBase, rPre) = splitVersion(remote)
        val (lBase, lPre) = splitVersion(local)
        val baseCmp = compareSegments(rBase, lBase)
        if (baseCmp != 0) return baseCmp > 0
        if (rPre == null && lPre != null) return true
        if (rPre != null && lPre == null) return false
        if (rPre == null && lPre == null) return false
        return compareSegments(
            rPre!!.split(".").map { it.toIntOrNull() ?: 0 },
            lPre!!.split(".").map { it.toIntOrNull() ?: 0 }
        ) > 0
    }

    /**
     * Check if the current native app version meets the minimum required by a webapp.
     * Compares BASE versions only (ignores pre-release suffix).
     * 0.4.0-beta.6 satisfies min_native=0.4.0 because base(0.4.0-beta.6) == 0.4.0.
     * Returns true if minNative is null or empty (backwards compat — old manifests without field).
     */
    fun isCompatibleNativeVersion(minNative: String?, appVersion: String): Boolean {
        if (minNative.isNullOrBlank()) return true
        val (minBase, _) = splitVersion(minNative)
        val (appBase, _) = splitVersion(appVersion)
        return compareSegments(appBase, minBase) >= 0
    }

    internal fun splitVersion(v: String): Pair<List<Int>, String?> {
        val parts = v.split("-", limit = 2)
        val base = parts[0].split(".").map { it.toIntOrNull() ?: 0 }
        val pre = if (parts.size > 1) parts[1] else null
        return Pair(base, pre)
    }

    internal fun compareSegments(a: List<Int>, b: List<Int>): Int {
        val maxLen = maxOf(a.size, b.size)
        for (i in 0 until maxLen) {
            val av = a.getOrElse(i) { 0 }
            val bv = b.getOrElse(i) { 0 }
            if (av != bv) return av.compareTo(bv)
        }
        return 0
    }

    fun androidManifestEndpoints(channel: String): List<String> {
        val prefix = if (channel == "beta") "beta/" else ""
        return listOf(
            "$CDN_PRIMARY/android/${prefix}latest.json",
            "$CDN_FALLBACK/android/${prefix}latest.json"
        )
    }

    fun webManifestEndpoints(channel: String): List<String> {
        val prefix = if (channel == "beta") "beta/" else ""
        return listOf(
            "$CDN_PRIMARY/web/${prefix}latest.json",
            "$CDN_FALLBACK/web/${prefix}latest.json"
        )
    }

    fun resolveDownloadURL(url: String, baseURL: String): String {
        if (url.startsWith("http://") || url.startsWith("https://")) {
            return url
        }
        return "$baseURL/$url"
    }

    fun sha256(data: ByteArray): String {
        val digest = MessageDigest.getInstance("SHA-256")
        return digest.digest(data).joinToString("") { "%02x".format(it) }
    }

    /** SHA-256 hash a raw platform ID to 32 lowercase hex chars (128 bit). */
    fun hashToUdid(raw: String): String {
        return sha256(raw.toByteArray(Charsets.UTF_8)).substring(0, 32)
    }
}
