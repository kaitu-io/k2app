import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';

const native = vi.hoisted(() => {
  const listeners = new Map<string, (arg: any) => void>();
  return {
    listeners,
    isPluginAvailable: vi.fn(),
    checkPermissions: vi.fn(),
    requestPermissions: vi.fn(),
    register: vi.fn(),
    getInfo: vi.fn(),
    openUrl: vi.fn(),
    remove: vi.fn(),
  };
});

vi.mock('@capacitor/core', () => ({
  Capacitor: { isPluginAvailable: native.isPluginAvailable },
  registerPlugin: (name: string) =>
    name === 'App'
      ? { getInfo: native.getInfo }
      : {
          checkPermissions: native.checkPermissions,
          requestPermissions: native.requestPermissions,
          register: native.register,
          addListener: async (event: string, cb: (arg: any) => void) => {
            native.listeners.set(event, cb);
            return { remove: native.remove };
          },
        },
}));
vi.mock('k2-plugin', () => ({ K2Plugin: { openUrl: native.openUrl } }));

import { buildCapacitorPush, IOS_APP_SETTINGS_URL, REGISTER_TIMEOUT_MS } from '../capacitor-push';

describe('buildCapacitorPush', () => {
  beforeEach(() => {
    native.listeners.clear();
    native.isPluginAvailable.mockReturnValue(true);
    native.register.mockResolvedValue(undefined);
  });
  afterEach(() => {
    vi.clearAllMocks();
    vi.useRealTimers();
  });

  it('absent off iOS and on shells without the native plugin', () => {
    expect(buildCapacitorPush('android')).toBeUndefined();
    expect(buildCapacitorPush('web')).toBeUndefined();
    native.isPluginAvailable.mockReturnValue(false);
    expect(buildCapacitorPush('ios')).toBeUndefined();
  });

  it('maps native permission states', async () => {
    const b = buildCapacitorPush('ios')!;
    for (const [nativeState, mapped] of [
      ['granted', 'granted'],
      ['denied', 'denied'],
      ['prompt', 'prompt'],
      ['prompt-with-rationale', 'prompt'],
    ] as const) {
      native.checkPermissions.mockResolvedValue({ receive: nativeState });
      expect(await b.checkPermission()).toBe(mapped);
    }
  });

  it('register resolves the APNs token and removes its listeners', async () => {
    native.register.mockImplementation(async () => {
      native.listeners.get('registration')!({ value: 'abcd' });
    });
    expect(await buildCapacitorPush('ios')!.register()).toBe('abcd');
    expect(native.remove).toHaveBeenCalledTimes(2);
  });

  it('register resolves null on registrationError', async () => {
    native.register.mockImplementation(async () => {
      native.listeners.get('registrationError')!({ error: 'no entitlement' });
    });
    expect(await buildCapacitorPush('ios')!.register()).toBeNull();
  });

  it('register resolves null when APNs never answers', async () => {
    vi.useFakeTimers();
    const p = buildCapacitorPush('ios')!.register();
    await vi.advanceTimersByTimeAsync(REGISTER_TIMEOUT_MS);
    expect(await p).toBeNull();
  });

  it('tap passes the notification data through', () => {
    const seen: unknown[] = [];
    buildCapacitorPush('ios')!.onTap((d) => seen.push(d));
    native.listeners.get('pushNotificationActionPerformed')!({
      actionId: 'tap',
      notification: { data: { route: '/purchase', aps: {} } },
    });
    expect(seen).toEqual([{ route: '/purchase', aps: {} }]);
  });

  it('appId is the running bundle id; openSettings opens the app settings page', async () => {
    native.getInfo.mockResolvedValue({ id: 'com.allnationconnect.anc.wgios' });
    const b = buildCapacitorPush('ios')!;
    expect(await b.appId()).toBe('com.allnationconnect.anc.wgios');
    await b.openSettings();
    expect(native.openUrl).toHaveBeenCalledWith({ url: IOS_APP_SETTINGS_URL });
  });
});
