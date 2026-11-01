import { describe, it, expect } from 'vitest';
import { pickAutoTunnel, type TunnelProbe } from '../auto-tunnel-pick';
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

/** rng returning the given values in order, then repeating the last. */
function seq(...v: number[]): () => number {
  let i = 0;
  return () => v[Math.min(i++, v.length - 1)];
}

describe('pickAutoTunnel', () => {
  it('returns null for empty input', () => {
    expect(pickAutoTunnel([])).toBeNull();
  });

  it('returns null when no tunnel has serverUrl', () => {
    const t = [tunnel(1, 'a', 0.9, '')];
    expect(pickAutoTunnel(t)).toBeNull();
  });

  it('returns the only valid tunnel regardless of rng', () => {
    const t = [tunnel(1, 'a', 0.7)];
    expect(pickAutoTunnel(t, [], () => 0.999)?.domain).toBe('a');
  });

  describe('power of two choices', () => {
    it('compares two random candidates and takes the higher score', () => {
      const t = [tunnel(1, 'a', 0.5), tunnel(2, 'b', 0.9), tunnel(3, 'c', 0.1)];
      // first draw idx 0 (a); second draw from the other two: 0.4*2=0 → shifted? idx0 is taken so j=0 → 1 (b)
      expect(pickAutoTunnel(t, [], seq(0, 0.4))?.domain).toBe('b');
      // reversed order of draws still yields the higher score
      expect(pickAutoTunnel(t, [], seq(0.4, 0))?.domain).toBe('b');
    });

    it('tie keeps the first drawn', () => {
      const t = [tunnel(1, 'a', 0.5), tunnel(2, 'b', 0.5), tunnel(3, 'c', 0.5)];
      expect(pickAutoTunnel(t, [], seq(0.4, 0))?.domain).toBe('b'); // first draw idx 1
      expect(pickAutoTunnel(t, [], seq(0, 0))?.domain).toBe('a');
    });

    it('draws two distinct candidates', () => {
      const t = [tunnel(1, 'a', 0.5), tunnel(2, 'b', 0.9)];
      // rng constant 0 must neither loop forever nor compare a node with itself
      expect(pickAutoTunnel(t, [], () => 0)?.domain).toBe('b');
      expect(pickAutoTunnel(t, [], () => 0.999999)?.domain).toBe('b');
    });

    it('can reach nodes outside the top five', () => {
      const t = Array.from({ length: 10 }, (_, i) => tunnel(i + 1, `t${i}`, 0.9 - i * 0.05));
      // All near and probed → gate applies, no top-5 cap.
      // draw t8 (idx 8: 0.85*10=8) then second from 9 others: 0.99*9=8 → idx 8>=8 → 9 (t9)
      const near = () => ({ avgRttMs: 50, reachable: true });
      expect(pickAutoTunnel(t, [], seq(0.85, 0.99), near)?.domain).toBe('t8');
    });

    it('single candidate returns directly', () => {
      const t = [tunnel(1, 'a', 0.5), tunnel(2, 'z', 0)];
      expect(pickAutoTunnel(t, [], () => 0.7)?.domain).toBe('a');
    });
  });

  describe('probe filtering', () => {
    const probes = (m: Record<string, TunnelProbe>) => (t: Tunnel) => m[t.domain] ?? null;

    it('drops confirmed unreachable nodes', () => {
      const t = [tunnel(1, 'a', 0.9), tunnel(2, 'b', 0.5)];
      const p = probes({ a: { avgRttMs: 0, reachable: false } });
      for (const r of [0, 0.5, 0.99]) expect(pickAutoTunnel(t, [], () => r, p)?.domain).toBe('b');
    });

    it('ignores unreachable filter when it would empty the pool', () => {
      const t = [tunnel(1, 'a', 0.9), tunnel(2, 'b', 0.5)];
      const p = probes({
        a: { avgRttMs: 0, reachable: false },
        b: { avgRttMs: 0, reachable: false },
      });
      expect(pickAutoTunnel(t, [], () => 0, p)?.domain).toBe('a');
    });

    it('all candidates fresh-unreachable with zero RTT: gate skipped, still returns one', () => {
      const t = [tunnel(1, 'a', 0.9), tunnel(2, 'b', 0.5), tunnel(3, 'c', 0.4)];
      const dead = { avgRttMs: 0, reachable: false };
      const p = probes({ a: dead, b: dead, c: dead });
      expect(pickAutoTunnel(t, [], () => 0, p)).not.toBeNull();
    });

    it('zero-RTT probes do not count toward the threshold or become best', () => {
      const t = [tunnel(1, 'a', 0.5), tunnel(2, 'far', 0.99), tunnel(3, 'z', 0.1)];
      const p = probes({
        a: { avgRttMs: 10, reachable: true },
        far: { avgRttMs: 900, reachable: true },
        z: { avgRttMs: 0, reachable: true },
      });
      // only 2 real probes -> gate skipped, far stays a candidate
      expect(pickAutoTunnel(t, [], seq(0, 0.99), p)?.domain).toBe('far');
    });

    it('near gate uses 1.5x when best is large (best=100 → limit 150 inclusive)', () => {
      const t = [
        tunnel(1, 'best', 0.1), tunnel(2, 'edge', 0.2),
        tunnel(3, 'out', 0.99), tunnel(4, 'mid', 0.3),
      ];
      const p = probes({
        best: { avgRttMs: 100, reachable: true },
        mid: { avgRttMs: 140, reachable: true },
        edge: { avgRttMs: 150, reachable: true },
        out: { avgRttMs: 151, reachable: true },
      });
      const seen = new Set<string>();
      for (let a = 0; a < 20; a++) for (let b = 0; b < 20; b++) {
        seen.add(pickAutoTunnel(t, [], seq(a / 20, b / 20), p)!.domain);
      }
      expect(seen.has('out')).toBe(false);
      expect(seen.has('edge')).toBe(true);
      expect(seen.has('mid')).toBe(true);
    });

    it('drops fresh high-loss nodes like unreachable', () => {
      const t = [tunnel(1, 'lossy', 0.99), tunnel(2, 'ok', 0.5)];
      const p = probes({ lossy: { avgRttMs: 20, reachable: true, lossRate: 0.26 } });
      for (const r of [0, 0.5, 0.99]) expect(pickAutoTunnel(t, [], () => r, p)?.domain).toBe('ok');
    });

    it('loss of exactly 0.25 is tolerated', () => {
      const t = [tunnel(1, 'edge', 0.99), tunnel(2, 'ok', 0.5)];
      const p = probes({ edge: { avgRttMs: 20, reachable: true, lossRate: 0.25 } });
      expect(pickAutoTunnel(t, [], () => 0, p)?.domain).toBe('edge');
    });

    it('ignores the loss filter when it would empty the pool', () => {
      const t = [tunnel(1, 'a', 0.9), tunnel(2, 'b', 0.5)];
      const lossy = { avgRttMs: 20, reachable: true, lossRate: 0.9 };
      expect(pickAutoTunnel(t, [], () => 0, probes({ a: lossy, b: lossy }))).not.toBeNull();
    });

    it('high-loss probes do not count toward the 3-probed threshold or best RTT', () => {
      const t = [tunnel(1, 'a', 0.5), tunnel(2, 'b', 0.4), tunnel(3, 'far', 0.99), tunnel(4, 'lossy', 0.1)];
      const p = probes({
        a: { avgRttMs: 10, reachable: true },
        b: { avgRttMs: 20, reachable: true },
        far: { avgRttMs: 900, reachable: true },
        lossy: { avgRttMs: 1, reachable: true, lossRate: 0.5 },
      });
      // lossy dropped → 3 probed (a,b,far): gate applies with best=10 → far (900) excluded
      for (const r of [0, 0.3, 0.6, 0.99]) {
        expect(pickAutoTunnel(t, [], () => r, p)?.domain).not.toBe('far');
      }
    });

    it('fallback (gate skipped) keeps the top 80% by score, ceil', () => {
      const t = Array.from({ length: 10 }, (_, i) => tunnel(i + 1, `t${i}`, 0.9 - i * 0.05));
      const allowed = ['t0', 't1', 't2', 't3', 't4', 't5', 't6', 't7']; // ceil(10*0.8)=8
      const seen = new Set<string>();
      for (const r of [0, 0.1, 0.25, 0.4, 0.55, 0.7, 0.85, 0.99]) {
        const d = pickAutoTunnel(t, [], () => r)!.domain;
        expect(allowed).toContain(d);
        seen.add(pickAutoTunnel(t, [], seq(r, 0.99))!.domain);
      }
      // picks are not limited to five nodes
      const beyondFive = [...seen].some(d => !['t0', 't1', 't2', 't3', 't4'].includes(d));
      expect(beyondFive).toBe(true);
      // 2 probed only → still fallback
      const p = probes({ t9: { avgRttMs: 5, reachable: true }, t8: { avgRttMs: 6, reachable: true } });
      for (const r of [0, 0.5, 0.99]) {
        expect(allowed).toContain(pickAutoTunnel(t, [], () => r, p)!.domain);
      }
    });

    it('fallback ceil: 3 candidates keep 3, 2 keep 2, 6 keep 5', () => {
      const mk = (n: number) => Array.from({ length: n }, (_, i) => tunnel(i + 1, `t${i}`, 0.9 - i * 0.05));
      // 3 -> ceil(2.4)=3: lowest (t2) is reachable as the second draw vs t2 tie? use rng to pick t2 first -> loses to higher, so check pool via 2-vs-3
      const reach = (n: number) => {
        const out = new Set<string>();
        for (let a = 0; a < 20; a++) for (let b = 0; b < 20; b++) {
          out.add(pickAutoTunnel(mk(n), [], seq(a / 20, b / 20))!.domain);
        }
        return out;
      };
      // winner is always the higher score of the pair, so the max-reachable index shows the pool size
      expect(Math.max(...[...reach(6)].map(d => +d.slice(1)))).toBe(3); // pool 5 → worst winner is idx 3
      expect(Math.max(...[...reach(2)].map(d => +d.slice(1)))).toBe(0);
    });

    it('keeps only near nodes when 3+ are probed', () => {
      const t = [tunnel(1, 'a', 0.5), tunnel(2, 'b', 0.5), tunnel(3, 'far', 0.99)];
      const p = probes({
        a: { avgRttMs: 100, reachable: true },
        b: { avgRttMs: 120, reachable: true },
        far: { avgRttMs: 400, reachable: true },
      });
      for (const r of [0, 0.3, 0.6, 0.99]) {
        expect(pickAutoTunnel(t, [], () => r, p)?.domain).not.toBe('far');
      }
    });

    it('near gate allows best + 30ms', () => {
      // best=10: limit = max(15, 40) = 40 → 35 stays, 100 goes
      const t = [tunnel(1, 'a', 0.1), tunnel(2, 'b', 0.2), tunnel(3, 'c', 0.99)];
      const p = probes({
        a: { avgRttMs: 10, reachable: true },
        b: { avgRttMs: 35, reachable: true },
        c: { avgRttMs: 100, reachable: true },
      });
      const seen = new Set<string>();
      for (const r of [0, 0.3, 0.6, 0.99]) seen.add(pickAutoTunnel(t, [], () => r, p)!.domain);
      expect(seen.has('c')).toBe(false);
      expect(pickAutoTunnel(t, [], () => 0, p)?.domain).toBe('b');
    });

    it('skips the gate with fewer than 3 probed', () => {
      const t = [tunnel(1, 'a', 0.5), tunnel(2, 'far', 0.99), tunnel(3, 'c', 0.1)];
      const p = probes({
        a: { avgRttMs: 10, reachable: true },
        far: { avgRttMs: 900, reachable: true },
      });
      // far is a candidate: draw far then c → far wins
      expect(pickAutoTunnel(t, [], seq(0, 0.99), p)?.domain).toBe('far');
    });

    it('unprobed nodes are dropped once the gate applies', () => {
      const t = [
        tunnel(1, 'a', 0.2), tunnel(2, 'b', 0.2), tunnel(3, 'c', 0.2),
        tunnel(4, 'unprobed', 0.99),
      ];
      const p = probes({
        a: { avgRttMs: 50, reachable: true },
        b: { avgRttMs: 60, reachable: true },
        c: { avgRttMs: 70, reachable: true },
      });
      for (const r of [0, 0.3, 0.6, 0.99]) {
        expect(pickAutoTunnel(t, [], () => r, p)?.domain).not.toBe('unprobed');
      }
    });
  });

  it('excludes tunnels with score=0 from primary pool', () => {
    const t = [
      tunnel(1, 'zero1', 0),
      tunnel(2, 'zero2', 0),
      tunnel(3, 'good', 0.5),
    ];
    expect(pickAutoTunnel(t, [], () => 0.5)?.domain).toBe('good');
  });

  it('returns null when every score is 0 (hard blacklist — no fallback)', () => {
    const t = [
      tunnel(1, 'a', 0),
      tunnel(2, 'b', 0),
      tunnel(3, 'c', 0),
    ];
    expect(pickAutoTunnel(t, [], () => 0)).toBeNull();
  });

  it('excludes tunnels missing serverUrl', () => {
    const t = [
      tunnel(1, 'no-url', 0.9, ''),
      tunnel(2, 'good', 0.5),
    ];
    expect(pickAutoTunnel(t, [], () => 0)?.domain).toBe('good');
  });

  describe('country exclusion', () => {
    it('excludes tunnels in excluded countries', () => {
      const t = [
        tunnel(1, 'hk1', 0.9, undefined, 'HK'),
        tunnel(2, 'jp1', 0.8, undefined, 'JP'),
      ];
      // rng=0 would pick top-scored 'hk1' — exclusion must remove it.
      expect(pickAutoTunnel(t, ['hk'], () => 0)?.domain).toBe('jp1');
    });

    it('normalizes case: uppercase node.country vs mixed-case exclusion', () => {
      const t = [
        tunnel(1, 'hk1', 0.9, undefined, 'HK'),
        tunnel(2, 'jp1', 0.8, undefined, 'JP'),
      ];
      expect(pickAutoTunnel(t, ['Hk'], () => 0)?.domain).toBe('jp1');
    });

    it('returns null when every candidate is excluded (no fallback)', () => {
      const t = [
        tunnel(1, 'hk1', 0.9, undefined, 'HK'),
        tunnel(2, 'hk2', 0.8, undefined, 'HK'),
      ];
      expect(pickAutoTunnel(t, ['hk'], () => 0)).toBeNull();
    });

    it('empty exclusion list behaves exactly as before', () => {
      const t = [
        tunnel(1, 'a', 0.9),
        tunnel(2, 'b', 0.8),
      ];
      expect(pickAutoTunnel(t, [], () => 0)?.domain).toBe('a');
      expect(pickAutoTunnel(t, undefined, () => 0)?.domain).toBe('a');
    });

    it('exclusion of unknown country is a no-op', () => {
      const t = [tunnel(1, 'a', 0.9, undefined, 'JP')];
      expect(pickAutoTunnel(t, ['xx'], () => 0)?.domain).toBe('a');
    });
  });
});

