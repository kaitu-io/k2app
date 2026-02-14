import NetworkExtension
import K2Mobile  // gomobile xcframework

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

        engine = MobileNewEngine()

        let handler = EventBridge(provider: self)
        engine?.setEventHandler(handler)

        // Get TUN file descriptor from packetFlow
        guard let fd = self.packetFlow.value(forKey: "socket") as? Int32 else {
            completionHandler(NSError(domain: "io.kaitu", code: 2, userInfo: [NSLocalizedDescriptionKey: "Failed to get TUN fd"]))
            return
        }

        // Configure network settings
        let settings = NEPacketTunnelNetworkSettings(tunnelRemoteAddress: "10.0.0.1")
        settings.ipv4Settings = NEIPv4Settings(addresses: ["10.0.0.2"], subnetMasks: ["255.255.255.0"])
        settings.ipv4Settings?.includedRoutes = [NEIPv4Route.default()]
        settings.dnsSettings = NEDNSSettings(servers: ["1.1.1.1", "8.8.8.8"])
        settings.mtu = 1400

        setTunnelNetworkSettings(settings) { [weak self] error in
            if let error = error {
                completionHandler(error)
                return
            }

            var startError: NSError?
            self?.engine?.start(wireUrl, fd: Int(fd), error: &startError)
            if let error = startError {
                completionHandler(error)
            } else {
                // Save state to App Group
                UserDefaults(suiteName: "group.io.kaitu")?.set("connecting", forKey: "vpnState")
                completionHandler(nil)
            }
        }
    }

    override func stopTunnel(with reason: NEProviderStopReason, completionHandler: @escaping () -> Void) {
        engine?.stop()
        engine = nil
        UserDefaults(suiteName: "group.io.kaitu")?.set("stopped", forKey: "vpnState")
        completionHandler()
    }

    override func handleAppMessage(_ messageData: Data, completionHandler: ((Data?) -> Void)?) {
        guard let command = String(data: messageData, encoding: .utf8) else {
            completionHandler?(nil)
            return
        }

        if command == "status" {
            let json = engine?.statusJSON() ?? "{\"state\":\"stopped\"}"
            completionHandler?(json.data(using: .utf8))
        } else {
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
        let mapped = state == "disconnected" ? "stopped" : state
        UserDefaults(suiteName: "group.io.kaitu")?.set(mapped, forKey: "vpnState")
    }

    func onError(_ message: String?) {
        guard let message = message else { return }
        UserDefaults(suiteName: "group.io.kaitu")?.set(message, forKey: "vpnError")
    }

    func onStats(_ txBytes: Int64, rxBytes: Int64) {
        // Stats tracking if needed
    }
}
