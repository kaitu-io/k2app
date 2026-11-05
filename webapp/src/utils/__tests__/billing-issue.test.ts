import { describe, it, expect } from 'vitest';
import { billingIssueSub } from '../billing-issue';
import type { DataSubscription } from '../../services/api-types';

const s = (over: Partial<DataSubscription>): DataSubscription => ({
  provider: 'apple', tier: 'basic', currentPeriodEnd: 1, autoRenew: true,
  manage: { kind: 'apple_settings' }, isTrial: false, trialEndsAt: 0, ...over,
});

describe('billingIssueSub', () => {
  it('undefined / empty -> undefined', () => {
    expect(billingIssueSub(undefined)).toBeUndefined();
    expect(billingIssueSub([])).toBeUndefined();
  });
  it('all false -> undefined', () => {
    expect(billingIssueSub([s({ billingIssue: false })])).toBeUndefined();
  });
  it('field absent (old server) -> undefined', () => {
    expect(billingIssueSub([s({})])).toBeUndefined();
  });
  it('returns first flagged sub', () => {
    const b = s({ billingIssue: true, provider: 'stripe' });
    expect(billingIssueSub([s({ billingIssue: false }), b, s({ billingIssue: true })])).toBe(b);
  });
});
