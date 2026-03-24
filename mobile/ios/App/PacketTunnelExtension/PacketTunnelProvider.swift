import NetworkExtension
import Network
import K2Mobile  // gomobile xcframework (appext/ package)
import os.log

private let kAppGroup = "group.io.kaitu"
private let logger = Logger(subsystem: "com.allnationconnect.anc.wgios", category: "NE")

private struct ClientConfigSubset: Codable {
    var tun: TunConfig?
    var dns: DNSConfig?

    struct TunConfig: Codable {
        var ipv4: String?   // CIDR "10.0.0.2/24"
        var ipv6: String?   // CIDR "fd00::2/64"
    }
    struct DNSConfig: Codable {
        var proxy: [String]?
        var direct: [String]?
    }
}

// Pure helpers (parseIPv4CIDR, parseIPv6CIDR, stripPort) are in NEHelpers.swift

// MARK: - TUN fd scan (WireGuard approach)
// iOS SDK doesn't expose sys/kern_control.h — define structs manually.
// Layout from XNU: bsd/sys/kern_control.h

private let MAX_KCTL_NAME = 96

private struct ctl_info_manual {
    var ctl_id: UInt32 = 0
    var ctl_name: (CChar, CChar, CChar, CChar, CChar, CChar, CChar, CChar,
                   CChar, CChar, CChar, CChar, CChar, CChar, CChar, CChar,
                   CChar, CChar, CChar, CChar, CChar, CChar, CChar, CChar,
                   CChar, CChar, CChar, CChar, CChar, CChar, CChar, CChar,
                   CChar, CChar, CChar, CChar, CChar, CChar, CChar, CChar,
                   CChar, CChar, CChar, CChar, CChar, CChar, CChar, CChar,
                   CChar, CChar, CChar, CChar, CChar, CChar, CChar, CChar,
                   CChar, CChar, CChar, CChar, CChar, CChar, CChar, CChar,
                   CChar, CChar, CChar, CChar, CChar, CChar, CChar, CChar,
                   CChar, CChar, CChar, CChar, CChar, CChar, CChar, CChar,
                   CChar, CChar, CChar, CChar, CChar, CChar, CChar, CChar,
                   CChar, CChar, CChar, CChar, CChar, CChar, CChar, CChar) =
        (0,0,0,0,0,0,0,0, 0,0,0,0,0,0,0,0, 0,0,0,0,0,0,0,0, 0,0,0,0,0,0,0,0,
         0,0,0,0,0,0,0,0, 0,0,0,0,0,0,0,0, 0,0,0,0,0,0,0,0, 0,0,0,0,0,0,0,0,
         0,0,0,0,0,0,0,0, 0,0,0,0,0,0,0,0, 0,0,0,0,0,0,0,0, 0,0,0,0,0,0,0,0)
}

private struct sockaddr_ctl_manual {
    var sc_len: UInt8 = 0
    var sc_family: UInt8 = 0
    var ss_sysaddr: UInt16 = 0
    var sc_id: UInt32 = 0
    var sc_unit: UInt32 = 0
    var sc_reserved: (UInt32, UInt32, UInt32, UInt32, UInt32) = (0, 0, 0, 0, 0)
}

/// CTLIOCGINFO = _IOWR('N', 3, struct ctl_info)
/// IOC_INOUT(0xC0000000) | sizeof(ctl_info=100)<<16 | 'N'(0x4E)<<8 | 3
private let CTLIOCGINFO_VALUE: UInt = 0xC0644E03
private let AF_SYS_CONTROL: UInt16 = 2

/// Find the utun file descriptor by scanning open fds (WireGuard approach).
/// iOS SDK lacks kern_control.h so we define structs manually.
private func findTunnelFileDescriptor() -> Int32? {
    var ctlInfo = ctl_info_manual()
    withUnsafeMutablePointer(to: &ctlInfo.ctl_name) {
        $0.withMemoryRebound(to: CChar.self, capacity: MAX_KCTL_NAME) {
            _ = strcpy($0, "com.apple.net.utun_control")
        }
    }
    for fd: Int32 in 0...1024 {
        var addr = sockaddr_ctl_manual()
        var ret: Int32 = -1
        var len = socklen_t(MemoryLayout<sockaddr_ctl_manual>.size)
        withUnsafeMutablePointer(to: &addr) {
            $0.withMemoryRebound(to: sockaddr.self, capacity: 1) {
                ret = getpeername(fd, $0, &len)
            }
        }
        if ret != 0 || addr.sc_family != UInt8(AF_SYSTEM) { continue }
        if ctlInfo.ctl_id == 0 {
            ret = ioctl(fd, CTLIOCGINFO_VALUE, &ctlInfo)
            if ret != 0 { continue }
        }
        if addr.sc_id == ctlInfo.ctl_id { return fd }
    }
    return nil
}

