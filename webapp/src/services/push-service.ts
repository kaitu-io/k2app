/**
 * Push notifications — the one module business code goes through
 * (spec docs/superpowers/specs/2026-11-03-ios-push-design.md §5).
 *
 * - `pushSupported()` is the semantic capability flag: pages never probe
 *   `window._platform.push` themselves (webapp CLAUDE.md compatibility rule).
 * - Token registration, tap routing and the permission-prompt policy live here
 *   so they are testable without React.
 */
import { cloudApi } from './cloud-api';
import type { IPushBridge, PushPermission } from '../types/kaitu-core';

/** Mirrors api pushRouteWhitelist (api/logic_push.go). Any other route on a push is ignored. */
export const PUSH_ROUTE_WHITELIST: readonly string[] = ['/purchase', '/account', '/pro-histories'];

function bridge(): IPushBridge | undefined {
  return window._platform?.push;
}

export function pushSupported(): boolean {
  return !!bridge();
}

export async function getPushPermission(): Promise<PushPermission | null> {
  const b = bridge();
  if (!b) return null;
  try {
    return await b.checkPermission();
  } catch (err) {
    console.warn('[push] checkPermission failed:', err);
    return null;
  }
}

// ---------------------------------------------------------------------------
// Token registration
// ---------------------------------------------------------------------------

/** Token last sent to the server in this app session (avoid re-posting on every resume). */
let syncedToken: string | null = null;

/** Test hook. */
export function _resetPushSyncState(): void {
  syncedToken = null;
  pendingRoute = null;
  routeListener = null;
  tapWired = false;
}

/**
 * Registers with APNs and reports the token to Center. Only when the caller is
 * logged in and the system permission is already granted — never prompts.
 * `force` re-posts the same token (new login session: the server rebinds the
 * device's token to the new account and reactivates it after a logout).
 * Failures are logged only; push is never worth an error in the UI.
 */
export async function syncPushRegistration(opts: { force?: boolean } = {}): Promise<boolean> {
  const b = bridge();
  if (!b) return false;
  try {
    if ((await b.checkPermission()) !== 'granted') return false;
    const token = await b.register();
    if (!token) return false;
    if (!opts.force && token === syncedToken) return true;
    const topic = await b.appId();
    const resp = await cloudApi.post('/api/push/token', {
      platform: 'ios',
      provider: 'apns',
      app_flavor: 'app_store',
      token,
      topic,
      app_version: window._platform?.version ?? '',
    });
    if (resp.code !== 0) {
      console.warn('[push] token registration rejected:', resp.code, resp.message);
      return false;
    }
    syncedToken = token;
    return true;
  } catch (err) {
    console.warn('[push] token registration failed:', err);
    return false;
  }
}

// ---------------------------------------------------------------------------
// Tap → in-app route
// ---------------------------------------------------------------------------

let pendingRoute: string | null = null;
let routeListener: ((route: string) => void) | null = null;
let tapWired = false;

/** Whitelisted route carried by a push, or null. */
export function routeFromPushData(data: Record<string, unknown>): string | null {
  const route = data?.route;
  if (typeof route !== 'string') return null;
  if (!PUSH_ROUTE_WHITELIST.includes(route)) {
    console.warn('[push] ignoring non-whitelisted route:', route);
    return null;
  }
  return route;
}

/**
 * Wire the native tap listener once, as early as possible (bridge boot), so a
 * cold-start tap is captured before React mounts. The route is held until a
 * router-aware listener attaches (`onPushRoute`).
 */
export function wirePushTaps(): void {
  const b = bridge();
  if (!b || tapWired) return;
  tapWired = true;
  b.onTap((data) => {
    const route = routeFromPushData(data);
    if (!route) return;
    if (routeListener) routeListener(route);
    else pendingRoute = route;
  });
}

/** Router-side subscription. Delivers a route captured before mount immediately. Returns unsubscribe. */
export function onPushRoute(listener: (route: string) => void): () => void {
  routeListener = listener;
  if (pendingRoute) {
    const r = pendingRoute;
    pendingRoute = null;
    listener(r);
  }
  return () => {
    if (routeListener === listener) routeListener = null;
  };
}

// ---------------------------------------------------------------------------
// Permission prompt policy (§5.3)
// ---------------------------------------------------------------------------

const PROMPT_STATE_KEY = 'k2.push.prompt';
export const PROMPT_RETRY_AFTER_MS = 7 * 24 * 3600 * 1000;

interface PromptState {
  count: number;
  lastAt: number;
}

function readPromptState(): PromptState {
  try {
    const raw = localStorage.getItem(PROMPT_STATE_KEY);
    if (!raw) return { count: 0, lastAt: 0 };
    const v = JSON.parse(raw) as Partial<PromptState>;
    return { count: Number(v.count) || 0, lastAt: Number(v.lastAt) || 0 };
  } catch {
    return { count: 0, lastAt: 0 };
  }
}

/** Record that the in-app prompt was shown (whatever the answer). */
export function recordPushPromptShown(now: number = Date.now()): void {
  const s = readPromptState();
  try {
    localStorage.setItem(PROMPT_STATE_KEY, JSON.stringify({ count: s.count + 1, lastAt: now }));
  } catch {
    /* storage unavailable: worst case the prompt can show again */
  }
}

/**
 * Show the in-app prompt? Only while the system permission is still undecided,
 * the first time, and at most once more 7 days after a "later".
 */
export function shouldOfferPushPrompt(permission: PushPermission | null, now: number = Date.now()): boolean {
  if (permission !== 'prompt') return false;
  const s = readPromptState();
  if (s.count === 0) return true;
  return s.count === 1 && now - s.lastAt >= PROMPT_RETRY_AFTER_MS;
}

// ---------------------------------------------------------------------------
// Marketing consent (server-side, per user)
// ---------------------------------------------------------------------------

export async function getMarketingOptIn(): Promise<boolean | null> {
  try {
    const resp = await cloudApi.get<{ marketingOptIn: boolean }>('/api/push/preferences');
    return resp.code === 0 && resp.data ? resp.data.marketingOptIn : null;
  } catch {
    return null;
  }
}

export async function setMarketingOptIn(value: boolean): Promise<boolean> {
  try {
    const resp = await cloudApi.request('PUT', '/api/push/preferences', { marketingOptIn: value });
    return resp.code === 0;
  } catch {
    return false;
  }
}

/** Ask for the system permission (only from an explicit user action). */
export async function requestPushPermission(): Promise<PushPermission | null> {
  const b = bridge();
  if (!b) return null;
  try {
    return await b.requestPermission();
  } catch (err) {
    console.warn('[push] requestPermission failed:', err);
    return null;
  }
}

export async function openPushSettings(): Promise<void> {
  try {
    await bridge()?.openSettings();
  } catch (err) {
    console.warn('[push] openSettings failed:', err);
  }
}
