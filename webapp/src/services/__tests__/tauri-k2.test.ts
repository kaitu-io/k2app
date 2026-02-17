import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';

// Mock @tauri-apps/api/core
vi.mock('@tauri-apps/api/core', () => ({
  invoke: vi.fn(),
}));

// Mock @tauri-apps/plugin-http
vi.mock('@tauri-apps/plugin-http', () => ({
  fetch: vi.fn(),
}));

// Mock @tauri-apps/plugin-shell
vi.mock('@tauri-apps/plugin-shell', () => ({
  open: vi.fn(),
}));

import { invoke } from '@tauri-apps/api/core';
import { fetch as tauriFetch } from '@tauri-apps/plugin-http';
import { injectTauriGlobals } from '../tauri-k2';
import { getK2Source } from '../standalone-k2';

const mockInvoke = vi.mocked(invoke);
const mockTauriFetch = vi.mocked(tauriFetch);

describe('tauri-k2', () => {
  let originalK2: any;
  let originalPlatform: any;
  let originalFetch: typeof window.fetch;

  beforeEach(() => {
    originalK2 = window._k2;
    originalPlatform = window._platform;
    originalFetch = window.fetch;
    delete (window as any)._k2;
    delete (window as any)._platform;
    vi.clearAllMocks();
  });

  afterEach(() => {
    (window as any)._k2 = originalK2;
    (window as any)._platform = originalPlatform;
    window.fetch = originalFetch;
  });

  describe('injectTauriGlobals', () => {
    beforeEach(async () => {
      mockInvoke.mockImplementation(async (cmd: string) => {
        if (cmd === 'get_platform_info') {
          return { os: 'macos', version: '0.4.0' };
        }
        return { code: 0, message: 'ok', data: {} };
      });
      await injectTauriGlobals();
    });

    it('sets window._k2 with run method', () => {
      expect(window._k2).toBeDefined();
      expect(typeof window._k2.run).toBe('function');
    });

    it('sets window._platform with correct desktop properties', () => {
      expect(window._platform).toBeDefined();
      expect(window._platform.os).toBe('macos');
      expect(window._platform.isDesktop).toBe(true);
      expect(window._platform.isMobile).toBe(false);
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

      expect(mockInvoke).toHaveBeenCalledWith('daemon_exec', {
        action: 'up',
        params: config,
      });
    });

    it('_k2.run returns error response on invoke failure', async () => {
      mockInvoke.mockRejectedValueOnce(new Error('Service unreachable'));

      const result = await window._k2.run('status');

      expect(result.code).toBe(-1);
      expect(result.message).toContain('Service unreachable');
    });

    it('_platform.nativeExec calls invoke with action', async () => {
      mockInvoke.mockResolvedValueOnce('Service installed and started');

      const result = await window._platform.nativeExec!('admin_reinstall_service');

      expect(mockInvoke).toHaveBeenCalledWith('admin_reinstall_service', {});
      expect(result).toBe('Service installed and started');
    });

    it('_platform.nativeExec passes params to invoke', async () => {
      mockInvoke.mockResolvedValueOnce('ok');

      await window._platform.nativeExec!('some_action', { key: 'val' });

      expect(mockInvoke).toHaveBeenCalledWith('some_action', { key: 'val' });
    });

    it('_platform.nativeExec rejects on invoke error', async () => {
      mockInvoke.mockRejectedValueOnce(new Error('User cancelled'));

      await expect(window._platform.nativeExec!('admin_reinstall_service'))
        .rejects.toThrow('User cancelled');
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
  });

  describe('transformStatus', () => {
    // Test transformStatus through the _k2.run('status') path
    // since transformStatus is not exported

    beforeEach(async () => {
      mockInvoke.mockImplementation(async (cmd: string) => {
        if (cmd === 'get_platform_info') {
          return { os: 'macos', version: '0.4.0' };
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

    it('maps stopped with error to error state', async () => {
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

  describe('fetch override', () => {
    beforeEach(async () => {
      mockInvoke.mockImplementation(async (cmd: string) => {
        if (cmd === 'get_platform_info') {
          return { os: 'macos', version: '0.4.0' };
        }
        return { code: 0, message: 'ok', data: {} };
      });
      await injectTauriGlobals();
    });

    it('routes external HTTPS through tauriFetch', async () => {
      const mockResponse = new Response('{}', { status: 200 });
      mockTauriFetch.mockResolvedValueOnce(mockResponse as any);

      await window.fetch('https://w.app.52j.me/api/tunnels');

      expect(mockTauriFetch).toHaveBeenCalledWith(
        'https://w.app.52j.me/api/tunnels',
        undefined,
      );
    });

    it('keeps relative paths on native fetch', async () => {
      mockTauriFetch.mockClear();

      // Relative URLs should NOT go through tauriFetch
      // They'll fail in test env (no server) but that's fine — we just verify routing
      await window.fetch('/core', { method: 'POST' }).catch(() => {});

      expect(mockTauriFetch).not.toHaveBeenCalled();
    });

    it('keeps loopback URLs on native fetch', async () => {
      mockTauriFetch.mockClear();

      await window.fetch('http://127.0.0.1:1777/ping').catch(() => {});

      expect(mockTauriFetch).not.toHaveBeenCalled();
    });
  });

  describe('getK2Source', () => {
    it('returns tauri after Tauri injection', async () => {
      mockInvoke.mockImplementation(async (cmd: string) => {
        if (cmd === 'get_platform_info') {
          return { os: 'macos', version: '0.4.0' };
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
