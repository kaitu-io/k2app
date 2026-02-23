package io.kaitu

import android.app.Notification
import android.app.NotificationChannel
import android.app.NotificationManager
import android.content.Intent
import android.net.ConnectivityManager
import android.net.Network
import android.net.NetworkCapabilities
import android.net.NetworkRequest
import android.net.VpnService
import android.os.Handler
import android.os.IBinder
import android.os.Looper
import android.content.pm.ServiceInfo
import android.os.Build
import android.os.ParcelFileDescriptor
import android.util.Log
import io.kaitu.k2plugin.K2Plugin
import io.kaitu.k2plugin.VpnServiceBridge
import org.json.JSONObject
import mobile.Mobile
import mobile.Engine
import mobile.EventHandler as MobileEventHandler
import java.util.concurrent.Executors

private data class ParsedClientConfig(
    val tunIpv4: String?,
    val tunIpv6: String?,
    val dnsProxy: List<String>?
)

/** Parse CIDR "10.0.0.2/24" → Pair("10.0.0.2", 24), null on failure. */
private fun parseCIDR(cidr: String): Pair<String, Int>? {
    val parts = cidr.split("/", limit = 2)
    if (parts.size != 2) return null
    val prefix = parts[1].toIntOrNull() ?: return null
    return Pair(parts[0], prefix)
}

/** Strip port from "8.8.8.8:53" → "8.8.8.8". Handles IPv6 "[::1]:53" → "::1". */
private fun stripPort(addr: String): String {
    if (addr.startsWith("[")) {
        val close = addr.indexOf(']')
        return if (close > 0) addr.substring(1, close) else addr
    }
    val parts = addr.split(":")
    return if (parts.size == 2) parts[0] else addr // "ip:port" or bare IP/IPv6
}

/** Extract tun + dns fields from ClientConfig JSON. Null on parse failure. */
private fun parseClientConfig(configJSON: String): ParsedClientConfig? {
    return try {
        val root = JSONObject(configJSON)
        val tun = root.optJSONObject("tun")
        val dns = root.optJSONObject("dns")
        val dnsProxy = dns?.optJSONArray("proxy")?.let { arr ->
            (0 until arr.length()).map { arr.getString(it) }
        }
        ParsedClientConfig(
            tunIpv4 = tun?.optString("ipv4", null),
            tunIpv6 = tun?.optString("ipv6", null),
            dnsProxy = dnsProxy
        )
    } catch (e: Exception) {
        Log.w("K2VpnService", "Failed to parse ClientConfig: ${e.message}")
        null
    }
}

class K2VpnService : VpnService(), VpnServiceBridge, mobile.SocketProtector {

    // mobile.SocketProtector — marks socket FDs for VPN routing exclusion.
    // Delegates to VpnService.protect(int) which tells the OS kernel to route
    // this socket outside the TUN interface, preventing routing loops.
    override fun protect(fd: Int): Boolean {
        return super.protect(fd)
    }

    companion object {
        private const val TAG = "K2VpnService"
    }

    private var engine: Engine? = null
    private var vpnInterface: ParcelFileDescriptor? = null
    @Volatile
    private var plugin: K2Plugin? = null
    private val binder = VpnServiceBridge.BridgeBinder(this)
    private var networkCallback: ConnectivityManager.NetworkCallback? = null
    private val mainHandler = Handler(Looper.getMainLooper())
    private var pendingNetworkChange: Runnable? = null
    /** Single-thread executor for all blocking gomobile JNI calls — keeps them off the main thread. */
    private val engineExecutor = Executors.newSingleThreadExecutor { r ->
        Thread(r, "k2-engine").apply { isDaemon = true }
    }

    override fun onBind(intent: Intent?): IBinder {
        return binder
    }

