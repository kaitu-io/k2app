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

const NINETY_DAYS = 7776000;

/**
 * 构造一个"够用"的假 JWT：真实 header/signature 无关紧要，adoptTunnelToken
 * 只解 payload 的 exp claim（不验签）。remainingSeconds 可为负（模拟已过期）。
 */
function fakeJwt(remainingSeconds: number): string {
  const nowSeconds = Math.floor(Date.now() / 1000);
  const header = btoa(JSON.stringify({ alg: 'HS256', typ: 'JWT' }));
  const payload = btoa(JSON.stringify({ exp: nowSeconds + remainingSeconds }));
  return `${header}.${payload}.fakesig`;
}

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

  it('no-ops on undefined token', async () => {
    await adoptTunnelToken(undefined);
    expect(authService.setTunnelToken).not.toHaveBeenCalled();
  });

  // 根因（whole-branch review finding #1）：generateTunnelToken 每次都重签
  // Exp，字符串几乎从不相等——旧测试断言的"unchanged token"前提在生产环境
  // 里基本不会发生。真正的判据是"已存 token 剩余寿命是否跌破 50% 阈值"，
  // 与两次 JWT 字符串是否相同无关。这里喂两个不同的字符串，且已存 token
  // 剩余寿命仍在阈值之上 → 不应采纳。
  it('no-ops when stored token differs in string but is not yet stale (>50% remaining)', async () => {
    vi.mocked(authService.getTunnelToken).mockResolvedValue(fakeJwt(89 * 86400)); // 剩 89 天
    await adoptTunnelToken(fakeJwt(90 * 86400)); // 服务端重签的新字符串
    expect(authService.setTunnelToken).not.toHaveBeenCalled();
  });

  // 剩余寿命跌破 50%（<45 天）→ 必须采纳，即便字符串比较也会判定"不同"，
  // 但这里锚定的是判据本身（remaining-lifetime），不是字符串差异。
  it('adopts when the stored token has fallen below the 50% renewal threshold', async () => {
    vi.mocked(authService.getTunnelToken).mockResolvedValue(fakeJwt(30 * 86400)); // 剩 30 天 < 45 天
    const incoming = fakeJwt(90 * 86400);
    await adoptTunnelToken(incoming);
    expect(authService.setTunnelToken).toHaveBeenCalledWith(incoming);
  });

  // 已存 token 解不出 exp（格式坏掉）→ 视为陈旧，必须采纳。
  it('adopts when the stored token fails to decode', async () => {
    vi.mocked(authService.getTunnelToken).mockResolvedValue('not-a-jwt');
    const incoming = fakeJwt(90 * 86400);
    await adoptTunnelToken(incoming);
    expect(authService.setTunnelToken).toHaveBeenCalledWith(incoming);
  });

  // 首次采纳（没存过）→ 必须采纳。
  it('adopts on first-ever adoption (no stored token yet)', async () => {
    vi.mocked(authService.getTunnelToken).mockResolvedValue(null);
    const incoming = fakeJwt(NINETY_DAYS);
    await adoptTunnelToken(incoming);
    expect(authService.setTunnelToken).toHaveBeenCalledWith(incoming);
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
