import Foundation

/// Parse IPv4 CIDR "10.0.0.2/24" → ("10.0.0.2", "255.255.255.0"), nil on failure.
func parseIPv4CIDR(_ cidr: String) -> (String, String)? {
    let parts = cidr.split(separator: "/", maxSplits: 1)
    guard parts.count == 2, let prefix = Int(parts[1]), prefix >= 0, prefix <= 32 else { return nil }
    let mask = prefix == 0 ? UInt32(0) : UInt32.max << (32 - prefix)
    let m = String(format: "%d.%d.%d.%d",
                   (mask >> 24) & 0xFF, (mask >> 16) & 0xFF, (mask >> 8) & 0xFF, mask & 0xFF)
    return (String(parts[0]), m)
}

/// Parse IPv6 CIDR "fd00::2/64" → ("fd00::2", 64), nil on failure.
func parseIPv6CIDR(_ cidr: String) -> (String, Int)? {
    let parts = cidr.split(separator: "/", maxSplits: 1)
    guard parts.count == 2, let prefix = Int(parts[1]), prefix >= 0, prefix <= 128 else { return nil }
    return (String(parts[0]), prefix)
}

/// Strip port from "8.8.8.8:53" → "8.8.8.8". Handles IPv6 "[::1]:53" → "::1".
func stripPort(_ addr: String) -> String {
    if addr.hasPrefix("["), let closeBracket = addr.firstIndex(of: "]") {
        return String(addr[addr.index(after: addr.startIndex)..<closeBracket])
    }
    let parts = addr.split(separator: ":")
    if parts.count == 2 { return String(parts[0]) } // "ip:port"
    return addr // bare IP or IPv6 without port
}
