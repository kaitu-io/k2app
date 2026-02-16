import { describe, it, expect, vi, beforeEach } from 'vitest';
import { readFileSync } from 'node:fs';
import { resolve } from 'node:path';
import {
  resolveEntry,
  decrypt,
  DECRYPTION_KEY,
  CDN_SOURCES,
  DEFAULT_ENTRY,
} from '../antiblock';

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

function hexToBytes(hex: string): Uint8Array {
  const bytes = new Uint8Array(hex.length / 2);
  for (let i = 0; i < hex.length; i += 2) {
    bytes[i / 2] = parseInt(hex.substr(i, 2), 16);
  }
  return bytes;
}

async function encryptForTest(
  plaintext: string,
  keyHex: string,
): Promise<string> {
  const rawKey = hexToBytes(keyHex);
  const key = await crypto.subtle.importKey(
    'raw',
    rawKey.buffer as ArrayBuffer,
    'AES-GCM',
    false,
    ['encrypt'],
  );
  const iv = crypto.getRandomValues(new Uint8Array(12));
  const encoded = new TextEncoder().encode(plaintext);
  const ciphertext = await crypto.subtle.encrypt(
    { name: 'AES-GCM', iv: iv.buffer as ArrayBuffer },
    key,
    encoded.buffer as ArrayBuffer,
  );
  const result = new Uint8Array(iv.length + ciphertext.byteLength);
  result.set(iv);
  result.set(new Uint8Array(ciphertext), iv.length);
  return btoa(String.fromCharCode(...result));
}

/** A valid 256-bit hex key for test fixtures */
const TEST_KEY_HEX =
  'a01b2c3d4e5f60718293a4b5c6d7e8f90a1b2c3d4e5f60718293a4b5c6d7e8f9';

// ---------------------------------------------------------------------------
// Source code (for static analysis tests)
// ---------------------------------------------------------------------------

const sourceCode = readFileSync(
  resolve(__dirname, '../antiblock.ts'),
  'utf-8',
);

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

describe('antiblock — AES-256-GCM decryption', () => {
  // ── Crypto tests ────────────────────────────────────────────────────────

  it('test_decrypt_roundtrip', async () => {
    const original = JSON.stringify({
      entries: ['https://example.com', 'https://backup.example.com'],
    });
    const encrypted = await encryptForTest(original, TEST_KEY_HEX);
    const decrypted = await decrypt(encrypted, TEST_KEY_HEX);
    expect(decrypted).toBe(original);
  });

  it('test_decrypt_wrong_key_returns_null', async () => {
    const original = 'https://example.com';
    const encrypted = await encryptForTest(original, TEST_KEY_HEX);
    // Different key — last byte changed
    const wrongKey =
      'a01b2c3d4e5f60718293a4b5c6d7e8f90a1b2c3d4e5f60718293a4b5c6d7e8ff';
    const result = await decrypt(encrypted, wrongKey);
    expect(result).toBeNull();
  });

  it('test_decrypt_tampered_payload_returns_null', async () => {
    const original = 'https://example.com';
    const encrypted = await encryptForTest(original, TEST_KEY_HEX);
    // Decode base64 → flip a byte in the ciphertext portion → re-encode
    const raw = Uint8Array.from(atob(encrypted), (c) => c.charCodeAt(0));
    // Flip a byte after the 12-byte IV (i.e. in the ciphertext)
    raw[14] = raw[14]! ^ 0xff;
    const tampered = btoa(String.fromCharCode(...raw));
    const result = await decrypt(tampered, TEST_KEY_HEX);
    expect(result).toBeNull();
  });

  // ── resolveEntry integration tests ──────────────────────────────────────

  describe('resolveEntry', () => {
    let mockFetch: ReturnType<typeof vi.fn>;
    let mockLocalStorage: {
      getItem: ReturnType<typeof vi.fn>;
      setItem: ReturnType<typeof vi.fn>;
      removeItem: ReturnType<typeof vi.fn>;
    };

    beforeEach(() => {
      mockFetch = vi.fn();
      vi.stubGlobal('fetch', mockFetch);
      mockLocalStorage = {
        getItem: vi.fn(),
        setItem: vi.fn(),
        removeItem: vi.fn(),
      };
      vi.stubGlobal('localStorage', mockLocalStorage);
    });

    it('test_cache_hit_skips_fetch', async () => {
      mockLocalStorage.getItem.mockReturnValue('https://cached.example.com');
      mockFetch.mockRejectedValue(new Error('should not be called'));

      const entry = await resolveEntry();
      expect(entry).toBe('https://cached.example.com');
      // fetch should NOT have been awaited/blocking — resolveEntry returns
      // immediately from cache
    });

    it('test_all_cdn_fail_returns_default', async () => {
      mockLocalStorage.getItem.mockReturnValue(null);
      mockFetch.mockRejectedValue(new Error('network error'));

      const entry = await resolveEntry();
      expect(entry).toBe(DEFAULT_ENTRY);
    });

    it('test_background_refresh_on_cache_hit', async () => {
      mockLocalStorage.getItem.mockReturnValue('https://cached.example.com');
      mockFetch.mockResolvedValue({
        ok: false,
        text: () => Promise.resolve(''),
      });

      await resolveEntry();

      // Even though we got cache hit, fetch should still be called in background
      // Wait a tick for the background promise to fire
      await new Promise((r) => setTimeout(r, 10));
      expect(mockFetch).toHaveBeenCalled();
    });
  });

  // ── Static analysis tests ───────────────────────────────────────────────

  it('test_cdn_sources_are_github_urls', () => {
    const hasJsdelivrGh = CDN_SOURCES.some(
      (url) =>
        url.includes('jsdelivr.net/gh/kaitu-io/ui-theme'),
    );
    const hasStaticallyGh = CDN_SOURCES.some(
      (url) =>
        url.includes('statically.io/gh/kaitu-io/ui-theme'),
    );
    expect(hasJsdelivrGh).toBe(true);
    expect(hasStaticallyGh).toBe(true);
  });

  it('test_no_atob_in_source', () => {
    expect(sourceCode).not.toContain('atob(');
  });

  it('test_key_is_64_hex', () => {
    expect(DECRYPTION_KEY).toMatch(/^[0-9a-f]{64}$/);
  });

  it('test_default_entry_is_plain_url', () => {
    expect(DEFAULT_ENTRY.startsWith('https://')).toBe(true);
  });
});
