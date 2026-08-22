import XCTest

/// Tests for pure helper functions extracted from K2Plugin.swift (K2Helpers.swift).
/// These run on simulator — no NE, VPN, or real device required.
class K2HelpersTests: XCTestCase {

    // MARK: - remapStatusKeys

    func testRemapStatusKeys_snakeToCamel() {
        let input: [String: Any] = [
            "connected_at": "2024-01-01T00:00:00Z",
            "uptime_seconds": 3600,
            "state": "connected"
        ]
        let result = remapStatusKeys(input)
        XCTAssertEqual(result["connectedAt"] as? String, "2024-01-01T00:00:00Z")
        XCTAssertEqual(result["uptimeSeconds"] as? Int, 3600)
        XCTAssertEqual(result["state"] as? String, "connected")  // unmapped keys pass through
    }

    func testRemapStatusKeys_noSnakeKeys() {
        let input: [String: Any] = ["state": "disconnected"]
        let result = remapStatusKeys(input)
        XCTAssertEqual(result["state"] as? String, "disconnected")
        XCTAssertEqual(result.count, 1)
    }

    func testRemapStatusKeys_emptyDict() {
        let result = remapStatusKeys([:])
        XCTAssertTrue(result.isEmpty)
    }

    func testRemapStatusKeys_preservesNestedError() {
        let input: [String: Any] = [
            "state": "disconnected",
            "error": ["code": 503, "message": "server unreachable"] as [String: Any]
        ]
        let result = remapStatusKeys(input)
        XCTAssertEqual(result["state"] as? String, "disconnected")
        let error = result["error"] as? [String: Any]
        XCTAssertNotNil(error)
        XCTAssertEqual(error?["code"] as? Int, 503)
        XCTAssertEqual(error?["message"] as? String, "server unreachable")
    }

    // MARK: - mapVPNStatusString

    func testMapVPNStatusString_connected() {
        XCTAssertEqual(mapVPNStatusString(3), "connected")
    }

    func testMapVPNStatusString_connecting() {
        XCTAssertEqual(mapVPNStatusString(2), "connecting")
    }

    func testMapVPNStatusString_disconnecting() {
        XCTAssertEqual(mapVPNStatusString(5), "disconnecting")
    }

    func testMapVPNStatusString_reasserting() {
        XCTAssertEqual(mapVPNStatusString(4), "reconnecting")
    }

    func testMapVPNStatusString_disconnected() {
        XCTAssertEqual(mapVPNStatusString(1), "disconnected")
    }

    func testMapVPNStatusString_invalid() {
        XCTAssertEqual(mapVPNStatusString(0), "disconnected")
    }

    func testMapVPNStatusString_unknown() {
        XCTAssertEqual(mapVPNStatusString(99), "disconnected")
    }

    // MARK: - isNewerVersion

    func testIsNewerVersion_majorBump() {
        XCTAssertTrue(isNewerVersion("2.0.0", than: "1.0.0"))
    }

    func testIsNewerVersion_minorBump() {
        XCTAssertTrue(isNewerVersion("1.1.0", than: "1.0.0"))
    }

    func testIsNewerVersion_patchBump() {
        XCTAssertTrue(isNewerVersion("1.0.1", than: "1.0.0"))
    }

    func testIsNewerVersion_equal() {
        XCTAssertFalse(isNewerVersion("1.0.0", than: "1.0.0"))
    }

    func testIsNewerVersion_older() {
        XCTAssertFalse(isNewerVersion("0.9.0", than: "1.0.0"))
    }

    func testIsNewerVersion_differentLengths() {
        XCTAssertTrue(isNewerVersion("1.0.0.1", than: "1.0.0"))
        XCTAssertFalse(isNewerVersion("1.0.0", than: "1.0.0.1"))
    }

    func testIsNewerVersion_twoComponent() {
        XCTAssertTrue(isNewerVersion("1.1", than: "1.0"))
        XCTAssertFalse(isNewerVersion("1.0", than: "1.1"))
    }

    // MARK: - isCompatibleBridgeVersion

    func testBridgeVersion_nilPasses() {
        XCTAssertTrue(isCompatibleBridgeVersion(nil))
    }

    func testBridgeVersion_equalPasses() {
        XCTAssertTrue(isCompatibleBridgeVersion(k2BridgeApiVersion))
    }

