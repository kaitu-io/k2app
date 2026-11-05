/**
 * billing-issue — 付款失败（Apple grace / billing retry，Stripe past_due）订阅的判定与常量。
 * 服务端在 user.subscriptions[] 上下发 `billingIssue: true`；老服务端不带该字段，视为 false。
 */
import type { DataSubscription } from '../services/api-types';

/** Apple 账户「付款与配送」页（仅 iOS 可用 itms-apps 方案）。 */
export const APPLE_BILLING_URL = 'itms-apps://apps.apple.com/account/billing';

/** 返回第一条付款失败的订阅；没有则 undefined。 */
export function billingIssueSub(subs: DataSubscription[] | undefined): DataSubscription | undefined {
  return subs?.find((s) => s.billingIssue === true);
}