// stripPort is in NEHelpers.swift

class PacketTunnelProvider: NEPacketTunnelProvider {
    private var engine: AppextEngine?
    private var memoryTimer: DispatchSourceTimer?
    private var engineStartTime: Date?
    private var lastSleepTime: Date?
    private var lastWakeTime: Date?
    private var pathMonitor: NWPathMonitor?
    private let neDefaults = UserDefaults(suiteName: kAppGroup)

    /// Permanent error categories — retry is pointless without user action.
    private static let permanentCategories: Set<String> = ["client"]
    /// Permanent error codes — ConnectionFatal (570) regardless of category.
    private static let permanentCodes: Set<Int> = [570]
    /// Cooldown after permanent error (auth, payment, fatal) — blocks on-demand restart.
    private static let permanentErrorCooldown: TimeInterval = 30
    /// Cooldown after transient error (network, server) — prevents rapid on-demand loop.
    private static let transientErrorCooldown: TimeInterval = 5
    /// Max credible cooldown — clock-change protection (any cooldown further out is bogus).
    private static let maxCooldownGuard: TimeInterval = 60

    override func startTunnel(options: [String: NSObject]?, completionHandler: @escaping (Error?) -> Void) {
        logger.info("startTunnel called, options keys: \(options?.keys.joined(separator: ",") ?? "nil")")

        // Cooldown: after an engine error, block on-demand restart for a short period
        // to prevent rapid connect→fail→restart loops that break WiFi.
        // User-initiated connects (via K2Plugin) skip cooldown.
        let userInitiated = (options?["userInitiated"] as? NSNumber)?.boolValue ?? false
        if !userInitiated, let cooldownUntil = neDefaults?.double(forKey: "errorCooldownUntil"), cooldownUntil > 0 {
            let now = Date().timeIntervalSince1970
            let remaining = cooldownUntil - now
            if remaining > 0 && remaining <= Self.maxCooldownGuard {
                logger.warning("startTunnel: error cooldown active (\(Int(remaining))s remaining), failing fast")
                NativeLogger.shared.log("WARN", "startTunnel: blocked by error cooldown (\(Int(remaining))s remaining)")
                completionHandler(NSError(domain: "com.allnationconnect.anc.wgios", code: 429,
                                          userInfo: [NSLocalizedDescriptionKey: "Error cooldown — too soon to retry"]))
                return
            }
            // Cooldown expired or invalid (clock change) — clear and proceed
            neDefaults?.removeObject(forKey: "errorCooldownUntil")
        }

        // Detect jetsam: if previous session wrote "engineRunning=true" but never set it to false,
        // the NE process was killed by iOS (jetsam/memory pressure) without calling stopTunnel.
        if neDefaults?.bool(forKey: "engineRunning") == true {
            let prevStart = neDefaults?.object(forKey: "engineStartTime") as? Date
            let uptime = prevStart.map { Int(Date().timeIntervalSince($0)) } ?? -1
            logger.warning("startTunnel: previous session did NOT call stopTunnel (likely jetsam), uptime=\(uptime)s")
            NativeLogger.shared.log("WARN", "startTunnel: previous session ended without stopTunnel (jetsam?), uptime=\(uptime)s")
        }

        let configJSON: String
        if let config = options?["configJSON"] as? String {
            logger.info("Got configJSON from options (\(config.count) bytes)")
            configJSON = config
        } else if let config = (protocolConfiguration as? NETunnelProviderProtocol)?.providerConfiguration?["configJSON"] as? String {
            logger.info("Got configJSON from providerConfiguration (\(config.count) bytes)")
            configJSON = config
        } else if let config = neDefaults?.string(forKey: "configJSON"), !config.isEmpty {
            // On-demand restart: system calls startTunnel without options.
            // Config was saved to App Group by K2Plugin.connect().
            logger.info("Got configJSON from App Group (\(config.count) bytes)")
            configJSON = config
        } else {
            logger.error("Missing configJSON in options, providerConfiguration, and App Group")
            completionHandler(NSError(domain: "com.allnationconnect.anc.wgios", code: 1, userInfo: [NSLocalizedDescriptionKey: "Missing configJSON"]))
            return
        }

        // Clean old engine before creating new one
        if engine != nil {
            logger.info("Stopping old engine")
            do {
                try engine?.stop()
            } catch {
                logger.warning("Failed to stop old engine: \(error)")
            }
            engine = nil
        }

        logger.info("Creating AppextEngine")
        engine = AppextNewEngine()

        let handler = EventBridge(provider: self)
        engine?.setEventHandler(handler)

        // Configure network settings first, then get TUN fd in completion
        let clientConfig = parseClientConfig(from: configJSON)
        let settings = buildNetworkSettings(from: clientConfig)
        logger.info("Setting tunnel network settings")

        setTunnelNetworkSettings(settings) { [weak self] error in
            if let error = error {
                logger.error("setTunnelNetworkSettings FAILED: \(error.localizedDescription)")
                completionHandler(error)
                return
            }
            logger.info("Network settings applied OK")

            // Get TUN fd AFTER network settings are applied.
            // Primary: KVC keypath (sing-box ExtensionPlatformInterface.swift:191)
            // Fallback: fd scan for utun control socket (WireGuard/sing-box approach)
            let fd: Int32
            if let kvcFd = self?.packetFlow.value(forKeyPath: "socket.fileDescriptor") as? Int32, kvcFd >= 0 {
                fd = kvcFd
                logger.info("Got TUN fd=\(fd) (KVC)")
            } else if let scanFd = findTunnelFileDescriptor() {
                fd = scanFd
                logger.info("Got TUN fd=\(fd) (fd scan)")
            } else {
                logger.error("Failed to get TUN fd (both KVC and fd scan failed)")
                let err = NSError(domain: "com.allnationconnect.anc.wgios", code: 2,
                                  userInfo: [NSLocalizedDescriptionKey: "Failed to get TUN fd"])
                completionHandler(err)
                return
            }

            do {
                // Build EngineConfig with platform paths
                guard let engineCfg = AppextNewEngineConfig() else {
                    logger.error("Failed to create EngineConfig")
                    let err = NSError(domain: "com.allnationconnect.anc.wgios", code: 3, userInfo: [NSLocalizedDescriptionKey: "Failed to create EngineConfig"])
                    completionHandler(err)
                    return
                }
                let containerURL = FileManager.default.containerURL(forSecurityApplicationGroupIdentifier: kAppGroup)
                engineCfg.cacheDir = containerURL?.appendingPathComponent("k2").path ?? ""
                logger.info("EngineConfig cacheDir=\(engineCfg.cacheDir)")

                if !engineCfg.cacheDir.isEmpty {
                    do {
                        try FileManager.default.createDirectory(atPath: engineCfg.cacheDir, withIntermediateDirectories: true)
                    } catch {
                        logger.warning("Failed to create cacheDir: \(error)")
                    }
                }

                // Create logs directory for Go engine (k2.log) and NativeLogger (native.log)
                let logsDir = containerURL?.appendingPathComponent("logs")
                if let logsPath = logsDir?.path {
                    do {
                        try FileManager.default.createDirectory(atPath: logsPath, withIntermediateDirectories: true)
                    } catch {
                        logger.warning("Failed to create logsDir: \(error)")
                    }
                    engineCfg.logDir = logsPath
                    #if DEBUG
                    engineCfg.debug = true
                    #endif
                    logger.info("EngineConfig logDir=\(logsPath)")
                }

                // Initialize NativeLogger for native-layer file logging
                if let logsDir = logsDir {
                    NativeLogger.shared.setup(logsDir: logsDir)
                    NativeLogger.shared.log("INFO", "startTunnel: NativeLogger initialized")
                }

                logger.info("Calling engine.start()")
                try self?.engine?.start(configJSON, fd: Int(fd), cfg: engineCfg)
                logger.info("engine.start() returned OK")
                NativeLogger.shared.log("INFO", "startTunnel: engine started successfully")
                // Clear any stale cooldown — engine is running, on-demand should work normally
                self?.neDefaults?.removeObject(forKey: "errorCooldownUntil")
                let now = Date()
                self?.engineStartTime = now
                self?.neDefaults?.set(true, forKey: "engineRunning")
                self?.neDefaults?.set(now, forKey: "engineStartTime")
                self?.startMemoryMonitor()
                self?.startPathMonitor()
                completionHandler(nil)
            } catch {
                logger.error("engine.start() FAILED: \(error.localizedDescription, privacy: .public)")
                NativeLogger.shared.log("ERROR", "startTunnel: engine.start() failed: \(error.localizedDescription)")
                // Write transient cooldown to prevent rapid on-demand restart on persistent start failures
                self?.neDefaults?.set(
                    Date().timeIntervalSince1970 + PacketTunnelProvider.transientErrorCooldown,
                    forKey: "errorCooldownUntil"
                )
                completionHandler(error)
            }
        }
    }