    func testBridgeVersion_newerManifestFails() {
        XCTAssertFalse(isCompatibleBridgeVersion(k2BridgeApiVersion + 1))
    }

    func testBridgeVersion_explicitVersions() {
        XCTAssertTrue(isCompatibleBridgeVersion(1, bridgeVersion: 2))
        XCTAssertFalse(isCompatibleBridgeVersion(3, bridgeVersion: 2))
    }

    // MARK: - Web OTA boot decision (mirrors Android WebBootDecisionTest)

    /// A rollback must NAME the version it discards. Rolling back deletes
    /// web-update/ — version.txt included — so a version not captured here is
    /// gone, and the auto-check 3s later re-downloads the same broken bundle.
    func testDecideWebBoot_unconfirmedBootRollsBackAndNamesVersion() {
        XCTAssertEqual(
            decideWebBoot(hasWebUpdateDir: true, hasBootPending: true, hasIndex: true, diskVersion: "0.4.8.20000000"),
            .rollback(version: "0.4.8.20000000"))
    }

    func testDecideWebBoot_rollbackWithoutReadableVersionStillRollsBack() {
        XCTAssertEqual(
            decideWebBoot(hasWebUpdateDir: true, hasBootPending: true, hasIndex: true, diskVersion: nil),
            .rollback(version: nil))
    }

    /// An unconfirmed boot is a FAILURE and must be quarantined even when
    /// index.html is also gone; ordering the corrupt check first would
    /// silently drop the quarantine.
    func testDecideWebBoot_unconfirmedBootWinsOverCorruptCheck() {
        XCTAssertEqual(
            decideWebBoot(hasWebUpdateDir: true, hasBootPending: true, hasIndex: false, diskVersion: "0.4.8.20000000"),
            .rollback(version: "0.4.8.20000000"))
    }

    func testDecideWebBoot_confirmedBundleServedFromDisk() {
        XCTAssertEqual(
            decideWebBoot(hasWebUpdateDir: true, hasBootPending: false, hasIndex: true, diskVersion: "0.4.8.20000000"),
            .serveDisk)
    }

    func testDecideWebBoot_directoryWithoutIndexIsCleaned() {
        XCTAssertEqual(
            decideWebBoot(hasWebUpdateDir: true, hasBootPending: false, hasIndex: false, diskVersion: nil),
            .cleanCorrupt)
    }

    func testDecideWebBoot_noBundleServesBundled() {
        XCTAssertEqual(
            decideWebBoot(hasWebUpdateDir: false, hasBootPending: false, hasIndex: false, diskVersion: nil),
            .serveBundled)
    }

    func testDecideWebBoot_strayMarkerWithoutBundleServesBundled() {
        XCTAssertEqual(
            decideWebBoot(hasWebUpdateDir: false, hasBootPending: true, hasIndex: false, diskVersion: nil),
            .serveBundled)
    }

    // MARK: - Web OTA apply gate (quarantine)

    /// Without this the shell is a reinstall treadmill — see the Android
    /// AutoUpdatePlanTest quarantine block for the full mechanism.
    func testWebBundleSkipReason_quarantinedVersionIsNotReapplied() {
        XCTAssertEqual(
            webBundleSkipReason(remoteVersion: "0.4.8.20000000", localVersion: "0.4.8", quarantinedVersion: "0.4.8.20000000"),
            "version quarantined")
    }

    /// Version-scoped, not permanent: a newer bundle clears it.
    func testWebBundleSkipReason_newerVersionClearsQuarantine() {
        XCTAssertNil(
            webBundleSkipReason(remoteVersion: "0.4.8.20000001", localVersion: "0.4.8", quarantinedVersion: "0.4.8.20000000"))
    }

    func testWebBundleSkipReason_noNewerVersion() {
        XCTAssertEqual(
            webBundleSkipReason(remoteVersion: "0.4.8", localVersion: "0.4.8", quarantinedVersion: nil),
            "no newer version")
    }

    func testWebBundleSkipReason_appliesWhenNewerAndNotQuarantined() {
        XCTAssertNil(
            webBundleSkipReason(remoteVersion: "0.4.8.20000000", localVersion: "0.4.8", quarantinedVersion: nil))
    }
}
