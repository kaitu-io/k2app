/**
 * F6: recover from a stale-chunk load failure after a mid-session Web OTA
 * apply.
 *
 * The desktop poller can swap current/ under a running session (apply_pending
 * rotates current -> previous, pending -> current). The already-loaded
 * index.html still references the OLD build's hashed chunk filenames; the
 * first navigation to a lazy-loaded route/tab the user hasn't visited yet
 * triggers a dynamic import for a chunk that no longer exists on disk (the
 * new bundle has different hashes) -> 404 -> Vite's module-preload helper
 * fires `vite:preloadError` on `window` -> otherwise an uncaught rejection
 * that lands in the app's error boundary.
 *
 * Fix: catch that event, and reload once (not zero, not repeatedly — a
 * genuinely broken chunk that 404s even after a fresh index.html load must
 * not reload-loop forever). The sessionStorage flag is the "did we already
 * try this" guard; it's per-tab/session so a later real session naturally
 * gets to try again.
 */

const SESSION_FLAG = 'k2_chunk_reload_once';

type FlagStorage = Pick<Storage, 'getItem' | 'setItem'>;

/**
 * Pure decision: should this preload-error trigger a reload? Also records
 * that a reload was attempted, so a second error in the same session (i.e.
 * the reload didn't fix it) does not loop.
 */
export function shouldReloadOnChunkError(storage: FlagStorage): boolean {
  if (storage.getItem(SESSION_FLAG)) {
    return false;
  }
  storage.setItem(SESSION_FLAG, '1');
  return true;
}

export interface ChunkReloadGuardOptions {
  target?: EventTarget;
  storage?: FlagStorage;
  reload?: () => void;
}

/**
 * Install the `vite:preloadError` listener. Returns an uninstall function
 * (mainly for tests / HMR cleanup).
 */
export function installChunkReloadGuard(opts: ChunkReloadGuardOptions = {}): () => void {
  const target = opts.target ?? window;
  const storage = opts.storage ?? window.sessionStorage;
  const reload = opts.reload ?? (() => window.location.reload());

  const handler = (event: Event) => {
    // Vite's default preload-error behavior is to let the error propagate as
    // an unhandled rejection; preventDefault opts into handling it here
    // instead.
    event.preventDefault();
    if (shouldReloadOnChunkError(storage)) {
      console.warn('[WebApp] stale chunk detected (vite:preloadError) — reloading once');
      reload();
    } else {
      console.error('[WebApp] stale chunk error persisted after reload — giving up');
    }
  };

  target.addEventListener('vite:preloadError', handler);
  return () => target.removeEventListener('vite:preloadError', handler);
}