    private func parseClientConfig(from configJSON: String) -> ClientConfigSubset? {
        guard let data = configJSON.data(using: .utf8) else {
            logger.info("Could not encode configJSON to UTF-8 data")
            return nil
        }
        do {
            return try JSONDecoder().decode(ClientConfigSubset.self, from: data)
        } catch {
            logger.info("Could not parse ClientConfig subset, using defaults: \(error)")
            return nil
        }
    }

    private func buildNetworkSettings(from config: ClientConfigSubset?) -> NEPacketTunnelNetworkSettings {
        // IPv4: parse from config or use defaults
        let (ipv4Addr, ipv4Mask) = parseIPv4CIDR(config?.tun?.ipv4 ?? "10.0.0.2/24") ?? ("10.0.0.2", "255.255.255.0")
        // IPv6: parse from config or use defaults
        let (ipv6Addr, ipv6Prefix) = parseIPv6CIDR(config?.tun?.ipv6 ?? "fd00::2/64") ?? ("fd00::2", 64)

        let settings = NEPacketTunnelNetworkSettings(tunnelRemoteAddress: "10.0.0.1")

        // IPv4
        settings.ipv4Settings = NEIPv4Settings(addresses: [ipv4Addr], subnetMasks: [ipv4Mask])
        settings.ipv4Settings?.includedRoutes = [NEIPv4Route.default()]

        // IPv6 — capture default route so engine drops IPv6 (prevents DNS leak)
        settings.ipv6Settings = NEIPv6Settings(addresses: [ipv6Addr], networkPrefixLengths: [ipv6Prefix as NSNumber])
        settings.ipv6Settings?.includedRoutes = [NEIPv6Route.default()]

        // DNS — use proxy DNS servers from config (strip ports for NEDNSSettings), or defaults
        let dnsServers = config?.dns?.proxy?.map { stripPort($0) }.filter { !$0.isEmpty }
        settings.dnsSettings = NEDNSSettings(servers: (dnsServers?.isEmpty == false) ? dnsServers! : ["1.1.1.1", "8.8.8.8"])

        // MTU — matches Go DefaultMTU (not in ClientConfig)
        settings.mtu = NSNumber(value: 1400)

        return settings
    }

