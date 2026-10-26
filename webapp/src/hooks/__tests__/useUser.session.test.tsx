/**
 * useUser 换账号刷新 —— 回归测试。
 *
 * 缺陷（2026-10-27，iOS 4.4.14 真机暴露）：免登录设备账号让 isAuthenticated
 * 从启动起就是 true。在设备账号上登录真实账号时它不翻转，useUser 只随
 * isAuthenticated 重新加载，于是账户页停在设备账号（「未开通」）直到重启。
 * 登录路径都会 cacheStore.clear() + setIsAuthenticated(true)；后者推进
 * sessionEpoch，useUser 盯它重新加载。
 *
 * 用真实的 auth store 与 useUser，只 mock 网络。
 */

import { describe, it, expect, vi, beforeEach } from 'vitest';
import { renderHook, act, waitFor } from '@testing-library/react';
import type { DataUser } from '../../services/api-types';

const mockGet = vi.fn();
vi.mock('../../services/cloud-api', () => ({
  cloudApi: { get: (...args: unknown[]) => mockGet(...args) },
}));

const setDetectedProfile = vi.fn();
vi.mock('../../stores/config.store', () => ({
  useConfigStore: { getState: () => ({ setDetectedProfile }) },
}));

import { useUser } from '../useUser';
import { useAuthStore } from '../../stores/auth.store';
import { cacheStore } from '../../services/cache-store';

const user = (uuid: string, expiredAt: number): DataUser => ({
  uuid,
  expiredAt,
  isFirstOrderDone: false,
  loginIdentifies: [],
  deviceCount: 0,
  hasPassword: false,
});

const DEVICE = user('user-device', 0);
const MEMBER = user('user-member', 1_900_000_000);

describe('useUser 换账号刷新', () => {
  beforeEach(() => {
    vi.clearAllMocks();
    cacheStore.clear();
    localStorage.clear();
    useAuthStore.setState({ isAuthenticated: true, sessionEpoch: 1 });
  });

  it('已登录（设备账号）时再登录另一个账号，已挂载的实例换成新账号', async () => {
    mockGet.mockResolvedValue({ code: 0, data: DEVICE });
    const a = renderHook(() => useUser());
    const b = renderHook(() => useUser());
    await waitFor(() => {
      expect(a.result.current.user?.uuid).toBe('user-device');
      expect(b.result.current.user?.uuid).toBe('user-device');
    });

    // 登录成功路径的尾巴：清缓存 + setIsAuthenticated(true)（isAuthenticated 本来就是 true）
    mockGet.mockResolvedValue({ code: 0, data: MEMBER });
    const callsBefore = mockGet.mock.calls.length;
    act(() => {
      cacheStore.clear();
      useAuthStore.getState().setIsAuthenticated(true);
    });

    await waitFor(() => {
      expect(a.result.current.user?.uuid).toBe('user-member');
      expect(b.result.current.user?.uuid).toBe('user-member');
    });
    // 两个实例合并成一次请求
    expect(mockGet.mock.calls.length - callsBefore).toBe(1);
  });

  it('setIsAuthenticated(true) 每次推进 sessionEpoch，登出不推进', () => {
    const e0 = useAuthStore.getState().sessionEpoch;
    useAuthStore.getState().setIsAuthenticated(true);
    expect(useAuthStore.getState().sessionEpoch).toBe(e0 + 1);
    useAuthStore.getState().setIsAuthenticated(false);
    expect(useAuthStore.getState().sessionEpoch).toBe(e0 + 1);
    expect(useAuthStore.getState().isAuthenticated).toBe(false);
  });
});
