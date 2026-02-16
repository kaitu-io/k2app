import NetworkExtension
import K2Mobile  // gomobile xcframework

private let kAppGroup = "group.io.kaitu"

class PacketTunnelProvider: NEPacketTunnelProvider {
    private var engine: MobileEngine?

    override func startTunnel(options: [String: NSObject]?, completionHandler: @escaping (Error?) -> Void) {
        let wireUrl: String
        if let url = options?["wireUrl"] as? String {
            wireUrl = url
        } else if let url = (protocolConfiguration as? NETunnelProviderProtocol)?.providerConfiguration?["wireUrl"] as? String {
            wireUrl = url
        } else {
            completionHandler(NSError(domain: "io.kaitu", code: 1, userInfo: [NSLocalizedDescriptionKey: "Missing wireUrl"]))
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
        let settings = NEPacketTunnelNetworkSettings(tunnelRemoteAddress: "10.0.0.1")

        // IPv4
        settings.ipv4Settings = NEIPv4Settings(addresses: ["10.0.0.2"], subnetMasks: ["255.255.255.0"])
        settings.ipv4Settings?.includedRoutes = [NEIPv4Route.default()]

        // IPv6 — capture default route so engine drops IPv6 (prevents DNS leak)
        settings.ipv6Settings = NEIPv6Settings(addresses: ["fd00::2"], networkPrefixLengths: [64])
        settings.ipv6Settings?.includedRoutes = [NEIPv6Route.default()]

        settings.dnsSettings = NEDNSSettings(servers: ["1.1.1.1", "8.8.8.8"])
        settings.mtu = 1400

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

                try self?.engine?.start(wireUrl, fd: Int(fd), dataDir: dataDir)
                completionHandler(nil)
            } catch {
                completionHandler(error)
            }
        }
    }

    override func stopTunnel(with reason: NEProviderStopReason, completionHandler: @escaping () -> Void) {
        try? engine?.stop()
        engine = nil
        completionHandler()
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

    init(provider: PacketTunnelProvider) {
        self.provider = provider
    }

    func onStateChange(_ state: String?) {
        guard let state = state else { return }
        if state == "disconnected" {
            // Notify system that tunnel has stopped — triggers NEVPNStatusDidChange → .disconnected
            provider?.cancelTunnelWithError(nil)
        }
    }

    func onError(_ message: String?) {
        guard let message = message else { return }
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
