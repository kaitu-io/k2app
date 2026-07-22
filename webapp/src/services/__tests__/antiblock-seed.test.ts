import { describe, it, expect, vi, beforeEach, afterEach, beforeAll } from 'vitest';

// ---------------------------------------------------------------------------
// Module mocks — entry-pool (spy addNodes) and antiblock-crypto.loadJsonp (keyed
// by URL). decrypt/base64ToBytes stay real so decodeSeed exercises real
// AES-256-GCM with the real DECRYPTION_KEY.
// ---------------------------------------------------------------------------

vi.mock('../entry-pool', () => ({
  addNodes: vi.fn(),
}));

vi.mock('../antiblock-crypto', async (importOriginal) => {
  const actual = await importOriginal<typeof import('../antiblock-crypto')>();
  return { ...actual, loadJsonp: vi.fn() };
});

// 保活种子模块行为测试：kill-switch 默认 false，这里翻成 true。
// 默认关的行为见 antiblock-seed.relay-disabled.test.ts。
vi.mock('../relay-flag', () => ({ RELAY_ENABLED: true }));

import { loadJsonp, type JsonpConfig } from '../antiblock-crypto';
import { DECRYPTION_KEY } from '../antiblock';
import { addNodes } from '../entry-pool';
import { EMBEDDED_SEED } from '../antiblock-seed-embedded';
import { brandConfig } from '../../brands';

// seedUrls()/findFrontier() derive from antiblock.ts CDN_SOURCES, which is now
// brand-derived (Task 7). overleap ships zero CDN mirrors (not behind the
// GFW), so the entire CDN-driven frontier-advance path is a correct no-op —
// tests that exercise it only make sense for a brand with mirrors to probe.
const hasCdnSources = brandConfig.antiblockCdnSources.length > 0;
import {
  SEED_GLOBAL,
  CURSOR_KEY,
  ENTRY_KEY,
  PROBE_AFTER_KEY,
  SEEDED_KEY,
  PROBE_INTERVAL_MS,
  GAP_CONFIRM,
  seedPath,
  seedUrls,
  decodeSeed,
  findFrontier,
  bootstrapAntiblockSeed,
} from '../antiblock-seed';

const mockedLoadJsonp = vi.mocked(loadJsonp);
const mockedAddNodes = vi.mocked(addNodes);

// ---------------------------------------------------------------------------
// Helpers — real AES-256-GCM encryption fixture (mirrors antiblock.test.ts)
// ---------------------------------------------------------------------------

function hexToBytes(hex: string): Uint8Array {
  const bytes = new Uint8Array(hex.length / 2);
  for (let i = 0; i < hex.length; i += 2) {
    bytes[i / 2] = parseInt(hex.substr(i, 2), 16);
  }
  return bytes;
}

async function encryptForTest(plaintext: string, keyHex: string): Promise<string> {
  const rawKey = hexToBytes(keyHex);
  const key = await crypto.subtle.importKey('raw', rawKey, 'AES-GCM', false, ['encrypt']);
  const iv = crypto.getRandomValues(new Uint8Array(12));
  const encoded = new TextEncoder().encode(plaintext);
  const ciphertext = await crypto.subtle.encrypt({ name: 'AES-GCM', iv }, key, encoded);
  const result = new Uint8Array(iv.length + ciphertext.byteLength);
  result.set(iv);
  result.set(new Uint8Array(ciphertext), iv.length);
  return btoa(String.fromCharCode(...result));
}

async function makeSeedConfig(payload: {
  entries: string[];
  nodes: { ip: string; pin: string; ech: string }[];
}): Promise<JsonpConfig> {
  return { v: 1, data: await encryptForTest(JSON.stringify(payload), DECRYPTION_KEY) };
}

/** Parse the cursor N out of a `.../v/<N>.js` seed URL. */
function cursorFromUrl(url: string): number {
  const m = url.match(/\/v\/(\d+)\.js$/);
  return m ? Number(m[1]) : -1;
}

/** A Map-backed fake localStorage with spyable methods. */
function makeLocalStorage(initial: Record<string, string> = {}) {
  const store = new Map<string, string>(Object.entries(initial));
  return {
    store,
    getItem: vi.fn((k: string) => (store.has(k) ? store.get(k)! : null)),
    setItem: vi.fn((k: string, v: string) => {
      store.set(k, String(v));
    }),
    removeItem: vi.fn((k: string) => {
      store.delete(k);
    }),
  };
}

const SAMPLE_PAYLOAD = {
  entries: ['https://entry.example.com'],
  nodes: [{ ip: '1.2.3.4', pin: 'sha256/AAA', ech: 'ZWNo' }],
};

let validConfig: JsonpConfig;

beforeAll(async () => {
  validConfig = await makeSeedConfig(SAMPLE_PAYLOAD);
});

