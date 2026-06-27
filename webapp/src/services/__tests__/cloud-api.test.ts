/**
 * Cloud API Client Tests
 *
 * Tests for the cloud-api module that routes requests through
 * resolveAndFetch (direct → camouflage-node relay transport).
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
    getTokenEpoch: vi.fn().mockReturnValue(0),
  },
}));

// Mock antiblock — resolveEntry retained for antiblock.ts itself (not used by cloud-api directly)
vi.mock('../antiblock', () => ({
  resolveEntry: vi.fn().mockResolvedValue(''),
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

// Mock cache store
vi.mock('../cache-store', () => ({
  cacheStore: {
    clear: vi.fn(),
    get: vi.fn(),
    set: vi.fn(),
    delete: vi.fn(),
  },
}));

// Mock login-dialog store
const loginDialogOpen = vi.fn();
vi.mock('../../stores/login-dialog.store', () => ({
  useLoginDialogStore: {
    getState: () => ({ open: loginDialogOpen }),
  },
}));

// Mock i18n — echo the key so tests can assert on it
vi.mock('../../i18n/i18n', () => ({
  default: { t: (key: string) => key },
}));

// Mock resolve-and-fetch transport so request() can be driven deterministically
vi.mock('../resolve-and-fetch', () => ({
  resolveAndFetch: vi.fn(),
  CONTROL_PLANE_HOST: 'k2.52j.me',
}));

// Mock entry-pool seeding
vi.mock('../entry-pool', () => ({ addNodes: vi.fn() }));

import { cloudApi } from '../cloud-api';
import { authService } from '../auth-service';
import { useAuthStore } from '../../stores/auth.store';
import { cacheStore } from '../cache-store';
import { resolveAndFetch } from '../resolve-and-fetch';
import { addNodes } from '../entry-pool';

// Type helper for mocked functions
const mockedAuthService = vi.mocked(authService);
const mockedAuthStore = vi.mocked(useAuthStore);
const mockedCacheStore = vi.mocked(cacheStore);
const mockedResolveAndFetch = vi.mocked(resolveAndFetch);
const mockedAddNodes = vi.mocked(addNodes);

/** Helper: build a successful TransportResult */
function okTransport(status: number, body: unknown) {
  return {
    transport: 'ok' as const,
    status,
    json: async () => body,
  };
}

