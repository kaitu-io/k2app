/**
 * subs-resolver tests.
 *
 * Pick invariants are deterministic single-shot tests.
 * Distribution tests use a seeded mulberry32 PRNG so they're repeatable.
 *
 * Run: cd webapp && npx vitest run src/services/__tests__/subs-resolver.test.ts
 */

import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest';

import { resolveTunnel, pickWeighted, __test__ } from '../subs-resolver';

const { parseSubsUrl } = __test__;

// Seedable PRNG so distribution tests are reproducible.
function mulberry32(seed: number): () => number {
  let t = seed >>> 0;
  return () => {
    t = (t + 0x6D2B79F5) >>> 0;
    let r = Math.imul(t ^ (t >>> 15), 1 | t);
    r = (r + Math.imul(r ^ (r >>> 7), 61 | r)) ^ r;
    return ((r ^ (r >>> 14)) >>> 0) / 4294967296;
  };
}

const mockStorage = {
  get: vi.fn(),
  set: vi.fn(),
  remove: vi.fn(),
  has: vi.fn(),
  clear: vi.fn(),
  keys: vi.fn(),
};

const mockFetch = vi.fn();

beforeEach(() => {
  vi.useFakeTimers();
  vi.setSystemTime(new Date('2026-04-15T12:00:00Z'));

  (window as any)._platform = {
    os: 'ios',
    platformType: 'mobile',
    version: '0.4.0',
    storage: mockStorage,
  };
  globalThis.fetch = mockFetch as any;

  mockStorage.get.mockReset().mockResolvedValue(null);
  mockStorage.set.mockReset().mockResolvedValue(undefined);
  mockStorage.remove.mockReset().mockResolvedValue(undefined);
  mockFetch.mockReset();
});

afterEach(() => {
  vi.useRealTimers();
  delete (window as any)._platform;
  vi.restoreAllMocks();
});

// ============== parseSubsUrl ==============

describe('parseSubsUrl', () => {
  it('extracts endpoint, basicAuth, cacheKey from k2subs URL', () => {
    const out = parseSubsUrl('k2subs://udid:tok@k2.52j.me/api/subs?country=jp');
    expect(out.endpoint).toBe('https://k2.52j.me/api/subs?country=jp');
    expect(out.basicAuth).toBe('Basic ' + btoa('udid:tok'));
    expect(out.cacheKey).toMatch(/^k2subs\.cache\.[0-9a-f]{8}$/);
  });

  it('strips client-side refresh param from upstream qs', () => {
    const out = parseSubsUrl('k2subs://u:p@host/api/subs?country=jp&refresh=120');
    expect(out.endpoint).toBe('https://host/api/subs?country=jp');
  });

  it('handles URL with no query', () => {
    const out = parseSubsUrl('k2subs://u:p@host/api/subs');
    expect(out.endpoint).toBe('https://host/api/subs');
  });

  it('rejects non-k2subs scheme', () => {
    expect(() => parseSubsUrl('https://u:p@host/x')).toThrow(/scheme/);
  });

  it('rejects missing credentials', () => {
    expect(() => parseSubsUrl('k2subs://host/x')).toThrow(/credentials/);
  });

  it('produces stable cacheKey for same URL, different for different country', () => {
    const a = parseSubsUrl('k2subs://u:p@host/api/subs?country=jp');
    const b = parseSubsUrl('k2subs://u:p@host/api/subs?country=jp');
    const c = parseSubsUrl('k2subs://u:p@host/api/subs?country=us');
    expect(a.cacheKey).toBe(b.cacheKey);
    expect(a.cacheKey).not.toBe(c.cacheKey);
  });
});

// ============== pickWeighted invariants ==============

