import { describe, it, expect, vi, beforeEach } from 'vitest';
import { cloudApi } from '../cloud-api';

// Mock cloudApi before importing stats
vi.mock('../cloud-api', () => ({
  cloudApi: {
    request: vi.fn().mockResolvedValue({ code: 0 }),
  },
}));

// Mock window._platform with typed ISecureStorage
const mockStorage = new Map<string, any>();
Object.defineProperty(window, '_platform', {
  value: {
    os: 'macos',
    version: '0.4.0',
    storage: {
      get: vi.fn(async (key: string) => mockStorage.get(key) ?? null),
      set: vi.fn(async (key: string, value: any) => { mockStorage.set(key, value); }),
      remove: vi.fn(async (key: string) => { mockStorage.delete(key); }),
    },
    getUdid: vi.fn(async () => 'test-udid-123'),
  },
  writable: true,
});

// Mock crypto.subtle for SHA-256 + randomUUID for fallback
Object.defineProperty(globalThis, 'crypto', {
  value: {
    subtle: {
      digest: vi.fn(async () => new ArrayBuffer(32)),
    },
    randomUUID: vi.fn(() => 'fallback-uuid-1234'),
  },
  writable: true,
});

const mockRequest = cloudApi.request as ReturnType<typeof vi.fn>;

describe('statsService', () => {
  // Re-import statsService fresh each test to reset module-level _deviceHash cache
  let statsService: typeof import('../stats').statsService;

  beforeEach(async () => {
    vi.resetModules();
    // Re-import to get fresh module state (clears _deviceHash cache)
    const mod = await import('../stats');
    statsService = mod.statsService;

    mockStorage.clear();
    vi.clearAllMocks();
    // Re-set mocks cleared by vi.clearAllMocks()
    (window._platform!.storage.get as any).mockImplementation(
      async (key: string) => mockStorage.get(key) ?? null
    );
    (window._platform!.storage.set as any).mockImplementation(
      async (key: string, value: any) => { mockStorage.set(key, value); }
    );
    (window._platform!.storage.remove as any).mockImplementation(
      async (key: string) => { mockStorage.delete(key); }
    );
    (window._platform!.getUdid as any).mockResolvedValue('test-udid-123');
    mockRequest.mockResolvedValue({ code: 0 });
  });

  it('trackAppOpen queues event and flushes', async () => {
    await statsService.trackAppOpen();

    // Allow flush to complete
    await new Promise(r => setTimeout(r, 50));

    expect(mockRequest).toHaveBeenCalledWith(
      'POST',
      '/api/stats/events',
      expect.objectContaining({
        app_opens: expect.arrayContaining([
          expect.objectContaining({
            os: 'macos',
            app_version: '0.4.0',
          }),
        ]),
      })
    );
  });

  it('uses persistent fallback hash when getUdid fails', async () => {
    (window._platform!.getUdid as any).mockRejectedValue(new Error('no UDID'));

    await statsService.trackAppOpen();
    await new Promise(r => setTimeout(r, 50));

    // Should have generated and stored a fallback device ID
    expect(mockStorage.get('stats_device_id')).toBe('fallback-uuid-1234');

    // Should still have flushed with a hash (not "unknown")
    expect(mockRequest).toHaveBeenCalledWith(
      'POST',
      '/api/stats/events',
      expect.objectContaining({
        app_opens: expect.arrayContaining([
          expect.objectContaining({
            device_hash: expect.not.stringMatching(/^unknown$/),
          }),
        ]),
      })
    );
  });

  it('keeps events in queue on flush failure', async () => {
    mockRequest.mockResolvedValueOnce({ code: 500, message: 'error' });

    await statsService.trackAppOpen();
    await new Promise(r => setTimeout(r, 50));

    // Queue should still have the event (typed storage, no JSON.parse needed)
    const queue = mockStorage.get('stats_queue');
    expect(queue).toBeDefined();
    expect(queue.app_opens.length).toBeGreaterThan(0);
  });
});
