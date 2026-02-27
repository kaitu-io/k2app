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
}
