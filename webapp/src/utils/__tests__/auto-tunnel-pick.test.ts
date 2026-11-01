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
      // draw t8 (idx 8: 0.85*10=8) then second from 9 others: 0.99*9=8 → idx 8>=8 → 9 (t9)
      expect(pickAutoTunnel(t, [], seq(0.85, 0.99))?.domain).toBe('t8');
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
      expect(pickAutoTunnel(t, [], seq(0.4, 0.99), p)?.domain).toBe('far');
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
