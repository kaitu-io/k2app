import { describe, it, expect } from 'vitest';
import { pickAutoTunnel } from '../auto-tunnel-pick';
import type { Tunnel } from '../../services/api-types';

function tunnel(id: number, domain: string, recommendScore: number, serverUrl?: string): Tunnel {
  return {
    id,
    domain,
    name: domain,
    protocol: 'k2v5',
    port: 443,
    serverUrl: serverUrl ?? `k2v5://${domain}`,
    node: { name: '', country: '', region: '', ipv4: '', ipv6: '', load: 0, trafficUsagePercent: 0, bandwidthUsagePercent: 0 },
    recommendScore,
  };
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
    expect(pickAutoTunnel(t, () => 0.999)?.domain).toBe('a');
  });

  it('picks index 0 of top-5 when rng=0', () => {
    const t = [
      tunnel(1, 'a', 0.9),
      tunnel(2, 'b', 0.8),
      tunnel(3, 'c', 0.7),
      tunnel(4, 'd', 0.6),
      tunnel(5, 'e', 0.5),
    ];
    expect(pickAutoTunnel(t, () => 0)?.domain).toBe('a');
  });

  it('picks middle of top-5 when rng=0.5', () => {
    const t = [
      tunnel(1, 'a', 0.9),
      tunnel(2, 'b', 0.8),
      tunnel(3, 'c', 0.7),
      tunnel(4, 'd', 0.6),
      tunnel(5, 'e', 0.5),
    ];
    // floor(0.5 * 5) = 2 → 'c'
    expect(pickAutoTunnel(t, () => 0.5)?.domain).toBe('c');
  });

  it('picks last of top-5 when rng=0.99', () => {
    const t = [
      tunnel(1, 'a', 0.9),
      tunnel(2, 'b', 0.8),
      tunnel(3, 'c', 0.7),
      tunnel(4, 'd', 0.6),
      tunnel(5, 'e', 0.5),
    ];
    // floor(0.99 * 5) = 4 → 'e'
    expect(pickAutoTunnel(t, () => 0.99)?.domain).toBe('e');
  });

  it('never picks the 6th tunnel (top-5 boundary)', () => {
    const t = [
      tunnel(1, 'a', 0.9),
      tunnel(2, 'b', 0.8),
      tunnel(3, 'c', 0.7),
      tunnel(4, 'd', 0.6),
      tunnel(5, 'e', 0.5),
      tunnel(6, 'f', 0.4), // outside top-5
    ];
    // Even with rng pushed to its highest non-1 value, 'f' must never be picked.
    for (const r of [0, 0.2, 0.5, 0.8, 0.99]) {
      const picked = pickAutoTunnel(t, () => r);
      expect(picked?.domain).not.toBe('f');
    }
  });

  it('treats 0.5 entries as ordinary scores (no special handling)', () => {
    const t = Array.from({ length: 10 }, (_, i) => tunnel(i + 1, `t${i}`, 0.5));
    // Top-5 = first 5 in stable input order; rng indexes within that window.
    expect(pickAutoTunnel(t, () => 0)?.domain).toBe('t0');
    expect(pickAutoTunnel(t, () => 0.5)?.domain).toBe('t2'); // floor(0.5 * 5) = 2
    expect(pickAutoTunnel(t, () => 0.99)?.domain).toBe('t4'); // floor(0.99 * 5) = 4
  });

  it('excludes tunnels with score=0 from primary pool', () => {
    const t = [
      tunnel(1, 'zero1', 0),
      tunnel(2, 'zero2', 0),
      tunnel(3, 'good', 0.5),
    ];
    expect(pickAutoTunnel(t, () => 0.5)?.domain).toBe('good');
  });

  it('returns null when every score is 0 (hard blacklist — no fallback)', () => {
    const t = [
      tunnel(1, 'a', 0),
      tunnel(2, 'b', 0),
      tunnel(3, 'c', 0),
    ];
    expect(pickAutoTunnel(t, () => 0)).toBeNull();
  });

  it('excludes tunnels missing serverUrl', () => {
    const t = [
      tunnel(1, 'no-url', 0.9, ''),
      tunnel(2, 'good', 0.5),
    ];
    expect(pickAutoTunnel(t, () => 0)?.domain).toBe('good');
  });
});
