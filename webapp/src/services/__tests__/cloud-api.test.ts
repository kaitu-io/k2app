/**
 * Cloud API Client Tests (F2 - RED phase)
 *
 * Tests for the new cloud-api module that replaces window._k2.api.exec
 * with direct HTTP fetch() calls to the cloud API.
 *
 * The cloud API client:
 * - Makes direct fetch() calls (GET, POST, etc.)
 * - Injects Authorization: Bearer {token} header when token exists
 * - Handles 401 by attempting token refresh, retrying once, then failing with logout
 * - Returns SResponse format: { code, data?, message? }
 *
 * Run: cd webapp && npx vitest run --reporter=verbose src/services/__tests__/cloud-api.test.ts
 */

import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';

// Mock authService before importing cloudApi
vi.mock('../auth-service', () => ({
  authService: {
    getToken: vi.fn(),
    getRefreshToken: vi.fn(),
    setTokens: vi.fn(),
    clearTokens: vi.fn(),
  },
}));

// Mock auth store
vi.mock('../../stores/auth.store', () => ({
  useAuthStore: {
    setState: vi.fn(),
    getState: vi.fn(() => ({
      isAuthenticated: true,
      setIsAuthenticated: vi.fn(),
    })),
  },
}));

import { cloudApi } from '../cloud-api';
import { authService } from '../auth-service';
import { useAuthStore } from '../../stores/auth.store';

// Type helper for mocked functions
const mockedAuthService = vi.mocked(authService);
const mockedAuthStore = vi.mocked(useAuthStore);