    override func stopTunnel(with reason: NEProviderStopReason, completionHandler: @escaping () -> Void) {
        let reasonName = stopReasonName(reason)
        let uptime = engineStartTime.map { Int(Date().timeIntervalSince($0)) } ?? -1
        logger.info("stopTunnel: reason=\(reasonName)(\(reason.rawValue)) uptime=\(uptime)s")
        NativeLogger.shared.log("INFO", "stopTunnel: reason=\(reasonName)(\(reason.rawValue)) uptime=\(uptime)s")
        neDefaults?.set(false, forKey: "engineRunning")
        stopMemoryMonitor()
        stopPathMonitor()
        do {
            try engine?.stop()
            NativeLogger.shared.log("INFO", "stopTunnel: engine stopped")
        } catch {
            logger.warning("engine.stop() failed: \(error)")
            NativeLogger.shared.log("ERROR", "stopTunnel: engine.stop() failed: \(error)")
        }
        engine = nil
        engineStartTime = nil
        neDefaults?.removeObject(forKey: "engineStartTime")
        NativeLogger.shared.close()
        completionHandler()
    }

    private func stopReasonName(_ reason: NEProviderStopReason) -> String {
        switch reason {
        case .none: return "none"
        case .userInitiated: return "userInitiated"
        case .providerFailed: return "providerFailed"
        case .noNetworkAvailable: return "noNetworkAvailable"
        case .unrecoverableNetworkChange: return "unrecoverableNetworkChange"
        case .providerDisabled: return "providerDisabled"
        case .authenticationCanceled: return "authenticationCanceled"
        case .configurationFailed: return "configurationFailed"
        case .idleTimeout: return "idleTimeout"
        case .configurationDisabled: return "configurationDisabled"
        case .configurationRemoved: return "configurationRemoved"
        case .superceded: return "superceded"
        case .userLogout: return "userLogout"
        case .userSwitch: return "userSwitch"
        case .connectionFailed: return "connectionFailed"
        case .sleep: return "sleep"
        case .appUpdate: return "appUpdate"
        @unknown default: return "unknown(\(reason.rawValue))"
        }
    }

