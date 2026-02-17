/**
 * k2api - Unified API wrapper
 *
 * Responsibilities:
 * 1. Handle auth success: save tokens after login/register
 * 2. Handle 401/402 errors and update authStore
 * 3. Cache successful responses
 * 4. Return response unchanged (same interface as before)
 *
 * Uses cloudApi.request() for API calls instead of window._k2.api.
 *
 * Usage:
 * ```typescript
 * // All API requests now use api_request action
 * const response = await k2api().exec('api_request', {
 *   method: 'GET',
 *   path: '/api/user/info'
 * })
 *
 * // Login
 * const loginResponse = await k2api().exec('api_request', {
 *   method: 'POST',
 *   path: '/api/auth/login',
 *   body: { email, code }
 * })
 * // Tokens are automatically saved on success
 * ```
 */

import { cacheStore } from './cache-store';
import { cloudApi } from './cloud-api';
import { authService } from './auth-service';
import { useAuthStore } from '../stores/auth.store';

// Auth endpoints that return tokens
const AUTH_LOGIN_PATHS = ['/api/auth/login', '/api/auth/register', '/api/auth/refresh'];
const AUTH_LOGOUT_PATH = '/api/auth/logout';

// ============ 类型定义 ============

/**
 * k2api 配置
 */
interface K2ApiConfig {
  /** 缓存配置 */
  cache?: {
    key: string
    ttl?: number
    allowExpired?: boolean
    /** Stale-While-Revalidate: 立即返回缓存，后台静默刷新 */
    revalidate?: boolean
    /** 强制刷新：忽略缓存，直接请求 */
    forceRefresh?: boolean
  }
  /** 跳过认证检查（用于 login/register） */
  skipAuthCheck?: boolean
}

/**
 * API 响应格式（和 k2api().exec 一样）
 */
interface SResponse<T = any> {
  code: number
  data?: T
  message?: string
}

// ============ Helper Functions ============

/**
 * Check if path is an auth login endpoint
 */
function isAuthLoginPath(path: string): boolean {
  return AUTH_LOGIN_PATHS.some(p => path === p || path.startsWith(p + '?'));
}

/**
 * Check if path is logout endpoint
 */
function isLogoutPath(path: string): boolean {
  return path === AUTH_LOGOUT_PATH || path.startsWith(AUTH_LOGOUT_PATH + '?');
}

/**
 * Handle successful auth response - save tokens
 */
async function handleAuthSuccess(response: SResponse<any>, path: string): Promise<void> {
  // Only process successful login/register/refresh responses
  if (response.code !== 0 || !isAuthLoginPath(path)) {
    return;
  }

  const data = response.data;
  if (!data) return;

  // Extract tokens from response
  const accessToken = data.token || data.accessToken;
  const refreshToken = data.refreshToken;

  if (accessToken) {
    await authService.setTokens({
      accessToken,
      refreshToken,
    });
    console.debug('[K2Api] Tokens saved after auth success');
  }
}

/**
 * Handle successful logout - clear tokens
 */
async function handleLogoutSuccess(response: SResponse<any>, path: string): Promise<void> {
  if (response.code !== 0 || !isLogoutPath(path)) {
    return;
  }

  await authService.clearTokens();
  cacheStore.clear();
  console.debug('[K2Api] Tokens cleared after logout');
}

// ============ k2api 实现 ============

/**
 * Run API request via cloudApi
 */
async function _runApiRequest<T>(params: any): Promise<SResponse<T>> {
  return cloudApi.request<T>(params.method, params.path, params.body);
}

/**
 * 后台静默刷新（Stale-While-Revalidate）
 */
async function _revalidateInBackground<T>(
  action: string,
  params: any,
  cache: { key: string; ttl?: number }
): Promise<void> {
  try {
    const response = action === 'api_request'
      ? await _runApiRequest<T>(params)
      : await cloudApi.request<T>('POST', '/api/core', params);

    // 成功：更新缓存和 TTL
    if (response.code === 0 && response.data !== undefined) {
      cacheStore.set(cache.key, response.data, { ttl: cache.ttl });
      console.debug(`[K2Api] Background revalidation completed: ${cache.key}`);
    }

    // 401: 清除缓存、令牌并更新 auth state
    if (response.code === 401) {
      console.warn(`[K2Api] Background revalidation got 401: ${cache.key}`);
      useAuthStore.setState({
        isAuthenticated: false
      });
      cacheStore.clear();
      authService.clearTokens().catch(() => {
        // Silent failure in background
      });
    }

    // 402: 会员过期（user.store 会自动刷新用户信息）
    if (response.code === 402) {
      console.warn(`[K2Api] Background revalidation got 402: ${cache.key}`);
      // 过期状态从 user.expiredAt 计算，不维护单独的 flag
    }
  } catch (error) {
    // 静默失败，不影响用户体验
    console.debug(`[K2Api] Background revalidation failed: ${cache.key}`, error);
  }
}

