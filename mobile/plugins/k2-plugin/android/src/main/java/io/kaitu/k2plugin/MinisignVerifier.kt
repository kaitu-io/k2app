package io.kaitu.k2plugin

import org.bouncycastle.crypto.digests.Blake2bDigest
import org.bouncycastle.crypto.params.Ed25519PublicKeyParameters
import org.bouncycastle.crypto.signers.Ed25519Signer
import kotlin.io.encoding.Base64
import kotlin.io.encoding.ExperimentalEncodingApi

/**
 * Minisign signature verification for web OTA bundles (spec §3.4 / §5.1).
 *
 * Accepts ONLY prehashed signatures (algorithm "ED": Ed25519 over
 * BLAKE2b-512 of the file) — the publish-web-ota workflow signs with the
 * modern `minisign -S` CLI exclusively, same key pair as the Tauri desktop
 * updater. Legacy "Ed" signatures are rejected.
 *
 * Recorded blind spot: the trusted-comment global signature (lines 3-4 of the
 * .minisig) is NOT verified — comment tampering is undetectable, file-content
 * tampering is not.
 *
 * kotlin.io.encoding.Base64 keeps this object pure-JVM testable on minSdk 24
 * (java.util.Base64 needs API 26; android.util.Base64 can't run on the JVM).
 */
@OptIn(ExperimentalEncodingApi::class)
internal object MinisignVerifier {

    private const val SIG_LEN = 74     // 2 alg + 8 key id + 64 signature
    private const val PUBKEY_LEN = 42  // 2 alg + 8 key id + 32 key

    /**
     * @param data raw file bytes (web.zip)
     * @param sigBase64 base64 of the WHOLE .minisig text — the manifest `sig`
     *   field convention (same encoding as Tauri updater .sig files)
     * @param publicKeyBase64 the key line of the minisign public key
     *   (second line of minisign.pub, starts with "RW")
     */
    fun verify(data: ByteArray, sigBase64: String, publicKeyBase64: String): Boolean {
        val sigBytes: ByteArray
        val keyBytes: ByteArray
        try {
            val sigText = String(Base64.decode(sigBase64), Charsets.UTF_8)
            // .minisig layout: untrusted comment / signature / trusted comment / global sig
            val lines = sigText.split("\n").filter { it.isNotBlank() }
            if (lines.size < 2) return false
            sigBytes = Base64.decode(lines[1].trim())
            keyBytes = Base64.decode(publicKeyBase64.trim())
        } catch (_: IllegalArgumentException) {
            return false // malformed base64 anywhere = invalid signature
        }
        if (sigBytes.size != SIG_LEN || keyBytes.size != PUBKEY_LEN) return false
        // Signature algorithm must be "ED" (prehashed). Reject legacy "Ed".
        if (sigBytes[0] != 'E'.code.toByte() || sigBytes[1] != 'D'.code.toByte()) return false
        // Key ids (bytes 2..<10 of both structures) must match.
        for (i in 2 until 10) {
            if (sigBytes[i] != keyBytes[i]) return false
        }

        val digest = ByteArray(64)
        Blake2bDigest(512).apply {
            update(data, 0, data.size)
            doFinal(digest, 0)
        }
        val signature = sigBytes.copyOfRange(10, SIG_LEN)
        val publicKey = Ed25519PublicKeyParameters(keyBytes, 10)
        val signer = Ed25519Signer()
        signer.init(false, publicKey)
        signer.update(digest, 0, digest.size)
        return signer.verifySignature(signature)
    }
}
