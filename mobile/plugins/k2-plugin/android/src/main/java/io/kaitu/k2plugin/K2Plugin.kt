package io.kaitu.k2plugin

import android.app.Activity
import android.content.ComponentName
import android.content.Context
import android.content.Intent
import android.content.ServiceConnection
import android.net.VpnService
import android.os.Build
import android.os.Handler
import android.os.IBinder
import android.os.Looper
import android.provider.Settings
import android.util.Log
import androidx.activity.result.ActivityResult
import androidx.core.content.FileProvider
import com.getcapacitor.JSObject
import com.getcapacitor.Plugin
import com.getcapacitor.PluginCall
import com.getcapacitor.PluginMethod
import com.getcapacitor.annotation.ActivityCallback
import com.getcapacitor.annotation.CapacitorPlugin
import org.json.JSONObject
import java.io.File
import java.net.HttpURLConnection
import java.net.URL
import java.security.MessageDigest
import java.util.zip.ZipInputStream

@CapacitorPlugin(name = "K2Plugin")
class K2Plugin : Plugin() {

    companion object {
        private const val TAG = "K2Plugin"
        private val WEB_MANIFEST_ENDPOINTS = listOf(
            "https://d13jc1jqzlg4yt.cloudfront.net/kaitu/web/latest.json",
            "https://d0.all7.cc/kaitu/web/latest.json"
        )
        private val ANDROID_MANIFEST_ENDPOINTS = listOf(
            "https://d13jc1jqzlg4yt.cloudfront.net/kaitu/android/latest.json",
            "https://d0.all7.cc/kaitu/android/latest.json"
        )
    }

    private var vpnService: VpnServiceBridge? = null
    private var serviceConnection: ServiceConnection? = null
    private val vpnServiceClassName = "io.kaitu.K2VpnService"

    override fun load() {
        // Check for OTA web update
        val webUpdateDir = File(context.filesDir, "web-update")
        val indexFile = File(webUpdateDir, "index.html")
        if (webUpdateDir.exists() && indexFile.exists()) {
            bridge.setServerBasePath(webUpdateDir.absolutePath)
        } else if (webUpdateDir.exists()) {
            // Corrupt OTA dir, remove it
            webUpdateDir.deleteRecursively()
        }

        bindToService()

        // Auto-check for updates after 3s delay
        Handler(Looper.getMainLooper()).postDelayed({
            Thread { performAutoUpdateCheck() }.start()
        }, 3000)
    }

    override fun handleOnDestroy() {
        serviceConnection?.let {
            try { context.unbindService(it) } catch (_: Exception) {}
        }
        serviceConnection = null
        super.handleOnDestroy()
    }

    @PluginMethod
    fun checkReady(call: PluginCall) {
        val version = context.packageManager.getPackageInfo(context.packageName, 0).versionName ?: "unknown"
        val ret = JSObject()
        ret.put("ready", true)
        ret.put("version", version)
        call.resolve(ret)
    }

    @PluginMethod
    fun getUDID(call: PluginCall) {
        val udid = Settings.Secure.getString(context.contentResolver, Settings.Secure.ANDROID_ID)
        val ret = JSObject()
        ret.put("udid", udid)
        call.resolve(ret)
    }

    @PluginMethod
    fun getVersion(call: PluginCall) {
        val version = context.packageManager.getPackageInfo(context.packageName, 0).versionName ?: "unknown"
        val ret = JSObject()
        ret.put("version", version)
        ret.put("go", "embedded")
        ret.put("os", "android")
        ret.put("arch", android.os.Build.SUPPORTED_ABIS.firstOrNull() ?: "unknown")
        call.resolve(ret)
    }

