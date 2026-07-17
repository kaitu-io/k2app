import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';

vi.mock('../antiblock', () => ({ resolveEntry: vi.fn().mockResolvedValue('https://k2.52j.me') }));

import { resolveAndFetch } from '../resolve-and-fetch';
import { resolveEntry } from '../antiblock';
import * as pool from '../entry-pool';

const mockedResolveEntry = vi.mocked(resolveEntry);

// RELAY_ENABLED=false（真实 flag 值）：直连是唯一传输。relay 代码保留但零调用。
describe('resolveAndFetch with relay disabled (kill-switch)', () => {
  let originalFetch: typeof globalThis.fetch;
  beforeEach(() => {
    originalFetch = globalThis.fetch;
    localStorage.clear();
    vi.clearAllMocks();
    pool.__resetRelaySupportForTest();
    mockedResolveEntry.mockResolvedValue('https://k2.52j.me');
    (window as any)._k2 = { run: vi.fn() };
  });
  afterEach(() => {
    globalThis.fetch = originalFetch;
    delete (window as any)._k2;
    vi.useRealTimers();
  });

  it('goes straight to direct fetch; relay-fetch IPC is never issued', async () => {
    globalThis.fetch = vi.fn().mockResolvedValue({ status: 200, json: async () => ({ ok: true }) }) as any;
    const res = await resolveAndFetch({ method: 'GET', path: '/api/tunnels', headers: { Authorization: 'Bearer t' } });
    expect(res.transport).toBe('ok');
    if (res.transport === 'ok') expect(res.status).toBe(200);
    expect((window as any)._k2.run).not.toHaveBeenCalled();
    expect(globalThis.fetch).toHaveBeenCalledWith('https://k2.52j.me/api/tunnels', expect.objectContaining({ method: 'GET' }));
  });

  it('direct failure returns transport:fail WITHOUT falling back to relay', async () => {
    globalThis.fetch = vi.fn().mockRejectedValue(new TypeError('network down')) as any;
    const res = await resolveAndFetch({ method: 'GET', path: '/api/user/info', headers: {} });
    expect(res.transport).toBe('fail');
    expect((window as any)._k2.run).not.toHaveBeenCalled();
  });

  it('direct owns a 14s budget (was 5s when relay was primary)', async () => {
    vi.useFakeTimers();
    let aborted = false;
    globalThis.fetch = vi.fn((_url: any, init: any) => new Promise((_resolve, reject) => {
      init.signal.addEventListener('abort', () => {
        aborted = true;
        reject(new DOMException('Aborted', 'AbortError'));
      });
    })) as any;
    const p = resolveAndFetch({ method: 'GET', path: '/api/x', headers: {} });
    await vi.advanceTimersByTimeAsync(13999);
    expect(aborted).toBe(false);
    await vi.advanceTimersByTimeAsync(2);
    const res = await p;
    expect(aborted).toBe(true);
    expect(res.transport).toBe('fail');
  });
});