describe('Cloud API Client', () => {
  let originalFetch: typeof globalThis.fetch;

  beforeEach(() => {
    originalFetch = globalThis.fetch;
    vi.clearAllMocks();
  });

  afterEach(() => {
    globalThis.fetch = originalFetch;
    vi.restoreAllMocks();
  });

  // ==================== Basic HTTP Requests ====================

  describe('HTTP requests', () => {
    it('should send GET request with correct URL', async () => {
      const mockFetch = vi.fn().mockResolvedValue({
        ok: true,
        status: 200,
        json: () => Promise.resolve({ code: 0, data: { id: 1, email: 'test@test.com' } }),
      });
      globalThis.fetch = mockFetch;
      mockedAuthService.getToken.mockResolvedValue(null);

      await cloudApi.request('GET', '/api/user/info');

      expect(mockFetch).toHaveBeenCalledTimes(1);
      const [url, options] = mockFetch.mock.calls[0];
      expect(url).toBe('/api/user/info');
      expect(options.method).toBe('GET');
    });

    it('should send POST request with JSON body', async () => {
      const mockFetch = vi.fn().mockResolvedValue({
        ok: true,
        status: 200,
        json: () => Promise.resolve({ code: 0, data: { token: 'abc' } }),
      });
      globalThis.fetch = mockFetch;
      mockedAuthService.getToken.mockResolvedValue(null);

      const body = { email: 'test@test.com', code: '123456' };
      await cloudApi.request('POST', '/api/auth/login', body);

      expect(mockFetch).toHaveBeenCalledTimes(1);
      const [url, options] = mockFetch.mock.calls[0];
      expect(url).toBe('/api/auth/login');
      expect(options.method).toBe('POST');
      expect(options.headers['Content-Type']).toBe('application/json');
      expect(JSON.parse(options.body)).toEqual(body);
    });
  });

  // ==================== Auth Header Injection ====================

  describe('auth header injection', () => {
    it('should inject Authorization Bearer header when token exists', async () => {
      const mockFetch = vi.fn().mockResolvedValue({
        ok: true,
        status: 200,
        json: () => Promise.resolve({ code: 0, data: {} }),
      });
      globalThis.fetch = mockFetch;
      mockedAuthService.getToken.mockResolvedValue('my-access-token');

      await cloudApi.request('GET', '/api/user/info');

      const [, options] = mockFetch.mock.calls[0];
      expect(options.headers['Authorization']).toBe('Bearer my-access-token');
    });

    it('should not include Authorization header when no token', async () => {
      const mockFetch = vi.fn().mockResolvedValue({
        ok: true,
        status: 200,
        json: () => Promise.resolve({ code: 0, data: {} }),
      });
      globalThis.fetch = mockFetch;
      mockedAuthService.getToken.mockResolvedValue(null);

      await cloudApi.request('GET', '/api/auth/send-code');

      const [, options] = mockFetch.mock.calls[0];
      expect(options.headers['Authorization']).toBeUndefined();
    });
  });

  // ==================== 401 Handling with Token Refresh ====================

  describe('401 handling with token refresh', () => {
    it('should refresh token and retry on 401 response', async () => {
      // First call returns 401, refresh succeeds, retry returns success
      const mockFetch = vi.fn()
        // First request: 401
        .mockResolvedValueOnce({
          ok: false,
          status: 401,
          json: () => Promise.resolve({ code: 401, message: 'Unauthorized' }),
        })
        // Refresh token request: success
        .mockResolvedValueOnce({
          ok: true,
          status: 200,
          json: () => Promise.resolve({
            code: 0,
            data: {
              token: 'new-access-token',
              refreshToken: 'new-refresh-token',
            },
          }),
        })
        // Retry original request: success
        .mockResolvedValueOnce({
          ok: true,
          status: 200,
          json: () => Promise.resolve({ code: 0, data: { id: 1 } }),
        });
      globalThis.fetch = mockFetch;

      mockedAuthService.getToken.mockResolvedValue('expired-token');
      mockedAuthService.getRefreshToken.mockResolvedValue('valid-refresh-token');
      mockedAuthService.setTokens.mockResolvedValue(undefined);

      const response = await cloudApi.request('GET', '/api/user/info');

      // Should have made 3 fetch calls: original, refresh, retry
      expect(mockFetch).toHaveBeenCalledTimes(3);

      // Should have saved new tokens
      expect(mockedAuthService.setTokens).toHaveBeenCalledWith({
        accessToken: 'new-access-token',
        refreshToken: 'new-refresh-token',
      });

      // Should return successful response
      expect(response.code).toBe(0);
      expect(response.data).toEqual({ id: 1 });
    });

    it('should clear tokens and set isAuthenticated=false when refresh fails', async () => {
      const mockFetch = vi.fn()
        // First request: 401
        .mockResolvedValueOnce({
          ok: false,
          status: 401,
          json: () => Promise.resolve({ code: 401, message: 'Unauthorized' }),
        })
        // Refresh token request: also fails
        .mockResolvedValueOnce({
          ok: false,
          status: 401,
          json: () => Promise.resolve({ code: 401, message: 'Refresh token expired' }),
        });
      globalThis.fetch = mockFetch;

      mockedAuthService.getToken.mockResolvedValue('expired-token');
      mockedAuthService.getRefreshToken.mockResolvedValue('expired-refresh-token');
      mockedAuthService.clearTokens.mockResolvedValue(undefined);

      const response = await cloudApi.request('GET', '/api/user/info');

      // Should clear tokens
      expect(mockedAuthService.clearTokens).toHaveBeenCalled();

      // Should update auth store
      expect(mockedAuthStore.setState).toHaveBeenCalledWith({
        isAuthenticated: false,
      });

      // Should return 401 response
      expect(response.code).toBe(401);
    });
  });

  // ==================== Response Format ====================

  describe('response format', () => {
    it('should return SResponse format on success', async () => {
      const responseData = { id: 1, email: 'test@test.com', expiredAt: 9999999999 };
      const mockFetch = vi.fn().mockResolvedValue({
        ok: true,
        status: 200,
        json: () => Promise.resolve({ code: 0, data: responseData }),
      });
      globalThis.fetch = mockFetch;
      mockedAuthService.getToken.mockResolvedValue('token');

      const response = await cloudApi.request('GET', '/api/user/info');

      expect(response).toEqual({ code: 0, data: responseData });
    });

    it('should return error SResponse on network error', async () => {
      const mockFetch = vi.fn().mockRejectedValue(new TypeError('Failed to fetch'));
      globalThis.fetch = mockFetch;
      mockedAuthService.getToken.mockResolvedValue('token');

      const response = await cloudApi.request('GET', '/api/user/info');

      expect(response.code).toBe(-1);
      expect(response.message).toBeDefined();
      expect(typeof response.message).toBe('string');
    });
  });
});
