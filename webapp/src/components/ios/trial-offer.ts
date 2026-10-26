import type { IapProduct } from '../../types/kaitu-core';

/** periodUnit → 天数。year / unknown 不在表里：不猜，按"无试用"处理。 */
const DAYS_PER_UNIT: Partial<Record<string, number>> = { day: 1, week: 7, month: 30 };

/**
 * 商品配置的免费试用时长（天），**不看资格**。试用进行中 StoreKit 报告
 * `isEligibleForIntroOffer === false`（资格已用掉），会员页试用卡要靠这个拿总天数。
 * 非免费试用（payUpFront / payAsYouGo）、无 introOffer、无法换算的单位 → null。
 */
export function trialLengthOf(p?: IapProduct): { days: number } | null {
  const offer = p?.introOffer;
  if (!offer || offer.paymentMode !== 'freeTrial') return null;
  const perUnit = DAYS_PER_UNIT[offer.periodUnit];
  if (!perUnit || !(offer.periodValue > 0)) return null;
  return { days: offer.periodValue * perUnit };
}

/**
 * StoreKit 商品是否对当前 Apple ID 提供免费试用，以及试用天数。
 *
 * `isEligibleForIntroOffer` 在每个订阅商品上都会带（即使没配优惠），所以必须与
 * `introOffer.paymentMode === 'freeTrial'` 同时成立。旧壳两字段都没有 → null。
 */
export function trialOfferOf(p?: IapProduct): { days: number } | null {
  if (!p || p.isEligibleForIntroOffer !== true) return null;
  return trialLengthOf(p);
}
