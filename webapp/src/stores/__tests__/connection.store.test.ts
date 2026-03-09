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
  // Initialize config store with defaults
  configMod.useConfigStore.setState({ config: {}, loaded: true, ruleMode: 'chnroute' });
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
  it('connect snapshots connectedTunnel and calls _k2.run(up)', async () => {
    const { useConnectionStore, vpn } = await getStores();
    mockRun.mockResolvedValue({ code: 0 });

    // Select a cloud tunnel
    useConnectionStore.getState().selectCloudTunnel({
      id: 1,
      domain: 'tokyo.example.com',
      name: 'Tokyo',
      protocol: 'k2v5',
      port: 443,
      serverUrl: 'k2v5://tokyo.example.com:443',
      node: { country: 'JP' },
    } as any);

    // Mock auth service to inject credentials
    const { authService } = await import('../../services/auth-service');
    vi.mocked(authService.buildTunnelUrl).mockResolvedValue('k2v5://udid:token@tokyo.example.com:443');

    await useConnectionStore.getState().connect();

    // connectedTunnel snapshot should be set
    expect(useConnectionStore.getState().connectedTunnel).toEqual({
      source: 'cloud',
      domain: 'tokyo.example.com',
      name: 'Tokyo',
      country: 'JP',
      serverUrl: 'k2v5://tokyo.example.com:443',
    });

    // _k2.run('up') should have been called with config
    expect(mockRun).toHaveBeenCalledWith('up', expect.objectContaining({
      server: 'k2v5://udid:token@tokyo.example.com:443',
      mode: 'tun',
    }));

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
    await useConnectionStore.getState().connect();

    expect(mockRun).toHaveBeenCalledWith('up', expect.objectContaining({
      server: 'k2v5://alice:token@1.2.3.4:443#tokyo',
    }));
  });

  it('connect does nothing when no tunnel selected', async () => {
    const { useConnectionStore } = await getStores();
    mockRun.mockResolvedValue({ code: 0 });

    await useConnectionStore.getState().connect();

    expect(mockRun).not.toHaveBeenCalled();
  });

  it('connectEpoch guards against stale connect', async () => {
    const { useConnectionStore, vpn } = await getStores();

    // Select a cloud tunnel
    useConnectionStore.getState().selectCloudTunnel({
      id: 1,
      domain: 'slow.example.com',
      name: 'Slow',
      protocol: 'k2v5',
      port: 443,
      serverUrl: 'k2v5://slow.example.com:443',
      node: { country: 'US' },
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
    resolveAuth!('k2v5://udid:token@slow.example.com:443');
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
  it('connect persists server URL to config store BEFORE _k2.run', async () => {
    const { useConnectionStore, config } = await getStores();

    const persistOrder: string[] = [];
    const origUpdateConfig = config.useConfigStore.getState().updateConfig;
    vi.spyOn(config.useConfigStore.getState(), 'updateConfig').mockImplementation(async (partial) => {
      persistOrder.push('persist');
      return origUpdateConfig(partial);
    });

    mockRun.mockImplementation(async (action: string) => {
      if (action === 'up') persistOrder.push('run_up');
      return { code: 0 };
    });

    useConnectionStore.getState().selectCloudTunnel({
      id: 1,
      domain: 'test.com',
      name: 'Test',
      protocol: 'k2v5',
      port: 443,
      serverUrl: 'k2v5://test.com:443',
      node: { country: 'US' },
    } as any);

    const { authService } = await import('../../services/auth-service');
    vi.mocked(authService.buildTunnelUrl).mockResolvedValue('k2v5://u:t@test.com:443');

    await useConnectionStore.getState().connect();

    expect(persistOrder).toEqual(['persist', 'run_up']);
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

    // Put VPN into error state
    vpn.dispatch('USER_CONNECT');
    vpn.dispatch('BACKEND_ERROR', { code: 502, message: 'fail' });
    expect(vpn.useVPNMachineStore.getState().state).toBe('error');

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
