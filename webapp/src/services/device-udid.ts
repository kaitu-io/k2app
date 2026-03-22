import type { ISecureStorage } from '../types/kaitu-core';

const STORAGE_KEY = 'device-udid';
let cachedUdid: string | null = null;

/**
 * Get or generate a persistent device UDID.
 *
 * First call: reads from _platform.storage.
 * If not found: generates crypto.randomUUID(), stores it, returns SHA-256 hash.
 * Subsequent calls: returns cached value (no I/O).
 *
 * Output: 32 lowercase hex chars (SHA-256 first 16 bytes), same format as previous
 * hardware-based UDID.
 */
export async function getDeviceUdid(): Promise<string> {
  if (cachedUdid) return cachedUdid;

  const storage = window._platform?.storage;
  if (!storage) throw new Error('[DeviceUDID] Platform storage not available');

  let raw = await storage.get<string>(STORAGE_KEY);
  if (!raw) {
    raw = crypto.randomUUID();
    await storage.set(STORAGE_KEY, raw);
    await clearStaleAuthTokens(storage);
  }

  cachedUdid = await hashToUdid(raw);
  return cachedUdid;
}

async function hashToUdid(raw: string): Promise<string> {
  const data = new TextEncoder().encode(raw);
  const hashBuffer = await crypto.subtle.digest('SHA-256', data);
  return Array.from(new Uint8Array(hashBuffer))
    .slice(0, 16)
    .map(b => b.toString(16).padStart(2, '0'))
    .join('');
}

async function clearStaleAuthTokens(storage: ISecureStorage): Promise<void> {
  try {
    await storage.remove('k2.auth.token');
    await storage.remove('k2.auth.refresh');
    console.info('[DeviceUDID] New device UDID generated, cleared stale auth tokens');
  } catch {
    // Non-fatal: worst case user gets a VPN auth error and re-logs in manually
  }
}
