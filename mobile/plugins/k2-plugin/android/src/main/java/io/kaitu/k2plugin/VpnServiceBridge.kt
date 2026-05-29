package io.kaitu.k2plugin

import android.os.Binder

/**
 * Interface for the VPN service that the app module implements.
 * Breaks the circular dependency between k2-plugin and app modules.
 */
interface VpnServiceBridge {
    fun getStatusJSON(): String
    fun setPlugin(plugin: K2Plugin)
    fun setLogLevel(level: String)

    /**
     * Stateless app-region classifier for the App Bypass page. Returns the
     * gomobile Appext.classifyApps JSON ({"classifications":[...]}). Implemented
     * by the app-module service — the only place allowed to call gomobile —
     * keeping the plugin gomobile-free per the plugin-purity rule.
     */
    fun classifyApps(region: String, installedJSON: String): String

    class BridgeBinder(private val bridge: VpnServiceBridge) : Binder() {
        fun getService(): VpnServiceBridge = bridge
    }
}
