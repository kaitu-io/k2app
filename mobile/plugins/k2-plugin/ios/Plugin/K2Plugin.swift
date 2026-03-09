import Foundation
import Capacitor
import NetworkExtension
import CommonCrypto
import Gzip
import SSZipArchive
import os.log

private let kAppGroup = "group.io.kaitu"
private let logger = Logger(subsystem: "io.kaitu", category: "K2Plugin")

extension Notification.Name {
    static let k2DevEnabledChanged = Notification.Name("k2DevEnabledChanged")
}

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
        CAPPluginMethod(name: "appendLogs", returnType: CAPPluginReturnPromise),
        CAPPluginMethod(name: "uploadLogs", returnType: CAPPluginReturnPromise),
        CAPPluginMethod(name: "setLogLevel", returnType: CAPPluginReturnPromise),
        CAPPluginMethod(name: "setDevEnabled", returnType: CAPPluginReturnPromise),
        CAPPluginMethod(name: "debugDump", returnType: CAPPluginReturnPromise),
    ]

    private var vpnManager: NETunnelProviderManager?
    private var statusObserver: NSObjectProtocol?
    private var logsDir: URL?
    private var webappLogHandle: FileHandle?
    private let webappLogQueue = DispatchQueue(label: "io.kaitu.webapp-log")

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
                do {
                    try FileManager.default.removeItem(at: webUpdatePath)
                    logger.info("Removed corrupt OTA dir")
                } catch {
                    logger.warning("Failed to remove corrupt OTA dir: \(error)")
                }
            }
        }

        // Initialize App Group logs directory for webapp.log writes
        if let containerURL = FileManager.default.containerURL(forSecurityApplicationGroupIdentifier: kAppGroup) {
            let logsURL = containerURL.appendingPathComponent("logs")
            do {
                try FileManager.default.createDirectory(at: logsURL, withIntermediateDirectories: true)
                self.logsDir = logsURL
                // Open webapp.log FileHandle for append
                let webappLogURL = logsURL.appendingPathComponent("webapp.log")
                if !FileManager.default.fileExists(atPath: webappLogURL.path) {
                    FileManager.default.createFile(atPath: webappLogURL.path, contents: nil)
                }
                self.webappLogHandle = try FileHandle(forWritingTo: webappLogURL)
                self.webappLogHandle?.seekToEndOfFile()
            } catch {
                logger.warning("Failed to init logs dir: \(error)")
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

            // On disconnect, check App Group for error from NE process.
            // Error is stored as JSON {"code": 503, "message": "..."} or legacy plain string.
            if connection.status == .disconnected {
                let defaults = UserDefaults(suiteName: kAppGroup)
                if let errorStr = defaults?.string(forKey: "vpnError"), !errorStr.isEmpty {
                    logger.debug("Read vpnError from App Group: \(errorStr)")
                    defaults?.removeObject(forKey: "vpnError")
                    // Try parsing as JSON error object first, fall back to plain message
                    if let errorData = errorStr.data(using: .utf8),
                       let errorObj = try? JSONSerialization.jsonObject(with: errorData) as? [String: Any],
                       let code = errorObj["code"] as? Int,
                       let message = errorObj["message"] as? String {
                        logger.info("Emitting vpnError event: code=\(code) message=\(message)")
                        self?.notifyListeners("vpnError", data: ["code": code, "message": message])
                    } else {
                        logger.info("Emitting vpnError event (legacy string): \(errorStr)")
                        self?.notifyListeners("vpnError", data: ["message": errorStr])
                    }
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
        let raw = UIDevice.current.identifierForVendor?.uuidString ?? UUID().uuidString
        call.resolve(["udid": hashToUdid(raw)])
    }

    /// SHA-256 hash a raw platform ID to 32 lowercase hex chars (128 bit).
    private func hashToUdid(_ raw: String) -> String {
        let data = Data(raw.utf8)
        var hash = [UInt8](repeating: 0, count: Int(CC_SHA256_DIGEST_LENGTH))
        data.withUnsafeBytes { CC_SHA256($0.baseAddress, CC_LONG(data.count), &hash) }
        return hash.prefix(16).map { String(format: "%02x", $0) }.joined()
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
                        call.resolve(remapStatusKeys(json))
                    } else {
                        logger.debug("sendProviderMessage returned nil/unparseable response, falling back to NEVPNStatus")
                        let state = self.mapVPNStatus(manager.connection.status)
                        call.resolve(["state": state])
                    }
                }
            } catch {
                logger.debug("sendProviderMessage failed, falling back to NEVPNStatus: \(error)")
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

            if isNewerVersion(remoteVersion, than: localVersion) {
                var result: [String: Any] = [
                    "available": true,
                    "version": remoteVersion
                ]
                if let size = json["size"] as? Int {
                    result["size"] = size
                }
                let finalResult = result
                await MainActor.run { call.resolve(finalResult) }
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

            if isNewerVersion(remoteVersion, than: localVersion) {
                var result: [String: Any] = [
                    "available": true,
                    "version": remoteVersion
                ]
                if let appstoreUrl = json["appstore_url"] as? String {
                    result["url"] = appstoreUrl
                } else {
                    result["url"] = self.appStoreURL
                }
                let finalResult = result
                await MainActor.run { call.resolve(finalResult) }
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

    // MARK: - Logging

    private let maxWebappLogSize: UInt64 = 50 * 1024 * 1024 // 50MB
    private let isoFormatter = ISO8601DateFormatter()

    @objc func appendLogs(_ call: CAPPluginCall) {
        guard let entries = call.getArray("entries") as? [[String: Any]] else {
            call.resolve()
            return
        }
        webappLogQueue.async { [weak self] in
            guard let self = self, let handle = self.webappLogHandle,
                  let logURL = self.logsDir?.appendingPathComponent("webapp.log") else { return }

            // Check size, truncate if over limit
            if let attrs = try? FileManager.default.attributesOfItem(atPath: logURL.path),
               let size = attrs[.size] as? UInt64, size > self.maxWebappLogSize {
                handle.truncateFile(atOffset: 0)
            }

            for entry in entries {
                let level = (entry["level"] as? String)?.uppercased() ?? "LOG"
                let message = entry["message"] as? String ?? ""
                let ts = entry["timestamp"] as? Double ?? Date().timeIntervalSince1970 * 1000
                let date = Date(timeIntervalSince1970: ts / 1000)
                let line = "[\(self.isoFormatter.string(from: date))] [\(level)] \(message)\n"
                if let data = line.data(using: .utf8) {
                    handle.write(data)
                }
            }
        }
        call.resolve()
    }

    @objc func uploadLogs(_ call: CAPPluginCall) {
        let feedbackId = call.getString("feedbackId")

        Task {
            do {
                guard let logsDir = self.logsDir else {
                    await MainActor.run { call.reject("Logs directory not initialized") }
                    return
                }

                // Get UDID for S3 key
                let raw = UIDevice.current.identifierForVendor?.uuidString ?? UUID().uuidString
                let udid = hashToUdid(raw)

                // 1. Create staging dir
                let stagingDir = FileManager.default.temporaryDirectory
                    .appendingPathComponent("kaitu-log-upload-\(Int(Date().timeIntervalSince1970))")
                try FileManager.default.createDirectory(at: stagingDir, withIntermediateDirectories: true)

                // 2. Copy log files to staging and sanitize
                let logTypes = ["k2", "native", "webapp"]
                var sourceFiles: [URL] = []

                for logType in logTypes {
                    let logFile = logsDir.appendingPathComponent("\(logType).log")
                    guard FileManager.default.fileExists(atPath: logFile.path) else { continue }
                    guard let content = try? String(contentsOf: logFile, encoding: .utf8), !content.isEmpty else { continue }

                    let sanitized = self.sanitizeLogs(content)
                    let destFile = stagingDir.appendingPathComponent("\(logType).log")
                    try sanitized.write(to: destFile, atomically: true, encoding: .utf8)
                    sourceFiles.append(logFile)
                }

                if sourceFiles.isEmpty {
                    try? FileManager.default.removeItem(at: stagingDir)
                    let result: [String: Any] = ["success": false, "error": "No log files found"]
                    await MainActor.run { call.resolve(result) }
                    return
                }

                // 3. Create zip archive using SSZipArchive (mature library)
                let zipPath = stagingDir.appendingPathComponent("logs.zip").path
                let filePaths = try FileManager.default.contentsOfDirectory(
                    at: stagingDir, includingPropertiesForKeys: nil, options: [.skipsHiddenFiles]
                ).filter { !$0.hasDirectoryPath }.map { $0.path }
                guard SSZipArchive.createZipFile(atPath: zipPath, withFilesAtPaths: filePaths) else {
                    throw NSError(domain: "K2Plugin", code: -1, userInfo: [NSLocalizedDescriptionKey: "Failed to create zip archive"])
                }
                let archiveData = try Data(contentsOf: URL(fileURLWithPath: zipPath))

                // 4. Upload single archive
                let s3Key = self.generateS3Key(feedbackId: feedbackId, udid: udid)
                try await self.uploadToS3(s3Key: s3Key, data: archiveData)

                // 5. Truncate source files (preserves inodes for NE process)
                for logFile in sourceFiles {
                    if let handle = try? FileHandle(forWritingTo: logFile) {
                        handle.truncateFile(atOffset: 0)
                        handle.closeFile()
                    }
                }
                // Re-seek webapp log handle after truncation
                self.webappLogQueue.sync {
                    self.webappLogHandle?.seekToEndOfFile()
                }

                // 6. Clean up staging dir
                try? FileManager.default.removeItem(at: stagingDir)

                let result: [String: Any] = [
                    "success": true,
                    "s3Keys": [["name": "logs", "s3Key": s3Key]]
                ]
                await MainActor.run { call.resolve(result) }
            } catch {
                await MainActor.run {
                    call.resolve(["success": false, "error": error.localizedDescription])
                }
            }
        }
    }

    private let s3BucketURL = "https://kaitu-service-logs.s3.ap-northeast-1.amazonaws.com"

    private func sanitizeLogs(_ content: String) -> String {
        var result = content
        let patterns: [(String, String)] = [
            ("\"token\":\"", "\"token\":\"***\""),
            ("\"password\":\"", "\"password\":\"***\""),
            ("\"secret\":\"", "\"secret\":\"***\""),
            ("Authorization: Bearer ", "Authorization: Bearer ***"),
            ("X-K2-Token: ", "X-K2-Token: ***"),
        ]
        for (needle, replacement) in patterns {
            result = result.replacingOccurrences(of: needle, with: replacement)
        }
        return result
    }

    private func gzipCompress(_ data: Data) throws -> Data {
        return try data.gzipped()
    }

    private func generateS3Key(feedbackId: String?, udid: String) -> String {
        let now = Date()
        let dateFmt = DateFormatter()
        dateFmt.dateFormat = "yyyy/MM/dd"
        dateFmt.timeZone = TimeZone(identifier: "UTC")
        let timeFmt = DateFormatter()
        timeFmt.dateFormat = "HHmmss"
        timeFmt.timeZone = TimeZone(identifier: "UTC")

        let date = dateFmt.string(from: now)
        let timestamp = timeFmt.string(from: now)

        let prefix: String
        let identifier: String
        if let fbId = feedbackId {
            prefix = "feedback-logs"
            identifier = fbId
        } else {
            prefix = "service-logs"
            identifier = String(UUID().uuidString.prefix(8)).lowercased()
        }

        return "\(prefix)/\(udid)/\(date)/logs-\(timestamp)-\(identifier).zip"
    }

    private func uploadToS3(s3Key: String, data: Data) async throws {
        let url = URL(string: "\(s3BucketURL)/\(s3Key)")!
        var request = URLRequest(url: url)
        request.httpMethod = "PUT"
        let contentType = s3Key.hasSuffix(".zip") ? "application/zip" : "application/gzip"
        request.setValue(contentType, forHTTPHeaderField: "Content-Type")
        request.setValue("\(data.count)", forHTTPHeaderField: "Content-Length")
        request.timeoutInterval = 60

        let (_, response) = try await URLSession.shared.upload(for: request, from: data)
        guard let httpResponse = response as? HTTPURLResponse, (200..<300).contains(httpResponse.statusCode) else {
            let status = (response as? HTTPURLResponse)?.statusCode ?? -1
            throw NSError(domain: "K2Plugin", code: status, userInfo: [NSLocalizedDescriptionKey: "S3 upload failed: HTTP \(status)"])
        }
        logger.info("Uploaded to S3: \(s3Key)")
    }

    // MARK: - Log Level

    @objc func setLogLevel(_ call: CAPPluginCall) {
        let level = call.getString("level") ?? "info"
        // iOS: engine runs in NE extension (separate process).
        // Log level takes effect via configJSON.log.level at next connect.
        logger.info("setLogLevel: \(level) (takes effect on next connect)")
        call.resolve()
    }

    // MARK: - Dev Tools

    @objc func setDevEnabled(_ call: CAPPluginCall) {
        let enabled = call.getBool("enabled") ?? false
        logger.info("setDevEnabled: enabled=\(enabled), thread=\(Thread.isMainThread ? "main" : "background")")
        UserDefaults.standard.set(enabled, forKey: "k2_dev_enabled")
        logger.info("setDevEnabled: UserDefaults set, posting notification")
        NotificationCenter.default.post(name: .k2DevEnabledChanged, object: nil, userInfo: ["enabled": enabled])
        logger.info("setDevEnabled: notification posted")
        call.resolve()
    }

    // MARK: - Debug

    @objc func debugDump(_ call: CAPPluginCall) {
        let defaults = UserDefaults(suiteName: kAppGroup)
        let containerURL = FileManager.default.containerURL(forSecurityApplicationGroupIdentifier: kAppGroup)

        var dump: [String: Any] = [
            "appGroup": kAppGroup,
            "containerPath": containerURL?.path ?? "nil",
            "configJSON_exists": defaults?.string(forKey: "configJSON") != nil,
            "configJSON_length": defaults?.string(forKey: "configJSON")?.count ?? 0,
            "vpnError": defaults?.string(forKey: "vpnError") ?? "nil",
            "vpnManager_loaded": vpnManager != nil,
            "vpnManager_enabled": vpnManager?.isEnabled ?? false,
            "vpnManager_status": vpnManager != nil ? mapVPNStatus(vpnManager!.connection.status) : "no_manager",
            "vpnManager_protoBundleId": (vpnManager?.protocolConfiguration as? NETunnelProviderProtocol)?.providerBundleIdentifier ?? "nil",
            "appVersion": Bundle.main.infoDictionary?["CFBundleShortVersionString"] as? String ?? "unknown",
            "buildNumber": Bundle.main.infoDictionary?["CFBundleVersion"] as? String ?? "unknown",
            "deviceId": UIDevice.current.identifierForVendor?.uuidString ?? "nil",
        ]

        // Check cache dir contents
        if let containerPath = containerURL?.appendingPathComponent("k2").path {
            let contents = (try? FileManager.default.contentsOfDirectory(atPath: containerPath)) ?? []
            dump["cacheDirContents"] = contents
            dump["cacheDirExists"] = FileManager.default.fileExists(atPath: containerPath)
        }

        // Web update status
        let docsPath = FileManager.default.urls(for: .documentDirectory, in: .userDomainMask).first!
        let webUpdatePath = docsPath.appendingPathComponent("web-update")
        dump["webUpdate_exists"] = FileManager.default.fileExists(atPath: webUpdatePath.path)
        if let versionData = try? String(contentsOf: webUpdatePath.appendingPathComponent("version.txt"), encoding: .utf8) {
            dump["webUpdate_version"] = versionData.trimmingCharacters(in: .whitespacesAndNewlines)
        }

        logger.info("debugDump: \(dump)")
        call.resolve(dump)
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

    // remapStatusKeys is in K2Helpers.swift (module-level function)

    private func mapVPNStatus(_ status: NEVPNStatus) -> String {
        return mapVPNStatusString(status.rawValue)
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
                logger.debug("fetchManifest failed for \(endpoint): \(error.localizedDescription)")
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
                logger.warning("autocheck native update error: \(error.localizedDescription)")
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
                        logger.info("auto web OTA applied: \(remoteVersion)")
                    case .failure(let error):
                        logger.warning("auto web OTA failed: \(error.localizedDescription)")
                    }
                }
            } catch {
                logger.warning("autocheck web update error: \(error.localizedDescription)")
            }
        }
    }

    // isNewerVersion is in K2Helpers.swift (module-level function)

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

