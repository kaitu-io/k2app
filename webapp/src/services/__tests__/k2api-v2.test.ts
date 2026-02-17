/**
 * k2api v2 Tests (F2 - RED phase)
 *
 * Tests for k2api rewritten to use cloudApi instead of window._k2.api.
 *
 * Key changes from k2api v1:
 * - Uses cloudApi.request() instead of window._k2.api.exec()
 * - No more 'api_request' action wrapping -- calls cloudApi directly
 * - Cache and SWR behavior preserved
 * - Auth success/401/402 handling preserved
 *
 * Run: cd webapp && npx vitest run --reporter=verbose src/services/__tests__/k2api-v2.test.ts
 */

import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';

// Mock cloudApi
const mockCloudApiRequest = vi.fn();
vi.mock('../cloud-api', () => ({
  cloudApi: {
    request: mockCloudApiRequest,
  },
}));

// Mock authService
vi.mock('../auth-service', () => ({
  authService: {
    getToken: vi.fn(),
    getRefreshToken: vi.fn(),
    setTokens: vi.fn(),
    clearTokens: vi.fn(),
  },
  TOKEN_STORAGE_KEY: 'k2.auth.token',
  REFRESH_TOKEN_STORAGE_KEY: 'k2.auth.refresh',
}));

// Mock auth store
vi.mock('../../stores/auth.store', () => ({
  useAuthStore: {
    setState: vi.fn(),
    getState: vi.fn(() => ({
      isAuthenticated: true,
    })),
  },
}));

