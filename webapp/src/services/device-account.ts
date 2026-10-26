/**
 * Device account (login-free, zero-identity) — spec 2026-10-20 §4.1 / §4.2 / §4.2.1.
 *
 * On iOS shells whose Keychain identity storage works, a device with no token gets an
 * account created from its udid at startup, so the user never has to log in to start.
 * The token lives in the Keychain, so a reinstall comes back through `hasToken()`.
 *
 * Every failure path ends in 'login-required' and does nothing: the existing login UI
 * stays exactly as it is.
 */
import { cloudApi } from './cloud-api';
import { authService } from './auth-service';
import { cacheStore } from './cache-store';
import { getDeviceUdid, regenerateDeviceUdid } from './device-udid';
import { statsService } from './stats';
import { useAuthStore } from '../stores/auth.store';
import { ERROR_CODES } from '../utils/errorCatalog';
import i18n from '../i18n/i18n';
import type { AppConfig } from './api-types';

export type DeviceAccountResult = 'existing' | 'created' | 'restored' | 'skipped' | 'login-required';

const DEVICE_ACCOUNT_PATH = '/api/auth/device-account';
const DEVICE_ACCOUNT_RESTORE_PATH = '/api/auth/device-account/restore';
const APP_CONFIG_CACHE_KEY = 'api:app_config';

/** In-flight dedup: concurrent callers share one run (server rejects a concurrent second create as 403006). */
let inflight: Promise<DeviceAccountResult> | null = null;

export function ensureDeviceAccount(): Promise<DeviceAccountResult> {
  if (!inflight) {
    inflight = runEnsure().finally(() => {
      inflight = null;
    });
  }
  return inflight;
}

/**
 * Move the device account that owns this Apple transaction onto this udid (§4.2.1).
 * Tokens are saved by cloudApi (AUTH_TOKEN_PATHS). Returns false on any rejection
 * (e.g. 409004 — the transaction belongs to an identity account); callers decide what to show.
 */
export async function restoreDeviceAccount(jws: string): Promise<boolean> {
  const udid = await getDeviceUdid();
  const res = await cloudApi.post(DEVICE_ACCOUNT_RESTORE_PATH, {
    udid,
    signedTransaction: jws,
    remark: i18n.t('startup:startup.newDevice'),
  });
  if (res.code !== 0) {
    console.warn('[DeviceAccount] restore rejected:', res.code, res.message);
    return false;
  }
  markAuthenticated();
  return true;
}

async function runEnsure(): Promise<DeviceAccountResult> {
  try {
    if (window._platform?.keychainIdentity !== true) return 'skipped';
    if (await authService.hasToken()) return 'existing';
    if ((await loadAppConfig())?.deviceAccount !== true) return 'skipped';

    const code = await createAccount();
    if (code === 0) return 'created';
    if (code !== ERROR_CODES.DEVICE_ACCOUNT_PROOF_REQUIRED) return 'login-required';
    return await handleProofRequired();
  } catch (err) {
    console.warn('[DeviceAccount] ensure failed:', err);
    return 'login-required';
  }
}

/**
 * 403006: this udid already owns a zero-identity account but we have no token for it.
 * An Apple transaction on this device proves ownership → restore. No transaction →
 * the old account has nothing to lose: rotate the udid and create once more.
 */
async function handleProofRequired(): Promise<DeviceAccountResult> {
  const iap = window._platform?.iap;
  // Can't ask StoreKit → can't tell "no purchase" from "unknown"; don't abandon the account.
  if (!iap) return 'login-required';
  const transactions = await iap.restore();
  const jws = transactions.find((tx) => typeof tx.jws === 'string' && tx.jws.length > 0)?.jws;
  if (jws) {
    return (await restoreDeviceAccount(jws)) ? 'restored' : 'login-required';
  }
  if (transactions.length > 0) {
    // Purchases exist but this shell sent no JWS — not "no entitlement"; leave the udid alone.
    return 'login-required';
  }
  const freshUdid = await regenerateDeviceUdid();
  return (await createAccount(freshUdid)) === 0 ? 'created' : 'login-required';
}

async function createAccount(udid?: string): Promise<number> {
  udid ??= await getDeviceUdid();
  const res = await cloudApi.post(DEVICE_ACCOUNT_PATH, {
    udid,
    remark: i18n.t('startup:startup.newDevice'),
    language: i18n.language,
  });
  if (res.code === 0) {
    markAuthenticated();
    void statsService.trackFunnelOnce('device_account_created');
  } else {
    console.info('[DeviceAccount] create declined:', res.code);
  }
  return res.code;
}

/** Same tail as code / social login. */
function markAuthenticated(): void {
  cacheStore.clear();
  useAuthStore.getState().setIsAuthenticated(true);
}

/** Cached app config, or fetch it and cache it the way useAppConfig does. */
async function loadAppConfig(): Promise<AppConfig | null> {
  const cached = cacheStore.get<AppConfig>(APP_CONFIG_CACHE_KEY);
  if (cached) return cached;
  const res = await cloudApi.get<AppConfig>('/api/app/config');
  if (res.code === 0 && res.data) {
    cacheStore.set(APP_CONFIG_CACHE_KEY, res.data, { ttl: 3600 });
    return res.data;
  }
  return null;
}
