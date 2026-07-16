package io.kaitu

import android.app.Notification
import android.app.NotificationChannel
import android.app.NotificationManager
import android.content.Context
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
import io.kaitu.k2plugin.NativeLogger
import io.kaitu.k2plugin.VpnServiceBridge
import org.json.JSONObject
import appext.Appext
import appext.Engine
import appext.EventHandler as AppextEventHandler
import appext.NetEvent
import java.util.concurrent.ConcurrentHashMap
import java.util.concurrent.Executors
import java.util.concurrent.atomic.AtomicBoolean

// Pure utility functions (parseCIDR, stripPort, parseClientConfig, ParsedClientConfig)
// are in K2VpnServiceUtils.kt for JVM unit testing.

class K2VpnService : VpnService(), VpnServiceBridge, appext.SocketProtector, appext.PackageResolver {

    // appext.SocketProtector — marks socket FDs for VPN routing exclusion.
    // Delegates to VpnService.protect(int) which tells the OS kernel to route
    // this socket outside the TUN interface, preventing routing loops.
    override fun protect(fd: Int): Boolean {
        return super.protect(fd)
    }

    // appext.PackageResolver — maps Android UID → package name for the rule
    // engine's package_name match (App Bypass per-app routing).
    override fun packageForUID(uid: Int): String {
        val pm = packageManager
        pm.getPackagesForUid(uid)?.firstOrNull()?.let { return it }
        return pm.getNameForUid(uid) ?: ""
    }

    // App-Bypass v2: installer source is install-time fixed, cache per-process.
    private val installerCache = ConcurrentHashMap<String, String>()

