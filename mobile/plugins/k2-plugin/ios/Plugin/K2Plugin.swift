import Foundation
import Capacitor
import NetworkExtension
import CommonCrypto
import SSZipArchive

private let kAppGroup = "group.io.kaitu"

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

    private let webManifestEndpoints = [
        "https://d13jc1jqzlg4yt.cloudfront.net/kaitu/web/latest.json",
        "https://d0.all7.cc/kaitu/web/latest.json"
    ]
    private let iosManifestEndpoints = [
        "https://d13jc1jqzlg4yt.cloudfront.net/kaitu/ios/latest.json",
        "https://d0.all7.cc/kaitu/ios/latest.json"
    ]
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

        loadVPNManager()

        DispatchQueue.main.asyncAfter(deadline: .now() + 3.0) { [weak self] in
            self?.performAutoUpdateCheck()
        }
    }

    private func registerStatusObserver() {
        // Remove old observer if exists
        if let observer = statusObserver {
            NotificationCenter.default.removeObserver(observer)
        }

        statusObserver = NotificationCenter.default.addObserver(
            forName: .NEVPNStatusDidChange,
            object: vpnManager?.connection,
            queue: .main
        ) { [weak self] notification in
            guard let connection = notification.object as? NEVPNConnection else { return }
            let state = self?.mapVPNStatus(connection.status) ?? "disconnected"
            self?.notifyListeners("vpnStateChange", data: ["state": state])

            // On disconnect, check App Group for error from NE process
            if connection.status == .disconnected {
                let defaults = UserDefaults(suiteName: kAppGroup)
                if let errorMsg = defaults?.string(forKey: "vpnError"), !errorMsg.isEmpty {
                    defaults?.removeObject(forKey: "vpnError")
                    self?.notifyListeners("vpnError", data: ["message": errorMsg])
                }
            }
        }
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
        let doGetStatus: (NETunnelProviderManager) -> Void = { [weak self] manager in
            guard let self = self else { return }
            let session = manager.connection as? NETunnelProviderSession
            let message = "status".data(using: .utf8)!

            do {
                try session?.sendProviderMessage(message) { response in
                    if let data = response,
                       let json = try? JSONSerialization.jsonObject(with: data) as? [String: Any] {
                        call.resolve(self.remapStatusKeys(json))
                    } else {
                        let state = self.mapVPNStatus(manager.connection.status)
                        call.resolve(["state": state])
                    }
                }
            } catch {
                let state = self.mapVPNStatus(manager.connection.status)
                call.resolve(["state": state])
            }
        }

        if let manager = vpnManager {
            doGetStatus(manager)
        } else {
            // Auto-load if vpnManager not yet available (race with load())
            loadVPNManager { manager in
                if let manager = manager {
                    doGetStatus(manager)
                } else {
                    call.resolve(["state": "disconnected"])
                }
            }
        }
    }

    @objc func getConfig(_ call: CAPPluginCall) {
        let defaults = UserDefaults(suiteName: kAppGroup)
        let config = defaults?.string(forKey: "configJSON")
        call.resolve(["config": config ?? ""])
    }

    @objc func connect(_ call: CAPPluginCall) {
        guard let config = call.getString("config") else {
            call.reject("Missing config parameter")
            return
        }

        loadVPNManager { [weak self] manager in
            guard let manager = manager else {
                call.reject("Failed to load VPN configuration")
                return
            }

            let proto = (manager.protocolConfiguration as? NETunnelProviderProtocol) ?? NETunnelProviderProtocol()
            proto.providerBundleIdentifier = "io.kaitu.PacketTunnelExtension"
            proto.serverAddress = "Kaitu VPN"
            proto.providerConfiguration = ["configJSON": config]
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
                            "configJSON": NSString(string: config)
                        ])
                        // Save config to App Group for NE access
                        UserDefaults(suiteName: kAppGroup)?.set(config, forKey: "configJSON")
                        call.resolve()
                    } catch {
                        call.reject("Failed to start tunnel: \(error.localizedDescription)")
                    }
                }
            }
        }
    }

    @objc func disconnect(_ call: CAPPluginCall) {
        guard let connection = vpnManager?.connection else {
            call.resolve()
            return
        }

        // Already disconnected
        if connection.status == .disconnected {
            call.resolve()
            return
        }

        var disconnectObserver: NSObjectProtocol?
        var timeoutWork: DispatchWorkItem?

        let cleanup: () -> Void = {
            if let obs = disconnectObserver {
                NotificationCenter.default.removeObserver(obs)
                disconnectObserver = nil
            }
            timeoutWork?.cancel()
            timeoutWork = nil
        }

        // One-time observer for .disconnected
        disconnectObserver = NotificationCenter.default.addObserver(
            forName: .NEVPNStatusDidChange,
            object: connection,
            queue: .main
        ) { notification in
            guard let conn = notification.object as? NEVPNConnection,
                  conn.status == .disconnected else { return }
            cleanup()
            call.resolve()
        }

        // 5s timeout fallback
        let timeout = DispatchWorkItem {
            cleanup()
            call.resolve()
        }
        timeoutWork = timeout
        DispatchQueue.main.asyncAfter(deadline: .now() + 5.0, execute: timeout)

        // Trigger the disconnect
        connection.stopVPNTunnel()
    }

    // MARK: - Update Methods

    @objc func checkWebUpdate(_ call: CAPPluginCall) {
        Task {
            guard let (data, _) = await fetchManifest(endpoints: webManifestEndpoints),
                  let json = try? JSONSerialization.jsonObject(with: data) as? [String: Any],
                  let remoteVersion = json["version"] as? String else {
                await MainActor.run { call.resolve(["available": false]) }
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
                await MainActor.run { call.resolve(result) }
            } else {
                await MainActor.run { call.resolve(["available": false]) }
            }
        }
    }

    @objc func checkNativeUpdate(_ call: CAPPluginCall) {
        Task {
            guard let (data, _) = await fetchManifest(endpoints: iosManifestEndpoints),
                  let json = try? JSONSerialization.jsonObject(with: data) as? [String: Any],
                  let remoteVersion = json["version"] as? String else {
                await MainActor.run { call.resolve(["available": false]) }
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
                await MainActor.run { call.resolve(result) }
            } else {
                await MainActor.run { call.resolve(["available": false]) }
            }
        }
    }

    @objc func applyWebUpdate(_ call: CAPPluginCall) {
        Task {
            await applyWebUpdateInternal { result in
                switch result {
                case .success:
                    call.resolve()
                case .failure(let error):
                    call.reject("Failed to apply web update: \(error.localizedDescription)")
                }
            }
        }
    }

    /// Shared web update logic used by both applyWebUpdate (user-triggered) and auto-check.
    /// Calls completion on MainActor when done.
    private func applyWebUpdateInternal(completion: @escaping (Result<Void, Error>) -> Void) async {
        let documentsPath = FileManager.default.urls(for: .documentDirectory, in: .userDomainMask).first!
        let webUpdatePath = documentsPath.appendingPathComponent("web-update")
        let webBackupPath = documentsPath.appendingPathComponent("web-backup")
        let tempZipPath = documentsPath.appendingPathComponent("webapp-update.zip")

        do {
            // 1. Fetch manifest from dual CDN endpoints
            guard let (manifestData, baseURL) = await fetchManifest(endpoints: webManifestEndpoints),
                  let json = try? JSONSerialization.jsonObject(with: manifestData) as? [String: Any],
                  let zipUrlString = json["url"] as? String,
                  let remoteVersion = json["version"] as? String,
                  let expectedHash = json["hash"] as? String else {
                await MainActor.run { completion(.failure(NSError(domain: "K2Plugin", code: -1, userInfo: [NSLocalizedDescriptionKey: "Failed to fetch or parse web update manifest"]))) }
                return
            }

            let zipUrl = resolveDownloadURL(url: zipUrlString, baseURL: baseURL)

            // Strip "sha256:" prefix if present
            let cleanHash = expectedHash.hasPrefix("sha256:") ? String(expectedHash.dropFirst(7)) : expectedHash

            // 2. Download zip
            let (zipData, _) = try await URLSession.shared.data(from: zipUrl)

            // 3. Verify SHA256 hash
            let actualHash = self.sha256(data: zipData)
            guard actualHash == cleanHash else {
                await MainActor.run { completion(.failure(NSError(domain: "K2Plugin", code: -2, userInfo: [NSLocalizedDescriptionKey: "Hash mismatch: expected \(cleanHash), got \(actualHash)"]))) }
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

            await MainActor.run { completion(.success(())) }
        } catch {
            // Restore backup on failure
            try? FileManager.default.removeItem(at: webUpdatePath)
            if FileManager.default.fileExists(atPath: webBackupPath.path) {
                try? FileManager.default.moveItem(at: webBackupPath, to: webUpdatePath)
            }
            try? FileManager.default.removeItem(at: tempZipPath)

            await MainActor.run { completion(.failure(error)) }
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
            let bundleId = "io.kaitu.PacketTunnelExtension"
            let manager = managers?.first(where: {
                ($0.protocolConfiguration as? NETunnelProviderProtocol)?.providerBundleIdentifier == bundleId
            }) ?? NETunnelProviderManager()
            self?.vpnManager = manager
            self?.registerStatusObserver()
            completion?(manager)
        }
    }

    /// Remap Go StatusJSON snake_case keys to JS camelCase
    private func remapStatusKeys(_ json: [String: Any]) -> [String: Any] {
        let keyMap: [String: String] = [
            "connected_at": "connectedAt",
            "uptime_seconds": "uptimeSeconds",
        ]
        var result: [String: Any] = [:]
        for (key, value) in json {
            let newKey = keyMap[key] ?? key
            result[newKey] = value
        }
        return result
    }

    private func mapVPNStatus(_ status: NEVPNStatus) -> String {
        switch status {
        case .connected: return "connected"
        case .connecting: return "connecting"
        case .disconnecting: return "disconnecting"
        case .reasserting: return "reconnecting"
        case .disconnected, .invalid: return "disconnected"
        @unknown default: return "disconnected"
        }
    }

    // MARK: - Private Update Helpers

    /// Try each endpoint in order with a 10s timeout. Returns (data, baseURL) where baseURL
    /// is the manifest URL with the filename stripped (for resolving relative download paths).
    private func fetchManifest(endpoints: [String]) async -> (Data, URL)? {
        let config = URLSessionConfiguration.ephemeral
        config.timeoutIntervalForRequest = 10
        let session = URLSession(configuration: config)

        for endpoint in endpoints {
            guard let url = URL(string: endpoint) else { continue }
            do {
                let (data, response) = try await session.data(from: url)
                if let httpResponse = response as? HTTPURLResponse, httpResponse.statusCode == 200 {
                    let baseURL = url.deletingLastPathComponent()
                    return (data, baseURL)
                }
            } catch {
                NSLog("[K2Plugin] fetchManifest failed for %@: %@", endpoint, error.localizedDescription)
                continue
            }
        }
        return nil
    }

    /// Resolve a download URL: absolute URLs pass through, relative paths are appended to baseURL.
    private func resolveDownloadURL(url: String, baseURL: URL) -> URL {
        if url.hasPrefix("http://") || url.hasPrefix("https://") {
            return URL(string: url)!
        }
        return baseURL.appendingPathComponent(url)
    }

    /// Auto-check on cold start: check native update (emit event), then silently apply web OTA.
    private func performAutoUpdateCheck() {
        Task {
            // 1. Check native (iOS) update
            do {
                if let (data, _) = await fetchManifest(endpoints: iosManifestEndpoints),
                   let json = try JSONSerialization.jsonObject(with: data) as? [String: Any],
                   let remoteVersion = json["version"] as? String {
                    let localVersion = Bundle.main.infoDictionary?["CFBundleShortVersionString"] as? String ?? "0.0.0"
                    if isNewerVersion(remoteVersion, than: localVersion) {
                        let storeUrl = (json["appstore_url"] as? String) ?? appStoreURL
                        await MainActor.run {
                            self.notifyListeners("nativeUpdateAvailable", data: [
                                "version": remoteVersion,
                                "appStoreUrl": storeUrl
                            ])
                        }
                    }
                }
            } catch {
                NSLog("[K2Plugin] autocheck native update error: %@", error.localizedDescription)
            }

            // 2. Check + silently apply web OTA
            do {
                guard let (manifestData, _) = await fetchManifest(endpoints: webManifestEndpoints),
                      let json = try JSONSerialization.jsonObject(with: manifestData) as? [String: Any],
                      let remoteVersion = json["version"] as? String else {
                    return
                }

                let documentsPath = FileManager.default.urls(for: .documentDirectory, in: .userDomainMask).first!
                let versionFile = documentsPath.appendingPathComponent("web-update/version.txt")
                let localVersion: String
                if FileManager.default.fileExists(atPath: versionFile.path),
                   let storedVersion = try? String(contentsOf: versionFile, encoding: .utf8) {
                    localVersion = storedVersion.trimmingCharacters(in: .whitespacesAndNewlines)
                } else {
                    localVersion = Bundle.main.infoDictionary?["CFBundleShortVersionString"] as? String ?? "0.0.0"
                }

                guard isNewerVersion(remoteVersion, than: localVersion) else { return }

                await applyWebUpdateInternal { result in
                    switch result {
                    case .success:
                        NSLog("[K2Plugin] auto web OTA applied: %@", remoteVersion)
                    case .failure(let error):
                        NSLog("[K2Plugin] auto web OTA failed: %@", error.localizedDescription)
                    }
                }
            } catch {
                NSLog("[K2Plugin] autocheck web update error: %@", error.localizedDescription)
            }
        }
    }

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
        try SSZipArchive.unzipFile(
            atPath: sourceURL.path,
            toDestination: destinationURL.path,
            overwrite: true,
            password: nil
        )
    }
}