/**
 * k2api wrapper
 *
 * @param config - 配置（缓存、认证检查等）
 * @returns 包含 methods 的对象
 *
 * @example
 * const response = await k2api({ cache: { key: 'api:user_info', ttl: 60 } }).exec_api('api_request', {
 *   method: 'GET',
 *   path: '/api/user/info'
 * })
 * if (response.code === 0) {
 *   // 使用 response.data
 * }
 */
function k2api(config: K2ApiConfig = {}) {

  /**
   * Internal implementation for running an API call
   */
  async function _call<T = any>(action: string, params?: any): Promise<SResponse<T>> {

    // ======== 1. 检查缓存 ========
    if (config.cache && !config.cache.forceRefresh) {
      const cached = cacheStore.get<T>(config.cache.key);
      if (cached !== null) {
        console.debug(`[K2Api] Cache hit: ${config.cache.key}`);

        // Stale-While-Revalidate: 后台刷新
        if (config.cache.revalidate) {
          console.debug(`[K2Api] Revalidating in background: ${config.cache.key}`);
          // 不等待，立即返回缓存
          _revalidateInBackground<T>(action, params, config.cache).catch(() => {
            // 静默失败
          });
        }

        // 返回和原来一样的格式
        return { code: 0, data: cached };
      }
    }

    // forceRefresh: 跳过缓存，直接请求
    if (config.cache?.forceRefresh) {
      console.debug(`[K2Api] Force refresh: ${config.cache.key}`);
    }

    // ======== 2. 发起请求（通过 cloudApi）========
    let response: SResponse<T>;

    try {
      if (action === 'api_request') {
        response = await _runApiRequest<T>(params);
      } else {
        // Non-API actions: delegate to cloudApi with a generic approach
        response = await cloudApi.request<T>('POST', '/api/core', params);
      }
    } catch (networkError) {
      console.error('[K2Api] Network error:', networkError);

      // 尝试返回过期缓存作为 fallback
      if (config.cache?.allowExpired) {
        const expired = cacheStore.get<T>(config.cache.key, true);
        if (expired !== null) {
          console.info(`[K2Api] Using expired cache as fallback: ${config.cache.key}`);
          return { code: 0, data: expired };
        }
      }

      // 返回错误格式
      return { code: -1, message: 'Network request failed' };
    }

    // ======== 4. Handle auth success (save tokens) ========
    if (action === 'api_request' && params?.path) {
      await handleAuthSuccess(response, params.path);
      await handleLogoutSuccess(response, params.path);
    }

    // ======== 6. 处理 401: 认证过期 ========
    if (response.code === 401) {
      console.warn('[K2Api] 401 Unauthorized');

      // 更新认证状态
      useAuthStore.setState({
        isAuthenticated: false
      });

      // 清除所有缓存和令牌
      cacheStore.clear();
      await authService.clearTokens();

      // 原样返回 response（调用方或 authStore 监听器负责弹窗）
      return response;
    }

    // ======== 7. 处理 402: 会员过期 ========
    if (response.code === 402) {
      console.warn('[K2Api] 402 Membership Expired');

      // 原样返回 response
      return response;
    }

    // ======== 8. 成功响应：缓存 ========
    if (response.code === 0 && response.data !== undefined && config.cache) {
      cacheStore.set(config.cache.key, response.data, { ttl: config.cache.ttl });
      console.debug(`[K2Api] Response cached: ${config.cache.key}`);
    }

    // ======== 9. 原样返回 response ========
    return response;
  }

  return {
    /**
     * Run an API call (named 'exec' for backward compatibility)
     */
    exec: _call,
  };
}

// ============ 导出 ============

export { k2api };
export type { K2ApiConfig, SResponse };
