import { describe, it, expect, vi, beforeEach } from 'vitest';

vi.mock('@capacitor/core', () => ({
  Capacitor: { isNativePlatform: () => true },
  CapacitorHttp: { request: vi.fn() },
}));
vi.mock('@capacitor/clipboard', () => ({ Clipboard: {} }));
vi.mock('@capacitor/share', () => ({ Share: {} }));
vi.mock('k2-plugin', () => ({
  K2Plugin: {
    getConfig: vi.fn(),
    updateConfig: vi.fn(),
  },
}));

import { K2Plugin } from 'k2-plugin';
import { capacitorRun, rewriteConfigCredential } from '../capacitor-k2';

const STORED = JSON.stringify({
  routes: [
    { via: 'direct', match: { preset: 'cn-access' } },
    { via: 'k2v5://UDID-1:OLDTOK@h.example.com:443?ech=x&pin=sha256:abc', match: {} },
  ],
});

describe('rewriteConfigCredential', () => {
  it('replaces userinfo on k2v5 routes only', () => {
    const out = rewriteConfigCredential(STORED, 'UDID-1', 'NEWTOK');
    const cfg = JSON.parse(out);
    expect(cfg.routes[0].via).toBe('direct');
    expect(cfg.routes[1].via).toBe('k2v5://UDID-1:NEWTOK@h.example.com:443?ech=x&pin=sha256:abc');
  });

  it('injects userinfo when the stored URL has none', () => {
    const bare = JSON.stringify({ routes: [{ via: 'k2v5://h.example.com:443' }] });
    const cfg = JSON.parse(rewriteConfigCredential(bare, 'U', 'T'));
    expect(cfg.routes[0].via).toBe('k2v5://U:T@h.example.com:443');
  });

  it('returns input unchanged on malformed JSON', () => {
    expect(rewriteConfigCredential('not-json', 'U', 'T')).toBe('not-json');
  });
});

describe("capacitorRun('sync-credential')", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    // clearAllMocks 清实现 —— 重设。
    vi.mocked(K2Plugin.getConfig).mockResolvedValue({ config: STORED });
    vi.mocked(K2Plugin.updateConfig).mockResolvedValue(undefined);
  });

  it('rewrites the stored config and persists via updateConfig', async () => {
    const res = await capacitorRun('sync-credential', { udid: 'UDID-1', token: 'NEWTOK' });
    expect(res.code).toBe(0);
    expect(K2Plugin.updateConfig).toHaveBeenCalledTimes(1);
    const arg = vi.mocked(K2Plugin.updateConfig).mock.calls[0][0];
    expect(arg.config).toContain('UDID-1:NEWTOK@');
    expect(arg.config).not.toContain('OLDTOK');
  });

  it('no-ops when no config stored yet', async () => {
    vi.mocked(K2Plugin.getConfig).mockResolvedValue({ config: '' });
    const res = await capacitorRun('sync-credential', { udid: 'U', token: 'T' });
    expect(res.code).toBe(0);
    expect(K2Plugin.updateConfig).not.toHaveBeenCalled();
  });

  it('rejects missing params', async () => {
    const res = await capacitorRun('sync-credential', {});
    expect(res.code).not.toBe(0);
  });
});