    @PluginMethod
    fun getStatus(call: PluginCall) {
        val service = vpnService
        if (service != null) {
            val json = service.getStatusJSON()
            try {
                val obj = JSObject(json)
                call.resolve(remapStatusKeys(obj))
            } catch (e: Exception) {
                val ret = JSObject()
                ret.put("state", "disconnected")
                call.resolve(ret)
            }
        } else {
            val ret = JSObject()
            ret.put("state", "disconnected")
            call.resolve(ret)
        }
    }

    /** Remap Go StatusJSON snake_case keys to JS camelCase */
    private fun remapStatusKeys(obj: JSObject): JSObject {
        val keyMap = mapOf(
            "connected_at" to "connectedAt",
            "uptime_seconds" to "uptimeSeconds",
        )
        val result = JSObject()
        val keys = obj.keys()
        while (keys.hasNext()) {
            val key = keys.next()
            val newKey = keyMap[key] ?: key
            result.put(newKey, obj.get(key))
        }
        return result
    }

    @PluginMethod
    fun getConfig(call: PluginCall) {
        val prefs = context.getSharedPreferences("k2vpn", Context.MODE_PRIVATE)
        val config = prefs.getString("configJSON", "") ?: ""
        val ret = JSObject()
        ret.put("config", config)
        call.resolve(ret)
    }

    @PluginMethod
    fun connect(call: PluginCall) {
        val config = call.getString("config")
        if (config == null) {
            call.reject("Missing config parameter")
            return
        }

        // Save config JSON
        context.getSharedPreferences("k2vpn", Context.MODE_PRIVATE)
            .edit().putString("configJSON", config).apply()

        // Check VPN permission — Android requires user consent via VpnService.prepare()
        // Must use Activity context for proper VPN consent handling
        val act = activity
        if (act == null) {
            call.reject("No activity available for VPN permission")
            return
        }

        val prepareIntent = VpnService.prepare(act)
        if (prepareIntent != null) {
            // Permission not yet granted — show system VPN consent dialog
            startActivityForResult(call, prepareIntent, "vpnPermissionResult")
        } else {
            // Already authorized — start VPN directly
            startVpnService(config)
            call.resolve()
        }
    }

    @ActivityCallback
    private fun vpnPermissionResult(call: PluginCall, result: ActivityResult) {
        if (result.resultCode == Activity.RESULT_OK) {
            val config = call.getString("config")
            if (config != null) {
                startVpnService(config)
                call.resolve()
            } else {
                call.reject("config lost after VPN permission grant")
            }
        } else {
            call.reject("VPN permission denied by user")
        }
    }

    private fun startVpnService(configJSON: String) {
        val intent = Intent().apply {
            setClassName(context.packageName, vpnServiceClassName)
            action = "START"
            putExtra("configJSON", configJSON)
        }
        context.startForegroundService(intent)
    }

    @PluginMethod
    fun disconnect(call: PluginCall) {
        val intent = Intent().apply {
            setClassName(context.packageName, vpnServiceClassName)
            action = "STOP"
        }
        context.startService(intent)
        call.resolve()
    }

    // Called by K2VpnService when state changes
    fun onStateChange(state: String) {
        val data = JSObject()
        data.put("state", state)
        notifyListeners("vpnStateChange", data)
    }

    fun onError(message: String) {
        val data = JSObject()
        data.put("message", message)
        notifyListeners("vpnError", data)
    }

    // ── Update methods ──────────────────────────────────────────────

    @PluginMethod
    fun checkWebUpdate(call: PluginCall) {
        Thread {
            try {
                val result = fetchManifest(WEB_MANIFEST_ENDPOINTS)
                if (result == null) {
                    val ret = JSObject()
                    ret.put("available", false)
                    call.resolve(ret)
                    return@Thread
                }
                val (manifest, _) = result
                val remoteVersion = manifest.getString("version")
                val remoteSize = manifest.optLong("size", 0)

                // Read installed web version, fall back to app version
                val webVersionFile = File(File(context.filesDir, "web-update"), "version.txt")
                val localVersion = if (webVersionFile.exists()) {
                    webVersionFile.readText().trim()
                } else {
                    context.packageManager.getPackageInfo(context.packageName, 0).versionName ?: "0.0.0"
                }

                val ret = JSObject()
                if (isNewerVersion(remoteVersion, localVersion)) {
                    ret.put("available", true)
                    ret.put("version", remoteVersion)
                    ret.put("size", remoteSize)
                } else {
                    ret.put("available", false)
                }
                call.resolve(ret)
            } catch (e: Exception) {
                val ret = JSObject()
                ret.put("available", false)
                call.resolve(ret)
            }
        }.start()
    }

