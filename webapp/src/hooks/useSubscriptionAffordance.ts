import { useMemo } from 'react';
import { useUser } from './useUser';
import {
  subscriptionAffordance,
  type AffordanceResult,
} from '../utils/subscriptionAffordance';

/**
 * 非订阅会员剩余 ≤ 该天数时显示订阅面板（而不是无按钮的 status）。
 * 必须覆盖到期提醒的着陆：renewal_3d 推送（api/logic_push_triggers.go，候选人是
 * UTC 当天+3 整天内到期，即发送时最多剩约 4 天）与 7 天提醒邮件都跳 /purchase，
 * 窗口 < 它们 = 用户点进来找不到续费入口。
 */
export const RENEW_WINDOW_DAYS = 7;

/**
 * 全 iOS 购买 UI 的单一事实源：根据 user 决定 subscribe | manage | status。
 * 无用户（未登录潜客）→ subscribe：让其进入购买/登录流，绝不显示 status（否则会
 * 渲染"有效期至 1970"的坏面板）。
 */
export function useSubscriptionAffordance(): AffordanceResult {
  const { user } = useUser();
  return useMemo(() => {
    if (!user) return { mode: 'subscribe' };
    return subscriptionAffordance({
      expiredAt: user.expiredAt ?? 0,
      subscriptions: user.subscriptions ?? [],
      nowSec: Math.floor(Date.now() / 1000),
      renewWindowDays: RENEW_WINDOW_DAYS,
    });
  }, [user]);
}