    override fun onStartCommand(intent: Intent?, flags: Int, startId: Int): Int {
        if (intent == null) {
            stopSelf()
            return START_NOT_STICKY
        }
        when (intent.action) {
            "START" -> {
                val configJSON = intent.getStringExtra("configJSON")
                if (configJSON == null) {
                    stopSelf()
                    return START_NOT_STICKY
                }
                startVpn(configJSON)
            }
            "STOP" -> stopVpn()
            else -> stopSelf()
        }
        return START_NOT_STICKY
    }

    override fun onRevoke() {
        plugin?.onError("VPN permission revoked by system")
        stopVpn()
        super.onRevoke()
    }

    override fun setPlugin(plugin: K2Plugin) {
        this.plugin = plugin
    }

    override fun getStatusJSON(): String {
        return engine?.statusJSON() ?: "{\"state\":\"disconnected\"}"
    }

    private fun startVpn(configJSON: String) {
        Log.d(TAG, "startVpn: configJSON length=${configJSON.length}")
        if (engine != null) {
            Log.w(TAG, "Stopping existing VPN before reconnect")
            stopVpn()
        }
        createNotificationChannel()
        if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.UPSIDE_DOWN_CAKE) {
            startForeground(1, createNotification("Connecting..."),
                ServiceInfo.FOREGROUND_SERVICE_TYPE_SPECIAL_USE)
        } else {
            startForeground(1, createNotification("Connecting..."))
        }

        Log.d(TAG, "Creating engine...")
        engine = Mobile.newEngine()
        engine?.setEventHandler(object : MobileEventHandler {
            override fun onStateChange(state: String?) {
                Log.d(TAG, "onStateChange: $state")
                state?.let { s ->
                    // Dispatch to main thread — Go callbacks run on goroutine threads
                    mainHandler.post {
                        plugin?.onStateChange(s)
                        if (s == "connected") {
                            updateNotification("Connected")
                        }
                    }
                }
            }

            override fun onError(message: String?) {
                Log.e(TAG, "onError: $message")
                message?.let { msg ->
                    mainHandler.post { plugin?.onError(msg) }
                }
            }

            override fun onStats(txBytes: Long, rxBytes: Long) {
                // Stats tracking
            }
        })

        // Build VPN interface from config (with fallback defaults)
        Log.d(TAG, "Building VPN interface...")
        val config = parseClientConfig(configJSON)
        val (ipv4Addr, ipv4Prefix) = parseCIDR(config?.tunIpv4 ?: "10.0.0.2/24") ?: Pair("10.0.0.2", 24)
        val (ipv6Addr, ipv6Prefix) = parseCIDR(config?.tunIpv6 ?: "fd00::2/64") ?: Pair("fd00::2", 64)
        val dnsServers = config?.dnsProxy?.map { stripPort(it) }?.filter { it.isNotEmpty() }
            ?.takeIf { it.isNotEmpty() } ?: listOf("1.1.1.1", "8.8.8.8")

        val builder = Builder()
            .setSession("Kaitu VPN")
            .addAddress(ipv4Addr, ipv4Prefix)
            .addAddress(ipv6Addr, ipv6Prefix)
            .addRoute("0.0.0.0", 0)
            .addRoute("::", 0)
            .setMtu(1400)
            .also { b -> dnsServers.forEach { b.addDnsServer(it) } }

        vpnInterface = builder.establish()
        Log.d(TAG, "establish() result: vpnInterface=$vpnInterface")

        // Transfer fd ownership to Go engine via detachFd().
        // Go engine will close the fd when it stops — we must NOT close it from Kotlin.
        val rawFd = vpnInterface?.detachFd()
        if (rawFd == null || rawFd == -1) {
            Log.e(TAG, "establish() returned null — VPN permission not granted?")
            plugin?.onError("VPN establish failed: permission not granted or system rejected")
            stopVpn()
            return
        }

