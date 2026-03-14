import Foundation
import os.log

/// Thread-safe file logger for native-layer events in the NE process.
/// Writes to `native.log` in the shared App Group logs directory.
/// Only initialized in PacketTunnelProvider (NE process), NOT in K2Plugin (App process).
///
/// Uses POSIX `open(O_APPEND)` instead of `FileHandle(forWritingTo:)` to ensure
/// cross-process safety: if another process (K2Plugin) truncates the file,
/// the kernel always seeks to the actual end before each write — no null-byte gaps.
final class NativeLogger {
    static let shared = NativeLogger()

    private let queue = DispatchQueue(label: "com.allnationconnect.anc.wgios.native-logger")
    private var fd: Int32 = -1
    private var logFileURL: URL?
    private let maxFileSize: UInt64 = 50 * 1024 * 1024 // 50MB
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

            // O_APPEND: kernel atomically seeks to end before each write.
            // Safe against cross-process truncation (K2Plugin uploadLogs).
            let fileFd = Darwin.open(fileURL.path, O_WRONLY | O_APPEND | O_CREAT, 0o644)
            if fileFd >= 0 {
                self.fd = fileFd
            }
        }
    }

    /// Append a log entry. Thread-safe.
    func log(_ level: String, _ message: String) {
        queue.async { [weak self] in
            guard let self = self, self.fd >= 0, let url = self.logFileURL else { return }

            // Check file size, truncate if over limit
            if let attrs = try? FileManager.default.attributesOfItem(atPath: url.path),
               let size = attrs[.size] as? UInt64, size > self.maxFileSize {
                ftruncate(self.fd, 0)
            }

            let timestamp = self.dateFormatter.string(from: Date())
            let line = "[\(timestamp)] [\(level.uppercased())] \(message)\n"
            if let data = line.data(using: .utf8) {
                data.withUnsafeBytes { ptr in
                    if let base = ptr.baseAddress {
                        Darwin.write(self.fd, base, ptr.count)
                    }
                }
            }
        }
    }

    /// Close the file descriptor.
    func close() {
        queue.sync {
            if fd >= 0 {
                Darwin.close(fd)
                fd = -1
            }
        }
    }
}
