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
import mobile.Mobile
import mobile.Engine
import mobile.EventHandler as MobileEventHandler

class K2VpnService : VpnService(), VpnServiceBridge {

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
                state?.let {
                    plugin?.onStateChange(it)
                    if (it == "connected") {
                        updateNotification("Connected")
                    }
                }
            }

            override fun onError(message: String?) {
                Log.e(TAG, "onError: $message")
                message?.let { plugin?.onError(it) }
            }

            override fun onStats(txBytes: Long, rxBytes: Long) {
                // Stats tracking
            }
        })

        // Build VPN interface
        Log.d(TAG, "Building VPN interface...")
        val builder = Builder()
            .setSession("Kaitu VPN")
            .addAddress("10.0.0.2", 32)
            .addAddress("fd00::2", 128)
            .addRoute("0.0.0.0", 0)
            .addRoute("::", 0)
            .addDnsServer("1.1.1.1")
            .addDnsServer("8.8.8.8")
            .setMtu(1400)

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

        try {
            Log.d(TAG, "Starting engine with fd=$rawFd")
            engine?.start(configJSON, rawFd.toLong(), filesDir.absolutePath)
            Log.d(TAG, "Engine started successfully")
            registerNetworkCallback()
        } catch (e: Exception) {
            Log.e(TAG, "Engine start failed: ${e.message}", e)
            plugin?.onError(e.message ?: "Failed to start engine")
            stopVpn()
        }
    }

    private fun stopVpn() {
        unregisterNetworkCallback()
        engine?.stop()
        engine = null
        // fd ownership was transferred to Go engine via detachFd() — don't close vpnInterface
        vpnInterface = null
        stopForeground(STOP_FOREGROUND_REMOVE)
        stopSelf()
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
                    Log.d(TAG, "Triggering engine network change reset")
                    engine?.onNetworkChanged()
                }
                pendingNetworkChange = runnable
                mainHandler.postDelayed(runnable, 500)
            }

            override fun onLost(network: Network) {
                Log.d(TAG, "Network lost: $network — clearing dead connections immediately")
                pendingNetworkChange?.let { mainHandler.removeCallbacks(it) }
                pendingNetworkChange = null
                engine?.onNetworkChanged()
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
