import { describe, it, expect, vi, beforeEach } from 'vitest';
import { renderHook } from '@testing-library/react';
import { useSubscriptionAffordance } from '../useSubscriptionAffordance';
import type { DataUser } from '../../services/api-types';

const mockUser = vi.fn<[], { user: DataUser | null }>();
vi.mock('../useUser', () => ({ useUser: () => mockUser() }));

const baseUser: DataUser = {
  uuid: 'user-x', expiredAt: 0, isFirstOrderDone: false,
  loginIdentifies: [], deviceCount: 0, hasPassword: false,
};

describe('useSubscriptionAffordance', () => {
  beforeEach(() => vi.clearAllMocks());

  it('no user (prospect/logged-out) → subscribe', () => {
    mockUser.mockReturnValue({ user: null });
    const { result } = renderHook(() => useSubscriptionAffordance());
    expect(result.current.mode).toBe('subscribe');
  });

  it('expired user, no subs → subscribe', () => {
    mockUser.mockReturnValue({ user: { ...baseUser, expiredAt: 1 } });
    const { result } = renderHook(() => useSubscriptionAffordance());
    expect(result.current.mode).toBe('subscribe');
  });

  it('active additive member (future expiry, no subs) → status', () => {
    mockUser.mockReturnValue({ user: { ...baseUser, expiredAt: Math.floor(Date.now() / 1000) + 9_000_000 } });
    const { result } = renderHook(() => useSubscriptionAffordance());
    expect(result.current.mode).toBe('status');
  });

  it('active apple sub → manage', () => {
    mockUser.mockReturnValue({
      user: {
        ...baseUser,
        expiredAt: Math.floor(Date.now() / 1000) + 9_000_000,
        subscriptions: [{ provider: 'apple', tier: 'basic', currentPeriodEnd: 0, autoRenew: true, manage: { kind: 'apple_settings' } }],
      },
    });
    const { result } = renderHook(() => useSubscriptionAffordance());
    expect(result.current.mode).toBe('manage');
  });
});
