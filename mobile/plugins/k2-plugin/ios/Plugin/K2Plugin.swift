import Foundation
import Capacitor
import NetworkExtension

@objc(K2Plugin)
public class K2Plugin: CAPPlugin, CAPBridgedPlugin {
    public let identifier = "K2Plugin"
    public let jsName = "K2Plugin"
    public let pluginMethods: [CAPPluginMethod] = [
        CAPPluginMethod(name: "checkReady", returnType: CAPPluginReturnPromise),
        CAPPluginMethod(name: "getUDID", returnType: CAPPluginReturnPromise),
        CAPPluginMethod(name: "getVersion", returnType: CAPPluginReturnPromise),
        CAPPluginMethod(name: "getStatus", returnType: CAPPluginReturnPromise),
        CAPPluginMethod(name: "getConfig", returnType: CAPPluginReturnPromise),
        CAPPluginMethod(name: "connect", returnType: CAPPluginReturnPromise),
        CAPPluginMethod(name: "disconnect", returnType: CAPPluginReturnPromise),
    ]

    private var vpnManager: NETunnelProviderManager?
    private var statusObserver: NSObjectProtocol?

    override public func load() {
        statusObserver = NotificationCenter.default.addObserver(
            forName: .NEVPNStatusDidChange,
            object: nil,
            queue: .main
        ) { [weak self] notification in
            guard let connection = notification.object as? NEVPNConnection else { return }
            let state = self?.mapVPNStatus(connection.status) ?? "stopped"
            self?.notifyListeners("vpnStateChange", data: ["state": state])
        }
        loadVPNManager()
    }

    deinit {
        if let observer = statusObserver {
            NotificationCenter.default.removeObserver(observer)
        }
    }

    @objc func checkReady(_ call: CAPPluginCall) {
        let version = Bundle.main.infoDictionary?["CFBundleShortVersionString"] as? String ?? "unknown"
        call.resolve(["ready": true, "version": version])
    }

    @objc func getUDID(_ call: CAPPluginCall) {
        let udid = UIDevice.current.identifierForVendor?.uuidString ?? UUID().uuidString
        call.resolve(["udid": udid])
    }

    @objc func getVersion(_ call: CAPPluginCall) {
        let version = Bundle.main.infoDictionary?["CFBundleShortVersionString"] as? String ?? "unknown"
        call.resolve([
            "version": version,
            "go": "embedded",
            "os": "ios",
            "arch": "arm64"
        ])
    }

    @objc func getStatus(_ call: CAPPluginCall) {
        guard let manager = vpnManager else {
            call.resolve(["state": "stopped"])
            return
        }

        // Try sendProviderMessage for rich status (StatusJSON)
        let session = manager.connection as? NETunnelProviderSession
        let message = "status".data(using: .utf8)!

        do {
            try session?.sendProviderMessage(message) { response in
                if let data = response,
                   let json = try? JSONSerialization.jsonObject(with: data) as? [String: Any] {
                    // Map engine "disconnected" to webapp "stopped"
                    var result = json
                    if let state = result["state"] as? String, state == "disconnected" {
                        result["state"] = "stopped"
                    }
                    call.resolve(result as! [String: Any])
                } else {
                    // Fallback to connection status
                    let state = self.mapVPNStatus(manager.connection.status)
                    call.resolve(["state": state])
                }
            }
        } catch {
            let state = mapVPNStatus(manager.connection.status)
            call.resolve(["state": state])
        }
    }

    @objc func getConfig(_ call: CAPPluginCall) {
        let defaults = UserDefaults(suiteName: "group.io.kaitu")
        let wireUrl = defaults?.string(forKey: "wireUrl")
        call.resolve(["wireUrl": wireUrl ?? ""])
    }

    @objc func connect(_ call: CAPPluginCall) {
        guard let wireUrl = call.getString("wireUrl") else {
            call.reject("Missing wireUrl parameter")
            return
        }

        loadVPNManager { [weak self] manager in
            guard let manager = manager else {
                call.reject("Failed to load VPN configuration")
                return
            }

            let proto = (manager.protocolConfiguration as? NETunnelProviderProtocol) ?? NETunnelProviderProtocol()
            proto.providerBundleIdentifier = "io.kaitu.PacketTunnelExtension"
            proto.serverAddress = wireUrl
            proto.providerConfiguration = ["wireUrl": wireUrl]
            manager.protocolConfiguration = proto
            manager.isEnabled = true
            manager.localizedDescription = "Kaitu VPN"

            manager.saveToPreferences { error in
                if let error = error {
                    call.reject("Failed to save VPN config: \(error.localizedDescription)")
                    return
                }
                manager.loadFromPreferences { error in
                    if let error = error {
                        call.reject("Failed to reload config: \(error.localizedDescription)")
                        return
                    }
                    do {
                        try (manager.connection as? NETunnelProviderSession)?.startVPNTunnel(options: [
                            "wireUrl": NSString(string: wireUrl)
                        ])
                        // Save wireUrl to App Group for NE access
                        UserDefaults(suiteName: "group.io.kaitu")?.set(wireUrl, forKey: "wireUrl")
                        call.resolve()
                    } catch {
                        call.reject("Failed to start tunnel: \(error.localizedDescription)")
                    }
                }
            }
        }
    }

    @objc func disconnect(_ call: CAPPluginCall) {
        vpnManager?.connection.stopVPNTunnel()
        call.resolve()
    }

    // MARK: - Private

    private func loadVPNManager(completion: ((NETunnelProviderManager?) -> Void)? = nil) {
        NETunnelProviderManager.loadAllFromPreferences { [weak self] managers, error in
            let manager = managers?.first ?? NETunnelProviderManager()
            self?.vpnManager = manager
            completion?(manager)
        }
    }

    private func mapVPNStatus(_ status: NEVPNStatus) -> String {
        switch status {
        case .connected: return "connected"
        case .connecting, .reasserting: return "connecting"
        default: return "stopped"
        }
    }
}