describe('k2api v2 (using cloudApi)', () => {
  beforeEach(() => {
    vi.clearAllMocks();
    vi.resetModules();

    // Re-apply mocks after resetModules
    vi.mock('../cloud-api', () => ({
      cloudApi: {
        request: mockCloudApiRequest,
      },
    }));

    vi.mock('../auth-service', () => ({
      authService: {
        getToken: vi.fn(),
        getRefreshToken: vi.fn(),
        setTokens: vi.fn().mockResolvedValue(undefined),
        clearTokens: vi.fn().mockResolvedValue(undefined),
      },
      TOKEN_STORAGE_KEY: 'k2.auth.token',
      REFRESH_TOKEN_STORAGE_KEY: 'k2.auth.refresh',
    }));

    vi.mock('../../stores/auth.store', () => ({
      useAuthStore: {
        setState: vi.fn(),
        getState: vi.fn(() => ({
          isAuthenticated: true,
        })),
      },
    }));
  });

  afterEach(() => {
    vi.restoreAllMocks();
  });

  // ==================== Core: uses cloudApi, not _k2.api ====================

  describe('uses cloudApi instead of _k2.api', () => {
    it('should call cloudApi.request instead of window._k2.api', async () => {
      const { k2api } = await import('../k2api');
      const { cloudApi } = await import('../cloud-api');

      vi.mocked(cloudApi.request).mockResolvedValue({ code: 0, data: { id: 1 } });

      // k2api().exec delegates to cloudApi.request for api_request action
      await k2api().exec('api_request', {
        method: 'GET',
        path: '/api/user/info',
      });

      // Should call cloudApi.request, NOT window._k2.api
      expect(cloudApi.request).toHaveBeenCalledWith('GET', '/api/user/info', undefined);

      // Verify window._k2.api was NOT used
      // (window._k2.api doesn't exist in the new architecture)
      expect((window as any)._k2?.api).toBeUndefined();
    });
  });

  // ==================== Cache ====================

  describe('caching', () => {
    it('should return cached response without network call on second request', async () => {
      const { k2api } = await import('../k2api');
      const { cloudApi } = await import('../cloud-api');

      vi.mocked(cloudApi.request).mockResolvedValue({
        code: 0,
        data: { nodes: ['node1', 'node2'] },
      });

      // First request: hits network
      await k2api({ cache: { key: 'api:nodes', ttl: 60 } }).exec('api_request', {
        method: 'GET',
        path: '/api/nodes',
      });

      // Second request: should use cache
      const response = await k2api({ cache: { key: 'api:nodes' } }).exec('api_request', {
        method: 'GET',
        path: '/api/nodes',
      });

      // cloudApi.request should only be called once
      expect(cloudApi.request).toHaveBeenCalledTimes(1);
      expect(response.code).toBe(0);
      expect(response.data).toEqual({ nodes: ['node1', 'node2'] });
    });
  });

  // ==================== Auth Success ====================

  describe('auth success handling', () => {
    it('should save tokens after successful login response', async () => {
      const { k2api } = await import('../k2api');
      const { cloudApi } = await import('../cloud-api');
      const { authService } = await import('../auth-service');

      vi.mocked(cloudApi.request).mockResolvedValue({
        code: 0,
        data: {
          token: 'new-access-token',
          refreshToken: 'new-refresh-token',
          user: { id: 1 },
        },
      });

      const response = await k2api().exec('api_request', {
        method: 'POST',
        path: '/api/auth/login',
        body: { email: 'test@test.com', code: '123456' },
      });

      expect(response.code).toBe(0);

      // Should save tokens via authService
      expect(authService.setTokens).toHaveBeenCalledWith({
        accessToken: 'new-access-token',
        refreshToken: 'new-refresh-token',
      });
    });
  });

  // ==================== 401 Handling ====================

  describe('401 handling', () => {
    it('should clear auth and update store on 401 response', async () => {
      const { k2api } = await import('../k2api');
      const { cloudApi } = await import('../cloud-api');
      const { authService } = await import('../auth-service');
      const { useAuthStore } = await import('../../stores/auth.store');

      vi.mocked(cloudApi.request).mockResolvedValue({
        code: 401,
        message: 'Unauthorized',
      });

      const response = await k2api().exec('api_request', {
        method: 'GET',
        path: '/api/user/info',
      });

      expect(response.code).toBe(401);

      // Should clear tokens
      expect(authService.clearTokens).toHaveBeenCalled();

      // Should update auth store
      expect(useAuthStore.setState).toHaveBeenCalledWith({
        isAuthenticated: false,
      });
    });
  });

  // ==================== 402 Handling ====================

  describe('402 handling', () => {
    it('should return 402 response unchanged for membership expired', async () => {
      const { k2api } = await import('../k2api');
      const { cloudApi } = await import('../cloud-api');

      vi.mocked(cloudApi.request).mockResolvedValue({
        code: 402,
        message: 'Membership expired',
      });

      const response = await k2api().exec('api_request', {
        method: 'GET',
        path: '/api/user/info',
      });

      // 402 is returned unchanged -- consumer handles membership-expired UI
      expect(response.code).toBe(402);
      expect(response.message).toBe('Membership expired');
    });
  });

  // ==================== SWR Revalidate ====================

  describe('stale-while-revalidate', () => {
    it('should return stale cache immediately and trigger background refresh', async () => {
      const { k2api } = await import('../k2api');
      const { cloudApi } = await import('../cloud-api');

      // First call: populate cache
      vi.mocked(cloudApi.request).mockResolvedValue({
        code: 0,
        data: { version: 1 },
      });

      await k2api({ cache: { key: 'api:data', ttl: 60 } }).exec('api_request', {
        method: 'GET',
        path: '/api/data',
      });

      // Clear call count
      vi.mocked(cloudApi.request).mockClear();

      // Second call with SWR: should return cached immediately
      vi.mocked(cloudApi.request).mockResolvedValue({
        code: 0,
        data: { version: 2 },
      });

      const response = await k2api({
        cache: { key: 'api:data', ttl: 60, revalidate: true },
      }).exec('api_request', {
        method: 'GET',
        path: '/api/data',
      });

      // Should return stale data immediately
      expect(response.code).toBe(0);
      expect(response.data).toEqual({ version: 1 });

      // Background revalidation should be triggered (may or may not have completed)
      // Wait a tick for the background promise to fire
      await new Promise(resolve => setTimeout(resolve, 10));

      // cloudApi.request should have been called for background refresh
      expect(cloudApi.request).toHaveBeenCalled();
    });
  });
});
