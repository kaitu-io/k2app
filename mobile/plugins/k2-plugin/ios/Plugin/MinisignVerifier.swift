import Foundation
import CryptoKit

/// BLAKE2b-512, sequential, unkeyed (RFC 7693) — vendored because neither
/// CryptoKit nor CommonCrypto provides BLAKE2. Only what minisign prehashed
/// verification needs. Correctness is pinned by MinisignVerifierTests: three
/// hashlib.blake2b vectors (empty / "abc" / 300-byte multi-block) plus an
/// end-to-end signature produced by the real minisign CLI.
struct Blake2b {
    private static let iv: [UInt64] = [
        0x6a09_e667_f3bc_c908, 0xbb67_ae85_84ca_a73b,
        0x3c6e_f372_fe94_f82b, 0xa54f_f53a_5f1d_36f1,
        0x510e_527f_ade6_82d1, 0x9b05_688c_2b3e_6c1f,
        0x1f83_d9ab_fb41_bd6b, 0x5be0_cd19_137e_2179,
    ]

    private static let sigma: [[Int]] = [
        [0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15],
        [14, 10, 4, 8, 9, 15, 13, 6, 1, 12, 0, 2, 11, 7, 5, 3],
        [11, 8, 12, 0, 5, 2, 15, 13, 10, 14, 3, 6, 7, 1, 9, 4],
        [7, 9, 3, 1, 13, 12, 11, 14, 2, 6, 5, 10, 4, 0, 15, 8],
        [9, 0, 5, 7, 2, 4, 10, 15, 14, 1, 11, 12, 6, 8, 3, 13],
        [2, 12, 6, 10, 0, 11, 8, 3, 4, 13, 7, 5, 15, 14, 1, 9],
        [12, 5, 1, 15, 14, 13, 4, 10, 0, 7, 6, 3, 9, 2, 8, 11],
        [13, 11, 7, 14, 12, 1, 3, 9, 5, 0, 15, 4, 8, 6, 2, 10],
        [6, 15, 14, 9, 11, 3, 0, 8, 12, 2, 13, 7, 1, 4, 10, 5],
        [10, 2, 8, 4, 7, 6, 1, 5, 15, 11, 9, 14, 3, 12, 13, 0],
    ]

    static func hash512(_ message: Data) -> Data {
        var h = iv
        // Parameter block word 0: digest_length=64 | key_length<<8 | fanout<<16 | depth<<24
        h[0] ^= 0x0101_0040

        let bytes = [UInt8](message)
        var t: UInt64 = 0
        var offset = 0
        // Process all full blocks EXCEPT the last block (the final block — even
        // when exactly 128 bytes — is compressed with the finalization flag).
        while bytes.count - offset > 128 {
            t &+= 128
            compress(&h, Array(bytes[offset..<offset + 128]), t: t, isLast: false)
            offset += 128
        }
        var lastBlock = [UInt8](repeating: 0, count: 128)
        let remaining = bytes.count - offset
        for i in 0..<remaining { lastBlock[i] = bytes[offset + i] }
        t &+= UInt64(remaining)
        compress(&h, lastBlock, t: t, isLast: true)

        var out = Data(capacity: 64)
        for word in h {
            withUnsafeBytes(of: word.littleEndian) { out.append(contentsOf: $0) }
        }
        return out
    }

