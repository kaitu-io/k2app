import Foundation
import Security

/// Minimal Keychain string store backing the `secureGet/secureSet/secureRemove`
/// bridge methods (bridge v6). Unlike `storage*` (a JSON file in the app
/// container, wiped on uninstall), Keychain items survive app reinstall, so the
/// webapp uses this for device-account credentials.
///
/// Generic-password items keyed by (service = bundle id, account = key).
/// Accessibility is `AfterFirstUnlockThisDeviceOnly`: readable in the background
/// after first unlock, never synced to iCloud Keychain or restored to another
/// device.
enum KeychainStore {
    private static var service: String { Bundle.main.bundleIdentifier ?? "kaitu" }

    private static func baseQuery(_ key: String) -> [String: Any] {
        return [
            kSecClass as String: kSecClassGenericPassword,
            kSecAttrService as String: service,
            kSecAttrAccount as String: key,
        ]
    }

    static func get(_ key: String) -> String? {
        var query = baseQuery(key)
        query[kSecReturnData as String] = true
        query[kSecMatchLimit as String] = kSecMatchLimitOne
        var item: CFTypeRef?
        guard SecItemCopyMatching(query as CFDictionary, &item) == errSecSuccess,
              let data = item as? Data else {
            return nil
        }
        return String(data: data, encoding: .utf8)
    }

    /// Delete-then-add, so a changed accessibility class never lingers on an
    /// old item. Returns false if the add fails.
    @discardableResult
    static func set(_ key: String, _ value: String) -> Bool {
        SecItemDelete(baseQuery(key) as CFDictionary)
        var attrs = baseQuery(key)
        attrs[kSecValueData as String] = Data(value.utf8)
        attrs[kSecAttrAccessible as String] = kSecAttrAccessibleAfterFirstUnlockThisDeviceOnly
        return SecItemAdd(attrs as CFDictionary, nil) == errSecSuccess
    }

    static func remove(_ key: String) {
        SecItemDelete(baseQuery(key) as CFDictionary)
    }
}
