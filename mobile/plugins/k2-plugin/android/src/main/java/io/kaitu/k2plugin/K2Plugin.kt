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
import java.io.ByteArrayOutputStream
import java.io.File
import java.net.HttpURLConnection
import java.net.URL
import java.security.MessageDigest
import java.text.SimpleDateFormat
import java.util.Date
import java.util.Locale
import java.util.TimeZone
import java.util.UUID
import java.util.zip.GZIPOutputStream
import java.util.zip.ZipInputStream

@CapacitorPlugin(name = "K2Plugin")
class K2Plugin : Plugin() {

    companion object {
        private const val TAG = "K2Plugin"
    }

    private var vpnService: VpnServiceBridge? = null
    private var serviceConnection: ServiceConnection? = null
    private val vpnServiceClassName = "io.kaitu.K2VpnService"
    private var logsDir: File? = null
    private val webappLogLock = Any()
    private val S3_BUCKET_URL = "https://kaitu-service-logs.s3.ap-northeast-1.amazonaws.com"
    private val MAX_WEBAPP_LOG_SIZE = 50L * 1024 * 1024 // 50MB

    override fun load() {
        Log.d(TAG, "load: K2Plugin initializing")

        // Check for OTA web update with boot verification
        val webUpdateDir = File(context.filesDir, "web-update")
        val bootPending = File(webUpdateDir, ".boot-pending")
        val indexFile = File(webUpdateDir, "index.html")

        if (webUpdateDir.exists() && bootPending.exists()) {
            // OTA webapp failed to call checkReady() last time — rollback
            Log.w(TAG, "load: OTA boot verification failed — rolling back to bundled webapp")
            val webBackupDir = File(context.filesDir, "web-backup")
            webUpdateDir.deleteRecursively()
            webBackupDir.deleteRecursively()
        } else if (webUpdateDir.exists() && indexFile.exists()) {
            Log.d(TAG, "load: OTA web update found, setting server base path: ${webUpdateDir.absolutePath}")
            bootPending.createNewFile()
            bridge.setServerBasePath(webUpdateDir.absolutePath)
        } else if (webUpdateDir.exists()) {
            Log.w(TAG, "load: corrupt OTA web dir (no index.html) — removing")
            webUpdateDir.deleteRecursively()
        }

        // Initialize logs directory for webapp.log writes
        logsDir = File(context.filesDir, "logs").also { it.mkdirs() }

        bindToService()

        // Auto-check for updates after 3s delay
        Handler(Looper.getMainLooper()).postDelayed({
            Log.d(TAG, "load: starting auto-update check")
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
        // Clear OTA boot-pending marker (webapp loaded successfully)
        val bootPending = File(context.filesDir, "web-update/.boot-pending")
        if (bootPending.exists()) {
            bootPending.delete()
            Log.d(TAG, "checkReady: OTA boot verified — cleared .boot-pending")
        }

        val version = context.packageManager.getPackageInfo(context.packageName, 0).versionName ?: "unknown"
        val ret = JSObject()
        ret.put("ready", true)
        ret.put("version", version)
        call.resolve(ret)
    }

    @PluginMethod
    fun getUDID(call: PluginCall) {
        val raw = Settings.Secure.getString(context.contentResolver, Settings.Secure.ANDROID_ID)
        val ret = JSObject()
        ret.put("udid", K2PluginUtils.hashToUdid(raw))
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
            Log.d(TAG, "getStatus: raw JSON from service: $json")
            try {
                val obj = JSObject(json)
                call.resolve(remapStatusKeys(obj))
            } catch (e: Exception) {
                Log.w(TAG, "getStatus: failed to parse service JSON: ${e.message}")
                val ret = JSObject()
                ret.put("state", "disconnected")
                call.resolve(ret)
            }
        } else {
            Log.d(TAG, "getStatus: service not bound — returning disconnected")
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
            Log.e(TAG, "connect: missing config parameter")
            call.reject("Missing config parameter")
            return
        }
        Log.i(TAG, "connect: config length=${config.length}")

        // Save config JSON
        context.getSharedPreferences("k2vpn", Context.MODE_PRIVATE)
            .edit().putString("configJSON", config).apply()
        Log.d(TAG, "connect: config saved to SharedPreferences")

        // Check VPN permission — Android requires user consent via VpnService.prepare()
        // CRITICAL: Must use Activity context (NOT Application context).
        // Using app context → establish() returns null on Android 15+.
        val act = activity
        if (act == null) {
            Log.e(TAG, "connect: no activity available — cannot request VPN permission")
            call.reject("No activity available for VPN permission")
            return
        }

        val prepareIntent = VpnService.prepare(act)
        if (prepareIntent != null) {
            Log.d(TAG, "connect: VPN permission not yet granted — showing system consent dialog")
            startActivityForResult(call, prepareIntent, "vpnPermissionResult")
        } else {
            Log.d(TAG, "connect: VPN permission already granted — starting service directly")
            startVpnService(config)
            call.resolve()
        }
    }

    @ActivityCallback
    private fun vpnPermissionResult(call: PluginCall, result: ActivityResult) {
        Log.d(TAG, "vpnPermissionResult: resultCode=${result.resultCode}")
        if (result.resultCode == Activity.RESULT_OK) {
            val config = call.getString("config")
            if (config != null) {
                Log.i(TAG, "vpnPermissionResult: permission granted — starting VPN service")
                startVpnService(config)
                call.resolve()
            } else {
                Log.e(TAG, "vpnPermissionResult: config lost after permission grant")
                call.reject("config lost after VPN permission grant")
            }
        } else {
            Log.w(TAG, "vpnPermissionResult: user denied VPN permission")
            call.reject("VPN permission denied by user")
        }
    }

    private fun startVpnService(configJSON: String) {
        Log.d(TAG, "startVpnService: sending START intent to $vpnServiceClassName")
        val intent = Intent().apply {
            setClassName(context.packageName, vpnServiceClassName)
            action = "START"
            putExtra("configJSON", configJSON)
        }
        context.startForegroundService(intent)
    }

    @PluginMethod
    fun disconnect(call: PluginCall) {
        Log.i(TAG, "disconnect: sending STOP intent")
        val intent = Intent().apply {
            setClassName(context.packageName, vpnServiceClassName)
            action = "STOP"
        }
        context.startService(intent)
        call.resolve()
    }

    /**
     * Unified status callback from K2VpnService — receives engine status JSON.
     * JSON format: {"state":"...","error":{"code":N,"message":"..."},"connected_at":"...","uptime_seconds":N}
     *
     * Emits vpnStateChange (always) with full remapped status for transformStatus() in JS.
     * Also emits vpnError when disconnected+error for immediate error UI path.
     */
    fun onStatus(statusJSON: String) {
        Log.d(TAG, "onStatus: $statusJSON")
        try {
            val obj = JSONObject(statusJSON)
            val state = obj.optString("state", "disconnected")

            // Emit vpnStateChange with full remapped status — JS transformStatus() handles all cases
            val data = JSObject(statusJSON)
            notifyListeners("vpnStateChange", remapStatusKeys(data))

            // Also emit vpnError for terminal error states (disconnected + error).
            // Do NOT emit for connected+error — that's a retrying state (TUN up, wire broken).
            val errorObj = obj.optJSONObject("error")
            if (errorObj != null && state == "disconnected") {
                val errorData = JSObject()
                errorData.put("code", errorObj.optInt("code", 570))
                errorData.put("message", errorObj.optString("message", "unknown error"))
                Log.w(TAG, "onStatus: terminal error — code=${errorObj.optInt("code")} message=${errorObj.optString("message")}")
                notifyListeners("vpnError", errorData)
            }
        } catch (e: Exception) {
            Log.e(TAG, "onStatus: failed to parse status JSON: ${e.message}", e)
            // Fallback: emit basic disconnected state
            val data = JSObject()
            data.put("state", "disconnected")
            notifyListeners("vpnStateChange", data)
        }
    }

    /**
     * Direct error callback for non-engine errors (e.g., VPN permission revoked by system).
     * Kept as fallback — prefer onStatus() for engine events.
     */
    fun onError(message: String) {
        Log.w(TAG, "onError: $message")
        val data = JSObject()
        data.put("code", 570)
        data.put("message", message)
        notifyListeners("vpnError", data)
    }

    // ── Update methods ──────────────────────────────────────────────

    @PluginMethod
    fun checkWebUpdate(call: PluginCall) {
        Thread {
            try {
                val result = fetchManifest(K2PluginUtils.webManifestEndpoints(getChannel()))
                if (result == null) {
                    val ret = JSObject()
                    ret.put("available", false)
                    call.resolve(ret)
                    return@Thread
                }
                val (manifest, _) = result
                val remoteVersion = manifest.getString("version")
                val remoteSize = manifest.optLong("size", 0)

                // Check min_native compatibility
                val minNative = manifest.optString("min_native", "")
                val appVersion = context.packageManager
                    .getPackageInfo(context.packageName, 0).versionName ?: "0.0.0"
                if (!K2PluginUtils.isCompatibleNativeVersion(minNative, appVersion)) {
                    Log.w(TAG, "Web OTA skipped: min_native=$minNative > app=$appVersion")
                    val ret = JSObject()
                    ret.put("available", false)
                    ret.put("reason", "native_too_old")
                    call.resolve(ret)
                    return@Thread
                }

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
                val result = fetchManifest(K2PluginUtils.androidManifestEndpoints(getChannel()))
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
                val result = fetchManifest(K2PluginUtils.webManifestEndpoints(getChannel()))
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

                // Mark boot-pending for verification on next cold start
                File(webUpdateDir, ".boot-pending").createNewFile()

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
                val result = fetchManifest(K2PluginUtils.androidManifestEndpoints(getChannel()))
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

    @PluginMethod
    fun appendLogs(call: PluginCall) {
        val entries = call.getArray("entries") ?: run { call.resolve(); return }
        val dir = logsDir ?: run { call.resolve(); return }
        val webappLog = File(dir, "webapp.log")

        try {
            val dateFmt = SimpleDateFormat("yyyy-MM-dd'T'HH:mm:ss.SSS'Z'", Locale.US)
            dateFmt.timeZone = TimeZone.getTimeZone("UTC")

            val sb = StringBuilder()
            for (i in 0 until entries.length()) {
                val entry = entries.getJSONObject(i)
                val level = entry.optString("level", "LOG").uppercase()
                val message = entry.optString("message", "")
                val ts = entry.optDouble("timestamp", System.currentTimeMillis().toDouble())
                val timestamp = dateFmt.format(Date(ts.toLong()))
                sb.append("[$timestamp] [$level] $message\n")
            }
            synchronized(webappLogLock) {
                // Truncate if over size limit
                if (webappLog.exists() && webappLog.length() > MAX_WEBAPP_LOG_SIZE) {
                    webappLog.writeText("")
                }
                webappLog.appendText(sb.toString())
            }
        } catch (e: Exception) {
            Log.w(TAG, "appendLogs failed: ${e.message}")
        }
        call.resolve()
    }

    @PluginMethod
    fun uploadLogs(call: PluginCall) {
        val feedbackId = call.getString("feedbackId")

        Thread {
            try {
                val dir = logsDir
                if (dir == null || !dir.exists()) {
                    val ret = JSObject()
                    ret.put("success", false)
                    ret.put("error", "Logs directory not initialized")
                    call.resolve(ret)
                    return@Thread
                }

                val raw = android.provider.Settings.Secure.getString(context.contentResolver, android.provider.Settings.Secure.ANDROID_ID)
                val udid = K2PluginUtils.hashToUdid(raw)

                // 1. Create staging dir
                val stagingDir = File(context.cacheDir, "kaitu-log-upload-${System.currentTimeMillis()}")
                stagingDir.mkdirs()

                // 2. Scan log directory for all log files
                val sourceFiles = mutableListOf<File>()
                dir.listFiles()?.filter {
                    it.isFile && (it.name.endsWith(".log") || it.name.endsWith(".log.gz"))
                }?.forEach { logFile ->
                    if (logFile.length() == 0L) return@forEach

                    if (logFile.name.endsWith(".log.gz")) {
                        // .gz files: copy as binary (no sanitization)
                        val destFile = File(stagingDir, logFile.name)
                        logFile.copyTo(destFile, overwrite = true)
                        Log.d(TAG, "uploadLogs: ${logFile.name} included (binary, size=${logFile.length()})")
                    } else {
                        // .log files: read as UTF-8, sanitize
                        val content = try { logFile.readText() } catch (e: Exception) { return@forEach }
                        if (content.isEmpty()) return@forEach
                        val sanitized = sanitizeLogContent(content)
                        File(stagingDir, logFile.name).writeText(sanitized)
                        Log.d(TAG, "uploadLogs: ${logFile.name} included (size=${logFile.length()})")
                    }
                    sourceFiles.add(logFile)
                }

                if (sourceFiles.isEmpty()) {
                    stagingDir.deleteRecursively()
                    val ret = JSObject()
                    ret.put("success", false)
                    ret.put("error", "No log files found")
                    call.resolve(ret)
                    return@Thread
                }

                // 3. Create zip archive using JDK ZipOutputStream
                val zipFile = File(stagingDir, "logs.zip")
                java.util.zip.ZipOutputStream(zipFile.outputStream()).use { zos ->
                    stagingDir.listFiles()?.filter { it.isFile && it.name != "logs.zip" }?.forEach { file ->
                        zos.putNextEntry(java.util.zip.ZipEntry(file.name))
                        file.inputStream().use { it.copyTo(zos) }
                        zos.closeEntry()
                    }
                }
                val archiveData = zipFile.readBytes()

                // 4. Upload single archive
                val s3Key = generateS3Key(feedbackId, udid)
                uploadToS3(s3Key, archiveData)

                // 5. Clean up staging dir
                stagingDir.deleteRecursively()

                val ret = JSObject()
                ret.put("success", true)
                val keysArray = org.json.JSONArray()
                keysArray.put(JSONObject().apply {
                    put("name", "logs")
                    put("s3Key", s3Key)
                })
                ret.put("s3Keys", keysArray)
                call.resolve(ret)
            } catch (e: Exception) {
                val ret = JSObject()
                ret.put("success", false)
                ret.put("error", e.message ?: "Upload failed")
                call.resolve(ret)
            }
        }.start()
    }

    private fun sanitizeLogContent(content: String): String {
        var result = content
        val patterns = listOf(
            "\"token\":\"" to "\"token\":\"***\"",
            "\"password\":\"" to "\"password\":\"***\"",
            "\"secret\":\"" to "\"secret\":\"***\"",
            "Authorization: Bearer " to "Authorization: Bearer ***",
            "X-K2-Token: " to "X-K2-Token: ***",
        )
        for ((needle, replacement) in patterns) {
            result = result.replace(needle, replacement)
        }
        return result
    }

    private fun gzipCompress(data: ByteArray): ByteArray {
        val bos = ByteArrayOutputStream()
        GZIPOutputStream(bos).use { it.write(data) }
        return bos.toByteArray()
    }

    private fun generateS3Key(feedbackId: String?, udid: String): String {
        val dateFmt = SimpleDateFormat("yyyy/MM/dd", Locale.US)
        dateFmt.timeZone = TimeZone.getTimeZone("UTC")
        val timeFmt = SimpleDateFormat("HHmmss", Locale.US)
        timeFmt.timeZone = TimeZone.getTimeZone("UTC")
        val now = Date()
        val date = dateFmt.format(now)
        val timestamp = timeFmt.format(now)

        val identifier = feedbackId ?: UUID.randomUUID().toString().take(8)

        val version = context.packageManager.getPackageInfo(context.packageName, 0).versionName ?: "unknown"
        return "mobile/$version/$udid/$date/logs-$timestamp-$identifier.zip"
    }

    private fun uploadToS3(s3Key: String, data: ByteArray) {
        val url = URL("$S3_BUCKET_URL/$s3Key")
        val conn = url.openConnection() as HttpURLConnection
        conn.requestMethod = "PUT"
        val contentType = if (s3Key.endsWith(".zip")) "application/zip" else "application/gzip"
        conn.setRequestProperty("Content-Type", contentType)
        conn.setRequestProperty("Content-Length", data.size.toString())
        conn.connectTimeout = 60000
        conn.readTimeout = 60000
        conn.doOutput = true
        conn.outputStream.use { it.write(data) }
        val code = conn.responseCode
        if (code !in 200..299) {
            throw java.io.IOException("S3 upload failed: HTTP $code")
        }
        Log.d(TAG, "Uploaded to S3: $s3Key")
    }

    @PluginMethod
    fun setLogLevel(call: PluginCall) {
        val level = call.getString("level") ?: "info"
        try {
            vpnService?.setLogLevel(level)
            Log.d(TAG, "setLogLevel: $level")
            call.resolve()
        } catch (e: Exception) {
            Log.w(TAG, "setLogLevel failed: ${e.message}")
            call.resolve() // best-effort
        }
    }

    @PluginMethod
    fun setDevEnabled(call: PluginCall) {
        val enabled = call.getBoolean("enabled", false) ?: false
        android.webkit.WebView.setWebContentsDebuggingEnabled(enabled)
        call.resolve()
    }

    @PluginMethod
    fun debugDump(call: PluginCall) {
        call.resolve(JSObject())
    }

    @PluginMethod
    fun getUpdateChannel(call: PluginCall) {
        val ret = JSObject()
        ret.put("channel", getChannel())
        call.resolve(ret)
    }

    @PluginMethod
    fun setUpdateChannel(call: PluginCall) {
        val channel = call.getString("channel") ?: "stable"
        val oldChannel = getChannel()
        saveChannel(channel)
        val ret = JSObject()
        ret.put("channel", channel)
        call.resolve(ret)
        if (oldChannel == "beta" && channel == "stable") {
            performAutoUpdateCheck(forceDowngrade = true)
        } else {
            performAutoUpdateCheck()
        }
    }

    private fun getChannel(): String =
        context.getSharedPreferences("k2_prefs", Context.MODE_PRIVATE)
            .getString("update_channel", "stable") ?: "stable"

    private fun saveChannel(channel: String) =
        context.getSharedPreferences("k2_prefs", Context.MODE_PRIVATE)
            .edit().putString("update_channel", channel).apply()

    // ── Auto-update check ───────────────────────────────────────────

    private fun performAutoUpdateCheck(forceDowngrade: Boolean = false) {
        try {
            val channel = getChannel()
            // 1. Check native update first
            val nativeResult = fetchManifest(K2PluginUtils.androidManifestEndpoints(channel))
            if (nativeResult != null) {
                val (manifest, baseURL) = nativeResult
                val remoteVersion = manifest.getString("version")
                val totalSize = manifest.optLong("size", 0)
                val apkUrl = resolveDownloadURL(manifest.getString("url"), baseURL)
                val minAndroid = manifest.optInt("min_android", 0)
                val localVersion = context.packageManager
                    .getPackageInfo(context.packageName, 0).versionName ?: "0.0.0"

                val shouldUpdate = if (forceDowngrade && localVersion.contains("-beta")) {
                    remoteVersion != localVersion
                } else {
                    isNewerVersion(remoteVersion, localVersion)
                }

                if (shouldUpdate && Build.VERSION.SDK_INT >= minAndroid) {
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
            val webResult = fetchManifest(K2PluginUtils.webManifestEndpoints(channel))
            if (webResult != null) {
                val (manifest, baseURL) = webResult
                val remoteVersion = manifest.getString("version")
                val zipUrl = resolveDownloadURL(manifest.getString("url"), baseURL)
                val rawHash = manifest.getString("hash")
                val expectedHash = if (rawHash.startsWith("sha256:")) rawHash.removePrefix("sha256:") else rawHash

                // Check min_native compatibility
                val minNative = manifest.optString("min_native", "")
                val appVersionForCompat = context.packageManager
                    .getPackageInfo(context.packageName, 0).versionName ?: "0.0.0"
                if (!K2PluginUtils.isCompatibleNativeVersion(minNative, appVersionForCompat)) {
                    Log.w(TAG, "Auto web OTA skipped: min_native=$minNative > app=$appVersionForCompat")
                    return
                }

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
                    File(webUpdateDir, ".boot-pending").createNewFile()
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
    // Pure utility functions extracted to K2PluginUtils for JVM unit testing.
    private fun resolveDownloadURL(url: String, baseURL: String) = K2PluginUtils.resolveDownloadURL(url, baseURL)
    private fun isNewerVersion(remote: String, local: String) = K2PluginUtils.isNewerVersion(remote, local)
    private fun sha256(data: ByteArray) = K2PluginUtils.sha256(data)

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
        Log.d(TAG, "bindToService: binding to $vpnServiceClassName")
        serviceConnection = object : ServiceConnection {
            override fun onServiceConnected(name: ComponentName?, binder: IBinder?) {
                Log.d(TAG, "onServiceConnected: binder=$binder")
                vpnService = (binder as? VpnServiceBridge.BridgeBinder)?.getService()
                vpnService?.setPlugin(this@K2Plugin)
                Log.i(TAG, "onServiceConnected: vpnService bound=${vpnService != null}")
            }
            override fun onServiceDisconnected(name: ComponentName?) {
                Log.w(TAG, "onServiceDisconnected: service connection lost")
                vpnService = null
            }
        }
        val intent = Intent().setClassName(context.packageName, vpnServiceClassName)
        context.bindService(intent, serviceConnection!!, Context.BIND_AUTO_CREATE)
    }
}
