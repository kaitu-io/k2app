import { describe, it, expect, beforeEach } from 'vitest';
import type { ProbeResult } from '../../services/api-types';
import { useProbeStore } from '../probe.store';

function mkResult(overrides: Partial<ProbeResult> = {}): ProbeResult {
  return {
    url: 'k2v5://u:t@host.example:443',
    avgRttMs: 45,
    minRttMs: 40,
    maxRttMs: 55,
    jitterMs: 15,
    lossRate: 0,
    reachable: true,
    echoSupported: true,
    probeScore: 0.7,
    measuredAt: new Date().toISOString(),
    ...overrides,
  };
}

describe('probe.store', () => {
  beforeEach(() => {
    useProbeStore.setState({ results: new Map(), inFlight: new Set(), lastUpdated: 0 });
  });

  it('record stores results keyed by domain', () => {
    useProbeStore.getState().record([
      mkResult({ url: 'k2v5://u:t@jp.k2.example:443' }),
      mkResult({ url: 'k2v5://u:t@us.k2.example:443', probeScore: 0.4 }),
    ]);
    const { getScore, results } = useProbeStore.getState();
    expect(results.size).toBe(2);
    expect(getScore('jp.k2.example')).toBeCloseTo(0.7);
    expect(getScore('us.k2.example')).toBeCloseTo(0.4);
  });

  it('getScore returns null when no data', () => {
    expect(useProbeStore.getState().getScore('never.probed')).toBeNull();
  });

  it('getScore returns null when stale (> 15min)', () => {
    const old = mkResult({ measuredAt: new Date(Date.now() - 16 * 60 * 1000).toISOString() });
    useProbeStore.getState().record([old]);
    expect(useProbeStore.getState().getScore('host.example')).toBeNull();
  });

  it('markInFlight / clearInFlight round-trip', () => {
    const { markInFlight, clearInFlight } = useProbeStore.getState();
    markInFlight(['jp.k2.example', 'us.k2.example']);
    expect(useProbeStore.getState().inFlight.has('jp.k2.example')).toBe(true);
    clearInFlight(['jp.k2.example']);
    expect(useProbeStore.getState().inFlight.has('jp.k2.example')).toBe(false);
    expect(useProbeStore.getState().inFlight.has('us.k2.example')).toBe(true);
  });

  it('sentinel probeScore=-1 yields null getScore', () => {
    useProbeStore.getState().record([
      mkResult({ url: 'k2v5://u:t@old.k2.example:443', probeScore: -1, echoSupported: false }),
    ]);
    expect(useProbeStore.getState().getScore('old.k2.example')).toBeNull();
  });

  it('getResult returns full result even when score is null', () => {
    useProbeStore.getState().record([
      mkResult({ url: 'k2v5://u:t@old.k2.example:443', probeScore: -1, echoSupported: false }),
    ]);
    const r = useProbeStore.getState().getResult('old.k2.example');
    expect(r).not.toBeNull();
    expect(r?.echoSupported).toBe(false);
  });
});
