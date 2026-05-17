/**
 * standalone-k2 coreExec status normalization.
 *
 * Guards the bridge contract from webapp/CLAUDE.md "Bridge & VPN State Contract":
 * standalone-k2 must call transformStatus() on status responses so that
 * - daemon's legacy "stopped" rewrites to "disconnected"
 * - disconnected/connected + lastError synthesises state='error'
 * - non-status actions pass through unchanged.
 *
 * Without this, Linux desktop / k2r gateway users would never see error
 * overlays when the engine reports a fatal error in disconnected state.
 */

import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest';
import { standaloneK2 } from '../standalone-k2';

function mockFetchJson(body: any): void {
  global.fetch = vi.fn().mockResolvedValueOnce({
    ok: true,
    json: async () => body,
  }) as unknown as typeof fetch;
}

describe('standalone-k2 coreExec status transformation', () => {
  beforeEach(() => {
    vi.restoreAllMocks();
  });

  afterEach(() => {
    vi.restoreAllMocks();
  });

  it('rewrites legacy "stopped" state to "disconnected"', async () => {
    mockFetchJson({ code: 0, data: { state: 'stopped' } });
    const res = await standaloneK2.run<any>('status');
    expect(res.code).toBe(0);
    expect(res.data.state).toBe('disconnected');
  });

  it('synthesizes state="error" from disconnected + structured error', async () => {
    mockFetchJson({
      code: 0,
      data: {
        state: 'disconnected',
        error: { code: 570, message: 'wire dead' },
      },
    });
    const res = await standaloneK2.run<any>('status');
    expect(res.code).toBe(0);
    expect(res.data.state).toBe('error');
    expect(res.data.error).toEqual({ code: 570, message: 'wire dead' });
    expect(res.data.retrying).toBe(false);
  });

  it('marks retrying=true when connected + non-client error', async () => {
    mockFetchJson({
      code: 0,
      data: {
        state: 'connected',
        error: { code: 503, message: 'server unreachable' },
      },
    });
    const res = await standaloneK2.run<any>('status');
    expect(res.data.state).toBe('error');
    expect(res.data.retrying).toBe(true);
  });

  it('marks retrying=false when connected + client-action error', async () => {
    mockFetchJson({
      code: 0,
      data: {
        state: 'connected',
        error: { code: 401, message: 'auth rejected' },
      },
    });
    const res = await standaloneK2.run<any>('status');
    expect(res.data.state).toBe('error');
    expect(res.data.retrying).toBe(false);
  });

  it('passes through non-status actions unchanged', async () => {
    mockFetchJson({ code: 0, data: { state: 'stopped' } });
    const res = await standaloneK2.run<any>('up');
    // No transform on non-status action — raw 'stopped' survives.
    expect(res.data.state).toBe('stopped');
  });

  it('skips transform when status returns error code', async () => {
    mockFetchJson({ code: -1, message: 'Service unavailable' });
    const res = await standaloneK2.run<any>('status');
    expect(res.code).toBe(-1);
    expect(res.data).toBeUndefined();
  });
});
