/**
 * localStorage wrapper that swallows SecurityError, QuotaExceededError, and
 * SSR access errors.
 *
 * Safari ties Web Storage to the cookie setting: when the user blocks all
 * cookies (Settings -> Safari -> Privacy -> Block All Cookies), every
 * getItem/setItem/removeItem call throws DOMException SecurityError. The
 * `window.localStorage` object itself remains truthy, so feature detection
 * via `typeof window.localStorage` does NOT work; only try/catch around the
 * actual method call detects this. Firefox in strict tracking-protection
 * mode behaves the same way.
 *
 * `set` returns `false` so callers (e.g. `applyLoginCredentials`) can decide
 * what to do when persistence is unavailable.
 */
export const safeStorage = {
  get(key: string): string | null {
    if (typeof window === 'undefined') return null;
    try {
      return window.localStorage.getItem(key);
    } catch {
      return null;
    }
  },
  set(key: string, value: string): boolean {
    if (typeof window === 'undefined') return false;
    try {
      window.localStorage.setItem(key, value);
      return true;
    } catch {
      return false;
    }
  },
  remove(key: string): void {
    if (typeof window === 'undefined') return;
    try {
      window.localStorage.removeItem(key);
    } catch {
      /* noop */
    }
  },
};
