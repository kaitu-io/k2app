/**
 * membership-format — iOS 会员中心的纯展示逻辑（无 React、无副作用、可单测）。
 * 仅做「到期 unix 秒 → 剩余天数 / 紧迫度 / 本地化日期」的转换。
 */

import { formatDate } from '../../utils/time';

export type ExpiryUrgency = 'normal' | 'warning' | 'critical';

/** 剩余天数（向上取整；已过期或无到期 → 0）。 */
export function daysRemaining(expiredAtSec: number, nowSec: number): number {
  if (!expiredAtSec || expiredAtSec <= nowSec) return 0;
  return Math.ceil((expiredAtSec - nowSec) / 86400);
}

/** 紧迫度分级：≤3 天 critical（红）、≤7 天 warning（橙）、其余 normal。 */
export function expiryUrgency(days: number): ExpiryUrgency {
  if (days <= 3) return 'critical';
  if (days <= 7) return 'warning';
  return 'normal';
}

/** 到期日期（ISO 格式 YYYY-MM-DD；无效/0 → 空串，调用方据此决定是否渲染）。 */
export function formatExpiryDate(sec: number): string {
  if (!sec || sec <= 0) return '';
  return formatDate(sec);
}

/** MUI 调色板色名，按紧迫度映射（供 Typography/Chip 的 color 使用）。 */
export function urgencyColor(urgency: ExpiryUrgency): 'success' | 'warning' | 'error' {
  switch (urgency) {
    case 'critical':
      return 'error';
    case 'warning':
      return 'warning';
    default:
      return 'success';
  }
}
