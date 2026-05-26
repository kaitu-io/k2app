import { describe, it, expect, beforeEach } from 'vitest';

import { useAppBypassStore, __resetAppBypassInflightForTests } from '../app-bypass.store';
import { mockDaemonBackedPlatform, mockK2Run } from '../../test/utils/platform-mock';

describe('app-bypass.store daemon API path', () => {
  beforeEach(() => {
    mockDaemonBackedPlatform();
    __resetAppBypassInflightForTests();
    useAppBypassStore.setState({
      entries: [], loaded: false, featureSupported: undefined, region: '',
      candidates: [], candidatesLoadedAt: 0, candidatesLoading: false, candidatesError: null,
    });
  });

  it('load() calls app-bypass-get and populates state', async () => {
    mockK2Run(async (action) => {
      if (action === 'app-bypass-get') {
        return { code: 0, message: 'ok', data: {
          feature_supported: true,
          region: 'cn',
          custom: { process_adds: ['Steam', 'steam_osx'], package_adds: [] },
        }};
      }
      throw new Error('unexpected: ' + action);
    });
    await useAppBypassStore.getState().load();
    const s = useAppBypassStore.getState();
    expect(s.loaded).toBe(true);
    expect(s.featureSupported).toBe(true);
    expect(s.region).toBe('cn');
    expect(s.entries.map(e => e.id)).toEqual(['Steam', 'steam_osx']);
    expect((window._k2.run as any)).toHaveBeenCalledWith('app-bypass-get');
  });

  it('add() pushes process add delta and updates from daemon response', async () => {
    useAppBypassStore.setState({ loaded: true });
    mockK2Run(async (action, params) => {
      if (action === 'app-bypass-set-custom') {
        expect(params).toEqual({
          add: { process: ['Photoshop'], package: [] },
          remove: { process: [], package: [] },
        });
        return { code: 0, message: 'ok', data: {
          feature_supported: true,
          region: '',
          custom: { process_adds: ['Photoshop'], package_adds: [] },
        }};
      }
      throw new Error('unexpected: ' + action);
    });
    await useAppBypassStore.getState().add({
      id: 'photoshop', label: 'Photoshop', kind: 'process', names: ['Photoshop'],
    });
    expect(useAppBypassStore.getState().entries.map(e => e.id)).toEqual(['Photoshop']);
  });

  it('add() pushes package add delta for Android-style entries', async () => {
    useAppBypassStore.setState({ loaded: true });
    mockK2Run(async (action, params) => {
      if (action === 'app-bypass-set-custom') {
        expect(params).toEqual({
          add: { process: [], package: ['com.gtja.client'] },
          remove: { process: [], package: [] },
        });
        return { code: 0, message: 'ok', data: {
          feature_supported: true,
          region: '',
          custom: { process_adds: [], package_adds: ['com.gtja.client'] },
        }};
      }
      throw new Error('unexpected: ' + action);
    });
    await useAppBypassStore.getState().add({
      id: 'com.gtja', label: 'GTJA', kind: 'package', names: ['com.gtja.client'],
    });
    expect(useAppBypassStore.getState().entries.map(e => e.id)).toEqual(['com.gtja.client']);
  });

  it('remove() pushes remove delta', async () => {
    useAppBypassStore.setState({
      loaded: true,
      entries: [{ id: 'a', label: 'a', kind: 'process', names: ['a'], addedAt: 0 }],
    });
    mockK2Run(async (action, params) => {
      if (action === 'app-bypass-set-custom') {
        expect(params).toEqual({
          add: { process: [], package: [] },
          remove: { process: ['a'], package: [] },
        });
        return { code: 0, message: 'ok', data: {
          feature_supported: true,
          region: '',
          custom: { process_adds: [], package_adds: [] },
        }};
      }
      throw new Error('unexpected');
    });
    await useAppBypassStore.getState().remove('a');
    expect(useAppBypassStore.getState().entries).toEqual([]);
  });

  it('clear() pushes remove deltas for every known name', async () => {
    useAppBypassStore.setState({
      loaded: true,
      entries: [
        { id: 'p', label: 'p', kind: 'process', names: ['p1', 'p2'], addedAt: 0 },
        { id: 'q', label: 'q', kind: 'package', names: ['q.pkg'], addedAt: 0 },
      ],
    });
    mockK2Run(async (action, params) => {
      if (action === 'app-bypass-set-custom') {
        expect(params).toEqual({
          add: { process: [], package: [] },
          remove: { process: ['p1', 'p2'], package: ['q.pkg'] },
        });
        return { code: 0, message: 'ok', data: {
          feature_supported: true,
          region: '',
          custom: { process_adds: [], package_adds: [] },
        }};
      }
      throw new Error('unexpected');
    });
    await useAppBypassStore.getState().clear();
    expect(useAppBypassStore.getState().entries).toEqual([]);
  });

  it('rescan() removes old names and adds new names in one round trip', async () => {
    useAppBypassStore.setState({
      loaded: true,
      entries: [{ id: 'photoshop', label: 'Photoshop', kind: 'process', names: ['old1', 'old2'], addedAt: 0 }],
    });
    mockK2Run(async (action, params) => {
      if (action === 'app-bypass-set-custom') {
        expect(params).toEqual({
          add: { process: ['new1', 'new2'], package: [] },
          remove: { process: ['old1', 'old2'], package: [] },
        });
        return { code: 0, message: 'ok', data: {
          feature_supported: true,
          region: '',
          custom: { process_adds: ['new1', 'new2'], package_adds: [] },
        }};
      }
      throw new Error('unexpected');
    });
    await useAppBypassStore.getState().rescan('photoshop', ['new1', 'new2']);
    expect(useAppBypassStore.getState().entries.map(e => e.id)).toEqual(['new1', 'new2']);
  });

  it('setRegion() calls app-bypass-set-region and updates region', async () => {
    useAppBypassStore.setState({ loaded: true });
    mockK2Run(async (action, params) => {
      if (action === 'app-bypass-set-region') {
        expect(params).toEqual({ region: 'cn' });
        return { code: 0, message: 'ok', data: {
          feature_supported: true,
          region: 'cn',
          custom: { process_adds: [], package_adds: [] },
        }};
      }
      throw new Error('unexpected');
    });
    await useAppBypassStore.getState().setRegion('cn');
    expect(useAppBypassStore.getState().region).toBe('cn');
  });

  it('daemon get failure leaves state safely empty', async () => {
    mockK2Run(async () => ({ code: 503, message: 'daemon offline', data: null }));
    await useAppBypassStore.getState().load();
    const s = useAppBypassStore.getState();
    expect(s.loaded).toBe(true);
    expect(s.entries).toEqual([]);
    expect(s.featureSupported).toBeUndefined();
  });

  it('add() preserves stale entries when daemon is unreachable', async () => {
    useAppBypassStore.setState({
      loaded: true,
      entries: [{ id: 'existing', label: 'existing', kind: 'process', names: ['existing'], addedAt: 0 }],
    });
    mockK2Run(async () => ({ code: 503, message: 'daemon offline', data: null }));
    await useAppBypassStore.getState().add({
      id: 'new', label: 'new', kind: 'process', names: ['new'],
    });
    // Existing stale view kept; daemon path bails on null snapshot.
    expect(useAppBypassStore.getState().entries.map(e => e.id)).toEqual(['existing']);
  });

  it('load() with old daemon (code 400 unknown action) sets featureSupported=false and matched=[]', async () => {
    mockK2Run(async () => ({
      code: 400,
      message: 'unknown action: app-bypass-get',
      data: null,
    }));
    await useAppBypassStore.getState().load();
    const s = useAppBypassStore.getState();
    expect(s.loaded).toBe(true);
    expect(s.featureSupported).toBe(false);
    expect(s.entries).toEqual([]);
  });

  it('daemonSetRegion against old daemon does not crash; returns without updating region', async () => {
    useAppBypassStore.setState({ loaded: true, region: '' });
    mockK2Run(async () => ({
      code: 400,
      message: 'unknown action: app-bypass-set-region',
      data: null,
    }));
    // Should not throw
    await useAppBypassStore.getState().setRegion('cn');
    // Region stays unchanged because the unsupported path bails early
    expect(useAppBypassStore.getState().region).toBe('');
  });
});