describe('pickWeighted', () => {
  it('throws on empty candidates', () => {
    expect(() => pickWeighted([])).toThrow();
  });

  it('weight=0 fallback: when all weights are 0, picks uniformly', () => {
    const cands = [
      { url: 'A', weight: 0 },
      { url: 'B', weight: 0 },
      { url: 'C', weight: 0 },
    ];
    const seen = new Set<string>();
    const rng = mulberry32(42);
    for (let i = 0; i < 200; i++) seen.add(pickWeighted(cands, rng).url);
    expect(seen).toEqual(new Set(['A', 'B', 'C']));
  });

  it('weight>0 candidates dominate weight=0', () => {
    const cands = [
      { url: 'A', weight: 10 },
      { url: 'Z', weight: 0 },
    ];
    const rng = mulberry32(7);
    for (let i = 0; i < 50; i++) {
      expect(pickWeighted(cands, rng).url).toBe('A');
    }
  });

  it('distribution matches weights within ±10%', () => {
    const cands = [
      { url: 'A', weight: 3 },
      { url: 'B', weight: 1 },
    ];
    const rng = mulberry32(1234);
    let aCount = 0;
    const N = 4000;
    for (let i = 0; i < N; i++) {
      if (pickWeighted(cands, rng).url === 'A') aCount++;
    }
    const ratio = aCount / N;
    expect(ratio).toBeGreaterThan(0.65); // expected 0.75, ±10%
    expect(ratio).toBeLessThan(0.85);
  });
});

// ============== resolveTunnel: fresh fetch ==============

describe('resolveTunnel — fresh fetch', () => {
  it('fetches /api/subs and writes cache when no cache exists', async () => {
    mockFetch.mockResolvedValueOnce({
      ok: true,
      status: 200,
      json: async () => ({
        tunnels: [
          { url: 'k2v5://u:p@a/x', weight: 1 },
          { url: 'k2v5://u:p@b/x', weight: 1 },
        ],
        refresh: 1800,
      }),
    });

    const r = await resolveTunnel('k2subs://u:p@host/api/subs?country=jp');

    expect(r.source).toBe('fresh');
    expect(r.allCandidates.length).toBe(2);
    expect(['k2v5://u:p@a/x', 'k2v5://u:p@b/x']).toContain(r.url);
    expect(mockStorage.set).toHaveBeenCalledTimes(1);
    const [, value] = mockStorage.set.mock.calls[0];
    expect(value.tunnels.length).toBe(2);
    expect(value.refresh).toBe(1800);
  });

  it('forwards Authorization Basic header to upstream', async () => {
    mockFetch.mockResolvedValueOnce({
      ok: true,
      status: 200,
      json: async () => ({ tunnels: [{ url: 'k2v5://x', weight: 1 }], refresh: 1800 }),
    });

    await resolveTunnel('k2subs://myudid:mytoken@host/api/subs');

    expect(mockFetch).toHaveBeenCalledWith(
      'https://host/api/subs',
      expect.objectContaining({
        headers: { Authorization: 'Basic ' + btoa('myudid:mytoken') },
      }),
    );
  });

  it('throws when /api/subs returns empty tunnel list', async () => {
    mockFetch.mockResolvedValueOnce({
      ok: true,
      status: 200,
      json: async () => ({ tunnels: [], refresh: 1800 }),
    });

    await expect(resolveTunnel('k2subs://u:p@host/api/subs')).rejects.toThrow(/empty/);
  });
});

// ============== resolveTunnel: cache behavior ==============

