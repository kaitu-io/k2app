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

  it('picks index 0 of top-3 when rng=0', () => {
    const t = [
      tunnel(1, 'a', 0.9),
      tunnel(2, 'b', 0.8),
      tunnel(3, 'c', 0.7),
      tunnel(4, 'd', 0.5),
      tunnel(5, 'e', 0.5),
    ];
    expect(pickAutoTunnel(t, () => 0)?.domain).toBe('a');
  });

  it('picks index 1 of top-3 when rng=0.5', () => {
    const t = [
      tunnel(1, 'a', 0.9),
      tunnel(2, 'b', 0.8),
      tunnel(3, 'c', 0.7),
    ];
    expect(pickAutoTunnel(t, () => 0.5)?.domain).toBe('b');
  });

  it('picks index 2 of top-3 when rng=0.99', () => {
    const t = [
      tunnel(1, 'a', 0.9),
      tunnel(2, 'b', 0.8),
      tunnel(3, 'c', 0.7),
    ];
    expect(pickAutoTunnel(t, () => 0.99)?.domain).toBe('c');
  });

  it('treats 0.5 entries as ordinary scores (no special handling)', () => {
    const t = Array.from({ length: 10 }, (_, i) => tunnel(i + 1, `t${i}`, 0.5));
    expect(pickAutoTunnel(t, () => 0)?.domain).toBe('t0');
    expect(pickAutoTunnel(t, () => 0.5)?.domain).toBe('t1');
    expect(pickAutoTunnel(t, () => 0.99)?.domain).toBe('t2');
  });

  it('excludes tunnels with score=0 from primary pool', () => {
    const t = [
      tunnel(1, 'zero1', 0),
      tunnel(2, 'zero2', 0),
      tunnel(3, 'good', 0.5),
    ];
    expect(pickAutoTunnel(t, () => 0.5)?.domain).toBe('good');
  });

  it('falls back to all-with-serverUrl when every score is 0', () => {
    const t = [
      tunnel(1, 'a', 0),
      tunnel(2, 'b', 0),
      tunnel(3, 'c', 0),
    ];
    const picked = pickAutoTunnel(t, () => 0);
    expect(picked?.domain).toBe('a');
  });

  it('excludes tunnels missing serverUrl', () => {
    const t = [
      tunnel(1, 'no-url', 0.9, ''),
      tunnel(2, 'good', 0.5),
    ];
    expect(pickAutoTunnel(t, () => 0)?.domain).toBe('good');
  });
});