    override fun installerForPackage(packageName: String): String {
        if (packageName.isEmpty()) return ""
        installerCache[packageName]?.let { return it }
        val installer = try {
            val pm = packageManager
            if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.R) {
                pm.getInstallSourceInfo(packageName).installingPackageName ?: ""
            } else {
                @Suppress("DEPRECATION")
                pm.getInstallerPackageName(packageName) ?: ""
            }
        } catch (e: Exception) {
            ""
        }
        installerCache[packageName] = installer
        return installer
    }

    companion object {
        private const val TAG = "K2VpnService"
        /** Worst-case bound on how long a memory-pressure pause can last before self-waking. */
        private const val PAUSE_SAFETY_NET_MS = 60_000L
    }

    // @Volatile: read from main, engineExecutor, and system callback threads.
    // Stale-engine guard in EventHandler.onStatus relies on fresh visibility of
    // this field to drop late emits from retired engines.
    @Volatile
    private var engine: Engine? = null
    private var vpnInterface: ParcelFileDescriptor? = null
    @Volatile
    private var plugin: K2Plugin? = null
    private val binder = VpnServiceBridge.BridgeBinder(this)
    private var networkCallback: ConnectivityManager.NetworkCallback? = null
    private val mainHandler = Handler(Looper.getMainLooper())
    private var pendingNetworkChange: Runnable? = null
    /**
     * Whether a terminal error status (state=disconnected + error) from the
     * current engine session has already been forwarded to the plugin. The
     * engine classifies failures (e.g. 101 network) and emits them via
     * OnStatus before start() throws; the catch-block safety net must not
     * overwrite that with a generic 570. Written and read only on the main
     * thread (both the EventHandler forward and the catch synthesis run
     * inside mainHandler.post), reset at the top of startVpn().
     */
    private var terminalErrorForwarded = false
    /** Tracks whether engine was paused by onTrimMemory — reset on wake or stopVpn. */
    private val enginePaused = AtomicBoolean(false)
    /**
     * Bounds how long the engine can stay paused. The only other wake path is
     * ConnectivityManager.onAvailable(), which does not fire on a stable,
     * unchanging network — on a fixed home WiFi this left the tunnel torn
     * down (while the UI still read "connected") for 11-58 minutes in the
     * field (ticket #3169). This timer force-wakes regardless.
     */
    private var pendingPauseTimeout: Runnable? = null
    /** Single-thread executor for all blocking gomobile JNI calls — keeps them off the main thread. */
    private val engineExecutor = Executors.newSingleThreadExecutor { r ->
        Thread(r, "k2-engine").apply { isDaemon = true }
    }

    override fun onBind(intent: Intent?): IBinder? {
        // System binds with SERVICE_INTERFACE action for always-on VPN management
        if (intent?.action == SERVICE_INTERFACE) {
            Log.d(TAG, "onBind: system VPN bind (SERVICE_INTERFACE)")
            return super.onBind(intent)
        }
        Log.d(TAG, "onBind: app bind (K2Plugin bridge)")
        return binder
    }

    override fun onStartCommand(intent: Intent?, flags: Int, startId: Int): Int {
        Log.d(TAG, "onStartCommand: action=${intent?.action} flags=$flags startId=$startId")

        // System-initiated restart (always-on VPN): null intent OR SERVICE_INTERFACE action.
        // Different Android versions/OEMs may use either form.
        val isSystemRestart = intent == null || intent.action == SERVICE_INTERFACE
        if (isSystemRestart) {
            val savedConfig = applicationContext
                .getSharedPreferences("k2vpn", Context.MODE_PRIVATE)
                .getString("configJSON", null)
            if (savedConfig != null) {
                Log.i(TAG, "onStartCommand: system restart (action=${intent?.action}) — recovering from saved config")
                NativeLogger.log("INFO", "onStartCommand: always-on VPN restart, config length=${savedConfig.length}")
                startVpn(savedConfig)
                return START_NOT_STICKY
            }
            Log.w(TAG, "onStartCommand: system restart (action=${intent?.action}), no saved config — stopping self")
            stopSelf()
            return START_NOT_STICKY
        }

        when (intent!!.action) {
            "START" -> {
                val configJSON = intent.getStringExtra("configJSON")
                if (configJSON == null) {
                    Log.e(TAG, "onStartCommand START: missing configJSON — stopping self")
                    stopSelf()
                    return START_NOT_STICKY
                }
                Log.d(TAG, "onStartCommand START: configJSON length=${configJSON.length}")
                startVpn(configJSON)
            }
            "STOP" -> {
                Log.d(TAG, "onStartCommand STOP: tearing down VPN")
                NativeLogger.log("INFO", "onStartCommand STOP: synthesizing disconnected before teardown")
                // Synthesize disconnected status BEFORE eng.stop() runs.
                //
                // eng.stop() goes through engineExecutor (single-thread). Under wire-recovery
                // conditions, the executor can be queued behind a slow Wake/Pause/notifyNetEvent
                // whose tm.ResetConnections takes seconds — so the engine's own OnStatus emit
                // is unbounded. Without this synthetic, UI is stuck on "disconnecting" until
                // either the executor drains or the user swipe-kills the app (#254 mechanism).
                //
                // Mirror onRevoke()'s pattern: caller synthesizes, then calls stopVpn(). Only
                // the user-disconnect entry needs this — internal stopVpn callers (startVpn
                // restart, establish failure, engine.start failure) already emit appropriate
                // status before calling stopVpn, and the JS state machine they'd hit is not
                // "connected", so a synthetic disconnected here would cause a UI flash.
                val syntheticStatus = JSONObject().apply { put("state", "disconnected") }
                plugin?.onStatus(syntheticStatus.toString())
                stopVpn()
            }
            else -> {
                Log.w(TAG, "onStartCommand: unknown action=${intent.action} — stopping self")
                stopSelf()
            }
        }
        return START_NOT_STICKY
    }

    override fun onRevoke() {
        Log.w(TAG, "onRevoke: VPN permission revoked by system")
        NativeLogger.log("WARN", "onRevoke: VPN permission revoked")
        // Synthesize a status event for revocation (engine won't emit this).
        // Use code 403 (Forbidden) to indicate permission issue.
        val revokeStatus = JSONObject().apply {
            put("state", "disconnected")
            put("error", JSONObject().apply {
                put("code", 403)
                put("message", "VPN permission revoked by system")
            })
        }
        plugin?.onStatus(revokeStatus.toString())
        stopVpn()
        super.onRevoke()
    }

    override fun onTrimMemory(level: Int) {
        super.onTrimMemory(level)
        // RUNNING_CRITICAL (was RUNNING_LOW): this is a foreground service, so it
        // only ever sees the RUNNING_* tiers (5/10/15), never the backgroundable
        // BACKGROUND/MODERATE/COMPLETE tiers (40/60/80). RUNNING_LOW fires readily
        // during ordinary background use and was tearing down the tunnel far more
        // often than genuine memory pressure warranted (ticket #3169).
        if (level >= TRIM_MEMORY_RUNNING_CRITICAL && engine != null && !enginePaused.get()) {
            Log.w(TAG, "onTrimMemory: level=$level — pausing engine and freeing Go memory")
            NativeLogger.log("WARN", "onTrimMemory: level=$level — pausing engine")
            enginePaused.set(true)

            pendingPauseTimeout?.let { mainHandler.removeCallbacks(it) }
            val timeout = Runnable {
                if (enginePaused.compareAndSet(true, false)) {
                    Log.w(TAG, "pause safety-net: no network callback within ${PAUSE_SAFETY_NET_MS}ms — waking anyway")
                    NativeLogger.log("WARN", "pause safety-net: waking engine without network callback")
                    engineExecutor.execute {
                        try {
                            engine?.wake()
                        } catch (e: Exception) {
                            Log.e(TAG, "pause safety-net: engine.wake() failed: ${e.message}", e)
                        }
                    }
                }
            }
            pendingPauseTimeout = timeout
            mainHandler.postDelayed(timeout, PAUSE_SAFETY_NET_MS)

            engineExecutor.execute {
                try {
                    engine?.pause()
                    Appext.freeMemory()
                    Log.d(TAG, "onTrimMemory: engine paused, memory freed")
                } catch (e: Exception) {
                    Log.e(TAG, "onTrimMemory: pause/freeMemory failed: ${e.message}", e)
                }
            }
        }
    }

    override fun setPlugin(plugin: K2Plugin) {
        this.plugin = plugin
    }

    override fun getStatusJSON(): String {
        return engine?.statusJSON() ?: "{\"state\":\"disconnected\"}"
    }

    override fun setLogLevel(level: String) {
        Appext.setLogLevel(level)
    }

    // Stateless app-region classifier for the App Bypass page. Pure gomobile call
    // (file read + krs match, no engine state) so it runs directly off the
    // plugin's worker thread without engineExecutor, like setLogLevel. cacheDir is
    // the same base passed to Engine.Start; Appext appends "/rules" internally.
    override fun classifyApps(region: String, installedJSON: String): String {
        return try {
            Appext.classifyApps(cacheDir.absolutePath, region, installedJSON)
        } catch (e: Exception) {
            Log.w(TAG, "classifyApps failed: ${e.message}")
            "{\"classifications\":[]}"
        }
    }

    // Antiblock control-plane relay. Stateless static gomobile call (own TCP+uTLS
    // +HTTP round-trip, no engine state) so it runs directly off the plugin's
    // worker thread, VPN-independent — the whole point is bootstrapping before the
    // tunnel exists. Returns the {code,message,data} envelope verbatim; on a native
    // fault emit code:-1 so the webapp transport learns to use the direct fallback.
    override fun relayFetch(request: String): String {
        return try {
            Appext.relayFetch(request)
        } catch (e: Exception) {
            Log.w(TAG, "relayFetch failed: ${e.message}")
            "{\"code\":-1,\"message\":\"relay native error\"}"
        }
    }

    // Antiblock relay node feed. Stateless static gomobile call — incrementally
    // registers camouflage-node descriptors with the RelayManager (Go owns
    // storage/ranking/health/reuse). VPN-independent, like relayFetch.
    override fun relayAddNodes(nodesJSON: String): String {
        return try {
            Appext.relayAddNodes(nodesJSON)
        } catch (e: Exception) {
            Log.w(TAG, "relayAddNodes failed: ${e.message}")
            "{\"code\":-1,\"message\":\"relay native error\"}"
        }
    }

    private fun startVpn(configJSON: String) {
        Log.i(TAG, "startVpn: configJSON length=${configJSON.length}")
        NativeLogger.log("INFO", "startVpn: configJSON length=${configJSON.length}")
        if (engine != null) {
            Log.w(TAG, "startVpn: engine already exists — stopping before reconnect")
            stopVpn()
        }
        terminalErrorForwarded = false
        createNotificationChannel()
        if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.UPSIDE_DOWN_CAKE) {
            startForeground(1, createNotification("Connecting..."),
                ServiceInfo.FOREGROUND_SERVICE_TYPE_SPECIAL_USE)
        } else {
            startForeground(1, createNotification("Connecting..."))
        }

        Log.d(TAG, "Creating engine...")
        // Bind the engine instance to a local val so the EventHandler closure can
        // identity-compare against the field — this lets late emits from a retired
        // engine (rapid stop+connect race) be dropped before they reach JS.
        val newEngine = Appext.newEngine()
        engine = newEngine
        newEngine.setEventHandler(object : AppextEventHandler {
            /**
             * Unified status callback — receives JSON: {"state":"...","error":{...},"connected_at":"..."}
             * Replaces the old onStateChange+onError dual callbacks (Go EventHandler API migrated).
             * iOS already uses this pattern (PacketTunnelProvider.swift EventBridge.onStatus).
             */
            override fun onStatus(statusJSON: String?) {
                // Stale-engine guard: rapid stop+connect leaves the OLD engine's
                // pending eng.stop() to complete on the executor — its synchronous
                // OnStatus(disconnected) emit would race the new session's
                // "connecting" state and flash the UI to idle. Closure-captured
                // newEngine ≠ current field engine ⇒ this handler belongs to a
                // retired session, drop the emit. iOS doesn't need this because
                // VPN status is owned by the system (NEVPNStatusDidChange).
                if (engine !== newEngine) {
                    Log.d(TAG, "onStatus from stale engine — ignoring: $statusJSON")
                    NativeLogger.log("DEBUG", "onStatus from stale engine — ignored: ${statusJSON ?: "null"}")
                    return
                }
                Log.d(TAG, "onStatus: $statusJSON")
                NativeLogger.log("DEBUG", "onStatus: ${statusJSON ?: "null"}")
                if (statusJSON == null) {
                    Log.w(TAG, "onStatus called with null JSON — ignoring")
                    return
                }
                mainHandler.post {
                    // Forward full status JSON to K2Plugin for JS event dispatch
                    plugin?.onStatus(statusJSON)

                    // Update notification based on state
                    try {
                        val obj = JSONObject(statusJSON)
                        val state = obj.optString("state", "")
                        Log.d(TAG, "onStatus parsed state=$state")
                        when (state) {
                            "connected" -> updateNotification("Connected")
                            "connecting" -> updateNotification("Connecting...")
                            "reconnecting" -> updateNotification("Reconnecting...")
                            "disconnected" -> {
                                val hasError = obj.has("error") && !obj.isNull("error")
                                if (hasError) {
                                    terminalErrorForwarded = true
                                    val errObj = obj.optJSONObject("error")
                                    Log.w(TAG, "Disconnected with error: code=${errObj?.optInt("code")} message=${errObj?.optString("message")}")
                                } else {
                                    Log.d(TAG, "Normal disconnect")
                                }
                            }
                        }
                    } catch (e: Exception) {
                        Log.w(TAG, "Failed to parse status JSON for notification: ${e.message}")
                    }
                }
            }

            override fun onStats(txBytes: Long, rxBytes: Long) {
                // Stats tracking — could forward to JS in future
                Log.v(TAG, "onStats: tx=$txBytes rx=$rxBytes")
            }
        })

        // Build VPN interface from config (with fallback defaults)
        val config = K2VpnServiceUtils.parseClientConfig(configJSON)
        val (ipv4Addr, ipv4Prefix) = K2VpnServiceUtils.parseCIDR(config?.tunIpv4 ?: "10.0.0.2/24") ?: Pair("10.0.0.2", 24)
        val (ipv6Addr, ipv6Prefix) = K2VpnServiceUtils.parseCIDR(config?.tunIpv6 ?: "fd00::2/64") ?: Pair("fd00::2", 64)
        val dnsServers = config?.dnsProxy?.map { K2VpnServiceUtils.stripPort(it) }?.filter { it.isNotEmpty() }
            ?.takeIf { it.isNotEmpty() } ?: listOf("1.1.1.1", "8.8.8.8")
        Log.i(TAG, "Building VPN interface: ipv4=$ipv4Addr/$ipv4Prefix ipv6=$ipv6Addr/$ipv6Prefix dns=$dnsServers")

        val builder = Builder()
            .setSession("kaitu.io")
            .addAddress(ipv4Addr, ipv4Prefix)
            .addAddress(ipv6Addr, ipv6Prefix)
            .addRoute("0.0.0.0", 0)
            .addRoute("::", 0)
            .setMtu(1400)
            .also { b -> dnsServers.forEach { b.addDnsServer(it) } }

        // Same-UID traffic (cloudApi, S3 log upload, OTA, auto-update) must bypass our
        // own TUN — otherwise log upload fails exactly when VPN is unhealthy, the case
        // we need logs for. iOS gets this for free via the separate NE process.
        try {
            builder.addDisallowedApplication(packageName)
        } catch (e: android.content.pm.PackageManager.NameNotFoundException) {
            Log.w(TAG, "addDisallowedApplication failed for own package: ${e.message}")
        }

        // App Bypass — kernel-level per-package exclusion. Android sandboxing (API 29+
        // SELinux + hidepid=2) blocks /proc/net/tcp reads, so the Go rule engine's
        // package_name match cannot work. addDisallowedApplication routes the listed
        // apps' traffic around our TUN at the kernel layer instead.
        val disallowed = K2VpnServiceUtils.parseDisallowedPackages(configJSON, packageName)
        for (pkg in disallowed) {
            try {
                builder.addDisallowedApplication(pkg)
                Log.i(TAG, "App Bypass: addDisallowedApplication $pkg")
            } catch (e: android.content.pm.PackageManager.NameNotFoundException) {
                Log.w(TAG, "App Bypass: package not installed, skipped: $pkg")
            }
        }
        NativeLogger.log("INFO", "App Bypass: applied ${disallowed.size} disallowed packages")

        vpnInterface = builder.establish()
        Log.d(TAG, "establish() result: vpnInterface=$vpnInterface")

        if (vpnInterface == null) {
            Log.e(TAG, "establish() returned null — VPN permission not granted?")
            val errorStatus = JSONObject().apply {
                put("state", "disconnected")
                put("error", JSONObject().apply {
                    put("code", 403)
                    put("message", "VPN establish failed: permission not granted or system rejected")
                })
            }
            plugin?.onStatus(errorStatus.toString())
            stopVpn()
            return
        }
        // Pass fd to Go WITHOUT detaching — Kotlin retains ParcelFileDescriptor ownership.
        // Go's appext.Start() already calls syscall.Dup(fd) internally (appext.go:63-68),
        // so Go gets its own independent copy. On disconnect, stopVpn() calls
        // vpnInterface.close() which notifies Android to tear down VPN routing + status bar icon.
        val rawFd = vpnInterface!!.fd
        Log.d(TAG, "fd=$rawFd — Go will dup internally, Kotlin retains ParcelFileDescriptor")

        // Run blocking gomobile engine.start() off the main thread to prevent ANR.
        val cachePath = cacheDir.absolutePath
        val logsDir = java.io.File(filesDir, "logs").also { it.mkdirs() }
        val logsDirPath = logsDir.absolutePath
        NativeLogger.setup(logsDir)
        engineExecutor.execute {
            try {
                Log.d(TAG, "Starting engine with fd=$rawFd (background thread)")
                val engineCfg = Appext.newEngineConfig()
                engineCfg.cacheDir = cachePath
                engineCfg.logDir = logsDirPath
                if (BuildConfig.DEBUG) {
                    engineCfg.debug = true
                }
                Log.d(TAG, "EngineConfig logDir=$logsDirPath debug=${engineCfg.debug}")
                engineCfg.socketProtector = this@K2VpnService
                engineCfg.packageResolver = this@K2VpnService
                engine?.start(configJSON, rawFd.toLong(), engineCfg)
                Log.d(TAG, "Engine started successfully")
                NativeLogger.log("INFO", "startVpn: engine started successfully")
                mainHandler.post { registerNetworkCallback() }
            } catch (e: Exception) {
                Log.e(TAG, "Engine start failed: ${e.message}", e)
                NativeLogger.log("ERROR", "startVpn: engine start failed: ${e.message}")
                mainHandler.post {
                    // The engine classifies failures and emits them via OnStatus
                    // (e.g. 101 network) before start() throws — on those paths a
                    // synthesized status here would arrive second and overwrite
                    // the accurate code with a generic 570 in the UI. Only
                    // synthesize for start failures that never emitted one
                    // (that's the safety net's actual job); always tear down.
                    if (!terminalErrorForwarded) {
                        val errorStatus = JSONObject().apply {
                            put("state", "disconnected")
                            put("error", JSONObject().apply {
                                put("code", 570)
                                put("message", e.message ?: "Failed to start engine")
                            })
                        }
                        plugin?.onStatus(errorStatus.toString())
                    }
                    stopVpn()
                }
            }
        }
    }

    // IMPORTANT: vpnInterface.close() is critical for VPN teardown.
    // Without it, Android keeps VPN routing active (all traffic routed into dead TUN),
    // breaking ALL outbound network requests from the app (including WebView fetch).
    // This manifests as: same-origin (https://localhost) works, but all external
    // requests hang indefinitely. Only a phone reboot can recover from this state.
    // Fixed in a3d5af5 by switching from detachFd() to fd + close().
    private fun stopVpn() {
        Log.i(TAG, "stopVpn: beginning teardown (engine=${engine != null})")
        NativeLogger.log("INFO", "stopVpn: beginning teardown")
        enginePaused.set(false)
        pendingPauseTimeout?.let { mainHandler.removeCallbacks(it) }
        pendingPauseTimeout = null
        unregisterNetworkCallback()
        // Capture eng + pfd locally and clear fields synchronously on the caller thread.
        //
        // Race A (close-after-replace): the deferred mainHandler.post below may run after
        // a startVpn() has assigned a NEW vpnInterface — without local capture, the post
        // would close the new TUN. Local pfd ref → only the OLD pfd is closed.
        //
        // Race B (stopSelf-killing-new-session): same scenario — without the engine==null
        // gate below, the deferred stopSelf would terminate the just-started session.
        //
        // Both writes to engine/vpnInterface happen on main thread (startVpn + stopVpn +
        // mainHandler.post all main-thread-bound), so the field reads in the post are
        // serialized — no torn read possible.
        val eng = engine
        val pfd = vpnInterface
        engine = null
        vpnInterface = null
        if (eng != null) {
            // Run blocking gomobile stop() off main thread
            engineExecutor.execute {
                try {
                    Log.d(TAG, "stopVpn: calling engine.stop() on background thread")
                    eng.stop()
                    Log.d(TAG, "stopVpn: engine.stop() completed")
                    NativeLogger.log("INFO", "stopVpn: engine.stop() completed")
                } catch (e: Exception) {
                    Log.e(TAG, "stopVpn: engine.stop() threw: ${e.message}", e)
                    NativeLogger.log("ERROR", "stopVpn: engine.stop() threw: ${e.message}")
                }
                mainHandler.post {
                    // Close the LOCAL pfd captured at stopVpn entry — never the field.
                    // ParcelFileDescriptor.close() is idempotent (safe if called twice).
                    try { pfd?.close() } catch (_: Exception) {}
                    // Only stop the service if no new session has started in the meantime.
                    // engine != null here means startVpn() ran during teardown — the new
                    // session owns the foreground notification + service lifecycle now.
                    if (engine == null) {
                        Log.d(TAG, "stopVpn: removing foreground service + stopping self")
                        stopForeground(STOP_FOREGROUND_REMOVE)
                        stopSelf()
                    } else {
                        Log.i(TAG, "stopVpn: deferred stopSelf skipped — new session active")
                        NativeLogger.log("INFO", "stopVpn: deferred stopSelf skipped — new session active")
                    }
                }
            }
        } else {
            try { pfd?.close() } catch (_: Exception) {}
            Log.d(TAG, "stopVpn: no engine — just stopping foreground service")
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
                NativeLogger.log("DEBUG", "onAvailable: network change")
                pendingNetworkChange?.let { mainHandler.removeCallbacks(it) }
                // A real network callback beat the safety-net timer — cancel it so it
                // doesn't fire a redundant wake() later (engine.wake() call below is a
                // no-op past this point either way, but this keeps the log honest about
                // which path actually recovered the tunnel).
                pendingPauseTimeout?.let { mainHandler.removeCallbacks(it) }
                pendingPauseTimeout = null
                val runnable = Runnable {
                    // Run gomobile call off main thread
                    engineExecutor.execute {
                        // If engine was paused due to memory pressure, wake it first.
                        if (enginePaused.compareAndSet(true, false)) {
                            Log.d(TAG, "onAvailable: waking paused engine")
                            try { engine?.wake() } catch (e: Exception) {
                                Log.e(TAG, "onAvailable: engine.wake() failed: ${e.message}", e)
                            }
                        }
                        Log.d(TAG, "Triggering engine network change")
                        val caps = cm.getNetworkCapabilities(network)
                        val lp = cm.getLinkProperties(network)
                        val event = NetEvent().apply {
                            // Use "changed" not "available" — Android onAvailable means
                            // "a network joined the set", not "network recovered from nothing".
                            // WiFi→cellular handoff fires onAvailable(cellular) without onLost(WiFi).
                            signal = "changed"
                            interfaceName = lp?.interfaceName ?: ""
                            isWifi = caps?.hasTransport(NetworkCapabilities.TRANSPORT_WIFI) == true
                            isCellular = caps?.hasTransport(NetworkCapabilities.TRANSPORT_CELLULAR) == true
                            hasIPv4 = lp?.linkAddresses?.any { it.address is java.net.Inet4Address } == true
                            hasIPv6 = lp?.linkAddresses?.any { it.address is java.net.Inet6Address } == true
                            source = "connectivity"
                        }
                        engine?.notifyNetEvent(event)
                    }
                }
                pendingNetworkChange = runnable
                mainHandler.postDelayed(runnable, 500)
            }

            override fun onLost(network: Network) {
                Log.d(TAG, "Network lost: $network — clearing dead connections immediately")
                NativeLogger.log("DEBUG", "onLost: network lost")
                pendingNetworkChange?.let { mainHandler.removeCallbacks(it) }
                pendingNetworkChange = null
                // Run gomobile call off main thread
                engineExecutor.execute {
                    // Check if ANY network is still active before declaring unavailable.
                    // onLost fires per-network — if WiFi lost but cellular up, don't say unavailable.
                    val activeNet = cm.activeNetwork
                    val signal = if (activeNet == null) "unavailable" else "changed"
                    val event = NetEvent().apply {
                        this.signal = signal
                        source = "connectivity"
                        if (activeNet != null) {
                            val activeCaps = cm.getNetworkCapabilities(activeNet)
                            val activeLp = cm.getLinkProperties(activeNet)
                            interfaceName = activeLp?.interfaceName ?: ""
                            isWifi = activeCaps?.hasTransport(NetworkCapabilities.TRANSPORT_WIFI) == true
                            isCellular = activeCaps?.hasTransport(NetworkCapabilities.TRANSPORT_CELLULAR) == true
                        }
                    }
                    engine?.notifyNetEvent(event)
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
            .setContentTitle("kaitu.io")
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
