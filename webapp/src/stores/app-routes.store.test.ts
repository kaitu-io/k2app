import { describe, test, expect, beforeEach } from 'vitest';
import { useAppRoutesStore, migrateLegacyKey, OLD_STORAGE_KEY, NEW_STORAGE_KEY } from './app-routes.store';

describe('app-routes.store (Plan B)', () => {
  beforeEach(() => {
    localStorage.clear();
    useAppRoutesStore.setState(useAppRoutesStore.getInitialState?.() ?? {}, true);
  });

  test('initial state has empty region + empty overrides', () => {
    const s = useAppRoutesStore.getState();
    expect(s.region).toBe('');
    expect(s.forceProxy).toEqual([]);
    expect(s.forceDirect).toEqual([]);
  });

  test('setRegion persists to localStorage under new key', () => {
    useAppRoutesStore.getState().setRegion('cn');
    expect(useAppRoutesStore.getState().region).toBe('cn');
    const raw = localStorage.getItem(NEW_STORAGE_KEY);
    expect(raw).not.toBeNull();
    const parsed = JSON.parse(raw!);
    // zustand persist wraps state under a `state` key; tolerate both shapes.
    expect(parsed.state?.region ?? parsed.region).toBe('cn');
  });

  test('migrateLegacyKey drops the legacy k2.advanced.app_bypass key', () => {
    localStorage.setItem(OLD_STORAGE_KEY, JSON.stringify({ region: 'cn', custom: { process_adds: ['Steam.app'] } }));
    migrateLegacyKey();
    const s = useAppRoutesStore.getState();
    expect(s.forceProxy).toEqual([]);
    expect(s.forceDirect).toEqual([]);
    expect(localStorage.getItem(OLD_STORAGE_KEY)).toBeNull();
  });
});
