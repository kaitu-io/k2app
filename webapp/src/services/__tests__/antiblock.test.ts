import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { readFileSync } from 'node:fs';
import { resolve } from 'node:path';
import {
  resolveEntry,
  resolveEntries,
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
    rawKey,
    'AES-GCM',
    false,
    ['encrypt'],
  );
  const iv = crypto.getRandomValues(new Uint8Array(12));
  const encoded = new TextEncoder().encode(plaintext);
  const ciphertext = await crypto.subtle.encrypt(
    { name: 'AES-GCM', iv },
    key,
    encoded,
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
      mockLocalStorage.getItem.mockReturnValue(
        JSON.stringify({ entries: ['https://cached.example.com'], ts: 1 }),
      );

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
      mockLocalStorage.getItem.mockReturnValue(
        JSON.stringify({ entries: ['https://cached.example.com'], ts: 1 }),
      );
      const appendSpy = vi.spyOn(document.head, 'appendChild');

      await resolveEntry();

      // Wait a tick for the background refresh to fire
      await new Promise((r) => setTimeout(r, 10));
      // Background refresh should inject <script> tags for all CDN sources
      const scriptCalls = appendSpy.mock.calls.filter(
        ([node]) => node instanceof HTMLScriptElement,
      );
      expect(scriptCalls.length).toBeGreaterThan(0);
    });
  });

  // ── Static analysis tests ───────────────────────────────────────────────

  it('test_cdn_sources_have_multiple_mirrors', () => {
    expect(CDN_SOURCES.length).toBeGreaterThanOrEqual(3);
    expect(CDN_SOURCES.some((u) => u.includes('cdn.jsdelivr.net'))).toBe(true);
    expect(CDN_SOURCES.some((u) => u.includes('fastly.jsdelivr.net'))).toBe(true);
    expect(CDN_SOURCES.some((u) => u.includes('gcore.jsdelivr.net'))).toBe(true);
  });

  it('test_cdn_sources_include_cn_reachable_and_diverse_mirrors', () => {
    // 网宿 CDNetworks 官方边缘（CN 友好）
    expect(CDN_SOURCES.some((u) => u.includes('quantil.jsdelivr.net'))).toBe(true);
    // 国内镜像（CN 直达；从海外探测不通属预期）
    expect(CDN_SOURCES.some((u) => u.includes('jsd.cdn.zzko.cn'))).toBe(true);
    // 独立于 jsDelivr 基础设施的 GitHub 代理（故障域隔离）
    expect(CDN_SOURCES.some((u) => u.includes('cdn.statically.io'))).toBe(true);
    // 所有源必须是 jsDelivr 兼容的 /gh/ 路径且以 ui.js 结尾（seedUrls 依赖此形状）
    for (const u of CDN_SOURCES) {
      expect(u).toMatch(/^https:\/\/[^/]+\/gh\/kaitu-io\/ui-theme@dist\/ui\.js$/);
    }
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

// ---------------------------------------------------------------------------
// ts freshness marker + single atomic record + multi-entry resolution
// ---------------------------------------------------------------------------

describe('antiblock — ts freshness + single-record store + resolveEntries', () => {
  let store: Record<string, string>;

  async function makeConfig(
    entries: string[],
    ts: number | undefined,
  ): Promise<{ v: number; data: string }> {
    const payload: Record<string, unknown> = { entries };
    if (ts !== undefined) payload.ts = ts;
    return { v: 1, data: await encryptForTest(JSON.stringify(payload), DECRYPTION_KEY) };
  }

  beforeEach(() => {
    store = {};
    vi.stubGlobal('localStorage', {
      getItem: (k: string) => (k in store ? store[k]! : null),
      setItem: (k: string, v: string) => { store[k] = String(v); },
      removeItem: (k: string) => { delete store[k]; },
      clear: () => { store = {}; },
    });
  });

  afterEach(async () => {
    // 排空上个 test 遗留的后台镜像回调（setTimeout），让它们在本 test 的 mock+store
    // 仍生效时落地，避免泄漏到下个 test 污染共享的 window.__k2ac。
    await new Promise((r) => setTimeout(r, 15));
    vi.restoreAllMocks();
    // eslint-disable-next-line @typescript-eslint/no-explicit-any
    delete (window as any).__k2ac;
  });

  it('test_picks_highest_ts_across_mirrors', async () => {
    // 快镜像陈旧 (ts=100)、慢镜像新鲜 (ts=200)。首个返回可能是旧的，
    // 但后台升级后记录必须收敛到 ts=200 的 entries。
    const fastHost = new URL(CDN_SOURCES[0]!).host;
    const cfgOld = await makeConfig(['https://old.example'], 100);
    const cfgNew = await makeConfig(['https://new.example'], 200);
    vi.spyOn(document.head, 'appendChild').mockImplementation(
      // eslint-disable-next-line @typescript-eslint/no-explicit-any
      (<T extends Node>(node: T): T => {
        if (node instanceof HTMLScriptElement && node.src) {
          const isFast = node.src.includes(fastHost);
          setTimeout(() => {
            // eslint-disable-next-line @typescript-eslint/no-explicit-any
            (window as any).__k2ac = isFast ? cfgOld : cfgNew;
            node.onload?.(new Event('load'));
          }, isFast ? 0 : 5);
        }
        return node;
      }) as typeof document.head.appendChild,
    );
    await resolveEntries();
    await new Promise((r) => setTimeout(r, 30)); // let background upgrade land
    const rec = JSON.parse(store['k2_entry_cfg']!);
    expect(rec.ts).toBe(200);
    expect(rec.entries).toEqual(['https://new.example']);
  });

  it('test_ts_guard_rejects_stale_background_write', async () => {
    store['k2_entry_cfg'] = JSON.stringify({ entries: ['https://fresh'], ts: 500 });
    const stale = await makeConfig(['https://stale'], 100);
    setupScriptMock(() => stale);
    const entries = await resolveEntries();      // cache hit → returns fresh + bg refresh
    expect(entries).toEqual(['https://fresh']);
    await new Promise((r) => setTimeout(r, 20));
    const rec = JSON.parse(store['k2_entry_cfg']!);
    expect(rec.ts).toBe(500);                    // stale ts=100 must NOT overwrite
    expect(rec.entries).toEqual(['https://fresh']);
  });

  it('test_missing_ts_treated_as_zero_but_entries_valid', async () => {
    const noTs = await makeConfig(['https://noTs'], undefined);
    setupScriptMock(() => noTs);
    const entries = await resolveEntries();
    expect(entries).toEqual(['https://noTs']);
    expect(JSON.parse(store['k2_entry_cfg']!).ts).toBe(0);
  });

  it('test_resolveEntries_returns_full_list_resolveEntry_returns_first', async () => {
    const cfg = await makeConfig(['https://a', 'https://b', 'https://c'], 300);
    setupScriptMock(() => cfg);
    const list = await resolveEntries();
    expect(list).toEqual(['https://a', 'https://b', 'https://c']);
    expect(await resolveEntry()).toBe('https://a'); // cache hit → record[0]
  });

  it('test_no_legacy_migration_from_k2_entry_url', async () => {
    store['k2_entry_url'] = 'https://poisoned.cloudfront'; // legacy poisoned key
    setupScriptMock(() => null);                           // all CDN fail
    const entries = await resolveEntries();
    expect(entries).toEqual([DEFAULT_ENTRY]);              // NOT the legacy value
  });

  it('test_cache_hit_returns_record_entries', async () => {
    store['k2_entry_cfg'] = JSON.stringify({ entries: ['https://c1', 'https://c2'], ts: 42 });
    const entries = await resolveEntries();
    expect(entries).toEqual(['https://c1', 'https://c2']);
  });

  it('test_all_cdn_fail_returns_default_list', async () => {
    setupScriptMock(() => null);
    expect(await resolveEntries()).toEqual([DEFAULT_ENTRY]);
  });
});
