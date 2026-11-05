/**
 * home-trial-state — 首页试用卡片的显示判定（纯函数）。
 * 仅「Apple 试用中 + 已关自动续订」显示；续订开着时会员页已有 TrialStatusCard，首页不重复打扰。
 */

import type { DataUser } from '../../services/api-types';
import { purchaseSurfaceAvailable } from '../../utils/purchase-surface';
import { billingIssueSub } from '../../utils/billing-issue';
import { daysRemaining } from './membership-format';

export type HomeTrialState =
  | { kind: 'none' }
  | { kind: 'renewOff'; daysLeft: number; endsAtSec: number; urgent: boolean };

export function homeTrialState(user: DataUser | null, nowSec: number): HomeTrialState {
  if (!purchaseSurfaceAvailable()) return { kind: 'none' };
  // 付款失败卡片（首页另有展示）优先，试用提醒让位。
  if (billingIssueSub(user?.subscriptions)) return { kind: 'none' };
  const sub = user?.subscriptions?.find((s) => s.provider === 'apple' && s.trialEndsAt > nowSec && s.autoRenew === false);
  if (!sub) return { kind: 'none' };
  const daysLeft = daysRemaining(sub.trialEndsAt, nowSec);
  return { kind: 'renewOff', daysLeft, endsAtSec: sub.trialEndsAt, urgent: daysLeft <= 2 };
}
