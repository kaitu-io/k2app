import Foundation

/// Remap Go StatusJSON snake_case keys to JS camelCase.
func remapStatusKeys(_ json: [String: Any]) -> [String: Any] {
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

/// Map NEVPNStatus raw value to string without importing NetworkExtension.
/// Raw values: 0=invalid, 1=disconnected, 2=connecting, 3=connected, 4=reasserting, 5=disconnecting
func mapVPNStatusString(_ rawValue: Int) -> String {
    switch rawValue {
    case 3: return "connected"
    case 2: return "connecting"
    case 5: return "disconnecting"
    case 4: return "reconnecting"
    case 0, 1: return "disconnected"
    default: return "disconnected"
    }
}

/// Semantic version comparison: true if remote > local.
func isNewerVersion(_ remote: String, than local: String) -> Bool {
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
