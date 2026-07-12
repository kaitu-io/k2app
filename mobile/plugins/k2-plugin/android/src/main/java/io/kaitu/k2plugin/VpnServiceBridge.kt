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

    /**
     * Antiblock control-plane relay: send one HTTP request to Center through a
     * camouflage node. `request` is a JSON-stringified wire.RelayRequest;
     * returns the JSON {code,message,data} envelope from Appext.relayFetch.
     * Stateless static gomobile call — VPN-independent, so it works during
     * cold-start bootstrap while the tunnel is down. Implemented by the
     * app-module service (the only gomobile-allowed layer).
     */
    fun relayFetch(request: String): String

    /**
     * Antiblock relay node feed: incrementally register camouflage-node
     * descriptors (JSON array of {ip,port?,pin,ech,score?}) with the native
     * RelayManager via Appext.relayAddNodes. Go owns node storage/ranking/health;
     * this only forwards descriptors the webapp discovered. Returns the
     * {code,message,data:{added,total}} envelope. Same gomobile-allowed layer.
     */
    fun relayAddNodes(nodesJSON: String): String

    class BridgeBinder(private val bridge: VpnServiceBridge) : Binder() {
        fun getService(): VpnServiceBridge = bridge
    }
}
