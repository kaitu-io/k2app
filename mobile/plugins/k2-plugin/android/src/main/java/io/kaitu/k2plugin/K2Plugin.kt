package io.kaitu.k2plugin

import android.content.ComponentName
import android.content.Context
import android.content.Intent
import android.content.ServiceConnection
import android.os.IBinder
import android.provider.Settings
import com.getcapacitor.JSObject
import com.getcapacitor.Plugin
import com.getcapacitor.PluginCall
import com.getcapacitor.PluginMethod
import com.getcapacitor.annotation.CapacitorPlugin

@CapacitorPlugin(name = "K2Plugin")
class K2Plugin : Plugin() {

    private var vpnService: K2VpnService? = null
    private var serviceConnection: ServiceConnection? = null

    override fun load() {
        bindToService()
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
                ret.put("state", "stopped")
                call.resolve(ret)
            }
        } else {
            val ret = JSObject()
            ret.put("state", "stopped")
            call.resolve(ret)
        }
    }

    /** Remap Go StatusJSON snake_case keys to JS camelCase and map "disconnected" → "stopped" */
    private fun remapStatusKeys(obj: JSObject): JSObject {
        val keyMap = mapOf(
            "connected_at" to "connectedAt",
            "uptime_seconds" to "uptimeSeconds",
            "wire_url" to "wireUrl",
        )
        val result = JSObject()
        val keys = obj.keys()
        while (keys.hasNext()) {
            val key = keys.next()
            val newKey = keyMap[key] ?: key
            result.put(newKey, obj.get(key))
        }
        if (result.getString("state") == "disconnected") {
            result.put("state", "stopped")
        }
        return result
    }

    @PluginMethod
    fun getConfig(call: PluginCall) {
        val prefs = context.getSharedPreferences("k2vpn", Context.MODE_PRIVATE)
        val wireUrl = prefs.getString("wireUrl", "") ?: ""
        val ret = JSObject()
        ret.put("wireUrl", wireUrl)
        call.resolve(ret)
    }

    @PluginMethod
    fun connect(call: PluginCall) {
        val wireUrl = call.getString("wireUrl")
        if (wireUrl == null) {
            call.reject("Missing wireUrl parameter")
            return
        }

        // Save wireUrl
        context.getSharedPreferences("k2vpn", Context.MODE_PRIVATE)
            .edit().putString("wireUrl", wireUrl).apply()

        // Start VPN service
        val intent = Intent(context, K2VpnService::class.java).apply {
            action = "START"
            putExtra("wireUrl", wireUrl)
        }
        context.startForegroundService(intent)
        call.resolve()
    }

    @PluginMethod
    fun disconnect(call: PluginCall) {
        val intent = Intent(context, K2VpnService::class.java).apply {
            action = "STOP"
        }
        context.startService(intent)
        call.resolve()
    }

    // Called by K2VpnService when state changes
    fun onStateChange(state: String) {
        val mapped = if (state == "disconnected") "stopped" else state
        val data = JSObject()
        data.put("state", mapped)
        notifyListeners("vpnStateChange", data)
    }

    fun onError(message: String) {
        val data = JSObject()
        data.put("message", message)
        notifyListeners("vpnError", data)
    }

    private fun bindToService() {
        serviceConnection = object : ServiceConnection {
            override fun onServiceConnected(name: ComponentName?, binder: IBinder?) {
                vpnService = (binder as? K2VpnService.LocalBinder)?.getService()
                vpnService?.setPlugin(this@K2Plugin)
            }
            override fun onServiceDisconnected(name: ComponentName?) {
                vpnService = null
            }
        }
        val intent = Intent(context, K2VpnService::class.java)
        context.bindService(intent, serviceConnection!!, Context.BIND_AUTO_CREATE)
    }
}
