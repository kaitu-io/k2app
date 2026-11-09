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

// Funnel: connect() reports first_connect_attempt through statsService.
const mockTrackFunnelOnce = vi.fn();
vi.mock('../../services/stats', () => ({
  statsService: {
    trackFunnel: vi.fn(),
    trackFunnelOnce: (...args: unknown[]) => mockTrackFunnelOnce(...args),
    trackAppOpen: vi.fn(),
    trackConnect: vi.fn(),
    trackDisconnect: vi.fn(),
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
  mockTrackFunnelOnce.mockReset();
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

  it('user-initiated connect reports first_connect_attempt (once-variant); a rejected connect does not', async () => {
    const { useConnectionStore, vpn } = await getStores();
    mockRun.mockResolvedValue({ code: 0 });
    const { authService } = await import('../../services/auth-service');
    vi.mocked(authService.buildTunnelUrl).mockResolvedValue('k2v5://u:t@tokyo.example.com:443');
    useConnectionStore.getState().selectCloudTunnel({
      id: 1, domain: 'tokyo.example.com', name: 'Tokyo', protocol: 'k2v5',
      port: 443, serverUrl: 'k2v5://tokyo.example.com:443', node: { country: 'JP' },
    } as any);

    // Already connecting -> connect() is rejected before USER_CONNECT: no attempt reported.
    vpn.useVPNMachineStore.setState({ state: 'connecting' });
    await useConnectionStore.getState().connect();
    await new Promise((r) => setTimeout(r, 0));
    expect(mockTrackFunnelOnce).not.toHaveBeenCalled();

    vpn.useVPNMachineStore.setState({ state: 'idle' });
    await useConnectionStore.getState().connect();
    await new Promise((r) => setTimeout(r, 0));
    expect(mockTrackFunnelOnce).toHaveBeenCalledWith('first_connect_attempt');
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

      expect(mockCloudApiGet).toHaveBeenCalledWith('/api/v20260717/tunnels');
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

      expect(mockCloudApiGet).toHaveBeenCalledWith('/api/v20260717/tunnels');
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

// ==================== excludedCountries ====================

describe('excludedCountries', () => {
  it('setExcludedCountries normalizes to lowercase, dedupes, sorts, and persists JSON', async () => {
    const connMod = await import('../connection.store');
    await connMod.useConnectionStore.getState().setExcludedCountries(['HK', 'hk', 'JP']);
    expect(connMod.useConnectionStore.getState().excludedCountries).toEqual(['hk', 'jp']);
    expect(mockStorage.set).toHaveBeenCalledWith(
      'k2.connection.excludedCountries',
      JSON.stringify(['hk', 'jp']),
    );
  });

  it('setExcludedCountries([]) removes the storage key', async () => {
    const connMod = await import('../connection.store');
    await connMod.useConnectionStore.getState().setExcludedCountries([]);
    expect(connMod.useConnectionStore.getState().excludedCountries).toEqual([]);
    expect(mockStorage.remove).toHaveBeenCalledWith('k2.connection.excludedCountries');
  });

  it('loadExcludedCountries parses persisted JSON', async () => {
    mockStorage.get.mockResolvedValue(JSON.stringify(['hk', 'kr']));
    const connMod = await import('../connection.store');
    await connMod.useConnectionStore.getState().loadExcludedCountries();
    expect(connMod.useConnectionStore.getState().excludedCountries).toEqual(['hk', 'kr']);
    expect(connMod.useConnectionStore.getState().excludedCountriesLoaded).toBe(true);
  });

  it('loadExcludedCountries falls back to [] on corrupted JSON', async () => {
    mockStorage.get.mockResolvedValue('not-json{{');
    const connMod = await import('../connection.store');
    await connMod.useConnectionStore.getState().loadExcludedCountries();
    expect(connMod.useConnectionStore.getState().excludedCountries).toEqual([]);
    expect(connMod.useConnectionStore.getState().excludedCountriesLoaded).toBe(true);
  });

  it('loadExcludedCountries falls back to [] when nothing persisted', async () => {
    mockStorage.get.mockResolvedValue(null);
    const connMod = await import('../connection.store');
    await connMod.useConnectionStore.getState().loadExcludedCountries();
    expect(connMod.useConnectionStore.getState().excludedCountries).toEqual([]);
    expect(connMod.useConnectionStore.getState().excludedCountriesLoaded).toBe(true);
  });

  it('connect() dispatches 573 when the auto-pick pool is emptied by the country filter', async () => {
    const { useConnectionStore, vpn } = await getStores();
    mockRun.mockResolvedValue({ code: 0 });

    const { cacheStore } = await import('../../services/cache-store');
    const tunnel = {
      id: 50,
      domain: 'hk.example.com',
      name: 'HK Node',
      protocol: 'k2v5',
      port: 443,
      serverUrl: 'k2v5://hk.example.com:443',
      recommendScore: 0.8,
      node: { country: 'HK' },
    } as any;
    cacheStore.set('api:tunnels', { items: [tunnel] });

    await useConnectionStore.getState().setExcludedCountries(['hk']);
    await useConnectionStore.getState().connect();

    // _k2.run('up') must NOT be called
    expect(mockRun).not.toHaveBeenCalledWith('up', expect.anything());
    expect(vpn.useVPNMachineStore.getState().state).toBe('idle');
    expect(vpn.useVPNMachineStore.getState().error?.code).toBe(573);
  });

  it('connect() dispatches 572 when the tunnel list is genuinely empty regardless of filter', async () => {
    const { useConnectionStore, vpn } = await getStores();
    mockRun.mockResolvedValue({ code: 0 });

    // cacheStore has no entry → tunnelList = [] (filter is irrelevant to an empty list)
    await useConnectionStore.getState().setExcludedCountries(['hk']);
    await useConnectionStore.getState().connect();

    expect(mockRun).not.toHaveBeenCalledWith('up', expect.anything());
    expect(vpn.useVPNMachineStore.getState().state).toBe('idle');
    expect(vpn.useVPNMachineStore.getState().error?.code).toBe(572);
  });
});

// ==================== serverMode = k2sub Tests ====================

describe('serverMode = k2sub', () => {
  beforeEach(() => {
    (window as any)._platform = {
      os: 'linux' as const,
      version: '0.4.4',
      storage: mockStorage,
    };
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

// ==================== Disconnect Feedback Gating Tests ====================

describe('Connection Store - Disconnect Feedback Gating', () => {
  function setupConnected(useConnectionStore: any, vpn: any, durationMs: number) {
    vpn.dispatch('USER_CONNECT');
    vpn.dispatch('BACKEND_CONNECTED');
    useConnectionStore.setState({
      connectedTunnel: {
        source: 'cloud',
        domain: 'test.com',
        name: 'Test',
        country: 'US',
        serverUrl: 'k2v5://test.com:443',
      },
      connectedAt: Date.now() - durationMs,
    });
  }

  it('does NOT set feedbackRequested when durationSec < 20', async () => {
    const { useConnectionStore, vpn } = await getStores();
    mockRun.mockResolvedValue({ code: 0 });
    setupConnected(useConnectionStore, vpn, 5_000); // 5s connection

    const infoSpy = vi.spyOn(console, 'info').mockImplementation(() => {});

    await useConnectionStore.getState().disconnect();

    const state = useConnectionStore.getState();
    expect(state.feedbackRequested).toBe(false);
    expect(state.lastConnectionInfo).toBeNull();
    expect(infoSpy).toHaveBeenCalledWith(
      expect.stringContaining('feedback dialog suppressed'),
    );
    infoSpy.mockRestore();
  });

  it('sets feedbackRequested when durationSec >= 20', async () => {
    const { useConnectionStore, vpn } = await getStores();
    mockRun.mockResolvedValue({ code: 0 });
    setupConnected(useConnectionStore, vpn, 25_000); // 25s connection

    await useConnectionStore.getState().disconnect();

    const state = useConnectionStore.getState();
    expect(state.feedbackRequested).toBe(true);
    expect(state.lastConnectionInfo).not.toBeNull();
    expect(state.lastConnectionInfo!.durationSec).toBeGreaterThanOrEqual(20);
  });

  it('does NOT set feedbackRequested when user is not authenticated (current behavior)', async () => {
    const { useConnectionStore, vpn } = await getStores();
    mockRun.mockResolvedValue({ code: 0 });
    setupConnected(useConnectionStore, vpn, 30_000);

    const authMod = await import('../auth.store');
    authMod.useAuthStore.setState({ isAuthenticated: false });

    await useConnectionStore.getState().disconnect();

    expect(useConnectionStore.getState().feedbackRequested).toBe(false);
  });

  // ===== startAt-preferred duration tests (cold-start regression guard) =====

  it('uses engine startAt when set, ignoring stale local connectedAt (cold-start case)', async () => {
    const { useConnectionStore, vpn } = await getStores();
    const { useVPNMachineStore } = vpn;
    mockRun.mockResolvedValue({ code: 0 });
    vpn.dispatch('USER_CONNECT');
    vpn.dispatch('BACKEND_CONNECTED');
    // Simulate post-cold-start: connectedTunnel restored, connectedAt still null,
    // engine reports session started 600s ago.
    useVPNMachineStore.setState({ startAt: Math.floor(Date.now() / 1000) - 600 });
    useConnectionStore.setState({
      connectedTunnel: {
        source: 'cloud',
        domain: 'test.com',
        name: 'Test',
        country: 'US',
        serverUrl: 'k2v5://test.com:443',
      },
      connectedAt: null,
    });

    await useConnectionStore.getState().disconnect();

    const info = useConnectionStore.getState().lastConnectionInfo;
    expect(info).not.toBeNull();
    expect(info!.durationSec).toBeGreaterThanOrEqual(595);
    expect(info!.durationSec).toBeLessThanOrEqual(605);
  });

  it('falls back to local connectedAt when engine startAt is null', async () => {
    const { useConnectionStore, vpn } = await getStores();
    const { useVPNMachineStore } = vpn;
    mockRun.mockResolvedValue({ code: 0 });
    vpn.dispatch('USER_CONNECT');
    vpn.dispatch('BACKEND_CONNECTED');
    useVPNMachineStore.setState({ startAt: null });
    setupConnected(useConnectionStore, vpn, 30_000);

    await useConnectionStore.getState().disconnect();

    const info = useConnectionStore.getState().lastConnectionInfo;
    expect(info!.durationSec).toBeGreaterThanOrEqual(28);
    expect(info!.durationSec).toBeLessThanOrEqual(32);
  });

  it('clamps to 0 when startAt is in the future (NTP backward jump guard)', async () => {
    const { useConnectionStore, vpn } = await getStores();
    const { useVPNMachineStore } = vpn;
    mockRun.mockResolvedValue({ code: 0 });
    vpn.dispatch('USER_CONNECT');
    vpn.dispatch('BACKEND_CONNECTED');
    useVPNMachineStore.setState({ startAt: Math.floor(Date.now() / 1000) + 60 });
    useConnectionStore.setState({
      connectedTunnel: {
        source: 'cloud',
        domain: 'test.com',
        name: 'Test',
        country: 'US',
        serverUrl: 'k2v5://test.com:443',
      },
      connectedAt: Date.now() - 25_000,
    });

    await useConnectionStore.getState().disconnect();

    const info = useConnectionStore.getState().lastConnectionInfo;
    // 25s connectedAt would normally pass the 20s gate, but startAt-future clamps to 0
    // → suppressed below MIN_FEEDBACK_DURATION_SEC.
    expect(info).toBeNull();
    expect(useConnectionStore.getState().feedbackRequested).toBe(false);
  });
});

// ==================== Cloud Access Revocation (402) Tests ====================

describe('Connection Store - cloud access revocation (402)', () => {
  it('defaults to cloudAccessRevoked=false', async () => {
    const { useConnectionStore } = await getStores();
    expect(useConnectionStore.getState().cloudAccessRevoked).toBe(false);
  });

  it('setCloudAccess(false) revokes: sets flag, clears selection, deletes tunnel cache', async () => {
    const { useConnectionStore } = await getStores();
    const { cacheStore } = await import('../../services/cache-store');
    cacheStore.set('api:tunnels', { items: [{ id: 1 }], echConfigList: undefined } as any);

    useConnectionStore.setState({
      selectedCloudTunnel: { domain: 'tokyo.example.com', serverUrl: 'https://x' } as any,
      activeTunnel: { source: 'cloud', domain: 'tokyo.example.com' } as any,
      cloudAccessRevoked: false,
    });

    useConnectionStore.getState().setCloudAccess(false);

    const s = useConnectionStore.getState();
    expect(s.cloudAccessRevoked).toBe(true);
    expect(s.selectedCloudTunnel).toBeNull();
    expect(s.activeTunnel).toBeNull();
    expect(cacheStore.get('api:tunnels')).toBeNull();
  });

  it('setCloudAccess(true) clears the revoked flag (membership restored)', async () => {
    const { useConnectionStore } = await getStores();
    useConnectionStore.setState({ cloudAccessRevoked: true });

    useConnectionStore.getState().setCloudAccess(true);

    expect(useConnectionStore.getState().cloudAccessRevoked).toBe(false);
  });

  it('hasConnectableSelection: manual + revoked → false (connect button disabled)', async () => {
    const { hasConnectableSelection } = await getStores();
    expect(
      hasConnectableSelection({
        serverMode: 'manual',
        activeTunnel: null,
        selectedCloudTunnel: null,
        cloudAccessRevoked: true,
      }),
    ).toBe(false);
  });

  it('hasConnectableSelection: manual + not revoked → Auto still connectable', async () => {
    const { hasConnectableSelection } = await getStores();
    expect(
      hasConnectableSelection({
        serverMode: 'manual',
        activeTunnel: null,
        selectedCloudTunnel: null,
        cloudAccessRevoked: false,
      }),
    ).toBe(true);
  });

  it('hasConnectableSelection: self_hosted unaffected by revoked flag', async () => {
    const { hasConnectableSelection } = await getStores();
    expect(
      hasConnectableSelection({
        serverMode: 'self_hosted',
        activeTunnel: { source: 'self_hosted', domain: 'x' } as any,
        selectedCloudTunnel: null,
        cloudAccessRevoked: true,
      }),
    ).toBe(true);
  });
});

// ==================== Stale `up` Response Tests ====================

/**
 * Sibling of the stale-status race in vpn-machine.store: `_k2.run('up')` can
 * outlive the intent that started it. If the user disconnects and reconnects
 * while it is in flight, its (failed) result describes a dead attempt — letting
 * it dispatch BACKEND_ERROR tears down the *live* attempt's UI state.
 */
describe('Connection Store - stale up() response', () => {
  function selectTunnel(store: any) {
    store.getState().selectCloudTunnel({
      id: 1, domain: 'tokyo.example.com', name: 'Tokyo', protocol: 'k2v5',
      port: 443, serverUrl: 'k2v5://tokyo.example.com:443', node: { country: 'JP' },
    } as any);
  }

  it('up() failing after the epoch moved does not kill the newer attempt', async () => {
    const { useConnectionStore, vpn } = await getStores();
    const { authService } = await import('../../services/auth-service');
    vi.mocked(authService.buildTunnelUrl).mockResolvedValue('k2v5://u:t@tokyo.example.com:443');

    // One resolver per `up` call — the second connect must not steal the first's.
    const upResolvers: Array<(v: any) => void> = [];
    mockRun.mockImplementation((action: string) => {
      if (action === 'up') return new Promise((r) => { upResolvers.push(r); });
      return Promise.resolve({ code: 0 });
    });

    selectTunnel(useConnectionStore);
    const firstConnect = useConnectionStore.getState().connect();
    // Let connect() get past its awaits and issue `up`.
    await Promise.resolve(); await Promise.resolve(); await Promise.resolve();
    expect(vpn.useVPNMachineStore.getState().state).toBe('connecting');

    // User disconnects (epoch++) and immediately reconnects (epoch++).
    await useConnectionStore.getState().disconnect();
    vpn.dispatch('BACKEND_DISCONNECTED'); // backend confirms → idle
    useConnectionStore.getState().connect();
    await Promise.resolve(); await Promise.resolve(); await Promise.resolve();
    expect(vpn.useVPNMachineStore.getState().state).toBe('connecting');

    expect(upResolvers.length).toBe(2);

    // The FIRST up() finally fails — it must not touch the machine.
    upResolvers[0]({ code: 570, message: 'stale failure' });
    await firstConnect;

    expect(vpn.useVPNMachineStore.getState().state).toBe('connecting');
    expect(vpn.useVPNMachineStore.getState().error).toBeNull();
  });

  it('up() failing within the same epoch still reports the error', async () => {
    // Mutation canary: proves the guard above did not simply mute all failures.
    const { useConnectionStore, vpn } = await getStores();
    const { authService } = await import('../../services/auth-service');
    vi.mocked(authService.buildTunnelUrl).mockResolvedValue('k2v5://u:t@tokyo.example.com:443');
    mockRun.mockResolvedValue({ code: 570, message: 'permission denied' });

    selectTunnel(useConnectionStore);
    await useConnectionStore.getState().connect();

    expect(vpn.useVPNMachineStore.getState().state).toBe('idle');
    expect(vpn.useVPNMachineStore.getState().error).toEqual({ code: 570, message: 'permission denied' });
  });
});

/**
 * 504 RuleBundlesUnavailable one-shot global fallback.
 *
 * Pre-2026-06-14 engines fail closed when the split country's bundles can't
 * be ensured (the incident the old server-side geo hotfix papered over);
 * newer engines 504 only on a true cold start. The OTA webapp serves both
 * generations, so connection.store retries ONCE with global-shape routes
 * (zero bundle dependency). cn is exempt — its bundles ship embedded, so a
 * 504 there is a real fault a global retry won't fix.
 */
describe('Connection Store - 504 rule-bundle global fallback', () => {
  async function setupWithCountry(country: string) {
    const stores = await getStores();
    stores.config.useConfigStore.setState({ country, countryVia: 'direct', autoDetect: false });
    const { authService } = await import('../../services/auth-service');
    vi.mocked(authService.buildTunnelUrl).mockResolvedValue('k2v5://u:t@tokyo.example.com:443');
    const tunnel = {
      id: 1, domain: 'tokyo.example.com', name: 'Tokyo', protocol: 'k2v5',
      port: 443, serverUrl: 'k2v5://tokyo.example.com:443', node: { country: 'JP' },
    } as any;
    stores.useConnectionStore.getState().selectCloudTunnel(tunnel);
    return stores;
  }

  it('504 with a non-cn country retries once with global-shape routes and succeeds silently', async () => {
    const { useConnectionStore, vpn } = await setupWithCountry('tr');
    mockRun
      .mockResolvedValueOnce({ code: 504, message: 'rule bundles unavailable' })
      .mockResolvedValueOnce({ code: 0 });

    await useConnectionStore.getState().connect();

    const upCalls = mockRun.mock.calls.filter(c => c[0] === 'up');
    expect(upCalls).toHaveLength(2);

    // First attempt carried the country split (region route present).
    const firstRoutes = upCalls[0][1].config.routes as Array<{ via: string; match: Record<string, unknown> }>;
    expect(firstRoutes.some(r => r.match?.region === 'tr')).toBe(true);

    // Retry is the global shape: single route, no region/preset — zero bundle deps.
    const retryRoutes = upCalls[1][1].config.routes as Array<{ via: string; match: Record<string, unknown> }>;
    expect(retryRoutes).toHaveLength(1);
    expect(retryRoutes[0].match).toEqual({ all: true });
    expect(retryRoutes[0].via).toBe('k2v5://u:t@tokyo.example.com:443');

    // Success → no error dispatched, machine still in connecting (awaiting backend).
    expect(vpn.useVPNMachineStore.getState().state).toBe('connecting');
    expect(vpn.useVPNMachineStore.getState().error).toBeNull();
  });

  it('surfaces the retry failure when the global fallback also fails', async () => {
    const { useConnectionStore, vpn } = await setupWithCountry('tr');
    mockRun
      .mockResolvedValueOnce({ code: 504, message: 'rule bundles unavailable' })
      .mockResolvedValueOnce({ code: 503, message: 'server unreachable' });

    await useConnectionStore.getState().connect();

    expect(mockRun.mock.calls.filter(c => c[0] === 'up')).toHaveLength(2);
    expect(vpn.useVPNMachineStore.getState().state).toBe('idle');
    expect(vpn.useVPNMachineStore.getState().error?.code).toBe(503);
  });

  it('504 with country=cn does NOT retry (embedded bundles make it a real fault)', async () => {
    const { useConnectionStore, vpn } = await setupWithCountry('cn');
    mockRun.mockResolvedValueOnce({ code: 504, message: 'rule bundles unavailable' });

    await useConnectionStore.getState().connect();

    expect(mockRun.mock.calls.filter(c => c[0] === 'up')).toHaveLength(1);
    expect(vpn.useVPNMachineStore.getState().state).toBe('idle');
    expect(vpn.useVPNMachineStore.getState().error?.code).toBe(504);
  });

  it('non-504 errors do not trigger the fallback', async () => {
    const { useConnectionStore, vpn } = await setupWithCountry('tr');
    mockRun.mockResolvedValueOnce({ code: 401, message: 'auth rejected' });

    await useConnectionStore.getState().connect();

    expect(mockRun.mock.calls.filter(c => c[0] === 'up')).toHaveLength(1);
    expect(vpn.useVPNMachineStore.getState().error?.code).toBe(401);
  });
});

// ==================== Self-hosted selection stays in sync ====================
//
// In self_hosted mode activeTunnel is derived from the self-hosted store's
// tunnel. It used to be computed only when the tab was switched, so a tunnel
// loaded or saved afterwards never reached it: the connect button stayed on
// "select a server" and the self-hosted row was not clickable, until the user
// switched tabs away and back.
describe('self_hosted activeTunnel follows the self-hosted tunnel', () => {
  const URI_A = 'k2v5://alice:token@1.2.3.4:443?country=JP#tokyo';
  const URI_B = 'k2v5://bob:token@5.6.7.8:443?country=SG#sg';
  const flush = () => new Promise((r) => setTimeout(r, 0));

  it('cold start with persisted self_hosted mode and a stored tunnel is connectable', async () => {
    mockStorage.get.mockImplementation(async (key: string) => {
      if (key === 'k2.vpn.server_mode') return 'self_hosted';
      if (key === 'k2.self_hosted.tunnel') return { uri: URI_A, name: 'tokyo', country: 'JP' };
      return undefined;
    });
    const sh = await import('../self-hosted.store');
    const conn = await import('../connection.store');
    await sh.useSelfHostedStore.getState().loadTunnel();
    const cleanup = conn.initializeConnectionStore();
    await flush();

    const s = conn.useConnectionStore.getState();
    expect(s.serverMode).toBe('self_hosted');
    expect(s.activeTunnel?.serverUrl).toBe(URI_A);
    expect(conn.hasConnectableSelection(s)).toBe(true);
    cleanup();
  });

  it('cold start where the tunnel loads after the server mode is connectable', async () => {
    mockStorage.get.mockImplementation(async (key: string) => {
      if (key === 'k2.vpn.server_mode') return 'self_hosted';
      if (key === 'k2.self_hosted.tunnel') return { uri: URI_A, name: 'tokyo', country: 'JP' };
      return undefined;
    });
    const sh = await import('../self-hosted.store');
    const conn = await import('../connection.store');
    const cleanup = conn.initializeConnectionStore();
    await flush();
    expect(conn.useConnectionStore.getState().serverMode).toBe('self_hosted');
    await sh.useSelfHostedStore.getState().loadTunnel();

    const s = conn.useConnectionStore.getState();
    expect(s.activeTunnel?.serverUrl).toBe(URI_A);
    expect(conn.hasConnectableSelection(s)).toBe(true);
    cleanup();
  });

  it('saving a tunnel while the self-hosted tab is already open makes it connectable', async () => {
    const sh = await import('../self-hosted.store');
    sh.useSelfHostedStore.setState({ tunnel: null, loaded: true });
    const { useConnectionStore, initializeConnectionStore, hasConnectableSelection } = await getStores();
    const cleanup = initializeConnectionStore();
    await flush();

    await useConnectionStore.getState().setServerMode('self_hosted'); // tab opened before configuring
    expect(hasConnectableSelection(useConnectionStore.getState())).toBe(false);

    await sh.useSelfHostedStore.getState().saveTunnel(URI_A);

    const s = useConnectionStore.getState();
    expect(s.activeTunnel?.serverUrl).toBe(URI_A);
    expect(hasConnectableSelection(s)).toBe(true);
    cleanup();
  });

  it('replacing and clearing the tunnel update activeTunnel; connectedTunnel stays frozen', async () => {
    const sh = await import('../self-hosted.store');
    sh.useSelfHostedStore.setState({ tunnel: { uri: URI_A, name: 'tokyo', country: 'JP' }, loaded: true });
    const { useConnectionStore, initializeConnectionStore, hasConnectableSelection } = await getStores();
    const cleanup = initializeConnectionStore();
    await flush();
    useConnectionStore.getState().selectSelfHosted();
    const frozen = useConnectionStore.getState().activeTunnel;
    useConnectionStore.setState({ connectedTunnel: frozen });

    await sh.useSelfHostedStore.getState().saveTunnel(URI_B);
    expect(useConnectionStore.getState().activeTunnel?.serverUrl).toBe(URI_B);
    expect(useConnectionStore.getState().connectedTunnel).toBe(frozen);

    await sh.useSelfHostedStore.getState().clearTunnel();
    expect(useConnectionStore.getState().activeTunnel).toBeNull();
    expect(hasConnectableSelection(useConnectionStore.getState())).toBe(false);
    cleanup();
  });

  it('in manual mode a self-hosted save leaves the cloud selection alone', async () => {
    const sh = await import('../self-hosted.store');
    sh.useSelfHostedStore.setState({ tunnel: null, loaded: true });
    const { useConnectionStore, initializeConnectionStore } = await getStores();
    const cleanup = initializeConnectionStore();
    await flush();
    const tunnel = { id: 1, domain: 'tokyo.example.com', name: 'Tokyo', protocol: 'k2v5', port: 443,
      serverUrl: 'k2v5://tokyo.example.com:443', node: { country: 'JP' } } as any;
    useConnectionStore.getState().selectCloudTunnel(tunnel);
    const before = useConnectionStore.getState().activeTunnel;

    await sh.useSelfHostedStore.getState().saveTunnel(URI_A);

    expect(useConnectionStore.getState().serverMode).toBe('manual');
    expect(useConnectionStore.getState().activeTunnel).toBe(before);
    cleanup();
  });
});

// Cloud-side events must not wipe a self-hosted selection: CloudTunnelList stays
// mounted on the other tab, and for an account without membership its fetch
// answers 402 right after launch. That cleared activeTunnel even in self_hosted
// mode, so a non-member self-hosting user was stuck on "select a server".
describe('cloud events leave a self_hosted selection alone', () => {
  const URI_A = 'k2v5://alice:token@1.2.3.4:443?country=JP#tokyo';
  const cloud = { id: 1, domain: 'tokyo.example.com', name: 'Tokyo', protocol: 'k2v5', port: 443,
    serverUrl: 'k2v5://tokyo.example.com:443', node: { country: 'JP' } } as any;

  async function selfHostedWithStaleCloudPick() {
    const sh = await import('../self-hosted.store');
    sh.useSelfHostedStore.setState({ tunnel: { uri: URI_A, name: 'tokyo', country: 'JP' }, loaded: true });
    const stores = await getStores();
    stores.useConnectionStore.getState().selectCloudTunnel(cloud); // picked earlier, then switched away
    stores.useConnectionStore.getState().selectSelfHosted();
    return stores;
  }

  it('402 (setCloudAccess(false)) in self_hosted mode keeps activeTunnel', async () => {
    const { useConnectionStore, hasConnectableSelection } = await selfHostedWithStaleCloudPick();
    useConnectionStore.getState().setCloudAccess(false);
    const s = useConnectionStore.getState();
    expect(s.cloudAccessRevoked).toBe(true);
    expect(s.selectedCloudTunnel).toBeNull();
    expect(s.activeTunnel?.serverUrl).toBe(URI_A);
    expect(hasConnectableSelection(s)).toBe(true);
  });

  it('402 in manual mode still clears the cloud selection', async () => {
    const { useConnectionStore } = await getStores();
    useConnectionStore.getState().selectCloudTunnel(cloud);
    useConnectionStore.getState().setCloudAccess(false);
    expect(useConnectionStore.getState().activeTunnel).toBeNull();
    expect(useConnectionStore.getState().selectedCloudTunnel).toBeNull();
  });

  it('a stale cloud pick going offline in self_hosted mode keeps activeTunnel', async () => {
    const { useConnectionStore } = await selfHostedWithStaleCloudPick();
    useConnectionStore.getState().reconcileSelection([]);
    expect(useConnectionStore.getState().selectedCloudTunnel).toBeNull();
    expect(useConnectionStore.getState().activeTunnel?.serverUrl).toBe(URI_A);
  });

  it('the cloud pick going offline in manual mode still falls back to Auto', async () => {
    const { useConnectionStore } = await getStores();
    useConnectionStore.getState().selectCloudTunnel(cloud);
    useConnectionStore.getState().reconcileSelection([]);
    expect(useConnectionStore.getState().activeTunnel).toBeNull();
    expect(useConnectionStore.getState().selectedCloudTunnel).toBeNull();
  });
});

// ==================== Auto picker: real pickAutoTunnel, real caches ====================

describe('connect() Auto with the real picker', () => {
  afterEach(async () => {
    const { cacheStore } = await import('../../services/cache-store');
    cacheStore.clear();
  });

  const mkTunnel = (id: number, domain: string, score: number, country = 'JP') => ({
    id, domain, name: domain, protocol: 'k2v5', port: 443,
    serverUrl: `k2v5://${domain}:443`, recommendScore: score, node: { country },
  }) as any;
  const mkProbe = (domain: string, o: Record<string, unknown> = {}) => ({
    url: `k2v5://${domain}:443`, avgRttMs: 50, minRttMs: 40, maxRttMs: 60, jitterMs: 1, lossRate: 0,
    reachable: true, echoSupported: true, probeScore: 0.9, measuredAt: new Date().toISOString(), ...o,
  });
  /** mulberry32, so a run can be replayed exactly through Math.random. */
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

  async function setup(tunnels: any[]) {
    const stores = await getStores();
    const { cacheStore } = await import('../../services/cache-store');
    const { authService } = await import('../../services/auth-service');
    vi.mocked(authService.buildTunnelUrl).mockImplementation(async (u: string) => u);
    mockRun.mockResolvedValue({ code: 0 });
    cacheStore.set('api:tunnels', { items: tunnels });
    return stores;
  }

  /** One fresh connect() with a seeded Math.random; returns the domain sent to up(). */
  async function connectOnce(tunnels: any[], seed: number, probes: any[]) {
    vi.resetModules();
    mockRun.mockReset();
    const { useConnectionStore } = await setup(tunnels);
    const { useProbeStore } = await import('../probe.store');
    useProbeStore.getState().record(probes);
    const spy = vi.spyOn(Math, 'random').mockImplementation(prng(seed));
    await useConnectionStore.getState().connect();
    spy.mockRestore();
    const up = mockRun.mock.calls.find((c: any[]) => c[0] === 'up');
    expect(up).toBeDefined();
    const routes = up![1].config.routes as Array<{ via: string }>;
    const hit = tunnels.find(t => routes.some(r => r.via === t.serverUrl));
    expect(hit).toBeDefined();
    return hit.domain as string;
  }

  it('sends a top-80% tunnel to up(), and a populated probe store changes nothing', async () => {
    // n=10 keeps 8. Probes favour the two cut nodes and condemn everything else, so any
    // consumer of the probe store would send different tunnels for the same seed.
    const tunnels = Array.from({ length: 10 }, (_, i) =>
      mkTunnel(i + 1, `n${i}.example.com`, 0.95 - i * 0.05));
    const cut = new Set(['n8.example.com', 'n9.example.com']);
    const probes = tunnels.map(t => (cut.has(t.domain)
      ? mkProbe(t.domain, { avgRttMs: 5, probeScore: 1 })
      : mkProbe(t.domain, { reachable: false, avgRttMs: 0, lossRate: 1, probeScore: 0 })));

    const seen = new Set<string>();
    for (let seed = 1; seed <= 8; seed++) {
      const withoutProbes = await connectOnce(tunnels, seed, []);
      const withProbes = await connectOnce(tunnels, seed, probes);
      expect(withProbes).toBe(withoutProbes);
      expect(cut.has(withProbes)).toBe(false);
      seen.add(withProbes);
    }
    expect(seen.size).toBeGreaterThan(1);
  });

  it('all excluded by country -> 573 FILTERED', async () => {
    const tunnels = [mkTunnel(1, 'hk1.example.com', 0.8, 'HK'), mkTunnel(2, 'hk2.example.com', 0.5, 'HK')];
    const { useConnectionStore, vpn } = await setup(tunnels);
    await useConnectionStore.getState().setExcludedCountries(['hk']);
    await useConnectionStore.getState().connect();
    expect(mockRun).not.toHaveBeenCalledWith('up', expect.anything());
    expect(vpn.useVPNMachineStore.getState().error?.code).toBe(573);
  });

  it('empty list -> 572 AUTO', async () => {
    const { useConnectionStore, vpn } = await setup([]);
    await useConnectionStore.getState().connect();
    expect(mockRun).not.toHaveBeenCalledWith('up', expect.anything());
    expect(vpn.useVPNMachineStore.getState().error?.code).toBe(572);
  });
});

// ==================== Auto failover: primary + fallbacks on distinct machines ====================

describe('connect() Auto sends fallbacks', () => {
  afterEach(async () => {
    const { cacheStore } = await import('../../services/cache-store');
    cacheStore.clear();
  });

  /** mulberry32, so connect()'s Math.random draws can be replayed. */
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

  /** n tunnels, each on its own machine (distinct ipv4). */
  const machines = (n: number) => Array.from({ length: n }, (_, i) => ({
    id: i + 1, domain: `m${i}.example.com`, name: `m${i}`, protocol: 'k2v5', port: 443,
    serverUrl: `k2v5://m${i}.example.com:443`, recommendScore: 0.9 - i * 0.05,
    node: { country: 'JP', ipv4: `10.9.0.${i}`, ipv6: '' },
  })) as any[];

  async function setup(tunnels: any[]) {
    const stores = await getStores();
    const { cacheStore } = await import('../../services/cache-store');
    const { authService } = await import('../../services/auth-service');
    vi.mocked(authService.buildTunnelUrl).mockImplementation(async (u: string) => u.replace('k2v5://', 'k2v5://u:t@'));
    mockRun.mockResolvedValue({ code: 0 });
    cacheStore.set('api:tunnels', { items: tunnels });
    return { ...stores, authService };
  }

  const upConfig = () => mockRun.mock.calls.find((c: any[]) => c[0] === 'up')![1].config;

  it('Auto builds 4 tunnel URLs (primary + 3 fallbacks) and keys fallbacks by the primary via', async () => {
    const { useConnectionStore, authService } = await setup(machines(8));
    await useConnectionStore.getState().connect();

    expect(vi.mocked(authService.buildTunnelUrl)).toHaveBeenCalledTimes(4);
    const cfg = upConfig();
    const primaryVia = cfg.routes.find((r: any) => r.match.all).via as string;
    expect(Object.keys(cfg.fallbacks)).toEqual([primaryVia]);
    const fb = cfg.fallbacks[primaryVia] as string[];
    expect(fb).toHaveLength(3);
    expect(new Set([primaryVia, ...fb]).size).toBe(4);
    for (const u of fb) expect(u.startsWith('k2v5://u:t@')).toBe(true);
    expect(cfg.netcheck.length).toBeGreaterThan(0);
  });

  it('a failing fallback URL build is dropped without affecting the primary', async () => {
    const tunnels = machines(8);
    const { useConnectionStore, authService } = await setup(tunnels);
    // Same seed for the expectation and for connect()'s Math.random draws.
    const { pickAutoCandidates } = await import('../../utils/auto-tunnel-pick');
    const expected = pickAutoCandidates(tunnels, [], 4, prng(7));
    expect(expected).toHaveLength(4);
    const doomed = expected[2].serverUrl;
    vi.mocked(authService.buildTunnelUrl).mockImplementation(async (u: string) => {
      if (u === doomed) throw new Error('storage exploded');
      return u.replace('k2v5://', 'k2v5://u:t@');
    });
    const spy = vi.spyOn(Math, 'random').mockImplementation(prng(7));
    await useConnectionStore.getState().connect();
    spy.mockRestore();

    const auth = (u: string) => u.replace('k2v5://', 'k2v5://u:t@');
    const cfg = upConfig();
    expect(cfg.routes.some((r: any) => r.via === auth(expected[0].serverUrl))).toBe(true);
    expect(cfg.fallbacks).toEqual({
      [auth(expected[0].serverUrl)]: [auth(expected[1].serverUrl), auth(expected[3].serverUrl)],
    });
  });

  it('one tunnel in Auto → no fallbacks key', async () => {
    const { useConnectionStore, authService } = await setup(machines(1));
    await useConnectionStore.getState().connect();
    expect(vi.mocked(authService.buildTunnelUrl)).toHaveBeenCalledTimes(1);
    expect('fallbacks' in upConfig()).toBe(false);
  });

  it('manual selection builds exactly 1 tunnel URL and sends no fallbacks', async () => {
    const tunnels = machines(8);
    const { useConnectionStore, authService } = await setup(tunnels);
    useConnectionStore.getState().selectCloudTunnel(tunnels[2]);
    await useConnectionStore.getState().connect();

    expect(vi.mocked(authService.buildTunnelUrl)).toHaveBeenCalledTimes(1);
    expect('fallbacks' in upConfig()).toBe(false);
  });

  it('the 504 global-routes retry carries the same fallbacks', async () => {
    const { useConnectionStore } = await setup(machines(8));
    const { useConfigStore } = await import('../config.store');
    useConfigStore.setState({ country: 'tr', countryVia: 'direct', autoDetect: false });
    mockRun.mockReset();
    mockRun
      .mockResolvedValueOnce({ code: 504, message: 'rule bundles unavailable' })
      .mockResolvedValueOnce({ code: 0 });
    await useConnectionStore.getState().connect();

    const ups = mockRun.mock.calls.filter((c: any[]) => c[0] === 'up');
    expect(ups).toHaveLength(2);
    expect(ups[1][1].config.fallbacks).toEqual(ups[0][1].config.fallbacks);
    expect(Object.values(ups[1][1].config.fallbacks as Record<string, string[]>)[0]).toHaveLength(3);
  });
});

// ==================== Pinned tunnels: persisted selection, pinned reconcile, pinned fallbacks ====================

describe('pinned tunnels', () => {
  afterEach(async () => {
    const { cacheStore } = await import('../../services/cache-store');
    cacheStore.clear();
  });

  const machines = (n: number) => Array.from({ length: n }, (_, i) => ({
    id: i + 1, domain: `m${i}.example.com`, name: `m${i}`, protocol: 'k2v5', port: 443,
    serverUrl: `k2v5://m${i}.example.com:443`, recommendScore: 0.9 - i * 0.05,
    node: { country: 'JP', ipv4: `10.9.0.${i}`, ipv6: '' },
  })) as any[];

  async function setup() {
    const stores = await getStores();
    const pinnedMod = await import('../pinned-tunnels.store');
    pinnedMod.usePinnedTunnelsStore.setState({ pinned: [], recent: [], loaded: true });
    const { cacheStore } = await import('../../services/cache-store');
    const { authService } = await import('../../services/auth-service');
    vi.mocked(authService.buildTunnelUrl).mockImplementation(async (u: string) => u.replace('k2v5://', 'k2v5://u:t@'));
    mockRun.mockResolvedValue({ code: 0 });
    mockStorage.set.mockResolvedValue(undefined);
    mockStorage.remove.mockResolvedValue(undefined);
    return { ...stores, pinned: pinnedMod.usePinnedTunnelsStore, cacheStore, authService };
  }

  const upConfig = () => mockRun.mock.calls.find((c: any[]) => c[0] === 'up')![1].config;
  const auth = (u: string) => u.replace('k2v5://', 'k2v5://u:t@');

  describe('selection persistence', () => {
    it('selectCloudTunnel persists the domain; Auto and self_hosted clear it', async () => {
      const { useConnectionStore } = await setup();
      const t = machines(1)[0];
      useConnectionStore.getState().selectCloudTunnel({ ...t, domain: 'M0.Example.com' });
      expect(mockStorage.set).toHaveBeenCalledWith('k2.connection.selectedDomain', 'm0.example.com');
      useConnectionStore.getState().clearCloudSelection();
      expect(mockStorage.remove).toHaveBeenCalledWith('k2.connection.selectedDomain');
      mockStorage.remove.mockClear();
      useConnectionStore.getState().selectSelfHosted();
      expect(mockStorage.remove).toHaveBeenCalledWith('k2.connection.selectedDomain');
    });

    it('restores the stored selection on the first tunnel list', async () => {
      const { useConnectionStore } = await setup();
      const tunnels = machines(3);
      useConnectionStore.setState({ selectedDomain: 'm2.example.com', selectedDomainLoaded: true, selectionRestored: false });
      useConnectionStore.getState().reconcileSelection(tunnels);
      expect(useConnectionStore.getState().selectedCloudTunnel).toBe(tunnels[2]);
      expect(useConnectionStore.getState().selectionRestored).toBe(true);
    });

    it('storage loading after the list arrived still restores (race)', async () => {
      const { useConnectionStore, cacheStore } = await setup();
      const tunnels = machines(3);
      cacheStore.set('api:tunnels', { items: tunnels });
      useConnectionStore.setState({ selectedDomainLoaded: false, selectionRestored: false });
      useConnectionStore.getState().reconcileSelection(tunnels); // list first: nothing to restore yet
      expect(useConnectionStore.getState().selectedCloudTunnel).toBeNull();
      expect(useConnectionStore.getState().selectionRestored).toBe(false);
      mockStorage.get.mockImplementation(async (k: string) => (k === 'k2.connection.selectedDomain' ? 'm1.example.com' : null));
      await useConnectionStore.getState().loadSelectedDomain();
      expect(useConnectionStore.getState().selectedCloudTunnel).toBe(tunnels[1]);
    });

    it('a user Auto tap before restore wins over the stored selection', async () => {
      const { useConnectionStore } = await setup();
      const tunnels = machines(3);
      useConnectionStore.setState({ selectedDomain: 'm2.example.com', selectedDomainLoaded: true, selectionRestored: false });
      useConnectionStore.getState().clearCloudSelection();
      useConnectionStore.getState().reconcileSelection(tunnels);
      expect(useConnectionStore.getState().selectedCloudTunnel).toBeNull();
    });

    it('stored domain missing from a (possibly stale) list → Auto for now, restored by a later list', async () => {
      const { useConnectionStore } = await setup();
      const tunnels = machines(3);
      useConnectionStore.setState({ selectedDomain: 'm2.example.com', selectedDomainLoaded: true, selectionRestored: false });
      useConnectionStore.getState().reconcileSelection(tunnels.slice(0, 2));
      expect(useConnectionStore.getState().selectedCloudTunnel).toBeNull();
      expect(useConnectionStore.getState().selectionRestored).toBe(false);
      useConnectionStore.getState().reconcileSelection(tunnels);
      expect(useConnectionStore.getState().selectedCloudTunnel).toBe(tunnels[2]);
    });

    it('started on self_hosted → restore stays armed and runs on switching to the cloud tab', async () => {
      const { useConnectionStore, cacheStore } = await setup();
      const tunnels = machines(3);
      cacheStore.set('api:tunnels', { items: tunnels });
      useConnectionStore.setState({ serverMode: 'self_hosted', selectedDomain: 'm1.example.com', selectedDomainLoaded: true, selectionRestored: false });
      useConnectionStore.getState().reconcileSelection(tunnels);
      expect(useConnectionStore.getState().selectedCloudTunnel).toBeNull();
      await useConnectionStore.getState().setServerMode('manual');
      expect(useConnectionStore.getState().selectedCloudTunnel).toBe(tunnels[1]);
    });

    it('never restores under a live connection', async () => {
      const { useConnectionStore, vpn } = await setup();
      vpn.useVPNMachineStore.setState({ state: 'connected' } as any);
      useConnectionStore.setState({ selectedDomain: 'm1.example.com', selectedDomainLoaded: true, selectionRestored: false });
      useConnectionStore.getState().reconcileSelection(machines(2));
      expect(useConnectionStore.getState().selectedCloudTunnel).toBeNull();
      expect(useConnectionStore.getState().selectionRestored).toBe(false);
    });

    it('adopts the fresh list object when the selected tunnel URL/address changed', async () => {
      const { useConnectionStore } = await setup();
      const old = machines(1)[0];
      useConnectionStore.getState().selectCloudTunnel(old);
      const fresh = { ...old, node: { ...old.node, ipv4: '10.9.9.9' } };
      useConnectionStore.getState().reconcileSelection([fresh]);
      expect(useConnectionStore.getState().selectedCloudTunnel).toBe(fresh);
      expect(useConnectionStore.getState().activeTunnel?.ipv4).toBe('10.9.9.9');
    });

    it('does not restore over self_hosted mode', async () => {
      const { useConnectionStore } = await setup();
      useConnectionStore.setState({ serverMode: 'self_hosted', selectedDomain: 'm1.example.com', selectedDomainLoaded: true, selectionRestored: false });
      useConnectionStore.getState().reconcileSelection(machines(2));
      expect(useConnectionStore.getState().selectedCloudTunnel).toBeNull();
    });
  });

  describe('reconcile inside the pinned pool', () => {
    it('vanished pinned selection → next pin still in the list, in pin order', async () => {
      const { useConnectionStore, pinned } = await setup();
      const t = machines(4);
      pinned.getState().pin(t[0]);
      pinned.getState().pin(t[3]);
      pinned.getState().pin(t[2]);
      useConnectionStore.getState().selectCloudTunnel(t[0]);
      useConnectionStore.getState().reconcileSelection([t[1], t[2], t[3]]);
      expect(useConnectionStore.getState().selectedCloudTunnel).toBe(t[3]);
      expect(useConnectionStore.getState().activeTunnel?.domain).toBe('m3.example.com');
    });

    it('all pins gone → Auto (as before)', async () => {
      const { useConnectionStore, pinned } = await setup();
      const t = machines(3);
      pinned.getState().pin(t[0]);
      pinned.getState().pin(t[1]);
      useConnectionStore.getState().selectCloudTunnel(t[0]);
      useConnectionStore.getState().reconcileSelection([t[2]]);
      expect(useConnectionStore.getState().selectedCloudTunnel).toBeNull();
    });

    it('vanished unpinned selection → Auto even when pins exist', async () => {
      const { useConnectionStore, pinned } = await setup();
      const t = machines(3);
      pinned.getState().pin(t[1]);
      useConnectionStore.getState().selectCloudTunnel(t[0]);
      useConnectionStore.getState().reconcileSelection([t[1], t[2]]);
      expect(useConnectionStore.getState().selectedCloudTunnel).toBeNull();
    });
  });

  describe('connect()', () => {
    it('pinned primary sends the other pins as fallbacks, in pin order', async () => {
      const { useConnectionStore, pinned, cacheStore } = await setup();
      const t = machines(5);
      cacheStore.set('api:tunnels', { items: t });
      pinned.getState().pin(t[3]);
      pinned.getState().pin(t[1]);
      pinned.getState().pin(t[4]);
      useConnectionStore.getState().selectCloudTunnel(t[1]);
      await useConnectionStore.getState().connect();
      expect(upConfig().fallbacks).toEqual({
        [auth(t[1].serverUrl)]: [auth(t[3].serverUrl), auth(t[4].serverUrl)],
      });
    });

    it('single pin → no fallbacks (retry in place)', async () => {
      const { useConnectionStore, pinned, cacheStore } = await setup();
      const t = machines(3);
      cacheStore.set('api:tunnels', { items: t });
      pinned.getState().pin(t[0]);
      useConnectionStore.getState().selectCloudTunnel(t[0]);
      await useConnectionStore.getState().connect();
      expect('fallbacks' in upConfig()).toBe(false);
    });

    it('unpinned manual pick sends no fallbacks even when pins exist', async () => {
      const { useConnectionStore, pinned, cacheStore } = await setup();
      const t = machines(3);
      cacheStore.set('api:tunnels', { items: t });
      pinned.getState().pin(t[1]);
      pinned.getState().pin(t[2]);
      useConnectionStore.getState().selectCloudTunnel(t[0]);
      await useConnectionStore.getState().connect();
      expect('fallbacks' in upConfig()).toBe(false);
    });

    it('a rejected up() does not record recent', async () => {
      const { useConnectionStore, pinned, cacheStore } = await setup();
      const t = machines(1);
      cacheStore.set('api:tunnels', { items: t });
      mockRun.mockResolvedValue({ code: 403, message: 'no' });
      await useConnectionStore.getState().connect();
      expect(pinned.getState().recent).toEqual([]);
    });

    it('records the connected cloud tunnel as recent (manual and Auto)', async () => {
      const { useConnectionStore, pinned, cacheStore } = await setup();
      const t = machines(1);
      cacheStore.set('api:tunnels', { items: t });
      await useConnectionStore.getState().connect(); // Auto resolves m0
      expect(pinned.getState().recent.map(r => r.domain)).toEqual(['m0.example.com']);
    });
  });
});