beforeEach(() => {
  vi.clearAllMocks();
});

afterEach(() => {
  vi.restoreAllMocks();
});

// ---------------------------------------------------------------------------
// decodeSeed
// ---------------------------------------------------------------------------

describe('decodeSeed', () => {
  it('decodes a real-key fixture into {entries,nodes}', async () => {
    const cfg = await makeSeedConfig(SAMPLE_PAYLOAD);
    const out = await decodeSeed(cfg);
    expect(out).toEqual(SAMPLE_PAYLOAD);
  });

  it('returns null on null input', async () => {
    expect(await decodeSeed(null)).toBeNull();
  });

  it('returns null when v !== 1', async () => {
    const cfg = await makeSeedConfig(SAMPLE_PAYLOAD);
    expect(await decodeSeed({ v: 2, data: cfg.data })).toBeNull();
  });

  it('returns null on garbage data (decrypt fails)', async () => {
    expect(await decodeSeed({ v: 1, data: 'not-real-ciphertext' })).toBeNull();
  });

  it('returns null on shape mismatch (missing nodes array)', async () => {
    const cfg = await makeSeedConfig({ entries: ['https://x'], nodes: [] });
    // Re-encrypt a payload that has entries but no nodes.
    const badData = await encryptForTest(
      JSON.stringify({ entries: ['https://x'] }),
      DECRYPTION_KEY,
    );
    expect(await decodeSeed({ v: 1, data: badData })).toBeNull();
    // Sanity: the well-formed control still decodes.
    expect(await decodeSeed(cfg)).toEqual({ entries: ['https://x'], nodes: [] });
  });
});

// ---------------------------------------------------------------------------
// seedPath / seedUrls
// ---------------------------------------------------------------------------

describe('constants', () => {
  it('use the spec-mandated values distinct from legacy config.js', () => {
    expect(SEED_GLOBAL).toBe('__k2sd');
    expect(CURSOR_KEY).toBe('k2_seed_cursor');
    expect(ENTRY_KEY).toBe('k2_entry_url');
    expect(PROBE_AFTER_KEY).toBe('k2_seed_probe_after');
    expect(PROBE_INTERVAL_MS).toBe(6 * 60 * 60 * 1000);
    expect(GAP_CONFIRM).toBe(4);
  });
});

describe('seedPath / seedUrls', () => {
  it('seedPath builds v/<n>.js', () => {
    expect(seedPath(42)).toBe('v/42.js');
  });

  it.runIf(hasCdnSources)(
    'seedUrls rewrites every /ui.js mirror to /v/<n>.js',
    () => {
      const urls = seedUrls(7);
      expect(urls.length).toBeGreaterThanOrEqual(3);
      for (const u of urls) {
        expect(u).toMatch(/\/v\/7\.js$/);
        expect(u).not.toContain('/ui.js');
        expect(u).not.toContain('/config.js');
      }
    },
  );
});

// ---------------------------------------------------------------------------
// findFrontier — galloping
// ---------------------------------------------------------------------------

describe('findFrontier', () => {
  /** Mock loadJsonp so that exists(n) is truthy iff `pred(n)`. */
  function mockExistsBy(pred: (n: number) => boolean) {
    mockedLoadJsonp.mockImplementation((url: string) => {
      const n = cursorFromUrl(url);
      return Promise.resolve(pred(n) ? validConfig : null);
    });
  }

  it.runIf(hasCdnSources)(
    'gallops across a FAR frontier (valid n<=350, floor=10) → cursor 350',
    async () => {
      mockExistsBy((n) => n <= 350);
      const res = await findFrontier(10);
      expect(res).not.toBeNull();
      expect(res!.cursor).toBe(350);
      expect(res!.payload).toEqual(SAMPLE_PAYLOAD);
    },
  );

  it.runIf(hasCdnSources)(
    'jumps a single gap (valid n<=200 and n==202, 201 missing, floor=190) → cursor 202',
    async () => {
      mockExistsBy((n) => n <= 200 || n === 202);
      const res = await findFrontier(190);
      expect(res).not.toBeNull();
      expect(res!.cursor).toBe(202);
    },
  );

  it.runIf(hasCdnSources)(
    'bridges a CI gap (valid n=200,202, n=201 missing, floor=199) → cursor 202',
    async () => {
      // Simulates a CDN publish gap: v/200.js exists, v/201.js was never published
      // (CI gap), v/202.js exists, nothing above. Gallop hits 200, misses 201 →
      // binary gives best=200. Gap-confirm must bridge the hole and return 202.
      mockExistsBy((n) => n === 200 || n === 202);
      const res = await findFrontier(199);
      expect(res).not.toBeNull();
      expect(res!.cursor).toBe(202);
    },
  );

  it('returns null when nothing exists above floor', async () => {
    mockExistsBy(() => false);
    const res = await findFrontier(5);
    expect(res).toBeNull();
  });
});

