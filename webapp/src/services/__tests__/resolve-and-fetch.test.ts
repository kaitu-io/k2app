import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';

vi.mock('../antiblock', () => ({ resolveEntry: vi.fn().mockResolvedValue('https://k2.52j.me') }));
// 保活 relay-first 行为测试：kill-switch 默认 false，这里翻成 true。
// 默认关的行为见 resolve-and-fetch.relay-disabled.test.ts。
vi.mock('../relay-flag', () => ({ RELAY_ENABLED: true }));

import { resolveAndFetch, CONTROL_PLANE_HOST } from '../resolve-and-fetch';
import { resolveEntry } from '../antiblock';
import * as pool from '../entry-pool';

const mockedResolveEntry = vi.mocked(resolveEntry);

// Transport order is RELAY-FIRST, direct-fallback (see resolveAndFetch). Node
// selection / ranking / sequential failover / connection reuse all live in the Go
// RelayManager — the webapp sends ONE node-less relay-fetch and reads the
// {code,message,data} envelope: 0=ok (any HTTP status passthrough), 502=relay
// failed→direct, -1=unsupported→learn+direct, 503=transient→direct (relay stays on).
describe('resolveAndFetch (relay-first, direct-fallback, node selection in Go)', () => {
  let originalFetch: typeof globalThis.fetch;
  beforeEach(() => {
    originalFetch = globalThis.fetch;
    localStorage.clear();
    vi.clearAllMocks();
    pool.__resetRelaySupportForTest();
    mockedResolveEntry.mockResolvedValue('https://k2.52j.me');
    (window as any)._k2 = { run: vi.fn() };
  });
  afterEach(() => { globalThis.fetch = originalFetch; delete (window as any)._k2; });

  it('sends a node-less relay-fetch; success is returned verbatim and direct is never attempted', async () => {
    globalThis.fetch = vi.fn() as any; // must NOT be called when relay wins
    (window as any)._k2.run = vi.fn().mockResolvedValue({ code: 0, message: 'ok', data: { status: 200, headers: {}, body: '{"ok":true}' } });
    const res = await resolveAndFetch({ method: 'GET', path: '/api/tunnels', headers: { Authorization: 'Bearer t' } });
    expect(res.transport).toBe('ok');
    if (res.transport === 'ok') {
      expect(res.status).toBe(200);
      expect(await res.json()).toEqual({ ok: true });
    }
    // Node-less: exactly {centerHost,method,path,headers,body} — NO ip/pin/ech.
    expect((window as any)._k2.run).toHaveBeenCalledWith('relay-fetch', {
      centerHost: CONTROL_PLANE_HOST, method: 'GET', path: '/api/tunnels', headers: { Authorization: 'Bearer t' }, body: undefined,
    });
    expect(globalThis.fetch).not.toHaveBeenCalled();
  });

  it('relay non-2xx (401) is returned verbatim as transport:ok, no direct', async () => {
    // The relay WORKED — cloud-api._handle401 owns the refresh. Treating 401 as a
    // relay failure would fall to direct and (over a blocked network) lose refresh.
    globalThis.fetch = vi.fn() as any;
    (window as any)._k2.run = vi.fn().mockResolvedValue({ code: 0, message: 'ok', data: { status: 401, headers: {}, body: '{"code":401}' } });
    const res = await resolveAndFetch({ method: 'GET', path: '/api/user/info', headers: { Authorization: 'Bearer expired' } });
    expect(res.transport).toBe('ok');
    if (res.transport === 'ok') {
      expect(res.status).toBe(401);
      expect(await res.json()).toEqual({ code: 401 });
    }
    expect((window as any)._k2.run).toHaveBeenCalledTimes(1);
    expect(globalThis.fetch).not.toHaveBeenCalled();
  });

  it('relay failure (502: Go has no usable nodes / all exhausted) falls back to direct', async () => {
    globalThis.fetch = vi.fn().mockResolvedValue({ status: 200, json: async () => ({ code: 0, data: 1 }) }) as any;
    (window as any)._k2.run = vi.fn().mockResolvedValue({ code: 502, message: 'relay: no nodes available' });
    const res = await resolveAndFetch({ method: 'GET', path: '/api/x', headers: {} });
    expect(res.transport).toBe('ok');
    if (res.transport === 'ok') {
      expect(res.status).toBe(200);
      expect(await res.json()).toEqual({ code: 0, data: 1 });
    }
    expect((window as any)._k2.run).toHaveBeenCalled();
    expect(globalThis.fetch).toHaveBeenCalledWith('https://k2.52j.me/api/x', expect.objectContaining({ method: 'GET' }));
    expect(pool.isRelaySupported()).toBe(true); // 502 is a node fault, not a capability downgrade
  });

  it('relay unsupported (code:-1) is learned: this request uses direct, the next skips relay entirely', async () => {
    globalThis.fetch = vi.fn().mockResolvedValue({ status: 200, json: async () => ({ ok: 1 }) }) as any;
    (window as any)._k2.run = vi.fn().mockResolvedValue({ code: -1, message: 'relay unsupported on this build' });
    const res1 = await resolveAndFetch({ method: 'GET', path: '/api/a', headers: {} });
    expect(res1.transport).toBe('ok');
    expect((window as any)._k2.run).toHaveBeenCalledTimes(1);
    expect(pool.isRelaySupported()).toBe(false);
    // 2nd request: relay skipped entirely → direct only, no new _k2 call
    const res2 = await resolveAndFetch({ method: 'GET', path: '/api/b', headers: {} });
    expect(res2.transport).toBe('ok');
    expect((window as any)._k2.run).toHaveBeenCalledTimes(1);
  });

  it('transient relay code (503) does NOT disable relay: this request uses direct, the next still retries relay', async () => {
    // Regression guard for the mobile bind-race: a not-ready-yet native (Android
    // service not bound) returns TRANSIENT 503, never -1, so the session-scoped
    // relay capability must survive.
    globalThis.fetch = vi.fn().mockResolvedValue({ status: 200, json: async () => ({ ok: 1 }) }) as any;
    (window as any)._k2.run = vi.fn().mockResolvedValue({ code: 503, message: 'relay bridge not bound yet' });
    const res1 = await resolveAndFetch({ method: 'GET', path: '/api/a', headers: {} });
    expect(res1.transport).toBe('ok');
    expect(pool.isRelaySupported()).toBe(true);
    const callsAfterFirst = (window as any)._k2.run.mock.calls.length;
    const res2 = await resolveAndFetch({ method: 'GET', path: '/api/b', headers: {} });
    expect(res2.transport).toBe('ok');
    expect((window as any)._k2.run.mock.calls.length).toBeGreaterThan(callsAfterFirst);
  });

  it('no _k2 global: relay short-circuits to direct without throwing', async () => {
    delete (window as any)._k2;
    globalThis.fetch = vi.fn().mockResolvedValue({ status: 200, json: async () => ({ ok: 1 }) }) as any;
    const res = await resolveAndFetch({ method: 'GET', path: '/api/x', headers: {} });
    expect(res.transport).toBe('ok');
    expect(globalThis.fetch).toHaveBeenCalled();
  });

  it('direct fallback returns 4xx/5xx verbatim (does not error)', async () => {
    globalThis.fetch = vi.fn().mockResolvedValue({ status: 500, json: async () => ({ code: -1 }) }) as any;
    (window as any)._k2.run = vi.fn().mockResolvedValue({ code: 502, message: 'relay: no nodes available' });
    const res = await resolveAndFetch({ method: 'GET', path: '/api/x', headers: {} });
    expect(res.transport).toBe('ok');
    if (res.transport === 'ok') expect(res.status).toBe(500);
  });

  it('relay fails and direct fails → transport:fail', async () => {
    globalThis.fetch = vi.fn().mockRejectedValue(new TypeError('blocked')) as any;
    (window as any)._k2.run = vi.fn().mockResolvedValue({ code: 502, message: 'relay: all nodes exhausted' });
    const res = await resolveAndFetch({ method: 'GET', path: '/api/x', headers: {} });
    expect(res.transport).toBe('fail');
    expect((window as any)._k2.run).toHaveBeenCalled();
    expect(globalThis.fetch).toHaveBeenCalled();
  });

  it('relay that hangs is abandoned at 9s (< 15s budget) then falls back to direct', async () => {
    // Budget guard: RELAY_TIMEOUT_MS (9s) must leave room for the 5s direct probe
    // inside cloud-api's 15s cap. A hung relay-fetch IPC must NOT eat the whole
    // budget, or the outer withTimeout fires before direct runs.
    vi.useFakeTimers();
    try {
      (window as any)._k2.run = vi.fn().mockReturnValue(new Promise<never>(() => {})); // never settles
      globalThis.fetch = vi.fn().mockResolvedValue({ status: 200, json: async () => ({ ok: 1 }) }) as any;
      const resPromise = resolveAndFetch({ method: 'GET', path: '/api/x', headers: {} });
      await vi.advanceTimersByTimeAsync(8999);
      expect(globalThis.fetch).not.toHaveBeenCalled();
      await vi.advanceTimersByTimeAsync(1);
      const res = await resPromise;
      expect(res.transport).toBe('ok');
      expect(globalThis.fetch).toHaveBeenCalled();
    } finally {
      vi.useRealTimers();
    }
  });

  it('under cloud-api\'s 15s total cap, a hung relay still leaves room for the direct fallback to return', async () => {
    vi.useFakeTimers();
    try {
      (window as any)._k2.run = vi.fn().mockReturnValue(new Promise<never>(() => {})); // hangs forever
      globalThis.fetch = vi.fn().mockResolvedValue({ status: 200, json: async () => ({ ok: 1 }) }) as any;

      const TIMEOUT = Symbol('timeout');
      const budget = new Promise<typeof TIMEOUT>((r) => setTimeout(() => r(TIMEOUT), 15000));
      const race = Promise.race([
        resolveAndFetch({ method: 'GET', path: '/api/x', headers: {} }),
        budget,
      ]);

      await vi.advanceTimersByTimeAsync(15000);
      const res = await race;
      expect(res).not.toBe(TIMEOUT);
      expect((res as { transport: string }).transport).toBe('ok');
    } finally {
      vi.useRealTimers();
    }
  });
});