    @PluginMethod
    fun checkNativeUpdate(call: PluginCall) {
        Thread {
            try {
                val result = fetchManifest(ANDROID_MANIFEST_ENDPOINTS)
                if (result == null) {
                    val ret = JSObject()
                    ret.put("available", false)
                    call.resolve(ret)
                    return@Thread
                }
                val (manifest, baseURL) = result
                val remoteVersion = manifest.getString("version")
                val remoteSize = manifest.optLong("size", 0)
                val remoteUrl = resolveDownloadURL(manifest.getString("url"), baseURL)
                val minAndroid = manifest.optInt("min_android", 0)

                val localVersion = context.packageManager
                    .getPackageInfo(context.packageName, 0).versionName ?: "0.0.0"

                val ret = JSObject()
                if (isNewerVersion(remoteVersion, localVersion) && Build.VERSION.SDK_INT >= minAndroid) {
                    ret.put("available", true)
                    ret.put("version", remoteVersion)
                    ret.put("size", remoteSize)
                    ret.put("url", remoteUrl)
                } else {
                    ret.put("available", false)
                }
                call.resolve(ret)
            } catch (e: Exception) {
                val ret = JSObject()
                ret.put("available", false)
                call.resolve(ret)
            }
        }.start()
    }

    @PluginMethod
    fun applyWebUpdate(call: PluginCall) {
        Thread {
            val webUpdateDir = File(context.filesDir, "web-update")
            val webBackupDir = File(context.filesDir, "web-backup")
            try {
                // Fetch manifest to get URL and hash
                val result = fetchManifest(WEB_MANIFEST_ENDPOINTS)
                    ?: throw java.io.IOException("All web manifest endpoints failed")
                val (manifest, baseURL) = result
                val zipUrl = resolveDownloadURL(manifest.getString("url"), baseURL)
                val remoteVersion = manifest.getString("version")
                val rawHash = manifest.getString("hash")
                // Strip "sha256:" prefix if present
                val expectedHash = if (rawHash.startsWith("sha256:")) rawHash.removePrefix("sha256:") else rawHash

                // Download the zip
                val zipData = fetchUrl(zipUrl)

                // Verify SHA-256
                val actualHash = sha256(zipData)
                if (actualHash != expectedHash) {
                    // Restore backup on hash mismatch
                    if (webBackupDir.exists()) {
                        webBackupDir.renameTo(webUpdateDir)
                    }
                    call.reject("Hash mismatch: expected $expectedHash, got $actualHash")
                    return@Thread
                }

                // If existing web-update exists, move to web-backup
                if (webUpdateDir.exists()) {
                    webBackupDir.deleteRecursively()
                    webUpdateDir.renameTo(webBackupDir)
                }

                // Unzip to web-update/
                val tempZip = File(context.cacheDir, "webapp.zip")
                tempZip.writeBytes(zipData)
                try {
                    unzip(tempZip, webUpdateDir)
                } finally {
                    tempZip.delete()
                }

                // Flatten nested subdirectory if index.html not at root
                val indexFile = File(webUpdateDir, "index.html")
                if (!indexFile.exists()) {
                    val contents = webUpdateDir.listFiles() ?: emptyArray()
                    if (contents.size == 1 && contents[0].isDirectory) {
                        val subdir = contents[0]
                        subdir.listFiles()?.forEach { item ->
                            item.renameTo(File(webUpdateDir, item.name))
                        }
                        subdir.deleteRecursively()
                    }
                }

                // Write version for future comparison
                File(webUpdateDir, "version.txt").writeText(remoteVersion)

                call.resolve()
            } catch (e: Exception) {
                // Restore backup on failure
                webUpdateDir.deleteRecursively()
                if (webBackupDir.exists()) {
                    webBackupDir.renameTo(webUpdateDir)
                }
                call.reject("Failed to apply web update: ${e.message}", e)
            }
        }.start()
    }

