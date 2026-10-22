/**
 * Social sign-in (Google / Apple) — platform-independent half.
 *
 * Which buttons show = server switch (appConfig.socialSignIn) × platform
 * capability (`_platform.socialSignIn`) × Google reachability probe, and never
 * on the router UI. Spec: docs/superpowers/specs/2026-10-20-social-sign-in-design.md
 * §6.1.1 / §6.5.
 */
import type { SocialProvider } from '../types/kaitu-core';

export interface SocialSignInSwitch {
  apple: boolean;
  google: boolean;
}

/** Raw nonce: 32 random bytes as hex. Only the server ever sees this value. */
export function createNonce(): string {
  const bytes = new Uint8Array(32);
  crypto.getRandomValues(bytes);
  return Array.from(bytes, (b) => b.toString(16).padStart(2, '0')).join('');
}

/** Lower-case hex SHA-256 — the form the server compares the token's nonce claim to. */
export async function sha256Hex(input: string): Promise<string> {
  const digest = await crypto.subtle.digest('SHA-256', new TextEncoder().encode(input));
  return Array.from(new Uint8Array(digest), (b) => b.toString(16).padStart(2, '0')).join('');
}

const GOOGLE_PROBE_URL = 'https://accounts.google.com/generate_204';
const GOOGLE_PROBE_TIMEOUT_MS = 3000;

/**
 * Can this device reach Google right now? Before sign-in the VPN is down, so
 * mainland users can't — a Google button there would always fail. `no-cors`:
 * any HTTP answer at all proves reachability; only a network error or the
 * timeout counts as unreachable.
 */
export async function probeGoogle(timeoutMs = GOOGLE_PROBE_TIMEOUT_MS): Promise<boolean> {
  const controller = new AbortController();
  const timer = setTimeout(() => controller.abort(), timeoutMs);
  try {
    await fetch(GOOGLE_PROBE_URL, { mode: 'no-cors', cache: 'no-store', signal: controller.signal });
    return true;
  } catch {
    return false;
  } finally {
    clearTimeout(timer);
  }
}

/**
 * Providers to show, in display order (Apple first). `onGoogleProbed` lets the
 * caller render Apple immediately and add Google only once the probe succeeds
 * (the probe must not block the dialog).
 */
export async function resolveSocialProviders(opts: {
  serverSwitch: SocialSignInSwitch | undefined;
  probe?: () => Promise<boolean>;
  onImmediate: (providers: SocialProvider[]) => void;
}): Promise<SocialProvider[]> {
  const platform = window._platform;
  const bridge = platform?.socialSignIn;
  if (!bridge || !opts.serverSwitch || platform?.platformType === 'gateway') {
    opts.onImmediate([]);
    return [];
  }
  let native: SocialProvider[];
  try {
    native = await bridge.availableProviders();
  } catch {
    native = [];
  }
  const enabled = native.filter((p) => opts.serverSwitch?.[p]);
  const immediate = enabled.filter((p) => p !== 'google');
  opts.onImmediate(immediate);
  if (!enabled.includes('google')) return immediate;
  const reachable = await (opts.probe ?? probeGoogle)();
  return reachable ? [...immediate, 'google'] : immediate;
}
