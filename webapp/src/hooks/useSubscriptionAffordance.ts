import { useMemo } from 'react';
import { useUser } from './useUser';
import {
  subscriptionAffordance,
  type AffordanceResult,
} from '../utils/subscriptionAffordance';

/**
 * v1 = 方案 A：活跃会员不兜售订阅。方案 B 时把这里改成 30/60（或读远端配置）。
 */
const RENEW_WINDOW_DAYS = 0;

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
