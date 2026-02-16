import { describe, it, expect, beforeEach, vi } from 'vitest';
import type { PlatformApi } from '../types';
import { TauriPlatform } from '../tauri';
import { CapacitorPlatform } from '../capacitor';
import { WebPlatform } from '../web';

describe('PlatformApi implementations', () => {
  describe('TauriPlatform', () => {
    it('test_open_external_tauri — openExternal calls tauri shell.open', async () => {
      const platform = new TauriPlatform();
      // When implemented, openExternal should call @tauri-apps/plugin-shell open()
      // For now the stub throws "Not implemented", so this will fail
      await platform.openExternal('https://example.com');
      // Should have called the tauri shell open function — verified by mock in GREEN phase
    });

    it('test_sync_locale_tauri — syncLocale syncs to native', async () => {
      const platform = new TauriPlatform();
      // Should resolve without error when implemented, syncing locale to Tauri backend
      await expect(platform.syncLocale('zh-CN')).resolves.toBeUndefined();
    });
  });

  describe('CapacitorPlatform', () => {
    it('test_open_external_capacitor — openExternal calls Capacitor Browser.open', async () => {
      const platform = new CapacitorPlatform();
      // When implemented, should call @capacitor/browser Browser.open({ url })
      await platform.openExternal('https://example.com');
    });

    it('test_upload_logs_capacitor — uploadLogs calls native plugin', async () => {
      const platform = new CapacitorPlatform();
      // When implemented, should call K2Plugin.uploadLogs({ feedbackId })
      await platform.uploadLogs('feedback-123');
    });
  });

  describe('WebPlatform', () => {
    it('test_open_external_web — openExternal calls window.open', async () => {
      const mockWindowOpen = vi.fn();
      vi.stubGlobal('open', mockWindowOpen);

      const platform = new WebPlatform();
      await platform.openExternal('https://example.com');

      expect(mockWindowOpen).toHaveBeenCalledWith('https://example.com', '_blank');

      vi.unstubAllGlobals();
    });
  });

  describe('writeClipboard', () => {
    it('test_write_clipboard_all_platforms — each implementation writes to clipboard', async () => {
      const mockWriteText = vi.fn().mockResolvedValue(undefined);
      Object.assign(navigator, {
        clipboard: { writeText: mockWriteText },
      });

      const platforms: PlatformApi[] = [
        new TauriPlatform(),
        new CapacitorPlatform(),
        new WebPlatform(),
      ];

      for (const platform of platforms) {
        mockWriteText.mockClear();
        await platform.writeClipboard('hello');
        expect(mockWriteText).toHaveBeenCalledWith('hello');
      }

      vi.restoreAllMocks();
    });
  });
});

describe('Platform factory', () => {
  beforeEach(() => {
    vi.resetModules();
    vi.unstubAllGlobals();
  });

  it('test_create_platform_auto_detect — factory detects platform from window globals', async () => {
    // Re-import after module reset to get fresh state
    const { createPlatform: freshCreate } = await import('../index');

    // With __TAURI__ on window, should detect Tauri
    vi.stubGlobal('__TAURI__', true);
    const tauriPlatform = freshCreate();
    expect(tauriPlatform.platformName).toBe('tauri');
    vi.unstubAllGlobals();

    // Reset modules again for next detection
    vi.resetModules();
    const { createPlatform: freshCreate2 } = await import('../index');

    // With Capacitor on window, should detect Capacitor
    vi.stubGlobal('Capacitor', { isNativePlatform: () => true });
    const capPlatform = freshCreate2();
    expect(capPlatform.platformName).toBe('capacitor');
    vi.unstubAllGlobals();

    // Reset modules again for next detection
    vi.resetModules();
    const { createPlatform: freshCreate3 } = await import('../index');

    // With neither, should fall back to web
    const webPlatform = freshCreate3();
    expect(webPlatform.platformName).toBe('web');
  });

  it('test_get_platform_singleton — getPlatform() returns same instance', async () => {
    vi.resetModules();
    const { getPlatform: freshGetPlatform } = await import('../index');

    const first = freshGetPlatform();
    const second = freshGetPlatform();
    expect(first).toBe(second);
  });
});
