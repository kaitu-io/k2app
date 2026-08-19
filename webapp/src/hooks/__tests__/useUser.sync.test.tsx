/**
 * useUser 跨实例同步 —— 回归测试。
 *
 * 缺陷（2026-08-19，iOS IAP 真机暴露）：购买成功后授权日期不刷新。
 * useUser 在 17 个调用点各持一份 useState，cacheStore 只写不广播，所以
 * `verifyAndGrant` 写入的新用户数据只有发起购买的那个组件能看到；父级
 * Purchase.tsx 与 useSubscriptionAffordance 内部的实例都停在旧值，
 * affordance 不翻转，UI 原地不动。
 *
 * 这些测试用**真实的 useUser**（只 mock 掉网络与两个 store）——把 useUser 换成
 * 替身的测试结构上测不出跨实例同步。
 */

import { describe, it, expect, vi, beforeEach } from 'vitest';
import { renderHook, act, waitFor } from '@testing-library/react';
import type { DataUser } from '../../services/api-types';

const mockGet = vi.fn();
vi.mock('../../services/cloud-api', () => ({
  cloudApi: { get: (...args: unknown[]) => mockGet(...args) },
}));

vi.mock('../../stores/auth.store', () => ({
  useAuthStore: (selector: (s: { isAuthenticated: boolean }) => unknown) =>
    selector({ isAuthenticated: true }),
}));

const setDetectedProfile = vi.fn();
vi.mock('../../stores/config.store', () => ({
  useConfigStore: { getState: () => ({ setDetectedProfile }) },
}));

import { useUser } from '../useUser';
import { cacheStore } from '../../services/cache-store';

const USER_CACHE_KEY = 'api:user_info';

const userAt = (expiredAt: number): DataUser => ({
  uuid: 'user-9378',
  expiredAt,
  isFirstOrderDone: false,
  loginIdentifies: [],
  deviceCount: 0,
  hasPassword: false,
});

const OLD_EXPIRY = 1_786_767_369; // 2026-08-15，购买前
const NEW_EXPIRY = 1_818_303_369; // 一年后，购买后 Center 返回的值

describe('useUser 跨实例同步', () => {
  beforeEach(() => {
    vi.clearAllMocks();
    cacheStore.clear();
    localStorage.clear();
    // clearAllMocks 会清掉实现，每个用例重设。
    mockGet.mockResolvedValue({ code: 0, data: userAt(OLD_EXPIRY) });
  });

  it('一个实例写入缓存后，另一个已挂载的实例看到新的 expiredAt', async () => {
    // 两个实例并存 —— 真实场景里是 Purchase.tsx、useSubscriptionAffordance、
    // IosSubscribePanel 各自一份。
    const a = renderHook(() => useUser());
    const b = renderHook(() => useUser());

    await waitFor(() => {
      expect(a.result.current.user?.expiredAt).toBe(OLD_EXPIRY);
      expect(b.result.current.user?.expiredAt).toBe(OLD_EXPIRY);
    });

    // verifyAndGrant 成功后做的事：把 Center 返回的新用户写进缓存。
    act(() => {
      cacheStore.set(USER_CACHE_KEY, userAt(NEW_EXPIRY), { ttl: 3600 });
    });

    // 两个实例都必须看到新到期日，而不只是写入方。
    await waitFor(() => {
      expect(a.result.current.user?.expiredAt).toBe(NEW_EXPIRY);
      expect(b.result.current.user?.expiredAt).toBe(NEW_EXPIRY);
    });
  });

  it('isMembership 随广播重算 —— 过期用户购买后立即变成会员', async () => {
    mockGet.mockResolvedValue({ code: 0, data: userAt(1) }); // 早已过期
    const { result } = renderHook(() => useUser());

    await waitFor(() => expect(result.current.isMembership).toBe(false));

    act(() => {
      cacheStore.set(USER_CACHE_KEY, userAt(NEW_EXPIRY), { ttl: 3600 });
    });

    await waitFor(() => {
      expect(result.current.isMembership).toBe(true);
      expect(result.current.isExpired).toBe(false);
    });
  });

  it('缓存被清除时实例不炸，且不会把 user 打成脏值', async () => {
    const { result } = renderHook(() => useUser());
    await waitFor(() => expect(result.current.user?.expiredAt).toBe(OLD_EXPIRY));

    act(() => {
      cacheStore.delete(USER_CACHE_KEY);
    });

    // 登出等场景：缓存没了不代表要把界面打成 null 抖动，但绝不能拿到脏值。
    await waitFor(() => {
      const u = result.current.user;
      expect(u === null || u.expiredAt === OLD_EXPIRY).toBe(true);
    });
  });
});