    @PluginMethod
    fun downloadNativeUpdate(call: PluginCall) {
        Thread {
            try {
                // Fetch manifest to get APK URL
                val result = fetchManifest(ANDROID_MANIFEST_ENDPOINTS)
                    ?: throw java.io.IOException("All Android manifest endpoints failed")
                val (manifest, baseURL) = result
                val apkUrl = resolveDownloadURL(manifest.getString("url"), baseURL)
                val remoteVersion = manifest.getString("version")
                val totalSize = manifest.optLong("size", 0)

                // Cache by version to avoid re-downloading
                val apkFile = File(context.cacheDir, "update-$remoteVersion.apk")

                // Skip download if cached file exists and size matches
                if (apkFile.exists() && totalSize > 0 && apkFile.length() == totalSize) {
                    val ret = JSObject()
                    ret.put("path", apkFile.absolutePath)
                    call.resolve(ret)
                    return@Thread
                }

                val url = URL(apkUrl)
                val conn = url.openConnection() as HttpURLConnection
                conn.connectTimeout = 15000
                conn.readTimeout = 30000
                val code = conn.responseCode
                if (code != 200) {
                    throw java.io.IOException("HTTP $code from $apkUrl")
                }

                val contentLength = if (totalSize > 0) totalSize else conn.contentLengthLong

                conn.inputStream.use { input ->
                    apkFile.outputStream().use { output ->
                        val buffer = ByteArray(8192)
                        var bytesRead: Long = 0
                        var lastPercent = -1
                        var len: Int

                        while (input.read(buffer).also { len = it } != -1) {
                            output.write(buffer, 0, len)
                            bytesRead += len

                            if (contentLength > 0) {
                                val percent = ((bytesRead * 100) / contentLength).toInt()
                                if (percent != lastPercent) {
                                    lastPercent = percent
                                    val data = JSObject()
                                    data.put("percent", percent)
                                    notifyListeners("updateDownloadProgress", data)
                                }
                            }
                        }
                    }
                }

                val ret = JSObject()
                ret.put("path", apkFile.absolutePath)
                call.resolve(ret)
            } catch (e: Exception) {
                call.reject("Failed to download native update: ${e.message}", e)
            }
        }.start()
    }

    @PluginMethod
    fun installNativeUpdate(call: PluginCall) {
        val path = call.getString("path")
        if (path == null) {
            call.reject("Missing path parameter")
            return
        }

        try {
            val apkFile = File(path)
            if (!apkFile.exists()) {
                call.reject("APK file not found: $path")
                return
            }

            val uri = FileProvider.getUriForFile(
                context,
                "io.kaitu.fileprovider",
                apkFile
            )

            val intent = Intent(Intent.ACTION_INSTALL_PACKAGE).apply {
                setData(uri)
                addFlags(Intent.FLAG_GRANT_READ_URI_PERMISSION)
                addFlags(Intent.FLAG_ACTIVITY_NEW_TASK)
            }

            activity?.runOnUiThread {
                activity?.startActivity(intent)
            }

            call.resolve()
        } catch (e: Exception) {
            call.reject("Failed to install update: ${e.message}", e)
        }
    }

    // ── Auto-update check ───────────────────────────────────────────

