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

export async function adoptTunnelToken(token: string | undefined): Promise<void> {
  if (!token) return;
  try {
    const current = await authService.getTunnelToken();
    if (current === token) return;
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
