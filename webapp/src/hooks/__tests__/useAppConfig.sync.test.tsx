/**
 * useAppConfig 跨实例同步 —— 回归测试。
 *
 * 与 useUser.sync.test.tsx 同型。useAppConfig 被 7 处调用，每处各持一份 useState，
 * 且它的取数 effect 依赖是 `[]`——挂载一次就再也不重拉。所以没有缓存广播时，
 * 任何一处（或别的模块）刷新了配置，其余实例会一直停在旧值直到组件卸载重挂；
 * cacheStore.clear()（切换账号）之后更是会永远显示上一个账号的配置。
 *
 * 这里用**真实的 useAppConfig**，只 mock 网络。
 */

import { describe, it, expect, vi, beforeEach } from 'vitest';
import { renderHook, act, waitFor } from '@testing-library/react';
import type { AppConfig } from '../../services/api-types';

const mockGet = vi.fn();
vi.mock('../../services/cloud-api', () => ({
  cloudApi: { get: (...args: unknown[]) => mockGet(...args) },
}));

import { useAppConfig } from '../useAppConfig';
import { cacheStore } from '../../services/cache-store';

const CONFIG_CACHE_KEY = 'api:app_config';

const configWithReward = (purchaseRewardDays: number): AppConfig => ({
  appLinks: {
    baseURL: 'https://example.test',
    installPath: '/i',
    discoveryPath: '/d',
    privacyPath: '/p',
    termsPath: '/t',
    walletPath: '/w',
  } as AppConfig['appLinks'],
  inviteReward: {
    purchaseRewardDays,
    inviterPurchaseRewardDays: 7,
    minRewardMonths: 12,
  },
});

describe('useAppConfig 跨实例同步', () => {
  beforeEach(() => {
    vi.clearAllMocks();
    cacheStore.clear();
    localStorage.clear();
    // clearAllMocks 清掉实现，每个用例重设。
    mockGet.mockResolvedValue({ code: 0, data: configWithReward(30) });
  });

  it('一个实例写入缓存后，另一个已挂载的实例看到新配置', async () => {
    const a = renderHook(() => useAppConfig());
    const b = renderHook(() => useAppConfig());

    await waitFor(() => {
      expect(a.result.current.appConfig?.inviteReward.purchaseRewardDays).toBe(30);
      expect(b.result.current.appConfig?.inviteReward.purchaseRewardDays).toBe(30);
    });

    // 别处刷新了配置（后台 revalidate / 另一个页面的取数）。
    act(() => {
      cacheStore.set(CONFIG_CACHE_KEY, configWithReward(60), { ttl: 3600 });
    });

    await waitFor(() => {
      expect(a.result.current.appConfig?.inviteReward.purchaseRewardDays).toBe(60);
      expect(b.result.current.appConfig?.inviteReward.purchaseRewardDays).toBe(60);
    });
  });

  it('effect 依赖是 [] 也能拿到更新 —— 不依赖重新挂载', async () => {
    const { result, rerender } = renderHook(() => useAppConfig());
    await waitFor(() =>
      expect(result.current.appConfig?.inviteReward.purchaseRewardDays).toBe(30),
    );

    // 取数只发生过一次：证明后面的更新确实来自广播而不是重新取数。
    expect(mockGet).toHaveBeenCalledTimes(1);

    act(() => {
      cacheStore.set(CONFIG_CACHE_KEY, configWithReward(90), { ttl: 3600 });
    });
    rerender();

    await waitFor(() =>
      expect(result.current.appConfig?.inviteReward.purchaseRewardDays).toBe(90),
    );
    expect(mockGet).toHaveBeenCalledTimes(1);
  });
});
