/**
 * useUser Hook - 用 k2api revalidate 替代 user.store.ts
 *
 * 设计：
 * - 使用 k2api 的 revalidate 功能
 * - 长 TTL (1小时) + 后台刷新 = 和 store 一样的体验
 * - 无需手动管理 loading/error state
 * - 无需编写 store actions
 *
 * 优势：
 * 1. 立即返回缓存数据（无等待）
 * 2. 后台自动刷新保证数据最终一致
 * 3. 401/402 自动处理
 * 4. 代码更简单（无需 store 样板代码）
 */

import { useState, useEffect, useMemo } from 'react';
import { cloudApi } from '../services/cloud-api';
import { cacheStore } from '../services/cache-store';
import { useAuthStore } from '../stores/auth.store';
import { useConfigStore } from '../stores/config.store';
import type { DataUser } from '../services/api-types';

/**
 * Push Center-detected country + suggestedProfile into the config store.
 * When autoDetect is on, this also updates selectedCountry.
 *
 * TODO(country-ownership): 前端不应再依赖后端的 currentCountry / suggestedProfile。
 *   后端按请求源 IP 判国家(api/geoip.go maybeUpdateUserCountry)会被 geo-via-tunnel
 *   污染——全局代理下控制面请求走隧道,Center 拿到的是出口节点 IP,误判用户所在国
 *   (本例:东京出口 → currentCountry=jp → 分流要 jp 规则包 → 缺包 504 无法连接)。
 *   目标形态:country 的判定完全由前端自己完成(本地网络环境探测),后端 currentCountry
 *   仅作辅助/参考,不再直接喂进 config store。届时移除本函数对 user.currentCountry /
 *   user.suggestedProfile 的使用。当前先硬钉 cn 兜底。
 */
function syncDetectedProfile(user: DataUser): void {
  useConfigStore.getState().setDetectedProfile({
    country: 'cn', // 强制 cn：cn.krs 内置,永不缺包 504;规避 geo-via-tunnel 污染(出口节点 IP 误判国家)
    profile: user.suggestedProfile ?? null,
  });
}

interface UseUserReturn {
  user: DataUser | null;
  loading: boolean;
  isMembership: boolean;
  isExpired: boolean;
  fetchUser: (forceRefresh?: boolean) => Promise<void>;
}

/**
 * 获取用户信息
 *
 * 使用 Stale-While-Revalidate 策略：
 * - 首次加载：正常等待请求
 * - 后续访问：立即返回缓存，后台刷新
 * - TTL: 1小时（足够长，体验像 store）
 *
 * @example
 * ```tsx
 * function Account() {
 *   const { user, loading, isMembership, refreshUser } = useUser();
 *
 *   if (loading) return <Loading />;
 *   if (!user) return <Login />;
 *
 *   return <div>{user.expiredAt}</div>;
 * }
 * ```
 */
export function useUser(): UseUserReturn {
  const [user, setUser] = useState<DataUser | null>(null);
  const [loading, setLoading] = useState(true);
  const isAuthenticated = useAuthStore((s) => s.isAuthenticated);

  // 加载用户数据
  const fetchUser = async (forceRefresh: boolean = false) => {
    if (!isAuthenticated) {
      setUser(null);
      setLoading(false);
      return;
    }

    setLoading(true);
    try {
      // Check cache first (unless force refresh)
      if (!forceRefresh) {
        const cached = cacheStore.get<DataUser>('api:user_info');
        if (cached) {
          setUser(cached);
          syncDetectedProfile(cached);
          setLoading(false);
          // Background revalidate
          cloudApi.get<DataUser>('/api/user/info').then(res => {
            if (res.code === 0 && res.data) {
              cacheStore.set('api:user_info', res.data, { ttl: 3600 });
              setUser(res.data);
              syncDetectedProfile(res.data);
            }
          });
          return;
        }
      }

      const response = await cloudApi.get<DataUser>('/api/user/info');

      if (response.code === 0 && response.data) {
        setUser(response.data);
        cacheStore.set('api:user_info', response.data, { ttl: 3600 });
        syncDetectedProfile(response.data);
      } else if (response.code === 401) {
        setUser(null);
      } else {
        // On failure, try expired cache as fallback
        if (!forceRefresh) {
          const expired = cacheStore.get<DataUser>('api:user_info', true);
          if (expired) {
            setUser(expired);
            setLoading(false);
            return;
          }
        }
        console.warn('[useUser] Failed to fetch user:', response.message);
      }
    } catch (error) {
      console.error('[useUser] Error:', error);
    } finally {
      setLoading(false);
    }
  };

  // 初次加载
  useEffect(() => {
    fetchUser();
  }, [isAuthenticated]);

  // 计算派生状态
  const isMembership = useMemo(() => {
    if (!user) return false;
    return user.expiredAt > Date.now() / 1000;
  }, [user]);

  const isExpired = useMemo(() => {
    return !isMembership;
  }, [isMembership]);

  return {
    user,
    loading,
    isMembership,
    isExpired,
    fetchUser
  };
}
