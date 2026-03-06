package io.kaitu.k2plugin

import java.security.MessageDigest

internal object K2PluginUtils {

    fun isNewerVersion(remote: String, local: String): Boolean {
        val r = remote.split(".").map { it.toIntOrNull() ?: 0 }
        val l = local.split(".").map { it.toIntOrNull() ?: 0 }
        val maxLen = maxOf(r.size, l.size)
        for (i in 0 until maxLen) {
            val rv = r.getOrElse(i) { 0 }
            val lv = l.getOrElse(i) { 0 }
            if (rv > lv) return true
            if (rv < lv) return false
        }
        return false
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
