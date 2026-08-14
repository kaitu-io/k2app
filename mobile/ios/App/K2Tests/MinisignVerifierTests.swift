import XCTest

/// Fixture identical to the Android MinisignVerifierTest — generated with the
/// standard minisign 0.12 CLI (the exact tool publish-web-ota.yml signs with):
///   minisign -G -W -f -p k2test.pub -s k2test.key
///   printf 'k2 web ota fixture v1\n' > payload.bin
///   minisign -S -s k2test.key -m payload.bin -t 'k2 web ota test fixture' -c 'k2 web ota test fixture'
class MinisignVerifierTests: XCTestCase {

    static let payloadB64 = "azIgd2ViIG90YSBmaXh0dXJlIHYxCg=="
    static let testPubkey = "RWQbVOK/uTNAZO5Fntw2AWmP++ASJrTLeQIg420038pe5ARj+QL27xpW"
    static let sigB64 =
        "dW50cnVzdGVkIGNvbW1lbnQ6IGsyIHdlYiBvdGEgdGVzdCBmaXh0dXJlClJVUWJWT0svdVROQVpKYnJIalNmU1dxTFFSM3RGV3RmMzdyU2V6alhTbE56YlI0YWQyZWMvUDJ3TWVSWWF4dXNRcENCbitpL3ovN2FtR2NqbnFFTmxNOWhpSUxYRGovT213UT0KdHJ1c3RlZCBjb21tZW50OiBrMiB3ZWIgb3RhIHRlc3QgZml4dHVyZQo1L2p3TmFVMDNaNXZyZ0R1ZGVCM0NHMUVYcHRxeXpLUlIxcHFjOUZ5WkhPTE1UMWJWNHNpaU5ieWhhTjRTTmRoWmFJRGQ4QWFTVGJLWC80M0UvM2JBUT09Cg=="
    static let productionPubkey = "RWSD3s7XX1TXQLaSafFQyIycEGH5v0d7EOsPUmQGJMRjnCuqq3eAVKEE"

    private func hex(_ data: Data) -> String {
        data.map { String(format: "%02x", $0) }.joined()
    }

    // MARK: - BLAKE2b-512 vectors (each re-derivable via:
    //   python3 -c "import hashlib; print(hashlib.blake2b(b'<input>').hexdigest())")

    func testBlake2b_emptyInput() {
        XCTAssertEqual(
            hex(Blake2b.hash512(Data())),
            "786a02f742015903c6c6fd852552d272912f4740e15847618a86e217f71f5419d25e1031afee585313896444934eb04b903a685b1448b755d56f701afe9be2ce")
    }

    func testBlake2b_abc() {
        XCTAssertEqual(
            hex(Blake2b.hash512(Data("abc".utf8))),
            "ba80a53f981c4d0d6a2797b69f12f6e94c212f14685ac4b74b12bb6fdbffa2d17d87c5392aab792dc252d5de4533cc9518d38aa8dbf1925ab92386edd4009923")
    }

    func testBlake2b_multiBlockInput() {
        // 300 bytes of 'a' spans three 128-byte compression blocks.
        XCTAssertEqual(
            hex(Blake2b.hash512(Data(repeating: 0x61, count: 300))),
            "a2ff3040eda405b929c2fc2fd93e8add6ac3bb5369b679bae170ac6956863ca006285f132a868000fc3fae5bc696e5d17fe3fddfb4a342876c40451184742986")
    }

    // MARK: - Minisign end-to-end

    func testVerify_validSignature() {
        let payload = Data(base64Encoded: Self.payloadB64)!
        XCTAssertTrue(MinisignVerifier.verify(data: payload, sigBase64: Self.sigB64, publicKeyBase64: Self.testPubkey))
    }

    func testVerify_tamperedPayloadFails() {
        var payload = Data(base64Encoded: Self.payloadB64)!
        payload[0] ^= 0xff
        XCTAssertFalse(MinisignVerifier.verify(data: payload, sigBase64: Self.sigB64, publicKeyBase64: Self.testPubkey))
    }

    func testVerify_wrongKeyFails() {
        let payload = Data(base64Encoded: Self.payloadB64)!
        XCTAssertFalse(MinisignVerifier.verify(data: payload, sigBase64: Self.sigB64, publicKeyBase64: Self.productionPubkey))
    }

    func testVerify_garbageSigFails() {
        let payload = Data(base64Encoded: Self.payloadB64)!
        XCTAssertFalse(MinisignVerifier.verify(data: payload, sigBase64: "bm90IGEgc2ln", publicKeyBase64: Self.testPubkey))
        XCTAssertFalse(MinisignVerifier.verify(data: payload, sigBase64: "!!!not-base64!!!", publicKeyBase64: Self.testPubkey))
        XCTAssertFalse(MinisignVerifier.verify(data: payload, sigBase64: "", publicKeyBase64: Self.testPubkey))
    }

    func testProductionPubkeyShape() {
        let key = Data(base64Encoded: Self.productionPubkey)!
        XCTAssertEqual(key.count, 42) // 2 alg + 8 key id + 32 key
        XCTAssertEqual(key[0], UInt8(ascii: "E"))
        XCTAssertEqual(key[1], UInt8(ascii: "d")) // public keys are always alg "Ed"
    }
}
