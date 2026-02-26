import NetworkExtension
import K2Mobile  // gomobile xcframework (appext/ package)

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
    private var engine: AppextEngine?

    override func startTunnel(options: [String: NSObject]?, completionHandler: @escaping (Error?) -> Void) {
        NSLog("[K2:NE] startTunnel called, options keys: %@", options?.keys.joined(separator: ",") ?? "nil")

        let configJSON: String
        if let config = options?["configJSON"] as? String {
            NSLog("[K2:NE] Got configJSON from options (%d bytes)", config.count)
            configJSON = config
        } else if let config = (protocolConfiguration as? NETunnelProviderProtocol)?.providerConfiguration?["configJSON"] as? String {
            NSLog("[K2:NE] Got configJSON from providerConfiguration (%d bytes)", config.count)
            configJSON = config
        } else {
            NSLog("[K2:NE] ERROR: Missing configJSON")
            completionHandler(NSError(domain: "io.kaitu", code: 1, userInfo: [NSLocalizedDescriptionKey: "Missing configJSON"]))
            return
        }

        // Clean old engine before creating new one
        if engine != nil {
            NSLog("[K2:NE] Stopping old engine")
            try? engine?.stop()
            engine = nil
        }

        NSLog("[K2:NE] Creating AppextEngine")
        engine = AppextNewEngine()

        let handler = EventBridge(provider: self)
        engine?.setEventHandler(handler)

        // Configure network settings first, then get TUN fd in completion
        let clientConfig = parseClientConfig(from: configJSON)
        let settings = buildNetworkSettings(from: clientConfig)
        NSLog("[K2:NE] Setting tunnel network settings")

        setTunnelNetworkSettings(settings) { [weak self] error in
            if let error = error {
                NSLog("[K2:NE] setTunnelNetworkSettings FAILED: %@", error.localizedDescription)
                completionHandler(error)
                return
            }
            NSLog("[K2:NE] Network settings applied OK")

            // Get TUN fd AFTER network settings are applied.
            // Primary: KVC keypath (sing-box ExtensionPlatformInterface.swift:191)
            // Fallback: fd scan for utun control socket (WireGuard/sing-box approach)
            let fd: Int32
            if let kvcFd = self?.packetFlow.value(forKeyPath: "socket.fileDescriptor") as? Int32, kvcFd >= 0 {
                fd = kvcFd
                NSLog("[K2:NE] Got TUN fd=%d (KVC)", fd)
            } else if let scanFd = findTunnelFileDescriptor() {
                fd = scanFd
                NSLog("[K2:NE] Got TUN fd=%d (fd scan)", fd)
            } else {
                NSLog("[K2:NE] ERROR: Failed to get TUN fd (both KVC and fd scan failed)")
                let err = NSError(domain: "io.kaitu", code: 2,
                                  userInfo: [NSLocalizedDescriptionKey: "Failed to get TUN fd"])
                completionHandler(err)
                return
            }

            do {
                // Build EngineConfig with platform paths
                guard let engineCfg = AppextNewEngineConfig() else {
                    NSLog("[K2:NE] ERROR: Failed to create EngineConfig")
                    let err = NSError(domain: "io.kaitu", code: 3, userInfo: [NSLocalizedDescriptionKey: "Failed to create EngineConfig"])
                    completionHandler(err)
                    return
                }
                let containerURL = FileManager.default.containerURL(forSecurityApplicationGroupIdentifier: kAppGroup)
                engineCfg.cacheDir = containerURL?.appendingPathComponent("k2").path ?? ""
                NSLog("[K2:NE] EngineConfig cacheDir=%@", engineCfg.cacheDir)

                if !engineCfg.cacheDir.isEmpty {
                    try? FileManager.default.createDirectory(atPath: engineCfg.cacheDir, withIntermediateDirectories: true)
                }

                NSLog("[K2:NE] Calling engine.start()")
                try self?.engine?.start(configJSON, fd: Int(fd), cfg: engineCfg)
                NSLog("[K2:NE] engine.start() returned OK")
                completionHandler(nil)
            } catch {
                NSLog("[K2:NE] engine.start() FAILED: %@", error.localizedDescription)
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
        NSLog("[K2:NE] stopTunnel called, reason=%ld", reason.rawValue)
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
            NSLog("[K2:NE] onStatus: invalid or nil JSON")
            return
        }

        NSLog("[K2:NE] onStatus: state=%@", state)

        if state == "disconnected" {
            if let errorObj = parsed["error"] as? [String: Any] {
                let code = errorObj["code"] as? Int ?? 0
                let message = errorObj["message"] as? String ?? "unknown error"
                NSLog("[K2:NE] Disconnected with error: code=%d message=%@", code, message)

                // Write structured error JSON to App Group so K2Plugin preserves the error code.
                // Format: {"code": 503, "message": "server unreachable"}
                if let errorJSON = try? JSONSerialization.data(withJSONObject: errorObj),
                   let errorStr = String(data: errorJSON, encoding: .utf8) {
                    UserDefaults(suiteName: kAppGroup)?.set(errorStr, forKey: "vpnError")
                } else {
                    UserDefaults(suiteName: kAppGroup)?.set(message, forKey: "vpnError")
                }

                let nsError = NSError(domain: "io.kaitu", code: code,
                                      userInfo: [NSLocalizedDescriptionKey: message])
                provider?.cancelTunnelWithError(nsError)
            } else {
                NSLog("[K2:NE] Normal disconnect")
                provider?.cancelTunnelWithError(nil)
            }
        }
        // Other states (connecting, connected, reconnecting, paused) are transient — log only
    }

    func onStats(_ txBytes: Int64, rxBytes: Int64) {
        // Stats tracking if needed
    }
}
