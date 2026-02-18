import Network
import NetworkExtension
import K2Mobile  // gomobile xcframework

private let kAppGroup = "group.io.kaitu"

private struct TunnelSettings: Codable {
    var dns: [String]?
    var mtu: Int?
    var tunnelRemoteAddress: String?
}

private struct ConfigWrapper: Codable {
    var tunnel: TunnelSettings?
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
        let tunnelSettings = parseTunnelSettings(from: configJSON)
        let settings = buildNetworkSettings(from: tunnelSettings)

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
                // Compute App Group storage path for k2rule cache
                let containerURL = FileManager.default.containerURL(forSecurityApplicationGroupIdentifier: kAppGroup)
                let dataDir = containerURL?.appendingPathComponent("k2").path ?? ""

                // Create directory if needed
                if !dataDir.isEmpty {
                    try? FileManager.default.createDirectory(atPath: dataDir, withIntermediateDirectories: true)
                }

                try self?.engine?.start(configJSON, fd: Int(fd), dataDir: dataDir)
                self?.startMonitoringNetwork()
                completionHandler(nil)
            } catch {
                completionHandler(error)
            }
        }
    }

    private func parseTunnelSettings(from configJSON: String) -> TunnelSettings? {
        guard let data = configJSON.data(using: .utf8),
              let wrapper = try? JSONDecoder().decode(ConfigWrapper.self, from: data) else {
            return nil
        }
        return wrapper.tunnel
    }

    private func buildNetworkSettings(from tunnelSettings: TunnelSettings?) -> NEPacketTunnelNetworkSettings {
        let remoteAddr = tunnelSettings?.tunnelRemoteAddress ?? "10.0.0.1"
        let settings = NEPacketTunnelNetworkSettings(tunnelRemoteAddress: remoteAddr)

        // IPv4
        settings.ipv4Settings = NEIPv4Settings(addresses: ["10.0.0.2"], subnetMasks: ["255.255.255.0"])
        settings.ipv4Settings?.includedRoutes = [NEIPv4Route.default()]

        // IPv6 — capture default route so engine drops IPv6 (prevents DNS leak)
        settings.ipv6Settings = NEIPv6Settings(addresses: ["fd00::2"], networkPrefixLengths: [64])
        settings.ipv6Settings?.includedRoutes = [NEIPv6Route.default()]

        // DNS — use custom if provided, else defaults
        let dnsServers = tunnelSettings?.dns ?? ["1.1.1.1", "8.8.8.8"]
        settings.dnsSettings = NEDNSSettings(servers: dnsServers)

        // MTU
        let mtu = tunnelSettings?.mtu ?? 1400
        settings.mtu = NSNumber(value: mtu)

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
