package io.kaitu.k2plugin

import java.io.File
import java.text.SimpleDateFormat
import java.util.Date
import java.util.Locale
import java.util.TimeZone

/**
 * Thread-safe file logger for native-layer events in the VPN service process.
 * Writes to `native.log` in the app's logs directory.
 */
object NativeLogger {
    private var logFile: File? = null
    private val lock = Any()
    private const val MAX_FILE_SIZE = 20L * 1024 * 1024 // 20 MB (was 50 MB)

    private val dateFormat = SimpleDateFormat("yyyy-MM-dd'T'HH:mm:ss.SSS'Z'", Locale.US).apply {
        timeZone = TimeZone.getTimeZone("UTC")
    }

    /** Initialize logger with the logs directory. Call from K2VpnService.startVpn(). */
    fun setup(logsDir: File) {
        synchronized(lock) {
            logsDir.mkdirs()
            logFile = File(logsDir, "native.log")
        }
    }

    /** Append a log entry. Thread-safe. */
    fun log(level: String, message: String) {
        synchronized(lock) {
            val file = logFile ?: return

            // Truncate if over size limit
            if (file.exists() && file.length() > MAX_FILE_SIZE) {
                file.writeText("")
            }

            val timestamp = dateFormat.format(Date())
            val line = "[$timestamp] [${level.uppercase()}] $message\n"
            file.appendText(line)
        }
    }
}
