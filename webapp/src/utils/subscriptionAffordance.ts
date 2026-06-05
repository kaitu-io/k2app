import type { DataSubscription } from '../services/api-types';

export type AffordanceMode = 'subscribe' | 'manage' | 'status';

export interface AffordanceInput {
  /** user.expiredAt — unix 秒 */
  expiredAt: number;
  /** user.subscriptions — 活跃续订列表（跨 provider） */
  subscriptions: DataSubscription[];
  /** 当前时间 unix 秒 */
  nowSec: number;
  /**
   * 续订前置窗口（天）。v1 = 0 ⇒ 方案 A（活跃叠加会员一律 status，仅过期者 subscribe）。
   * 方案 B 把它调到 30/60，让临到期的一次性会员可顺势转订阅。
   */
  renewWindowDays: number;
}

export interface AffordanceResult {
  mode: AffordanceMode;
  activeSub?: DataSubscription;
}

/**
 * 决定 iOS 订阅轨该给用户什么入口。provider 中立、平台无关、纯函数。
 *  - 有任意活跃续订 → manage（指向该 provider 管理面），绝不再兜售（防永久双扣）。
 *  - 否则：已过期或剩余 ≤ 窗口 → subscribe；其余 → status（防为重叠时间二次付费）。
 *
 * 注意：登录态/无用户的处理在 useSubscriptionAffordance（无用户=潜客→subscribe），
 * 本纯函数只看 expiredAt + subscriptions。
 */
export function subscriptionAffordance(input: AffordanceInput): AffordanceResult {
  const { expiredAt, subscriptions, nowSec, renewWindowDays } = input;
  if (subscriptions.length > 0) {
    return { mode: 'manage', activeSub: subscriptions[0] };
  }
  const windowSec = renewWindowDays * 86400;
  if (expiredAt <= nowSec || expiredAt - nowSec <= windowSec) {
    return { mode: 'subscribe' };
  }
  return { mode: 'status' };
}
