package io.kaitu

import org.junit.Assert.assertEquals
import org.junit.Assert.assertNotNull
import org.junit.Assert.assertNull
import org.junit.Test

class K2VpnServiceUtilsTest {

    // ==================== parseCIDR ====================

    @Test
    fun parseCIDR_ipv4_valid() {
        val result = K2VpnServiceUtils.parseCIDR("10.0.0.2/24")
        assertNotNull(result)
        assertEquals("10.0.0.2", result!!.first)
        assertEquals(24, result.second)
    }

    @Test
    fun parseCIDR_ipv6_valid() {
        val result = K2VpnServiceUtils.parseCIDR("fdfe:dcba:9876::7/64")
        assertNotNull(result)
        assertEquals("fdfe:dcba:9876::7", result!!.first)
        assertEquals(64, result.second)
    }

    @Test
    fun parseCIDR_no_slash_returns_null() {
        assertNull(K2VpnServiceUtils.parseCIDR("10.0.0.2"))
    }

    @Test
    fun parseCIDR_non_numeric_prefix_returns_null() {
        assertNull(K2VpnServiceUtils.parseCIDR("10.0.0.2/abc"))
    }

    @Test
    fun parseCIDR_empty_returns_null() {
        assertNull(K2VpnServiceUtils.parseCIDR(""))
    }

    // ==================== stripPort ====================

    @Test
    fun stripPort_ipv4_with_port() {
        assertEquals("8.8.8.8", K2VpnServiceUtils.stripPort("8.8.8.8:53"))
    }

    @Test
    fun stripPort_ipv6_bracketed_with_port() {
        assertEquals("::1", K2VpnServiceUtils.stripPort("[::1]:53"))
    }

    @Test
    fun stripPort_ipv4_no_port() {
        assertEquals("8.8.8.8", K2VpnServiceUtils.stripPort("8.8.8.8"))
    }

    @Test
    fun stripPort_bare_ipv6_passthrough() {
        // Bare IPv6 "::1" has multiple colons — parts.size > 2, returns as-is
        assertEquals("::1", K2VpnServiceUtils.stripPort("::1"))
    }

    @Test
    fun stripPort_bracketed_no_port() {
        assertEquals("::1", K2VpnServiceUtils.stripPort("[::1]"))
    }

    // ==================== parseClientConfig ====================

    @Test
    fun parseClientConfig_full_config() {
        val json = """
            {
                "tun": {"ipv4": "198.18.0.7/15", "ipv6": "fdfe:dcba:9876::7/64"},
                "dns": {"proxy": ["8.8.8.8:53", "1.1.1.1:53"]}
            }
        """.trimIndent()
        val result = K2VpnServiceUtils.parseClientConfig(json)
        assertNotNull(result)
        assertEquals("198.18.0.7/15", result!!.tunIpv4)
        assertEquals("fdfe:dcba:9876::7/64", result.tunIpv6)
        assertEquals(listOf("8.8.8.8:53", "1.1.1.1:53"), result.dnsProxy)
    }

    @Test
    fun parseClientConfig_missing_tun() {
        val json = """{"dns": {"proxy": ["8.8.8.8:53"]}}"""
        val result = K2VpnServiceUtils.parseClientConfig(json)
        assertNotNull(result)
        assertNull(result!!.tunIpv4)
        assertNull(result.tunIpv6)
        assertEquals(listOf("8.8.8.8:53"), result.dnsProxy)
    }

    @Test
    fun parseClientConfig_missing_dns() {
        val json = """{"tun": {"ipv4": "10.0.0.2/24"}}"""
        val result = K2VpnServiceUtils.parseClientConfig(json)
        assertNotNull(result)
        assertEquals("10.0.0.2/24", result!!.tunIpv4)
        assertNull(result.dnsProxy)
    }

    @Test
    fun parseClientConfig_empty_json() {
        val result = K2VpnServiceUtils.parseClientConfig("{}")
        assertNotNull(result)
        assertNull(result!!.tunIpv4)
        assertNull(result.tunIpv6)
        assertNull(result.dnsProxy)
    }

    @Test
    fun parseClientConfig_invalid_json_returns_null() {
        assertNull(K2VpnServiceUtils.parseClientConfig("not json"))
    }

    @Test
    fun parseClientConfig_dns_proxy_with_ports() {
        val json = """{"dns": {"proxy": ["[::1]:53", "114.114.114.114:53"]}}"""
        val result = K2VpnServiceUtils.parseClientConfig(json)
        assertNotNull(result)
        assertEquals(listOf("[::1]:53", "114.114.114.114:53"), result!!.dnsProxy)
    }
}
