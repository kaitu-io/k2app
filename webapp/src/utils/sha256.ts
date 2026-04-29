import jsSha256 from 'fast-sha256';

/**
 * SHA-256 with polyfill for WebViews where SubtleCrypto.digest is unavailable
 * (Capacitor uses https://localhost which is a secure context per spec, but
 * some Huawei OEM WebView builds reportedly strip SubtleCrypto). Native path
 * is preferred when present.
 *
 * Returns identical 32-byte output for either path; FIPS 180-2 verified.
 */
export async function sha256(data: Uint8Array): Promise<Uint8Array> {
  if (crypto.subtle?.digest) {
    const buf = await crypto.subtle.digest('SHA-256', data);
    return new Uint8Array(buf);
  }
  return jsSha256(data);
}
