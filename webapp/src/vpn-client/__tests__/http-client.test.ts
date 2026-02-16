import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { HttpVpnClient } from '../http-client';

describe('HttpVpnClient', () => {
  let client: HttpVpnClient;
  let mockFetch: ReturnType<typeof vi.fn>;

  beforeEach(() => {
    mockFetch = vi.fn();
    vi.stubGlobal('fetch', mockFetch);
    client = new HttpVpnClient();
  });

  afterEach(() => {
    client.destroy();
    vi.restoreAllMocks();
  });

  describe('connect', () => {
    it('sends POST /api/core with action:up and wire_url', async () => {
      mockFetch.mockResolvedValueOnce({
        ok: true,
        json: () => Promise.resolve({ code: 0, message: 'ok' }),
      });

      await client.connect('wg://example.com/tunnel');

      expect(mockFetch).toHaveBeenCalledWith(
        expect.stringContaining('/api/core'),
        expect.objectContaining({
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({ action: 'up', params: { wire_url: 'wg://example.com/tunnel' } }),
        }),
      );
    });

    it('throws on non-ok response', async () => {
      mockFetch.mockResolvedValueOnce({
        ok: false,
        status: 500,
        statusText: 'Internal Server Error',
      });

      await expect(client.connect('wg://example.com')).rejects.toThrow();
    });
  });

  describe('disconnect', () => {
    it('sends POST /api/core with action:down', async () => {
      mockFetch.mockResolvedValueOnce({
        ok: true,
        json: () => Promise.resolve({ code: 0, message: 'ok' }),
      });

      await client.disconnect();

      expect(mockFetch).toHaveBeenCalledWith(
        expect.stringContaining('/api/core'),
        expect.objectContaining({
          method: 'POST',
          body: JSON.stringify({ action: 'down' }),
        }),
      );
    });
  });

  describe('getStatus', () => {
    it('sends action:status and maps response to VpnStatus', async () => {
      mockFetch.mockResolvedValueOnce({
        ok: true,
        json: () =>
          Promise.resolve({
            code: 0,
            message: 'ok',
            data: { state: 'connected', connectedAt: '2024-01-01T00:00:00Z', uptimeSeconds: 100 },
          }),
      });

      const status = await client.getStatus();

      expect(status.state).toBe('connected');
      expect(status.connectedAt).toBe('2024-01-01T00:00:00Z');
      expect(status.uptimeSeconds).toBe(100);
    });
  });

  describe('getVersion', () => {
    it('sends action:version and returns VersionInfo', async () => {
      mockFetch.mockResolvedValueOnce({
        ok: true,
        json: () =>
          Promise.resolve({
            code: 0,
            message: 'ok',
            data: { version: '1.0.0', go: '1.21', os: 'darwin', arch: 'arm64' },
          }),
      });

      const version = await client.getVersion();

      expect(version).toEqual({ version: '1.0.0', go: '1.21', os: 'darwin', arch: 'arm64' });
    });
  });

  describe('getUDID', () => {
    it('returns deviceId from status response', async () => {
      mockFetch.mockResolvedValueOnce({
        ok: true,
        json: () =>
          Promise.resolve({
            code: 0,
            message: 'ok',
            data: { deviceId: 'test-device-id', state: 'stopped' },
          }),
      });

      const udid = await client.getUDID();
      expect(udid).toBe('test-device-id');
    });

    it('generates UUID when deviceId is missing', async () => {
      mockFetch.mockResolvedValueOnce({
        ok: true,
        json: () =>
          Promise.resolve({
            code: 0,
            message: 'ok',
            data: { state: 'stopped' },
          }),
      });

      const udid = await client.getUDID();
      expect(udid).toBeTruthy();
      expect(typeof udid).toBe('string');
    });
  });

  describe('getConfig', () => {
    it('sends action:get_config', async () => {
      mockFetch.mockResolvedValueOnce({
        ok: true,
        json: () =>
          Promise.resolve({
            code: 0,
            message: 'ok',
            data: { wireUrl: 'wg://test', configPath: '/etc/config' },
          }),
      });

      const config = await client.getConfig();
      expect(config).toEqual({ wireUrl: 'wg://test', configPath: '/etc/config' });
    });
  });

  describe('checkReady', () => {
    it('returns ready:true with version when ping and version succeed', async () => {
      // First call: ping
      mockFetch.mockResolvedValueOnce({ ok: true });
      // Second call: version
      mockFetch.mockResolvedValueOnce({
        ok: true,
        json: () =>
          Promise.resolve({
            code: 0,
            message: 'ok',
            data: { version: '2.0.0', go: '1.21', os: 'darwin', arch: 'arm64' },
          }),
      });

      const ready = await client.checkReady();
      expect(ready).toEqual({ ready: true, version: '2.0.0' });
    });

    it('returns ready:false with not_running when ping fails', async () => {
      mockFetch.mockRejectedValueOnce(new Error('Connection refused'));

      const ready = await client.checkReady();
      expect(ready).toEqual({ ready: false, reason: 'not_running' });
    });
  });

  describe('baseUrl', () => {
    it('uses empty baseUrl in dev mode', async () => {
      mockFetch.mockResolvedValueOnce({
        ok: true,
        json: () => Promise.resolve({ code: 0, message: 'ok' }),
      });

      await client.connect('wg://test');

      expect(mockFetch).toHaveBeenCalledWith(
        '/api/core',
        expect.anything(),
      );
    });

    it('uses empty baseUrl when not Tauri and not dev (same-origin)', async () => {
      const origDev = import.meta.env.DEV;
      import.meta.env.DEV = false;
      delete (window as any).__TAURI__;
      try {
        const webClient = new HttpVpnClient();
        mockFetch.mockResolvedValueOnce({
          ok: true,
          json: () => Promise.resolve({ code: 0, message: 'ok' }),
        });
        await webClient.connect('wg://test');

        expect(mockFetch).toHaveBeenCalledWith(
          '/api/core',
          expect.anything(),
        );
        webClient.destroy();
      } finally {
        import.meta.env.DEV = origDev;
      }
    });

    it('uses absolute URL when Tauri and not dev', async () => {
      const origDev = import.meta.env.DEV;
      import.meta.env.DEV = false;
      (window as any).__TAURI__ = {};
      try {
        const tauriClient = new HttpVpnClient();
        mockFetch.mockResolvedValueOnce({
          ok: true,
          json: () => Promise.resolve({ code: 0, message: 'ok' }),
        });
        await tauriClient.connect('wg://test');

        expect(mockFetch).toHaveBeenCalledWith(
          'http://127.0.0.1:1777/api/core',
          expect.anything(),
        );
        tauriClient.destroy();
      } finally {
        delete (window as any).__TAURI__;
        import.meta.env.DEV = origDev;
      }
    });
  });

  describe('subscribe', () => {
    it('deduplicates identical state events', async () => {
      vi.useFakeTimers();
      const listener = vi.fn();

      // Mock getStatus to always return 'connected'
      mockFetch.mockResolvedValue({
        ok: true,
        json: () =>
          Promise.resolve({
            code: 0,
            message: 'ok',
            data: { state: 'connected' },
          }),
      });

      const unsubscribe = client.subscribe(listener);

      // Advance timers to trigger multiple polls
      await vi.advanceTimersByTimeAsync(2000);
      await vi.advanceTimersByTimeAsync(2000);
      await vi.advanceTimersByTimeAsync(2000);

      // Should only get one state_change event since state is always 'connected'
      const stateChangeEvents = listener.mock.calls.filter(
        (call) => call[0].type === 'state_change',
      );
      expect(stateChangeEvents.length).toBe(1);

      unsubscribe();
      vi.useRealTimers();
    });

    it('emits state_change when state changes', async () => {
      vi.useFakeTimers();
      const listener = vi.fn();

      let callCount = 0;
      mockFetch.mockImplementation(() => {
        callCount++;
        const state = callCount <= 1 ? 'connecting' : 'connected';
        return Promise.resolve({
          ok: true,
          json: () =>
            Promise.resolve({
              code: 0,
              message: 'ok',
              data: { state },
            }),
        });
      });

      const unsubscribe = client.subscribe(listener);

      await vi.advanceTimersByTimeAsync(2000);
      await vi.advanceTimersByTimeAsync(2000);

      const stateChangeEvents = listener.mock.calls.filter(
        (call) => call[0].type === 'state_change',
      );
      expect(stateChangeEvents.length).toBe(2);
      expect(stateChangeEvents[0]![0].state).toBe('connecting');
      expect(stateChangeEvents[1]![0].state).toBe('connected');

      unsubscribe();
      vi.useRealTimers();
    });

    it('returns unsubscribe function that stops polling when no listeners', () => {
      vi.useFakeTimers();

      mockFetch.mockResolvedValue({
        ok: true,
        json: () =>
          Promise.resolve({
            code: 0,
            message: 'ok',
            data: { state: 'stopped' },
          }),
      });

      const unsub = client.subscribe(() => {});
      unsub();

      // After unsubscribing, no more fetch calls should be made
      const callsBefore = mockFetch.mock.calls.length;
      vi.advanceTimersByTime(4000);
      expect(mockFetch.mock.calls.length).toBe(callsBefore);

      vi.useRealTimers();
    });
  });
});
