/**
 * Capacitor K2 Bridge Tests — RED phase
 *
 * Tests for the Capacitor mobile bridge (capacitor-k2.ts) which injects:
 *   window._k2      = { run(action, params) }     (VPN control via K2Plugin)
 *   window._platform = { os, isMobile, ..., storage, getUdid }
 *
 * All tests should FAIL because capacitor-k2.ts does not exist yet.
 */

import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';

// Mock @capacitor/core
vi.mock('@capacitor/core', () => ({
  Capacitor: {
    isNativePlatform: vi.fn(() => true),
    getPlatform: vi.fn(() => 'ios'),
  },
}));

// Mock k2-plugin — full mock object with all K2PluginInterface methods
const mockK2Plugin = {
  checkReady: vi.fn(),
  getUDID: vi.fn(),
  getVersion: vi.fn(),
  getStatus: vi.fn(),
  getConfig: vi.fn(),
  connect: vi.fn(),
  disconnect: vi.fn(),
  addListener: vi.fn(),
  checkWebUpdate: vi.fn(),
  checkNativeUpdate: vi.fn(),
  applyWebUpdate: vi.fn(),
  downloadNativeUpdate: vi.fn(),
  installNativeUpdate: vi.fn(),
};

vi.mock('k2-plugin', () => ({
  K2Plugin: mockK2Plugin,
}));

