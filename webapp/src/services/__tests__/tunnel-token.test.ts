import { describe, it, expect, vi, beforeEach } from 'vitest';

vi.mock('../auth-service', () => ({
  authService: {
    getTunnelToken: vi.fn(),
    setTunnelToken: vi.fn(),
    getUdid: vi.fn(),
  },
}));
import { authService } from '../auth-service';
import { adoptTunnelToken } from '../tunnel-token';

describe('adoptTunnelToken', () => {
  beforeEach(() => {
    vi.clearAllMocks();
    // clearAllMocks 清实现 —— 逐个重设。
    vi.mocked(authService.getTunnelToken).mockResolvedValue(null);
    vi.mocked(authService.setTunnelToken).mockResolvedValue(undefined);
    vi.mocked(authService.getUdid).mockResolvedValue('UDID-1');
    (window as any)._platform = { os: 'macos' };
    (window as any)._k2 = { run: vi.fn().mockResolvedValue({ code: 0, message: 'ok' }) };
  });

  it('persists a new token', async () => {
    await adoptTunnelToken('NEW');
    expect(authService.setTunnelToken).toHaveBeenCalledWith('NEW');
  });

  it('no-ops on undefined and on unchanged token', async () => {
    await adoptTunnelToken(undefined);
    vi.mocked(authService.getTunnelToken).mockResolvedValue('SAME');
    await adoptTunnelToken('SAME');
    expect(authService.setTunnelToken).not.toHaveBeenCalled();
  });

  it('syncs native config on mobile platforms only', async () => {
    (window as any)._platform = { os: 'ios' };
    await adoptTunnelToken('NEW');
    expect((window as any)._k2.run).toHaveBeenCalledWith('sync-credential', {
      udid: 'UDID-1',
      token: 'NEW',
    });

    vi.mocked((window as any)._k2.run).mockClear();
    (window as any)._platform = { os: 'macos' };
    await adoptTunnelToken('NEWER');
    expect((window as any)._k2.run).not.toHaveBeenCalled();
  });

  it('swallows native sync failure (older native builds)', async () => {
    (window as any)._platform = { os: 'android' };
    (window as any)._k2 = { run: vi.fn().mockRejectedValue(new Error('no method')) };
    await expect(adoptTunnelToken('NEW')).resolves.toBeUndefined();
    expect(authService.setTunnelToken).toHaveBeenCalledWith('NEW');
  });
});
