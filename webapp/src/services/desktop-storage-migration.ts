/**
 * Desktop origin migration (spec 2026-08-14-web-ota-design §5.2).
 *
 * The Tauri shell moved the UI origin from tauri://localhost
 * (http://tauri.localhost on Windows) to the kaitu-ui:// custom protocol.
 * localStorage does not carry across origins:
 *  - at the OLD origin the shell loads the bundled UI with ?migrate=export —
 *    we dump localStorage to the Rust side and signal done (Rust then
 *    navigates this webview to the new origin);
 *  - at the NEW origin, if the Rust side holds a snapshot, we merge it into
 *    the (empty) localStorage, clear it, and ask the caller to reload once so
 *    module-eval-time readers (i18n's kaitu-language) see migrated values.
 *
 * Failure fallback everywhere: proceed to the new origin with fresh storage —
 * desktop auth tokens live in Rust storage.json (_platform.storage), so login
 * state is NOT at risk; only preferences/caches are.
 *
 * Bridge-layer file: like tauri-storage.ts, this is part of the Tauri bridge
 * surface and may import @tauri-apps/api (webapp constitutional rule).
 */

type InvokeFn = <T = unknown>(cmd: string, args?: Record<string, unknown>) => Promise<T>;

export function isExportMode(search: string): boolean {
  return new URLSearchParams(search).get('migrate') === 'export';
}

export function collectLocalStorageSnapshot(): Record<string, string> {
  const snap: Record<string, string> = {};
  for (let i = 0; i < localStorage.length; i++) {
    const key = localStorage.key(i);
    if (key !== null) {
      const value = localStorage.getItem(key);
      if (value !== null) snap[key] = value;
    }
  }
  return snap;
}

/** Merge-missing semantics: never overwrite a key the new origin already has. */
export function applySnapshot(snap: Record<string, string>): number {
  let applied = 0;
  for (const [key, value] of Object.entries(snap)) {
    if (localStorage.getItem(key) === null) {
      localStorage.setItem(key, value);
      applied++;
    }
  }
  return applied;
}

export async function runExportFlow(invoke: InvokeFn): Promise<void> {
  try {
    const json = JSON.stringify(collectLocalStorageSnapshot());
    await invoke('storage_migration_put', { json });
    console.info('[Migration] localStorage snapshot exported');
  } catch (e) {
    console.error('[Migration] export failed (fresh start at new origin):', e);
  } finally {
    try {
      await invoke('storage_migration_done');
    } catch (e) {
      console.error('[Migration] done signal failed (watchdog will recover):', e);
    }
  }
}

export async function runImportFlow(invoke: InvokeFn): Promise<'imported' | 'none'> {
  let raw: string | null = null;
  try {
    raw = await invoke<string | null>('storage_migration_get');
  } catch {
    return 'none'; // old shell without the command
  }
  if (!raw) return 'none';
  let applied = 0;
  try {
    const snap = JSON.parse(raw) as Record<string, string>;
    applied = applySnapshot(snap);
    console.info(`[Migration] imported ${applied} localStorage keys`);
  } catch (e) {
    console.error('[Migration] snapshot parse/apply failed:', e);
  }
  try {
    await invoke('storage_migration_clear');
  } catch {
    // Non-fatal: merge-missing semantics make a re-import a no-op.
  }
  return applied > 0 ? 'imported' : 'none';
}

/**
 * Entry called from main.tsx before app boot.
 * 'halt'    → export page: stop booting, Rust navigates this webview away.
 * 'reload'  → snapshot imported: caller reloads once so module-eval readers rerun.
 * 'continue'→ normal boot.
 */
export async function runDesktopStorageMigration(): Promise<'halt' | 'reload' | 'continue'> {
  if (!(window as { __TAURI__?: unknown }).__TAURI__) return 'continue';
  let invoke: InvokeFn;
  try {
    ({ invoke } = await import('@tauri-apps/api/core'));
  } catch {
    return 'continue';
  }
  if (isExportMode(window.location.search)) {
    await runExportFlow(invoke);
    return 'halt';
  }
  const result = await runImportFlow(invoke);
  return result === 'imported' ? 'reload' : 'continue';
}
