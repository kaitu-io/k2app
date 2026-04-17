/**
 * Connection Store — Smart Server Mode Tests
 *
 * Tests serverMode/smartCountry persistence, loadServerMode restoration,
 * and connect() behavior in smart mode (k2subs:// URL generation).
 *
 * Run: cd webapp && npx vitest run src/stores/__tests__/connection.store.smart.test.ts
 */

import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest';

// Mock auth-service
vi.mock('../../services/auth-service', () => ({
  authService: {
    buildTunnelUrl: vi.fn(),
    buildSubsUrl: vi.fn(),
  },
}));

// Mock window globals
const mockRun = vi.fn();
const mockStorage = {
  get: vi.fn(),
  set: vi.fn(),
  remove: vi.fn(),
};

beforeEach(() => {
  vi.resetModules();
  (window as any)._k2 = { run: mockRun };
  (window as any)._platform = {
    os: 'macos' as const,
    version: '0.4.0',
    storage: mockStorage,
    updater: { channel: 'stable' },
  };
  mockRun.mockReset();
  mockStorage.get.mockReset();
  mockStorage.set.mockReset();
  mockStorage.remove.mockReset();
});

afterEach(() => {
  vi.restoreAllMocks();
  delete (window as any)._k2;
  delete (window as any)._platform;
});

async function getStores() {
  const connMod = await import('../connection.store');
  const vpnMod = await import('../vpn-machine.store');
  const configMod = await import('../config.store');
  // Initialize config store with split + cn (equivalent to old chnroute behavior).
  configMod.useConfigStore.setState({
    loaded: true,
    defaultVia: 'proxy',
    countryVia: 'direct',
    autoDetect: false,
    country: 'cn',
    suggestedProfile: null,
    detectedCountry: null,
  });
  // Mark lastServerUrl as loaded so cold-start recovery doesn't wait.
  connMod.useConnectionStore.setState({ lastServerUrl: null, lastServerUrlLoaded: true, serverModeLoaded: true });
  // Default to authenticated so smart-mode connect() doesn't short-circuit to LoginDialog
  // (tests that exercise the unauth path flip this off explicitly).
  const authMod = await import('../auth.store');
  authMod.useAuthStore.setState({ isAuthenticated: true });
  return { ...connMod, vpn: vpnMod, config: configMod };
}

// ==================== setServerMode Tests ====================

describe('setServerMode', () => {
  it('updates serverMode to self_hosted', async () => {
    const { useConnectionStore } = await getStores();
    await useConnectionStore.getState().setServerMode('self_hosted');
    expect(useConnectionStore.getState().serverMode).toBe('self_hosted');
  });

  it('persists self_hosted mode to storage', async () => {
    const { useConnectionStore } = await getStores();
    await useConnectionStore.getState().setServerMode('self_hosted');
    expect(mockStorage.set).toHaveBeenCalledWith('k2.vpn.server_mode', 'self_hosted');
  });

  it('persists smart mode to storage', async () => {
    const { useConnectionStore } = await getStores();
    await useConnectionStore.getState().setServerMode('smart');
    expect(mockStorage.set).toHaveBeenCalledWith('k2.vpn.server_mode', 'smart');
  });
});

// ==================== setSmartCountry Tests ====================

describe('setSmartCountry', () => {
  it('stores country code', async () => {
    const { useConnectionStore } = await getStores();
    await useConnectionStore.getState().setSmartCountry('jp');
    expect(useConnectionStore.getState().smartCountry).toBe('jp');
    expect(mockStorage.set).toHaveBeenCalledWith('k2.vpn.smart_country', 'jp');
  });

  it('removes from storage when set to null', async () => {
    const { useConnectionStore } = await getStores();
    await useConnectionStore.getState().setSmartCountry(null);
    expect(useConnectionStore.getState().smartCountry).toBeNull();
    expect(mockStorage.remove).toHaveBeenCalledWith('k2.vpn.smart_country');
  });
});

// ==================== loadServerMode Tests ====================

