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
  return { ...connMod, vpn: vpnMod, config: configMod };
}

// ==================== setServerMode Tests ====================

describe('setServerMode', () => {
  it('updates serverMode state', async () => {
    const { useConnectionStore } = await getStores();
    await useConnectionStore.getState().setServerMode('manual');
    expect(useConnectionStore.getState().serverMode).toBe('manual');
  });

  it('persists to storage', async () => {
    const { useConnectionStore } = await getStores();
    await useConnectionStore.getState().setServerMode('manual');
    expect(mockStorage.set).toHaveBeenCalledWith('k2.vpn.server_mode', 'manual');
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

  it('restores manual mode', async () => {
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

    // Verify _k2.run('up') was called with the subs URL in routes
    expect(mockRun).toHaveBeenCalledWith('up', expect.objectContaining({
      routes: expect.arrayContaining([
        expect.objectContaining({ via: 'k2subs://udid:tok@k2.52j.me/api/subs' }),
      ]),
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

  it('manual mode without activeTunnel does not connect', async () => {
    const { useConnectionStore } = await getStores();
    mockRun.mockResolvedValue({ code: 0 });

    useConnectionStore.setState({
      serverMode: 'manual',
      activeTunnel: null,
    });

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