describe('resolveTunnel — cache', () => {
  it('cache hit within TTL skips fetch', async () => {
    mockStorage.get.mockResolvedValueOnce({
      tunnels: [{ url: 'k2v5://cached/x', weight: 1 }],
      refresh: 1800,
      fetchedAt: Date.now() - 60_000, // 1min ago
    });

    const r = await resolveTunnel('k2subs://u:p@host/api/subs');

    expect(r.source).toBe('cache');
    expect(r.url).toBe('k2v5://cached/x');
    expect(mockFetch).not.toHaveBeenCalled();
  });

  it('cache miss when older than refresh seconds', async () => {
    mockStorage.get.mockResolvedValueOnce({
      tunnels: [{ url: 'k2v5://stale/x', weight: 1 }],
      refresh: 1800,
      fetchedAt: Date.now() - 60 * 60 * 1000, // 1h ago, refresh=1800s
    });
    mockFetch.mockResolvedValueOnce({
      ok: true,
      status: 200,
      json: async () => ({ tunnels: [{ url: 'k2v5://fresh/x', weight: 1 }], refresh: 1800 }),
    });

    const r = await resolveTunnel('k2subs://u:p@host/api/subs');

    expect(r.source).toBe('fresh');
    expect(r.url).toBe('k2v5://fresh/x');
    expect(mockFetch).toHaveBeenCalledTimes(1);
  });

  it('fetch failure with stale cache <24h: returns stale cache', async () => {
    mockStorage.get.mockResolvedValueOnce({
      tunnels: [{ url: 'k2v5://stale/x', weight: 1 }],
      refresh: 1800,
      fetchedAt: Date.now() - 6 * 60 * 60 * 1000, // 6h
    });
    mockFetch.mockRejectedValueOnce(new Error('network down'));

    const r = await resolveTunnel('k2subs://u:p@host/api/subs');

    expect(r.source).toBe('cache');
    expect(r.url).toBe('k2v5://stale/x');
  });

  it('fetch failure with cache >24h: throws and removes cache', async () => {
    mockStorage.get.mockResolvedValueOnce({
      tunnels: [{ url: 'k2v5://ancient/x', weight: 1 }],
      refresh: 1800,
      fetchedAt: Date.now() - 25 * 60 * 60 * 1000, // 25h
    });
    mockFetch.mockRejectedValueOnce(new Error('network down'));

    await expect(resolveTunnel('k2subs://u:p@host/api/subs')).rejects.toThrow();
    expect(mockStorage.remove).toHaveBeenCalledTimes(1);
  });

  it('fetch failure with no cache: throws', async () => {
    mockFetch.mockRejectedValueOnce(new Error('connection refused'));
    await expect(resolveTunnel('k2subs://u:p@host/api/subs')).rejects.toThrow(/refused/);
  });
});

// ============== resolveTunnel: exclude ==============

describe('resolveTunnel — exclude', () => {
  it('exclude list filters candidates before pick', async () => {
    mockFetch.mockResolvedValueOnce({
      ok: true,
      status: 200,
      json: async () => ({
        tunnels: [
          { url: 'k2v5://A', weight: 1 },
          { url: 'k2v5://B', weight: 1 },
          { url: 'k2v5://C', weight: 1 },
        ],
        refresh: 1800,
      }),
    });

    const r = await resolveTunnel('k2subs://u:p@host/api/subs', ['k2v5://A', 'k2v5://B']);

    expect(r.url).toBe('k2v5://C');
    // allCandidates is the full set (caller may want it for further retry rounds)
    expect(r.allCandidates.length).toBe(3);
  });

  it('throws when all tunnels excluded', async () => {
    mockFetch.mockResolvedValueOnce({
      ok: true,
      status: 200,
      json: async () => ({ tunnels: [{ url: 'k2v5://A', weight: 1 }], refresh: 1800 }),
    });

    await expect(
      resolveTunnel('k2subs://u:p@host/api/subs', ['k2v5://A']),
    ).rejects.toThrow(/excluded/);
  });
});

// ============== HTTP error handling ==============

describe('resolveTunnel — HTTP errors', () => {
  it('non-2xx with body propagates as error', async () => {
    mockFetch.mockResolvedValueOnce({
      ok: false,
      status: 401,
      text: async () => 'invalid credentials',
    });

    await expect(resolveTunnel('k2subs://u:p@host/api/subs')).rejects.toThrow(/401/);
  });

  it('non-2xx falls back to fresh cache when present', async () => {
    mockStorage.get.mockResolvedValueOnce({
      tunnels: [{ url: 'k2v5://cached/x', weight: 1 }],
      refresh: 1800,
      fetchedAt: Date.now() - 2 * 60 * 60 * 1000, // 2h, beyond refresh but within stale fallback
    });
    mockFetch.mockResolvedValueOnce({
      ok: false,
      status: 502,
      text: async () => 'bad gateway',
    });

    const r = await resolveTunnel('k2subs://u:p@host/api/subs');
    expect(r.source).toBe('cache');
    expect(r.url).toBe('k2v5://cached/x');
  });
});