    // MARK: - Memory Monitoring

    private func startMemoryMonitor() {
        stopMemoryMonitor()
        let timer = DispatchSource.makeTimerSource(queue: .global(qos: .utility))
        timer.schedule(deadline: .now() + 10, repeating: 10)
        timer.setEventHandler { [weak self] in
            guard self != nil else { return }
            let snapshot = AppextMemorySnapshot()
            logger.notice("MEM: \(snapshot, privacy: .public)")
        }
        timer.resume()
        memoryTimer = timer
    }

    private func stopMemoryMonitor() {
        memoryTimer?.cancel()
        memoryTimer = nil
    }

    // MARK: - Memory Pressure

    /// Called by iOS when the NE process should reduce memory usage.
    /// Pauses the engine to release wire transport resources, then frees Go's heap.
    override func sleep(completionHandler: @escaping () -> Void) {
        let sinceWake = lastWakeTime.map { String(format: "%.1f", Date().timeIntervalSince($0)) } ?? "n/a"
        let mem = AppextMemorySnapshot()
        logger.info("sleep: pausing engine (sinceWake=\(sinceWake)s, mem=\(mem, privacy: .public))")
        NativeLogger.shared.log("INFO", "sleep: pausing engine (sinceWake=\(sinceWake)s)")
        lastSleepTime = Date()
        stopMemoryMonitor()
        engine?.pause()
        AppextFreeMemory()
        completionHandler()
    }

    /// Called by iOS when the NE process can resume normal operation.
    /// Wakes the engine so it re-establishes fresh wire connections.
    override func wake() {
        let sinceSleep = lastSleepTime.map { String(format: "%.1f", Date().timeIntervalSince($0)) } ?? "n/a"
        logger.info("wake: resuming engine (sinceSleep=\(sinceSleep)s)")
        NativeLogger.shared.log("INFO", "wake: resuming engine (sinceSleep=\(sinceSleep)s)")
        lastWakeTime = Date()
        engine?.wake()
    }

    override func handleAppMessage(_ messageData: Data, completionHandler: ((Data?) -> Void)?) {
        guard let command = String(data: messageData, encoding: .utf8) else {
            completionHandler?(nil)
            return
        }

        switch command {
        case "status":
            let json = engine?.statusJSON() ?? "{\"state\":\"disconnected\"}"
            completionHandler?(json.data(using: .utf8))
        case "error":
            let defaults = UserDefaults(suiteName: kAppGroup)
            let error = defaults?.string(forKey: "vpnError") ?? ""
            defaults?.removeObject(forKey: "vpnError")
            completionHandler?(error.data(using: .utf8))
        default:
            completionHandler?(nil)
        }
    }

    // MARK: - Network Path Monitor

    private func startPathMonitor() {
        let monitor = NWPathMonitor()
        monitor.pathUpdateHandler = { [weak self] path in
            guard let self = self else { return }

            // Set reasserting BEFORE engine notify — protects NE from iOS kill
            // during WiFi transitions. iOS 17+: 5-min timeout before auto-disconnect.
            if path.status != .satisfied {
                self.reasserting = true
                NativeLogger.shared.log("INFO", "reasserting: true (network unsatisfied)")
            }

            guard let event = AppextNewNetEvent() else { return }
            event.source = "nwpath"
            event.isWifi = path.usesInterfaceType(.wifi) || path.usesInterfaceType(.wiredEthernet)
            event.isCellular = path.usesInterfaceType(.cellular)
            event.hasIPv4 = path.supportsIPv4
            event.hasIPv6 = path.supportsIPv6
            if path.status == .satisfied {
                event.signal = "available"
            } else {
                event.signal = "unavailable"
            }
            self.engine?.notify(event)

            // Clear reasserting AFTER engine notify — engine starts reconnecting first
            if path.status == .satisfied {
                self.reasserting = false
                NativeLogger.shared.log("INFO", "reasserting: false (network satisfied)")
            }
        }
        monitor.start(queue: .main)
        pathMonitor = monitor
    }

