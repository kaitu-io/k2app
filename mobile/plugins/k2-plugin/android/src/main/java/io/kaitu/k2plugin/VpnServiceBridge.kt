package io.kaitu.k2plugin

import android.os.Binder

/**
 * Interface for the VPN service that the app module implements.
 * Breaks the circular dependency between k2-plugin and app modules.
 */
interface VpnServiceBridge {
    fun getStatusJSON(): String
    fun setPlugin(plugin: K2Plugin)

    class BridgeBinder(private val bridge: VpnServiceBridge) : Binder() {
        fun getService(): VpnServiceBridge = bridge
    }
}
