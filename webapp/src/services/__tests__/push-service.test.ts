import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';

const api = vi.hoisted(() => ({ post: vi.fn(), get: vi.fn(), request: vi.fn() }));
vi.mock('../cloud-api', () => ({ cloudApi: api }));

import {
  PROMPT_RETRY_AFTER_MS,
  PUSH_ROUTE_WHITELIST,
  _resetPushSyncState,
  getMarketingOptIn,
  onPushRoute,
  pushSupported,
  recordPushPromptShown,
  routeFromPushData,
  setMarketingOptIn,
  shouldOfferPushPrompt,
  syncPushRegistration,
  wirePushTaps,
} from '../push-service';
import type { IPushBridge } from '../../types/kaitu-core';

function fakeBridge(over: Partial<IPushBridge> = {}) {
  let tap: ((d: Record<string, unknown>) => void) | null = null;
  const b: IPushBridge = {
    checkPermission: vi.fn().mockResolvedValue('granted'),
    requestPermission: vi.fn().mockResolvedValue('granted'),
    register: vi.fn().mockResolvedValue('tok-1'),
    appId: vi.fn().mockResolvedValue('io.overleap'),
    onTap: vi.fn((cb) => {
      tap = cb;
    }),
    openSettings: vi.fn().mockResolvedValue(undefined),
    ...over,
  };
  return { bridge: b, tap: (d: Record<string, unknown>) => tap?.(d) };
}

