import { describe, it, expect } from 'vitest';
import { pickAutoTunnel } from '../auto-tunnel-pick';
import type { Tunnel } from '../../services/api-types';

function tunnel(id: number, domain: string, recommendScore: number, serverUrl?: string, country = ''): Tunnel {
  return {
    id,
    domain,
    name: domain,
    protocol: 'k2v5',
    port: 443,
    serverUrl: serverUrl ?? `k2v5://${domain}`,
    node: { name: '', country, region: '', ipv4: '', ipv6: '', load: 0, trafficUsagePercent: 0, bandwidthUsagePercent: 0 },
    recommendScore,
  };
}

/** mulberry32 seeded PRNG */
function prng(seed: number): () => number {
  let a = seed >>> 0;
  return () => {
    a = (a + 0x6D2B79F5) >>> 0;
    let t = a;
    t = Math.imul(t ^ (t >>> 15), t | 1);
    t ^= t + Math.imul(t ^ (t >>> 7), t | 61);
    return ((t ^ (t >>> 14)) >>> 0) / 4294967296;
  };
}

/** rng returning the given values in order, then repeating the last. */
function seq(...v: number[]): () => number {
  let i = 0;
  return () => v[Math.min(i++, v.length - 1)];
}

/** n tunnels with strictly descending scores: `r0` is rank 1, `r<n-1>` is rank n. */
const distinct = (n: number): Tunnel[] =>
  Array.from({ length: n }, (_, i) => tunnel(i + 1, `r${i}`, 1 - i / (n + 1)));

/** Reverses the array: the worst input order for anything relying on API sorting. */
const reversed = <T,>(a: T[]): T[] => [...a].reverse();

function draw(tunnels: Tunnel[], draws: number, seed: number): Map<string, number> {
  const rng = prng(seed);
  const counts = new Map<string, number>();
  for (let k = 0; k < draws; k++) {
    const d = pickAutoTunnel(tunnels, [], rng)!.domain;
    counts.set(d, (counts.get(d) ?? 0) + 1);
  }
  return counts;
}

/** Integer ceil(4n/5), with no floating point involved. */
const keptCount = (n: number): number => (4 * n + 4 - ((4 * n + 4) % 5)) / 5;

/** P(rank r of m wins) under power of two choices with distinct scores; r is 1-based. */
const p2c = (r: number, m: number): number => (2 * (m - r)) / (m * (m - 1));

/** Five standard deviations of a binomial count: wide enough for a fixed seed, narrow enough to bite. */
const fiveSigma = (draws: number, p: number): number => 5 * Math.sqrt(draws * p * (1 - p));

describe('pickAutoTunnel eligibility', () => {
  it('returns null for empty input', () => {
    expect(pickAutoTunnel([])).toBeNull();
  });

  it('returns the only candidate regardless of rng', () => {
    for (const r of [0, 0.5, 0.9999999999]) {
      expect(pickAutoTunnel([tunnel(1, 'a', 0.7)], [], () => r)?.domain).toBe('a');
    }
  });

  it('score 0 is a hard blacklist with no fallback', () => {
    const zero = [tunnel(1, 'a', 0), tunnel(2, 'b', 0)];
    expect(pickAutoTunnel(zero, [], prng(1))).toBeNull();
    for (let s = 0; s < 200; s++) {
      expect(pickAutoTunnel([...zero, tunnel(3, 'good', 0.5)], [], prng(s))?.domain).toBe('good');
    }
  });

  it('missing serverUrl is excluded with no fallback', () => {
    expect(pickAutoTunnel([tunnel(1, 'a', 0.9, '')], [], prng(1))).toBeNull();
    for (let s = 0; s < 200; s++) {
      const t = [tunnel(1, 'no-url', 0.9, ''), tunnel(2, 'good', 0.5)];
      expect(pickAutoTunnel(t, [], prng(s))?.domain).toBe('good');
    }
  });

  it('excluded countries are removed, any case, with no fallback', () => {
    const t = [tunnel(1, 'hk1', 0.9, undefined, 'HK'), tunnel(2, 'jp1', 0.8, undefined, 'JP')];
    for (let s = 0; s < 200; s++) {
      expect(pickAutoTunnel(t, ['hk'], prng(s))?.domain).toBe('jp1');
      expect(pickAutoTunnel(t, ['Hk'], prng(s))?.domain).toBe('jp1');
    }
    const lower = [tunnel(1, 'hk1', 0.9, undefined, 'hk'), tunnel(2, 'jp1', 0.8, undefined, 'jp')];
    expect(pickAutoTunnel(lower, ['HK'], () => 0)?.domain).toBe('jp1');
    expect(pickAutoTunnel(t, ['hk', 'jp'], prng(1))).toBeNull();
  });

  it('an unknown excluded country and a missing node change nothing', () => {
    expect(pickAutoTunnel([tunnel(1, 'a', 0.9, undefined, 'JP')], ['xx'], prng(1))?.domain).toBe('a');
    const noNode = { ...tunnel(1, 'a', 0.9), node: undefined } as unknown as Tunnel;
    expect(pickAutoTunnel([noNode], ['jp'], prng(1))?.domain).toBe('a');
  });

  it('filtering happens before the cut, so excluded nodes do not use up the top 80%', () => {
    // Without the bad nodes, 2 candidates remain and the better one always wins.
    const t = [
      tunnel(1, 'zero', 0),
      tunnel(2, 'no-url', 0.99, ''),
      tunnel(3, 'hk', 0.98, undefined, 'HK'),
      tunnel(4, 'good', 0.5),
      tunnel(5, 'ok', 0.4),
    ];
    for (let s = 0; s < 200; s++) expect(pickAutoTunnel(t, ['hk'], prng(s))?.domain).toBe('good');
  });

  it('does not mutate the input array', () => {
    const t = reversed(distinct(10));
    const before = [...t];
    pickAutoTunnel(t, [], prng(1));
    expect(t).toEqual(before);
  });
});

