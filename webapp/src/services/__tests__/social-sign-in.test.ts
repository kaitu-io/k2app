import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { createNonce, sha256Hex, probeGoogle, resolveSocialProviders } from '../social-sign-in';
import type { ISocialSignIn, SocialProvider } from '../../types/kaitu-core';

function setPlatform(platformType: string, providers: SocialProvider[] | Error | null) {
  const bridge: ISocialSignIn | undefined =
    providers === null
      ? undefined
      : {
          availableProviders: vi.fn(() =>
            providers instanceof Error ? Promise.reject(providers) : Promise.resolve(providers)
          ),
          signIn: vi.fn(),
        };
  (window as any)._platform = { os: 'ios', platformType, socialSignIn: bridge };
}

const BOTH_ON = { apple: true, google: true };

describe('nonce helpers', () => {
  it('sha256Hex matches the standard test vector (server compares lower-case hex)', async () => {
    expect(await sha256Hex('abc')).toBe('ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad');
  });

  it('createNonce is 64 hex chars and not repeated', () => {
    const a = createNonce();
    expect(a).toMatch(/^[0-9a-f]{64}$/);
    expect(createNonce()).not.toBe(a);
  });
});

describe('probeGoogle', () => {
  afterEach(() => vi.unstubAllGlobals());

  it('any response (even opaque) = reachable', async () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue({ type: 'opaque' }));
    expect(await probeGoogle()).toBe(true);
  });

  it('network error = unreachable', async () => {
    vi.stubGlobal('fetch', vi.fn().mockRejectedValue(new TypeError('Failed to fetch')));
    expect(await probeGoogle()).toBe(false);
  });

  it('times out instead of hanging (blocked networks drop packets silently)', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn((_u: string, init: RequestInit) =>
        new Promise((_res, rej) => init.signal!.addEventListener('abort', () => rej(new Error('aborted'))))
      )
    );
    expect(await probeGoogle(20)).toBe(false);
  });
});

describe('resolveSocialProviders', () => {
  let onImmediate: ReturnType<typeof vi.fn>;
  beforeEach(() => {
    onImmediate = vi.fn();
  });

  it('old shell without the capability → nothing', async () => {
    setPlatform('mobile', null);
    expect(await resolveSocialProviders({ serverSwitch: BOTH_ON, probe: async () => true, onImmediate })).toEqual([]);
    expect(onImmediate).toHaveBeenCalledWith([]);
  });

  it('router UI (gateway) never shows social sign-in', async () => {
    setPlatform('gateway', ['apple', 'google']);
    expect(await resolveSocialProviders({ serverSwitch: BOTH_ON, probe: async () => true, onImmediate })).toEqual([]);
  });

  it('server that does not send the switch (old API) → nothing', async () => {
    setPlatform('mobile', ['apple', 'google']);
    expect(await resolveSocialProviders({ serverSwitch: undefined, probe: async () => true, onImmediate })).toEqual([]);
  });

  it('iOS, both on, Google reachable → Apple immediately, then Apple + Google', async () => {
    setPlatform('mobile', ['apple', 'google']);
    const result = await resolveSocialProviders({ serverSwitch: BOTH_ON, probe: async () => true, onImmediate });
    expect(onImmediate).toHaveBeenCalledWith(['apple']);
    expect(result).toEqual(['apple', 'google']);
  });

  it('Google unreachable (mainland, VPN off) → Apple only', async () => {
    setPlatform('mobile', ['apple', 'google']);
    expect(await resolveSocialProviders({ serverSwitch: BOTH_ON, probe: async () => false, onImmediate })).toEqual([
      'apple',
    ]);
  });

  it('server switch off for a provider hides it and skips the probe', async () => {
    setPlatform('mobile', ['apple', 'google']);
    const probe = vi.fn(async () => true);
    expect(
      await resolveSocialProviders({ serverSwitch: { apple: true, google: false }, probe, onImmediate })
    ).toEqual(['apple']);
    expect(probe).not.toHaveBeenCalled();
  });

  it('Android (no Apple yet): server Apple switch alone shows nothing', async () => {
    setPlatform('mobile', ['google']);
    expect(
      await resolveSocialProviders({ serverSwitch: { apple: true, google: false }, probe: async () => true, onImmediate })
    ).toEqual([]);
  });

  it('bridge failure degrades to no buttons, not an error', async () => {
    setPlatform('mobile', new Error('boom'));
    expect(await resolveSocialProviders({ serverSwitch: BOTH_ON, probe: async () => true, onImmediate })).toEqual([]);
  });
});