describe('push-service', () => {
  let original: unknown;

  beforeEach(() => {
    original = window._platform;
    _resetPushSyncState();
    localStorage.clear();
    api.post.mockResolvedValue({ code: 0 });
    api.get.mockResolvedValue({ code: 0, data: { marketingOptIn: true } });
    api.request.mockResolvedValue({ code: 0 });
  });

  afterEach(() => {
    (window as any)._platform = original;
    vi.clearAllMocks();
  });

  it('is unsupported without the bridge (old shells, non-iOS)', async () => {
    (window as any)._platform = { os: 'ios' };
    expect(pushSupported()).toBe(false);
    expect(await syncPushRegistration()).toBe(false);
    expect(api.post).not.toHaveBeenCalled();
  });

  it('route whitelist matches the server (api/logic_push.go pushRouteWhitelist)', () => {
    expect([...PUSH_ROUTE_WHITELIST].sort()).toEqual(['/account', '/pro-histories', '/purchase']);
  });

  describe('syncPushRegistration', () => {
    it('posts the token with the running app bundle id as topic', async () => {
      const { bridge } = fakeBridge();
      (window as any)._platform = { os: 'ios', version: '0.4.15', push: bridge };
      expect(await syncPushRegistration()).toBe(true);
      expect(api.post).toHaveBeenCalledWith('/api/push/token', {
        platform: 'ios', provider: 'apns', app_flavor: 'app_store',
        token: 'tok-1', topic: 'io.overleap', app_version: '0.4.15',
      });
    });

    it('never registers or posts without granted permission', async () => {
      const { bridge } = fakeBridge({ checkPermission: vi.fn().mockResolvedValue('prompt') });
      (window as any)._platform = { os: 'ios', push: bridge };
      expect(await syncPushRegistration({ force: true })).toBe(false);
      expect(bridge.register).not.toHaveBeenCalled();
      expect(bridge.requestPermission).not.toHaveBeenCalled();
      expect(api.post).not.toHaveBeenCalled();
    });

    it('does not repost the same token unless forced (new login session)', async () => {
      const { bridge } = fakeBridge();
      (window as any)._platform = { os: 'ios', push: bridge };
      await syncPushRegistration();
      await syncPushRegistration();
      expect(api.post).toHaveBeenCalledTimes(1);
      await syncPushRegistration({ force: true });
      expect(api.post).toHaveBeenCalledTimes(2);
    });

    it('reports failure on a null token or a rejected registration, without throwing', async () => {
      const { bridge } = fakeBridge({ register: vi.fn().mockResolvedValue(null) });
      (window as any)._platform = { os: 'ios', push: bridge };
      expect(await syncPushRegistration()).toBe(false);

      const b2 = fakeBridge().bridge;
      (window as any)._platform = { os: 'ios', push: b2 };
      api.post.mockResolvedValue({ code: 400, message: 'topic does not match brand' });
      expect(await syncPushRegistration()).toBe(false);
      api.post.mockRejectedValue(new Error('network'));
      expect(await syncPushRegistration({ force: true })).toBe(false);
    });
  });

  describe('tap routing', () => {
    it('only whitelisted string routes pass', () => {
      expect(routeFromPushData({ route: '/purchase' })).toBe('/purchase');
      expect(routeFromPushData({ route: '/evil' })).toBeNull();
      expect(routeFromPushData({ route: 42 })).toBeNull();
      expect(routeFromPushData({})).toBeNull();
    });

    it('holds a cold-start tap until the router subscribes, then delivers once', () => {
      const { bridge, tap } = fakeBridge();
      (window as any)._platform = { os: 'ios', push: bridge };
      wirePushTaps();
      tap({ route: '/purchase', aps: {} });

      const seen: string[] = [];
      const off = onPushRoute((r) => seen.push(r));
      expect(seen).toEqual(['/purchase']);
      tap({ route: '/account' });
      expect(seen).toEqual(['/purchase', '/account']);
      off();
      tap({ route: '/pro-histories' });
      expect(seen).toEqual(['/purchase', '/account']);
    });

    it('drops non-whitelisted taps', () => {
      const { bridge, tap } = fakeBridge();
      (window as any)._platform = { os: 'ios', push: bridge };
      wirePushTaps();
      const seen: string[] = [];
      onPushRoute((r) => seen.push(r));
      tap({ route: 'https://evil.example' });
      expect(seen).toEqual([]);
    });

    it('wires the native listener only once', () => {
      const { bridge } = fakeBridge();
      (window as any)._platform = { os: 'ios', push: bridge };
      wirePushTaps();
      wirePushTaps();
      expect(bridge.onTap).toHaveBeenCalledTimes(1);
    });
  });

  describe('permission prompt policy', () => {
    const t0 = 1_800_000_000_000;

    it('offers only while undecided', () => {
      expect(shouldOfferPushPrompt('prompt', t0)).toBe(true);
      expect(shouldOfferPushPrompt('granted', t0)).toBe(false);
      expect(shouldOfferPushPrompt('denied', t0)).toBe(false);
      expect(shouldOfferPushPrompt(null, t0)).toBe(false);
    });

    it('after "later": once more after 7 days, then never', () => {
      recordPushPromptShown(t0);
      expect(shouldOfferPushPrompt('prompt', t0 + PROMPT_RETRY_AFTER_MS - 1)).toBe(false);
      expect(shouldOfferPushPrompt('prompt', t0 + PROMPT_RETRY_AFTER_MS)).toBe(true);
      recordPushPromptShown(t0 + PROMPT_RETRY_AFTER_MS);
      expect(shouldOfferPushPrompt('prompt', t0 + 100 * PROMPT_RETRY_AFTER_MS)).toBe(false);
    });

    it('survives corrupt storage', () => {
      localStorage.setItem('k2.push.prompt', '{not json');
      expect(shouldOfferPushPrompt('prompt', t0)).toBe(true);
    });
  });

  describe('marketing consent', () => {
    it('reads and writes the server preference', async () => {
      expect(await getMarketingOptIn()).toBe(true);
      expect(api.get).toHaveBeenCalledWith('/api/push/preferences');
      expect(await setMarketingOptIn(false)).toBe(true);
      expect(api.request).toHaveBeenCalledWith('PUT', '/api/push/preferences', { marketingOptIn: false });
    });

    it('returns null / false on failure', async () => {
      api.get.mockResolvedValue({ code: 500 });
      expect(await getMarketingOptIn()).toBeNull();
      api.request.mockRejectedValue(new Error('x'));
      expect(await setMarketingOptIn(true)).toBe(false);
    });
  });
});
