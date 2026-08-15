import { describe, it, expect, beforeEach, vi } from 'vitest';
import {
  shouldReloadOnChunkError,
  installChunkReloadGuard,
} from '../chunk-reload-guard';

function fakeStorage() {
  const map = new Map<string, string>();
  return {
    getItem: (k: string) => map.get(k) ?? null,
    setItem: (k: string, v: string) => {
      map.set(k, v);
    },
  };
}

describe('shouldReloadOnChunkError', () => {
  it('returns true and sets the flag on first call', () => {
    const storage = fakeStorage();
    expect(shouldReloadOnChunkError(storage)).toBe(true);
    expect(storage.getItem('k2_chunk_reload_once')).toBe('1');
  });

  it('returns false on a second call within the same session', () => {
    const storage = fakeStorage();
    expect(shouldReloadOnChunkError(storage)).toBe(true);
    expect(shouldReloadOnChunkError(storage)).toBe(false);
  });
});

describe('installChunkReloadGuard', () => {
  let target: EventTarget;

  beforeEach(() => {
    target = new EventTarget();
  });

  it('reloads once on the first vite:preloadError', () => {
    const storage = fakeStorage();
    const reload = vi.fn();
    const uninstall = installChunkReloadGuard({ target, storage, reload });

    const event = new Event('vite:preloadError', { cancelable: true });
    target.dispatchEvent(event);

    expect(reload).toHaveBeenCalledTimes(1);
    expect(event.defaultPrevented).toBe(true);
    uninstall();
  });

  it('does not reload a second time once the flag is set', () => {
    const storage = fakeStorage();
    const reload = vi.fn();
    const uninstall = installChunkReloadGuard({ target, storage, reload });

    target.dispatchEvent(new Event('vite:preloadError', { cancelable: true }));
    target.dispatchEvent(new Event('vite:preloadError', { cancelable: true }));

    expect(reload).toHaveBeenCalledTimes(1);
    uninstall();
  });

  it('does nothing after uninstall', () => {
    const storage = fakeStorage();
    const reload = vi.fn();
    const uninstall = installChunkReloadGuard({ target, storage, reload });
    uninstall();

    target.dispatchEvent(new Event('vite:preloadError', { cancelable: true }));
    expect(reload).not.toHaveBeenCalled();
  });
});
