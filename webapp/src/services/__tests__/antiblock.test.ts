import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
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
// JSONP script mock — simulates <script> tag loading setting window.__k2ac
// ---------------------------------------------------------------------------

/**
 * Mock document.head.appendChild to intercept <script> injections.
 * When a script is appended, it simulates JSONP by setting window.__k2ac
 * then firing onload (or onerror).
 */
function setupScriptMock(
  configProvider: (url: string) => { v: number; data: string } | null,
) {
  const origAppendChild = document.head.appendChild.bind(document.head);

  vi.spyOn(document.head, 'appendChild').mockImplementation(
    <T extends Node>(node: T): T => {
      if (node instanceof HTMLScriptElement && node.src) {
        const url = node.src;
        // Simulate async script load
        setTimeout(() => {
          const config = configProvider(url);
          if (config) {
            // eslint-disable-next-line @typescript-eslint/no-explicit-any
    (window as any).__k2ac = config;
            node.onload?.(new Event('load'));
          } else {
            node.onerror?.(new Event('error') as ErrorEvent);
          }
        }, 0);
        return node;
      }
      return origAppendChild(node) as T;
    },
  );
}

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
    const wrongKey =
      'a01b2c3d4e5f60718293a4b5c6d7e8f90a1b2c3d4e5f60718293a4b5c6d7e8ff';
    const result = await decrypt(encrypted, wrongKey);
    expect(result).toBeNull();
  });

  it('test_decrypt_tampered_payload_returns_null', async () => {
    const original = 'https://example.com';
    const encrypted = await encryptForTest(original, TEST_KEY_HEX);
    const raw = Uint8Array.from(atob(encrypted), (c) => c.charCodeAt(0));
    raw[14] = raw[14]! ^ 0xff;
    const tampered = btoa(String.fromCharCode(...raw));
    const result = await decrypt(tampered, TEST_KEY_HEX);
    expect(result).toBeNull();
  });

  // ── resolveEntry integration tests ──────────────────────────────────────

  describe('resolveEntry', () => {
    let mockLocalStorage: {
      getItem: ReturnType<typeof vi.fn>;
      setItem: ReturnType<typeof vi.fn>;
      removeItem: ReturnType<typeof vi.fn>;
    };

    beforeEach(() => {
      mockLocalStorage = {
        getItem: vi.fn(),
        setItem: vi.fn(),
        removeItem: vi.fn(),
      };
      vi.stubGlobal('localStorage', mockLocalStorage);
    });

    afterEach(() => {
      vi.restoreAllMocks();
      delete // eslint-disable-next-line @typescript-eslint/no-explicit-any
    (window as any).__k2ac;
    });

    it('test_cache_hit_skips_fetch', async () => {
      mockLocalStorage.getItem.mockReturnValue('https://cached.example.com');

      const entry = await resolveEntry();
      expect(entry).toBe('https://cached.example.com');
    });

    it('test_all_cdn_fail_returns_default', async () => {
      mockLocalStorage.getItem.mockReturnValue(null);
      // All scripts fail to load
      setupScriptMock(() => null);

      const entry = await resolveEntry();
      expect(entry).toBe(DEFAULT_ENTRY);
    });

    it('test_background_refresh_on_cache_hit', async () => {
      mockLocalStorage.getItem.mockReturnValue('https://cached.example.com');
      const appendSpy = vi.spyOn(document.head, 'appendChild');

      await resolveEntry();

      // Wait a tick for the background refresh to fire
      await new Promise((r) => setTimeout(r, 10));
      // Background refresh should inject a <script> tag
      const scriptCalls = appendSpy.mock.calls.filter(
        ([node]) => node instanceof HTMLScriptElement,
      );
      expect(scriptCalls.length).toBeGreaterThan(0);
    });
  });

  // ── Static analysis tests ───────────────────────────────────────────────

  it('test_cdn_sources_are_github_urls', () => {
    const hasJsdelivrGh = CDN_SOURCES.some((url) =>
      url.includes('jsdelivr.net/gh/kaitu-io/ui-theme'),
    );
    expect(hasJsdelivrGh).toBe(true);
    expect(CDN_SOURCES.length).toBeGreaterThan(0);
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
