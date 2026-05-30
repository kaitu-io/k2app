package io.kaitu

import org.json.JSONObject

internal data class ParsedClientConfig(
    val tunIpv4: String?,
    val tunIpv6: String?,
    val dnsProxy: List<String>?
)

internal object K2VpnServiceUtils {

    /** Parse CIDR "10.0.0.2/24" → Pair("10.0.0.2", 24), null on failure. */
    fun parseCIDR(cidr: String): Pair<String, Int>? {
        val parts = cidr.split("/", limit = 2)
        if (parts.size != 2) return null
        val prefix = parts[1].toIntOrNull() ?: return null
        return Pair(parts[0], prefix)
    }

    /** Strip port from "8.8.8.8:53" → "8.8.8.8". Handles IPv6 "[::1]:53" → "::1". */
    fun stripPort(addr: String): String {
        if (addr.startsWith("[")) {
            val close = addr.indexOf(']')
            return if (close > 0) addr.substring(1, close) else addr
        }
        val parts = addr.split(":")
        return if (parts.size == 2) parts[0] else addr // "ip:port" or bare IP/IPv6
    }

    /** Extract tun + dns fields from ClientConfig JSON. Null on parse failure. */
    fun parseClientConfig(configJSON: String): ParsedClientConfig? {
        return try {
            val root = JSONObject(configJSON)
            val tun = root.optJSONObject("tun")
            val dns = root.optJSONObject("dns")
            val dnsProxy = dns?.optJSONArray("proxy")?.let { arr ->
                (0 until arr.length()).map { arr.getString(it) }
            }
            ParsedClientConfig(
                tunIpv4 = tun?.optString("ipv4", "")?.ifEmpty { null },
                tunIpv6 = tun?.optString("ipv6", "")?.ifEmpty { null },
                dnsProxy = dnsProxy
            )
        } catch (e: Exception) {
            null
        }
    }

    /**
     * Extracts the App Bypass package list from ClientConfig JSON for
     * kernel-level exclusion via VpnService.Builder.addDisallowedApplication.
     *
     * Consumes any route whose via == "direct" and match.apps is a
     * non-empty array (the Plan B/C unified per-app override field — on Android
     * each entry is a package name). Deduplicates (LinkedHashSet preserves first-seen order),
     * trims whitespace, drops empty strings, and excludes selfPackage (already
     * added by the caller). Returns empty list on parse failure — bypass is
     * advisory; engine still operates without it.
     */
    fun parseDisallowedPackages(configJSON: String, selfPackage: String): List<String> {
        return try {
            val routes = JSONObject(configJSON).optJSONArray("routes") ?: return emptyList()
            val result = LinkedHashSet<String>()
            for (i in 0 until routes.length()) {
                val route = routes.optJSONObject(i) ?: continue
                if (route.optString("via") != "direct") continue
                val pkgs = route.optJSONObject("match")?.optJSONArray("apps") ?: continue
                for (j in 0 until pkgs.length()) {
                    val pkg = pkgs.optString(j).trim()
                    if (pkg.isNotEmpty() && pkg != selfPackage) {
                        result.add(pkg)
                    }
                }
            }
            result.toList()
        } catch (e: Exception) {
            emptyList()
        }
    }
}
