import { describe, it, expect, afterEach } from 'vitest';
import { sha256 } from '../sha256';

// Known SHA-256 test vectors (FIPS 180-2 / RFC 6234)
const VECTORS: Array<[string, string]> = [
  ['abc', 'ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad'],
  ['', 'e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855'],
  [
    'abcdbcdecdefdefgefghfghighijhijkijkljklmklmnlmnomnopnopq',
    '248d6a61d20638b8e5c026930c3e6039a33ce45964ff2167f6ecedd419db06c1',
  ],
];

function bytesToHex(b: Uint8Array): string {
  return Array.from(b, x => x.toString(16).padStart(2, '0')).join('');
}

describe('sha256', () => {
  const originalSubtle = crypto.subtle;

  afterEach(() => {
    Object.defineProperty(crypto, 'subtle', {
      value: originalSubtle,
      configurable: true,
      writable: true,
    });
  });

  it.each(VECTORS)('native subtle.digest matches FIPS vector for %j', async (input, expected) => {
    const hash = await sha256(new TextEncoder().encode(input));
    expect(bytesToHex(hash)).toBe(expected);
    expect(hash.byteLength).toBe(32);
  });

  it.each(VECTORS)('fallback (subtle missing) matches FIPS vector for %j', async (input, expected) => {
    Object.defineProperty(crypto, 'subtle', { value: undefined, configurable: true, writable: true });

    const hash = await sha256(new TextEncoder().encode(input));
    expect(bytesToHex(hash)).toBe(expected);
    expect(hash.byteLength).toBe(32);
  });

  it('native and fallback produce identical output for the same input', async () => {
    const data = new TextEncoder().encode('kaitu-device-udid-consistency-check');

    const nativeHash = await sha256(data);

    Object.defineProperty(crypto, 'subtle', { value: undefined, configurable: true, writable: true });
    const fallbackHash = await sha256(data);

    expect(bytesToHex(fallbackHash)).toBe(bytesToHex(nativeHash));
  });
});
