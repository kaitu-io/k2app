import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
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

      // The flush requested while the first was in flight runs as a follow-up:
      // it sends the survivor, never the already-sent first event.
      expect(mockRequest).toHaveBeenCalledTimes(2);
      const followUp = (mockRequest.mock.calls[1][2] as any).funnel.map((e: any) => e.event);
      expect(followUp).toEqual(['paywall_view']);
      expect(mockStorage.get('stats_queue')).toBeUndefined();

      await statsService.trackFunnel('plan_select');
      await flushWait();
      const events = (mockRequest.mock.calls[2][2] as any).funnel.map((e: any) => e.event);
      expect(events).toEqual(['plan_select']);
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

  describe('trackFunnelDaily', () => {
    const flushWait = () => new Promise(r => setTimeout(r, 50));
    const sent = (event: string) =>
      mockRequest.mock.calls.flatMap(c => (c[2] as any).funnel ?? []).filter((e: any) => e.event === event);

    afterEach(() => { vi.useRealTimers(); });
    const setNow = (iso: string) => {
      vi.useFakeTimers({ toFake: ['Date'] });
      vi.setSystemTime(new Date(iso));
    };

    it('reconnects on the same UTC day send connect_ok once', async () => {
      setNow('2026-10-01T08:00:00Z');
      await statsService.trackFunnelDaily('connect_ok');
      setNow('2026-10-01T23:59:59Z');
      await statsService.trackFunnelDaily('connect_ok');
      await statsService.trackFunnelDaily('connect_ok');
      await flushWait();
      expect(sent('connect_ok')).toHaveLength(1);
      expect(mockStorage.get('funnel_day:connect_ok')).toBe('2026-10-01');
    });

    it('day rollover (UTC) sends again', async () => {
      setNow('2026-10-01T23:59:59Z');
      await statsService.trackFunnelDaily('connect_ok');
      setNow('2026-10-02T00:00:01Z');
      await statsService.trackFunnelDaily('connect_ok');
      await statsService.trackFunnelDaily('connect_ok');
      await flushWait();
      expect(sent('connect_ok')).toHaveLength(2);
      expect(mockStorage.get('funnel_day:connect_ok')).toBe('2026-10-02');
    });

    it('existing installs still emit connect_ok (seeding only suppresses the once-events)', async () => {
      mockStorage.set('device-udid', 'raw-uuid-from-an-older-version');
      await seedFunnelOnceFlagsForExistingInstall();
      await statsService.trackFunnelOnce('first_connect_ok');
      await statsService.trackFunnelDaily('connect_ok');
      await flushWait();
      expect(sent('first_connect_ok')).toHaveLength(0);
      expect(sent('connect_ok')).toHaveLength(1);
    });

    it('concurrent calls enqueue once', async () => {
      await Promise.all([
        statsService.trackFunnelDaily('connect_ok'),
        statsService.trackFunnelDaily('connect_ok'),
      ]);
      await flushWait();
      expect(sent('connect_ok')).toHaveLength(1);
    });

    it('the day flag is not written when the queue write failed', async () => {
      (window._platform!.storage.set as any).mockImplementation(async (key: string, value: any) => {
        if (key === 'stats_queue') throw new Error('disk full');
        mockStorage.set(key, value);
      });
      await statsService.trackFunnelDaily('connect_ok');
      expect(mockStorage.get('funnel_day:connect_ok')).toBeUndefined();

      (window._platform!.storage.set as any).mockImplementation(
        async (key: string, value: any) => { mockStorage.set(key, value); }
      );
      await statsService.trackFunnelDaily('connect_ok');
      await flushWait();
      expect(sent('connect_ok')).toHaveLength(1);
    });

    it('never throws when storage is missing', async () => {
      const platform = window._platform as any;
      const saved = platform.storage;
      platform.storage = undefined;
      try {
        await expect(statsService.trackFunnelDaily('connect_ok')).resolves.toBeUndefined();
      } finally {
        platform.storage = saved;
      }
    });
  });

  describe('batching, cap and follow-up flush', () => {
    const flushWait = () => new Promise(r => setTimeout(r, 50));
    const sizeOf = (body: any) =>
      body.app_opens.length + body.connections.length + body.funnel.length;
    const ts = (i: number) => new Date(Date.UTC(2026, 8, 1, 0, 0, i)).toISOString();
    const funnelItem = (i: number) => ({
      eid: `eid-${i}`, device_hash: 'test-udid-123', os: 'macos', app_version: '0.4.0',
      event: 'paywall_view', created_at: ts(i),
    });
    const openItem = (i: number) => ({
      device_hash: 'test-udid-123', os: 'macos', app_version: '0.4.0', locale: 'en', created_at: ts(i),
    });
    const connItem = (i: number) => ({
      device_hash: 'test-udid-123', os: 'macos', app_version: '0.4.0', event: 'connect',
      node_type: 'cloud', node_ipv4: '', node_region: '', rule_mode: 'global',
      duration_sec: 0, disconnect_reason: '', created_at: ts(i),
    });

    /** 149 stored (mixed arrays) + the one trackFunnel() enqueues = 150. */
    const seed149 = () => {
      mockStorage.set('stats_queue', {
        app_opens: Array.from({ length: 30 }, (_, i) => openItem(i)),
        connections: Array.from({ length: 40 }, (_, i) => connItem(30 + i)),
        funnel: Array.from({ length: 79 }, (_, i) => funnelItem(70 + i)),
      });
    };

    it('150 queued events go out as two requests of 100 and 50, queue empty', async () => {
      seed149();
      await statsService.trackFunnel('login_view');
      await vi.waitFor(() => expect(mockRequest).toHaveBeenCalledTimes(2));
      await flushWait();

      expect(mockRequest).toHaveBeenCalledTimes(2);
      const [first, second] = mockRequest.mock.calls.map(c => c[2] as any);
      expect(sizeOf(first)).toBe(100);
      expect(sizeOf(second)).toBe(50);
      // Fixed order, oldest first: opens, then connections, then funnel.
      expect(first.app_opens).toHaveLength(30);
      expect(first.connections).toHaveLength(40);
      expect(first.funnel.map((e: any) => e.eid)).toEqual(
        Array.from({ length: 30 }, (_, i) => `eid-${70 + i}`),
      );
      expect(second.app_opens).toHaveLength(0);
      expect(second.connections).toHaveLength(0);
      expect(second.funnel[0].eid).toBe('eid-100');
      expect(second.funnel[49].event).toBe('login_view');
      expect(mockStorage.get('stats_queue')).toBeUndefined();
    });

    it('a failing second batch keeps exactly the unsent 50', async () => {
      seed149();
      mockRequest.mockResolvedValueOnce({ code: 0 }).mockResolvedValueOnce({ code: 500 });
      await statsService.trackFunnel('login_view');
      await vi.waitFor(() => expect(mockRequest).toHaveBeenCalledTimes(2));
      await flushWait();

      expect(mockRequest).toHaveBeenCalledTimes(2); // stopped on the first failure
      const left = mockStorage.get('stats_queue');
      expect(left.app_opens).toHaveLength(0);
      expect(left.connections).toHaveLength(0);
      expect(left.funnel).toHaveLength(50);
      expect(left.funnel[0].eid).toBe('eid-100');
      expect(left.funnel.map((e: any) => e.eid)).toEqual(
        (mockRequest.mock.calls[1][2] as any).funnel.map((e: any) => e.eid),
      );
    });

    it('a throwing request also stops the drain and keeps the queue', async () => {
      seed149();
      mockRequest.mockRejectedValueOnce(new Error('offline'));
      await statsService.trackFunnel('login_view');
      await flushWait();
      expect(mockRequest).toHaveBeenCalledTimes(1);
      const left = mockStorage.get('stats_queue');
      expect(left.app_opens.length + left.connections.length + left.funnel.length).toBe(150);
    });

    it('caps the stored queue at 500, dropping the oldest across arrays', async () => {
      mockRequest.mockResolvedValue({ code: 500 }); // nothing leaves the queue
      mockStorage.set('stats_queue', {
        // globally oldest two items are app_opens[0] (t=0) and funnel[0] (t=1)
        app_opens: [openItem(0), openItem(5)],
        connections: Array.from({ length: 198 }, (_, i) => connItem(10 + i)),
        funnel: [funnelItem(1), ...Array.from({ length: 299 }, (_, i) => funnelItem(300 + i))],
      });

      await statsService.trackFunnel('login_view');   // 501 → drops app_opens[0]
      await statsService.trackFunnel('plan_select');  // 501 → drops funnel[0]
      await flushWait();

      const q = mockStorage.get('stats_queue');
      expect(q.app_opens.length + q.connections.length + q.funnel.length).toBe(500);
      expect(q.app_opens.map((e: any) => e.created_at)).toEqual([ts(5)]);
      expect(q.connections).toHaveLength(198);
      expect(q.funnel[0].eid).toBe('eid-300');
      expect(q.funnel.slice(-2).map((e: any) => e.event)).toEqual(['login_view', 'plan_select']);
    });

    it('an already oversized stored queue is cut back to 500 on the next enqueue', async () => {
      mockRequest.mockResolvedValue({ code: 500 });
      mockStorage.set('stats_queue', {
        app_opens: [], connections: [],
        funnel: Array.from({ length: 800 }, (_, i) => funnelItem(i)),
      });
      await statsService.trackFunnel('login_view');
      await flushWait();
      const q = mockStorage.get('stats_queue');
      expect(q.funnel).toHaveLength(500);
      expect(q.funnel[0].eid).toBe('eid-301');
      expect(q.funnel[499].event).toBe('login_view');
    });

    it('cap eviction during an in-flight flush never trims unsent items', async () => {
      mockStorage.set('stats_queue', {
        app_opens: [], connections: [],
        funnel: Array.from({ length: 499 }, (_, i) => funnelItem(i)),
      });
      let resolveFirst!: (v: any) => void;
      mockRequest.mockImplementationOnce(() => new Promise(res => { resolveFirst = res; }));
      mockRequest.mockResolvedValue({ code: 500 }); // later batches fail → queue stays inspectable

      await statsService.trackFunnel('login_view'); // 500 stored; batch eid-0..eid-99 in flight
      await vi.waitFor(() => expect(mockRequest).toHaveBeenCalledTimes(1));
      // 3 more while in flight: the cap evicts eid-0..eid-2 (already in the in-flight batch)
      await statsService.trackFunnel('plan_select');
      await statsService.trackFunnel('plan_select');
      await statsService.trackFunnel('plan_select');
      expect(mockStorage.get('stats_queue').funnel).toHaveLength(500);

      resolveFirst({ code: 0 });
      await flushWait();

      const q = mockStorage.get('stats_queue');
      // 100 were sent; 3 of them were already evicted → only 97 more leave the queue.
      expect(q.funnel).toHaveLength(403);
      expect(q.funnel[0].eid).toBe('eid-100');
    });

    it('a flush requested during a flight triggers a follow-up for the new event', async () => {
      let resolveFirst!: (v: any) => void;
      mockRequest.mockImplementationOnce(() => new Promise(res => { resolveFirst = res; }));

      await statsService.trackFunnel('paywall_view');
      await vi.waitFor(() => expect(mockRequest).toHaveBeenCalledTimes(1));
      // e.g. checkout_start right before the external browser opens
      await statsService.trackFunnel('checkout_start', { plan: 'pro' });
      expect(mockRequest).toHaveBeenCalledTimes(1);

      resolveFirst({ code: 0 });
      await vi.waitFor(() => expect(mockRequest).toHaveBeenCalledTimes(2));
      await flushWait();

      expect((mockRequest.mock.calls[1][2] as any).funnel.map((e: any) => e.event)).toEqual(['checkout_start']);
      expect(mockRequest).toHaveBeenCalledTimes(2);
      expect(mockStorage.get('stats_queue')).toBeUndefined();
    });

    // The successful case above is also covered by the drain loop (items remain →
    // next batch). This is the case only the "requested during flight" flag covers:
    // the in-flight request FAILS, so the drain stops — the follow-up still runs once.
    it('a flush requested during a flight that fails still gets one follow-up', async () => {
      let resolveFirst!: (v: any) => void;
      mockRequest.mockImplementationOnce(() => new Promise(res => { resolveFirst = res; }));

      await statsService.trackFunnel('paywall_view');
      await vi.waitFor(() => expect(mockRequest).toHaveBeenCalledTimes(1));
      await statsService.trackFunnel('checkout_start', { plan: 'pro' });

      resolveFirst({ code: 500 });
      await flushWait();

      expect(mockRequest).toHaveBeenCalledTimes(2);
      expect((mockRequest.mock.calls[1][2] as any).funnel.map((e: any) => e.event))
        .toEqual(['paywall_view', 'checkout_start']);
      expect(mockStorage.get('stats_queue')).toBeUndefined();
    });

    it('a failed flush with nothing requested meanwhile is not retried on its own', async () => {
      mockRequest.mockResolvedValue({ code: 500 });
      await statsService.trackFunnel('paywall_view');
      await flushWait();
      expect(mockRequest).toHaveBeenCalledTimes(1);
    });

    it('no follow-up request when nothing was queued during the flight', async () => {
      await statsService.trackFunnel('paywall_view');
      await flushWait();
      expect(mockRequest).toHaveBeenCalledTimes(1);
    });
  });
});