    private static func compress(_ h: inout [UInt64], _ block: [UInt8], t: UInt64, isLast: Bool) {
        var m = [UInt64](repeating: 0, count: 16)
        for i in 0..<16 {
            var word: UInt64 = 0
            for j in 0..<8 { word |= UInt64(block[i * 8 + j]) << (8 * UInt64(j)) }
            m[i] = word
        }
        var v = h + iv
        v[12] ^= t // low counter word; message sizes here never exceed 2^64
        if isLast { v[14] = ~v[14] }
        for round in 0..<12 {
            let s = sigma[round % 10]
            g(&v, 0, 4, 8, 12, m[s[0]], m[s[1]])
            g(&v, 1, 5, 9, 13, m[s[2]], m[s[3]])
            g(&v, 2, 6, 10, 14, m[s[4]], m[s[5]])
            g(&v, 3, 7, 11, 15, m[s[6]], m[s[7]])
            g(&v, 0, 5, 10, 15, m[s[8]], m[s[9]])
            g(&v, 1, 6, 11, 12, m[s[10]], m[s[11]])
            g(&v, 2, 7, 8, 13, m[s[12]], m[s[13]])
            g(&v, 3, 4, 9, 14, m[s[14]], m[s[15]])
        }
        for i in 0..<8 { h[i] ^= v[i] ^ v[i + 8] }
    }

    private static func g(_ v: inout [UInt64], _ a: Int, _ b: Int, _ c: Int, _ d: Int, _ x: UInt64, _ y: UInt64) {
        v[a] = v[a] &+ v[b] &+ x
        v[d] = rotr(v[d] ^ v[a], 32)
        v[c] = v[c] &+ v[d]
        v[b] = rotr(v[b] ^ v[c], 24)
        v[a] = v[a] &+ v[b] &+ y
        v[d] = rotr(v[d] ^ v[a], 16)
        v[c] = v[c] &+ v[d]
        v[b] = rotr(v[b] ^ v[c], 63)
    }

    private static func rotr(_ x: UInt64, _ n: UInt64) -> UInt64 {
        (x >> n) | (x << (64 - n))
    }
}

/// Minisign signature verification for web OTA bundles (spec §3.4 / §5.1).
///
/// Accepts ONLY prehashed signatures (algorithm "ED": Ed25519 over
/// BLAKE2b-512 of the file) — the publish-web-ota workflow signs with the
/// modern `minisign -S` CLI exclusively, same key pair as the Tauri desktop
/// updater. Legacy "Ed" signatures are rejected.
///
/// Recorded blind spot: the trusted-comment global signature (.minisig lines
/// 3-4) is NOT verified — comment tampering is undetectable, file content is.
enum MinisignVerifier {

    /// - Parameters:
    ///   - data: raw file bytes (web.zip)
    ///   - sigBase64: base64 of the WHOLE .minisig text — the manifest `sig`
    ///     field convention (same encoding as Tauri updater .sig files)
    ///   - publicKeyBase64: the key line of the minisign public key
    ///     (second line of minisign.pub, starts with "RW")
    static func verify(data: Data, sigBase64: String, publicKeyBase64: String) -> Bool {
        guard let sigFileData = Data(base64Encoded: sigBase64),
              let sigText = String(data: sigFileData, encoding: .utf8) else { return false }
        // .minisig layout: untrusted comment / signature / trusted comment / global sig
        let lines = sigText.split(separator: "\n").map(String.init).filter { !$0.isEmpty }
        guard lines.count >= 2,
              let sigBytes = Data(base64Encoded: lines[1]),
              sigBytes.count == 74, // 2 alg + 8 key id + 64 signature
              let keyBytes = Data(base64Encoded: publicKeyBase64.trimmingCharacters(in: .whitespaces)),
              keyBytes.count == 42  // 2 alg + 8 key id + 32 key
        else { return false }
        // Signature algorithm must be "ED" (prehashed). Reject legacy "Ed".
        guard sigBytes[0] == UInt8(ascii: "E"), sigBytes[1] == UInt8(ascii: "D") else { return false }
        // Key ids (bytes 2..<10 of both structures) must match.
        guard sigBytes.subdata(in: 2..<10) == keyBytes.subdata(in: 2..<10) else { return false }

        let signature = sigBytes.subdata(in: 10..<74)
        let rawKey = keyBytes.subdata(in: 10..<42)
        guard let publicKey = try? Curve25519.Signing.PublicKey(rawRepresentation: rawKey) else { return false }
        return publicKey.isValidSignature(signature, for: Blake2b.hash512(data))
    }
}
