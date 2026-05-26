import { describe, it, expect, beforeEach } from 'vitest';

import { useAppBypassStore, __resetAppBypassInflightForTests } from '../app-bypass.store';
import { mockDaemonBackedPlatform, mockK2Run } from '../../test/utils/platform-mock';

describe('app-bypass.store one-shot migration', () => {
  beforeEach(() => {
    mockDaemonBackedPlatform();
    __resetAppBypassInflightForTests();
    useAppBypassStore.setState({
      entries: [], loaded: false, featureSupported: undefined, region: '',
      candidates: [], candidatesLoadedAt: 0, candidatesLoading: false, candidatesError: null,
    });
  });

  it('migrates local entries to daemon when daemon is empty', async () => {
    const storage = window._platform.storage as any;
    storage.get.mockResolvedValueOnce({
      v: 1,
      entries: [
        { id: 'wechat', label: 'WeChat', kind: 'process', names: ['WeChat'], addedAt: 1 },
        { id: 'com.gtja', label: 'GTJA', kind: 'package', names: ['com.gtja.client'], addedAt: 2 },
      ],
    });

    const calls: any[] = [];
    mockK2Run(async (action, params) => {
      calls.push({ action, params });
      if (action === 'app-bypass-get') {
        return { code: 0, message: 'ok', data: {
          feature_supported: true,
          region: '',
          custom: { process_adds: [], package_adds: [] },
        }};
      }
      if (action === 'app-bypass-set-custom') {
        return { code: 0, message: 'ok', data: {
          feature_supported: true,
          region: '',
          custom: {
            process_adds: params.add.process,
            package_adds: params.add.package,
          },
        }};
      }
      throw new Error('unexpected ' + action);
    });

    await useAppBypassStore.getState().load();

    // Order: get → set-custom (migration) → storage.remove
    expect(calls[0].action).toBe('app-bypass-get');
    expect(calls[1].action).toBe('app-bypass-set-custom');
    expect(calls[1].params).toEqual({
      add: { process: ['WeChat'], package: ['com.gtja.client'] },
      remove: { process: [], package: [] },
    });
    expect(storage.remove).toHaveBeenCalledWith('k2.advanced.app_bypass');
    expect(useAppBypassStore.getState().entries.map(e => e.id)).toEqual(['WeChat', 'com.gtja.client']);
  });

  it('skips migration when daemon already has entries', async () => {
    const storage = window._platform.storage as any;
    storage.get.mockResolvedValueOnce({ v: 1, entries: [
      { id: 'wechat', label: 'WeChat', kind: 'process', names: ['WeChat'], addedAt: 1 },
    ]});
    mockK2Run(async (action) => {
      if (action === 'app-bypass-get') {
        return { code: 0, message: 'ok', data: {
          feature_supported: true,
          region: 'cn',
          custom: { process_adds: ['QQ'], package_adds: [] },
        }};
      }
      throw new Error('migration should not call set-custom');
    });
    await useAppBypassStore.getState().load();
    expect(storage.remove).not.toHaveBeenCalled();
    expect(useAppBypassStore.getState().entries.map(e => e.id)).toEqual(['QQ']);
  });

  it('survives migration failure without losing local entries', async () => {
    const storage = window._platform.storage as any;
    storage.get.mockResolvedValueOnce({ v: 1, entries: [
      { id: 'wechat', label: 'WeChat', kind: 'process', names: ['WeChat'], addedAt: 1 },
    ]});
    mockK2Run(async (action) => {
      if (action === 'app-bypass-get') {
        return { code: 0, message: 'ok', data: {
          feature_supported: true,
          region: '',
          custom: { process_adds: [], package_adds: [] },
        }};
      }
      if (action === 'app-bypass-set-custom') {
        return { code: 503, message: 'daemon transient error', data: null };
      }
      throw new Error('unexpected');
    });
    await useAppBypassStore.getState().load();
    expect(storage.remove).not.toHaveBeenCalled();
    // Daemon snapshot is empty, so entries are empty; next launch retries.
    expect(useAppBypassStore.getState().entries).toEqual([]);
  });

  it('skips migration when both daemon and local storage are empty', async () => {
    const storage = window._platform.storage as any;
    storage.get.mockResolvedValueOnce(null);
    const calls: any[] = [];
    mockK2Run(async (action) => {
      calls.push(action);
      if (action === 'app-bypass-get') {
        return { code: 0, message: 'ok', data: {
          feature_supported: true,
          region: '',
          custom: { process_adds: [], package_adds: [] },
        }};
      }
      throw new Error('should not be called: ' + action);
    });
    await useAppBypassStore.getState().load();
    expect(calls).toEqual(['app-bypass-get']);
    expect(storage.remove).not.toHaveBeenCalled();
  });
});
