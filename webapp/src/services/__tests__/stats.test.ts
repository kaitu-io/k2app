import { describe, it, expect, vi, beforeEach } from 'vitest';
import { cloudApi } from '../cloud-api';

// Mock cloudApi before importing stats
vi.mock('../cloud-api', () => ({
  cloudApi: {
    request: vi.fn().mockResolvedValue({ code: 0 }),
  },
}));

// Deterministic 36-char UUIDs (the crypto mock below returns a short fallback)
let uuidCounter = 0;
vi.mock('../../utils/uuid', () => ({
  randomUUID: vi.fn(() => `00000000-0000-4000-8000-${String(++uuidCounter).padStart(12, '0')}`),
}));

// Mock device-udid module
vi.mock('../device-udid', () => ({
  getDeviceUdid: vi.fn().mockResolvedValue('test-udid-123'),
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
  let seedFunnelOnceFlagsForExistingInstall: typeof import('../stats').seedFunnelOnceFlagsForExistingInstall;

  beforeEach(async () => {
    vi.resetModules();
    // Re-import to get fresh module state (clears _deviceHash cache)
    const mod = await import('../stats');
    statsService = mod.statsService;
    seedFunnelOnceFlagsForExistingInstall = mod.seedFunnelOnceFlagsForExistingInstall;

    mockStorage.clear();
    vi.clearAllMocks();
    // Re-set device-udid mock after clearAllMocks
    const { getDeviceUdid } = await import('../device-udid');
    vi.mocked(getDeviceUdid).mockResolvedValue('test-udid-123');
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

  it('uses fallback hash when getDeviceUdid fails', async () => {
    const { getDeviceUdid } = await import('../device-udid');
    vi.mocked(getDeviceUdid).mockRejectedValue(new Error('no UDID'));

    await statsService.trackAppOpen();
    await new Promise(r => setTimeout(r, 50));

    // Should still have flushed with 'unknown' as device hash
    expect(mockRequest).toHaveBeenCalledWith(
      'POST',
      '/api/stats/events',
      expect.objectContaining({
        app_opens: expect.arrayContaining([
          expect.objectContaining({
            device_hash: 'unknown',
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

  describe('funnel events', () => {
    const flushWait = () => new Promise(r => setTimeout(r, 50));
    const sentFunnel = () =>
      mockRequest.mock.calls.flatMap(c => (c[2] as any).funnel ?? []);

    it('trackFunnel queues and flushes with eid', async () => {
      await statsService.trackFunnel('paywall_view', { source: 'account' });
      await flushWait();

      const body = mockRequest.mock.calls[0][2] as any;
      expect(body.funnel).toHaveLength(1);
      const ev = body.funnel[0];
      expect(ev.event).toBe('paywall_view');
      expect(ev.eid).toHaveLength(36);
      expect(ev.device_hash).toBe('test-udid-123');
      expect(ev.os).toBe('macos');
      expect(ev.app_version).toBe('0.4.0');
      expect(new Date(ev.created_at).toISOString()).toBe(ev.created_at);
      expect(ev.source).toBe('account');
      expect('plan' in ev).toBe(false);
      expect('channel' in ev).toBe(false);
    });

    it('failed flush keeps funnel events and reuses eid', async () => {
      mockRequest.mockResolvedValueOnce({ code: 500, message: 'error' });
      await statsService.trackFunnel('login_view');
      await flushWait();
      expect(mockStorage.get('stats_queue').funnel).toHaveLength(1);

      await statsService.trackFunnel('plan_select', { plan: 'pro' });
      await flushWait();

      const first = mockRequest.mock.calls[0][2] as any;
      const second = mockRequest.mock.calls[1][2] as any;
      const loginFirst = first.funnel.find((e: any) => e.event === 'login_view');
      const loginSecond = second.funnel.find((e: any) => e.event === 'login_view');
      expect(loginSecond.eid).toBe(loginFirst.eid);
      expect(second.funnel).toHaveLength(2);
    });

    it('events queued during an in-flight flush survive', async () => {
      let resolveFirst!: (v: any) => void;
      mockRequest.mockImplementationOnce(
        () => new Promise(res => { resolveFirst = res; })
      );

      await statsService.trackFunnel('login_view');
      await vi.waitFor(() => expect(mockRequest).toHaveBeenCalledTimes(1));

      await statsService.trackFunnel('paywall_view');
      expect(mockStorage.get('stats_queue').funnel).toHaveLength(2);

      resolveFirst({ code: 0 });
      await flushWait();

      const remaining = mockStorage.get('stats_queue');
      expect(remaining.funnel).toHaveLength(1);
      expect(remaining.funnel[0].event).toBe('paywall_view');

      await statsService.trackFunnel('plan_select');
      await flushWait();
      // Next flush sends the survivor (+ the new one), never the already-sent first event
      const events = (mockRequest.mock.calls[1][2] as any).funnel.map((e: any) => e.event);
      expect(events).toEqual(['paywall_view', 'plan_select']);
    });

    it('legacy persisted queue without funnel field works', async () => {
      mockStorage.set('stats_queue', { app_opens: [], connections: [] });
      await expect(statsService.trackFunnel('login_view')).resolves.toBeUndefined();
      await flushWait();
      expect(sentFunnel()).toHaveLength(1);
    });

    it('trackFunnelOnce sends once across calls', async () => {
      await statsService.trackFunnelOnce('app_first_open');
      await statsService.trackFunnelOnce('app_first_open');
      await statsService.trackFunnelOnce('app_first_open');
      await flushWait();

      expect(sentFunnel().filter((e: any) => e.event === 'app_first_open')).toHaveLength(1);
      expect(mockStorage.get('funnel_once:app_first_open')).toBeTruthy();
    });

    it('trackFunnel never throws when storage is missing', async () => {
      const platform = window._platform as any;
      const saved = platform.storage;
      platform.storage = undefined;
      try {
        await expect(statsService.trackFunnel('login_view')).resolves.toBeUndefined();
        await expect(statsService.trackFunnelOnce('login_view')).resolves.toBeUndefined();
      } finally {
        platform.storage = saved;
      }
    });

    it('two concurrent trackFunnel calls both end up sent exactly once', async () => {
      await Promise.all([
        statsService.trackFunnel('login_view'),
        statsService.trackFunnel('paywall_view'),
      ]);
      await flushWait();
      await statsService.trackFunnel('plan_select'); // trigger a flush for any survivor
      await flushWait();

      const names = sentFunnel().map((e: any) => e.event).sort();
      expect(names).toEqual(['login_view', 'paywall_view', 'plan_select']);
    });

    it('an enqueue racing the post-flush trim loses nothing and resends nothing', async () => {
      const delay = () => new Promise(r => setTimeout(r, 10));
      (window._platform!.storage.get as any).mockImplementation(async (key: string) => {
        const v = mockStorage.get(key) ?? null; // value is read at call time, delivered late
        await delay();
        return v === null ? null : structuredClone(v);
      });
      (window._platform!.storage.remove as any).mockImplementation(async (key: string) => {
        await delay();
        mockStorage.delete(key);
      });
      (window._platform!.storage.set as any).mockImplementation(async (key: string, value: any) => {
        await delay();
        mockStorage.set(key, structuredClone(value));
      });

      let resolveFirst!: (v: any) => void;
      mockRequest.mockImplementationOnce(() => new Promise(res => { resolveFirst = res; }));

      await statsService.trackFunnel('login_view');
      await vi.waitFor(() => expect(mockRequest).toHaveBeenCalledTimes(1));

      resolveFirst({ code: 0 });
      // Enqueue while the trim is reading/writing
      const racing = statsService.trackFunnel('paywall_view');
      await racing;
      await new Promise(r => setTimeout(r, 300));
      await statsService.trackFunnel('plan_select');
      await new Promise(r => setTimeout(r, 300));

      const names = sentFunnel().map((e: any) => e.event).sort();
      expect(names).toEqual(['login_view', 'paywall_view', 'plan_select']);
    });

    it('trackFunnelOnce does not set the flag when the queue write failed', async () => {
      (window._platform!.storage.set as any).mockImplementation(async (key: string, value: any) => {
        if (key === 'stats_queue') throw new Error('disk full');
        mockStorage.set(key, value);
      });
      await statsService.trackFunnelOnce('app_first_open');
      await flushWait();
      expect(mockStorage.get('funnel_once:app_first_open')).toBeUndefined();
      expect(sentFunnel()).toHaveLength(0);

      (window._platform!.storage.set as any).mockImplementation(
        async (key: string, value: any) => { mockStorage.set(key, value); }
      );
      await statsService.trackFunnelOnce('app_first_open');
      await flushWait();
      expect(sentFunnel().map((e: any) => e.event)).toEqual(['app_first_open']);
      expect(mockStorage.get('funnel_once:app_first_open')).toBeTruthy();
    });

    it('concurrent trackFunnelOnce calls for one event enqueue once', async () => {
      await Promise.all([
        statsService.trackFunnelOnce('app_first_open'),
        statsService.trackFunnelOnce('app_first_open'),
      ]);
      await flushWait();
      expect(sentFunnel().filter((e: any) => e.event === 'app_first_open')).toHaveLength(1);
    });
  });

  describe('existing-install seeding', () => {
    const flushWait = () => new Promise(r => setTimeout(r, 50));
    const sentFunnel = () =>
      mockRequest.mock.calls.flatMap(c => (c[2] as any).funnel ?? []);
    const ONCE = ['app_first_open', 'first_connect_attempt', 'first_connect_ok'] as const;

    it('existing install: flags set, nothing queued, later once-events send nothing', async () => {
      mockStorage.set('device-udid', 'raw-uuid-from-an-older-version');

      await seedFunnelOnceFlagsForExistingInstall();
      await flushWait();

      for (const ev of ONCE) expect(mockStorage.get(`funnel_once:${ev}`)).toBeTruthy();
      expect(mockStorage.get('stats_queue')).toBeUndefined();
      expect(mockRequest).not.toHaveBeenCalled();

      for (const ev of ONCE) await statsService.trackFunnelOnce(ev);
      await flushWait();
      expect(sentFunnel()).toHaveLength(0);
      expect(mockRequest).not.toHaveBeenCalled();
    });

    it('fresh install: no flags seeded, app_first_open sent exactly once', async () => {
      await seedFunnelOnceFlagsForExistingInstall();
      for (const ev of ONCE) expect(mockStorage.get(`funnel_once:${ev}`)).toBeUndefined();

      await statsService.trackFunnelOnce('app_first_open');
      await statsService.trackFunnelOnce('app_first_open');
      await flushWait();
      expect(sentFunnel().map((e: any) => e.event)).toEqual(['app_first_open']);
    });

    it('fresh install whose first enqueue failed is not reclassified as existing next launch', async () => {
      await seedFunnelOnceFlagsForExistingInstall(); // launch 1: no device-udid yet
      mockStorage.set('device-udid', 'created-during-launch-1'); // getDeviceUdid() ran, enqueue failed

      await seedFunnelOnceFlagsForExistingInstall(); // launch 2
      expect(mockStorage.get('funnel_once:app_first_open')).toBeUndefined();

      await statsService.trackFunnelOnce('app_first_open');
      await flushWait();
      expect(sentFunnel().map((e: any) => e.event)).toEqual(['app_first_open']);
    });

    it('install that already has the once flag is left alone', async () => {
      mockStorage.set('device-udid', 'raw');
      mockStorage.set('funnel_once:app_first_open', true);
      await seedFunnelOnceFlagsForExistingInstall();
      expect(mockStorage.get('funnel_once:first_connect_ok')).toBeUndefined();
    });

    it('storage unavailable: no throw, no flags', async () => {
      const platform = window._platform as any;
      const saved = platform.storage;
      platform.storage = undefined;
      try {
        await expect(seedFunnelOnceFlagsForExistingInstall()).resolves.toBeUndefined();
      } finally {
        platform.storage = saved;
      }
      expect([...mockStorage.keys()]).toEqual([]);

      (window._platform!.storage.get as any).mockImplementation(async () => {
        throw new Error('storage broken');
      });
      await expect(seedFunnelOnceFlagsForExistingInstall()).resolves.toBeUndefined();
      expect([...mockStorage.keys()]).toEqual([]);
    });
  });
});
