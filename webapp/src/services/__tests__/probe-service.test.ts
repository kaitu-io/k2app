import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest';
import type { Tunnel } from '../api-types';

const mockRun = vi.fn();

beforeEach(() => {
  vi.resetModules();
  (window as any)._k2 = { run: mockRun };
  (window as any)._platform = {
    platformType: 'desktop',
    os: 'macos',
    version: '0.4.0',
    storage: { get: vi.fn(), set: vi.fn(), remove: vi.fn() },
  };
  mockRun.mockReset();
});

afterEach(() => {
  vi.restoreAllMocks();
  delete (window as any)._k2;
  delete (window as any)._platform;
});

function mkTunnel(id: number, domain: string): Tunnel {
  return {
    id,
    domain,
    name: domain,
    serverUrl: `k2v5://u:t@${domain}:443`,
    node: { country: 'jp', id: 'n-' + id },
    recommendScore: 0.5,
  } as unknown as Tunnel;
}

async function importModules() {
  const vpnMod = await import('../../stores/vpn-machine.store');
  const probeMod = await import('../../stores/probe.store');
  const svc = await import('../probe-service');
  return { vpnMod, probeMod, svc };
}

describe('probe-service.runProbe', () => {
  it('skips when platformType is web', async () => {
    (window as any)._platform.platformType = 'web';
    const { svc } = await importModules();
    await svc.runProbe([mkTunnel(1, 'jp.k2.example')]);
    expect(mockRun).not.toHaveBeenCalled();
  });

  it('skips when VPN is connected', async () => {
    const { vpnMod, svc } = await importModules();
    vpnMod.useVPNMachineStore.setState({ state: 'connected' } as any);
    await svc.runProbe([mkTunnel(1, 'jp.k2.example')]);
    expect(mockRun).not.toHaveBeenCalled();
  });

  it('calls _k2.run probe and records results', async () => {
    const { vpnMod, probeMod, svc } = await importModules();
    vpnMod.useVPNMachineStore.setState({ state: 'idle' } as any);
    probeMod.useProbeStore.setState({ results: new Map(), inFlight: new Set(), lastUpdated: 0 });

    const now = new Date().toISOString();
    mockRun.mockResolvedValue({
      code: 0,
      data: {
        results: [{
          url: 'k2v5://u:t@jp.k2.example:443',
          avgRttMs: 40, minRttMs: 35, maxRttMs: 50, jitterMs: 15, lossRate: 0,
          reachable: true, echoSupported: true, probeScore: 0.75,
          measuredAt: now,
        }],
      },
    });

    await svc.runProbe([mkTunnel(1, 'jp.k2.example')]);

    expect(mockRun).toHaveBeenCalledWith('probe', expect.objectContaining({
      urls: ['k2v5://u:t@jp.k2.example:443'],
    }));
    expect(probeMod.useProbeStore.getState().getScore('jp.k2.example')).toBeCloseTo(0.75);
  });

  it('clears inFlight on error', async () => {
    const { vpnMod, probeMod, svc } = await importModules();
    vpnMod.useVPNMachineStore.setState({ state: 'idle' } as any);
    probeMod.useProbeStore.setState({ results: new Map(), inFlight: new Set(), lastUpdated: 0 });
    mockRun.mockRejectedValue(new Error('network'));

    await svc.runProbe([mkTunnel(1, 'jp.k2.example')]);
    expect(probeMod.useProbeStore.getState().inFlight.has('jp.k2.example')).toBe(false);
  });

  it('handles response.code != 0 without populating store', async () => {
    const { vpnMod, probeMod, svc } = await importModules();
    vpnMod.useVPNMachineStore.setState({ state: 'idle' } as any);
    probeMod.useProbeStore.setState({ results: new Map(), inFlight: new Set(), lastUpdated: 0 });
    mockRun.mockResolvedValue({ code: 501, message: 'not supported' });

    await svc.runProbe([mkTunnel(1, 'jp.k2.example')]);
    expect(probeMod.useProbeStore.getState().results.size).toBe(0);
    expect(probeMod.useProbeStore.getState().inFlight.has('jp.k2.example')).toBe(false);
  });

  it('returns early on empty tunnels list (no _k2 call)', async () => {
    const { svc } = await importModules();
    await svc.runProbe([]);
    expect(mockRun).not.toHaveBeenCalled();
  });
});