describe('pickAutoTunnel top 80% cut', () => {
  it.each([
    // n, kept m, ranks ever returned. The lowest kept rank can never win a comparison.
    [1, 1, 1],
    [2, 2, 1],
    [4, 4, 3],
    [5, 4, 3],
    [6, 5, 4],
    [10, 8, 7],
    [41, 33, 32],
  ])('n=%i keeps %i and returns exactly the top %i, never the lowest floor(n/5)', (n, m, returned) => {
    expect(keptCount(n)).toBe(m);
    expect(n - m).toBe(Math.floor(n / 5));
    for (const input of [distinct(n), reversed(distinct(n))]) {
      const counts = draw(input, 20000, n);
      for (let i = 0; i < n; i++) {
        const c = counts.get(`r${i}`) ?? 0;
        if (i < returned) expect(c, `rank ${i + 1} of ${n}`).toBeGreaterThan(0);
        else expect(c, `rank ${i + 1} of ${n}`).toBe(0);
      }
    }
  });

  it('kept size is integer ceil(4n/5) for every n in 1..200', () => {
    for (let n = 1; n <= 200; n++) {
      // A constant rng just below 1 skips every swap and draws the two lowest kept
      // nodes, so the winner is rank m-1: the result reveals m for each n.
      const picked = pickAutoTunnel(distinct(n), [], () => 0.9999999999)!;
      const m = keptCount(n);
      expect(picked.domain, `n=${n}`).toBe(`r${Math.max(m - 2, 0)}`);
    }
  });
});

describe('pickAutoTunnel ties across the cut', () => {
  // n=10 keeps 8. Two clear leaders, then eight nodes tied for the remaining six slots.
  const tied = (): Tunnel[] => [
    tunnel(1, 'lead1', 0.9),
    tunnel(2, 'lead2', 0.8),
    ...Array.from({ length: 8 }, (_, i) => tunnel(i + 3, `tie${i}`, 0.5)),
  ];
  const N = 100000;

  it('every tied node is reachable, whatever the input order', () => {
    for (const input of [tied(), reversed(tied())]) {
      const counts = draw(input, N, 2024);
      for (let i = 0; i < 8; i++) expect(counts.get(`tie${i}`) ?? 0).toBeGreaterThan(0);
    }
  });

  it('the tied nodes share equally: no position in the API response is favoured', () => {
    const counts = draw(tied(), N, 7);
    const shares = Array.from({ length: 8 }, (_, i) => counts.get(`tie${i}`) ?? 0);
    const mean = shares.reduce((a, b) => a + b, 0) / shares.length;
    for (const c of shares) expect(Math.abs(c - mean)).toBeLessThanOrEqual(fiveSigma(N, mean / N));
  });

  it('the distribution does not depend on input order', () => {
    const base = tied();
    const permuted = [...base].sort((a, b) => (a.id * 7) % 11 - (b.id * 7) % 11);
    const reference = draw(base, N, 11);
    for (const [label, input] of [['reversed', reversed(base)], ['permuted', permuted]] as const) {
      const other = draw(input, N, 13);
      for (const t of base) {
        const a = reference.get(t.domain) ?? 0;
        const b = other.get(t.domain) ?? 0;
        // Two independent counts: the difference has sqrt(2) times one count's deviation.
        const tolerance = Math.SQRT2 * fiveSigma(N, Math.max(a, 1) / N);
        expect(Math.abs(a - b), `${label} ${t.domain}`).toBeLessThanOrEqual(tolerance);
      }
    }
  });
});

