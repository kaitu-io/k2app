import XCTest

/// Tests for pure helper functions extracted from PacketTunnelProvider.swift (NEHelpers.swift).
/// These run on simulator — no NE or real device required.
class NEHelpersTests: XCTestCase {

    // MARK: - parseIPv4CIDR

    func testParseIPv4CIDR_24() {
        let result = parseIPv4CIDR("10.0.0.2/24")
        XCTAssertNotNil(result)
        XCTAssertEqual(result?.0, "10.0.0.2")
        XCTAssertEqual(result?.1, "255.255.255.0")
    }

    func testParseIPv4CIDR_0() {
        let result = parseIPv4CIDR("0.0.0.0/0")
        XCTAssertNotNil(result)
        XCTAssertEqual(result?.0, "0.0.0.0")
        XCTAssertEqual(result?.1, "0.0.0.0")
    }

    func testParseIPv4CIDR_32() {
        let result = parseIPv4CIDR("192.168.1.1/32")
        XCTAssertNotNil(result)
        XCTAssertEqual(result?.0, "192.168.1.1")
        XCTAssertEqual(result?.1, "255.255.255.255")
    }

    func testParseIPv4CIDR_16() {
        let result = parseIPv4CIDR("172.16.0.0/16")
        XCTAssertNotNil(result)
        XCTAssertEqual(result?.0, "172.16.0.0")
        XCTAssertEqual(result?.1, "255.255.0.0")
    }

    func testParseIPv4CIDR_defaultTunAddr() {
        // Default TUN address used by k2 engine
        let result = parseIPv4CIDR("198.18.0.7/15")
        XCTAssertNotNil(result)
        XCTAssertEqual(result?.0, "198.18.0.7")
        XCTAssertEqual(result?.1, "255.254.0.0")
    }

    func testParseIPv4CIDR_invalidNoSlash() {
        XCTAssertNil(parseIPv4CIDR("10.0.0.1"))
    }

    func testParseIPv4CIDR_invalidPrefix33() {
        XCTAssertNil(parseIPv4CIDR("10.0.0.1/33"))
    }

    func testParseIPv4CIDR_invalidPrefixNegative() {
        XCTAssertNil(parseIPv4CIDR("10.0.0.1/-1"))
    }

    func testParseIPv4CIDR_invalidGarbage() {
        XCTAssertNil(parseIPv4CIDR("invalid"))
    }

    func testParseIPv4CIDR_invalidPrefixNonNumeric() {
        XCTAssertNil(parseIPv4CIDR("10.0.0.1/abc"))
    }

    func testParseIPv4CIDR_emptyString() {
        XCTAssertNil(parseIPv4CIDR(""))
    }

    // MARK: - parseIPv6CIDR

    func testParseIPv6CIDR_64() {
        let result = parseIPv6CIDR("fd00::2/64")
        XCTAssertNotNil(result)
        XCTAssertEqual(result?.0, "fd00::2")
        XCTAssertEqual(result?.1, 64)
    }

    func testParseIPv6CIDR_128() {
        let result = parseIPv6CIDR("::1/128")
        XCTAssertNotNil(result)
        XCTAssertEqual(result?.0, "::1")
        XCTAssertEqual(result?.1, 128)
    }

    func testParseIPv6CIDR_0() {
        let result = parseIPv6CIDR("::/0")
        XCTAssertNotNil(result)
        XCTAssertEqual(result?.0, "::")
        XCTAssertEqual(result?.1, 0)
    }

    func testParseIPv6CIDR_defaultTunAddr() {
        // Default TUN IPv6 address used by k2 engine
        let result = parseIPv6CIDR("fdfe:dcba:9876::7/64")
        XCTAssertNotNil(result)
        XCTAssertEqual(result?.0, "fdfe:dcba:9876::7")
        XCTAssertEqual(result?.1, 64)
    }

    func testParseIPv6CIDR_invalidPrefix129() {
        XCTAssertNil(parseIPv6CIDR("fd00::2/129"))
    }

    func testParseIPv6CIDR_invalidNoSlash() {
        XCTAssertNil(parseIPv6CIDR("fd00::2"))
    }

    func testParseIPv6CIDR_invalidEmpty() {
        XCTAssertNil(parseIPv6CIDR(""))
    }

    // MARK: - stripPort

    func testStripPort_ipv4WithPort() {
        XCTAssertEqual(stripPort("8.8.8.8:53"), "8.8.8.8")
    }

    func testStripPort_ipv6WithBracketPort() {
        XCTAssertEqual(stripPort("[::1]:53"), "::1")
    }

    func testStripPort_ipv6WithBracketNoPort() {
        XCTAssertEqual(stripPort("[::1]"), "::1")
    }

    func testStripPort_bareIPv4() {
        XCTAssertEqual(stripPort("1.1.1.1"), "1.1.1.1")
    }

    func testStripPort_bareIPv6() {
        // IPv6 without brackets — colons make it ambiguous, should pass through
        XCTAssertEqual(stripPort("::1"), "::1")
    }

    func testStripPort_emptyString() {
        XCTAssertEqual(stripPort(""), "")
    }
}
