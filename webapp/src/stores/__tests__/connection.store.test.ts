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
  // Use smart mode — tests that need a specific tunnel call selectCloudTunnel() first.
  connMod.useConnectionStore.setState({ lastServerUrl: null, lastServerUrlLoaded: true, serverMode: 'smart' as const, serverModeLoaded: true });
  return { ...connMod, vpn: vpnMod, config: configMod };
}

// ==================== Selection Tests ====================

describe('Connection Store - Selection', () => {
  it('defaults to cloud source with no tunnel selected', async () => {
    const { useConnectionStore } = await getStores();
    const state = useConnectionStore.getState();

    expect(state.selectedSource).toBe('cloud');
    expect(state.selectedCloudTunnel).toBeNull();
    expect(state.activeTunnel).toBeNull();
    expect(state.connectedTunnel).toBeNull();
  });

  it('selectCloudTunnel sets source and computes activeTunnel', async () => {
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
    expect(state.selectedSource).toBe('cloud');
    expect(state.selectedCloudTunnel).toBe(tunnel);
    expect(state.activeTunnel).toEqual({
      source: 'cloud',
      domain: 'tokyo.example.com',
      name: 'Tokyo',
      country: 'JP',
      serverUrl: 'k2v5://tokyo.example.com:443',
    });
  });

  it('selectSelfHosted sets source and computes activeTunnel from self-hosted store', async () => {
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
    expect(state.selectedSource).toBe('self_hosted');
    expect(state.activeTunnel).toEqual({
      source: 'self_hosted',
      domain: '1.2.3.4',
      name: 'tokyo',
      country: 'JP',
      serverUrl: 'k2v5://alice:token@1.2.3.4:443?country=JP#tokyo',
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
  it('connect (smart mode) snapshots synthetic connectedTunnel and calls _k2.run(up)', async () => {
    const { useConnectionStore, vpn } = await getStores();
    mockRun.mockResolvedValue({ code: 0 });

    // Smart mode (default) — mock buildSubsUrl
    const { authService } = await import('../../services/auth-service');
    vi.mocked(authService.buildSubsUrl).mockResolvedValue('k2subs://udid:token@k2.52j.me/api/subs');

    await useConnectionStore.getState().connect();

    // connectedTunnel is a synthetic subs snapshot
    const ct = useConnectionStore.getState().connectedTunnel;
    expect(ct?.source).toBe('cloud');
    expect(ct?.domain).toBe('subs');
    expect(ct?.serverUrl).toBe('k2subs://udid:token@k2.52j.me/api/subs');

    // _k2.run('up') called with subs URL in routes
    expect(mockRun).toHaveBeenCalledWith('up', expect.objectContaining({ mode: 'tun' }));
    const upCall = mockRun.mock.calls.find(c => c[0] === 'up');
    const routes = upCall?.[1]?.routes as Array<{ via: string }>;
    expect(routes.some(r => r.via === 'k2subs://udid:token@k2.52j.me/api/subs')).toBe(true);
    expect(upCall?.[1]?.server).toBeUndefined();

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
    const routes = upCall?.[1]?.routes as Array<{ via: string; match: Record<string, unknown> }>;
    const lastRoute = routes[routes.length - 1];
    expect(lastRoute?.via).toBe('k2v5://alice:token@1.2.3.4:443#tokyo');
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

  it('connectEpoch guards against stale connect', async () => {
    const { useConnectionStore, vpn } = await getStores();

    // Mock auth service to be slow
    const { authService } = await import('../../services/auth-service');
    let resolveAuth: (v: string) => void;
    vi.mocked(authService.buildSubsUrl).mockImplementation(
      () => new Promise(resolve => { resolveAuth = resolve; }),
    );

    // Start connect (smart mode — default)
    const connectPromise = useConnectionStore.getState().connect();

    // connect() dispatches USER_CONNECT after auth resolves, so VPN state is
    // still idle here. Manually move to 'connecting' so disconnect guard passes.
    vpn.dispatch('USER_CONNECT');

    // While auth is resolving, user disconnects (bumps epoch)
    await useConnectionStore.getState().disconnect();

    // Now resolve auth — connect should bail due to epoch mismatch
    resolveAuth!('k2subs://udid:token@k2.52j.me/api/subs');
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
    vi.mocked(authService.buildSubsUrl).mockResolvedValue('k2subs://udid:token@k2.52j.me/api/subs');

    await useConnectionStore.getState().connect();

    expect(order).toEqual(['persist', 'run_up']);
    expect(useConnectionStore.getState().lastServerUrl).toBe('k2subs://udid:token@k2.52j.me/api/subs');
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