// ---------------------------------------------------------------------------
// bootstrapAntiblockSeed
// ---------------------------------------------------------------------------

describe('bootstrapAntiblockSeed', () => {
  const NOW = 1_000_000_000_000;

  beforeEach(() => {
    vi.spyOn(Date, 'now').mockReturnValue(NOW);
  });

  it.runIf(hasCdnSources)(
    'cold start: seeds EMBEDDED_SEED.nodes, writes ENTRY_KEY, runs network even when throttle is in the future',
    async () => {
      const ls = makeLocalStorage({ [PROBE_AFTER_KEY]: String(NOW + PROBE_INTERVAL_MS) });
      vi.stubGlobal('localStorage', ls);
      mockedLoadJsonp.mockResolvedValue(null); // no CDN frontier

      await bootstrapAntiblockSeed();

      expect(mockedAddNodes).toHaveBeenCalledWith(EMBEDDED_SEED.nodes);
      expect(ls.setItem).toHaveBeenCalledWith(ENTRY_KEY, EMBEDDED_SEED.entries[0]);
      // network ran despite a future throttle (cold-start bypass)
      expect(mockedLoadJsonp).toHaveBeenCalled();
    },
  );

  it('warm + throttle in the future: does NOT call loadJsonp (throttled)', async () => {
    // Warm = already seeded this install (SEEDED_KEY set). Node storage moved to
    // Go, so "warm" is a persisted marker, not a non-empty webapp pool.
    const ls = makeLocalStorage({
      [PROBE_AFTER_KEY]: String(NOW + PROBE_INTERVAL_MS),
      [SEEDED_KEY]: '1',
    });
    vi.stubGlobal('localStorage', ls);

    await bootstrapAntiblockSeed();

    expect(mockedLoadJsonp).not.toHaveBeenCalled();
  });

  it.runIf(hasCdnSources)(
    'warm + throttle expired: runs gallop and pushes PROBE_AFTER_KEY forward',
    async () => {
      const ls = makeLocalStorage({ [PROBE_AFTER_KEY]: String(NOW - 1) });
      vi.stubGlobal('localStorage', ls);
      mockedLoadJsonp.mockResolvedValue(null);

      await bootstrapAntiblockSeed();

      expect(mockedLoadJsonp).toHaveBeenCalled();
      expect(ls.setItem).toHaveBeenCalledWith(
        PROBE_AFTER_KEY,
        String(NOW + PROBE_INTERVAL_MS),
      );
    },
  );

  it.runIf(hasCdnSources)(
    'cold + CDN advance: adds nodes, persists cursor, writes entry',
    async () => {
      const ls = makeLocalStorage(); // SEEDED_KEY unset → cold
      vi.stubGlobal('localStorage', ls);
      // Floor = max(persisted cursor=0, EMBEDDED_SEED.cursor). Probe a frontier just
      // above the real embedded floor so the test is independent of the seed's
      // build-regenerated cursor value.
      const base = EMBEDDED_SEED.cursor;
      mockedLoadJsonp.mockImplementation((url: string) => {
        const n = cursorFromUrl(url);
        return Promise.resolve(n > base && n <= base + 3 ? validConfig : null);
      });

      await bootstrapAntiblockSeed();

      // EMBEDDED nodes first (always), then payload nodes after advance
      expect(mockedAddNodes).toHaveBeenCalledWith(SAMPLE_PAYLOAD.nodes);
      expect(ls.setItem).toHaveBeenCalledWith(CURSOR_KEY, String(base + 3));
      expect(ls.setItem).toHaveBeenCalledWith(ENTRY_KEY, SAMPLE_PAYLOAD.entries[0]);
    },
  );

  it('never throws when loadJsonp returns null everywhere', async () => {
    const ls = makeLocalStorage();
    vi.stubGlobal('localStorage', ls);
    mockedLoadJsonp.mockResolvedValue(null);

    await expect(bootstrapAntiblockSeed()).resolves.toBeUndefined();
    // throttle still pushed forward (always, after a network run)
    expect(ls.setItem).toHaveBeenCalledWith(
      PROBE_AFTER_KEY,
      String(NOW + PROBE_INTERVAL_MS),
    );
  });

  it('never throws when localStorage itself throws', async () => {
    const throwingLs = {
      getItem: vi.fn(() => {
        throw new Error('SSR: no localStorage');
      }),
      setItem: vi.fn(() => {
        throw new Error('SSR: no localStorage');
      }),
      removeItem: vi.fn(),
    };
    vi.stubGlobal('localStorage', throwingLs);
    mockedLoadJsonp.mockResolvedValue(null);

    await expect(bootstrapAntiblockSeed()).resolves.toBeUndefined();
  });
});
