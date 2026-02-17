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
import type { DataUser } from '../services/api-types';

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
          setLoading(false);
          // Background revalidate
          cloudApi.get<DataUser>('/api/user/info').then(res => {
            if (res.code === 0 && res.data) {
              cacheStore.set('api:user_info', res.data, { ttl: 3600 });
              setUser(res.data);
            }
          });
          return;
        }
      }

      const response = await cloudApi.get<DataUser>('/api/user/info');

      if (response.code === 0 && response.data) {
        setUser(response.data);
        cacheStore.set('api:user_info', response.data, { ttl: 3600 });
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
