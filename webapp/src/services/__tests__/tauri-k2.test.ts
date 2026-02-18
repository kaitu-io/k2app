import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';

// Mock @tauri-apps/api/core
vi.mock('@tauri-apps/api/core', () => ({
  invoke: vi.fn(),
}));

// Mock @tauri-apps/api/event
vi.mock('@tauri-apps/api/event', () => ({
  listen: vi.fn().mockResolvedValue(() => {}),
}));

// Mock @tauri-apps/plugin-opener
vi.mock('@tauri-apps/plugin-opener', () => ({
  openUrl: vi.fn(),
}));

// Mock @tauri-apps/plugin-clipboard-manager
vi.mock('@tauri-apps/plugin-clipboard-manager', () => ({
  writeText: vi.fn(),
  readText: vi.fn().mockResolvedValue(''),
}));

import { invoke } from '@tauri-apps/api/core';
import { listen } from '@tauri-apps/api/event';
import { openUrl } from '@tauri-apps/plugin-opener';
import { writeText, readText } from '@tauri-apps/plugin-clipboard-manager';
import { injectTauriGlobals } from '../tauri-k2';
import { getK2Source } from '../standalone-k2';

const mockInvoke = vi.mocked(invoke);
const mockListen = vi.mocked(listen);
const mockOpenUrl = vi.mocked(openUrl);
const mockWriteText = vi.mocked(writeText);
const mockReadText = vi.mocked(readText);

