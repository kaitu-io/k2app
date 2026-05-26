import { describe, it, expect, beforeEach } from 'vitest';
import {
  useAppBypassStore,
  __resetAppBypassPreviewInflightForTests,
} from '../app-bypass.store';
import { mockDaemonBackedPlatform, mockMobilePlatform, mockK2Run } from '../../test/utils/platform-mock';

describe('app-bypass.store refreshPreview', () => {
  beforeEach(() => {
    __resetAppBypassPreviewInflightForTests();
    useAppBypassStore.setState({
      matched: [], matchedLoadedAt: 0, matchedLoading: false, matchedError: null,
    });
  });

  it('mobile: refreshPreview is no-op', async () => {
    mockMobilePlatform();
    let called = false;
    mockK2Run(async () => { called = true; return { code: 0, message: 'ok', data: null }; });
    await useAppBypassStore.getState().refreshPreview();
    expect(called).toBe(false);
    expect(useAppBypassStore.getState().matched).toEqual([]);
  });

  it('daemon: populates matched from preview response', async () => {
    mockDaemonBackedPlatform();
    mockK2Run(async (action) => {
      if (action === 'app-bypass-preview') {
        return { code: 0, message: 'ok', data: {
          region: 'cn',
          matched: [
            { id: '/Applications/WeChat.app', label: 'WeChat',
              names: ['wechat'], hit_kind: 'process_prefix', hit_pattern: 'wechat' },
          ],
        }};
      }
      throw new Error('unexpected');
    });
    await useAppBypassStore.getState().refreshPreview();
    const s = useAppBypassStore.getState();
    expect(s.matched).toHaveLength(1);
    expect(s.matched[0].hit_kind).toBe('process_prefix');
    expect(s.matchedError).toBeNull();
  });

  it('daemon error sets matchedError without clearing matched', async () => {
    mockDaemonBackedPlatform();
    useAppBypassStore.setState({
      matched: [{ id: 'x', label: 'X', hit_kind: 'process_exact', hit_pattern: 'x' }],
    });
    mockK2Run(async () => ({ code: 503, message: 'down', data: null }));
    await useAppBypassStore.getState().refreshPreview();
    const s = useAppBypassStore.getState();
    expect(s.matched).toHaveLength(1); // not cleared
    expect(s.matchedError).toBe('dashboard:appBypass.loadFailed');
  });

  it('in-flight dedup: concurrent calls share one Promise', async () => {
    mockDaemonBackedPlatform();
    let calls = 0;
    mockK2Run(async () => {
      calls++;
      await new Promise(r => setTimeout(r, 10));
      return { code: 0, message: 'ok', data: { region: '', matched: [] } };
    });
    const p1 = useAppBypassStore.getState().refreshPreview();
    const p2 = useAppBypassStore.getState().refreshPreview();
    expect(p1).toBe(p2);
    await p1;
    expect(calls).toBe(1);
  });
});
