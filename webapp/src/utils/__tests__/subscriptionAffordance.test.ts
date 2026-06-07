import { describe, it, expect } from 'vitest';
import { subscriptionAffordance } from '../subscriptionAffordance';
import type { DataSubscription } from '../../services/api-types';

const NOW = 1_700_000_000;
const DAY = 86400;
const appleSub: DataSubscription = {
  provider: 'apple',
  tier: 'basic',
  currentPeriodEnd: NOW + 300 * DAY,
  autoRenew: true,
  manage: { kind: 'apple_settings' },
};

describe('subscriptionAffordance (renewWindowDays = 0, 方案 A)', () => {
  it('expired, no sub → subscribe', () => {
    const r = subscriptionAffordance({ expiredAt: NOW - DAY, subscriptions: [], nowSec: NOW, renewWindowDays: 0 });
    expect(r.mode).toBe('subscribe');
  });

  it('active additive member, no sub → status (no double-pay)', () => {
    const r = subscriptionAffordance({ expiredAt: NOW + 100 * DAY, subscriptions: [], nowSec: NOW, renewWindowDays: 0 });
    expect(r.mode).toBe('status');
  });

  it('active recurring sub → manage, carries the sub', () => {
    const r = subscriptionAffordance({ expiredAt: NOW + 300 * DAY, subscriptions: [appleSub], nowSec: NOW, renewWindowDays: 0 });
    expect(r.mode).toBe('manage');
    expect(r.activeSub).toBe(appleSub);
  });

  it('sub present takes precedence even if expiry already passed', () => {
    const r = subscriptionAffordance({ expiredAt: NOW - DAY, subscriptions: [appleSub], nowSec: NOW, renewWindowDays: 0 });
    expect(r.mode).toBe('manage');
  });
});

describe('subscriptionAffordance (renewWindowDays = 30, 方案 B)', () => {
  it('additive member within window → subscribe', () => {
    const r = subscriptionAffordance({ expiredAt: NOW + 20 * DAY, subscriptions: [], nowSec: NOW, renewWindowDays: 30 });
    expect(r.mode).toBe('subscribe');
  });

  it('additive member beyond window → status', () => {
    const r = subscriptionAffordance({ expiredAt: NOW + 90 * DAY, subscriptions: [], nowSec: NOW, renewWindowDays: 30 });
    expect(r.mode).toBe('status');
  });
});
