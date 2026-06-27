import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';

vi.mock('../antiblock', () => ({ resolveEntry: vi.fn().mockResolvedValue('https://k2.52j.me') }));

import { resolveAndFetch, CONTROL_PLANE_HOST } from '../resolve-and-fetch';
import { resolveEntry } from '../antiblock';
import * as pool from '../entry-pool';
import type { NodeEntry } from '../node-descriptor';

const mockedResolveEntry = vi.mocked(resolveEntry);
const N = (ip: string): NodeEntry => ({ ip, pin: 'sha256:' + ip, ech: 'E' + ip });

describe('resolveAndFetch', () => {
  let originalFetch: typeof globalThis.fetch;
  beforeEach(() => {
    originalFetch = globalThis.fetch;
    localStorage.clear();
    vi.clearAllMocks();
    mockedResolveEntry.mockResolvedValue('https://k2.52j.me');
    (window as any)._k2 = { run: vi.fn() };
  });
  afterEach(() => { globalThis.fetch = originalFetch; delete (window as any)._k2; });

  it('direct success returns the fetch response verbatim and clears sticky', async () => {
    pool.markDirectBlocked();
    pool.clearDirectBlocked(); // ensure not blocked
    globalThis.fetch = vi.fn().mockResolvedValue({ status: 200, json: async () => ({ code: 0, data: 1 }) }) as any;
    const res = await resolveAndFetch({ method: 'GET', path: '/api/x', headers: {} });
    expect(res.transport).toBe('ok');
    if (res.transport === 'ok') {
      expect(res.status).toBe(200);
      expect(await res.json()).toEqual({ code: 0, data: 1 });
    }
    expect(globalThis.fetch).toHaveBeenCalledWith('https://k2.52j.me/api/x', expect.objectContaining({ method: 'GET' }));
    expect((window as any)._k2.run).not.toHaveBeenCalled(); // no relay on direct success
  });

  it('direct returns 4xx/5xx as-is (does NOT fall back to relay)', async () => {
    globalThis.fetch = vi.fn().mockResolvedValue({ status: 500, json: async () => ({ code: -1 }) }) as any;
    const res = await resolveAndFetch({ method: 'GET', path: '/api/x', headers: {} });
    expect(res.transport).toBe('ok');
    if (res.transport === 'ok') expect(res.status).toBe(500);
    expect((window as any)._k2.run).not.toHaveBeenCalled();
  });

  it('direct connection failure falls back to relay (first node wins)', async () => {
    globalThis.fetch = vi.fn().mockRejectedValue(new TypeError('Load failed')) as any;
    pool.addNodes([N('1.1.1.1'), N('2.2.2.2')]);
    (window as any)._k2.run = vi.fn().mockResolvedValue({ code: 0, message: 'ok', data: { status: 200, headers: {}, body: '{"ok":true}' } });
    const res = await resolveAndFetch({ method: 'GET', path: '/api/tunnels', headers: { Authorization: 'Bearer t' } });
    expect(res.transport).toBe('ok');
    if (res.transport === 'ok') {
      expect(res.status).toBe(200);
      expect(await res.json()).toEqual({ ok: true });
    }
    // relay called with the control-plane host + path + headers
    expect((window as any)._k2.run).toHaveBeenCalledWith('relay-fetch', expect.objectContaining({
      centerHost: CONTROL_PLANE_HOST, method: 'GET', path: '/api/tunnels', headers: { Authorization: 'Bearer t' },
    }));
    expect(pool.isDirectBlocked()).toBe(true); // sticky set after direct failure
  });

  it('when direct is sticky-blocked, skips direct and goes straight to relay', async () => {
    globalThis.fetch = vi.fn() as any;
    pool.markDirectBlocked();
    pool.addNodes([N('1.1.1.1')]);
    (window as any)._k2.run = vi.fn().mockResolvedValue({ code: 0, data: { status: 200, body: '{}' } });
    const res = await resolveAndFetch({ method: 'GET', path: '/api/x', headers: {} });
    expect(res.transport).toBe('ok');
    expect(globalThis.fetch).not.toHaveBeenCalled();
  });

  it('all relay nodes failing yields transport:fail', async () => {
    globalThis.fetch = vi.fn().mockRejectedValue(new TypeError('blocked')) as any;
    pool.addNodes([N('1.1.1.1')]);
    (window as any)._k2.run = vi.fn().mockResolvedValue({ code: -1, message: 'relay unsupported on this build' });
    const res = await resolveAndFetch({ method: 'GET', path: '/api/x', headers: {} });
    expect(res.transport).toBe('fail');
  });

  it('relay with empty pool yields transport:fail (no _k2 call)', async () => {
    globalThis.fetch = vi.fn().mockRejectedValue(new TypeError('blocked')) as any;
    const res = await resolveAndFetch({ method: 'GET', path: '/api/x', headers: {} });
    expect(res.transport).toBe('fail');
    expect((window as any)._k2.run).not.toHaveBeenCalled();
  });

  it('relay node that never resolves times out after 15s and yields transport:fail', async () => {
    // Part A timeout test (RED before fix, GREEN after per-relay timeout added to relayOne)
    vi.useFakeTimers();
    try {
      // Direct is blocked so we go straight to relay
      pool.markDirectBlocked();
      pool.addNodes([N('3.3.3.3')]);
      // _k2.run returns a promise that NEVER settles — simulates hung IPC/daemon
      (window as any)._k2.run = vi.fn().mockReturnValue(new Promise<never>(() => {}));

      const resPromise = resolveAndFetch({ method: 'GET', path: '/api/x', headers: {} });

      // Advance to just before timeout — still pending
      await vi.advanceTimersByTimeAsync(14999);
      // Advance past the timeout
      await vi.advanceTimersByTimeAsync(1);

      const res = await resPromise;
      expect(res.transport).toBe('fail');
    } finally {
      vi.useRealTimers();
    }
  });
});
