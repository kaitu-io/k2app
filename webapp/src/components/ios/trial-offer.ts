import type { IapProduct } from '../../types/kaitu-core';

/** periodUnit → 天数。year / unknown 不在表里：不猜，按"无试用"处理。 */
const DAYS_PER_UNIT: Partial<Record<string, number>> = { day: 1, week: 7, month: 30 };

/**
 * StoreKit 商品是否对当前 Apple ID 提供免费试用，以及试用天数。
 *
 * `isEligibleForIntroOffer` 在每个订阅商品上都会带（即使没配优惠），所以必须与
 * `introOffer.paymentMode === 'freeTrial'` 同时成立。旧壳两字段都没有 → null。
 */
export function trialOfferOf(p?: IapProduct): { days: number } | null {
  if (!p || p.isEligibleForIntroOffer !== true) return null;
  const offer = p.introOffer;
  if (!offer || offer.paymentMode !== 'freeTrial') return null;
  const perUnit = DAYS_PER_UNIT[offer.periodUnit];
  if (!perUnit || !(offer.periodValue > 0)) return null;
  return { days: offer.periodValue * perUnit };
}
