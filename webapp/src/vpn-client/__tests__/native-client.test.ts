import { describe, it, expect, beforeEach, vi } from 'vitest';
import { NativeVpnClient } from '../native-client';
import type { VpnEvent } from '../types';

function createMockPlugin() {
  return {
    checkReady: vi.fn(),
    getUDID: vi.fn(),
    getVersion: vi.fn(),
    getStatus: vi.fn(),
    getConfig: vi.fn(),
    connect: vi.fn(),
    disconnect: vi.fn(),
    addListener: vi.fn(),
  };
}

describe('NativeVpnClient', () => {
  let plugin: ReturnType<typeof createMockPlugin>;
  let client: NativeVpnClient;

  beforeEach(() => {
    plugin = createMockPlugin();
    client = new NativeVpnClient(plugin);
  });

  it('connect() calls plugin.connect({ wireUrl })', async () => {
    plugin.connect.mockResolvedValue(undefined);
    await client.connect('wss://example.com/wire');
    expect(plugin.connect).toHaveBeenCalledWith({ wireUrl: 'wss://example.com/wire' });
  });

  it('disconnect() calls plugin.disconnect()', async () => {
    plugin.disconnect.mockResolvedValue(undefined);
    await client.disconnect();
    expect(plugin.disconnect).toHaveBeenCalled();
  });

  it('getStatus() maps "disconnected" to "stopped"', async () => {
    plugin.getStatus.mockResolvedValue({ state: 'disconnected' });
    const status = await client.getStatus();
    expect(status.state).toBe('stopped');
  });

  it('getStatus() passes through "connected"', async () => {
    plugin.getStatus.mockResolvedValue({
      state: 'connected',
      connectedAt: '2026-01-01T00:00:00Z',
      uptimeSeconds: 120,
      wireUrl: 'wss://example.com/wire',
    });
    const status = await client.getStatus();
    expect(status.state).toBe('connected');
    expect(status.connectedAt).toBe('2026-01-01T00:00:00Z');
    expect(status.uptimeSeconds).toBe(120);
    expect(status.wireUrl).toBe('wss://example.com/wire');
  });

  it('getStatus() passes through "connecting"', async () => {
    plugin.getStatus.mockResolvedValue({ state: 'connecting' });
    const status = await client.getStatus();
    expect(status.state).toBe('connecting');
  });

  it('getStatus() maps unknown state to "stopped"', async () => {
    plugin.getStatus.mockResolvedValue({ state: 'unknown_state' });
    const status = await client.getStatus();
    expect(status.state).toBe('stopped');
  });

  it('getStatus() includes error field', async () => {
    plugin.getStatus.mockResolvedValue({ state: 'disconnected', error: 'auth failed' });
    const status = await client.getStatus();
    expect(status.error).toBe('auth failed');
  });

  it('checkReady() returns ready state with version', async () => {
    plugin.checkReady.mockResolvedValue({ ready: true, version: '1.2.3' });
    const result = await client.checkReady();
    expect(result).toEqual({ ready: true, version: '1.2.3' });
  });

  it('checkReady() returns not ready state', async () => {
    plugin.checkReady.mockResolvedValue({ ready: false, reason: 'not_running' });
    const result = await client.checkReady();
    expect(result).toEqual({ ready: false, reason: 'not_running' });
  });

  it('getUDID() extracts .udid from plugin result', async () => {
    plugin.getUDID.mockResolvedValue({ udid: 'device-abc-123' });
    const udid = await client.getUDID();
    expect(udid).toBe('device-abc-123');
  });

  it('getVersion() returns plugin version info', async () => {
    const versionInfo = { version: '1.0.0', go: '1.21', os: 'ios', arch: 'arm64' };
    plugin.getVersion.mockResolvedValue(versionInfo);
    const result = await client.getVersion();
    expect(result).toEqual(versionInfo);
  });

  it('getConfig() returns plugin config', async () => {
    plugin.getConfig.mockResolvedValue({ wireUrl: 'wss://example.com/wire' });
    const result = await client.getConfig();
    expect(result).toEqual({ wireUrl: 'wss://example.com/wire' });
  });

  describe('subscribe()', () => {
    it('maps vpnStateChange "disconnected" to state_change "stopped"', async () => {
      const removeFn = vi.fn().mockResolvedValue(undefined);
      plugin.addListener.mockResolvedValue({ remove: removeFn });

      const events: VpnEvent[] = [];
      client.subscribe((event) => events.push(event));

      // Get the vpnStateChange handler that was registered
      const stateChangeCall = plugin.addListener.mock.calls.find(
        (call) => call[0] === 'vpnStateChange'
      );
      expect(stateChangeCall).toBeDefined();

      // Simulate the plugin emitting a state change
      const handler = stateChangeCall![1];
      handler({ state: 'disconnected' });

      expect(events).toEqual([{ type: 'state_change', state: 'stopped' }]);
    });

    it('maps vpnStateChange "connected" correctly', async () => {
      const removeFn = vi.fn().mockResolvedValue(undefined);
      plugin.addListener.mockResolvedValue({ remove: removeFn });

      const events: VpnEvent[] = [];
      client.subscribe((event) => events.push(event));

      const stateChangeCall = plugin.addListener.mock.calls.find(
        (call) => call[0] === 'vpnStateChange'
      );
      const handler = stateChangeCall![1];
      handler({ state: 'connected' });

      expect(events).toEqual([{ type: 'state_change', state: 'connected' }]);
    });

    it('forwards vpnError events', async () => {
      const removeFn = vi.fn().mockResolvedValue(undefined);
      plugin.addListener.mockResolvedValue({ remove: removeFn });

      const events: VpnEvent[] = [];
      client.subscribe((event) => events.push(event));

      const errorCall = plugin.addListener.mock.calls.find(
        (call) => call[0] === 'vpnError'
      );
      expect(errorCall).toBeDefined();

      const handler = errorCall![1];
      handler({ message: 'connection timeout' });

      expect(events).toEqual([{ type: 'error', message: 'connection timeout' }]);
    });

    it('returns unsubscribe function that removes listener', async () => {
      const removeFn = vi.fn().mockResolvedValue(undefined);
      plugin.addListener.mockResolvedValue({ remove: removeFn });

      const events: VpnEvent[] = [];
      const unsubscribe = client.subscribe((event) => events.push(event));

      // Wait for plugin listener promises to resolve
      await vi.waitFor(() => {
        expect(removeFn).not.toHaveBeenCalled();
      });

      unsubscribe();

      // After last listener removed, plugin listeners should be cleaned up
      expect(removeFn).toHaveBeenCalled();
    });

    it('only sets up plugin listeners once for multiple subscribers', async () => {
      const removeFn = vi.fn().mockResolvedValue(undefined);
      plugin.addListener.mockResolvedValue({ remove: removeFn });

      client.subscribe(() => {});
      client.subscribe(() => {});

      // addListener should only be called twice (vpnStateChange + vpnError), not four times
      expect(plugin.addListener).toHaveBeenCalledTimes(2);
    });
  });

  describe('destroy()', () => {
    it('removes all plugin listeners and clears subscribers', async () => {
      const removeFn = vi.fn().mockResolvedValue(undefined);
      plugin.addListener.mockResolvedValue({ remove: removeFn });

      const events: VpnEvent[] = [];
      client.subscribe((event) => events.push(event));

      // Wait for plugin listener setup
      await vi.waitFor(() => {
        expect(plugin.addListener).toHaveBeenCalledTimes(2);
      });

      client.destroy();

      // Plugin listeners should be removed
      expect(removeFn).toHaveBeenCalled();

      // Simulate event after destroy - should not reach subscriber
      const stateChangeCall = plugin.addListener.mock.calls.find(
        (call) => call[0] === 'vpnStateChange'
      );
      stateChangeCall![1]({ state: 'connected' });

      expect(events).toHaveLength(0);
    });
  });
});
