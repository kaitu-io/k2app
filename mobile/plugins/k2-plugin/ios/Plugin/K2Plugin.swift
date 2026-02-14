import Foundation
import Capacitor
import NetworkExtension
import CommonCrypto
import SSZipArchive

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
        CAPPluginMethod(name: "checkWebUpdate", returnType: CAPPluginReturnPromise),
        CAPPluginMethod(name: "checkNativeUpdate", returnType: CAPPluginReturnPromise),
        CAPPluginMethod(name: "applyWebUpdate", returnType: CAPPluginReturnPromise),
        CAPPluginMethod(name: "downloadNativeUpdate", returnType: CAPPluginReturnPromise),
        CAPPluginMethod(name: "installNativeUpdate", returnType: CAPPluginReturnPromise),
    ]

    private var vpnManager: NETunnelProviderManager?
    private var statusObserver: NSObjectProtocol?

    private let webManifestURL = "https://d0.all7.cc/kaitu/web/latest.json"
    private let iosManifestURL = "https://d0.all7.cc/kaitu/ios/latest.json"
    private let appStoreURL = "https://apps.apple.com/app/id6759199298"

    override public func load() {
        // Check for OTA web update
        let documentsPath = FileManager.default.urls(for: .documentDirectory, in: .userDomainMask).first!
        let webUpdatePath = documentsPath.appendingPathComponent("web-update")
        if FileManager.default.fileExists(atPath: webUpdatePath.path) {
            let indexPath = webUpdatePath.appendingPathComponent("index.html")
            if FileManager.default.fileExists(atPath: indexPath.path) {
                bridge?.setServerBasePath(webUpdatePath.path)
            } else {
                // Corrupt OTA dir, remove it
                try? FileManager.default.removeItem(at: webUpdatePath)
            }
        }

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

    // MARK: - VPN Methods

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
                    call.resolve(self.remapStatusKeys(json))
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

    // MARK: - Update Methods

    @objc func checkWebUpdate(_ call: CAPPluginCall) {
        guard let url = URL(string: webManifestURL) else {
            call.resolve(["available": false])
            return
        }

        URLSession.shared.dataTask(with: url) { data, response, error in
            guard let data = data, error == nil,
                  let json = try? JSONSerialization.jsonObject(with: data) as? [String: Any],
                  let remoteVersion = json["version"] as? String else {
                call.resolve(["available": false])
                return
            }

            // Read installed web version, fall back to app version
            let documentsPath = FileManager.default.urls(for: .documentDirectory, in: .userDomainMask).first!
            let versionFile = documentsPath.appendingPathComponent("web-update/version.txt")
            let localVersion: String
            if FileManager.default.fileExists(atPath: versionFile.path),
               let storedVersion = try? String(contentsOf: versionFile, encoding: .utf8) {
                localVersion = storedVersion.trimmingCharacters(in: .whitespacesAndNewlines)
            } else {
                localVersion = Bundle.main.infoDictionary?["CFBundleShortVersionString"] as? String ?? "0.0.0"
            }

            if self.isNewerVersion(remoteVersion, than: localVersion) {
                var result: [String: Any] = [
                    "available": true,
                    "version": remoteVersion
                ]
                if let size = json["size"] as? Int {
                    result["size"] = size
                }
                call.resolve(result)
            } else {
                call.resolve(["available": false])
            }
        }.resume()
    }

    @objc func checkNativeUpdate(_ call: CAPPluginCall) {
        guard let url = URL(string: iosManifestURL) else {
            call.resolve(["available": false])
            return
        }

        URLSession.shared.dataTask(with: url) { data, response, error in
            guard let data = data, error == nil,
                  let json = try? JSONSerialization.jsonObject(with: data) as? [String: Any],
                  let remoteVersion = json["version"] as? String else {
                call.resolve(["available": false])
                return
            }

            let localVersion = Bundle.main.infoDictionary?["CFBundleShortVersionString"] as? String ?? "0.0.0"

            if self.isNewerVersion(remoteVersion, than: localVersion) {
                var result: [String: Any] = [
                    "available": true,
                    "version": remoteVersion
                ]
                if let appstoreUrl = json["appstore_url"] as? String {
                    result["url"] = appstoreUrl
                } else {
                    result["url"] = self.appStoreURL
                }
                call.resolve(result)
            } else {
                call.resolve(["available": false])
            }
        }.resume()
    }

    @objc func applyWebUpdate(_ call: CAPPluginCall) {
        guard let manifestUrl = URL(string: webManifestURL) else {
            call.reject("Invalid manifest URL")
            return
        }

        Task {
            let documentsPath = FileManager.default.urls(for: .documentDirectory, in: .userDomainMask).first!
            let webUpdatePath = documentsPath.appendingPathComponent("web-update")
            let webBackupPath = documentsPath.appendingPathComponent("web-backup")
            let tempZipPath = documentsPath.appendingPathComponent("webapp-update.zip")

            do {
                // 1. Fetch manifest
                let (manifestData, _) = try await URLSession.shared.data(from: manifestUrl)
                guard let json = try? JSONSerialization.jsonObject(with: manifestData) as? [String: Any],
                      let zipUrlString = json["url"] as? String,
                      let remoteVersion = json["version"] as? String,
                      let expectedHash = json["hash"] as? String,
                      let zipUrl = URL(string: zipUrlString) else {
                    await MainActor.run { call.reject("Failed to fetch or parse web update manifest") }
                    return
                }

                // Strip "sha256:" prefix if present
                let cleanHash = expectedHash.hasPrefix("sha256:") ? String(expectedHash.dropFirst(7)) : expectedHash

                // 2. Download zip
                let (zipData, _) = try await URLSession.shared.data(from: zipUrl)

                // 3. Verify SHA256 hash
                let actualHash = self.sha256(data: zipData)
                guard actualHash == cleanHash else {
                    await MainActor.run { call.reject("Hash mismatch: expected \(cleanHash), got \(actualHash)") }
                    return
                }

                // 4. Write zip to temp file
                try zipData.write(to: tempZipPath)

                // If existing web-update exists, move to web-backup
                if FileManager.default.fileExists(atPath: webUpdatePath.path) {
                    if FileManager.default.fileExists(atPath: webBackupPath.path) {
                        try FileManager.default.removeItem(at: webBackupPath)
                    }
                    try FileManager.default.moveItem(at: webUpdatePath, to: webBackupPath)
                }

                // Create web-update directory
                try FileManager.default.createDirectory(at: webUpdatePath, withIntermediateDirectories: true)

                // 5. Unzip to web-update/
                try self.unzip(fileAt: tempZipPath, to: webUpdatePath)

                // Clean up temp zip
                try? FileManager.default.removeItem(at: tempZipPath)

                // Flatten nested subdirectory if index.html not at root
                let indexPath = webUpdatePath.appendingPathComponent("index.html")
                if !FileManager.default.fileExists(atPath: indexPath.path) {
                    let contents = try FileManager.default.contentsOfDirectory(at: webUpdatePath, includingPropertiesForKeys: nil)
                    if contents.count == 1, contents[0].hasDirectoryPath {
                        let subdir = contents[0]
                        let subContents = try FileManager.default.contentsOfDirectory(at: subdir, includingPropertiesForKeys: nil)
                        for item in subContents {
                            let dest = webUpdatePath.appendingPathComponent(item.lastPathComponent)
                            try FileManager.default.moveItem(at: item, to: dest)
                        }
                        try FileManager.default.removeItem(at: subdir)
                    }
                }

                // Write version for future comparison
                try remoteVersion.write(to: webUpdatePath.appendingPathComponent("version.txt"),
                                        atomically: true, encoding: .utf8)

                await MainActor.run { call.resolve() }
            } catch {
                // Restore backup on failure
                try? FileManager.default.removeItem(at: webUpdatePath)
                if FileManager.default.fileExists(atPath: webBackupPath.path) {
                    try? FileManager.default.moveItem(at: webBackupPath, to: webUpdatePath)
                }
                try? FileManager.default.removeItem(at: tempZipPath)

                await MainActor.run { call.reject("Failed to apply web update: \(error.localizedDescription)") }
            }
        }
    }

    @objc func downloadNativeUpdate(_ call: CAPPluginCall) {
        // On iOS, native updates go through the App Store
        call.resolve(["path": "appstore"])
    }

    @objc func installNativeUpdate(_ call: CAPPluginCall) {
        // iOS cannot install updates itself — open App Store
        guard let url = URL(string: appStoreURL) else {
            call.reject("Invalid App Store URL")
            return
        }
        DispatchQueue.main.async {
            UIApplication.shared.open(url, options: [:], completionHandler: nil)
            call.resolve()
        }
    }

    // MARK: - Private VPN Helpers

    private func loadVPNManager(completion: ((NETunnelProviderManager?) -> Void)? = nil) {
        NETunnelProviderManager.loadAllFromPreferences { [weak self] managers, error in
            let manager = managers?.first ?? NETunnelProviderManager()
            self?.vpnManager = manager
            completion?(manager)
        }
    }

    /// Remap Go StatusJSON snake_case keys to JS camelCase and map "disconnected" -> "stopped"
    private func remapStatusKeys(_ json: [String: Any]) -> [String: Any] {
        let keyMap: [String: String] = [
            "connected_at": "connectedAt",
            "uptime_seconds": "uptimeSeconds",
            "wire_url": "wireUrl",
        ]
        var result: [String: Any] = [:]
        for (key, value) in json {
            let newKey = keyMap[key] ?? key
            result[newKey] = value
        }
        if let state = result["state"] as? String, state == "disconnected" {
            result["state"] = "stopped"
        }
        return result
    }

    private func mapVPNStatus(_ status: NEVPNStatus) -> String {
        switch status {
        case .connected: return "connected"
        case .connecting, .reasserting: return "connecting"
        default: return "stopped"
        }
    }

    // MARK: - Private Update Helpers

    private func isNewerVersion(_ remote: String, than local: String) -> Bool {
        let r = remote.split(separator: ".").compactMap { Int($0) }
        let l = local.split(separator: ".").compactMap { Int($0) }
        for i in 0..<max(r.count, l.count) {
            let rv = i < r.count ? r[i] : 0
            let lv = i < l.count ? l[i] : 0
            if rv > lv { return true }
            if rv < lv { return false }
        }
        return false
    }

    private func sha256(data: Data) -> String {
        var hash = [UInt8](repeating: 0, count: Int(CC_SHA256_DIGEST_LENGTH))
        data.withUnsafeBytes {
            _ = CC_SHA256($0.baseAddress, CC_LONG(data.count), &hash)
        }
        return hash.map { String(format: "%02x", $0) }.joined()
    }

    private func unzip(fileAt sourceURL: URL, to destinationURL: URL) throws {
        let success = SSZipArchive.unzipFile(
            atPath: sourceURL.path,
            toDestination: destinationURL.path,
            overwrite: true,
            password: nil
        )
        if !success {
            throw NSError(
                domain: "K2Plugin",
                code: -1,
                userInfo: [NSLocalizedDescriptionKey: "Failed to unzip file"]
            )
        }
    }
}
