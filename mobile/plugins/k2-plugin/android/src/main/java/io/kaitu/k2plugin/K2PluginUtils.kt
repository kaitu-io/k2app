package io.kaitu.k2plugin

import android.content.Context
import java.security.MessageDigest

internal object K2PluginUtils {

    /**
     * Brand CDN base URLs. The plugin module has no flavors of its own — brand
     * comes from the host app's per-flavor `brand.xml` resources (`k2_cdn_primary`
     * / `k2_cdn_fallback`), resolved by name at runtime so this module carries
     * zero brand literals.
     */
    fun cdnPrimary(context: Context): String = brandString(context, "k2_cdn_primary")
    fun cdnFallback(context: Context): String = brandString(context, "k2_cdn_fallback")

    private fun brandString(context: Context, name: String): String {
        val id = context.resources.getIdentifier(name, "string", context.packageName)
        require(id != 0) { "host app missing brand string resource: $name" }
        return context.getString(id)
    }

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

    fun androidManifestEndpoints(channel: String, cdnPrimary: String, cdnFallback: String): List<String> {
        val prefix = if (channel == "beta") "beta/" else ""
        return listOf(
            "$cdnPrimary/android/${prefix}latest.json",
            "$cdnFallback/android/${prefix}latest.json"
        )
    }

    fun webManifestEndpoints(channel: String, cdnPrimary: String, cdnFallback: String): List<String> {
        val prefix = if (channel == "beta") "beta/" else ""
        return listOf(
            "$cdnPrimary/web/${prefix}latest.json",
            "$cdnFallback/web/${prefix}latest.json"
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
