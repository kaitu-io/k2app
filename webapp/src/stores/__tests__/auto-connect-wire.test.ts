import { readFileSync } from 'node:fs';
import path from 'node:path';
import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest';

// Golden wire body, shared with api/api_tunnel_v20260717_test.go. The Go test proves
// the real response type produces exactly these bytes' shape; this one proves Auto
// connects from them. Renaming a key therefore has to go through both.
const repoRoot = path.resolve(__dirname, '../../../..');
const wireBody = readFileSync(path.join(repoRoot, 'contracts/tunnel-list-wire.json'), 'utf8');

const mockResolveAndFetch = vi.fn();
vi.mock('../../services/resolve-and-fetch', () => ({
  resolveAndFetch: (...args: unknown[]) => mockResolveAndFetch(...args),
}));

vi.mock('../../services/auth-service', () => ({
  authService: {
    getToken: vi.fn().mockResolvedValue('access-token'),
    getTokenEpoch: vi.fn().mockReturnValue(0),
    getTunnelToken: vi.fn().mockResolvedValue(null),
    setTunnelToken: vi.fn().mockResolvedValue(undefined),
    buildTunnelUrl: vi.fn(async (u: string) => u),
  },
}));

vi.mock('../../services/stats', () => ({
  statsService: { trackFunnelOnce: vi.fn(), trackFunnelDaily: vi.fn(), trackFunnel: vi.fn() },
}));

const mockRun = vi.fn();

beforeEach(() => {
  vi.resetModules();
  (window as any)._k2 = { run: mockRun };
  (window as any)._platform = {
    os: 'macos' as const,
    version: '0.4.0',
    storage: { get: vi.fn(), set: vi.fn(), remove: vi.fn() },
    updater: { channel: 'stable' },
  };
  mockRun.mockReset();
  mockRun.mockResolvedValue({ code: 0 });
  mockResolveAndFetch.mockReset();
});

afterEach(() => {
  delete (window as any)._k2;
  delete (window as any)._platform;
});

/** mulberry32, so repeated connects are reproducible through Math.random. */
const prng = (seed: number) => {
  let a = seed >>> 0;
  return () => {
    a = (a + 0x6D2B79F5) >>> 0;
    let t = a;
    t = Math.imul(t ^ (t >>> 15), t | 1);
    t ^= t + Math.imul(t ^ (t >>> 7), t | 61);
    return ((t ^ (t >>> 14)) >>> 0) / 4294967296;
  };
};

/** Fresh stores, then the raw body through real cloudApi and cache into connect(). Returns the up() vias. */
async function connectFromWire(excludedCountries: string[], seed: number): Promise<string[]> {
  vi.resetModules();
  mockRun.mockReset();
  mockRun.mockResolvedValue({ code: 0 });
  mockResolveAndFetch.mockReset();
  mockResolveAndFetch.mockResolvedValue({
    transport: 'ok', status: 200, json: async () => JSON.parse(wireBody),
  });

  const { useConnectionStore, refreshTunnelsCacheAfterAutoPick } = await import('../connection.store');
  const { useConfigStore } = await import('../config.store');
  const { useAuthStore } = await import('../auth.store');
  const { cacheStore } = await import('../../services/cache-store');
  useConfigStore.setState({
    loaded: true, defaultVia: 'proxy', countryVia: 'direct', autoDetect: false, country: 'cn',
    suggestedProfile: null, detectedCountry: null,
  });
  useConnectionStore.setState({
    lastServerUrl: null, lastServerUrlLoaded: true, serverMode: 'manual' as const, serverModeLoaded: true,
  });
  useAuthStore.setState({ isAuthenticated: true });
  await useConnectionStore.getState().setExcludedCountries(excludedCountries);

  // The same refresh connect() schedules, so the cache is written by production code.
  refreshTunnelsCacheAfterAutoPick({ pollIntervalMs: 1 });
  await vi.waitFor(() => expect(cacheStore.get('api:tunnels')).not.toBeNull());
  expect(mockResolveAndFetch).toHaveBeenCalledWith(
    expect.objectContaining({ method: 'GET', path: '/api/v20260717/tunnels' }),
  );

  const spy = vi.spyOn(Math, 'random').mockImplementation(prng(seed));
  await useConnectionStore.getState().connect();
  spy.mockRestore();
  cacheStore.clear();

  const up = mockRun.mock.calls.find((c: any[]) => c[0] === 'up');
  expect(up, 'Auto must reach up(): a renamed wire key empties the pool and gives 572').toBeDefined();
  return (up![1].config.routes as Array<{ via: string }>).map(r => r.via);
}

describe('connect() Auto from the raw /api/v20260717/tunnels body', () => {
  const wire = JSON.parse(wireBody).data.items as Array<{ domain: string; serverUrl: string }>;
  const urlOf = (domain: string) => wire.find(t => t.domain === domain)!.serverUrl;

  it('real cloudApi, real cache and real picker reach up() with a wire serverUrl', async () => {
    const vias = await connectFromWire([], 1);
    expect(wire.some(t => vias.includes(t.serverUrl))).toBe(true);
  });

  it('excluded countries read node.country from the wire and are never picked', async () => {
    // Without the exclusion this fixture returns only tokyo-1 or seoul-1, so a renamed
    // country key would let them through.
    for (let seed = 1; seed <= 8; seed++) {
      const vias = await connectFromWire(['jp', 'kr'], seed);
      expect(vias, `seed ${seed}`).not.toContain(urlOf('tokyo-1.example.com'));
      expect(vias, `seed ${seed}`).not.toContain(urlOf('seoul-1.example.com'));
      expect(vias, `seed ${seed}`).toContain(urlOf('exhausted-1.example.com'));
    }
  });
});
