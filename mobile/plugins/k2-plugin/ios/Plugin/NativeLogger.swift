import Foundation
import os.log

/// Thread-safe file logger for native-layer events in the NE process.
/// Writes to `native.log` in the shared App Group logs directory.
/// Only initialized in PacketTunnelProvider (NE process), NOT in K2Plugin (App process).
final class NativeLogger {
    static let shared = NativeLogger()

    private let queue = DispatchQueue(label: "com.allnationconnect.anc.wgios.native-logger")
    private var fileHandle: FileHandle?
    private var logFileURL: URL?
    private let maxFileSize: UInt64 = 20 * 1024 * 1024 // 20 MB (was 50 MB)
    private let dateFormatter = ISO8601DateFormatter()

    private init() {}

    /// Initialize logger with the shared logs directory.
    /// Call from PacketTunnelProvider.startTunnel() with the App Group logs path.
    func setup(logsDir: URL) {
        queue.sync {
            let fileURL = logsDir.appendingPathComponent("native.log")
            self.logFileURL = fileURL

            if !FileManager.default.fileExists(atPath: fileURL.path) {
                FileManager.default.createFile(atPath: fileURL.path, contents: nil)
            }

            self.fileHandle = try? FileHandle(forWritingTo: fileURL)
            self.fileHandle?.seekToEndOfFile()
        }
    }

    /// Append a log entry. Thread-safe.
    func log(_ level: String, _ message: String) {
        queue.async { [weak self] in
            guard let self = self, let handle = self.fileHandle, let url = self.logFileURL else { return }

            // Check file size, truncate if over limit
            if let attrs = try? FileManager.default.attributesOfItem(atPath: url.path),
               let size = attrs[.size] as? UInt64, size > self.maxFileSize {
                handle.truncateFile(atOffset: 0)
                handle.seekToEndOfFile()
            }

            let timestamp = self.dateFormatter.string(from: Date())
            let line = "[\(timestamp)] [\(level.uppercased())] \(message)\n"
            if let data = line.data(using: .utf8) {
                handle.write(data)
            }
        }
    }

    /// Close the file handle.
    func close() {
        queue.sync {
            fileHandle?.closeFile()
            fileHandle = nil
        }
    }
}