    private fun performAutoUpdateCheck() {
        try {
            // 1. Check native update first
            val nativeResult = fetchManifest(ANDROID_MANIFEST_ENDPOINTS)
            if (nativeResult != null) {
                val (manifest, baseURL) = nativeResult
                val remoteVersion = manifest.getString("version")
                val totalSize = manifest.optLong("size", 0)
                val apkUrl = resolveDownloadURL(manifest.getString("url"), baseURL)
                val minAndroid = manifest.optInt("min_android", 0)
                val localVersion = context.packageManager
                    .getPackageInfo(context.packageName, 0).versionName ?: "0.0.0"

                if (isNewerVersion(remoteVersion, localVersion) && Build.VERSION.SDK_INT >= minAndroid) {
                    // Download APK in background
                    val apkFile = File(context.cacheDir, "update-$remoteVersion.apk")

                    // Skip download if cached file exists and size matches
                    val needsDownload = !(apkFile.exists() && totalSize > 0 && apkFile.length() == totalSize)

                    if (needsDownload) {
                        val url = URL(apkUrl)
                        val conn = url.openConnection() as HttpURLConnection
                        conn.connectTimeout = 15000
                        conn.readTimeout = 30000
                        val code = conn.responseCode
                        if (code != 200) {
                            throw java.io.IOException("HTTP $code from $apkUrl")
                        }

                        conn.inputStream.use { input ->
                            apkFile.outputStream().use { output ->
                                val buffer = ByteArray(8192)
                                var len: Int
                                while (input.read(buffer).also { len = it } != -1) {
                                    output.write(buffer, 0, len)
                                }
                            }
                        }
                    }

                    // Emit nativeUpdateReady event
                    val data = JSObject()
                    data.put("version", remoteVersion)
                    data.put("size", apkFile.length())
                    data.put("path", apkFile.absolutePath)
                    notifyListeners("nativeUpdateReady", data)
                    return
                }
            }

            // 2. No native update — check web OTA
            val webResult = fetchManifest(WEB_MANIFEST_ENDPOINTS)
            if (webResult != null) {
                val (manifest, baseURL) = webResult
                val remoteVersion = manifest.getString("version")
                val zipUrl = resolveDownloadURL(manifest.getString("url"), baseURL)
                val rawHash = manifest.getString("hash")
                val expectedHash = if (rawHash.startsWith("sha256:")) rawHash.removePrefix("sha256:") else rawHash

                val webVersionFile = File(File(context.filesDir, "web-update"), "version.txt")
                val localVersion = if (webVersionFile.exists()) {
                    webVersionFile.readText().trim()
                } else {
                    context.packageManager.getPackageInfo(context.packageName, 0).versionName ?: "0.0.0"
                }

                if (isNewerVersion(remoteVersion, localVersion)) {
                    val webUpdateDir = File(context.filesDir, "web-update")
                    val webBackupDir = File(context.filesDir, "web-backup")

                    // Download the zip
                    val zipData = fetchUrl(zipUrl)

                    // Verify SHA-256
                    val actualHash = sha256(zipData)
                    if (actualHash != expectedHash) {
                        Log.w(TAG, "Auto-update web OTA hash mismatch: expected $expectedHash, got $actualHash")
                        return
                    }

                    // If existing web-update exists, move to web-backup
                    if (webUpdateDir.exists()) {
                        webBackupDir.deleteRecursively()
                        webUpdateDir.renameTo(webBackupDir)
                    }

                    // Unzip to web-update/
                    val tempZip = File(context.cacheDir, "webapp.zip")
                    tempZip.writeBytes(zipData)
                    try {
                        unzip(tempZip, webUpdateDir)
                    } finally {
                        tempZip.delete()
                    }

                    // Flatten nested subdirectory if index.html not at root
                    val indexFile = File(webUpdateDir, "index.html")
                    if (!indexFile.exists()) {
                        val contents = webUpdateDir.listFiles() ?: emptyArray()
                        if (contents.size == 1 && contents[0].isDirectory) {
                            val subdir = contents[0]
                            subdir.listFiles()?.forEach { item ->
                                item.renameTo(File(webUpdateDir, item.name))
                            }
                            subdir.deleteRecursively()
                        }
                    }

                    // Write version for future comparison
                    File(webUpdateDir, "version.txt").writeText(remoteVersion)
                    Log.d(TAG, "Auto-update web OTA applied: $remoteVersion")
                }
            }
        } catch (e: Exception) {
            Log.w(TAG, "Auto-update check failed: ${e.message}")
        }
    }

