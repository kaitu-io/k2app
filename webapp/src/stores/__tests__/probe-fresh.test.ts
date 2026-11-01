import { describe, it, expect } from 'vitest';
import { freshProbeOf } from '../probe.store';
import type { ProbeResult } from '../../services/api-types';

const base = (o: Partial<ProbeResult>): ProbeResult => ({
  url: 'k2v5://a', avgRttMs: 50, minRttMs: 40, maxRttMs: 60, jitterMs: 1, lossRate: 0,
  reachable: true, echoSupported: true, probeScore: 0.9, measuredAt: new Date().toISOString(), ...o,
});
const old = () => new Date(Date.now() - 16 * 60 * 1000).toISOString();

describe('freshProbeOf', () => {
  it('returns RTT for a fresh measured result', () => {
    expect(freshProbeOf(base({}))).toEqual({ avgRttMs: 50, reachable: true });
  });
  it('treats stale results as no data', () => {
    expect(freshProbeOf(base({ measuredAt: old() }))).toBeNull();
    expect(freshProbeOf(base({ reachable: false, measuredAt: old() }))).toBeNull();
  });
  it('treats the -1 sentinel as no data', () => {
    expect(freshProbeOf(base({ probeScore: -1, avgRttMs: 0 }))).toBeNull();
  });
  it('reports fresh unreachable', () => {
    expect(freshProbeOf(base({ reachable: false, probeScore: 0 }))?.reachable).toBe(false);
  });
  it('null for missing', () => {
    expect(freshProbeOf(null)).toBeNull();
  });
});