    private func stopPathMonitor() {
        pathMonitor?.cancel()
        pathMonitor = nil
    }
}

// MARK: - EventBridge

class EventBridge: NSObject, AppextEventHandlerProtocol {
    weak var provider: PacketTunnelProvider?

    init(provider: PacketTunnelProvider) {
        self.provider = provider
    }

    func onStatus(_ statusJSON: String?) {
        guard let json = statusJSON,
              let data = json.data(using: .utf8),
              let parsed = try? JSONSerialization.jsonObject(with: data) as? [String: Any],
              let state = parsed["state"] as? String else {
            logger.warning("onStatus: invalid or nil JSON: \(statusJSON ?? "nil")")
            return
        }

        logger.debug("onStatus: state=\(state, privacy: .public)")
        NativeLogger.shared.log("DEBUG", "onStatus: \(json)")

        if state == "disconnected" {
            if let errorObj = parsed["error"] as? [String: Any] {
                let code = errorObj["code"] as? Int ?? 0
                let category = errorObj["category"] as? String ?? ""
                let message = errorObj["message"] as? String ?? "unknown error"
                logger.error("Disconnected with error: code=\(code) category=\(category, privacy: .public) message=\(message, privacy: .public)")
                NativeLogger.shared.log("ERROR", "onStatus: disconnected with error code=\(code) category=\(category) message=\(message)")

                // Write structured error JSON to App Group so K2Plugin preserves the error code.
                // Format: {"code": 503, "message": "server unreachable"}
                if let errorJSON = try? JSONSerialization.data(withJSONObject: errorObj),
                   let errorStr = String(data: errorJSON, encoding: .utf8) {
                    UserDefaults(suiteName: kAppGroup)?.set(errorStr, forKey: "vpnError")
                } else {
                    UserDefaults(suiteName: kAppGroup)?.set(message, forKey: "vpnError")
                }

                let isPermanent = PacketTunnelProvider.permanentCategories.contains(category)
                    || PacketTunnelProvider.permanentCodes.contains(code)
                let cooldown = isPermanent
                    ? PacketTunnelProvider.permanentErrorCooldown
                    : PacketTunnelProvider.transientErrorCooldown

                // Write cooldown to App Group — blocks on-demand restart for the specified duration.
                // Permanent errors (auth, payment): 30s cooldown — user action needed.
                // Transient errors (network, server): 5s cooldown — prevents rapid loop, allows quick recovery.
                let defaults = UserDefaults(suiteName: kAppGroup)
                defaults?.set(Date().timeIntervalSince1970 + cooldown, forKey: "errorCooldownUntil")
                logger.error("Cancelling tunnel: isPermanent=\(isPermanent) cooldown=\(Int(cooldown))s (code=\(code) category=\(category, privacy: .public))")
                NativeLogger.shared.log("ERROR", "onStatus: cancelling tunnel, isPermanent=\(isPermanent) cooldown=\(Int(cooldown))s")

                let nsError = NSError(domain: "com.allnationconnect.anc.wgios", code: code,
                                      userInfo: [NSLocalizedDescriptionKey: message])
                provider?.cancelTunnelWithError(nsError)
            } else {
                logger.info("Normal disconnect")
                NativeLogger.shared.log("INFO", "onStatus: normal disconnect")
                provider?.cancelTunnelWithError(nil)
            }
        } else if state == "connected", let errorObj = parsed["error"] as? [String: Any] {
            // Wire error while TUN is up — write to App Group for K2Plugin polling to pick up.
            // Do NOT call cancelTunnelWithError — tunnel is still running.
            let code = errorObj["code"] as? Int ?? 0
            let message = errorObj["message"] as? String ?? "unknown error"
            logger.error("Connected with wire error: code=\(code) message=\(message, privacy: .public)")
            if let errorJSON = try? JSONSerialization.data(withJSONObject: errorObj),
               let errorStr = String(data: errorJSON, encoding: .utf8) {
                UserDefaults(suiteName: kAppGroup)?.set(errorStr, forKey: "vpnError")
            }
        } else if state == "connected" {
            // Wire recovered — clear stale error and cooldown
            NativeLogger.shared.log("INFO", "onStatus: connected")
            UserDefaults(suiteName: kAppGroup)?.removeObject(forKey: "vpnError")
        }
        // Other states (connecting, reconnecting, paused) are transient — log only
    }

    func onStats(_ txBytes: Int64, rxBytes: Int64) {
        // Stats tracking if needed
    }
}
