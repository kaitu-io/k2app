import { describe, it, expect } from 'vitest';
import { trialOfferOf, trialLengthOf, trialUsedUp } from '../trial-offer';
import type { IapProduct } from '../../../types/kaitu-core';

function product(over: Partial<IapProduct> = {}): IapProduct {
  return {
    id: 'sub.1y', displayName: 'x', description: 'x', displayPrice: '$12.99',
    price: 88, periodUnit: 'year', periodValue: 1, ...over,
  } as IapProduct;
}
const offer = (periodUnit: any, periodValue: number, paymentMode: any = 'freeTrial') =>
  ({ paymentMode, periodUnit, periodValue, displayPrice: '$0.00' });

describe('trialOfferOf', () => {
  it('week x1 -> 7 days', () => {
    expect(trialOfferOf(product({ isEligibleForIntroOffer: true, introOffer: offer('week', 1) }))).toEqual({ days: 7 });
  });
  it('day x3 -> 3 days', () => {
    expect(trialOfferOf(product({ isEligibleForIntroOffer: true, introOffer: offer('day', 3) }))).toEqual({ days: 3 });
  });
  it('month x1 -> 30 days', () => {
    expect(trialOfferOf(product({ isEligibleForIntroOffer: true, introOffer: offer('month', 1) }))).toEqual({ days: 30 });
  });
  it('not eligible -> null', () => {
    expect(trialOfferOf(product({ isEligibleForIntroOffer: false, introOffer: offer('week', 1) }))).toBeNull();
  });
  it('eligible but no intro offer -> null (flag is on every subscription)', () => {
    expect(trialOfferOf(product({ isEligibleForIntroOffer: true }))).toBeNull();
  });
  it('payUpFront -> null', () => {
    expect(trialOfferOf(product({ isEligibleForIntroOffer: true, introOffer: offer('week', 1, 'payUpFront') }))).toBeNull();
  });
  it('fields missing (old shell) -> null', () => {
    expect(trialOfferOf(product())).toBeNull();
    expect(trialOfferOf(undefined)).toBeNull();
  });
  it('year / unknown unit -> null (no guessing)', () => {
    expect(trialOfferOf(product({ isEligibleForIntroOffer: true, introOffer: offer('year', 1) }))).toBeNull();
    expect(trialOfferOf(product({ isEligibleForIntroOffer: true, introOffer: offer('unknown', 1) }))).toBeNull();
  });
  it('non-positive period -> null', () => {
    expect(trialOfferOf(product({ isEligibleForIntroOffer: true, introOffer: offer('day', 0) }))).toBeNull();
  });
});

describe('trialUsedUp', () => {
  const USED = { isEligibleForIntroOffer: false, introOffer: offer('week', 1) };
  const user = (over: Record<string, unknown> = {}) => ({ isFirstOrderDone: false, subscriptions: [], ...over }) as any;

  it('true for ineligible + never paid + no active sub', () => {
    expect(trialUsedUp(product(USED), user())).toBe(true);
    expect(trialUsedUp(product(USED), user({ isFirstOrderDone: undefined, subscriptions: undefined }))).toBe(true);
  });
  it('false when eligible / paid before / has active apple sub / product has no trial / no user', () => {
    expect(trialUsedUp(product({ ...USED, isEligibleForIntroOffer: true }), user())).toBe(false);
    expect(trialUsedUp(product({ ...USED, isEligibleForIntroOffer: undefined }), user())).toBe(false);
    expect(trialUsedUp(product(USED), user({ isFirstOrderDone: true }))).toBe(false);
    expect(trialUsedUp(product(USED), user({ subscriptions: [{ provider: 'apple' }] }))).toBe(false);
    expect(trialUsedUp(product({ isEligibleForIntroOffer: false }), user())).toBe(false);
    expect(trialUsedUp(undefined, user())).toBe(false);
    expect(trialUsedUp(product(USED), null)).toBe(false);
  });
  it('a non-apple sub does not count as active apple sub', () => {
    expect(trialUsedUp(product(USED), user({ subscriptions: [{ provider: 'stripe' }] }))).toBe(true);
  });
});

describe('trialLengthOf (ignores eligibility — used during an active trial)', () => {
  it('eligible=false still returns the trial length', () => {
    expect(trialLengthOf(product({ isEligibleForIntroOffer: false, introOffer: offer('week', 1) }))).toEqual({ days: 7 });
  });
  it('eligibility field absent still returns the trial length', () => {
    expect(trialLengthOf(product({ introOffer: offer('day', 3) }))).toEqual({ days: 3 });
  });
  it('payUpFront -> null', () => {
    expect(trialLengthOf(product({ isEligibleForIntroOffer: false, introOffer: offer('week', 1, 'payUpFront') }))).toBeNull();
  });
  it('missing intro offer / product -> null', () => {
    expect(trialLengthOf(product({ isEligibleForIntroOffer: false }))).toBeNull();
    expect(trialLengthOf(undefined)).toBeNull();
  });
  it('year / zero period -> null', () => {
    expect(trialLengthOf(product({ introOffer: offer('year', 1) }))).toBeNull();
    expect(trialLengthOf(product({ introOffer: offer('week', 0) }))).toBeNull();
  });
});
