package io.kaitu

import android.app.Notification
import android.app.NotificationChannel
import android.app.NotificationManager
import android.content.Intent
import android.net.VpnService
import android.os.IBinder
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
    private var plugin: K2Plugin? = null
    private val binder = VpnServiceBridge.BridgeBinder(this)

    override fun onBind(intent: Intent?): IBinder {
        return binder
    }

    override fun onStartCommand(intent: Intent?, flags: Int, startId: Int): Int {
        when (intent?.action) {
            "START" -> {
                val configJSON = intent.getStringExtra("configJSON") ?: return START_NOT_STICKY
                startVpn(configJSON)
            }
            "STOP" -> stopVpn()
        }
        return START_STICKY
    }

    override fun onRevoke() {
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
        createNotificationChannel()
        startForeground(1, createNotification("Connecting..."))

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
            .addRoute("0.0.0.0", 0)
            .addDnsServer("1.1.1.1")
            .addDnsServer("8.8.8.8")
            .setMtu(1400)

        vpnInterface = builder.establish()
        Log.d(TAG, "establish() result: vpnInterface=$vpnInterface, fd=${vpnInterface?.fd}")

        val fd = vpnInterface?.fd
        if (fd == null) {
            Log.e(TAG, "establish() returned null — VPN permission not granted?")
            plugin?.onError("VPN establish failed: permission not granted or system rejected")
            stopVpn()
            return
        }

        try {
            Log.d(TAG, "Starting engine with fd=$fd")
            engine?.start(configJSON, fd.toLong(), filesDir.absolutePath)
            Log.d(TAG, "Engine started successfully")
        } catch (e: Exception) {
            Log.e(TAG, "Engine start failed: ${e.message}", e)
            plugin?.onError(e.message ?: "Failed to start engine")
            stopVpn()
        }
    }

    private fun stopVpn() {
        engine?.stop()
        engine = null
        vpnInterface?.close()
        vpnInterface = null
        stopForeground(STOP_FOREGROUND_REMOVE)
        stopSelf()
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
