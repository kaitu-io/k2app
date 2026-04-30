/**
 * Connection Store Tests
 *
 * Tests connection target selection, orchestration, connectedTunnel snapshot,
 * and connectEpoch guard.
 *
 * Run: cd webapp && npx vitest run src/stores/__tests__/connection.store.test.ts
 */

import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest';

// Mock auth-service
vi.mock('../../services/auth-service', () => ({
  authService: {
    buildTunnelUrl: vi.fn(),
    buildSubsUrl: vi.fn(),
  },
}));

// Mock cloud-api so the Auto-pick refresh doesn't issue real HTTP calls.
const mockCloudApiGet = vi.fn();
vi.mock('../../services/cloud-api', () => ({
  cloudApi: {
    get: (...args: unknown[]) => mockCloudApiGet(...args),
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
  mockCloudApiGet.mockReset();
  mockCloudApiGet.mockResolvedValue({ code: 0, message: 'ok', data: { items: [], echConfigList: undefined } });
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
  // Default mode is 'manual' — tests that need a tunnel call selectCloudTunnel() first.
  connMod.useConnectionStore.setState({ lastServerUrl: null, lastServerUrlLoaded: true, serverMode: 'manual' as const, serverModeLoaded: true });
  // Default to authenticated so connect() reaches the connect path.
  const authMod = await import('../auth.store');
  authMod.useAuthStore.setState({ isAuthenticated: true });
  return { ...connMod, vpn: vpnMod, config: configMod };
}

// ==================== Selection Tests ====================

describe('Connection Store - Selection', () => {
  it('defaults to manual mode with no tunnel selected', async () => {
    const { useConnectionStore } = await getStores();
    const state = useConnectionStore.getState();

    expect(state.serverMode).toBe('manual');
    expect(state.selectedCloudTunnel).toBeNull();
    expect(state.activeTunnel).toBeNull();
    expect(state.connectedTunnel).toBeNull();
  });

  it('selectCloudTunnel sets tunnel and computes activeTunnel', async () => {
    const { useConnectionStore } = await getStores();

    const tunnel = {
      id: 1,
      domain: 'tokyo.example.com',
      name: 'Tokyo',
      protocol: 'k2v5',
      port: 443,
      serverUrl: 'k2v5://tokyo.example.com:443',
      node: { country: 'JP' },
    } as any;

    useConnectionStore.getState().selectCloudTunnel(tunnel);

    const state = useConnectionStore.getState();
    expect(state.selectedCloudTunnel).toBe(tunnel);
    expect(state.activeTunnel).toEqual({
      source: 'cloud',
      domain: 'tokyo.example.com',
      name: 'Tokyo',
      country: 'JP',
      serverUrl: 'k2v5://tokyo.example.com:443',
      ipv4: '',
    });
  });

  it('selectSelfHosted sets serverMode and computes activeTunnel from self-hosted store', async () => {
    // Set up self-hosted store with a tunnel
    const selfHostedMod = await import('../self-hosted.store');
    selfHostedMod.useSelfHostedStore.setState({
      tunnel: {
        uri: 'k2v5://alice:token@1.2.3.4:443?country=JP#tokyo',
        name: 'tokyo',
        country: 'JP',
      },
      loaded: true,
    });

    const { useConnectionStore } = await getStores();
    useConnectionStore.getState().selectSelfHosted();

    const state = useConnectionStore.getState();
    expect(state.serverMode).toBe('self_hosted');
    expect(state.activeTunnel).toEqual({
      source: 'self_hosted',
      domain: '1.2.3.4',
      name: 'tokyo',
      country: 'JP',
      serverUrl: 'k2v5://alice:token@1.2.3.4:443?country=JP#tokyo',
      ipv4: '',
    });
  });

  it('selectSelfHosted with no tunnel results in null activeTunnel', async () => {
    const selfHostedMod = await import('../self-hosted.store');
    selfHostedMod.useSelfHostedStore.setState({ tunnel: null, loaded: true });

    const { useConnectionStore } = await getStores();
    useConnectionStore.getState().selectSelfHosted();

    expect(useConnectionStore.getState().activeTunnel).toBeNull();
  });
});

// ==================== Connect Tests ====================

describe('Connection Store - Connect', () => {
  it('connect (manual mode) snapshots connectedTunnel and calls _k2.run(up)', async () => {
    const { useConnectionStore, vpn } = await getStores();
    mockRun.mockResolvedValue({ code: 0 });

    const { authService } = await import('../../services/auth-service');
    vi.mocked(authService.buildTunnelUrl).mockResolvedValue('k2v5://u:t@tokyo.example.com:443');

    const tunnel = {
      id: 1, domain: 'tokyo.example.com', name: 'Tokyo', protocol: 'k2v5',
      port: 443, serverUrl: 'k2v5://tokyo.example.com:443', node: { country: 'JP' },
    } as any;
    useConnectionStore.getState().selectCloudTunnel(tunnel);

    await useConnectionStore.getState().connect();

    // connectedTunnel is the selected cloud tunnel snapshot
    const ct = useConnectionStore.getState().connectedTunnel;
    expect(ct?.source).toBe('cloud');
    expect(ct?.domain).toBe('tokyo.example.com');

    // _k2.run('up') called with envelope { config, alwaysOn } carrying
    // the resolved k2v5 URL in routes (manual mode uses the selected tunnel's
    // serverUrl directly — no k2subs indirection).
    expect(mockRun).toHaveBeenCalledWith('up', expect.objectContaining({
      config: expect.objectContaining({ mode: 'tun' }),
      alwaysOn: false,
    }));
    const upCall = mockRun.mock.calls.find(c => c[0] === 'up');
    const routes = upCall?.[1]?.config?.routes as Array<{ via: string }>;
    expect(routes.some(r => r.via === 'k2v5://u:t@tokyo.example.com:443')).toBe(true);
    expect(upCall?.[1]?.config?.server).toBeUndefined();

    // USER_CONNECT dispatched
    expect(vpn.useVPNMachineStore.getState().state).toBe('connecting');
  });

  it('connect with self-hosted uses raw URI (no auth injection)', async () => {
    const selfHostedMod = await import('../self-hosted.store');
    selfHostedMod.useSelfHostedStore.setState({
      tunnel: {
        uri: 'k2v5://alice:token@1.2.3.4:443#tokyo',
        name: 'tokyo',
        country: 'JP',
      },
      loaded: true,
    });

    const { useConnectionStore } = await getStores();
    mockRun.mockResolvedValue({ code: 0 });

    useConnectionStore.getState().selectSelfHosted();
    useConnectionStore.setState({ serverMode: 'self_hosted' });
    await useConnectionStore.getState().connect();

    const upCall = mockRun.mock.calls.find(c => c[0] === 'up');
    expect(upCall).toBeDefined();
    const routes = upCall?.[1]?.config?.routes as Array<{ via: string; match: Record<string, unknown> }>;
    const lastRoute = routes[routes.length - 1];
    expect(lastRoute?.via).toBe('k2v5://alice:token@1.2.3.4:443#tokyo');
  });

  it('connect passes alwaysOn=true when config store has it set', async () => {
    const { useConnectionStore, vpn: _vpn } = await getStores();
    mockRun.mockResolvedValue({ code: 0 });

    const { authService } = await import('../../services/auth-service');
    vi.mocked(authService.buildTunnelUrl).mockResolvedValue('k2v5://u:t@tokyo.example.com:443');

    // Manual mode requires a selected tunnel to reach the connect path.
    const tunnel = {
      id: 1, domain: 'tokyo.example.com', name: 'Tokyo', protocol: 'k2v5',
      port: 443, serverUrl: 'k2v5://tokyo.example.com:443', node: { country: 'JP' },
    } as any;
    useConnectionStore.getState().selectCloudTunnel(tunnel);

    // Flip alwaysOn on in config store
    const { useConfigStore } = await import('../config.store');
    useConfigStore.setState({ alwaysOn: true });

    await useConnectionStore.getState().connect();

    const upCall = mockRun.mock.calls.find(c => c[0] === 'up');
    expect(upCall?.[1]?.alwaysOn).toBe(true);

    // Cleanup — reset for subsequent tests
    useConfigStore.setState({ alwaysOn: false });
  });

  it('connect does nothing when self_hosted mode has no tunnel configured', async () => {
    const selfHostedMod = await import('../self-hosted.store');
    selfHostedMod.useSelfHostedStore.setState({ tunnel: null, loaded: true });

    const { useConnectionStore } = await getStores();
    mockRun.mockResolvedValue({ code: 0 });

    useConnectionStore.setState({ serverMode: 'self_hosted' });
    await useConnectionStore.getState().connect();

    expect(mockRun).not.toHaveBeenCalled();
  });

  it('connect does nothing when manual mode has no tunnel selected', async () => {
    const { useConnectionStore } = await getStores();
    mockRun.mockResolvedValue({ code: 0 });

    // Default manual mode with no selection
    await useConnectionStore.getState().connect();

    expect(mockRun).not.toHaveBeenCalled();
  });

  it('connectEpoch guards against stale connect', async () => {
    const { useConnectionStore, vpn } = await getStores();

    // Select a cloud tunnel for manual mode
    useConnectionStore.getState().selectCloudTunnel({
      id: 1, domain: 'tokyo.example.com', name: 'Tokyo', protocol: 'k2v5',
      port: 443, serverUrl: 'k2v5://tokyo.example.com:443', node: { country: 'JP' },
    } as any);

    // Mock auth service to be slow
    const { authService } = await import('../../services/auth-service');
    let resolveAuth: (v: string) => void;
    vi.mocked(authService.buildTunnelUrl).mockImplementation(
      () => new Promise(resolve => { resolveAuth = resolve; }),
    );

    // Start connect
    const connectPromise = useConnectionStore.getState().connect();

    // connect() dispatches USER_CONNECT after auth resolves, so VPN state is
    // still idle here. Manually move to 'connecting' so disconnect guard passes.
    vpn.dispatch('USER_CONNECT');

    // While auth is resolving, user disconnects (bumps epoch)
    await useConnectionStore.getState().disconnect();

    // Now resolve auth — connect should bail due to epoch mismatch
    resolveAuth!('k2v5://u:t@tokyo.example.com:443');
    await connectPromise;

    // _k2.run('up') should NOT have been called
    expect(mockRun).toHaveBeenCalledWith('down'); // from disconnect
    expect(mockRun).not.toHaveBeenCalledWith('up', expect.anything());
  });
});

// ==================== Disconnect Tests ====================

describe('Connection Store - Disconnect', () => {
  it('disconnect clears connectedTunnel and calls _k2.run(down)', async () => {
    const { useConnectionStore, vpn } = await getStores();
    mockRun.mockResolvedValue({ code: 0 });

    // Put machine into connected state first
    vpn.dispatch('USER_CONNECT');
    vpn.dispatch('BACKEND_CONNECTED');
    expect(vpn.useVPNMachineStore.getState().state).toBe('connected');

    // Simulate being connected
    useConnectionStore.setState({
      connectedTunnel: {
        source: 'cloud',
        domain: 'test.com',
        name: 'Test',
        country: 'US',
        serverUrl: 'k2v5://test.com:443',
      },
    });

    await useConnectionStore.getState().disconnect();

    expect(useConnectionStore.getState().connectedTunnel).toBeNull();
    expect(mockRun).toHaveBeenCalledWith('down');
    expect(vpn.useVPNMachineStore.getState().state).toBe('disconnecting');
  });
});

// ==================== Config Persistence Tests ====================

describe('Connection Store - Config Persistence', () => {
  it('connect persists lastServerUrl BEFORE _k2.run so cold-start can recover', async () => {
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
    vi.mocked(authService.buildTunnelUrl).mockResolvedValue('k2v5://u:t@tokyo.example.com:443');

    useConnectionStore.getState().selectCloudTunnel({
      id: 1, domain: 'tokyo.example.com', name: 'Tokyo', protocol: 'k2v5',
      port: 443, serverUrl: 'k2v5://tokyo.example.com:443', node: { country: 'JP' },
    } as any);

    await useConnectionStore.getState().connect();

    expect(order).toEqual(['persist', 'run_up']);
    expect(useConnectionStore.getState().lastServerUrl).toBe('k2v5://u:t@tokyo.example.com:443');
  });
});

// ==================== State Guard Tests ====================

describe('Connection Store - State Guards', () => {
  it('connect rejects when VPN is already connecting (double-click)', async () => {
    const { useConnectionStore, vpn } = await getStores();
    mockRun.mockResolvedValue({ code: 0 });

    useConnectionStore.getState().selectCloudTunnel({
      id: 1, domain: 'test.com', name: 'Test', protocol: 'k2v5',
      port: 443, serverUrl: 'k2v5://test.com:443', node: { country: 'US' },
    } as any);

    // Put VPN into connecting state
    vpn.dispatch('USER_CONNECT');
    expect(vpn.useVPNMachineStore.getState().state).toBe('connecting');

    await useConnectionStore.getState().connect();

    // _k2.run should NOT have been called — guard rejected
    expect(mockRun).not.toHaveBeenCalled();
  });

  it('connect rejects when VPN is connected', async () => {
    const { useConnectionStore, vpn } = await getStores();
    mockRun.mockResolvedValue({ code: 0 });

    useConnectionStore.getState().selectCloudTunnel({
      id: 1, domain: 'test.com', name: 'Test', protocol: 'k2v5',
      port: 443, serverUrl: 'k2v5://test.com:443', node: { country: 'US' },
    } as any);

    vpn.dispatch('USER_CONNECT');
    vpn.dispatch('BACKEND_CONNECTED');
    expect(vpn.useVPNMachineStore.getState().state).toBe('connected');

    await useConnectionStore.getState().connect();
    expect(mockRun).not.toHaveBeenCalled();
  });

  it('connect rejects when VPN is disconnecting', async () => {
    const { useConnectionStore, vpn } = await getStores();
    mockRun.mockResolvedValue({ code: 0 });

    useConnectionStore.getState().selectCloudTunnel({
      id: 1, domain: 'test.com', name: 'Test', protocol: 'k2v5',
      port: 443, serverUrl: 'k2v5://test.com:443', node: { country: 'US' },
    } as any);

    vpn.dispatch('USER_CONNECT');
    vpn.dispatch('BACKEND_CONNECTED');
    vpn.dispatch('USER_DISCONNECT');
    expect(vpn.useVPNMachineStore.getState().state).toBe('disconnecting');

    await useConnectionStore.getState().connect();
    expect(mockRun).not.toHaveBeenCalled();
  });

  it('connect allowed from error state', async () => {
    const { useConnectionStore, vpn } = await getStores();
    mockRun.mockResolvedValue({ code: 0 });

    useConnectionStore.getState().selectCloudTunnel({
      id: 1, domain: 'test.com', name: 'Test', protocol: 'k2v5',
      port: 443, serverUrl: 'k2v5://test.com:443', node: { country: 'US' },
    } as any);

    const { authService } = await import('../../services/auth-service');
    vi.mocked(authService.buildTunnelUrl).mockResolvedValue('k2v5://u:t@test.com:443');

    // Put VPN into error→idle state
    vpn.dispatch('USER_CONNECT');
    vpn.dispatch('BACKEND_ERROR', { error: { code: 502, message: 'fail' } });
    expect(vpn.useVPNMachineStore.getState().state).toBe('idle');

    await useConnectionStore.getState().connect();
    expect(mockRun).toHaveBeenCalledWith('up', expect.anything());
  });

  it('disconnect rejects when already idle', async () => {
    const { useConnectionStore, vpn } = await getStores();
    mockRun.mockResolvedValue({ code: 0 });

    expect(vpn.useVPNMachineStore.getState().state).toBe('idle');

    await useConnectionStore.getState().disconnect();
    expect(mockRun).not.toHaveBeenCalled();
  });

  it('disconnect rejects when already disconnecting (double-click)', async () => {
    const { useConnectionStore, vpn } = await getStores();
    mockRun.mockResolvedValue({ code: 0 });

    vpn.dispatch('USER_CONNECT');
    vpn.dispatch('BACKEND_CONNECTED');
    vpn.dispatch('USER_DISCONNECT');
    expect(vpn.useVPNMachineStore.getState().state).toBe('disconnecting');

    await useConnectionStore.getState().disconnect();
    expect(mockRun).not.toHaveBeenCalled();
  });
});

// ==================== Selection → Mode symmetry Tests ====================

describe('Connection Store - Selection mode symmetry', () => {
  it('selectCloudTunnel sets serverMode to manual and persists', async () => {
    const { useConnectionStore } = await getStores();

    const tunnel = {
      id: 1, domain: 'tokyo.example.com', name: 'Tokyo', protocol: 'k2v5',
      port: 443, serverUrl: 'k2v5://tokyo.example.com:443', node: { country: 'JP' },
    } as any;

    // Flip to self_hosted first, then select cloud — should revert to manual.
    useConnectionStore.setState({ serverMode: 'self_hosted' });
    useConnectionStore.getState().selectCloudTunnel(tunnel);

    expect(useConnectionStore.getState().serverMode).toBe('manual');
    expect(mockStorage.set).toHaveBeenCalledWith('k2.vpn.server_mode', 'manual');
  });

  it('selectSelfHosted persists serverMode to storage', async () => {
    const selfHostedMod = await import('../self-hosted.store');
    selfHostedMod.useSelfHostedStore.setState({
      tunnel: { uri: 'k2v5://u:t@1.2.3.4:443', name: 'n', country: 'JP' },
      loaded: true,
    });

    const { useConnectionStore } = await getStores();
    useConnectionStore.getState().selectSelfHosted();

    expect(useConnectionStore.getState().serverMode).toBe('self_hosted');
    expect(mockStorage.set).toHaveBeenCalledWith('k2.vpn.server_mode', 'self_hosted');
  });
});

// ==================== connect() empty-serverUrl hard guards ====================

describe('Connection Store - connect() hard guards', () => {
  it('manual mode with selectedCloudTunnel.serverUrl = "" aborts and does NOT call _k2.run', async () => {
    const { useConnectionStore, vpn } = await getStores();
    mockRun.mockResolvedValue({ code: 0 });

    // Directly inject an invalid selection (simulates stale tunnel list where serverUrl was cleared)
    useConnectionStore.setState({
      serverMode: 'manual',
      selectedCloudTunnel: {
        id: 9, domain: 'broken.example.com', name: 'Broken', protocol: 'k2v5',
        port: 443, serverUrl: '', node: { country: 'US' },
      } as any,
    });

    await useConnectionStore.getState().connect();

    expect(mockRun).not.toHaveBeenCalledWith('up', expect.anything());
    // VPN machine should surface an error (not stuck in connecting)
    expect(vpn.useVPNMachineStore.getState().state).toBe('idle');
    expect(vpn.useVPNMachineStore.getState().error).not.toBeNull();
  });

  it('self_hosted mode with tunnel.uri = "" aborts and does NOT call _k2.run', async () => {
    const selfHostedMod = await import('../self-hosted.store');
    selfHostedMod.useSelfHostedStore.setState({
      tunnel: { uri: '', name: 'broken', country: '' },
      loaded: true,
    });

    const { useConnectionStore, vpn } = await getStores();
    mockRun.mockResolvedValue({ code: 0 });

    useConnectionStore.setState({ serverMode: 'self_hosted' });

    await useConnectionStore.getState().connect();

    expect(mockRun).not.toHaveBeenCalledWith('up', expect.anything());
    expect(vpn.useVPNMachineStore.getState().state).toBe('idle');
  });
});

// ==================== VPN State Lifecycle Tests ====================

describe('Connection Store - VPN State Lifecycle', () => {
  it('clears connectedTunnel when VPN transitions to idle (daemon restart)', async () => {
    const { useConnectionStore, vpn } = await getStores();

    // Simulate: user was connected
    vpn.dispatch('USER_CONNECT');
    vpn.dispatch('BACKEND_CONNECTED');
    useConnectionStore.setState({
      connectedTunnel: {
        source: 'cloud',
        domain: 'tokyo.example.com',
        name: 'Tokyo',
        country: 'JP',
        serverUrl: 'k2v5://tokyo.example.com:443',
      },
    });

    // Initialize lifecycle subscription
    const { initializeConnectionStore } = await import('../connection.store');
    const cleanup = initializeConnectionStore();

    // Simulate: daemon restart → backend reports disconnected
    vpn.dispatch('BACKEND_DISCONNECTED');
    expect(vpn.useVPNMachineStore.getState().state).toBe('idle');

    // connectedTunnel should be cleared
    expect(useConnectionStore.getState().connectedTunnel).toBeNull();

    cleanup();
  });

  it('clears connectedTunnel when BACKEND_ERROR transitions to idle (was error state)', async () => {
    const { useConnectionStore, vpn } = await getStores();

    vpn.dispatch('USER_CONNECT');
    vpn.dispatch('BACKEND_CONNECTED');
    useConnectionStore.setState({
      connectedTunnel: {
        source: 'cloud',
        domain: 'tokyo.example.com',
        name: 'Tokyo',
        country: 'JP',
        serverUrl: 'k2v5://tokyo.example.com:443',
      },
    });

    const { initializeConnectionStore } = await import('../connection.store');
    const cleanup = initializeConnectionStore();

    // connected → idle via BACKEND_ERROR (clears connectedTunnel)
    vpn.dispatch('BACKEND_ERROR', { error: { code: 570, message: 'fatal' } });
    expect(vpn.useVPNMachineStore.getState().state).toBe('idle');

    expect(useConnectionStore.getState().connectedTunnel).toBeNull();

    cleanup();
  });

  it('clears connectedTunnel when VPN transitions to idle from serviceDown recovery', async () => {
    const { useConnectionStore, vpn } = await getStores();

    vpn.dispatch('USER_CONNECT');
    vpn.dispatch('BACKEND_CONNECTED');
    useConnectionStore.setState({
      connectedTunnel: {
        source: 'cloud',
        domain: 'tokyo.example.com',
        name: 'Tokyo',
        country: 'JP',
        serverUrl: 'k2v5://tokyo.example.com:443',
      },
    });

    const { initializeConnectionStore } = await import('../connection.store');
    const cleanup = initializeConnectionStore();

    // Daemon crash → serviceDown → daemon restart → idle
    vpn.dispatch('SERVICE_UNREACHABLE');
    expect(useConnectionStore.getState().connectedTunnel).not.toBeNull(); // still set during serviceDown

    vpn.dispatch('SERVICE_REACHABLE');
    expect(vpn.useVPNMachineStore.getState().state).toBe('idle');
    expect(useConnectionStore.getState().connectedTunnel).toBeNull();

    cleanup();
  });

  it('clears connectedTunnel when BACKEND_ERROR transitions to idle', async () => {
    const { useConnectionStore, vpn } = await getStores();

    vpn.dispatch('USER_CONNECT');
    vpn.dispatch('BACKEND_CONNECTED');
    const tunnel = {
      source: 'cloud' as const,
      domain: 'tokyo.example.com',
      name: 'Tokyo',
      country: 'JP',
      serverUrl: 'k2v5://tokyo.example.com:443',
    };
    useConnectionStore.setState({ connectedTunnel: tunnel });

    const { initializeConnectionStore } = await import('../connection.store');
    const cleanup = initializeConnectionStore();

    // connected → idle via BACKEND_ERROR (clears connectedTunnel)
    vpn.dispatch('BACKEND_ERROR', { error: { code: 570, message: 'fatal' } });
    expect(vpn.useVPNMachineStore.getState().state).toBe('idle');
    expect(useConnectionStore.getState().connectedTunnel).toBeNull();

    cleanup();
  });

  it('is idempotent with user-initiated disconnect (connectedTunnel already null)', async () => {
    const { useConnectionStore, vpn } = await getStores();
    mockRun.mockResolvedValue({ code: 0 });

    vpn.dispatch('USER_CONNECT');
    vpn.dispatch('BACKEND_CONNECTED');
    useConnectionStore.setState({
      connectedTunnel: {
        source: 'cloud',
        domain: 'tokyo.example.com',
        name: 'Tokyo',
        country: 'JP',
        serverUrl: 'k2v5://tokyo.example.com:443',
      },
    });

    const { initializeConnectionStore } = await import('../connection.store');
    const cleanup = initializeConnectionStore();

    // User-initiated disconnect clears connectedTunnel first
    await useConnectionStore.getState().disconnect();
    expect(useConnectionStore.getState().connectedTunnel).toBeNull();

    // VPN transitions to idle — subscription fires but connectedTunnel is already null (no-op)
    vpn.dispatch('BACKEND_DISCONNECTED');
    expect(useConnectionStore.getState().connectedTunnel).toBeNull();

    cleanup();
  });

  it('cleanup function unsubscribes from VPN state changes', async () => {
    const { useConnectionStore, vpn } = await getStores();

    vpn.dispatch('USER_CONNECT');
    vpn.dispatch('BACKEND_CONNECTED');
    const tunnel = {
      source: 'cloud' as const,
      domain: 'tokyo.example.com',
      name: 'Tokyo',
      country: 'JP',
      serverUrl: 'k2v5://tokyo.example.com:443',
    };
    useConnectionStore.setState({ connectedTunnel: tunnel });

    const { initializeConnectionStore } = await import('../connection.store');
    const cleanup = initializeConnectionStore();
    cleanup(); // unsubscribe

    // After cleanup, VPN going idle should NOT clear connectedTunnel
    vpn.dispatch('BACKEND_DISCONNECTED');
    expect(useConnectionStore.getState().connectedTunnel).toEqual(tunnel);
  });

  it('clears connectedTunnel when VPN transitions to idle from connecting (fast auth rejection)', async () => {
    const { useConnectionStore, vpn } = await getStores();

    // connect() sets connectedTunnel before dispatching USER_CONNECT
    vpn.dispatch('USER_CONNECT');
    useConnectionStore.setState({
      connectedTunnel: {
        source: 'cloud',
        domain: 'tokyo.example.com',
        name: 'Tokyo',
        country: 'JP',
        serverUrl: 'k2v5://tokyo.example.com:443',
      },
    });
    expect(vpn.useVPNMachineStore.getState().state).toBe('connecting');

    const { initializeConnectionStore } = await import('../connection.store');
    const cleanup = initializeConnectionStore();

    // Backend immediately rejects → idle
    vpn.dispatch('BACKEND_DISCONNECTED');
    expect(vpn.useVPNMachineStore.getState().state).toBe('idle');
    expect(useConnectionStore.getState().connectedTunnel).toBeNull();

    cleanup();
  });

  it('clears connectedTunnel when VPN transitions to idle from reconnecting', async () => {
    const { useConnectionStore, vpn } = await getStores();
    vi.useFakeTimers();

    vpn.dispatch('USER_CONNECT');
    vpn.dispatch('BACKEND_CONNECTED');
    vpn.dispatch('BACKEND_RECONNECTING');
    vi.advanceTimersByTime(3000); // let debounce fire
    expect(vpn.useVPNMachineStore.getState().state).toBe('reconnecting');

    useConnectionStore.setState({
      connectedTunnel: {
        source: 'cloud',
        domain: 'tokyo.example.com',
        name: 'Tokyo',
        country: 'JP',
        serverUrl: 'k2v5://tokyo.example.com:443',
      },
    });

    const { initializeConnectionStore } = await import('../connection.store');
    const cleanup = initializeConnectionStore();

    // Reconnect fails permanently → idle
    vpn.dispatch('BACKEND_DISCONNECTED');
    expect(vpn.useVPNMachineStore.getState().state).toBe('idle');
    expect(useConnectionStore.getState().connectedTunnel).toBeNull();

    cleanup();
    vi.useRealTimers();
  });

  it('handles rapid state cycling (idle → connecting → idle → connecting → idle)', async () => {
    const { useConnectionStore, vpn } = await getStores();

    const tunnel = {
      source: 'cloud' as const,
      domain: 'tokyo.example.com',
      name: 'Tokyo',
      country: 'JP',
      serverUrl: 'k2v5://tokyo.example.com:443',
    };

    const { initializeConnectionStore } = await import('../connection.store');
    const cleanup = initializeConnectionStore();

    // Cycle 1: connect attempt fails immediately
    useConnectionStore.setState({ connectedTunnel: tunnel });
    vpn.dispatch('USER_CONNECT');
    vpn.dispatch('BACKEND_DISCONNECTED');
    expect(useConnectionStore.getState().connectedTunnel).toBeNull();

    // Cycle 2: another connect attempt fails
    useConnectionStore.setState({ connectedTunnel: tunnel });
    vpn.dispatch('USER_CONNECT');
    vpn.dispatch('BACKEND_DISCONNECTED');
    expect(useConnectionStore.getState().connectedTunnel).toBeNull();

    // Cycle 3: connect succeeds — connectedTunnel should persist
    useConnectionStore.setState({ connectedTunnel: tunnel });
    vpn.dispatch('USER_CONNECT');
    vpn.dispatch('BACKEND_CONNECTED');
    expect(useConnectionStore.getState().connectedTunnel).toEqual(tunnel);

    cleanup();
  });

  it('tunnel selection works after unexpected disconnect (end-to-end bug scenario)', async () => {
    const { useConnectionStore, vpn } = await getStores();

    // Phase 1: User connects to tunnel A
    const tunnelA = {
      id: 1, domain: 'www.yunnan.people.cn', name: 'AU 3187',
      protocol: 'k2v5', port: 443, serverUrl: 'k2v5://yunnan:443',
      node: { country: 'AU' },
    } as any;
    useConnectionStore.getState().selectCloudTunnel(tunnelA);

    vpn.dispatch('USER_CONNECT');
    vpn.dispatch('BACKEND_CONNECTED');
    useConnectionStore.setState({
      connectedTunnel: useConnectionStore.getState().activeTunnel,
    });

    const { initializeConnectionStore } = await import('../connection.store');
    const cleanup = initializeConnectionStore();

    // Phase 2: Daemon restarts (sleep/wake) → VPN goes idle
    vpn.dispatch('BACKEND_DISCONNECTED');
    expect(vpn.useVPNMachineStore.getState().state).toBe('idle');
    expect(useConnectionStore.getState().connectedTunnel).toBeNull();

    // Phase 3: User clicks tunnel B
    const tunnelB = {
      id: 2, domain: 'www.jiangxi.people.cn', name: 'AU 3188',
      protocol: 'k2v5', port: 443, serverUrl: 'k2v5://jiangxi:443',
      node: { country: 'AU' },
    } as any;
    useConnectionStore.getState().selectCloudTunnel(tunnelB);

    // displayTunnel = connectedTunnel ?? activeTunnel — should now be tunnel B
    const { connectedTunnel, activeTunnel } = useConnectionStore.getState();
    const displayTunnel = connectedTunnel ?? activeTunnel;
    expect(displayTunnel?.domain).toBe('www.jiangxi.people.cn');

    cleanup();
  });
});

// ==================== Smart → Manual migration Tests ====================

describe('smart → manual migration', () => {
  it('coerces persisted serverMode="smart" to "manual"', async () => {
    mockStorage.get.mockImplementation(async (key: string) => {
      if (key === 'k2.vpn.server_mode') return 'smart';
      return undefined;
    });
    const connMod = await import('../connection.store');
    await connMod.useConnectionStore.getState().loadServerMode();
    expect(connMod.useConnectionStore.getState().serverMode).toBe('manual');
  });

  it('preserves persisted "self_hosted"', async () => {
    mockStorage.get.mockImplementation(async (key: string) => {
      if (key === 'k2.vpn.server_mode') return 'self_hosted';
      return undefined;
    });
    const connMod = await import('../connection.store');
    await connMod.useConnectionStore.getState().loadServerMode();
    expect(connMod.useConnectionStore.getState().serverMode).toBe('self_hosted');
  });

  it('defaults missing value to "manual"', async () => {
    mockStorage.get.mockResolvedValue(undefined);
    const connMod = await import('../connection.store');
    await connMod.useConnectionStore.getState().loadServerMode();
    expect(connMod.useConnectionStore.getState().serverMode).toBe('manual');
  });

  it('stops reading smart_country (key no longer used)', async () => {
    mockStorage.get.mockResolvedValue(undefined);
    const connMod = await import('../connection.store');
    await connMod.useConnectionStore.getState().loadServerMode();
    const keys = mockStorage.get.mock.calls.map((c) => c[0]);
    expect(keys).toContain('k2.vpn.server_mode');
    expect(keys).not.toContain('k2.vpn.smart_country');
  });
});

// ==================== connect() Auto resolution Tests ====================

describe('connect() resolves Auto via pickAutoTunnel', () => {
  afterEach(async () => {
    const { cacheStore } = await import('../../services/cache-store');
    cacheStore.clear();
  });

  it('dispatches BACKEND_ERROR (code 572) when cacheStore is empty (Auto + no list)', async () => {
    const { useConnectionStore, vpn } = await getStores();
    mockRun.mockResolvedValue({ code: 0 });

    // Default state: manual mode, no selection (Auto sentinel in effect)
    // cacheStore has no entry → pickAutoTunnel receives [] → returns null

    await useConnectionStore.getState().connect();

    // _k2.run('up') must NOT be called
    expect(mockRun).not.toHaveBeenCalledWith('up', expect.anything());
    // VPN machine should reflect the error (idle + error set)
    expect(vpn.useVPNMachineStore.getState().state).toBe('idle');
    expect(vpn.useVPNMachineStore.getState().error).not.toBeNull();
    expect(vpn.useVPNMachineStore.getState().error?.code).toBe(572);
  });

  it('resolves Auto via pickAutoTunnel and proceeds to call _k2.run(up)', async () => {
    const { useConnectionStore } = await getStores();
    mockRun.mockResolvedValue({ code: 0 });

    const { authService } = await import('../../services/auth-service');
    vi.mocked(authService.buildTunnelUrl).mockResolvedValue('k2v5://u:t@auto.example.com:443');

    // Put a tunnel into cacheStore so Auto resolution succeeds
    const { cacheStore } = await import('../../services/cache-store');
    const tunnel = {
      id: 42,
      domain: 'auto.example.com',
      name: 'Auto Node',
      protocol: 'k2v5',
      port: 443,
      serverUrl: 'k2v5://auto.example.com:443',
      recommendScore: 0.8,
      node: { country: 'SG' },
    } as any;
    cacheStore.set('api:tunnels', { items: [tunnel] });

    // Manual mode, no selectedCloudTunnel → triggers Auto path
    await useConnectionStore.getState().connect();

    expect(mockRun).toHaveBeenCalledWith('up', expect.objectContaining({
      config: expect.objectContaining({ mode: 'tun' }),
    }));
    const upCall = mockRun.mock.calls.find((c: any[]) => c[0] === 'up');
    const routes = upCall?.[1]?.config?.routes as Array<{ via: string }>;
    expect(routes.some((r: { via: string }) => r.via === 'k2v5://u:t@auto.example.com:443')).toBe(true);

    // connectedTunnel snapshot uses the auto-picked tunnel's domain
    const state = useConnectionStore.getState();
    expect(state.connectedTunnel?.domain).toBe('auto.example.com');

    // selectedCloudTunnel remains null (re-pick on next connect = decision #2)
    expect(state.selectedCloudTunnel).toBeNull();
  });

  it('connectedTunnel snapshot carries ipv4 from resolved Auto pick', async () => {
    const { useConnectionStore } = await getStores();
    mockRun.mockResolvedValue({ code: 0 });

    const { authService } = await import('../../services/auth-service');
    vi.mocked(authService.buildTunnelUrl).mockResolvedValue('k2v5://u:t@auto.example.com:443');

    const { cacheStore } = await import('../../services/cache-store');
    const tunnel = {
      id: 43,
      domain: 'auto.example.com',
      name: 'Auto Node',
      protocol: 'k2v5',
      port: 443,
      serverUrl: 'k2v5://auto.example.com:443',
      recommendScore: 0.9,
      node: { country: 'SG', ipv4: '1.2.3.4', region: '', name: '', ipv6: '', isAlive: true, load: 0, trafficUsagePercent: 0, bandwidthUsagePercent: 0 },
    } as any;
    cacheStore.set('api:tunnels', { items: [tunnel] });

    // Manual mode + no selectedCloudTunnel → Auto resolve
    await useConnectionStore.getState().connect();

    const { connectedTunnel } = useConnectionStore.getState();
    expect(connectedTunnel?.ipv4).toBe('1.2.3.4');
    expect(connectedTunnel?.country).toBe('SG');
  });

  it('refreshes tunnel cache after Auto pick once VPN reaches a stable state', async () => {
    vi.useFakeTimers();
    try {
      const { useConnectionStore, vpn } = await getStores();
      mockRun.mockResolvedValue({ code: 0 });

      const { authService } = await import('../../services/auth-service');
      vi.mocked(authService.buildTunnelUrl).mockResolvedValue('k2v5://u:t@auto.example.com:443');

      const { cacheStore } = await import('../../services/cache-store');
      const tunnel = {
        id: 44,
        domain: 'auto.example.com',
        name: 'Auto Node',
        protocol: 'k2v5',
        port: 443,
        serverUrl: 'k2v5://auto.example.com:443',
        recommendScore: 0.7,
        node: { country: 'SG' },
      } as any;
      cacheStore.set('api:tunnels', { items: [tunnel] });

      const refreshedItems = [{ ...tunnel, recommendScore: 0.95 }];
      mockCloudApiGet.mockResolvedValue({ code: 0, message: 'ok', data: { items: refreshedItems } });

      await useConnectionStore.getState().connect();

      // After connect() returns, machine is in 'connecting'. Poll should hold.
      await vi.advanceTimersByTimeAsync(3000);
      expect(mockCloudApiGet).not.toHaveBeenCalled();

      // Simulate connect completing — now stable.
      vpn.useVPNMachineStore.setState({ state: 'connected' });
      await vi.advanceTimersByTimeAsync(1000);

      expect(mockCloudApiGet).toHaveBeenCalledWith('/api/tunnels/k2v4');
      const refreshed = cacheStore.get<any>('api:tunnels');
      expect(refreshed?.items?.[0]?.recommendScore).toBe(0.95);
    } finally {
      vi.useRealTimers();
    }
  });

  it('refreshes when VPN ends in idle (connect failure path)', async () => {
    vi.useFakeTimers();
    try {
      const { useConnectionStore, vpn } = await getStores();
      mockRun.mockResolvedValue({ code: 0 });

      const { authService } = await import('../../services/auth-service');
      vi.mocked(authService.buildTunnelUrl).mockResolvedValue('k2v5://u:t@auto.example.com:443');

      const { cacheStore } = await import('../../services/cache-store');
      cacheStore.set('api:tunnels', {
        items: [{
          id: 44, domain: 'auto.example.com', name: 'Auto', protocol: 'k2v5', port: 443,
          serverUrl: 'k2v5://auto.example.com:443', recommendScore: 0.7, node: { country: 'SG' },
        }],
      } as any);

      await useConnectionStore.getState().connect();
      // Simulate failure → state lands at idle.
      vpn.useVPNMachineStore.setState({ state: 'idle' });
      await vi.advanceTimersByTimeAsync(1000);

      expect(mockCloudApiGet).toHaveBeenCalledWith('/api/tunnels/k2v4');
    } finally {
      vi.useRealTimers();
    }
  });

  it('gives up tunnel refresh if VPN never stabilizes within max wait', async () => {
    vi.useFakeTimers();
    try {
      const { useConnectionStore } = await getStores();
      mockRun.mockResolvedValue({ code: 0 });

      const { authService } = await import('../../services/auth-service');
      vi.mocked(authService.buildTunnelUrl).mockResolvedValue('k2v5://u:t@auto.example.com:443');

      const { cacheStore } = await import('../../services/cache-store');
      cacheStore.set('api:tunnels', {
        items: [{
          id: 44, domain: 'auto.example.com', name: 'Auto', protocol: 'k2v5', port: 443,
          serverUrl: 'k2v5://auto.example.com:443', recommendScore: 0.7, node: { country: 'SG' },
        }],
      } as any);

      await useConnectionStore.getState().connect();
      // Stay in 'connecting' the whole time. Advance past the 30s cap.
      await vi.advanceTimersByTimeAsync(35_000);

      expect(mockCloudApiGet).not.toHaveBeenCalled();
    } finally {
      vi.useRealTimers();
    }
  });

  it('does not trigger tunnel refresh when a concrete tunnel is selected', async () => {
    vi.useFakeTimers();
    try {
      const { useConnectionStore, vpn } = await getStores();
      mockRun.mockResolvedValue({ code: 0 });

      const { authService } = await import('../../services/auth-service');
      vi.mocked(authService.buildTunnelUrl).mockResolvedValue('k2v5://u:t@manual.example.com:443');

      const tunnel = {
        id: 45,
        domain: 'manual.example.com',
        name: 'Manual Node',
        protocol: 'k2v5',
        port: 443,
        serverUrl: 'k2v5://manual.example.com:443',
        recommendScore: 0.5,
        node: { country: 'JP' },
      } as any;
      useConnectionStore.getState().selectCloudTunnel(tunnel);

      await useConnectionStore.getState().connect();
      // Even after VPN reaches a stable state, no refresh should fire.
      vpn.useVPNMachineStore.setState({ state: 'connected' });
      await vi.advanceTimersByTimeAsync(35_000);

      expect(mockCloudApiGet).not.toHaveBeenCalled();
    } finally {
      vi.useRealTimers();
    }
  });
});

// ==================== serverMode = k2sub Tests ====================

describe('serverMode = k2sub (gateway)', () => {
  beforeEach(() => {
    (window as any)._platform = {
      os: 'linux' as const,
      version: '0.4.4',
      storage: mockStorage,
      platformType: 'gateway',
    };
  });

  it('loadServerMode pins to k2sub on gateway when no stored value', async () => {
    mockStorage.get.mockResolvedValue(undefined);
    const connMod = await import('../connection.store');
    await connMod.useConnectionStore.getState().loadServerMode();
    expect(connMod.useConnectionStore.getState().serverMode).toBe('k2sub');
  });

  it('loadServerMode preserves stored "self_hosted" on gateway', async () => {
    mockStorage.get.mockImplementation(async (key: string) => {
      if (key === 'k2.vpn.server_mode') return 'self_hosted';
      return undefined;
    });
    const connMod = await import('../connection.store');
    await connMod.useConnectionStore.getState().loadServerMode();
    expect(connMod.useConnectionStore.getState().serverMode).toBe('self_hosted');
  });

  it('setSubsCountry persists country', async () => {
    const connMod = await import('../connection.store');
    await connMod.useConnectionStore.getState().setSubsCountry('jp');
    expect(connMod.useConnectionStore.getState().subsCountry).toBe('jp');
    expect(mockStorage.set).toHaveBeenCalledWith('k2.connection.subsCountry', 'jp');
  });

  it('setSubsCountry(null) removes from storage', async () => {
    const connMod = await import('../connection.store');
    await connMod.useConnectionStore.getState().setSubsCountry(null);
    expect(connMod.useConnectionStore.getState().subsCountry).toBeNull();
    expect(mockStorage.remove).toHaveBeenCalledWith('k2.connection.subsCountry');
  });

  it('connect with k2sub mode builds k2subs URL via buildSubsUrl', async () => {
    const { useConnectionStore } = await getStores();
    mockRun.mockResolvedValue({ code: 0 });

    const { authService } = await import('../../services/auth-service');
    vi.mocked(authService.buildSubsUrl).mockResolvedValue('k2subs://UDID:T@a.example/api/subs?country=jp');

    useConnectionStore.setState({ serverMode: 'k2sub', subsCountry: 'jp' });
    await useConnectionStore.getState().connect();

    expect(authService.buildSubsUrl).toHaveBeenCalledWith('jp');
    const upCall = mockRun.mock.calls.find(c => c[0] === 'up');
    expect(upCall).toBeDefined();
    const routes = upCall?.[1]?.config?.routes as Array<{ via: string }>;
    expect(routes.some(r => r.via === 'k2subs://UDID:T@a.example/api/subs?country=jp')).toBe(true);
  });
});

// ==================== hasConnectableSelection ====================

describe('hasConnectableSelection predicate', () => {
  it('manual + Auto sentinel default: true', async () => {
    const { hasConnectableSelection } = await import('../connection.store');
    expect(hasConnectableSelection({
      serverMode: 'manual',
      activeTunnel: null,
      selectedCloudTunnel: null,
    })).toBe(true);
  });

  it('manual + concrete tunnel: true', async () => {
    const { hasConnectableSelection } = await import('../connection.store');
    expect(hasConnectableSelection({
      serverMode: 'manual',
      activeTunnel: { source: 'cloud', domain: 'd', name: 'n', country: 'JP', serverUrl: 'k2v5://x', ipv4: '' },
      selectedCloudTunnel: { domain: 'd' } as any,
    })).toBe(true);
  });

  it('self_hosted with tunnel: true', async () => {
    const { hasConnectableSelection } = await import('../connection.store');
    expect(hasConnectableSelection({
      serverMode: 'self_hosted',
      activeTunnel: { source: 'self_hosted', domain: 'd', name: 'n', country: '', serverUrl: 'k2v5://x', ipv4: '' },
      selectedCloudTunnel: null,
    })).toBe(true);
  });

  it('self_hosted without tunnel: false', async () => {
    const { hasConnectableSelection } = await import('../connection.store');
    expect(hasConnectableSelection({
      serverMode: 'self_hosted',
      activeTunnel: null,
      selectedCloudTunnel: null,
    })).toBe(false);
  });

  it('k2sub with no country (Auto): true', async () => {
    const { hasConnectableSelection } = await import('../connection.store');
    expect(hasConnectableSelection({
      serverMode: 'k2sub',
      activeTunnel: null,
      selectedCloudTunnel: null,
    })).toBe(true);
  });

  it('k2sub with country selected: true', async () => {
    const { hasConnectableSelection } = await import('../connection.store');
    // subsCountry is independent of activeTunnel/selectedCloudTunnel — k2sub
    // is always ready regardless of those fields.
    expect(hasConnectableSelection({
      serverMode: 'k2sub',
      activeTunnel: null,
      selectedCloudTunnel: null,
    })).toBe(true);
  });
});

// ==================== enrichFromTunnelList Tests ====================

describe('enrichFromTunnelList preserves Auto', () => {
  it('does not overwrite selectedCloudTunnel when null (Auto)', async () => {
    const { useConnectionStore } = await getStores();

    // Arrange: cold-start — VPN is up (cloud source) but country not yet populated
    useConnectionStore.setState({
      selectedCloudTunnel: null, // Auto mode
      connectedTunnel: {
        source: 'cloud',
        domain: 'tokyo.kaitu.io',
        name: '',
        country: '',
        load: 0,
      } as any,
    });

    const tunnels = [
      {
        id: 1,
        domain: 'tokyo.kaitu.io',
        name: 'Tokyo',
        protocol: 'k2v5',
        port: 443,
        serverUrl: 'k2v5://tokyo.kaitu.io',
        node: {
          name: 'tokyo',
          country: 'JP',
          region: 'Tokyo',
          ipv4: '1.1.1.1',
          ipv6: '',
          isAlive: true,
          load: 50,
          trafficUsagePercent: 0,
          bandwidthUsagePercent: 0,
        },
        recommendScore: 0.7,
      },
    ] as any[];

    // Act
    useConnectionStore.getState().enrichFromTunnelList(tunnels);

    // Assert
    const s = useConnectionStore.getState();
    expect(s.connectedTunnel?.country).toBe('JP'); // enrichment applied
    expect(s.selectedCloudTunnel).toBeNull();       // Auto preserved
  });
});
