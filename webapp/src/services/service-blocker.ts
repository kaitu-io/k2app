/**
 * service-blocker — sole provider of the "why can't the desktop service run"
 * capability (bridge v4: `_platform.getServiceBlocker` / `openServiceSettings`).
 *
 * Components never probe `window._platform` for these methods themselves
 * (webapp/CLAUDE.md 兼容模型): a Web-OTA'd webapp runs on shells that predate
 * v4, where the methods are missing (Capacitor / web) or the Tauri command does
 * not exist yet (the invoke rejects). Both read as "no known blocker", so the
 * caller falls back to the pre-v4 behavior (reinstall prompt).
 *
 * Today the only blocker is macOS "Allow in the Background" being off for our
 * developer (#3911): reinstalling never sticks, turning the switch back on makes
 * launchd start the daemon immediately.
 */
import type { ServiceBlocker } from '../types/kaitu-core';

export type { ServiceBlocker };

const KNOWN_KINDS: ReadonlySet<string> = new Set(['background-disallowed']);

/** Current blocker, or null when there is none / the shell cannot tell. */
export async function fetchServiceBlocker(): Promise<ServiceBlocker | null> {
  const platform = window._platform;
  if (typeof platform?.getServiceBlocker !== 'function') return null;
  try {
    const b = await platform.getServiceBlocker();
    if (!b || !KNOWN_KINDS.has(b.kind) || typeof b.itemName !== 'string' || !b.itemName) {
      return null;
    }
    return b;
  } catch (err) {
    console.debug('[service-blocker] getServiceBlocker unavailable:', err);
    return null;
  }
}

/** Whether this shell can open the relevant System Settings pane. */
export function canOpenServiceSettings(): boolean {
  return typeof window._platform?.openServiceSettings === 'function';
}

/** Open System Settings at the switch. Resolves false if it could not. */
export async function openServiceSettings(): Promise<boolean> {
  const platform = window._platform;
  if (typeof platform?.openServiceSettings !== 'function') return false;
  try {
    await platform.openServiceSettings();
    return true;
  } catch (err) {
    console.warn('[service-blocker] openServiceSettings failed:', err);
    return false;
  }
}
