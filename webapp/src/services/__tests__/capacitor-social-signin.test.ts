import { describe, it, expect, vi, beforeEach } from 'vitest';

const native = vi.hoisted(() => ({
  initialize: vi.fn(),
  login: vi.fn(),
  isPluginAvailable: vi.fn(),
  hasGooglePlayServices: vi.fn(),
}));

vi.mock('@capacitor/core', () => ({
  Capacitor: { isPluginAvailable: native.isPluginAvailable },
  registerPlugin: () => ({ initialize: native.initialize, login: native.login }),
}));
vi.mock('k2-plugin', () => ({ K2Plugin: { hasGooglePlayServices: native.hasGooglePlayServices } }));

import { buildCapacitorSocialSignIn } from '../capacitor-social-signin';
import { brandConfig } from '../../brands';

describe('buildCapacitorSocialSignIn', () => {
  beforeEach(() => {
    vi.clearAllMocks();
    native.isPluginAvailable.mockReturnValue(true);
    native.initialize.mockResolvedValue(undefined);
    native.hasGooglePlayServices.mockResolvedValue({ available: true });
  });

  it('absent on shells without the native plugin (pre-plugin app versions)', () => {
    native.isPluginAvailable.mockReturnValue(false);
    expect(buildCapacitorSocialSignIn('ios')).toBeUndefined();
  });

  it('absent on non-mobile platforms', () => {
    expect(buildCapacitorSocialSignIn('web')).toBeUndefined();
  });

  it('iOS offers Apple and Google', async () => {
    expect(await buildCapacitorSocialSignIn('ios')!.availableProviders()).toEqual(['apple', 'google']);
  });

  it('Android offers Google only, and only with Play services', async () => {
    const bridge = buildCapacitorSocialSignIn('android')!;
    expect(await bridge.availableProviders()).toEqual(['google']);
    native.hasGooglePlayServices.mockResolvedValue({ available: false });
    expect(await bridge.availableProviders()).toEqual([]);
    native.hasGooglePlayServices.mockRejectedValue(new Error('not implemented'));
    expect(await bridge.availableProviders()).toEqual([]);
  });

  it('iOS init uses the brand client IDs; Apple returns its authorization code', async () => {
    native.login.mockResolvedValue({ provider: 'apple', result: { idToken: 'jwt', authorizationCode: 'code' } });
    const bridge = buildCapacitorSocialSignIn('ios')!;

    expect(await bridge.signIn('apple', 'hashed')).toEqual({ idToken: 'jwt', authorizationCode: 'code' });
    expect(native.initialize).toHaveBeenCalledWith({
      apple: { redirectUrl: '', useProperTokenExchange: true },
      google: {
        iOSClientId: brandConfig.social.googleIosClientId,
        iOSServerClientId: brandConfig.social.googleWebClientId,
        mode: 'online',
      },
    });
    expect(native.login).toHaveBeenCalledWith({
      provider: 'apple',
      options: { scopes: ['email', 'name'], nonce: 'hashed' },
    });
  });

  it('Android Google: web client as serverClientId, nonce passed through, no custom scopes', async () => {
    native.login.mockResolvedValue({ provider: 'google', result: { idToken: 'jwt', accessToken: null } });
    const bridge = buildCapacitorSocialSignIn('android')!;

    expect(await bridge.signIn('google', 'hashed')).toEqual({ idToken: 'jwt' });
    expect(native.initialize).toHaveBeenCalledWith({
      google: { webClientId: brandConfig.social.googleWebClientId, mode: 'online' },
    });
    expect(native.login).toHaveBeenCalledWith({ provider: 'google', options: { nonce: 'hashed' } });
  });

  it('initializes once across sign-ins, but retries after a failed init', async () => {
    native.login.mockResolvedValue({ provider: 'google', result: { idToken: 'jwt' } });
    native.initialize.mockRejectedValueOnce(new Error('init failed'));
    const bridge = buildCapacitorSocialSignIn('android')!;

    await expect(bridge.signIn('google', 'h')).rejects.toThrow('init failed');
    await bridge.signIn('google', 'h');
    await bridge.signIn('google', 'h');
    expect(native.initialize).toHaveBeenCalledTimes(2);
  });

  it('user cancel resolves null (no error shown)', async () => {
    native.login.mockRejectedValue(Object.assign(new Error('canceled'), { code: 'USER_CANCELLED' }));
    expect(await buildCapacitorSocialSignIn('ios')!.signIn('google', 'h')).toBeNull();
  });

  it('other native failures and a missing idToken reject', async () => {
    const bridge = buildCapacitorSocialSignIn('ios')!;
    native.login.mockRejectedValueOnce(new Error('network'));
    await expect(bridge.signIn('google', 'h')).rejects.toThrow('network');
    native.login.mockResolvedValueOnce({ provider: 'google', result: { idToken: null } });
    await expect(bridge.signIn('google', 'h')).rejects.toThrow(/no idToken/);
  });
});
