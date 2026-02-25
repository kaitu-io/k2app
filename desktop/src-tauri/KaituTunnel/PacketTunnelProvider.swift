import Foundation
import Network
import NetworkExtension
import K2MobileMacOS  // gomobile xcframework (macOS target, appext/ package)

private let kAppGroup = "group.io.kaitu.desktop"

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

/// CTLIOCGINFO = _IOWR('N', 3, struct ctl_info) — Swift can't import this macro directly.
/// Computed: IOC_INOUT(0xC0000000) | sizeof(ctl_info=100)<<16 | 'N'(0x4E)<<8 | 3
private let CTLIOCGINFO_VALUE: UInt = 0xC0644E03

/// Find the utun file descriptor by scanning open fds (WireGuard approach).
/// Works reliably in both App Extensions and System Extensions.
private func findTunnelFileDescriptor() -> Int32? {
    var ctlInfo = ctl_info()
    withUnsafeMutablePointer(to: &ctlInfo.ctl_name) {
        $0.withMemoryRebound(to: CChar.self, capacity: MemoryLayout.size(ofValue: $0.pointee)) {
            _ = strcpy($0, "com.apple.net.utun_control")
        }
    }
    for fd: Int32 in 0...1024 {
        var addr = sockaddr_ctl()
        var ret: Int32 = -1
        var len = socklen_t(MemoryLayout.size(ofValue: addr))
        withUnsafeMutablePointer(to: &addr) {
            $0.withMemoryRebound(to: sockaddr.self, capacity: 1) {
                ret = getpeername(fd, $0, &len)
            }
        }
        if ret != 0 || addr.sc_family != AF_SYSTEM { continue }
        if ctlInfo.ctl_id == 0 {
            ret = ioctl(fd, CTLIOCGINFO_VALUE, &ctlInfo)
            if ret != 0 { continue }
        }
        if addr.sc_id == ctlInfo.ctl_id { return fd }
    }
    return nil
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

/// Apple NetworkExtension MTU ceiling (4096 - UTUN_IF_HEADROOM_SIZE).
/// Must match Go provider.NetworkExtensionMTU.
private let networkExtensionMTU = 4064

class PacketTunnelProvider: NEPacketTunnelProvider {
    private var engine: AppextEngine?
    private var pathMonitor: NWPathMonitor?
    private var pendingNetworkChange: DispatchWorkItem?

    override func startTunnel(options: [String: NSObject]?, completionHandler: @escaping (Error?) -> Void) {
        NSLog("[KaituTunnel] startTunnel called, options keys: %@", options?.keys.joined(separator: ", ") ?? "nil")

        let configJSON: String
        if let config = options?["configJSON"] as? String {
            NSLog("[KaituTunnel] configJSON from options (len=%d)", config.count)
            configJSON = config
        } else if let config = (protocolConfiguration as? NETunnelProviderProtocol)?.providerConfiguration?["configJSON"] as? String {
            NSLog("[KaituTunnel] configJSON from providerConfiguration (len=%d)", config.count)
            configJSON = config
        } else {
            NSLog("[KaituTunnel] ERROR: Missing configJSON")
            completionHandler(NSError(domain: "io.kaitu.desktop", code: 1, userInfo: [NSLocalizedDescriptionKey: "Missing configJSON"]))
            return
        }

        // Clean old engine before creating new one
        if engine != nil {
            NSLog("[KaituTunnel] Cleaning old engine")
            try? engine?.stop()
            engine = nil
        }

        NSLog("[KaituTunnel] Creating AppextNewEngine")
        engine = AppextNewEngine()
        NSLog("[KaituTunnel] Engine created: %@", engine != nil ? "ok" : "nil")

        let handler = EventBridge(provider: self)
        engine?.setEventHandler(handler)

        // Configure network settings first, then get TUN fd in completion
        let clientConfig = parseClientConfig(from: configJSON)
        let settings = buildNetworkSettings(from: clientConfig)
        NSLog("[KaituTunnel] Calling setTunnelNetworkSettings")

        setTunnelNetworkSettings(settings) { [weak self] error in
            if let error = error {
                NSLog("[KaituTunnel] ERROR: setTunnelNetworkSettings failed: %@", error.localizedDescription)
                completionHandler(error)
                return
            }
            NSLog("[KaituTunnel] Network settings applied successfully")

            // Get TUN fd AFTER network settings are applied.
            // Try KVC first (works in App Extensions), fall back to fd scan (WireGuard approach,
            // works in System Extensions where KVC may return nil).
            let fd: Int32
            if let kvcFd = self?.packetFlow.value(forKey: "socket") as? Int32, kvcFd >= 0 {
                NSLog("[KaituTunnel] TUN fd via KVC: %d", kvcFd)
                fd = kvcFd
            } else if let scanFd = findTunnelFileDescriptor() {
                NSLog("[KaituTunnel] TUN fd via utun scan: %d", scanFd)
                fd = scanFd
            } else {
                NSLog("[KaituTunnel] ERROR: Failed to get TUN fd (both KVC and utun scan failed)")
                let err = NSError(domain: "io.kaitu.desktop", code: 2, userInfo: [NSLocalizedDescriptionKey: "Failed to get TUN fd"])
                completionHandler(err)
                return
            }

            do {
                // Build EngineConfig with platform paths (App Group shared container)
                guard let engineCfg = AppextNewEngineConfig() else {
                    NSLog("[KaituTunnel] ERROR: Failed to create EngineConfig")
                    let err = NSError(domain: "io.kaitu.desktop", code: 3, userInfo: [NSLocalizedDescriptionKey: "Failed to create EngineConfig"])
                    completionHandler(err)
                    return
                }
                let containerURL = FileManager.default.containerURL(forSecurityApplicationGroupIdentifier: kAppGroup)
                engineCfg.cacheDir = containerURL?.appendingPathComponent("k2").path ?? ""
                NSLog("[KaituTunnel] cacheDir: %@", engineCfg.cacheDir.isEmpty ? "(empty)" : engineCfg.cacheDir)

                if !engineCfg.cacheDir.isEmpty {
                    try? FileManager.default.createDirectory(atPath: engineCfg.cacheDir, withIntermediateDirectories: true)
                }

                // Redirect stderr to a file so we can capture Go panic output
                let stderrPath = (engineCfg.cacheDir as NSString).deletingLastPathComponent + "/go_stderr.log"
                NSLog("[KaituTunnel] Redirecting stderr to: %@", stderrPath)
                let fp = freopen(stderrPath, "w", stderr)
                NSLog("[KaituTunnel] freopen result: %@", fp != nil ? "ok" : "FAILED")

                // Diagnostic: dump exact configJSON to App Group for post-mortem
                let diagPath = (engineCfg.cacheDir as NSString).deletingLastPathComponent + "/diag_configJSON.txt"
                try? configJSON.write(toFile: diagPath, atomically: true, encoding: .utf8)
                NSLog("[KaituTunnel] configJSON first 200 chars: %@", String(configJSON.prefix(200)))
                NSLog("[KaituTunnel] configJSON len=%d, contains k2v5=%@", configJSON.count, configJSON.contains("k2v5") ? "YES" : "NO")

                NSLog("[KaituTunnel] Calling engine.start(fd=%d)", fd)
                try self?.engine?.start(configJSON, fd: Int(fd), cfg: engineCfg)
                NSLog("[KaituTunnel] Engine started successfully")
                self?.startMonitoringNetwork()
                completionHandler(nil)
            } catch {
                NSLog("[KaituTunnel] ERROR: engine.start failed: %@", error.localizedDescription)
                completionHandler(error)
            }
        }
    }

    private func parseClientConfig(from configJSON: String) -> ClientConfigSubset? {
        guard let data = configJSON.data(using: .utf8) else { return nil }
        return try? JSONDecoder().decode(ClientConfigSubset.self, from: data)
    }

    private func buildNetworkSettings(from config: ClientConfigSubset?) -> NEPacketTunnelNetworkSettings {
        // IPv4/IPv6: parse from config or use defaults aligned with Go config.DefaultTunIPv4/IPv6.
        let (ipv4Addr, ipv4Mask) = parseIPv4CIDR(config?.tun?.ipv4 ?? "198.18.0.7/15") ?? ("198.18.0.7", "254.0.0.0")
        let (ipv6Addr, ipv6Prefix) = parseIPv6CIDR(config?.tun?.ipv6 ?? "fdfe:dcba:9876::7/64") ?? ("fdfe:dcba:9876::7", 64)

        let settings = NEPacketTunnelNetworkSettings(tunnelRemoteAddress: "10.0.0.1")

        // IPv4 — capture default route
        settings.ipv4Settings = NEIPv4Settings(addresses: [ipv4Addr], subnetMasks: [ipv4Mask])
        settings.ipv4Settings?.includedRoutes = [NEIPv4Route.default()]

        // IPv6 — capture default route so engine drops IPv6 (prevents DNS leak)
        settings.ipv6Settings = NEIPv6Settings(addresses: [ipv6Addr], networkPrefixLengths: [ipv6Prefix as NSNumber])
        settings.ipv6Settings?.includedRoutes = [NEIPv6Route.default()]

        // DNS — use proxy DNS servers from config (strip ports for NEDNSSettings), or defaults
        let dnsServers = config?.dns?.proxy?.map { stripPort($0) }.filter { !$0.isEmpty }
        let dnsSettings = NEDNSSettings(servers: (dnsServers?.isEmpty == false) ? dnsServers! : ["1.1.1.1", "8.8.8.8"])
        // CRITICAL for macOS: matchDomains = [""] hijacks all DNS queries at system level
        dnsSettings.matchDomains = [""]
        settings.dnsSettings = dnsSettings

        // MTU — Apple NE performance ceiling (matches Go provider.NetworkExtensionMTU)
        settings.mtu = NSNumber(value: networkExtensionMTU)

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
            NSLog("[KaituTunnel] Network path satisfied, scheduling engine reset")
            // Debounce: cancel pending, schedule new after 500ms
            self?.pendingNetworkChange?.cancel()
            let workItem = DispatchWorkItem { [weak self] in
                NSLog("[KaituTunnel] Triggering engine onNetworkChanged")
                self?.engine?.onNetworkChanged()
            }
            self?.pendingNetworkChange = workItem
            DispatchQueue.global(qos: .utility).asyncAfter(deadline: .now() + 0.5, execute: workItem)
        }
        monitor.start(queue: DispatchQueue.global(qos: .utility))
        pathMonitor = monitor
        NSLog("[KaituTunnel] Network path monitor started")
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
            NSLog("[KaituTunnel:NE] onStatus: invalid or nil JSON")
            return
        }

        NSLog("[KaituTunnel:NE] onStatus: state=%@", state)

        if state == "disconnected" {
            if let errorObj = parsed["error"] as? [String: Any] {
                let code = errorObj["code"] as? Int ?? 0
                let message = errorObj["message"] as? String ?? "unknown error"
                NSLog("[KaituTunnel:NE] Disconnected with error: code=%d message=%@", code, message)

                // Write error to App Group so main app can read it
                UserDefaults(suiteName: kAppGroup)?.set(message, forKey: "vpnError")

                let nsError = NSError(domain: "io.kaitu.desktop", code: code,
                                      userInfo: [NSLocalizedDescriptionKey: message])
                provider?.cancelTunnelWithError(nsError)
            } else {
                NSLog("[KaituTunnel:NE] Normal disconnect")
                provider?.cancelTunnelWithError(nil)
            }
        }
        // Other states (connecting, connected, reconnecting, paused) are transient — log only
    }

    func onStats(_ txBytes: Int64, rxBytes: Int64) {
        // Stats tracking if needed
    }
}

// MARK: - System Extension Entry Point
// Required: register this process as a Network Extension provider and keep it alive.
// Without this, the executable exits immediately and nesessionmanager never sees the provider.
autoreleasepool {
    NEProvider.startSystemExtensionMode()
}
dispatchMain()
