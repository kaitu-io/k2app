package io.kaitu.k2plugin

import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test
import kotlin.io.encoding.Base64
import kotlin.io.encoding.ExperimentalEncodingApi

/**
 * Fixture generated with the standard minisign 0.12 CLI — the exact tool the
 * publish-web-ota workflow uses to sign web.zip:
 *   minisign -G -W -f -p k2test.pub -s k2test.key
 *   printf 'k2 web ota fixture v1\n' > payload.bin
 *   minisign -S -s k2test.key -m payload.bin -t 'k2 web ota test fixture' -c 'k2 web ota test fixture'
 * SIG_B64 = base64 of the whole payload.bin.minisig (the manifest `sig` convention).
 * Test-only key pair; the private key was discarded after generation.
 */
@OptIn(ExperimentalEncodingApi::class)
class MinisignVerifierTest {

    companion object {
        private const val PAYLOAD_B64 = "azIgd2ViIG90YSBmaXh0dXJlIHYxCg=="
        private const val TEST_PUBKEY = "RWQbVOK/uTNAZO5Fntw2AWmP++ASJrTLeQIg420038pe5ARj+QL27xpW"
        private const val SIG_B64 =
            "dW50cnVzdGVkIGNvbW1lbnQ6IGsyIHdlYiBvdGEgdGVzdCBmaXh0dXJlClJVUWJWT0svdVROQVpKYnJIalNmU1dxTFFSM3RGV3RmMzdyU2V6alhTbE56YlI0YWQyZWMvUDJ3TWVSWWF4dXNRcENCbitpL3ovN2FtR2NqbnFFTmxNOWhpSUxYRGovT213UT0KdHJ1c3RlZCBjb21tZW50OiBrMiB3ZWIgb3RhIHRlc3QgZml4dHVyZQo1L2p3TmFVMDNaNXZyZ0R1ZGVCM0NHMUVYcHRxeXpLUlIxcHFjOUZ5WkhPTE1UMWJWNHNpaU5ieWhhTjRTTmRoWmFJRGQ4QWFTVGJLWC80M0UvM2JBUT09Cg=="
    }

    private val payload = Base64.decode(PAYLOAD_B64)

    @Test
    fun verify_valid_signature() {
        assertTrue(MinisignVerifier.verify(payload, SIG_B64, TEST_PUBKEY))
    }

    @Test
    fun verify_tampered_payload_fails() {
        val tampered = payload.copyOf()
        tampered[0] = (tampered[0] + 1).toByte()
        assertFalse(MinisignVerifier.verify(tampered, SIG_B64, TEST_PUBKEY))
    }

    @Test
    fun verify_wrong_key_fails() {
        // Production pubkey — key id mismatch must fail before any crypto runs.
        assertFalse(MinisignVerifier.verify(payload, SIG_B64, K2PluginUtils.WEB_OTA_MINISIGN_PUBKEY))
    }

    @Test
    fun verify_garbage_sig_fails() {
        assertFalse(MinisignVerifier.verify(payload, "bm90IGEgc2ln", TEST_PUBKEY)) // valid b64, not a minisig
        assertFalse(MinisignVerifier.verify(payload, "!!!not-base64!!!", TEST_PUBKEY))
        assertFalse(MinisignVerifier.verify(payload, "", TEST_PUBKEY))
    }

    @Test
    fun production_pubkey_parses() {
        val key = Base64.decode(K2PluginUtils.WEB_OTA_MINISIGN_PUBKEY)
        assertEquals(42, key.size) // 2 alg + 8 key id + 32 key
        assertEquals('E'.code.toByte(), key[0])
        assertEquals('d'.code.toByte(), key[1]) // public keys are always alg "Ed"
    }
}