describe('loadServerMode', () => {
  it('restores persisted smart + country', async () => {
    mockStorage.get.mockImplementation(async (key: string) => {
      if (key === 'k2.vpn.server_mode') return 'smart';
      if (key === 'k2.vpn.smart_country') return 'jp';
      return null;
    });

    const { useConnectionStore } = await getStores();
    await useConnectionStore.getState().loadServerMode();

    const s = useConnectionStore.getState();
    expect(s.serverMode).toBe('smart');
    expect(s.smartCountry).toBe('jp');
    expect(s.serverModeLoaded).toBe(true);
  });

  it('defaults to smart when storage is empty', async () => {
    mockStorage.get.mockResolvedValue(null);

    const { useConnectionStore } = await getStores();
    await useConnectionStore.getState().loadServerMode();

    const s = useConnectionStore.getState();
    expect(s.serverMode).toBe('smart');
    expect(s.smartCountry).toBeNull();
    expect(s.serverModeLoaded).toBe(true);
  });

  it('restores persisted "manual" mode as manual (指定服务器)', async () => {
    mockStorage.get.mockImplementation(async (key: string) => {
      if (key === 'k2.vpn.server_mode') return 'manual';
      return null;
    });

    const { useConnectionStore } = await getStores();
    await useConnectionStore.getState().loadServerMode();

    expect(useConnectionStore.getState().serverMode).toBe('manual');
    expect(useConnectionStore.getState().serverModeLoaded).toBe(true);
  });
});

// ==================== Connect Smart Mode Tests ====================

describe('connect() — smart mode', () => {
  it('sets synthetic connectedTunnel with auto name when no country', async () => {
    const { useConnectionStore, vpn } = await getStores();
    mockRun.mockResolvedValue({ code: 0 });

    const { authService } = await import('../../services/auth-service');
    vi.mocked(authService.buildSubsUrl).mockResolvedValue('k2subs://udid:tok@k2.52j.me/api/subs');

    // Set smart mode with no country
    useConnectionStore.setState({ serverMode: 'smart', smartCountry: null });

    await useConnectionStore.getState().connect();

    const { connectedTunnel } = useConnectionStore.getState();
    expect(connectedTunnel).not.toBeNull();
    expect(connectedTunnel?.name).toBe('智能选择');
    expect(connectedTunnel?.domain).toBe('subs');
    expect(connectedTunnel?.serverUrl).toBe('k2subs://udid:tok@k2.52j.me/api/subs');
    expect(connectedTunnel?.country).toBe('');
    expect(connectedTunnel?.source).toBe('cloud');

    // Verify _k2.run('up') was called with the envelope carrying the subs URL in routes
    expect(mockRun).toHaveBeenCalledWith('up', expect.objectContaining({
      config: expect.objectContaining({
        routes: expect.arrayContaining([
          expect.objectContaining({ via: 'k2subs://udid:tok@k2.52j.me/api/subs' }),
        ]),
      }),
    }));
  });

  it('sets connectedTunnel with country in name when country selected', async () => {
    const { useConnectionStore } = await getStores();
    mockRun.mockResolvedValue({ code: 0 });

    const { authService } = await import('../../services/auth-service');
    vi.mocked(authService.buildSubsUrl).mockResolvedValue(
      'k2subs://udid:tok@k2.52j.me/api/subs?country=jp',
    );

    useConnectionStore.setState({ serverMode: 'smart', smartCountry: 'jp' });

    await useConnectionStore.getState().connect();

    const { connectedTunnel } = useConnectionStore.getState();
    expect(connectedTunnel).not.toBeNull();
    expect(connectedTunnel?.name).toBe('智能选择 · JP');
    expect(connectedTunnel?.country).toBe('jp');
    expect(connectedTunnel?.serverUrl).toContain('country=jp');
  });

  it('calls buildSubsUrl with country parameter', async () => {
    const { useConnectionStore } = await getStores();
    mockRun.mockResolvedValue({ code: 0 });

    const { authService } = await import('../../services/auth-service');
    vi.mocked(authService.buildSubsUrl).mockResolvedValue('k2subs://udid:tok@k2.52j.me/api/subs');

    useConnectionStore.setState({ serverMode: 'smart', smartCountry: 'us' });

    await useConnectionStore.getState().connect();

    expect(authService.buildSubsUrl).toHaveBeenCalledWith('us');
  });

  it('calls buildSubsUrl with null when no country selected', async () => {
    const { useConnectionStore } = await getStores();
    mockRun.mockResolvedValue({ code: 0 });

    const { authService } = await import('../../services/auth-service');
    vi.mocked(authService.buildSubsUrl).mockResolvedValue('k2subs://udid:tok@k2.52j.me/api/subs');

    useConnectionStore.setState({ serverMode: 'smart', smartCountry: null });

    await useConnectionStore.getState().connect();

    expect(authService.buildSubsUrl).toHaveBeenCalledWith(null);
  });

  it('smart mode does not require activeTunnel to be set', async () => {
    const { useConnectionStore } = await getStores();
    mockRun.mockResolvedValue({ code: 0 });

    const { authService } = await import('../../services/auth-service');
    vi.mocked(authService.buildSubsUrl).mockResolvedValue('k2subs://udid:tok@k2.52j.me/api/subs');

    // Smart mode with no tunnel selected — should still connect
    useConnectionStore.setState({
      serverMode: 'smart',
      smartCountry: null,
      activeTunnel: null,
      selectedCloudTunnel: null,
    });

    await useConnectionStore.getState().connect();

    // Should have called _k2.run('up')
    expect(mockRun).toHaveBeenCalledWith('up', expect.anything());
  });

  it('self_hosted mode without a configured tunnel does not connect', async () => {
    const { useConnectionStore } = await getStores();
    mockRun.mockResolvedValue({ code: 0 });

    // self_hosted mode but useSelfHostedStore has no tunnel (default mock returns null)
    useConnectionStore.setState({ serverMode: 'self_hosted' });

    await useConnectionStore.getState().connect();

    // _k2.run should NOT have been called
    expect(mockRun).not.toHaveBeenCalled();
  });

  it('persists lastServerUrl with subs URL before _k2.run', async () => {
    const { useConnectionStore } = await getStores();

    const order: string[] = [];
    mockStorage.set.mockImplementation(async (key: string) => {
      if (key === 'k2.vpn.last_server_url') order.push('persist');
    });
    mockRun.mockImplementation(async (action: string) => {
      if (action === 'up') order.push('run_up');
      return { code: 0 };
    });

    const { authService } = await import('../../services/auth-service');
    vi.mocked(authService.buildSubsUrl).mockResolvedValue('k2subs://udid:tok@k2.52j.me/api/subs');

    useConnectionStore.setState({ serverMode: 'smart', smartCountry: null });

    await useConnectionStore.getState().connect();

    expect(order).toEqual(['persist', 'run_up']);
    expect(useConnectionStore.getState().lastServerUrl).toBe('k2subs://udid:tok@k2.52j.me/api/subs');
  });

  it('no credentials → opens LoginDialog and does NOT call _k2.run(up)', async () => {
    const { useConnectionStore } = await getStores();
    mockRun.mockResolvedValue({ code: 0 });

    const { authService } = await import('../../services/auth-service');
    vi.mocked(authService.buildSubsUrl).mockResolvedValue('k2subs://k2.52j.me/api/subs');

    const authMod = await import('../auth.store');
    authMod.useAuthStore.setState({ isAuthenticated: false });

    const loginMod = await import('../login-dialog.store');
    const openSpy = vi.spyOn(loginMod.useLoginDialogStore.getState(), 'open');

    useConnectionStore.setState({ serverMode: 'smart', smartCountry: null });

    await useConnectionStore.getState().connect();

    expect(mockRun).not.toHaveBeenCalledWith('up', expect.anything());
    expect(openSpy).toHaveBeenCalledTimes(1);
  });

  it('dispatches USER_CONNECT and moves VPN to connecting state', async () => {
    const { useConnectionStore, vpn } = await getStores();
    mockRun.mockResolvedValue({ code: 0 });

    const { authService } = await import('../../services/auth-service');
    vi.mocked(authService.buildSubsUrl).mockResolvedValue('k2subs://udid:tok@k2.52j.me/api/subs');

    useConnectionStore.setState({ serverMode: 'smart', smartCountry: null });

    await useConnectionStore.getState().connect();

    // VPN machine should have transitioned through connecting
    // After successful _k2.run('up'), it may still be in connecting (waiting for BACKEND_CONNECTED)
    const vpnState = vpn.useVPNMachineStore.getState().state;
    expect(vpnState).toBe('connecting');
  });
});