    // ── Helpers ──────────────────────────────────────────────────────

    /**
     * Try each endpoint in order. Returns (manifest JSONObject, baseURL) on first success,
     * or null if all endpoints fail. baseURL is the URL up to but not including the filename.
     */
    private fun fetchManifest(endpoints: List<String>): Pair<JSONObject, String>? {
        for (endpoint in endpoints) {
            try {
                val url = URL(endpoint)
                val conn = url.openConnection() as HttpURLConnection
                conn.connectTimeout = 10000
                conn.readTimeout = 15000
                val code = conn.responseCode
                if (code != 200) {
                    conn.disconnect()
                    continue
                }
                val bytes = conn.inputStream.use { it.readBytes() }
                val manifest = JSONObject(String(bytes))
                val baseURL = endpoint.substringBeforeLast("/")
                return Pair(manifest, baseURL)
            } catch (e: Exception) {
                // Try next endpoint
                continue
            }
        }
        return null
    }

    /**
     * Resolve a download URL against a base URL. If the url is already absolute
     * (starts with http:// or https://), return as-is. Otherwise join baseURL + "/" + url.
     */
    private fun resolveDownloadURL(url: String, baseURL: String): String {
        if (url.startsWith("http://") || url.startsWith("https://")) {
            return url
        }
        return "$baseURL/$url"
    }

    private fun isNewerVersion(remote: String, local: String): Boolean {
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

    private fun sha256(data: ByteArray): String {
        val digest = MessageDigest.getInstance("SHA-256")
        return digest.digest(data).joinToString("") { "%02x".format(it) }
    }

    private fun unzip(zipFile: File, targetDir: File) {
        targetDir.mkdirs()
        ZipInputStream(zipFile.inputStream()).use { zis ->
            var entry = zis.nextEntry
            while (entry != null) {
                val outFile = File(targetDir, entry.name)
                if (!outFile.canonicalPath.startsWith(targetDir.canonicalPath + File.separator)) {
                    throw SecurityException("Zip Slip detected: ${entry.name}")
                }
                if (entry.isDirectory) {
                    outFile.mkdirs()
                } else {
                    outFile.parentFile?.mkdirs()
                    outFile.outputStream().use { zis.copyTo(it) }
                }
                zis.closeEntry()
                entry = zis.nextEntry
            }
        }
    }

    private fun fetchUrl(urlString: String): ByteArray {
        val url = URL(urlString)
        val conn = url.openConnection() as HttpURLConnection
        conn.connectTimeout = 15000
        conn.readTimeout = 30000
        val code = conn.responseCode
        if (code != 200) {
            throw java.io.IOException("HTTP $code from $urlString")
        }
        return conn.inputStream.use { it.readBytes() }
    }

    // ── VPN service binding ─────────────────────────────────────────

    private fun bindToService() {
        serviceConnection = object : ServiceConnection {
            override fun onServiceConnected(name: ComponentName?, binder: IBinder?) {
                vpnService = (binder as? VpnServiceBridge.BridgeBinder)?.getService()
                vpnService?.setPlugin(this@K2Plugin)
            }
            override fun onServiceDisconnected(name: ComponentName?) {
                vpnService = null
            }
        }
        val intent = Intent().setClassName(context.packageName, vpnServiceClassName)
        context.bindService(intent, serviceConnection!!, Context.BIND_AUTO_CREATE)
    }
}
