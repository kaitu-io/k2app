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
/// Handles -beta.N pre-release suffixes correctly.
func isNewerVersion(_ remote: String, than local: String) -> Bool {
    let (rBase, rPre) = splitVersion(remote)
    let (lBase, lPre) = splitVersion(local)
    let baseCmp = compareSegments(rBase, lBase)
    if baseCmp != 0 { return baseCmp > 0 }
    // Same base: stable (no pre-release) > beta (has pre-release)
    if rPre == nil && lPre != nil { return true }
    if rPre != nil && lPre == nil { return false }
    if rPre == nil && lPre == nil { return false }
    // Both have pre-release: compare segments
    let rPreSegs = rPre!.split(separator: ".").map { Int($0) ?? 0 }
    let lPreSegs = lPre!.split(separator: ".").map { Int($0) ?? 0 }
    return compareSegments(rPreSegs, lPreSegs) > 0
}

private func splitVersion(_ v: String) -> (base: [Int], pre: String?) {
    let parts = v.split(separator: "-", maxSplits: 1)
    let base = parts[0].split(separator: ".").map { Int($0) ?? 0 }
    let pre = parts.count > 1 ? String(parts[1]) : nil
    return (base, pre)
}

private func compareSegments(_ a: [Int], _ b: [Int]) -> Int {
    let maxLen = max(a.count, b.count)
    for i in 0..<maxLen {
        let av = i < a.count ? a[i] : 0
        let bv = i < b.count ? b[i] : 0
        if av != bv { return av < bv ? -1 : 1 }
    }
    return 0
}