describe('Cloud API Client', () => {
  beforeEach(() => {
    vi.clearAllMocks();
    localStorage.clear();
    // Default: resolveAndFetch returns a generic success so tests only need to
    // override what's relevant to them.
    mockedResolveAndFetch.mockResolvedValue(okTransport(200, { code: 0, data: {} }));
    mockedAuthService.getTokenEpoch.mockReturnValue(0);
  });

  afterEach(() => {
    vi.restoreAllMocks();
  });

  // ==================== Basic HTTP Requests ====================

  describe('HTTP requests', () => {
    it('should send GET request with correct method and path', async () => {
      mockedAuthService.getToken.mockResolvedValue(null);
      mockedResolveAndFetch.mockResolvedValue(
        okTransport(200, { code: 0, data: { id: 1, email: 'test@test.com' } })
      );

      await cloudApi.request('GET', '/api/user/info');

      expect(mockedResolveAndFetch).toHaveBeenCalledTimes(1);
      const req = mockedResolveAndFetch.mock.calls[0][0];
      expect(req.method).toBe('GET');
      expect(req.path).toBe('/api/user/info');
    });

    it('should send POST request with JSON body', async () => {
      mockedAuthService.getToken.mockResolvedValue(null);
      mockedResolveAndFetch.mockResolvedValue(
        okTransport(200, { code: 0, data: { token: 'abc' } })
      );

      const body = { email: 'test@test.com', code: '123456' };
      await cloudApi.request('POST', '/api/auth/login', body);

      expect(mockedResolveAndFetch).toHaveBeenCalledTimes(1);
      const req = mockedResolveAndFetch.mock.calls[0][0];
      expect(req.method).toBe('POST');
      expect(req.path).toBe('/api/auth/login');
      expect(req.headers['Content-Type']).toBe('application/json');
      expect(JSON.parse(req.body!)).toEqual(body);
    });
  });

  // ==================== Auth Header Injection ====================

  describe('auth header injection', () => {
    it('should inject Authorization Bearer header when token exists', async () => {
      mockedAuthService.getToken.mockResolvedValue('my-access-token');
      mockedResolveAndFetch.mockResolvedValue(okTransport(200, { code: 0, data: {} }));

      await cloudApi.request('GET', '/api/user/info');

      const req = mockedResolveAndFetch.mock.calls[0][0];
      expect(req.headers['Authorization']).toBe('Bearer my-access-token');
    });

    it('should not include Authorization header when no token', async () => {
      mockedAuthService.getToken.mockResolvedValue(null);
      mockedResolveAndFetch.mockResolvedValue(okTransport(200, { code: 0, data: {} }));

      await cloudApi.request('GET', '/api/auth/send-code');

      const req = mockedResolveAndFetch.mock.calls[0][0];
      expect(req.headers['Authorization']).toBeUndefined();
    });
  });

  // ==================== 401 Handling with Token Refresh ====================

  describe('401 handling with token refresh', () => {
    it('should refresh token and retry on 401 response', async () => {
      mockedAuthService.getToken.mockResolvedValue('expired-token');
      mockedAuthService.getRefreshToken.mockResolvedValue('valid-refresh-token');
      mockedAuthService.setTokens.mockResolvedValue(undefined);

      mockedResolveAndFetch
        // First request: 401
        .mockResolvedValueOnce(okTransport(401, { code: 401, message: 'Unauthorized' }))
        // Refresh token request: success
        .mockResolvedValueOnce(okTransport(200, {
          code: 0,
          data: { token: 'new-access-token', refreshToken: 'new-refresh-token' },
        }))
        // Retry original request: success
        .mockResolvedValueOnce(okTransport(200, { code: 0, data: { id: 1 } }));

      const response = await cloudApi.request('GET', '/api/user/info');

      // Should have made 3 resolveAndFetch calls: original, refresh, retry
      expect(mockedResolveAndFetch).toHaveBeenCalledTimes(3);

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
      mockedAuthService.getToken.mockResolvedValue('expired-token');
      mockedAuthService.getRefreshToken.mockResolvedValue('expired-refresh-token');
      mockedAuthService.clearTokens.mockResolvedValue(undefined);

      mockedResolveAndFetch
        // First request: 401
        .mockResolvedValueOnce(okTransport(401, { code: 401, message: 'Unauthorized' }))
        // Refresh token request: also fails
        .mockResolvedValueOnce(okTransport(401, { code: 401, message: 'Refresh token expired' }));

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

  // ==================== Antiblock Integration ====================

  describe('antiblock integration', () => {
    it('test_cloud_api_passes_path_to_transport', async () => {
      mockedAuthService.getToken.mockResolvedValue(null);
      mockedResolveAndFetch.mockResolvedValue(okTransport(200, { code: 0, data: {} }));

      await cloudApi.request('GET', '/api/plans');

      const req = mockedResolveAndFetch.mock.calls[0][0];
      expect(req.path).toBe('/api/plans');
    });

    it('test_cloud_api_refresh_passes_refresh_path_to_transport', async () => {
      mockedAuthService.getToken.mockResolvedValue('expired');
      mockedAuthService.getRefreshToken.mockResolvedValue('refresh');
      mockedAuthService.setTokens.mockResolvedValue(undefined);

      mockedResolveAndFetch
        // First request: 401
        .mockResolvedValueOnce(okTransport(401, { code: 401, message: 'Unauthorized' }))
        // Refresh: success
        .mockResolvedValueOnce(okTransport(200, {
          code: 0,
          data: { token: 'new-token', refreshToken: 'new-refresh' },
        }))
        // Retry: success
        .mockResolvedValueOnce(okTransport(200, { code: 0, data: { id: 1 } }));

      await cloudApi.request('GET', '/api/user/info');

      // Refresh call should use the refresh path
      const refreshReq = mockedResolveAndFetch.mock.calls[1][0];
      expect(refreshReq.path).toBe('/api/auth/refresh');

      // Retry call should use original path
      const retryReq = mockedResolveAndFetch.mock.calls[2][0];
      expect(retryReq.path).toBe('/api/user/info');
    });
  });

  // ==================== Response Format ====================

  describe('response format', () => {
    it('should return SResponse format on success', async () => {
      const responseData = { id: 1, email: 'test@test.com', expiredAt: 9999999999 };
      mockedAuthService.getToken.mockResolvedValue('token');
      mockedResolveAndFetch.mockResolvedValue(okTransport(200, { code: 0, data: responseData }));

      const response = await cloudApi.request('GET', '/api/user/info');

      expect(response).toEqual({ code: 0, data: responseData });
    });

    it('should return error SResponse on transport fail', async () => {
      mockedAuthService.getToken.mockResolvedValue('token');
      mockedResolveAndFetch.mockResolvedValue({ transport: 'fail' });

      const response = await cloudApi.request('GET', '/api/user/info');

      expect(response.code).toBe(-1);
      expect(response.message).toBeDefined();
      expect(typeof response.message).toBe('string');
    });
  });

  // ==================== Convenience Methods (get/post) ====================

  describe('convenience methods', () => {
    it('test_get_convenience_method', async () => {
      mockedAuthService.getToken.mockResolvedValue(null);
      mockedResolveAndFetch.mockResolvedValue(okTransport(200, { code: 0, data: { id: 1 } }));

      await cloudApi.get('/api/x');

      expect(mockedResolveAndFetch).toHaveBeenCalledTimes(1);
      const req = mockedResolveAndFetch.mock.calls[0][0];
      expect(req.path).toBe('/api/x');
      expect(req.method).toBe('GET');
    });

    it('test_post_convenience_method', async () => {
      mockedAuthService.getToken.mockResolvedValue(null);
      mockedResolveAndFetch.mockResolvedValue(okTransport(200, { code: 0, data: { ok: true } }));

      const body = { email: 'user@example.com', code: '000000' };
      await cloudApi.post('/api/x', body);

      expect(mockedResolveAndFetch).toHaveBeenCalledTimes(1);
      const req = mockedResolveAndFetch.mock.calls[0][0];
      expect(req.path).toBe('/api/x');
      expect(req.method).toBe('POST');
      expect(req.headers['Content-Type']).toBe('application/json');
      expect(JSON.parse(req.body!)).toEqual(body);
    });
  });

  // ==================== Auto Token Handling on Auth Paths ====================

  describe('auto token handling on auth paths', () => {
    it('test_login_auto_saves_tokens', async () => {
      mockedAuthService.getToken.mockResolvedValue(null);
      mockedAuthService.setTokens.mockResolvedValue(undefined);
      mockedResolveAndFetch.mockResolvedValue(
        okTransport(200, { code: 0, data: { accessToken: 'at', refreshToken: 'rt' } })
      );

      const response = await cloudApi.post('/api/auth/login', {
        email: 'user@example.com',
        code: '123456',
      });

      expect(response.code).toBe(0);
      expect(mockedAuthService.setTokens).toHaveBeenCalledWith({
        accessToken: 'at',
        refreshToken: 'rt',
      });
    });

    it('test_register_auto_saves_tokens', async () => {
      mockedAuthService.getToken.mockResolvedValue(null);
      mockedAuthService.setTokens.mockResolvedValue(undefined);
      mockedResolveAndFetch.mockResolvedValue(
        okTransport(200, { code: 0, data: { accessToken: 'at', refreshToken: 'rt' } })
      );

      const response = await cloudApi.post('/api/auth/register', {
        email: 'new@example.com',
        code: '654321',
      });

      expect(response.code).toBe(0);
      expect(mockedAuthService.setTokens).toHaveBeenCalledWith({
        accessToken: 'at',
        refreshToken: 'rt',
      });
    });

    // Regression guard: /api/auth/login/password used to fall through the
    // exact-match path allowlist, so a successful password login returned
    // tokens that were silently discarded. Dialog closed, user not logged in.
    it('test_password_login_auto_saves_tokens', async () => {
      mockedAuthService.getToken.mockResolvedValue(null);
      mockedAuthService.setTokens.mockResolvedValue(undefined);
      mockedResolveAndFetch.mockResolvedValue(
        okTransport(200, { code: 0, data: { accessToken: 'at', refreshToken: 'rt' } })
      );

      const response = await cloudApi.post('/api/auth/login/password', {
        email: 'user@example.com',
        password: 'hunter2',
      });

      expect(response.code).toBe(0);
      expect(mockedAuthService.setTokens).toHaveBeenCalledWith({
        accessToken: 'at',
        refreshToken: 'rt',
      });
    });

    it('test_logout_auto_clears', async () => {
      mockedAuthService.getToken.mockResolvedValue('some-token');
      mockedAuthService.clearTokens.mockResolvedValue(undefined);
      mockedResolveAndFetch.mockResolvedValue(okTransport(200, { code: 0 }));

      const response = await cloudApi.post('/api/auth/logout');

      expect(response.code).toBe(0);
      expect(mockedAuthService.clearTokens).toHaveBeenCalled();
      expect(mockedCacheStore.clear).toHaveBeenCalled();
    });

    it('test_non_auth_path_does_not_save_tokens', async () => {
      mockedAuthService.getToken.mockResolvedValue('token');
      mockedResolveAndFetch.mockResolvedValue(okTransport(200, { code: 0, data: { id: 1 } }));

      await cloudApi.request('GET', '/api/user/info');

      expect(mockedAuthService.setTokens).not.toHaveBeenCalled();
    });
  });

  // ==================== X-K2-Client Header ====================

  describe('X-K2-Client header', () => {
    it('should include X-K2-Client header when _platform is available', async () => {
      mockedAuthService.getToken.mockResolvedValue('token');
      mockedResolveAndFetch.mockResolvedValue(okTransport(200, { code: 0, data: {} }));

      // Inject _platform
      (window as any)._platform = { os: 'macos', version: '0.4.0-beta.1', arch: 'arm64' };

      await cloudApi.request('GET', '/api/user/info');

      const req = mockedResolveAndFetch.mock.calls[0][0];
      expect(req.headers['X-K2-Client']).toBe('kaitu-service/0.4.0-beta.1 (macos; arm64)');

      delete (window as any)._platform;
    });

    it('should use unknown arch when arch is not provided', async () => {
      mockedAuthService.getToken.mockResolvedValue('token');
      mockedResolveAndFetch.mockResolvedValue(okTransport(200, { code: 0, data: {} }));

      (window as any)._platform = { os: 'ios', version: '0.4.0' };

      await cloudApi.request('GET', '/api/user/info');

      const req = mockedResolveAndFetch.mock.calls[0][0];
      expect(req.headers['X-K2-Client']).toBe('kaitu-service/0.4.0 (ios; unknown)');

      delete (window as any)._platform;
    });

    it('should not include X-K2-Client header when _platform is unavailable', async () => {
      mockedAuthService.getToken.mockResolvedValue('token');
      mockedResolveAndFetch.mockResolvedValue(okTransport(200, { code: 0, data: {} }));

      delete (window as any)._platform;

      await cloudApi.request('GET', '/api/user/info');

      const req = mockedResolveAndFetch.mock.calls[0][0];
      expect(req.headers['X-K2-Client']).toBeUndefined();
    });

    it('should include X-K2-Client in refresh token request', async () => {
      mockedAuthService.getToken.mockResolvedValue('expired');
      mockedAuthService.getRefreshToken.mockResolvedValue('refresh');
      mockedAuthService.setTokens.mockResolvedValue(undefined);

      mockedResolveAndFetch
        .mockResolvedValueOnce(okTransport(401, { code: 401, message: 'Unauthorized' }))
        .mockResolvedValueOnce(okTransport(200, {
          code: 0,
          data: { token: 'new', refreshToken: 'new-refresh' },
        }))
        .mockResolvedValueOnce(okTransport(200, { code: 0, data: {} }));

      (window as any)._platform = { os: 'windows', version: '0.3.22', arch: 'amd64' };

      await cloudApi.request('GET', '/api/user/info');

      // Refresh call (2nd resolveAndFetch) should have the header
      const refreshReq = mockedResolveAndFetch.mock.calls[1][0];
      expect(refreshReq.headers['X-K2-Client']).toBe('kaitu-service/0.3.22 (windows; amd64)');

      delete (window as any)._platform;
    });

    it('should send kaitu-router product token when platformType is gateway', async () => {
      mockedAuthService.getToken.mockResolvedValue(null);
      mockedResolveAndFetch.mockResolvedValue(okTransport(200, { code: 0 }));

      (window as any)._platform = {
        os: 'linux',
        arch: 'arm64',
        version: '0.4.5',
        platformType: 'gateway',
      };

      await cloudApi.request('GET', '/api/test');

      const req = mockedResolveAndFetch.mock.calls[0][0];
      expect(req.headers['X-K2-Client']).toBe('kaitu-router/0.4.5 (linux; arm64)');

      delete (window as any)._platform;
    });

    it('should send kaitu-service product token on linux desktop (cmd/k2)', async () => {
      mockedAuthService.getToken.mockResolvedValue(null);
      mockedResolveAndFetch.mockResolvedValue(okTransport(200, { code: 0 }));

      (window as any)._platform = {
        os: 'linux',
        arch: 'amd64',
        version: '0.4.5',
        platformType: 'desktop',
      };

      await cloudApi.request('GET', '/api/test');

      const req = mockedResolveAndFetch.mock.calls[0][0];
      expect(req.headers['X-K2-Client']).toBe('kaitu-service/0.4.5 (linux; amd64)');

      delete (window as any)._platform;
    });

    it('should send kaitu-service for desktop/mobile/web platformTypes', async () => {
      for (const pt of ['desktop', 'mobile', 'web'] as const) {
        vi.clearAllMocks();
        mockedAuthService.getTokenEpoch.mockReturnValue(0);
        mockedAuthService.getToken.mockResolvedValue(null);
        mockedResolveAndFetch.mockResolvedValue(okTransport(200, { code: 0 }));

        (window as any)._platform = {
          os: 'macos',
          arch: 'arm64',
          version: '0.4.5',
          platformType: pt,
        };

        await cloudApi.request('GET', '/api/test');

        const req = mockedResolveAndFetch.mock.calls[0][0];
        expect(req.headers['X-K2-Client']).toMatch(/^kaitu-service\//);

        delete (window as any)._platform;
      }
    });
  });

  // ==================== 401 Edge Cases ====================

  describe('401 edge cases', () => {
    it('test_401_null_refresh_token_skips_request', async () => {
      mockedAuthService.getToken.mockResolvedValue('expired-token');
      mockedAuthService.getRefreshToken.mockResolvedValue(null);
      mockedAuthService.clearTokens.mockResolvedValue(undefined);

      mockedResolveAndFetch
        // First request: 401
        .mockResolvedValueOnce(okTransport(401, { code: 401, message: 'Unauthorized' }));

      const response = await cloudApi.request('GET', '/api/user/info');

      // Should NOT have made a refresh transport call — only the original request
      expect(mockedResolveAndFetch).toHaveBeenCalledTimes(1);

      // Should return 401 immediately
      expect(response.code).toBe(401);

      // Should clear tokens and set isAuthenticated false
      expect(mockedAuthService.clearTokens).toHaveBeenCalled();
      expect(mockedAuthStore.setState).toHaveBeenCalledWith({
        isAuthenticated: false,
      });
    });

    it('should NOT clear fresh tokens when stale 401 arrives after login', async () => {
      // Reproduce the race condition:
      // 1. Request A fires with no token (e.g., CloudTunnelList retry)
      // 2. Login succeeds, fresh tokens saved (setTokens called → epoch increments)
      // 3. Request A's 401 response arrives — should NOT clear the fresh tokens

      let resolveStaleRequest!: (value: ReturnType<typeof okTransport>) => void;

      mockedResolveAndFetch
        // Call 1: stale request — delayed, will resolve later as 401
        .mockImplementationOnce(() => new Promise(resolve => { resolveStaleRequest = resolve; }))
        // Call 2: login request — returns immediately with success
        .mockResolvedValueOnce(okTransport(200, {
          code: 0,
          data: { accessToken: 'fresh-access', refreshToken: 'fresh-refresh' },
        }))
        // Call 3: retry of stale request with fresh token — success
        .mockResolvedValueOnce(okTransport(200, { code: 0, data: { tunnels: [] } }));

      // Initially no token, no refresh token
      mockedAuthService.getToken.mockResolvedValue(null);
      mockedAuthService.getRefreshToken.mockResolvedValue(null);
      mockedAuthService.setTokens.mockResolvedValue(undefined);
      mockedAuthService.clearTokens.mockResolvedValue(undefined);

      // Track epoch: starts at 0, increments when setTokens is called
      let epoch = 0;
      mockedAuthService.getTokenEpoch.mockImplementation(() => epoch);
      mockedAuthService.setTokens.mockImplementation(async () => { epoch++; });

      // Step 1: Fire stale request (hangs on resolveAndFetch)
      const stalePromise = cloudApi.request('GET', '/api/tunnels/k2v4');

      // Step 2: Login succeeds — setTokens called, epoch goes 0→1
      const loginResponse = await cloudApi.post('/api/auth/login', {
        email: 'user@example.com',
        code: '123456',
      });
      expect(loginResponse.code).toBe(0);
      expect(mockedAuthService.setTokens).toHaveBeenCalledWith({
        accessToken: 'fresh-access',
        refreshToken: 'fresh-refresh',
      });
      expect(epoch).toBe(1); // Tokens were saved

      // Step 3: Stale 401 arrives — should NOT clear fresh tokens
      resolveStaleRequest(okTransport(401, { code: 401, message: 'authentication failed' }));

      const staleResult = await stalePromise;

      // KEY ASSERTIONS: fresh tokens must survive the stale 401
      expect(mockedAuthService.clearTokens).not.toHaveBeenCalled();
      expect(mockedAuthStore.setState).not.toHaveBeenCalledWith({ isAuthenticated: false });

      // The stale request should have retried with fresh token and succeeded
      expect(staleResult.code).toBe(0);
    });

    it('should terminate normally when retried stale request also gets 401', async () => {
      // Proves no infinite loop: stale 401 → epoch mismatch → retry → 401 again → epoch matches → normal clear
      let resolveStaleRequest!: (value: ReturnType<typeof okTransport>) => void;

      mockedResolveAndFetch
        // Call 1: stale request — delayed
        .mockImplementationOnce(() => new Promise(resolve => { resolveStaleRequest = resolve; }))
        // Call 2: login request — success
        .mockResolvedValueOnce(okTransport(200, {
          code: 0,
          data: { accessToken: 'fresh-access', refreshToken: 'fresh-refresh' },
        }))
        // Call 3: retry of stale request — also 401 (fresh token is also invalid)
        .mockResolvedValueOnce(okTransport(401, { code: 401, message: 'token revoked' }));

      mockedAuthService.getToken.mockResolvedValue(null);
      mockedAuthService.getRefreshToken.mockResolvedValue(null);
      mockedAuthService.setTokens.mockResolvedValue(undefined);
      mockedAuthService.clearTokens.mockResolvedValue(undefined);

      let epoch = 0;
      mockedAuthService.getTokenEpoch.mockImplementation(() => epoch);
      mockedAuthService.setTokens.mockImplementation(async () => { epoch++; });

      // Step 1: Fire stale request
      const stalePromise = cloudApi.request('GET', '/api/tunnels/k2v4');

      // Step 2: Login succeeds (epoch 0→1)
      await cloudApi.post('/api/auth/login', { email: 'u@e.com', code: '123' });

      // Step 3: Stale 401 arrives → epoch mismatch → retry
      // Step 4: Retry also gets 401 → epoch matches (1===1) → normal clear path
      resolveStaleRequest(okTransport(401, { code: 401, message: 'authentication failed' }));

      const staleResult = await stalePromise;

      // Should return 401 (not hang or loop)
      expect(staleResult.code).toBe(401);

      // On the retry (epoch matched), normal 401 handling fires: no refresh token → clear
      expect(mockedAuthService.clearTokens).toHaveBeenCalled();
      expect(mockedAuthStore.setState).toHaveBeenCalledWith({ isAuthenticated: false });

      // Should have made exactly 3 resolveAndFetch calls (stale + login + retry), NOT more
      expect(mockedResolveAndFetch).toHaveBeenCalledTimes(3);
    });

    it('test_401_concurrent_shares_refresh', async () => {
      mockedAuthService.getToken.mockResolvedValue('expired-token');
      mockedAuthService.getRefreshToken.mockResolvedValue('valid-refresh');
      mockedAuthService.setTokens.mockResolvedValue(undefined);

      mockedResolveAndFetch
        // Call 1: first request gets 401
        .mockResolvedValueOnce(okTransport(401, { code: 401, message: 'Unauthorized' }))
        // Call 2: second request gets 401
        .mockResolvedValueOnce(okTransport(401, { code: 401, message: 'Unauthorized' }))
        // Call 3: single shared refresh succeeds
        .mockResolvedValueOnce(okTransport(200, {
          code: 0,
          data: { token: 'new-token', refreshToken: 'new-refresh' },
        }))
        // Call 4: retry for first request
        .mockResolvedValueOnce(okTransport(200, { code: 0, data: { id: 1 } }))
        // Call 5: retry for second request
        .mockResolvedValueOnce(okTransport(200, { code: 0, data: { id: 2 } }));

      // Fire two requests concurrently
      const [res1, res2] = await Promise.all([
        cloudApi.request('GET', '/api/user/info'),
        cloudApi.request('GET', '/api/user/orders'),
      ]);

      // Both should succeed
      expect(res1.code).toBe(0);
      expect(res2.code).toBe(0);

      // Count how many calls went to the refresh path
      const refreshCalls = mockedResolveAndFetch.mock.calls.filter(
        ([req]) => req.path === '/api/auth/refresh'
      );

      // Only ONE refresh call should have been made
      expect(refreshCalls).toHaveLength(1);
    });
  });

  // ==================== 403002 Device Class Mismatch ====================

  describe('403002 device class mismatch', () => {
    beforeEach(() => {
      (authService.clearTokens as any).mockClear();
      loginDialogOpen.mockClear();
    });

    it('should clearTokens + isAuthenticated=false + openLoginDialog when server returns 403002', async () => {
      mockedAuthService.getToken.mockResolvedValue('some-token');
      mockedAuthService.clearTokens.mockResolvedValue(undefined);
      mockedResolveAndFetch.mockResolvedValueOnce(
        okTransport(200, { code: 403002, message: 'device class mismatch' })
      );

      const result = await cloudApi.request('GET', '/api/test');

      expect(authService.clearTokens).toHaveBeenCalledTimes(1);
      // Mirror 401-with-no-refresh: must flip isAuthenticated so UI gates re-evaluate
      // even if user dismisses the dialog.
      expect(mockedAuthStore.setState).toHaveBeenCalledWith({ isAuthenticated: false });
      expect(loginDialogOpen).toHaveBeenCalledWith({
        trigger: 'device-class-mismatch',
        message: 'auth:auth.deviceClassMismatch',
      });
      expect(result.code).toBe(403002);
    });

    it('should NOT trigger logout on 402001/403001/422003', async () => {
      for (const code of [402001, 403001, 422003]) {
        mockedResolveAndFetch.mockResolvedValueOnce(
          okTransport(200, { code, message: 'whatever' })
        );
        mockedAuthService.getToken.mockResolvedValue('some-token');

        await cloudApi.request('GET', '/api/test');
      }

      expect(authService.clearTokens).not.toHaveBeenCalled();
      expect(loginDialogOpen).not.toHaveBeenCalled();
    });
  });

  // ==================== Cloud API Client — antiblock relay transport ====================

  describe('Cloud API Client — antiblock relay transport', () => {
    beforeEach(() => {
      vi.clearAllMocks();
      mockedAuthService.getToken.mockResolvedValue('tok');
      mockedAuthService.getTokenEpoch.mockReturnValue(0);
    });

    it('routes through resolveAndFetch and parses its json', async () => {
      mockedResolveAndFetch.mockResolvedValue({ transport: 'ok', status: 200, json: async () => ({ code: 0, data: { items: [] } }) } as any);
      const res = await cloudApi.get('/api/v20260717/tunnels');
      expect(mockedResolveAndFetch).toHaveBeenCalledWith(expect.objectContaining({ method: 'GET', path: '/api/v20260717/tunnels' }));
      expect(res.code).toBe(0);
    });

    it('maps transport:fail to code -1', async () => {
      mockedResolveAndFetch.mockResolvedValue({ transport: 'fail' } as any);
      const res = await cloudApi.get('/api/x');
      expect(res.code).toBe(-1);
    });

    it('seeds the entry pool from a successful tunnels response', async () => {
      const items = [{ id: 1, serverUrl: 'k2v5://a:443?ech=E&pin=sha256:P&ip=1.1.1.1', node: { ipv4: '1.1.1.1' } }];
      mockedResolveAndFetch.mockResolvedValue({ transport: 'ok', status: 200, json: async () => ({ code: 0, data: { items } }) } as any);
      await cloudApi.get('/api/v20260717/tunnels');
      expect(mockedAddNodes).toHaveBeenCalled();
      const seeded = mockedAddNodes.mock.calls[0][0];
      expect(seeded).toEqual([{ ip: '1.1.1.1', pin: 'sha256:P', ech: 'E' }]);
    });

    it('does NOT seed the pool for non-tunnels paths', async () => {
      mockedResolveAndFetch.mockResolvedValue({ transport: 'ok', status: 200, json: async () => ({ code: 0, data: {} }) } as any);
      await cloudApi.get('/api/user/info');
      expect(mockedAddNodes).not.toHaveBeenCalled();
    });

    it('still delegates 401 to refresh (atomicity preserved)', async () => {
      mockedAuthService.getRefreshToken.mockResolvedValue('rt');
      // first call: 401; refresh: ok; retry: ok
      mockedResolveAndFetch
        .mockResolvedValueOnce({ transport: 'ok', status: 401, json: async () => ({ code: 401 }) } as any) // request
        .mockResolvedValueOnce({ transport: 'ok', status: 200, json: async () => ({ code: 0, data: { token: 'n', refreshToken: 'n2' } }) } as any) // _doRefresh
        .mockResolvedValueOnce({ transport: 'ok', status: 200, json: async () => ({ code: 0, data: 'ok' }) } as any); // retry
      const res = await cloudApi.get('/api/user/info');
      expect(res.code).toBe(0);
      expect(mockedAuthService.setTokens).toHaveBeenCalled();
    });
  });
});
