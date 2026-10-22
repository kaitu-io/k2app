/**
 * Capacitor social sign-in bridge — wraps @capgo/capacitor-social-login.
 *
 * The plugin's JS package lives in mobile/ only; the webapp talks to the native
 * side through `registerPlugin('SocialLogin')` with the minimal types below, so
 * it carries no capgo dependency. Shells built before the plugin shipped have
 * no native `SocialLogin` → `buildCapacitorSocialSignIn` returns undefined and
 * the buttons never render.
 *
 * Nonce: the plugin hands `nonce` through untouched on iOS Apple
 * (`request.nonce`), iOS Google (`signIn(nonce:)`) and Android Google
 * (`setNonce`), so callers pass sha256hex(raw) here and the raw value to the
 * server (spec 2026-10-20 §6.2).
 */
import { Capacitor, registerPlugin } from '@capacitor/core';
import { K2Plugin } from 'k2-plugin';
import { brandConfig } from '../brands';
import type { ISocialSignIn, SocialCredential, SocialProvider } from '../types/kaitu-core';

interface SocialLoginNative {
  initialize(options: Record<string, unknown>): Promise<void>;
  login(options: {
    provider: SocialProvider;
    options: Record<string, unknown>;
  }): Promise<{ provider: string; result: { idToken?: string | null; authorizationCode?: string | null } }>;
}

const SocialLogin = registerPlugin<SocialLoginNative>('SocialLogin');

/** Rejection code both native sides use when the user backs out. */
const USER_CANCELLED = 'USER_CANCELLED';

function initOptions(os: 'ios' | 'android'): Record<string, unknown> {
  const { googleWebClientId, googleIosClientId } = brandConfig.social;
  if (os === 'ios') {
    return {
      // redirectUrl '' = no plugin-side backend call; useProperTokenExchange
      // makes it return Apple's authorizationCode instead of hiding it.
      apple: { redirectUrl: '', useProperTokenExchange: true },
      google: { iOSClientId: googleIosClientId, iOSServerClientId: googleWebClientId, mode: 'online' },
    };
  }
  return { google: { webClientId: googleWebClientId, mode: 'online' } };
}

export function buildCapacitorSocialSignIn(os: string): ISocialSignIn | undefined {
  if (os !== 'ios' && os !== 'android') return undefined;
  if (!Capacitor.isPluginAvailable('SocialLogin')) return undefined;

  let ready: Promise<void> | null = null;
  const ensureInitialized = (): Promise<void> => {
    ready ??= SocialLogin.initialize(initOptions(os)).catch((err) => {
      ready = null; // let the next tap retry
      throw err;
    });
    return ready;
  };

  return {
    async availableProviders(): Promise<SocialProvider[]> {
      if (os === 'ios') return ['apple', 'google'];
      // Android Apple needs the website callback relay (spec §6.3) — not yet.
      // Google needs Play services (Credential Manager).
      try {
        const { available } = await K2Plugin.hasGooglePlayServices();
        return available ? ['google'] : [];
      } catch {
        return [];
      }
    },

    async signIn(provider: SocialProvider, hashedNonce: string): Promise<SocialCredential | null> {
      await ensureInitialized();
      // Google: plugin default scopes (openid email profile). Passing custom
      // scopes on Android would require MainActivity changes for the
      // AuthorizationClient result — don't.
      const options: Record<string, unknown> =
        provider === 'apple' ? { scopes: ['email', 'name'], nonce: hashedNonce } : { nonce: hashedNonce };
      let res: Awaited<ReturnType<SocialLoginNative['login']>>;
      try {
        res = await SocialLogin.login({ provider, options });
      } catch (err) {
        if ((err as { code?: string })?.code === USER_CANCELLED) return null;
        throw err;
      }
      const idToken = res.result?.idToken;
      if (!idToken) throw new Error(`${provider} sign-in returned no idToken`);
      const authorizationCode = res.result.authorizationCode || undefined;
      return authorizationCode ? { idToken, authorizationCode } : { idToken };
    },
  };
}