// ==================== Mobile branch (subs-resolver) ====================

vi.mock('../../services/subs-resolver', () => ({
  resolveTunnel: vi.fn(),
}));

describe('connect() — mobile smart mode', () => {
  beforeEach(() => {
    // Override platformType to mobile
    (window as any)._platform.platformType = 'mobile';
    (window as any)._platform.os = 'ios';
  });

  it('mobile smart auto: calls resolver and sends k2v5:// to _k2.run', async () => {
    const { useConnectionStore } = await getStores();
    mockRun.mockResolvedValue({ code: 0 });

    const { authService } = await import('../../services/auth-service');
    vi.mocked(authService.buildSubsUrl).mockResolvedValue('k2subs://udid:tok@k2.52j.me/api/subs');

    const { resolveTunnel } = await import('../../services/subs-resolver');
    vi.mocked(resolveTunnel).mockResolvedValue({
      url: 'k2v5://udid:tok@node-A.example.com:443?ech=x',
      allCandidates: [{ url: 'k2v5://udid:tok@node-A.example.com:443?ech=x', weight: 1 }],
      source: 'fresh',
      fetchedAt: Date.now(),
    });

    useConnectionStore.setState({ serverMode: 'smart', smartCountry: null });

    await useConnectionStore.getState().connect();

    expect(resolveTunnel).toHaveBeenCalledWith('k2subs://udid:tok@k2.52j.me/api/subs');
    expect(mockRun).toHaveBeenCalledWith('up', expect.objectContaining({
      config: expect.objectContaining({
        routes: expect.arrayContaining([
          expect.objectContaining({ via: 'k2v5://udid:tok@node-A.example.com:443?ech=x' }),
        ]),
      }),
    }));
  });

  it('mobile smart: retries with exclude on retryable engine error (570)', async () => {
    const { useConnectionStore } = await getStores();
    // First call: 570 (no outbound). Second call: 0 (success).
    mockRun.mockResolvedValueOnce({ code: 570, message: 'no k2v5 outbound' });
    mockRun.mockResolvedValueOnce({ code: 0 });

    const { authService } = await import('../../services/auth-service');
    vi.mocked(authService.buildSubsUrl).mockResolvedValue('k2subs://u:t@host/api/subs');

    const { resolveTunnel } = await import('../../services/subs-resolver');
    vi.mocked(resolveTunnel)
      .mockResolvedValueOnce({
        url: 'k2v5://u:t@A',
        allCandidates: [],
        source: 'fresh',
        fetchedAt: Date.now(),
      })
      .mockResolvedValueOnce({
        url: 'k2v5://u:t@B',
        allCandidates: [],
        source: 'cache',
        fetchedAt: Date.now(),
      });

    useConnectionStore.setState({ serverMode: 'smart', smartCountry: null });
    await useConnectionStore.getState().connect();

    expect(resolveTunnel).toHaveBeenCalledTimes(2);
    expect(resolveTunnel).toHaveBeenLastCalledWith('k2subs://u:t@host/api/subs', ['k2v5://u:t@A']);
    expect(mockRun).toHaveBeenCalledTimes(2);
  });

  it('mobile smart: gives up after 3 total attempts and dispatches BACKEND_ERROR', async () => {
    const { useConnectionStore, vpn } = await getStores();
    mockRun.mockResolvedValue({ code: 503, message: 'unreachable' });

    const { authService } = await import('../../services/auth-service');
    vi.mocked(authService.buildSubsUrl).mockResolvedValue('k2subs://u:t@host/api/subs');

    const { resolveTunnel } = await import('../../services/subs-resolver');
    vi.mocked(resolveTunnel).mockImplementation(async () => ({
      url: 'k2v5://u:t@' + Math.random().toString(36).slice(2),
      allCandidates: [],
      source: 'fresh',
      fetchedAt: Date.now(),
    }));

    useConnectionStore.setState({ serverMode: 'smart', smartCountry: null });
    await useConnectionStore.getState().connect();

    // resolveTunnel called once for initial + twice for retry
    expect(resolveTunnel).toHaveBeenCalledTimes(3);
    expect(mockRun).toHaveBeenCalledTimes(3);
    // After exhausting retries, error overlay should be set
    expect(vpn.useVPNMachineStore.getState().error?.code).toBe(503);
  });

  it('mobile smart: does NOT retry on 401 (account-level error)', async () => {
    const { useConnectionStore } = await getStores();
    mockRun.mockResolvedValue({ code: 401, message: 'invalid credentials' });

    const { authService } = await import('../../services/auth-service');
    vi.mocked(authService.buildSubsUrl).mockResolvedValue('k2subs://u:t@host/api/subs');

    const { resolveTunnel } = await import('../../services/subs-resolver');
    vi.mocked(resolveTunnel).mockResolvedValue({
      url: 'k2v5://u:t@A',
      allCandidates: [],
      source: 'fresh',
      fetchedAt: Date.now(),
    });

    useConnectionStore.setState({ serverMode: 'smart', smartCountry: null });
    await useConnectionStore.getState().connect();

    // Only 1 call — no retry on 401
    expect(resolveTunnel).toHaveBeenCalledTimes(1);
    expect(mockRun).toHaveBeenCalledTimes(1);
  });

  it('mobile smart: resolve() initial failure aborts before _k2.run', async () => {
    const { useConnectionStore } = await getStores();
    mockRun.mockResolvedValue({ code: 0 });

    const { authService } = await import('../../services/auth-service');
    vi.mocked(authService.buildSubsUrl).mockResolvedValue('k2subs://u:t@host/api/subs');

    const { resolveTunnel } = await import('../../services/subs-resolver');
    vi.mocked(resolveTunnel).mockRejectedValue(new Error('fetch failed'));

    useConnectionStore.setState({ serverMode: 'smart', smartCountry: null });
    await useConnectionStore.getState().connect();

    expect(mockRun).not.toHaveBeenCalled();
  });
});
