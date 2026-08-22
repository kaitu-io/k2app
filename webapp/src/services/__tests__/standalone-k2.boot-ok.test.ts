/**
 * Linux webui counterpart of tauri-k2.boot-ok.test.ts and
 * capacitor-k2.boot-ok.test.ts.
 *
 * The Linux shell is a Go daemon serving the webapp over HTTP, so its boot
 * handshake is a POST rather than a bridge call. Same contract as the other
 * three: it may only fire after the UI has rendered, and it must be inert
 * anywhere that is not the Linux shell — a plain browser (dev, standalone
 * web) has no daemon to confirm to.
 */
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';

import { confirmWebBootOk } from '../standalone-k2';

const BOOT_OK_URL = '/api/ui-boot-ok';

describe('standalone-k2 web boot handshake', () => {
  let fetchMock: ReturnType<typeof vi.fn>;

  beforeEach(() => {
    fetchMock = vi.fn().mockResolvedValue({ ok: true, status: 200 });
    vi.stubGlobal('fetch', fetchMock);
    delete (window as { __K2_GATEWAY__?: unknown }).__K2_GATEWAY__;
  });

  afterEach(() => {
    vi.unstubAllGlobals();
    delete (window as { __K2_GATEWAY__?: unknown }).__K2_GATEWAY__;
  });

  it('POSTs the confirmation when running under the Linux shell', async () => {
    window.__K2_GATEWAY__ = { version: '0.4.9', commit: 'abc', arch: 'amd64' };

    await confirmWebBootOk();

    expect(fetchMock).toHaveBeenCalledOnce();
    const [url, init] = fetchMock.mock.calls[0];
    expect(url).toBe(BOOT_OK_URL);
    expect(init?.method).toBe('POST');
  });

  // A plain browser has no k2 daemon behind it. Posting anyway would be a
  // guaranteed 404 on every page load of the standalone web build.
  it('does nothing in a plain browser', async () => {
    await confirmWebBootOk();
    expect(fetchMock).not.toHaveBeenCalled();
  });

  // An older daemon (0.4.8 and before) serves the UI but has no such route.
  // The handshake is best-effort: a missing endpoint must not surface.
  it('survives a shell without the endpoint', async () => {
    window.__K2_GATEWAY__ = { version: '0.4.8', commit: 'abc', arch: 'amd64' };
    fetchMock.mockResolvedValue({ ok: false, status: 404 });

    await expect(confirmWebBootOk()).resolves.toBeUndefined();
  });

  it('survives a network failure', async () => {
    window.__K2_GATEWAY__ = { version: '0.4.9', commit: 'abc', arch: 'amd64' };
    fetchMock.mockRejectedValue(new Error('connection refused'));

    await expect(confirmWebBootOk()).resolves.toBeUndefined();
  });
});
