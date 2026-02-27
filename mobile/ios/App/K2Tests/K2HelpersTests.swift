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
}
