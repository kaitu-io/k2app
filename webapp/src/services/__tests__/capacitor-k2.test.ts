/**
 * Capacitor K2 Bridge Tests
 *
 * Tests for the Capacitor mobile bridge (capacitor-k2.ts) which injects:
 *   window._k2      = { run(action, params) }     (VPN control via K2Plugin)
 *   window._platform = { os, version, storage, getUdid, ... }
 */

import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { Browser } from '@capacitor/browser';
import { Clipboard } from '@capacitor/clipboard';
import { Capacitor } from '@capacitor/core';

// Mock @capacitor/core
vi.mock('@capacitor/core', () => ({
  Capacitor: {
    isNativePlatform: vi.fn(() => true),
    getPlatform: vi.fn(() => 'ios'),
  },
}));

// Mock @capacitor/browser
vi.mock('@capacitor/browser', () => ({
  Browser: {
    open: vi.fn(),
  },
}));

// Mock @capacitor/clipboard
vi.mock('@capacitor/clipboard', () => ({
  Clipboard: {
    write: vi.fn(),
    read: vi.fn().mockResolvedValue({ type: 'text/plain', value: '' }),
  },
}));

// Mock k2-plugin — only methods the bridge actually calls
const mockK2Plugin = {
  checkReady: vi.fn(),
  getUDID: vi.fn(),
  getVersion: vi.fn(),
  getStatus: vi.fn(),
  connect: vi.fn(),
  disconnect: vi.fn(),
  addListener: vi.fn(),
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

    it('test_k2_run_status_with_structured_error_maps_to_ControlError', async () => {
      const { injectCapacitorGlobals } = await import('../capacitor-k2');
      await injectCapacitorGlobals();

      mockK2Plugin.getStatus.mockResolvedValue({
        state: 'disconnected',
        error: { code: 503, message: 'connection refused' },
      });

      const result = await window._k2.run('status');

      expect(result.code).toBe(0);
      expect(result.data).toBeDefined();
      expect(result.data.state).toBe('error');
      expect(result.data.error).toBeDefined();
      expect(result.data.error.code).toBe(503);
      expect(result.data.error.message).toBe('connection refused');
    });

    it('test_k2_run_status_with_string_error_backward_compat', async () => {
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
      expect(result.data.error.message).toBe('Connection timed out');
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

    it('test_capacitor_transformStatus_error_synthesis_structured', async () => {
      const { injectCapacitorGlobals } = await import('../capacitor-k2');
      await injectCapacitorGlobals();

      mockK2Plugin.getStatus.mockResolvedValue({
        state: 'disconnected',
        error: { code: 408, message: 'dial timeout' },
      });

      const result = await window._k2.run('status');
      expect(result.data.state).toBe('error');
      expect(result.data.error).toEqual({ code: 408, message: 'dial timeout' });
    });

    it('test_capacitor_transformStatus_error_synthesis_string_backward_compat', async () => {
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

    it('test_platform_openExternal_uses_browser_plugin', async () => {
      const { injectCapacitorGlobals } = await import('../capacitor-k2');
      await injectCapacitorGlobals();

      await window._platform.openExternal('https://example.com');

      expect(Browser.open).toHaveBeenCalledWith({ url: 'https://example.com' });
    });

    it('test_platform_writeClipboard_uses_clipboard_plugin', async () => {
      const { injectCapacitorGlobals } = await import('../capacitor-k2');
      await injectCapacitorGlobals();

      await window._platform.writeClipboard('hello-capacitor');

      expect(Clipboard.write).toHaveBeenCalledWith({ string: 'hello-capacitor' });
    });

    it('test_platform_readClipboard_uses_clipboard_plugin', async () => {
      const { injectCapacitorGlobals } = await import('../capacitor-k2');
      await injectCapacitorGlobals();

      vi.mocked(Clipboard.read).mockResolvedValueOnce({ type: 'text/plain', value: 'clipboard-data' });
      const result = await window._platform.readClipboard();

      expect(Clipboard.read).toHaveBeenCalled();
      expect(result).toBe('clipboard-data');
    });

    it('test_platform_syncLocale_is_noop', async () => {
      const { injectCapacitorGlobals } = await import('../capacitor-k2');
      await injectCapacitorGlobals();

      // syncLocale on mobile is a no-op — should not throw
      await expect(window._platform.syncLocale('zh-CN')).resolves.toBeUndefined();
    });
  });

  describe('updater', () => {
    /**
     * Helper: extract the callback registered for a given event name from addListener calls.
     * K2Plugin.addListener is called with (eventName, handler).
     */
    function getListenerCallback(eventName: string): ((data: any) => void) | undefined {
      const call = mockK2Plugin.addListener.mock.calls.find(
        (c: any[]) => c[0] === eventName,
      );
      return call ? call[1] : undefined;
    }

    it('test_injectCapacitorGlobals_sets_updater', async () => {
      const { injectCapacitorGlobals } = await import('../capacitor-k2');
      await injectCapacitorGlobals();

      expect(window._platform.updater).toBeDefined();
      expect(window._platform.updater).toBeTruthy();
    });

    it('test_updater_initial_state', async () => {
      const { injectCapacitorGlobals } = await import('../capacitor-k2');
      await injectCapacitorGlobals();

      const updater = window._platform.updater;
      expect(updater).toBeDefined();
      expect(updater!.isUpdateReady).toBe(false);
      expect(updater!.updateInfo).toBeNull();
      expect(updater!.isChecking).toBe(false);
      expect(updater!.error).toBeNull();
    });

    it('test_updater_handles_nativeUpdateReady_event', async () => {
      const { injectCapacitorGlobals } = await import('../capacitor-k2');
      await injectCapacitorGlobals();

      // Simulate K2Plugin emitting nativeUpdateReady (Android APK downloaded)
      const callback = getListenerCallback('nativeUpdateReady');
      expect(callback).toBeDefined();
      callback!({ version: '1.1.0', size: 12345, path: '/cache/app.apk' });

      const updater = window._platform.updater!;
      expect(updater.isUpdateReady).toBe(true);
      expect(updater.updateInfo).toBeDefined();
      expect(updater.updateInfo!.currentVersion).toBe('0.4.0');
      expect(updater.updateInfo!.newVersion).toBe('1.1.0');
    });

    it('test_updater_handles_nativeUpdateAvailable_event', async () => {
      const { injectCapacitorGlobals } = await import('../capacitor-k2');
      await injectCapacitorGlobals();

      // Simulate K2Plugin emitting nativeUpdateAvailable (iOS App Store)
      const callback = getListenerCallback('nativeUpdateAvailable');
      expect(callback).toBeDefined();
      callback!({ version: '1.1.0', appStoreUrl: 'https://apps.apple.com/app/id6759199298' });

      const updater = window._platform.updater!;
      expect(updater.isUpdateReady).toBe(true);
      expect(updater.updateInfo).toBeDefined();
      expect(updater.updateInfo!.newVersion).toBe('1.1.0');
    });

    it('test_applyUpdateNow_android_calls_installNativeUpdate', async () => {
      // Switch platform to android for this test
      vi.mocked(Capacitor.getPlatform).mockReturnValue('android');

      const { injectCapacitorGlobals } = await import('../capacitor-k2');
      await injectCapacitorGlobals();

      // Simulate nativeUpdateReady with a path (Android APK downloaded)
      const callback = getListenerCallback('nativeUpdateReady');
      expect(callback).toBeDefined();
      callback!({ version: '1.1.0', size: 12345, path: '/cache/app.apk' });

      mockK2Plugin.installNativeUpdate.mockResolvedValue(undefined);

      await window._platform.updater!.applyUpdateNow();

      expect(mockK2Plugin.installNativeUpdate).toHaveBeenCalledWith({ path: '/cache/app.apk' });

      // Restore platform to ios for other tests
      vi.mocked(Capacitor.getPlatform).mockReturnValue('ios');
    });

    it('test_applyUpdateNow_ios_opens_appstore', async () => {
      // Platform is ios (default mock)
      const { injectCapacitorGlobals } = await import('../capacitor-k2');
      await injectCapacitorGlobals();

      // Simulate nativeUpdateAvailable with App Store URL (iOS)
      const callback = getListenerCallback('nativeUpdateAvailable');
      expect(callback).toBeDefined();
      callback!({ version: '1.1.0', appStoreUrl: 'https://apps.apple.com/app/id6759199298' });

      await window._platform.updater!.applyUpdateNow();

      expect(Browser.open).toHaveBeenCalledWith({
        url: 'https://apps.apple.com/app/id6759199298',
      });
    });

    it('test_onUpdateReady_callback', async () => {
      const { injectCapacitorGlobals } = await import('../capacitor-k2');
      await injectCapacitorGlobals();

      const updater = window._platform.updater;
      expect(updater).toBeDefined();
      expect(updater!.onUpdateReady).toBeDefined();

      const onReadyCallback = vi.fn();
      updater!.onUpdateReady!(onReadyCallback);

      // Simulate nativeUpdateReady event
      const callback = getListenerCallback('nativeUpdateReady');
      expect(callback).toBeDefined();
      callback!({ version: '1.1.0', size: 12345, path: '/cache/app.apk' });

      expect(onReadyCallback).toHaveBeenCalledTimes(1);
      expect(onReadyCallback).toHaveBeenCalledWith(
        expect.objectContaining({
          newVersion: '1.1.0',
        }),
      );
    });

    it('test_onUpdateReady_returns_unsubscribe', async () => {
      const { injectCapacitorGlobals } = await import('../capacitor-k2');
      await injectCapacitorGlobals();

      const updater = window._platform.updater;
      expect(updater).toBeDefined();
      expect(updater!.onUpdateReady).toBeDefined();

      const onReadyCallback = vi.fn();
      const unsubscribe = updater!.onUpdateReady!(onReadyCallback);

      // Unsubscribe
      unsubscribe();

      // Simulate nativeUpdateReady event after unsubscribe
      const callback = getListenerCallback('nativeUpdateReady');
      expect(callback).toBeDefined();
      callback!({ version: '1.1.0', size: 12345, path: '/cache/app.apk' });

      // Callback should NOT have been invoked
      expect(onReadyCallback).not.toHaveBeenCalled();
    });

    it('test_web_ota_no_ui_notification', async () => {
      const { injectCapacitorGlobals } = await import('../capacitor-k2');
      await injectCapacitorGlobals();

      // Web OTA events (if emitted) should NOT set isUpdateReady
      // The bridge should NOT register a listener for web OTA that updates the updater state
      const updater = window._platform.updater;
      expect(updater).toBeDefined();

      // Even if there were a webOtaUpdate event, it shouldn't affect updater state
      const webOtaCallback = getListenerCallback('webOtaUpdate');
      if (webOtaCallback) {
        webOtaCallback({ version: '1.2.0' });
      }

      // Updater state should remain unchanged — web OTA is silent
      expect(updater!.isUpdateReady).toBe(false);
      expect(updater!.updateInfo).toBeNull();
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
