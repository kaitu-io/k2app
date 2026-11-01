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
    buildSubsUrl: vi.fn(),
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

describe('connect() Auto from the raw /api/v20260717/tunnels body', () => {
  it('real cloudApi, real cache and real picker reach up() with a wire serverUrl', async () => {
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

    // The same refresh connect() schedules, so the cache is written by production code.
    refreshTunnelsCacheAfterAutoPick({ pollIntervalMs: 1 });
    await vi.waitFor(() => expect(cacheStore.get('api:tunnels')).not.toBeNull());
    expect(mockResolveAndFetch).toHaveBeenCalledWith(
      expect.objectContaining({ method: 'GET', path: '/api/v20260717/tunnels' }),
    );

    await useConnectionStore.getState().connect();

    const up = mockRun.mock.calls.find((c: any[]) => c[0] === 'up');
    expect(up, 'Auto must reach up(): a renamed wire key empties the pool and gives 572').toBeDefined();
    const vias = (up![1].config.routes as Array<{ via: string }>).map(r => r.via);
    const wire = JSON.parse(wireBody).data.items as Array<{ serverUrl: string }>;
    expect(wire.some(t => vias.includes(t.serverUrl))).toBe(true);
    cacheStore.clear();
  });
});