describe('capacitor-k2', () => {
  let originalK2: any;
  let originalPlatform: any;

  beforeEach(() => {
    originalK2 = window._k2;
    originalPlatform = window._platform;
    delete (window as any)._k2;
    delete (window as any)._platform;
    vi.clearAllMocks();

    // Default mock implementations
    mockK2Plugin.checkReady.mockResolvedValue({ ready: true, version: '0.4.0' });
    mockK2Plugin.addListener.mockResolvedValue({ remove: vi.fn() });
  });

  afterEach(() => {
    (window as any)._k2 = originalK2;
    (window as any)._platform = originalPlatform;
  });

  describe('isCapacitorNative', () => {
    it('test_isCapacitorNative_returns_true', async () => {
      const { isCapacitorNative } = await import('../capacitor-k2');

      expect(isCapacitorNative()).toBe(true);
    });
  });

  describe('injectCapacitorGlobals', () => {
    it('test_injectCapacitorGlobals_sets_k2', async () => {
      const { injectCapacitorGlobals } = await import('../capacitor-k2');

      await injectCapacitorGlobals();

      expect(window._k2).toBeDefined();
      expect(typeof window._k2.run).toBe('function');
    });

    it('test_injectCapacitorGlobals_sets_platform', async () => {
      const { injectCapacitorGlobals } = await import('../capacitor-k2');

      await injectCapacitorGlobals();

      expect(window._platform).toBeDefined();
      expect(window._platform.os).toBe('ios');
      expect(window._platform.isMobile).toBe(true);
      expect(window._platform.isDesktop).toBe(false);
      expect(window._platform.version).toBe('0.4.0');
    });

    it('test_injectCapacitorGlobals_registers_event_listeners', async () => {
      const { injectCapacitorGlobals } = await import('../capacitor-k2');

      await injectCapacitorGlobals();

      // Should register listeners for vpnStateChange and vpnError
      const listenerCalls = mockK2Plugin.addListener.mock.calls.map(
        (call: any[]) => call[0],
      );
      expect(listenerCalls).toContain('vpnStateChange');
      expect(listenerCalls).toContain('vpnError');
    });
  });

  describe('_k2.run', () => {
    it('test_k2_run_status_returns_StatusResponseData', async () => {
      const { injectCapacitorGlobals } = await import('../capacitor-k2');
      await injectCapacitorGlobals();

      mockK2Plugin.getStatus.mockResolvedValue({
        state: 'connected',
        connectedAt: '2024-01-01T00:00:00Z',
        uptimeSeconds: 3600,
      });

      const result = await window._k2.run('status');

      expect(result.code).toBe(0);
      expect(result.data).toBeDefined();
      expect(result.data.state).toBe('connected');
      expect(result.data.running).toBe(true);
      expect(result.data.networkAvailable).toBeDefined();
    });

    it('test_k2_run_status_disconnected', async () => {
      const { injectCapacitorGlobals } = await import('../capacitor-k2');
      await injectCapacitorGlobals();

      mockK2Plugin.getStatus.mockResolvedValue({
        state: 'disconnected',
      });

      const result = await window._k2.run('status');

      expect(result.code).toBe(0);
      expect(result.data).toBeDefined();
      expect(result.data.state).toBe('disconnected');
      expect(result.data.running).toBe(false);
    });

    it('test_k2_run_status_with_error_maps_to_ControlError', async () => {
      const { injectCapacitorGlobals } = await import('../capacitor-k2');
      await injectCapacitorGlobals();

      mockK2Plugin.getStatus.mockResolvedValue({
        state: 'disconnected',
        error: 'Connection timed out',
      });

      const result = await window._k2.run('status');

      expect(result.code).toBe(0);
      expect(result.data).toBeDefined();
      expect(result.data.state).toBe('error');
      expect(result.data.error).toBeDefined();
      expect(result.data.error.code).toBe(570);
      expect(typeof result.data.error.message).toBe('string');
    });

    it('test_k2_run_up_calls_connect_with_config', async () => {
      const { injectCapacitorGlobals } = await import('../capacitor-k2');
      await injectCapacitorGlobals();

      mockK2Plugin.connect.mockResolvedValue(undefined);

      const config = { server: 'k2v5://example.com' };
      const result = await window._k2.run('up', config);

      expect(mockK2Plugin.connect).toHaveBeenCalledWith({
        config: JSON.stringify(config),
      });
      expect(result.code).toBe(0);
    });

    it('test_k2_run_up_without_config_returns_error', async () => {
      const { injectCapacitorGlobals } = await import('../capacitor-k2');
      await injectCapacitorGlobals();

      const result = await window._k2.run('up');

      expect(result.code).toBe(-1);
    });

    it('test_k2_run_down_calls_disconnect', async () => {
      const { injectCapacitorGlobals } = await import('../capacitor-k2');
      await injectCapacitorGlobals();

      mockK2Plugin.disconnect.mockResolvedValue(undefined);

      const result = await window._k2.run('down');

      expect(mockK2Plugin.disconnect).toHaveBeenCalled();
      expect(result.code).toBe(0);
    });

    it('test_k2_run_version_returns_version', async () => {
      const { injectCapacitorGlobals } = await import('../capacitor-k2');
      await injectCapacitorGlobals();

      mockK2Plugin.getVersion.mockResolvedValue({
        version: '0.4.0',
        go: '1.22',
        os: 'ios',
        arch: 'arm64',
      });

      const result = await window._k2.run('version');

      expect(result.code).toBe(0);
      expect(result.data).toBeDefined();
      expect(result.data.version).toBe('0.4.0');
    });

    it('test_k2_run_returns_error_on_exception', async () => {
      const { injectCapacitorGlobals } = await import('../capacitor-k2');
      await injectCapacitorGlobals();

      mockK2Plugin.getStatus.mockRejectedValue(new Error('Plugin not available'));

      const result = await window._k2.run('status');

      expect(result.code).toBe(-1);
      expect(result.message).toContain('Plugin not available');
    });

    it('test_capacitor_transformStatus_error_synthesis', async () => {
      const { injectCapacitorGlobals } = await import('../capacitor-k2');
      await injectCapacitorGlobals();

      mockK2Plugin.getStatus.mockResolvedValue({
        state: 'disconnected',
        error: 'DNS failed',
      });

      const result = await window._k2.run('status');
      expect(result.data.state).toBe('error');
      expect(result.data.error).toEqual({ code: 570, message: 'DNS failed' });
    });

    it('test_capacitor_transformStatus_disconnected_no_error', async () => {
      const { injectCapacitorGlobals } = await import('../capacitor-k2');
      await injectCapacitorGlobals();

      mockK2Plugin.getStatus.mockResolvedValue({
        state: 'disconnected',
      });

      const result = await window._k2.run('status');
      expect(result.data.state).toBe('disconnected');
      expect(result.data.error).toBeUndefined();
    });

    it('test_capacitor_transformStatus_connected_no_error', async () => {
      const { injectCapacitorGlobals } = await import('../capacitor-k2');
      await injectCapacitorGlobals();

      mockK2Plugin.getStatus.mockResolvedValue({
        state: 'connected',
        connectedAt: '2024-01-01T00:00:00Z',
      });

      const result = await window._k2.run('status');
      expect(result.data.state).toBe('connected');
      expect(result.data.error).toBeUndefined();
    });
  });

  describe('_platform', () => {
    it('test_platform_getUdid', async () => {
      const { injectCapacitorGlobals } = await import('../capacitor-k2');
      await injectCapacitorGlobals();

      mockK2Plugin.getUDID.mockResolvedValue({ udid: 'test-mobile-udid-123' });

      const udid = await window._platform.getUdid();

      expect(mockK2Plugin.getUDID).toHaveBeenCalled();
      expect(udid).toBe('test-mobile-udid-123');
    });
  });

  describe('getK2Source integration', () => {
    it('test_getK2Source_returns_capacitor', async () => {
      const { injectCapacitorGlobals } = await import('../capacitor-k2');
      await injectCapacitorGlobals();

      const { getK2Source } = await import('../standalone-k2');

      expect(getK2Source()).toBe('capacitor');
    });
  });
});

describe('standalone fallback (regression)', () => {
  let originalK2: any;
  let originalPlatform: any;

  beforeEach(() => {
    originalK2 = window._k2;
    originalPlatform = window._platform;
    delete (window as any)._k2;
    delete (window as any)._platform;
    vi.clearAllMocks();
    vi.resetModules();
  });

  afterEach(() => {
    (window as any)._k2 = originalK2;
    (window as any)._platform = originalPlatform;
  });

  it('test_standalone_still_works', async () => {
    // Without Capacitor native environment, standalone fallback should still work
    delete (window as any).__TAURI__;

    const { ensureK2Injected, getK2Source } = await import('../standalone-k2');
    ensureK2Injected();

    expect(window._k2).toBeDefined();
    expect(window._platform).toBeDefined();
    expect(getK2Source()).toBe('standalone');
  });
});
