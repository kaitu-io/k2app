import XCTest

/// Guard: derived brand values MUST equal the pre-split literals for the kaitu
/// build, or live users' NE configs get wiped by K2Plugin.loadVPNManager()'s
/// stale-config cleanup (which matches on providerBundleIdentifier +
/// localizedDescription). See CLAUDE.md / task-5-brief.md for the full story.
final class BrandDerivationTests: XCTestCase {
    func testKaituDerivationsMatchLegacyLiterals() {
        let bundleID = Bundle.main.bundleIdentifier ?? ""
        guard bundleID.hasPrefix("com.allnationconnect") else { return } // overleap build: skip
        XCTAssertEqual(bundleID + ".ThePacketTunnel", "com.allnationconnect.anc.wgios.ThePacketTunnel")
        XCTAssertEqual(Bundle.main.object(forInfoDictionaryKey: "K2AppGroup") as? String, "group.io.kaitu")
        XCTAssertEqual(Bundle.main.object(forInfoDictionaryKey: "K2VpnDisplayName") as? String, "kaitu.io")
    }
}
