/**
 * usePrivateNodes Hook — 专属节点订阅列表（Stale-While-Revalidate）
 *
 * 设计完全镜像 useUser：
 * - 首次加载：等待请求
 * - 后续访问：立即返回缓存，后台刷新（SWR）
 * - 失败时回退到过期缓存
 * - 未登录直接返回空列表（不发请求）
 *
 * 数据来源是 Phase 1-3 已落地的 service `getPrivateNodes()`
 * （GET /api/user/private-nodes，经 cloudApi 注入鉴权）。
 */

import { useState, useEffect, useCallback } from 'react';
import { cacheStore } from '../services/cache-store';
import { useAuthStore } from '../stores/auth.store';
import { getPrivateNodes } from '../services/private-node-service';
import type { PrivateNodeSubscriptionView } from '../services/api-types';

const CACHE_KEY = 'api:private_nodes';
// 比 useUser 的 1h 短：节点开通/流量状态变化更频繁，60s 平衡新鲜度与请求量。
const CACHE_TTL_SECONDS = 60;

interface UsePrivateNodesReturn {
  nodes: PrivateNodeSubscriptionView[];
  loading: boolean;
  error: Error | null;
  refresh: (forceRefresh?: boolean) => Promise<void>;
}

export function usePrivateNodes(): UsePrivateNodesReturn {
  const [nodes, setNodes] = useState<PrivateNodeSubscriptionView[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<Error | null>(null);
  const isAuthenticated = useAuthStore((s) => s.isAuthenticated);

  const refresh = useCallback(
    async (forceRefresh: boolean = false) => {
      if (!isAuthenticated) {
        setNodes([]);
        setError(null);
        setLoading(false);
        return;
      }

      setLoading(true);
      try {
        // 命中缓存：立即返回 + 后台 revalidate（SWR）
        if (!forceRefresh) {
          const cached = cacheStore.get<PrivateNodeSubscriptionView[]>(CACHE_KEY);
          if (cached) {
            setNodes(cached);
            setError(null);
            setLoading(false);
            getPrivateNodes()
              .then((resp) => {
                cacheStore.set(CACHE_KEY, resp.items, { ttl: CACHE_TTL_SECONDS });
                setNodes(resp.items);
              })
              .catch((e) => {
                // 后台刷新失败不打扰用户，保留缓存数据
                console.warn('[usePrivateNodes] background revalidate failed:', e);
              });
            return;
          }
        }

        const resp = await getPrivateNodes();
        cacheStore.set(CACHE_KEY, resp.items, { ttl: CACHE_TTL_SECONDS });
        setNodes(resp.items);
        setError(null);
      } catch (e) {
        // 失败时回退到过期缓存（若有）
        if (!forceRefresh) {
          const expired = cacheStore.get<PrivateNodeSubscriptionView[]>(CACHE_KEY, true);
          if (expired) {
            setNodes(expired);
            setError(null);
            setLoading(false);
            return;
          }
        }
        console.error('[usePrivateNodes] Error:', e);
        setError(e instanceof Error ? e : new Error(String(e)));
      } finally {
        setLoading(false);
      }
    },
    [isAuthenticated]
  );

  useEffect(() => {
    void refresh();
  }, [refresh]);

  return { nodes, loading, error, refresh };
}
