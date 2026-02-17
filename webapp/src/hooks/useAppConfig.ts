/**
 * useAppConfig Hook - 获取应用配置
 *
 * 使用 Stale-While-Revalidate 策略：
 * - 首次加载：等待请求
 * - 后续访问：立即返回缓存 + 后台刷新
 * - TTL: 1小时（配置变更频率低）
 *
 * 包含：
 * - appLinks: 应用链接配置
 * - minClientVersion: 最低客户端版本
 * - announcement: 公告信息
 * - inviteReward: 邀请奖励配置
 */

import { useState, useEffect } from 'react';
import { cloudApi } from '../services/cloud-api';
import { cacheStore } from '../services/cache-store';
import type { AppConfig } from '../services/api-types';

interface UseAppConfigReturn {
  appConfig: AppConfig | null;
  loading: boolean;
  error: string | null;
}

/**
 * 获取应用配置
 *
 * @example
 * ```tsx
 * function ForceUpgradeDialog() {
 *   const { appConfig, loading } = useAppConfig();
 *
 *   if (!appConfig) return null;
 *
 *   const minVersion = appConfig.minClientVersion;
 *   const downloadUrl = `${appConfig.appLinks.baseURL}${appConfig.appLinks.installPath}`;
 * }
 * ```
 */
export function useAppConfig(): UseAppConfigReturn {
  const [appConfig, setAppConfig] = useState<AppConfig | null>(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    const fetchConfig = async () => {
      try {
        setLoading(true);

        // Check cache first (SWR: return immediately, refresh in background)
        const cached = cacheStore.get<AppConfig>('api:app_config');
        if (cached) {
          setAppConfig(cached);
          setLoading(false);
          // Background revalidate
          cloudApi.get<AppConfig>('/api/app/config').then(res => {
            if (res.code === 0 && res.data) {
              cacheStore.set('api:app_config', res.data, { ttl: 3600 });
              setAppConfig(res.data);
            }
          });
          return;
        }

        const response = await cloudApi.get<AppConfig>('/api/app/config');

        if (response.code === 0 && response.data) {
          setAppConfig(response.data);
          cacheStore.set('api:app_config', response.data, { ttl: 3600 });
          setError(null);
        } else {
          console.error('[useAppConfig] Failed to load config:', response.code, response.message);
          setError('Failed to load app config');
        }
      } catch (err) {
        console.error('[useAppConfig] Error loading config:', err);
        setError('Failed to load app config');
      } finally {
        setLoading(false);
      }
    };

    fetchConfig();
  }, []);

  return {
    appConfig,
    loading,
    error
  };
}
