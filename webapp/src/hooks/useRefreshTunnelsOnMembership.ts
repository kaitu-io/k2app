/**
 * useRefreshTunnelsOnMembership — 会员由无到有时重拉云端隧道列表。
 *
 * 问题：隧道接口返回 402 后 connection.store 抬起 `cloudAccessRevoked`，首页主按钮
 * 因此把点击引去 /purchase；这个标记只在隧道列表**拉取成功**时才会放下。而 CloudTunnelList
 * 的重拉触发点只有首次加载 / 5 分钟定时 / 新登录会话 / 服务恢复——没有「会员变化」。
 * 结果新账号购买成功（含 Apple 免费试用）后，Center 已发放会员，客户端却一直按
 * 「无会员」处理，直到 5 分钟定时刷新或重启（工单 3997、4042）。
 *
 * 触发条件以**状态**为锚，而不是挂在某个购买入口上：`cloudAccessRevoked` 为真，且
 * 用户信息显示会员有效（expiredAt 在未来）。所有入账路径（IAP 购买/恢复、NextPay 回跳、
 * 兑换码、邀请奖励、restoreDeviceAccount 换号）最终都会更新用户信息——要么写
 * `api:user_info` 缓存（useUser 跨实例同步），要么 fetchUser(true)——Dashboard 的
 * useUser 因而拿到新 expiredAt，这里就会触发。
 *
 * 不会自激：服务端若仍返回 402，`cloudAccessRevoked` 保持为真、expiredAt 不变，
 * 依赖不变，不会再次触发。依赖写的是输入（revoked / expiredAt / ref），不是回调身份。
 */

import { useEffect, type RefObject } from 'react';
import { useConnectionStore } from '../stores/connection.store';
import type { CloudTunnelListHandle } from '../components/CloudTunnelList';

export function useRefreshTunnelsOnMembership(
  listRef: RefObject<CloudTunnelListHandle | null>,
  expiredAt: number | undefined,
): void {
  const cloudAccessRevoked = useConnectionStore((s) => s.cloudAccessRevoked);

  useEffect(() => {
    if (!cloudAccessRevoked) return;
    if (!expiredAt || expiredAt <= Date.now() / 1000) return;
    console.info('[Dashboard] membership active while cloud access revoked — refreshing tunnels');
    // force：跳过 SWR 快路径，必须真发一次请求；失败静默（5 分钟定时刷新兜底）。
    void listRef.current?.refresh({ force: true }).catch(() => {});
  }, [cloudAccessRevoked, expiredAt, listRef]);
}
