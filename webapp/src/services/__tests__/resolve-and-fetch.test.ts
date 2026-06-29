import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';

vi.mock('../antiblock', () => ({ resolveEntry: vi.fn().mockResolvedValue('https://k2.52j.me') }));

import { resolveAndFetch, CONTROL_PLANE_HOST } from '../resolve-and-fetch';
import { resolveEntry } from '../antiblock';
import * as pool from '../entry-pool';
import type { NodeEntry } from '../node-descriptor';

const mockedResolveEntry = vi.mocked(resolveEntry);
const N = (ip: string): NodeEntry => ({ ip, pin: 'sha256:' + ip, ech: 'E' + ip });

// Transport order is RELAY-FIRST, direct-fallback (see resolveAndFetch). Relay
// is the primary path where supported; direct is the safety net and the only
// path on web/mobile (learned via a relay code:-1).
describe('resolveAndFetch (relay-first, direct-fallback)', () => {
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

  it('relay success is returned verbatim and direct is never attempted', async () => {
    globalThis.fetch = vi.fn() as any; // must NOT be called when relay wins
    pool.addNodes([N('1.1.1.1'), N('2.2.2.2')]);
    (window as any)._k2.run = vi.fn().mockResolvedValue({ code: 0, message: 'ok', data: { status: 200, headers: {}, body: '{"ok":true}' } });
    const res = await resolveAndFetch({ method: 'GET', path: '/api/tunnels', headers: { Authorization: 'Bearer t' } });
    expect(res.transport).toBe('ok');
    if (res.transport === 'ok') {
      expect(res.status).toBe(200);
      expect(await res.json()).toEqual({ ok: true });
    }
    expect((window as any)._k2.run).toHaveBeenCalledWith('relay-fetch', expect.objectContaining({
      centerHost: CONTROL_PLANE_HOST, method: 'GET', path: '/api/tunnels', headers: { Authorization: 'Bearer t' },
    }));
    expect(globalThis.fetch).not.toHaveBeenCalled(); // relay won → no direct
  });

  it('relay non-2xx (401) is returned verbatim as transport:ok, no failover, no direct', async () => {
    // The node WORKED — cloud-api._handle401 owns the refresh. Treating 401 as a
    // node failure would (with one node) yield transport:fail and lose refresh.
    globalThis.fetch = vi.fn() as any;
    pool.addNodes([N('1.1.1.1')]);
    (window as any)._k2.run = vi.fn().mockResolvedValue({ code: 0, message: 'ok', data: { status: 401, headers: {}, body: '{"code":401}' } });
    const res = await resolveAndFetch({ method: 'GET', path: '/api/user/info', headers: { Authorization: 'Bearer expired' } });
    expect(res.transport).toBe('ok');
    if (res.transport === 'ok') {
      expect(res.status).toBe(401);
      expect(await res.json()).toEqual({ code: 401 });
    }
    expect((window as any)._k2.run).toHaveBeenCalledTimes(1); // success-passthrough, no failover
    expect(globalThis.fetch).not.toHaveBeenCalled();
  });

  it('relay node failure (502) falls back to direct', async () => {
    globalThis.fetch = vi.fn().mockResolvedValue({ status: 200, json: async () => ({ code: 0, data: 1 }) }) as any;
    pool.addNodes([N('1.1.1.1')]);
    (window as any)._k2.run = vi.fn().mockResolvedValue({ code: 502, message: 'relay failed' });
    const res = await resolveAndFetch({ method: 'GET', path: '/api/x', headers: {} });
    expect(res.transport).toBe('ok');
    if (res.transport === 'ok') {
      expect(res.status).toBe(200);
      expect(await res.json()).toEqual({ code: 0, data: 1 });
    }
    expect((window as any)._k2.run).toHaveBeenCalled(); // relay tried first
    expect(globalThis.fetch).toHaveBeenCalledWith('https://k2.52j.me/api/x', expect.objectContaining({ method: 'GET' }));
  });

  it('relay unsupported (code:-1) is learned: this request uses direct, the next skips relay entirely', async () => {
    globalThis.fetch = vi.fn().mockResolvedValue({ status: 200, json: async () => ({ ok: 1 }) }) as any;
    pool.addNodes([N('1.1.1.1')]);
    (window as any)._k2.run = vi.fn().mockResolvedValue({ code: -1, message: 'relay unsupported on this build' });
    // 1st request: relay attempted once (returns -1) → direct
    const res1 = await resolveAndFetch({ method: 'GET', path: '/api/a', headers: {} });
    expect(res1.transport).toBe('ok');
    expect((window as any)._k2.run).toHaveBeenCalledTimes(1);
    expect(pool.isRelaySupported()).toBe(false);
    // 2nd request: relay skipped entirely → direct only, no new _k2 call
    const res2 = await resolveAndFetch({ method: 'GET', path: '/api/b', headers: {} });
    expect(res2.transport).toBe('ok');
    expect((window as any)._k2.run).toHaveBeenCalledTimes(1); // still 1 — relay not retried
  });

  it('empty node pool: relay short-circuits without an _k2 call, falls back to direct', async () => {
    globalThis.fetch = vi.fn().mockResolvedValue({ status: 200, json: async () => ({ ok: 1 }) }) as any;
    const res = await resolveAndFetch({ method: 'GET', path: '/api/x', headers: {} });
    expect(res.transport).toBe('ok');
    expect((window as any)._k2.run).not.toHaveBeenCalled(); // tryRelay short-circuits on empty pool
    expect(globalThis.fetch).toHaveBeenCalled();
  });

  it('direct fallback returns 4xx/5xx verbatim (does not error)', async () => {
    globalThis.fetch = vi.fn().mockResolvedValue({ status: 500, json: async () => ({ code: -1 }) }) as any;
    const res = await resolveAndFetch({ method: 'GET', path: '/api/x', headers: {} }); // empty pool → direct
    expect(res.transport).toBe('ok');
    if (res.transport === 'ok') expect(res.status).toBe(500);
  });

  it('relay fails and direct fails → transport:fail', async () => {
    globalThis.fetch = vi.fn().mockRejectedValue(new TypeError('blocked')) as any;
    pool.addNodes([N('1.1.1.1')]);
    (window as any)._k2.run = vi.fn().mockResolvedValue({ code: 502, message: 'relay failed' });
    const res = await resolveAndFetch({ method: 'GET', path: '/api/x', headers: {} });
    expect(res.transport).toBe('fail');
    expect((window as any)._k2.run).toHaveBeenCalled();
    expect(globalThis.fetch).toHaveBeenCalled();
  });

  it('relay node that hangs is abandoned at 9s (< 15s budget) then falls back to direct', async () => {
    // Budget guard: RELAY_TIMEOUT_MS (9s) must leave room for the 5s direct probe
    // inside cloud-api's 15s REQUEST_TIMEOUT_MS. A hung relay must NOT eat the
    // whole budget, or the outer withTimeout fires before direct runs.
    vi.useFakeTimers();
    try {
      pool.addNodes([N('3.3.3.3')]);
      (window as any)._k2.run = vi.fn().mockReturnValue(new Promise<never>(() => {})); // never settles
      globalThis.fetch = vi.fn().mockResolvedValue({ status: 200, json: async () => ({ ok: 1 }) }) as any;
      const resPromise = resolveAndFetch({ method: 'GET', path: '/api/x', headers: {} });
      // Still pending just before the 9s relay timeout — direct not yet reached.
      await vi.advanceTimersByTimeAsync(8999);
      expect(globalThis.fetch).not.toHaveBeenCalled();
      // Trip the 9s relay timeout → relay abandoned → direct fallback runs.
      await vi.advanceTimersByTimeAsync(1);
      const res = await resPromise;
      expect(res.transport).toBe('ok'); // fell back to direct, well within the 15s cap
      expect(globalThis.fetch).toHaveBeenCalled();
    } finally {
      vi.useRealTimers();
    }
  });

  it('under cloud-api\'s 15s total cap, a hung relay still leaves room for the direct fallback to return (not timeout)', async () => {
    // Faithfully models cloud-api: withTimeout(resolveAndFetch(...), 15000).
    // Regression guard for the relay-first budget bug: if RELAY_TIMEOUT_MS were
    // >= the 15s cap, a hung relay would consume the whole budget and the outer
    // timeout would fire BEFORE the direct fallback ran.
    vi.useFakeTimers();
    try {
      pool.addNodes([N('4.4.4.4')]);
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
      expect(res).not.toBe(TIMEOUT);              // budget did NOT win
      expect((res as { transport: string }).transport).toBe('ok'); // direct fallback returned in time
    } finally {
      vi.useRealTimers();
    }
  });
});
