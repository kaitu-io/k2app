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
import { k2api } from '../services/k2api';
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
      const response = await k2api({
        cache: {
          key: 'api:user_info',
          ttl: 3600, // 1小时
          revalidate: !forceRefresh, // forceRefresh 时不使用 revalidate
          allowExpired: !forceRefresh, // forceRefresh 时不允许过期缓存
          forceRefresh: forceRefresh // 强制刷新时跳过缓存
        }
      }).exec<DataUser>('api_request', {
        method: 'GET',
        path: '/api/user/info'
      });

      if (response.code === 0 && response.data) {
        setUser(response.data);
      } else if (response.code === 401) {
        // 401 已被 k2api 处理，这里只需清空 user
        setUser(null);
      } else {
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