describe('pickAutoTunnel distribution', () => {
  // mulberry32 seeded PRNG
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
  const regions = ['JP', 'HK', 'SG', 'US', 'DE', 'KR'];
  // 41 nodes: 0..24 near (RTT 40-69, gate limit 70), 25..40 far (RTT 150-300).
  const fixture = Array.from({ length: 41 }, (_, i) =>
    tunnel(i + 1, `n${i}`, 0.3 + (i % 8) * 0.08, undefined, regions[i % regions.length]));
  const rttOf = (i: number) => (i < 25 ? 40 + i * 1.2 : 150 + (i - 25) * 10);
  const probeOf = (t: Tunnel) => ({ avgRttMs: rttOf(t.id - 1), reachable: true, lossRate: 0 });
  const N = 20000;

  it('spreads over near nodes, never far ones, bounded and score-weighted', () => {
    const rng = prng(12345);
    const counts = new Map<string, number>();
    for (let k = 0; k < N; k++) {
      const d = pickAutoTunnel(fixture, [], rng, probeOf)!.domain;
      counts.set(d, (counts.get(d) ?? 0) + 1);
    }
    const near = fixture.slice(0, 25);
    for (const t of near) expect(counts.get(t.domain) ?? 0).toBeGreaterThan(0);
    for (const t of fixture.slice(25)) expect(counts.get(t.domain) ?? 0).toBe(0);
    const uniform = N / near.length;
    for (const t of near) expect(counts.get(t.domain)!).toBeLessThanOrEqual(uniform * 2.5);
    // mean picks per score level rises with score
    const byScore = new Map<number, number[]>();
    for (const t of near) {
      const arr = byScore.get(t.recommendScore) ?? [];
      arr.push(counts.get(t.domain)!);
      byScore.set(t.recommendScore, arr);
    }
    const levels = [...byScore.keys()].sort((a, b) => a - b);
    const means = levels.map(l => byScore.get(l)!.reduce((x, y) => x + y, 0) / byScore.get(l)!.length);
    for (let i = 1; i < means.length; i++) expect(means[i]).toBeGreaterThan(means[i - 1]);
  });

  it('without probes only the top 80% by score are chosen (not limited to five)', () => {
    const rng = prng(777);
    // 41 nodes -> ceil(32.8) = 33 eligible; stable desc order, ties by input order
    const order = fixture.map((t, i) => ({ t, i }))
      .sort((x, y) => y.t.recommendScore - x.t.recommendScore || x.i - y.i)
      .map(x => x.t.domain);
    const eligible = new Set(order.slice(0, 33));
    const dropped = order.slice(33);
    expect(dropped).toHaveLength(8);
    const seen = new Set<string>();
    for (let k = 0; k < N; k++) {
      const d = pickAutoTunnel(fixture, [], rng)!.domain;
      expect(eligible.has(d)).toBe(true);
      seen.add(d);
    }
    for (const d of dropped) expect(seen.has(d)).toBe(false);
    expect(seen.size).toBeGreaterThan(5);
  });
});
