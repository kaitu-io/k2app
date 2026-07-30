/**
 * Tunnel token adoption (Phase 0 — 隧道专用凭据).
 *
 * /api/v20260717/tunnels 响应携带 `tunnelToken`：90 天、设备绑定，只进
 * k2v5:// URL 的 userinfo（绝不作 API bearer）。所有 tunnels 响应消费点把
 * 该字段汇入 adoptTunnelToken()：
 *   1) authService.setTunnelToken() 持久化；
 *   2) 移动端把新凭据同步进系统无参数拉起 VPN 时读取的原生存储
 *      （iOS App Group + providerConfiguration / Android SharedPreferences），
 *      见 capacitor bridge 的 'sync-credential' action。桌面（P2）由 daemon
 *      侧回写 persistedState，不走这里。
 */
import { authService } from './auth-service';

/**
 * Tunnel token 的服务端配置寿命（秒），镜像 api/logic_config.go
 * `TunnelTokenExpiry` 的缺省值（90 天 = 7776000s，spec §4.1）。
 *
 * 这是客户端侧的一个启发式常量，不是权威值——`generateTunnelToken`
 * 每次调用都重新算 `Exp = now + expiry`，没有固定 `iat`/`jti`，所以两次
 * 相隔仅 1s 的签发返回的 JWT 字符串几乎总是不同（即使 userID/deviceID/
 * roles/anchor 完全相同）。字符串相等判断因此在生产环境里几乎永远不
 * 会命中——真正有意义的判据是"剩余寿命是否跌破续期阈值"，与服务端
 * `maybeRenewTunnelToken` 的 50% 门镜像。此常量若与服务端配置漂移，
 * 只影响本地同步频率，不影响正确性：真实 API 请求永远带着当前有效
 * token，与本地是否采纳无关。
 */
const TUNNEL_TOKEN_LIFETIME_SECONDS = 7776000; // 90d

/**
 * 解码 JWT payload 拿到 `exp`（epoch 秒）。不验签——这是客户端优化用的
 * 启发式判断，不是安全校验。解码失败（格式不对/非 JSON/缺字段）返回 null。
 */
function decodeJwtExp(token: string): number | null {
  try {
    const parts = token.split('.');
    if (parts.length !== 3) return null;
    let b64 = parts[1].replace(/-/g, '+').replace(/_/g, '/');
    while (b64.length % 4 !== 0) b64 += '=';
    const payload = JSON.parse(atob(b64));
    return typeof payload.exp === 'number' ? payload.exp : null;
  } catch {
    return null;
  }
}

/**
 * 判断当前已存的 tunnel token 是否"足够陈旧"，值得采纳一份新到达的
 * token：没存过、解不出 exp、或剩余寿命跌破配置寿命的 50%——与服务端
 * `maybeRenewTunnelToken` 的滚动续期门保持同一判据（spec §4.3）。
 */
function isStoredTunnelTokenStale(stored: string | null): boolean {
  if (!stored) return true;
  const exp = decodeJwtExp(stored);
  if (exp === null) return true;
  const remaining = exp - Math.floor(Date.now() / 1000);
  return remaining * 2 < TUNNEL_TOKEN_LIFETIME_SECONDS;
}

export async function adoptTunnelToken(token: string | undefined): Promise<void> {
  if (!token) return;
  try {
    const current = await authService.getTunnelToken();
    if (!isStoredTunnelTokenStale(current)) return;
    await authService.setTunnelToken(token);
    await syncNativeVpnConfig(token);
  } catch (err) {
    console.warn('[TunnelToken] adopt failed:', err);
  }
}

async function syncNativeVpnConfig(token: string): Promise<void> {
  const os = window._platform?.os;
  if (os !== 'ios' && os !== 'android') return;
  try {
    const udid = await authService.getUdid();
    const res = await window._k2.run('sync-credential', { udid, token });
    if (res.code !== 0) {
      console.warn('[TunnelToken] native config sync skipped:', res.message);
    }
  } catch (err) {
    // 旧原生包没有 updateConfig —— 无害：原生存的旧 token 仍有 ≥45 天
    // 寿命，下次 connect 会整体覆盖。不 bump minNativeVersion。
    console.warn('[TunnelToken] native config sync failed:', err);
  }
}
