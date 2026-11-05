import { describe, it, expect, beforeEach } from 'vitest';
import { homeTrialState } from '../home-trial-state';
import type { DataUser, DataSubscription } from '../../../services/api-types';

const NOW = 1_800_000_000;
const DAY = 86400;

function sub(over: Partial<DataSubscription> = {}): DataSubscription {
  return {
    provider: 'apple',
    tier: 'pro',
    currentPeriodEnd: NOW + 3 * DAY,
    autoRenew: false,
    manage: { kind: 'apple_settings' },
    isTrial: true,
    trialEndsAt: NOW + 2.5 * DAY,
    ...over,
  };
}
const userWith = (...subs: DataSubscription[]) => ({ subscriptions: subs }) as unknown as DataUser;

describe('homeTrialState', () => {
  beforeEach(() => {
    (window as any)._platform = { os: 'ios', iap: {} };
  });

  it('renewOff with daysLeft=3, urgent=false', () => {
    const s = homeTrialState(userWith(sub()), NOW);
    expect(s).toEqual({ kind: 'renewOff', daysLeft: 3, endsAtSec: NOW + 2.5 * DAY, urgent: false });
  });

  it('billing issue yields to the home billing issue card', () => {
    expect(homeTrialState(userWith(sub({ billingIssue: true })), NOW)).toEqual({ kind: 'none' });
  });

  it('urgent when daysLeft <= 2', () => {
    const s = homeTrialState(userWith(sub({ trialEndsAt: NOW + 1.5 * DAY })), NOW);
    expect(s).toMatchObject({ kind: 'renewOff', daysLeft: 2, urgent: true });
  });

  it('order-independent: renew-off trial found even when a renewing trial sub comes first', () => {
    const off = sub({ trialEndsAt: NOW + 2.5 * DAY });
    const on = sub({ autoRenew: true, trialEndsAt: NOW + 5 * DAY });
    const expected = { kind: 'renewOff', daysLeft: 3, endsAtSec: NOW + 2.5 * DAY, urgent: false };
    expect(homeTrialState(userWith(on, off), NOW)).toEqual(expected);
    expect(homeTrialState(userWith(off, on), NOW)).toEqual(expected);
  });

  it('none when autoRenew is true', () => {
    expect(homeTrialState(userWith(sub({ autoRenew: true })), NOW)).toEqual({ kind: 'none' });
  });

  it('none when no subscription / null user', () => {
    expect(homeTrialState(userWith(), NOW)).toEqual({ kind: 'none' });
    expect(homeTrialState({} as DataUser, NOW)).toEqual({ kind: 'none' });
    expect(homeTrialState(null, NOW)).toEqual({ kind: 'none' });
  });

  it('none when trial already ended or not a trial', () => {
    expect(homeTrialState(userWith(sub({ trialEndsAt: NOW - 1 })), NOW)).toEqual({ kind: 'none' });
    expect(homeTrialState(userWith(sub({ trialEndsAt: 0, isTrial: false })), NOW)).toEqual({ kind: 'none' });
  });

  it('none for non-apple provider', () => {
    expect(homeTrialState(userWith(sub({ provider: 'stripe' })), NOW)).toEqual({ kind: 'none' });
  });

  it('none when purchase surface unavailable (iOS without IAP)', () => {
    (window as any)._platform = { os: 'ios' };
    expect(homeTrialState(userWith(sub()), NOW)).toEqual({ kind: 'none' });
  });
});
