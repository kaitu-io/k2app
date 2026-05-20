import { describe, it, expect, beforeEach, vi } from 'vitest';
import { useAppBypassStore } from '../app-bypass.store';
import { useConfigStore } from '../config.store';

describe('app-bypass privacy invariant', () => {
  beforeEach(() => {
    (window as any)._platform = {
      os: 'macos',
      storage: { get: vi.fn().mockResolvedValue(null), set: vi.fn().mockResolvedValue(undefined) },
    };
    useAppBypassStore.setState({
      entries: [
        { id: 'com.wechat', label: 'WeChat', kind: 'process', names: ['WeChat', 'WeChatAppEx'], addedAt: 1 },
      ],
      loaded: true,
    });
  });

  it('buildConnectConfig console.debug does NOT log entry names', () => {
    const debugSpy = vi.spyOn(console, 'debug').mockImplementation(() => {});
    useConfigStore.getState().buildConnectConfig('k2v5://test@host:443/');
    const allDebugCalls = debugSpy.mock.calls.flat().join(' ');
    expect(allDebugCalls).not.toContain('WeChat');
    expect(allDebugCalls).not.toContain('WeChatAppEx');
    expect(allDebugCalls).toContain('bypassEntryCount=1');
    debugSpy.mockRestore();
  });
});