describe('pickAutoTunnel power of two choices', () => {
  it('two candidates always resolve to the higher score, for any rng', () => {
    const t = [tunnel(1, 'low', 0.5), tunnel(2, 'high', 0.9)];
    for (const r of [0, 0.25, 0.5, 0.75, 0.9999999999]) {
      expect(pickAutoTunnel(t, [], () => r)?.domain).toBe('high');
    }
    for (let s = 0; s < 500; s++) expect(pickAutoTunnel(t, [], prng(s))?.domain).toBe('high');
  });

  it('tie keeps the first drawn', () => {
    const t = [tunnel(1, 'a', 0.5), tunnel(2, 'b', 0.5)];
    // First value is the single shuffle swap (0.99 keeps a,b); the next two are the draws.
    expect(pickAutoTunnel(t, [], seq(0.99, 0, 0))?.domain).toBe('a');
    expect(pickAutoTunnel(t, [], seq(0.99, 0.99, 0))?.domain).toBe('b');
  });

  it('survives rng extremes without looping or returning a filtered node', () => {
    for (const n of [1, 2, 3, 5, 6, 10, 41]) {
      const allowed = new Set(distinct(n).slice(0, keptCount(n)).map(t => t.domain));
      for (const r of [0, 0.9999999999, 1, -1, NaN]) {
        const picked = pickAutoTunnel(distinct(n), [], () => r);
        expect(picked, `n=${n} rng=${r}`).not.toBeNull();
        expect(allowed.has(picked!.domain), `n=${n} rng=${r}`).toBe(true);
      }
    }
  });

  it('extreme rng values with tied scores also return a real candidate', () => {
    const t = Array.from({ length: 7 }, (_, i) => tunnel(i + 1, `t${i}`, 0.5));
    for (const r of [0, 0.9999999999, 1, -1, NaN]) {
      expect(t).toContain(pickAutoTunnel(t, [], () => r));
    }
  });
});

describe('pickAutoTunnel distribution', () => {
  const N = 100000;

  it('41 distinct scores follow the P2C formula over the 33 kept nodes', () => {
    const n = 41;
    const m = keptCount(n);
    expect(m).toBe(33);
    const counts = draw(reversed(distinct(n)), N, 12345);

    // Top share: P(rank 1 of m) = 2/m, about 6.06% for m=33, so concentration is bounded.
    const topExpected = p2c(1, m);
    expect(topExpected).toBeCloseTo(2 / 33, 10);
    const topBound = N * topExpected + fiveSigma(N, topExpected);
    expect(topBound / N).toBeLessThan(0.0645);
    expect(counts.get('r0')!).toBeLessThanOrEqual(topBound);

    for (let rank = 1; rank <= n; rank++) {
      const c = counts.get(`r${rank - 1}`) ?? 0;
      if (rank < m) {
        const p = p2c(rank, m);
        expect(c, `rank ${rank}`).toBeGreaterThan(0);
        expect(Math.abs(c - N * p), `rank ${rank}`).toBeLessThanOrEqual(fiveSigma(N, p));
      } else {
        // rank m loses every comparison (formula gives 0); ranks past m were cut.
        expect(p2c(Math.min(rank, m), m)).toBe(0);
        expect(c, `rank ${rank}`).toBe(0);
      }
    }
  });

  it('all-same-score is uniform: the random cut gives every node the same chance', () => {
    // With every score tied, the kept 80% is a random subset and P2C keeps the first
    // drawn, so each of the n nodes wins with probability 1/n, not 1/m.
    const n = 10;
    const input = Array.from({ length: n }, (_, i) => tunnel(i + 1, `t${i}`, 0.7));
    const counts = draw(input, N, 99);
    const p = 1 / n;
    for (const t of input) {
      expect(Math.abs((counts.get(t.domain) ?? 0) - N * p), t.domain).toBeLessThanOrEqual(fiveSigma(N, p));
    }
  });
});
