import Network
import NetworkExtension
import K2Mobile  // gomobile xcframework

private let kAppGroup = "group.io.kaitu"

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

/// Parse IPv4 CIDR "10.0.0.2/24" → ("10.0.0.2", "255.255.255.0"), nil on failure.
private func parseIPv4CIDR(_ cidr: String) -> (String, String)? {
    let parts = cidr.split(separator: "/", maxSplits: 1)
    guard parts.count == 2, let prefix = Int(parts[1]), prefix >= 0, prefix <= 32 else { return nil }
    let mask = prefix == 0 ? UInt32(0) : UInt32.max << (32 - prefix)
    let m = String(format: "%d.%d.%d.%d",
                   (mask >> 24) & 0xFF, (mask >> 16) & 0xFF, (mask >> 8) & 0xFF, mask & 0xFF)
    return (String(parts[0]), m)
}

/// Parse IPv6 CIDR "fd00::2/64" → ("fd00::2", 64), nil on failure.
private func parseIPv6CIDR(_ cidr: String) -> (String, Int)? {
    let parts = cidr.split(separator: "/", maxSplits: 1)
    guard parts.count == 2, let prefix = Int(parts[1]), prefix >= 0, prefix <= 128 else { return nil }
    return (String(parts[0]), prefix)
}

/// Strip port from "8.8.8.8:53" → "8.8.8.8". Handles IPv6 "[::1]:53" → "::1".
private func stripPort(_ addr: String) -> String {
    if addr.hasPrefix("["), let closeBracket = addr.firstIndex(of: "]") {
        return String(addr[addr.index(after: addr.startIndex)..<closeBracket])
    }
    let parts = addr.split(separator: ":")
    if parts.count == 2 { return String(parts[0]) } // "ip:port"
    return addr // bare IP or IPv6 without port
}

class PacketTunnelProvider: NEPacketTunnelProvider {
    private var engine: MobileEngine?
    private var pathMonitor: NWPathMonitor?
    private var pendingNetworkChange: DispatchWorkItem?

    override func startTunnel(options: [String: NSObject]?, completionHandler: @escaping (Error?) -> Void) {
        let configJSON: String
        if let config = options?["configJSON"] as? String {
            configJSON = config
        } else if let config = (protocolConfiguration as? NETunnelProviderProtocol)?.providerConfiguration?["configJSON"] as? String {
            configJSON = config
        } else {
            completionHandler(NSError(domain: "io.kaitu", code: 1, userInfo: [NSLocalizedDescriptionKey: "Missing configJSON"]))
            return
        }

        // Clean old engine before creating new one
        if engine != nil {
            try? engine?.stop()
            engine = nil
        }

        engine = MobileNewEngine()

        let handler = EventBridge(provider: self)
        engine?.setEventHandler(handler)

        // Configure network settings first, then get TUN fd in completion
        let clientConfig = parseClientConfig(from: configJSON)
        let settings = buildNetworkSettings(from: clientConfig)

        setTunnelNetworkSettings(settings) { [weak self] error in
            if let error = error {
                completionHandler(error)
                return
            }

            // Get TUN fd AFTER network settings are applied
            guard let fd = self?.packetFlow.value(forKey: "socket") as? Int32, fd >= 0 else {
                let err = NSError(domain: "io.kaitu", code: 2, userInfo: [NSLocalizedDescriptionKey: "Failed to get TUN fd"])
                completionHandler(err)
                return
            }

            do {
                // Build EngineConfig with platform paths
                guard let engineCfg = MobileNewEngineConfig() else {
                    let err = NSError(domain: "io.kaitu", code: 3, userInfo: [NSLocalizedDescriptionKey: "Failed to create EngineConfig"])
                    completionHandler(err)
                    return
                }
                let containerURL = FileManager.default.containerURL(forSecurityApplicationGroupIdentifier: kAppGroup)
                engineCfg.cacheDir = containerURL?.appendingPathComponent("k2").path ?? ""
                // socketProtector left nil — iOS PacketTunnelProvider self-excludes at kernel level

                if !engineCfg.cacheDir.isEmpty {
                    try? FileManager.default.createDirectory(atPath: engineCfg.cacheDir, withIntermediateDirectories: true)
                }

                try self?.engine?.start(configJSON, fd: Int(fd), cfg: engineCfg)
                self?.startMonitoringNetwork()
                completionHandler(nil)
            } catch {
                completionHandler(error)
            }
        }
    }

    private func parseClientConfig(from configJSON: String) -> ClientConfigSubset? {
        guard let data = configJSON.data(using: .utf8) else { return nil }
        return try? JSONDecoder().decode(ClientConfigSubset.self, from: data)
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
        stopMonitoringNetwork()
        try? engine?.stop()
        engine = nil
        completionHandler()
    }

    // MARK: - Network Monitoring

    private func startMonitoringNetwork() {
        let monitor = NWPathMonitor()
        monitor.pathUpdateHandler = { [weak self] path in
            guard path.status == .satisfied else { return }
            NSLog("[PacketTunnel] Network path satisfied, scheduling engine reset")
            // Debounce: cancel pending, schedule new after 500ms
            self?.pendingNetworkChange?.cancel()
            let workItem = DispatchWorkItem { [weak self] in
                NSLog("[PacketTunnel] Triggering engine onNetworkChanged")
                self?.engine?.onNetworkChanged()
            }
            self?.pendingNetworkChange = workItem
            DispatchQueue.global(qos: .utility).asyncAfter(deadline: .now() + 0.5, execute: workItem)
        }
        monitor.start(queue: DispatchQueue.global(qos: .utility))
        pathMonitor = monitor
        NSLog("[PacketTunnel] Network path monitor started")
    }

    private func stopMonitoringNetwork() {
        pendingNetworkChange?.cancel()
        pendingNetworkChange = nil
        pathMonitor?.cancel()
        pathMonitor = nil
    }

    override func handleAppMessage(_ messageData: Data, completionHandler: ((Data?) -> Void)?) {
        guard let command = String(data: messageData, encoding: .utf8) else {
            completionHandler?(nil)
            return
        }

        switch command {
        case "status":
            let json = engine?.statusJSON() ?? "{\"state\":\"stopped\"}"
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
}

// MARK: - EventBridge

class EventBridge: NSObject, MobileEventHandlerProtocol {
    weak var provider: PacketTunnelProvider?
    private var hasReportedError = false

    init(provider: PacketTunnelProvider) {
        self.provider = provider
    }

    func onStateChange(_ state: String?) {
        guard let state = state else { return }
        if state == "connecting" {
            // New connection cycle — reset error flag
            hasReportedError = false
        } else if state == "disconnected" {
            if hasReportedError {
                // Error already reported and cancelTunnelWithError already called — skip
                // to avoid nil-cancel overwriting the error in App Group
                return
            }
            // Normal disconnect — notify system
            provider?.cancelTunnelWithError(nil)
        } else {
            // Log transient states (reconnecting, connected from OnNetworkChanged)
            // for debug observability. Not propagated to App process.
            NSLog("[K2:NE] transient state: %@", state)
        }
    }

    func onError(_ message: String?) {
        guard let message = message else { return }
        hasReportedError = true
        // Write error to App Group so main app can read it
        UserDefaults(suiteName: kAppGroup)?.set(message, forKey: "vpnError")
        // Notify system that tunnel has failed
        let error = NSError(domain: "io.kaitu", code: 100, userInfo: [NSLocalizedDescriptionKey: message])
        provider?.cancelTunnelWithError(error)
    }

    func onStats(_ txBytes: Int64, rxBytes: Int64) {
        // Stats tracking if needed
    }
}