describe('tauri-k2', () => {
  let originalK2: any;
  let originalPlatform: any;
  beforeEach(() => {
    originalK2 = window._k2;
    originalPlatform = window._platform;
    delete (window as any)._k2;
    delete (window as any)._platform;
    vi.clearAllMocks();
  });

  afterEach(() => {
    (window as any)._k2 = originalK2;
    (window as any)._platform = originalPlatform;
  });

  describe('injectTauriGlobals', () => {
    beforeEach(async () => {
      mockInvoke.mockImplementation(async (cmd: string) => {
        if (cmd === 'get_platform_info') {
          return { os: 'macos', version: '0.4.0' };
        }
        if (cmd === 'get_update_status') {
          return null; // No pending update by default
        }
        return { code: 0, message: 'ok', data: {} };
      });
      await injectTauriGlobals();
    });

    it('sets window._k2 with run method', () => {
      expect(window._k2).toBeDefined();
      expect(typeof window._k2.run).toBe('function');
    });

    it('sets window._platform with correct properties', () => {
      expect(window._platform).toBeDefined();
      expect(window._platform.os).toBe('macos');
      expect(window._platform.version).toBe('0.4.0');
    });

    it('_k2.run invokes daemon_exec IPC command', async () => {
      mockInvoke.mockResolvedValueOnce({
        code: 0,
        message: 'ok',
        data: { state: 'stopped' },
      });

      const result = await window._k2.run('status');

      expect(mockInvoke).toHaveBeenCalledWith('daemon_exec', {
        action: 'status',
        params: null,
      });
      expect(result.code).toBe(0);
      expect(result.data.state).toBe('disconnected');
      expect(result.data.running).toBe(false);
    });

    it('_k2.run passes params to daemon_exec', async () => {
      const config = { server: { wireUrl: 'test://url' } };
      mockInvoke.mockResolvedValueOnce({ code: 0, message: 'ok', data: {} });

      await window._k2.run('up', config);

      // Daemon handleUp expects params.config wrapping
      expect(mockInvoke).toHaveBeenCalledWith('daemon_exec', {
        action: 'up',
        params: { config },
      });
    });

    it('_k2.run down calls daemon_exec with action down', async () => {
      mockInvoke.mockResolvedValueOnce({ code: 0, message: 'ok', data: {} });

      const result = await window._k2.run('down');

      expect(mockInvoke).toHaveBeenCalledWith('daemon_exec', {
        action: 'down',
        params: null,
      });
      expect(result.code).toBe(0);
    });

    it('_k2.run version returns version data', async () => {
      mockInvoke.mockResolvedValueOnce({
        code: 0,
        message: 'ok',
        data: { version: '0.4.0' },
      });

      const result = await window._k2.run('version');

      expect(mockInvoke).toHaveBeenCalledWith('daemon_exec', {
        action: 'version',
        params: null,
      });
      expect(result.data.version).toBe('0.4.0');
    });

    it('_k2.run returns error response on invoke failure', async () => {
      mockInvoke.mockRejectedValueOnce(new Error('Service unreachable'));

      const result = await window._k2.run('status');

      expect(result.code).toBe(-1);
      expect(result.message).toContain('Service unreachable');
    });

    it('_platform.reinstallService calls admin_reinstall_service IPC', async () => {
      mockInvoke.mockResolvedValueOnce('ok');

      await window._platform.reinstallService!();

      expect(mockInvoke).toHaveBeenCalledWith('admin_reinstall_service');
    });

    it('_platform.getUdid invokes get_udid IPC command', async () => {
      mockInvoke.mockResolvedValueOnce({
        code: 0,
        message: 'ok',
        data: { udid: 'test-udid-123' },
      });

      const udid = await window._platform.getUdid();

      expect(mockInvoke).toHaveBeenCalledWith('get_udid');
      expect(udid).toBe('test-udid-123');
    });

    it('_platform.openExternal calls opener plugin', async () => {
      await window._platform.openExternal('https://example.com');
      expect(mockOpenUrl).toHaveBeenCalledWith('https://example.com');
    });

    it('_platform.writeClipboard calls clipboard-manager plugin', async () => {
      await window._platform.writeClipboard('hello');
      expect(mockWriteText).toHaveBeenCalledWith('hello');
    });

    it('_platform.readClipboard calls clipboard-manager plugin', async () => {
      mockReadText.mockResolvedValueOnce('clipboard-content');
      const result = await window._platform.readClipboard();
      expect(result).toBe('clipboard-content');
    });

    it('_platform.syncLocale calls sync_locale IPC', async () => {
      mockInvoke.mockResolvedValueOnce(null);
      await window._platform.syncLocale('zh-CN');
      expect(mockInvoke).toHaveBeenCalledWith('sync_locale', { locale: 'zh-CN' });
    });

    it('_platform.getPid calls get_pid IPC', async () => {
      mockInvoke.mockResolvedValueOnce(12345);
      const pid = await window._platform.getPid!();
      expect(mockInvoke).toHaveBeenCalledWith('get_pid');
      expect(pid).toBe(12345);
    });
  });

  describe('transformStatus', () => {
    // Test transformStatus through the _k2.run('status') path
    // since transformStatus is not exported

    beforeEach(async () => {
      mockInvoke.mockImplementation(async (cmd: string) => {
        if (cmd === 'get_platform_info') {
          return { os: 'macos', version: '0.4.0' };
        }
        if (cmd === 'get_update_status') {
          return null;
        }
        return { code: 0, message: 'ok', data: {} };
      });
      await injectTauriGlobals();
    });

    it('maps stopped to disconnected', async () => {
      mockInvoke.mockResolvedValueOnce({
        code: 0, message: 'ok',
        data: { state: 'stopped' },
      });
      const result = await window._k2.run('status');
      expect(result.data.state).toBe('disconnected');
      expect(result.data.running).toBe(false);
    });

    it('maps stopped with structured error to error state', async () => {
      mockInvoke.mockResolvedValueOnce({
        code: 0, message: 'ok',
        data: { state: 'stopped', error: { code: 503, message: 'connection refused' } },
      });
      const result = await window._k2.run('status');
      expect(result.data.state).toBe('error');
      expect(result.data.error).toEqual({ code: 503, message: 'connection refused' });
    });

    it('maps stopped with string error to error state (backward compat)', async () => {
      mockInvoke.mockResolvedValueOnce({
        code: 0, message: 'ok',
        data: { state: 'stopped', error: 'timeout' },
      });
      const result = await window._k2.run('status');
      expect(result.data.state).toBe('error');
      expect(result.data.error).toEqual({ code: 570, message: 'timeout' });
    });

    it('passes through connected state', async () => {
      mockInvoke.mockResolvedValueOnce({
        code: 0, message: 'ok',
        data: { state: 'connected', connected_at: '2024-01-01T00:00:00Z' },
      });
      const result = await window._k2.run('status');
      expect(result.data.state).toBe('connected');
      expect(result.data.running).toBe(true);
      expect(result.data.startAt).toBeDefined();
    });

    it('passes through connecting state', async () => {
      mockInvoke.mockResolvedValueOnce({
        code: 0, message: 'ok',
        data: { state: 'connecting' },
      });
      const result = await window._k2.run('status');
      expect(result.data.state).toBe('connecting');
      expect(result.data.running).toBe(true);
    });

    it('stopped without error stays disconnected', async () => {
      mockInvoke.mockResolvedValueOnce({
        code: 0, message: 'ok',
        data: { state: 'stopped' },
      });
      const result = await window._k2.run('status');
      expect(result.data.state).toBe('disconnected');
      expect(result.data.error).toBeUndefined();
    });

    it('connected state clears any error', async () => {
      mockInvoke.mockResolvedValueOnce({
        code: 0, message: 'ok',
        data: { state: 'connected', connected_at: '2024-01-01T00:00:00Z' },
      });
      const result = await window._k2.run('status');
      expect(result.data.state).toBe('connected');
      expect(result.data.error).toBeUndefined();
    });
  });

  describe('updater', () => {
    beforeEach(async () => {
      mockListen.mockResolvedValue(() => {});
      mockInvoke.mockImplementation(async (cmd: string) => {
        if (cmd === 'get_platform_info') {
          return { os: 'macos', version: '0.4.0' };
        }
        if (cmd === 'get_update_status') {
          return null;
        }
        return { code: 0, message: 'ok', data: {} };
      });
      await injectTauriGlobals();
    });

    it('updater exists with initial state', () => {
      expect(window._platform.updater).toBeDefined();
      expect(window._platform.updater!.isUpdateReady).toBe(false);
      expect(window._platform.updater!.updateInfo).toBeNull();
      expect(window._platform.updater!.isChecking).toBe(false);
      expect(window._platform.updater!.error).toBeNull();
    });

    it('updater initializes with existing update from Rust', async () => {
      // Re-inject with a pending update
      delete (window as any)._k2;
      delete (window as any)._platform;
      vi.clearAllMocks();

      mockListen.mockResolvedValue(() => {});
      mockInvoke.mockImplementation(async (cmd: string) => {
        if (cmd === 'get_platform_info') {
          return { os: 'macos', version: '0.3.0' };
        }
        if (cmd === 'get_update_status') {
          return { currentVersion: '0.3.0', newVersion: '0.4.0', releaseNotes: 'Bug fixes' };
        }
        return { code: 0, message: 'ok', data: {} };
      });

      await injectTauriGlobals();

      expect(window._platform.updater!.isUpdateReady).toBe(true);
      expect(window._platform.updater!.updateInfo).toEqual({
        currentVersion: '0.3.0',
        newVersion: '0.4.0',
        releaseNotes: 'Bug fixes',
      });
    });

    it('applyUpdateNow calls invoke', async () => {
      mockInvoke.mockResolvedValueOnce(undefined);
      await window._platform.updater!.applyUpdateNow();
      expect(mockInvoke).toHaveBeenCalledWith('apply_update_now');
    });

    it('checkUpdateManual sets isChecking and calls invoke', async () => {
      mockInvoke.mockResolvedValueOnce('Update 0.5.0 ready');

      const result = await window._platform.updater!.checkUpdateManual!();

      expect(mockInvoke).toHaveBeenCalledWith('check_update_now');
      expect(result).toBe('Update 0.5.0 ready');
      expect(window._platform.updater!.isChecking).toBe(false);
    });

    it('onUpdateReady registers listener via listen', () => {
      const callback = vi.fn();
      const unsub = window._platform.updater!.onUpdateReady!(callback);

      expect(mockListen).toHaveBeenCalledWith('update-ready', expect.any(Function));
      expect(typeof unsub).toBe('function');
    });
  });

  describe('uploadLogs', () => {
    beforeEach(async () => {
      mockInvoke.mockImplementation(async (cmd: string) => {
        if (cmd === 'get_platform_info') {
          return { os: 'macos', version: '0.4.0' };
        }
        if (cmd === 'get_update_status') {
          return null;
        }
        return { code: 0, message: 'ok', data: {} };
      });
      await injectTauriGlobals();
    });

    it('uploadLogs calls invoke with correct params', async () => {
      const params = {
        email: 'user@test.com',
        reason: 'connection failed',
        failureDurationMs: 5000,
        platform: 'macos',
        version: '0.4.0',
        feedbackId: 'fb-123',
      };
      mockInvoke.mockResolvedValueOnce({ success: true });

      const result = await window._platform.uploadLogs!(params);

      expect(mockInvoke).toHaveBeenCalledWith('upload_service_log_command', { params });
      expect(result.success).toBe(true);
    });
  });

  describe('getK2Source', () => {
    it('returns tauri after Tauri injection', async () => {
      mockInvoke.mockImplementation(async (cmd: string) => {
        if (cmd === 'get_platform_info') {
          return { os: 'macos', version: '0.4.0' };
        }
        if (cmd === 'get_update_status') {
          return null;
        }
        return { code: 0, message: 'ok', data: {} };
      });
      await injectTauriGlobals();

      expect(getK2Source()).toBe('tauri');
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
  });

  afterEach(() => {
    (window as any)._k2 = originalK2;
    (window as any)._platform = originalPlatform;
  });

  it('standalone injection still works without __TAURI__', async () => {
    delete (window as any).__TAURI__;

    const { ensureK2Injected, getK2Source } = await import('../standalone-k2');
    ensureK2Injected();

    expect(window._k2).toBeDefined();
    expect(window._platform).toBeDefined();
    expect(getK2Source()).toBe('standalone');
  });
});