        // Run blocking gomobile engine.start() off the main thread to prevent ANR.
        val cachePath = cacheDir.absolutePath
        engineExecutor.execute {
            try {
                Log.d(TAG, "Starting engine with fd=$rawFd (background thread)")
                val engineCfg = Mobile.newEngineConfig()
                engineCfg.cacheDir = cachePath
                engineCfg.socketProtector = this@K2VpnService
                engine?.start(configJSON, rawFd.toLong(), engineCfg)
                Log.d(TAG, "Engine started successfully")
                mainHandler.post { registerNetworkCallback() }
            } catch (e: Exception) {
                Log.e(TAG, "Engine start failed: ${e.message}", e)
                mainHandler.post {
                    plugin?.onError(e.message ?: "Failed to start engine")
                    stopVpn()
                }
            }
        }
    }

    private fun stopVpn() {
        unregisterNetworkCallback()
        val eng = engine
        engine = null
        // fd ownership was transferred to Go engine via detachFd() — don't close vpnInterface
        vpnInterface = null
        if (eng != null) {
            // Run blocking gomobile stop() off main thread
            engineExecutor.execute {
                try {
                    eng.stop()
                } catch (e: Exception) {
                    Log.w(TAG, "Engine stop error: ${e.message}")
                }
                mainHandler.post {
                    stopForeground(STOP_FOREGROUND_REMOVE)
                    stopSelf()
                }
            }
        } else {
            stopForeground(STOP_FOREGROUND_REMOVE)
            stopSelf()
        }
    }

    private fun registerNetworkCallback() {
        val cm = getSystemService(ConnectivityManager::class.java) ?: return
        val request = NetworkRequest.Builder()
            .addCapability(NetworkCapabilities.NET_CAPABILITY_INTERNET)
            .build()
        val callback = object : ConnectivityManager.NetworkCallback() {
            override fun onAvailable(network: Network) {
                Log.d(TAG, "Network available: $network")
                pendingNetworkChange?.let { mainHandler.removeCallbacks(it) }
                val runnable = Runnable {
                    // Run gomobile call off main thread
                    engineExecutor.execute {
                        Log.d(TAG, "Triggering engine network change reset")
                        engine?.onNetworkChanged()
                    }
                }
                pendingNetworkChange = runnable
                mainHandler.postDelayed(runnable, 500)
            }

            override fun onLost(network: Network) {
                Log.d(TAG, "Network lost: $network — clearing dead connections immediately")
                pendingNetworkChange?.let { mainHandler.removeCallbacks(it) }
                pendingNetworkChange = null
                // Run gomobile call off main thread
                engineExecutor.execute {
                    engine?.onNetworkChanged()
                }
            }
        }
        cm.registerNetworkCallback(request, callback)
        networkCallback = callback
        Log.d(TAG, "Network callback registered")
    }

    private fun unregisterNetworkCallback() {
        pendingNetworkChange?.let { mainHandler.removeCallbacks(it) }
        pendingNetworkChange = null
        networkCallback?.let {
            try {
                val cm = getSystemService(ConnectivityManager::class.java)
                cm?.unregisterNetworkCallback(it)
            } catch (e: Exception) {
                Log.w(TAG, "Failed to unregister network callback: ${e.message}")
            }
        }
        networkCallback = null
    }

    private fun createNotificationChannel() {
        val channel = NotificationChannel(
            "k2vpn",
            "K2 VPN",
            NotificationManager.IMPORTANCE_LOW
        ).apply {
            description = "VPN connection status"
        }
        val manager = getSystemService(NotificationManager::class.java)
        manager.createNotificationChannel(channel)
    }

    private fun createNotification(text: String): Notification {
        return Notification.Builder(this, "k2vpn")
            .setContentTitle("Kaitu VPN")
            .setContentText(text)
            .setSmallIcon(android.R.drawable.ic_lock_idle_lock)
            .setOngoing(true)
            .build()
    }

    private fun updateNotification(text: String) {
        val manager = getSystemService(NotificationManager::class.java)
        manager.notify(1, createNotification(text))
    }
}
