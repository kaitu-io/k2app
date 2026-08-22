/**
 * Mobile counterpart of tauri-k2.boot-ok.test.ts.
 *
 * The web-OTA rollback marker (`.boot-pending`) must only be cleared once the
 * UI has actually RENDERED. Clearing it during bridge init — which runs before
 * store init and the first React render — means a bundle that crashes in
 * either stage still reports "boot verified", so the next cold start serves
 * the same broken bundle instead of rolling it back. The desktop shell learned
 * this the hard way (2026-08-18 white screen); these tests pin the same
 * invariant for iOS/Android, where a defeated rollback has no hot-fix path.
 */
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';

import { Capacitor } from '@capacitor/core';

vi.mock('@capacitor/core', () => ({
  Capacitor: {
    isNativePlatform: vi.fn(() => true),
    getPlatform: vi.fn(() => 'android'),
  },
  CapacitorHttp: { request: vi.fn() },
}));

vi.mock('@capacitor/clipboard', () => ({
  Clipboard: {
    write: vi.fn(),
    read: vi.fn().mockResolvedValue({ type: 'text/plain', value: '' }),
  },
}));

const mockK2Plugin = {
  checkReady: vi.fn(),
  confirmWebBootOk: vi.fn(),
  getVersion: vi.fn(),
  getStatus: vi.fn(),
  connect: vi.fn(),
  disconnect: vi.fn(),
  addListener: vi.fn(),
  openUrl: vi.fn().mockResolvedValue(undefined),
  setLogLevel: vi.fn().mockResolvedValue(undefined),
  classifyApps: vi.fn(),
  relayFetch: vi.fn(),
  relayAddNodes: vi.fn(),
  getDefaultGateway: vi.fn(),
  getUpdateChannel: vi.fn(),
  setUpdateChannel: vi.fn(),
};

vi.mock('k2-plugin', () => ({ K2Plugin: mockK2Plugin }));

describe('capacitor-k2 web boot handshake', () => {
  let originalK2: unknown;
  let originalPlatform: unknown;

  beforeEach(() => {
    originalK2 = window._k2;
    originalPlatform = window._platform;
    delete (window as { _k2?: unknown })._k2;
    delete (window as { _platform?: unknown })._platform;
    vi.clearAllMocks();
    (Capacitor.getPlatform as unknown as ReturnType<typeof vi.fn>).mockReturnValue('android');
    mockK2Plugin.checkReady.mockResolvedValue({ ready: true, version: '0.4.8', bridgeVersion: 3 });
    mockK2Plugin.confirmWebBootOk.mockResolvedValue(undefined);
    mockK2Plugin.addListener.mockResolvedValue({ remove: vi.fn() });
    mockK2Plugin.setLogLevel.mockResolvedValue(undefined);
  });

  afterEach(() => {
    (window as { _k2?: unknown })._k2 = originalK2;
    (window as { _platform?: unknown })._platform = originalPlatform;
  });

  // The guard: bridge init happens BEFORE store init and first render, so
  // nothing in it may confirm the boot. Moving confirmWebBootOk back into
  // injectCapacitorGlobals turns this red.
  it('does NOT confirm web boot during bridge init', async () => {
    const { injectCapacitorGlobals } = await import('../capacitor-k2');
    await injectCapacitorGlobals();

    expect(mockK2Plugin.confirmWebBootOk).not.toHaveBeenCalled();
  });

  it('confirmWebBootOk calls the native confirmWebBootOk', async () => {
    const { confirmWebBootOk } = await import('../capacitor-k2');
    await confirmWebBootOk();

    expect(mockK2Plugin.confirmWebBootOk).toHaveBeenCalledOnce();
  });

  // 0.4.8 and older shells clear the marker inside checkReady() and have no
  // confirmWebBootOk at all. A newer webapp reaching them over web OTA must
  // not blow up on the missing method.
  it('confirmWebBootOk survives an old shell without the method', async () => {
    mockK2Plugin.confirmWebBootOk.mockRejectedValue(new Error('not implemented'));

    const { confirmWebBootOk } = await import('../capacitor-k2');
    await expect(confirmWebBootOk()).resolves.toBeUndefined();
  });

  it('injectCapacitorGlobals still resolves on an old shell without the method', async () => {
    mockK2Plugin.confirmWebBootOk.mockRejectedValue(new Error('not implemented'));

    const { injectCapacitorGlobals } = await import('../capacitor-k2');
    await expect(injectCapacitorGlobals()).resolves.toBeUndefined();
  });
});
