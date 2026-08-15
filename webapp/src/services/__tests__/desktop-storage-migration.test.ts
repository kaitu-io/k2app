import { describe, it, expect, beforeEach, vi } from 'vitest';
import {
  isExportMode,
  collectLocalStorageSnapshot,
  applySnapshot,
  runExportFlow,
  runImportFlow,
} from '../desktop-storage-migration';

type InvokeFn = <T = unknown>(cmd: string, args?: Record<string, unknown>) => Promise<T>;

describe('desktop-storage-migration', () => {
  beforeEach(() => {
    localStorage.clear();
  });

  it('isExportMode detects ?migrate=export only', () => {
    expect(isExportMode('?migrate=export')).toBe(true);
    expect(isExportMode('?foo=1&migrate=export')).toBe(true);
    expect(isExportMode('?migrate=import')).toBe(false);
    expect(isExportMode('')).toBe(false);
  });

  it('collect + apply roundtrip', () => {
    localStorage.setItem('kaitu-language', 'zh-CN');
    localStorage.setItem('k2_log_level', 'debug');
    const snap = collectLocalStorageSnapshot();
    localStorage.clear();
    expect(applySnapshot(snap)).toBe(2);
    expect(localStorage.getItem('kaitu-language')).toBe('zh-CN');
    expect(localStorage.getItem('k2_log_level')).toBe('debug');
  });

  it('applySnapshot never overwrites existing keys', () => {
    localStorage.setItem('kaitu-language', 'en-US');
    expect(applySnapshot({ 'kaitu-language': 'zh-CN', extra: '1' })).toBe(1);
    expect(localStorage.getItem('kaitu-language')).toBe('en-US');
    expect(localStorage.getItem('extra')).toBe('1');
  });

  it('runExportFlow puts full snapshot then signals done', async () => {
    localStorage.setItem('a', '1');
    const calls: Array<[string, unknown]> = [];
    const invoke = vi.fn(async (cmd: string, args?: unknown) => {
      calls.push([cmd, args]);
      return null;
    }) as unknown as InvokeFn;
    await runExportFlow(invoke);
    expect(calls[0][0]).toBe('storage_migration_put');
    expect(JSON.parse((calls[0][1] as { json: string }).json)).toEqual({ a: '1' });
    expect(calls[1][0]).toBe('storage_migration_done');
  });

  it('runExportFlow still signals done when put fails (fresh-start fallback)', async () => {
    const invoke = vi.fn(async (cmd: string) => {
      if (cmd === 'storage_migration_put') throw new Error('boom');
      return null;
    }) as unknown as InvokeFn;
    await runExportFlow(invoke);
    expect(invoke).toHaveBeenCalledWith('storage_migration_done');
  });

  it('runImportFlow imports, clears, reports imported', async () => {
    const invoke = vi.fn(async (cmd: string) => {
      if (cmd === 'storage_migration_get') {
        return JSON.stringify({ 'kaitu-language': 'ja' });
      }
      return null;
    }) as unknown as InvokeFn;
    expect(await runImportFlow(invoke)).toBe('imported');
    expect(localStorage.getItem('kaitu-language')).toBe('ja');
    expect(invoke).toHaveBeenCalledWith('storage_migration_clear');
  });

  it('runImportFlow returns none when nothing applies', async () => {
    // no data on the Rust side
    expect(await runImportFlow(vi.fn(async () => null) as unknown as InvokeFn)).toBe('none');
    // old shell without the command
    expect(
      await runImportFlow(
        vi.fn(async () => {
          throw new Error('command storage_migration_get not found');
        }) as unknown as InvokeFn,
      ),
    ).toBe('none');
    // data exists but every key already present locally → no reload loop
    localStorage.setItem('kaitu-language', 'ja');
    const invoke = vi.fn(async (cmd: string) =>
      cmd === 'storage_migration_get' ? JSON.stringify({ 'kaitu-language': 'zh-CN' }) : null,
    ) as unknown as InvokeFn;
    expect(await runImportFlow(invoke)).toBe('none');
    expect(localStorage.getItem('kaitu-language')).toBe('ja');
  });

  it('runImportFlow survives corrupt snapshot json', async () => {
    const invoke = vi.fn(async (cmd: string) =>
      cmd === 'storage_migration_get' ? 'not-json{{{' : null,
    ) as unknown as InvokeFn;
    expect(await runImportFlow(invoke)).toBe('none');
    expect(invoke).toHaveBeenCalledWith('storage_migration_clear');
  });
});
