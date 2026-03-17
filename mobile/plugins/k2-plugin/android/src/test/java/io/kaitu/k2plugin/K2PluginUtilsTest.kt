package io.kaitu.k2plugin

import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

class K2PluginUtilsTest {

    // ==================== isNewerVersion ====================

    @Test
    fun isNewerVersion_major_bump() {
        assertTrue(K2PluginUtils.isNewerVersion("1.1.0", "1.0.0"))
    }

    @Test
    fun isNewerVersion_equal_returns_false() {
        assertFalse(K2PluginUtils.isNewerVersion("1.0.0", "1.0.0"))
    }

    @Test
    fun isNewerVersion_older_returns_false() {
        assertFalse(K2PluginUtils.isNewerVersion("0.9.0", "1.0.0"))
    }

    @Test
    fun isNewerVersion_patch_bump() {
        assertTrue(K2PluginUtils.isNewerVersion("1.0.1", "1.0.0"))
    }

    @Test
    fun isNewerVersion_fewer_segments_equal() {
        // "1.0" == "1.0.0" (missing segment treated as 0)
        assertFalse(K2PluginUtils.isNewerVersion("1.0", "1.0.0"))
    }

    @Test
    fun isNewerVersion_extra_segment() {
        // "1.0.0.1" > "1.0.0" (extra segment > 0)
        assertTrue(K2PluginUtils.isNewerVersion("1.0.0.1", "1.0.0"))
    }

    @Test
    fun isNewerVersion_non_numeric_treated_as_zero() {
        // "abc" → [0], "1.0.0" → [1,0,0] → 0 < 1 → false
        assertFalse(K2PluginUtils.isNewerVersion("abc", "1.0.0"))
    }

    // ==================== isNewerVersion beta suffix ====================

    @Test
    fun isNewerVersion_stable_greater_than_same_beta() {
        assertTrue(K2PluginUtils.isNewerVersion("0.5.0", "0.5.0-beta.1"))
    }

    @Test
    fun isNewerVersion_beta_less_than_same_stable() {
        assertFalse(K2PluginUtils.isNewerVersion("0.5.0-beta.1", "0.5.0"))
    }

    @Test
    fun isNewerVersion_beta_increment() {
        assertTrue(K2PluginUtils.isNewerVersion("0.5.0-beta.2", "0.5.0-beta.1"))
    }

    @Test
    fun isNewerVersion_same_beta_equal() {
        assertFalse(K2PluginUtils.isNewerVersion("0.5.0-beta.1", "0.5.0-beta.1"))
    }

    @Test
    fun isNewerVersion_cross_version_beta_greater() {
        assertTrue(K2PluginUtils.isNewerVersion("0.5.0-beta.1", "0.4.0"))
    }

    @Test
    fun isNewerVersion_new_stable_greater_than_old_beta() {
        assertTrue(K2PluginUtils.isNewerVersion("0.6.0", "0.5.0-beta.1"))
    }

    // ==================== manifestEndpoints ====================

    @Test
    fun androidManifestEndpoints_stable() {
        val endpoints = K2PluginUtils.androidManifestEndpoints("stable")
        assertTrue(endpoints[0].endsWith("/android/latest.json"))
        assertFalse(endpoints[0].contains("/beta/"))
    }

    @Test
    fun androidManifestEndpoints_beta() {
        val endpoints = K2PluginUtils.androidManifestEndpoints("beta")
        assertTrue(endpoints[0].endsWith("/android/beta/latest.json"))
    }

    @Test
    fun webManifestEndpoints_stable() {
        val endpoints = K2PluginUtils.webManifestEndpoints("stable")
        assertTrue(endpoints[0].endsWith("/web/latest.json"))
        assertFalse(endpoints[0].contains("/beta/"))
    }

    @Test
    fun webManifestEndpoints_beta() {
        val endpoints = K2PluginUtils.webManifestEndpoints("beta")
        assertTrue(endpoints[0].endsWith("/web/beta/latest.json"))
    }

    // ==================== resolveDownloadURL ====================

    @Test
    fun resolveDownloadURL_relative_path() {
        assertEquals(
            "https://cdn.example.com/Kaitu-1.0.0.apk",
            K2PluginUtils.resolveDownloadURL("Kaitu-1.0.0.apk", "https://cdn.example.com")
        )
    }

    @Test
    fun resolveDownloadURL_absolute_https_passthrough() {
        assertEquals(
            "https://other.com/file.apk",
            K2PluginUtils.resolveDownloadURL("https://other.com/file.apk", "https://cdn.example.com")
        )
    }

    @Test
    fun resolveDownloadURL_absolute_http_passthrough() {
        assertEquals(
            "http://other.com/file.apk",
            K2PluginUtils.resolveDownloadURL("http://other.com/file.apk", "https://cdn.example.com")
        )
    }

    @Test
    fun resolveDownloadURL_stable_relative() {
        // Stable: manifest at /android/latest.json, APK at /android/VERSION/
        assertEquals(
            "https://cdn.example.com/kaitu/android/0.5.0/Kaitu-0.5.0.apk",
            K2PluginUtils.resolveDownloadURL(
                "0.5.0/Kaitu-0.5.0.apk",
                "https://cdn.example.com/kaitu/android"
            )
        )
    }

    @Test
    fun resolveDownloadURL_beta_relative() {
        // Beta: manifest at /android/beta/latest.json, APK at /android/beta/VERSION/
        assertEquals(
            "https://cdn.example.com/kaitu/android/beta/0.5.0-beta.1/Kaitu-0.5.0-beta.1.apk",
            K2PluginUtils.resolveDownloadURL(
                "0.5.0-beta.1/Kaitu-0.5.0-beta.1.apk",
                "https://cdn.example.com/kaitu/android/beta"
            )
        )
    }

    // ==================== sha256 ====================

    @Test
    fun sha256_hello() {
        assertEquals(
            "2cf24dba5fb0a30e26e83b2ac5b9e29e1b161e5c1fa7425e73043362938b9824",
            K2PluginUtils.sha256("hello".toByteArray())
        )
    }

    @Test
    fun sha256_empty() {
        assertEquals(
            "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855",
            K2PluginUtils.sha256(ByteArray(0))
        )
    }
}
